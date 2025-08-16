package kafka

import (
	"cmp"
	"github.com/IBM/sarama"
	"github.com/pkg/errors"
	"github.com/yuisofull/goload/pkg/message"
)

type Publisher struct {
	producer  sarama.SyncProducer
	marshaler Marshaler
}

func NewPublisher(cfg *PublisherConfig) (*Publisher, error) {
	var err error
	p := &Publisher{}

	p.marshaler = cmp.Or[Marshaler](cfg.Marshaler, DefaultMarshaler{})

	sconfig := sarama.NewConfig()
	{
		sconfig.Producer.Retry.Max = cfg.MaxRetry
		sconfig.Producer.RequiredAcks = sarama.WaitForAll
		sconfig.Producer.Return.Successes = true
		sconfig.Version = cmp.Or(cfg.Version, sarama.V2_0_0_0)
		sconfig.ClientID = cmp.Or(cfg.ClientID, "watermill")
	}
	p.producer, err = sarama.NewSyncProducer(cfg.BrokerHosts, sconfig)

	return p, err
}

type PublisherConfig struct {
	BrokerHosts []string
	Version     sarama.KafkaVersion
	ClientID    string
	MaxRetry    int
	Marshaler   Marshaler
}

func (p *Publisher) Publish(topic string, msgs ...*message.Message) error {
	for _, msg := range msgs {
		kafkaMsg, err := p.marshaler.Marshal(topic, msg)
		if err != nil {
			return errors.Wrapf(err, "cannot marshal message %s", msg.UUID)
		}

		// TODO: handle returned partition and offset
		_, _, err = p.producer.SendMessage(kafkaMsg)
		if err != nil {
			return errors.Wrapf(err, "cannot produce message %s", msg.UUID)
		}
	}

	return nil
}

func (p *Publisher) Close() error {
	return p.producer.Close()
}
