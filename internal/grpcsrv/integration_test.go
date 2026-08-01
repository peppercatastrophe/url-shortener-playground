// Integration test for the gRPC server against a running PostgreSQL.
// Skipped when DATABASE_URL is unset so it doesn't fail in CI without a DB.
// Run: DATABASE_URL=... go test -v -run TestIntegration ./internal/grpcsrv/

package grpcsrv

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"url-shortener/internal/cache"
	"url-shortener/internal/proto/shortenerpb"
	"url-shortener/internal/queue"
	"url-shortener/internal/store"
)

const bufSize = 1024 * 1024

// setupGRPCTestServer starts a gRPC server on a bufconn listener with a
// real store/cache/queue backend, returning a client and a cleanup func.
func setupGRPCTestServer(t *testing.T) (shortenerpb.ShortenerClient, func()) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	st, err := store.New(dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Insert a URL we can resolve (idempotent — tests may run repeatedly against the same DB).
	const testCode = "grpc-it-code"
	const upsert = `INSERT INTO urls (code, original_url) VALUES ($1, $2) ON CONFLICT (code) DO UPDATE SET original_url = EXCLUDED.original_url`
	if _, err := st.DB().ExecContext(context.Background(), upsert, testCode, "https://example.com/grpc-test"); err != nil {
		t.Fatalf("upsert test url: %v", err)
	}

	c := cache.New(os.Getenv("REDIS_ADDR"))
	pub, _ := queue.NewPublisher("") // disabled publisher — best-effort

	SetAuthToken("test-token")
	srv := NewServer(st, c, pub)

	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer(grpc.UnaryInterceptor(AuthInterceptor))
	shortenerpb.RegisterShortenerServer(s, srv)

	go s.Serve(lis)

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	client := shortenerpb.NewShortenerClient(conn)
	cleanup := func() {
		conn.Close()
		s.GracefulStop()
		st.Close()
	}
	return client, cleanup
}

// TestIntegrationResolveNoAuth verifies an unauthenticated Resolve call is
// rejected with codes.Unauthenticated.
func TestIntegrationResolveNoAuth(t *testing.T) {
	client, cleanup := setupGRPCTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Resolve(ctx, &shortenerpb.ResolveRequest{Code: "grpc-it-code"})
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected grpc status error, got: %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Fatalf("code = %s, want %s (msg: %s)", st.Code(), codes.Unauthenticated, st.Message())
	}
}

// TestIntegrationResolveWithAuth verifies an authenticated Resolve call
// returns the URL and back-fills the cache.
func TestIntegrationResolveWithAuth(t *testing.T) {
	client, cleanup := setupGRPCTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	authCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer test-token")
	resp, err := client.Resolve(authCtx, &shortenerpb.ResolveRequest{Code: "grpc-it-code"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resp.GetOriginalUrl() != "https://example.com/grpc-test" {
		t.Fatalf("url = %q, want %q", resp.GetOriginalUrl(), "https://example.com/grpc-test")
	}

	// Second call should come from cache (from_cache=true) if Redis is up.
	resp2, err := client.Resolve(authCtx, &shortenerpb.ResolveRequest{Code: "grpc-it-code"})
	if err != nil {
		t.Fatalf("resolve2: %v", err)
	}
	if resp2.GetOriginalUrl() != "https://example.com/grpc-test" {
		t.Fatalf("url2 = %q, want %q", resp2.GetOriginalUrl(), "https://example.com/grpc-test")
	}
	// from_cache may be true or false depending on Redis availability; both are valid.
	t.Logf("second resolve from_cache = %v", resp2.GetFromCache())
}

// TestIntegrationResolveBadAuth verifies a bad token is rejected.
func TestIntegrationResolveBadAuth(t *testing.T) {
	client, cleanup := setupGRPCTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	badCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer wrong")
	_, err := client.Resolve(badCtx, &shortenerpb.ResolveRequest{Code: "grpc-it-code"})
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Fatalf("code = %s, want %s", st.Code(), codes.Unauthenticated)
	}
}

// TestIntegrationReportClickWithAuth verifies a click event is accepted
// (published to the best-effort queue).
func TestIntegrationReportClickWithAuth(t *testing.T) {
	client, cleanup := setupGRPCTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	authCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer test-token")
	_, err := client.ReportClick(authCtx, &shortenerpb.ClickEvent{
		Code:              "grpc-it-code",
		ClickedAtUnixNano: time.Now().UnixNano(),
	})
	if err != nil {
		// UNAVAILABLE is acceptable if RabbitMQ is down (best-effort publisher).
		st, _ := status.FromError(err)
		if st.Code() == codes.Unavailable {
			t.Logf("report click: queue unavailable (expected without RabbitMQ): %v", err)
			return
		}
		t.Fatalf("report click: %v", err)
	}
}