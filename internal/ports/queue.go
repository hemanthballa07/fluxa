package ports

import "context"

// Publisher sends messages to a named exchange.
type Publisher interface {
	Publish(ctx context.Context, exchange, routingKey string, body []byte) error
	Close() error
}

// Consumer receives messages from a named queue.
type Consumer interface {
	Consume(ctx context.Context, queue string) (<-chan Delivery, error)
	Close() error
}

// Delivery wraps a single received message with ack/nack control.
type Delivery interface {
	Body() []byte
	Ack() error
	Nack(requeue bool) error
}
