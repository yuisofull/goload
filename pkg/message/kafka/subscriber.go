package kafka

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/yuisofull/goload/pkg/message"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IBM/sarama"
)

type ErrorHandler func(context.Context, error)

var ErrConsumerGroupEmpty = errors.New("consumer group is empty")

type Subscriber struct {
	config       *SubscriberConfig
	sconfig      *sarama.Config
	closing      chan struct{}
	closed       atomic.Bool
	wg           sync.WaitGroup
	errorHandler ErrorHandler
}

type SubscriberOption func(*Subscriber)

func WithErrorHandler(handler ErrorHandler) SubscriberOption {
	return func(s *Subscriber) {
		s.errorHandler = handler
	}
}

// NewSubscriber creates a new Kafka Subscriber.
func NewSubscriber(
	config *SubscriberConfig,
	opts ...SubscriberOption,
) (*Subscriber, error) {
	config.NackResendSleep = cmp.Or(config.NackResendSleep, time.Millisecond*100)
	config.ReconnectRetrySleep = cmp.Or(config.ReconnectRetrySleep, time.Second*1)
	config.Version = cmp.Or(config.Version, sarama.V2_0_0_0)
	config.ClientID = cmp.Or(config.ClientID, "watermill")
	config.Unmarshaler = cmp.Or(config.Unmarshaler, DefaultMarshaler{})
	if config.ConsumerGroup == "" {
		return nil, ErrConsumerGroupEmpty
	}
	if len(config.Brokers) == 0 {
		return nil, errors.New("brokers list is empty")
	}

	sconfig := sarama.NewConfig()
	{
		sconfig.Consumer.Return.Errors = true
		sconfig.Consumer.Offsets.Initial = sarama.OffsetOldest
		sconfig.Version = config.Version
		sconfig.ClientID = config.ClientID
		sconfig.Consumer.Offsets.AutoCommit.Enable = config.AutoCommit
	}
	subscriber := &Subscriber{
		config:       config,
		closing:      make(chan struct{}),
		sconfig:      sconfig,
		errorHandler: func(_ context.Context, _ error) {},
	}
	for _, opt := range opts {
		opt(subscriber)
	}
	subscriber.closed.Store(false)
	return subscriber, nil
}

type SubscriberConfig struct {
	// Kafka brokers list.
	Brokers []string

	// Unmarshaler is used to unmarshal messages from Kafka format into Watermill format.
	Unmarshaler Unmarshaler

	// Kafka client configuration.
	Version sarama.KafkaVersion
	// ClientID is used to identify the client.
	ClientID string

	// AutoCommit is used to enable or disable auto committing of offsets.
	AutoCommit bool

	// Kafka consumer group.
	// When empty, all messages from all partitions will be returned.
	ConsumerGroup string

	// How long after Nack message should be redelivered.
	NackResendSleep time.Duration

	// How long about unsuccessful reconnecting next reconnect will occur.
	ReconnectRetrySleep time.Duration

	InitializeTopicDetails *sarama.TopicDetail
}

// NoSleep can be set to SubscriberConfig.NackResendSleep and SubscriberConfig.ReconnectRetrySleep.
const NoSleep time.Duration = -1

func (s *Subscriber) Subscribe(baseCtx context.Context, topic string) (<-chan *message.Message, error) {
	if s.closed.Load() {
		return nil, errors.New("subscriber is closed")
	}

	out := make(chan *message.Message)

	client, err := sarama.NewClient(s.config.Brokers, s.sconfig)
	if err != nil {
		return nil, fmt.Errorf("cannot create kafka client: %w", err)
	}

	s.wg.Add(1)
	ctx, cancel := context.WithCancel(baseCtx)
	go func() {
		defer s.wg.Done()
		select {
		case <-s.closing:
		case <-baseCtx.Done():
		case <-ctx.Done():
			return
		}
		cancel()
	}()

	s.wg.Add(1)
	grp, err := sarama.NewConsumerGroupFromClient(s.config.ConsumerGroup, client)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case err, ok := <-grp.Errors():
				if !ok {
					return
				}
				s.errorHandler(ctx, err)
			case <-s.closing:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	handler := &consumerGroupHandler{
		ctx:             ctx,
		sconfig:         s.sconfig,
		out:             out,
		unmarshaler:     s.config.Unmarshaler,
		nackResendSleep: s.config.NackResendSleep,
	}

	s.wg.Add(1)
	go func() {
		defer func() {
			close(out)
			if closeErr := grp.Close(); closeErr != nil {
				s.errorHandler(ctx, closeErr)
			}
			if closeErr := client.Close(); closeErr != nil {
				s.errorHandler(ctx, closeErr)
			}
			cancel()
			s.wg.Done()
		}()
		if err := grp.Consume(ctx, []string{topic}, handler); err != nil {
			s.errorHandler(ctx, err)
		}
	}()
	return out, nil
}

