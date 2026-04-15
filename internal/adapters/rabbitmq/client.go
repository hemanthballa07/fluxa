package rabbitmq

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Client wraps an AMQP connection and implements both ports.Publisher and ports.Consumer.
// It declares all required exchanges and queues on construction.
type Client struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

// NewClient dials RabbitMQ, opens a channel, and declares the topology:
//   - exchange "events" (direct, durable) — ingest publishes here
//   - exchange "alerts" (fanout, durable)  — processor publishes fraud alerts here
//   - queue "events" bound to exchange "events" with routing key "events"
//   - queue "alerts" bound to exchange "alerts"
func NewClient(amqpURL string) (*Client, error) {
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: failed to dial %s: %w", amqpURL, err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rabbitmq: failed to open channel: %w", err)
	}

	if err := declareTopology(ch); err != nil {
		ch.Close()
		conn.Close()
		return nil, err
	}

	return &Client{conn: conn, channel: ch}, nil
}

func declareTopology(ch *amqp.Channel) error {
	exchanges := []struct {
		name, kind string
	}{
		{"events", "direct"},
		{"alerts", "fanout"},
	}
	for _, e := range exchanges {
		if err := ch.ExchangeDeclare(e.name, e.kind, true, false, false, false, nil); err != nil {
			return fmt.Errorf("rabbitmq: declare exchange %q: %w", e.name, err)
		}
	}

	queues := []struct {
		name, exchange, key string
	}{
		{"events", "events", "events"},
		{"alerts", "alerts", ""},
	}
	for _, q := range queues {
		if _, err := ch.QueueDeclare(q.name, true, false, false, false, nil); err != nil {
			return fmt.Errorf("rabbitmq: declare queue %q: %w", q.name, err)
		}
		if err := ch.QueueBind(q.name, q.key, q.exchange, false, nil); err != nil {
			return fmt.Errorf("rabbitmq: bind queue %q to exchange %q: %w", q.name, q.exchange, err)
		}
	}
	return nil
}

// Publish sends body to the given exchange with the given routing key.
func (c *Client) Publish(ctx context.Context, exchange, routingKey string, body []byte) error {
	err := c.channel.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
	if err != nil {
		return fmt.Errorf("rabbitmq: publish to %q: %w", exchange, err)
	}
	return nil
}

// Consume registers a consumer on the named queue and returns a channel of Delivery values.
func (c *Client) Consume(ctx context.Context, queue string) (<-chan Delivery, error) {
	msgs, err := c.channel.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: consume %q: %w", queue, err)
	}

	out := make(chan Delivery)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-msgs:
				if !ok {
					return
				}
				out <- &delivery{d: d}
			}
		}
	}()
	return out, nil
}

// Close shuts down the channel and connection.
func (c *Client) Close() error {
	if err := c.channel.Close(); err != nil {
		return err
	}
	return c.conn.Close()
}

// delivery wraps amqp091.Delivery to implement ports.Delivery.
type delivery struct {
	d amqp.Delivery
}

func (d *delivery) Body() []byte            { return d.d.Body }
func (d *delivery) Ack() error              { return d.d.Ack(false) }
func (d *delivery) Nack(requeue bool) error { return d.d.Nack(false, requeue) }

// Delivery is the interface that wraps a single received AMQP message.
// Exported here so callers can use it without importing ports directly.
type Delivery interface {
	Body() []byte
	Ack() error
	Nack(requeue bool) error
}
