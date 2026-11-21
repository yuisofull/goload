// Adapted from watermill-amqp v2.1.3, pkg/amqp/publisher.go. License notice in doc.go.

package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-kit/log"

	"github.com/yuisofull/goload/pkg/message"
)

var (
	ErrPublisherClosed = errors.New("publisher is closed")
	ErrNacked          = errors.New("message nacked by broker")
	ErrUnroutable      = errors.New("message is unroutable")
	ErrNoConfirm       = errors.New("no broker confirm received")
)

type Publisher struct {
	conn      *connectionWrapper
	config    Config
	marshaler Marshaler
	topic     topicClaim

	publishLock sync.Mutex
	channel     confirmChannel
}

type PublisherOption func(*Publisher)

func WithLogger(logger log.Logger) PublisherOption {
	return func(pub *Publisher) {
		pub.conn.logger = loggerAdapter{logger}
	}
}

func WithMarshaler(m Marshaler) PublisherOption {
	return func(pub *Publisher) {
		pub.marshaler = m
	}
}

func NewPublisher(cfg *Config, options ...PublisherOption) (*Publisher, error) {
	config, err := resolveConfig(cfg)
	if err != nil {
		return nil, err
	}

	conn := newConnectionWrapper(config, newDialer(config.ConfirmTimeout))
	p := &Publisher{
		conn:      conn,
		config:    config,
		marshaler: DefaultMarshaler{},
		channel:   confirmChannel{conn: conn},
	}
	for _, option := range options {
		option(p)
	}

	if err = conn.open(); err != nil {
		return nil, err
	}

	return p, nil
}

// Publish blocks until the broker has confirmed every message, or fails on the first one it has not.
func (p *Publisher) Publish(topic string, messages ...*message.Message) error {
	p.publishLock.Lock()
	defer p.publishLock.Unlock()

	if p.conn.closed.Load() {
		return ErrPublisherClosed
	}
	if err := p.topic.claim(topic, p.config.Queue); err != nil {
		return err
	}

	// Channel setup and topology declaration run broker round-trips before the
	// per-message confirm timeout starts, so bound them: a broker that stalls
	// here (e.g. a cold publish after a reconnect) is aborted after the timeout
	// rather than blocking the relay. The publish+confirm keeps its own
	// per-message timeout.
	if err := p.conn.withDeadline(p.config.ConfirmTimeout, func(conn amqpConnection) error {
		if err := p.channel.validate(conn); err != nil {
			return fmt.Errorf("cannot open channel: %w", err)
		}
		return p.preparePublishBindings(topic)
	}); err != nil {
		return err
	}

	for _, msg := range messages {
		if err := p.publishMessage(topic, msg); err != nil {
			return fmt.Errorf("cannot publish message %s: %w", msg.UUID, err)
		}
	}

	return nil
}

func (p *Publisher) Close() error {
	p.publishLock.Lock()
	defer p.publishLock.Unlock()

	return p.conn.close()
}

func (p *Publisher) publishMessage(topic string, msg *message.Message) error {
	amqpMsg, err := p.marshaler.Marshal(msg)
	if err != nil {
		return fmt.Errorf("cannot marshal message: %w", err)
	}

	ctx, cancel := context.WithTimeout(msg.Context(), p.config.ConfirmTimeout)
	defer cancel()

	if err = p.channel.publish(ctx, p.config.Exchange, topic, amqpMsg); err != nil {
		return err
	}

	p.conn.logger.Debug("Message confirmed by RabbitMQ", logFields{"topic": topic, "message_uuid": msg.UUID})

	return nil
}

// Watermill declares only the exchange here; an unroutable dispatch is an error, so the queue must exist too.
func (p *Publisher) preparePublishBindings(topic string) error {
	if p.channel.bindingsPrepared {
		return nil
	}

	if err := buildTopology(p.channel.amqpChan, p.config, topic); err != nil {
		return err
	}
	p.channel.bindingsPrepared = true

	return nil
}
