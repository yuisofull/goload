// Adapted from watermill-amqp v2.1.3, pkg/amqp/topology_builder.go. License notice in doc.go.

package rabbitmq

import (
	"errors"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	deadLetterExchangeArg = "x-dead-letter-exchange"
	deliveryLimitArg      = "x-delivery-limit"
	deadLetterStrategyArg = "x-dead-letter-strategy"
	overflowArg           = "x-overflow"
)

var (
	ErrTopicEmpty    = errors.New("rabbitmq: topic is empty")
	ErrTopicMismatch = errors.New("rabbitmq: a queue serves a single topic")
)

type topologyDeclarer interface {
	ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error
}

// buildTopology declares the work queue and its dead-letter parking lot. Watermill reads the
// exchange and queue settings from its Config; the dispatch queue has one shape, fixed here.
func buildTopology(ch topologyDeclarer, cfg Config, topic string) error {
	err := declareBoundQueue(ch, cfg.DeadLetterExchange, amqp.ExchangeFanout, cfg.DeadLetterQueue, "", amqp.Table{
		amqp.QueueTypeArg: amqp.QueueTypeQuorum,
	})
	if err != nil {
		return err
	}

	// Without a delivery limit a requeued poison message would loop forever.
	// at-least-once dead-lettering (which needs reject-publish overflow) keeps a
	// poison message if the dead-letter target is briefly unavailable.
	return declareBoundQueue(ch, cfg.Exchange, amqp.ExchangeDirect, cfg.Queue, topic, amqp.Table{
		amqp.QueueTypeArg:     amqp.QueueTypeQuorum,
		deadLetterExchangeArg: cfg.DeadLetterExchange,
		deliveryLimitArg:      cfg.DeliveryLimit,
		deadLetterStrategyArg: "at-least-once",
		overflowArg:           "reject-publish",
	})
}

func declareBoundQueue(ch topologyDeclarer, exchange, kind, queue, key string, queueArgs amqp.Table) error {
	if err := ch.ExchangeDeclare(exchange, kind, true, false, false, false, nil); err != nil {
		return fmt.Errorf("cannot declare exchange %s: %w", exchange, err)
	}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, queueArgs); err != nil {
		return fmt.Errorf("cannot declare queue %s: %w", queue, err)
	}
	if err := ch.QueueBind(queue, key, exchange, false, nil); err != nil {
		return fmt.Errorf("cannot bind queue %s to exchange %s: %w", queue, exchange, err)
	}
	return nil
}

type topicClaim struct {
	mu    sync.Mutex
	topic string
}

func (c *topicClaim) claim(topic, queue string) error {
	if topic == "" {
		return ErrTopicEmpty
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.topic == "" {
		c.topic = topic
	}
	if c.topic != topic {
		return fmt.Errorf("%w: queue %s already serves %s, cannot serve %s", ErrTopicMismatch, queue, c.topic, topic)
	}
	return nil
}
