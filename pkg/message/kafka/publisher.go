package kafka

import (
	"cmp"
	"fmt"
	"sync/atomic"

	"github.com/IBM/sarama"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/yuisofull/goload/pkg/message"
)

type Publisher struct {
	producer  sarama.SyncProducer
	marshaler Marshaler
	closed    atomic.Bool
	logger    log.Logger
}

func NewPublisher(cfg *PublisherConfig, options ...PublisherOption) (*Publisher, error) {
	var err error
	p := &Publisher{}

	p.marshaler = DefaultMarshaler{}

	sconfig := sarama.NewConfig()
	{
		sconfig.Producer.Retry.Max = cfg.MaxRetry
		sconfig.Producer.RequiredAcks = sarama.WaitForAll
		sconfig.Producer.Return.Successes = true
		sconfig.Version = cmp.Or(cfg.Version, sarama.V2_0_0_0)
		sconfig.ClientID = cmp.Or(cfg.ClientID, "watermill")
	}

	p.producer, err = sarama.NewSyncProducer(cfg.BrokerHosts, sconfig)

	for _, option := range options {
		option(p)
	}

	return p, err
}

type PublisherConfig struct {
	BrokerHosts []string
	Version     sarama.KafkaVersion
	ClientID    string
	MaxRetry    int
}
type PublisherOption func(*Publisher)

func WithLogger(logger log.Logger) PublisherOption {
	return func(pub *Publisher) {
		pub.logger = logger
	}
}

func WithMarshaler(m Marshaler) PublisherOption {
	return func(pub *Publisher) {
		pub.marshaler = m
	}
}

var ErrPublisherClosed = errors.New("publisher is closed")

func (p *Publisher) Publish(topic string, msgs ...*message.Message) (err error) {
	if p.closed.Load() {
		return ErrPublisherClosed
	}

	// Sarama's syncProducer panics with "send on closed channel" when the
	// producer has been closed. Recover and return it as a proper error.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("producer panic: %v", r)
		}
	}()

	for _, msg := range msgs {
		level.Info(p.logger).Log(
			"msg", "Sending message to Kafka",
			"topic", topic,
			"message_uuid", msg.UUID,
		)
		kafkaMsg, err := p.marshaler.Marshal(topic, msg)
		if err != nil {
			return errors.Wrapf(err, "cannot marshal message %s", msg.UUID)
		}

		partition, offset, err := p.producer.SendMessage(kafkaMsg)
		if err != nil {
			return errors.Wrapf(err, "cannot produce message %s", msg.UUID)
		}
		level.Info(p.logger).Log("msg", "Message sent to Kafka",
			"partition", partition,
			"offset", offset,
			"topic", topic,
			"message_uuid", msg.UUID,
		)
	}

	return nil
}

func (p *Publisher) Close() error {
	p.closed.Store(true)
	return p.producer.Close()
}
