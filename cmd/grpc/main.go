// Package main is the gRPC server entrypoint for the URL shortener.
// It exposes the Shortener service (Resolve, ReportClick) on a configured
// port, authenticating each call via a bearer-token interceptor. It shares
// the same store/cache/queue backend as the REST API binary; the two run as
// independent processes (see k8s/40-grpc.yaml, docker-compose).
package main

import (
	"context"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"

	"url-shortener/internal/cache"
	"url-shortener/internal/grpcsrv"
	"url-shortener/internal/proto/shortenerpb"
	"url-shortener/internal/queue"
	"url-shortener/internal/store"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://shortener:shortener@localhost:5432/shortener?sslmode=disable"
	}

	st, err := store.New(dsn)
	if err != nil {
		log.Fatalf("cannot open database: %v", err)
	}
	defer st.Close()

	// Apply embedded migrations on startup: same versioned path as the API
	// and worker, so the gRPC server is self-bootstrapping.
	if err := st.Migrate(context.Background()); err != nil {
		log.Fatalf("cannot migrate database: %v", err)
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	c := cache.New(redisAddr)
	if err := c.Ping(context.Background()); err != nil {
		log.Printf("WARNING: cache disabled, will fall back to database: %v", err)
	} else {
		log.Println("cache connected")
	}

	rabbitAddr := os.Getenv("RABBITMQ_ADDR")
	if rabbitAddr == "" {
		rabbitAddr = "amqp://guest:guest@localhost:5672/"
	}
	pub, err := queue.NewPublisher(rabbitAddr)
	if err != nil {
		log.Printf("WARNING: click queue disabled, events will be lost: %v", err)
	} else if rabbitAddr != "" {
		log.Println("queue connected")
	}
	defer pub.Close()

	grpcsrv.SetAuthToken(os.Getenv("GRPC_AUTH_TOKEN"))

	addr := os.Getenv("GRPC_ADDR")
	if addr == "" {
		addr = ":50051"
	}
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v", addr, err)
	}

	// Register the Shortener server with the auth unary interceptor so every
	// call is authenticated before reaching the handler.
	srv := grpc.NewServer(grpc.UnaryInterceptor(grpcsrv.AuthInterceptor))
	shortenerpb.RegisterShortenerServer(srv, grpcsrv.NewServer(st, c, pub))

	log.Printf("gRPC server listening on %s", addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}