// Package queue provides RabbitMQ access for click-event messaging.
// The API publishes click events; the worker consumes them.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ClicksQueue is the name of the queue that carries click events.
const ClicksQueue = "clicks"

// ClickEvent is the message published on each redirect.
type ClickEvent struct {
	Code      string    `json:"code"`
	ClickedAt time.Time `json:"clicked_at"`
}

// dial opens a connection and channel to RabbitMQ and declares the queue.
// The queue is declared durable so messages survive a broker restart.
func dial(addr string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(addr)
	if err != nil {
		return nil, nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("open channel: %w", err)
	}
	if _, err := ch.QueueDeclare(
		ClicksQueue, // name
		true,         // durable — messages survive broker restart
		false,        // autoDelete
		false,        // exclusive
		false,        // noWait
		nil,          // args
	); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("declare queue: %w", err)
	}
	// Fair dispatch: don't dispatch a new message to a worker until it
	// has acknowledged the previous one.
	if err := ch.Qos(1, 0, false); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("set qos: %w", err)
	}
	return conn, ch, nil
}

// Publisher publishes click events to RabbitMQ.
// A nil Publisher is disabled: Publish is a no-op. This lets the API
// run without RabbitMQ, so development is not blocked.
type Publisher struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewPublisher connects to RabbitMQ at addr and returns a Publisher.
// An empty addr returns a disabled (nil-channel) Publisher.
func NewPublisher(addr string) (*Publisher, error) {
	if addr == "" {
		return &Publisher{}, nil
	}
	conn, ch, err := dial(addr)
	if err != nil {
		return nil, err
	}
	return &Publisher{conn: conn, ch: ch}, nil
}

// Publish sends a click event to the queue. It is best-effort: errors
// are returned to the caller, which should log them but not fail the
// redirect, so the read path stays decoupled from the queue.
func (p *Publisher) Publish(ctx context.Context, evt ClickEvent) error {
	if p.ch == nil {
		return nil
	}
	body, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return p.ch.PublishWithContext(
		ctx,
		"",           // exchange — use the default
		ClicksQueue,  // routing key — the queue name
		false,        // mandatory
		false,        // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // survive broker restart
			Timestamp:    evt.ClickedAt,
			Body:         body,
		},
	)
}

// Close releases the channel and connection. Safe on a disabled Publisher.
func (p *Publisher) Close() error {
	if p.ch == nil {
		return nil
	}
	if err := p.ch.Close(); err != nil {
		return err
	}
	return p.conn.Close()
}

// Consumer consumes click events from RabbitMQ.
type Consumer struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewConsumer connects to RabbitMQ at addr and returns a Consumer.
func NewConsumer(addr string) (*Consumer, error) {
	conn, ch, err := dial(addr)
	if err != nil {
		return nil, err
	}
	return &Consumer{conn: conn, ch: ch}, nil
}

// Consume returns a channel of deliveries from the clicks queue.
// Messages are delivered with manual acknowledgment so a worker crash
// mid-process returns the message to the queue.
func (c *Consumer) Consume(ctx context.Context) (<-chan amqp.Delivery, error) {
	return c.ch.Consume(
		ClicksQueue, // queue
		"",          // consumer — let RabbitMQ generate a tag
		false,       // autoAck — manual ack so crashes don't lose messages
		false,       // exclusive
		false,       // noLocal
		false,       // noWait
		nil,          // args
	)
}

// Close releases the channel and connection.
func (c *Consumer) Close() error {
	if err := c.ch.Close(); err != nil {
		return err
	}
	return c.conn.Close()
}
