package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

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

	if err := st.Migrate(context.Background()); err != nil {
		log.Fatalf("cannot migrate database: %v", err)
	}


	rabbitAddr := os.Getenv("RABBITMQ_ADDR")
	if rabbitAddr == "" {
		rabbitAddr = "amqp://guest:guest@localhost:5672/"
	}

	consumer, err := queue.NewConsumer(rabbitAddr)
	if err != nil {
		log.Fatalf("cannot connect to queue: %v", err)
	}
	defer consumer.Close()
	log.Println("worker connected to queue, waiting for messages")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	deliveries, err := consumer.Consume(ctx)
	if err != nil {
		log.Fatalf("cannot consume: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("shutdown signal received, stopping worker")
			return
		case d, ok := <-deliveries:
			if !ok {
				log.Println("delivery channel closed, stopping worker")
				return
			}
			if err := processClick(ctx, st, d); err != nil {
				log.Printf("process click: %v — requeuing", err)
				// Negative-ack with requeue so the message is retried.
				// A persistent failure will loop; for a learning project
				// this is acceptable. In production, use a dead-letter queue.
				if nackErr := d.Nack(false, true); nackErr != nil {
					log.Printf("nack: %v", nackErr)
				}
				continue
			}
			if err := d.Ack(false); err != nil {
				log.Printf("ack: %v", err)
			}
		}
	}
}

// processClick decodes a delivery, inserts a click row, and returns nil on success.
func processClick(ctx context.Context, st *store.Store, d amqp.Delivery) error {
	var evt queue.ClickEvent
	if err := json.Unmarshal(d.Body, &evt); err != nil {
		return err
	}
	if evt.ClickedAt.IsZero() {
		evt.ClickedAt = time.Now()
	}
	return st.InsertClick(ctx, evt.Code, evt.ClickedAt)
}