func (s *Subscriber) Close() error {
	if s.closed.Load() {
		return nil
	}
	close(s.closing)
	s.closed.Store(true)
	s.wg.Wait()
	return nil
}

type consumerGroupHandler struct {
	ctx             context.Context
	sconfig         *sarama.Config
	out             chan *message.Message
	unmarshaler     Unmarshaler
	nackResendSleep time.Duration
}

func (c *consumerGroupHandler) Setup(session sarama.ConsumerGroupSession) error {
	return nil
}

func (c *consumerGroupHandler) Cleanup(session sarama.ConsumerGroupSession) error {
	return nil
}

func (c *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	baseCtx := c.ctx
	for kafkaMsg := range claim.Messages() {
		ctx := setPartitionToCtx(baseCtx, kafkaMsg.Partition)
		ctx = setPartitionOffsetToCtx(ctx, kafkaMsg.Offset)
		ctx = setMessageTimestampToCtx(ctx, kafkaMsg.Timestamp)
		ctx = setMessageKeyToCtx(ctx, kafkaMsg.Key)

		msg, err := c.unmarshaler.Unmarshal(kafkaMsg)
		if err != nil {
			return err
		}

		msg.SetContext(ctx)

		if err := c.send(ctx, session, msg); err != nil {
			return err
		}
		session.MarkMessage(kafkaMsg, "")
		if !c.sconfig.Consumer.Offsets.AutoCommit.Enable {
			// AutoCommit is disabled, so we should commit offset explicitly
			session.Commit()
		}
	}
	return nil
}

func (c *consumerGroupHandler) send(msgCtx context.Context, session sarama.ConsumerGroupSession, msg *message.Message) error {
	for {
		select {
		case c.out <- msg:
		case <-c.ctx.Done():
			return fmt.Errorf("context canceled before sending message: %w", c.ctx.Err())
		case <-session.Context().Done():
			return fmt.Errorf("session context canceled before sending message: %w", session.Context().Err())
		}
		select {
		case <-c.ctx.Done():
			return fmt.Errorf("context canceled before acking message: %w", c.ctx.Err())
		case <-session.Context().Done():
			return fmt.Errorf("session context canceled before acking message: %w", session.Context().Err())
		case <-msg.Acked():
			return nil
		case <-msg.Nacked():
			// reset acks, etc.
			msg = msg.Copy()
			msg.SetContext(msgCtx)

			if c.nackResendSleep != NoSleep {
				time.Sleep(c.nackResendSleep)
			}
		}
	}
}

func (s *Subscriber) SubscribeInitialize(topic string) (err error) {
	if s.config.InitializeTopicDetails == nil {
		return errors.New("s.config.InitializeTopicDetails is empty, cannot SubscribeInitialize")
	}

	clusterAdmin, err := sarama.NewClusterAdmin(s.config.Brokers, s.sconfig)
	if err != nil {
		return fmt.Errorf("cannot create cluster admin: %w", err)
	}
	defer func() {
		if closeErr := clusterAdmin.Close(); closeErr != nil {
			err = multierror.Append(err, closeErr)
		}
	}()

	if err := clusterAdmin.CreateTopic(topic, s.config.InitializeTopicDetails, false); err != nil && !strings.Contains(err.Error(), "Topic with this name already exists") {
		return fmt.Errorf("cannot create topic: %w", err)
	}

	return nil
}
