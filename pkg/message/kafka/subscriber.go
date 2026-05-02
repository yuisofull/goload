package kafka

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"

	"github.com/yuisofull/goload/pkg/message"

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
	logger       log.Logger
}

type SubscriberOption func(*Subscriber)

func WithErrorHandler(handler ErrorHandler) SubscriberOption {
	return func(s *Subscriber) {
		s.errorHandler = handler
	}
}

func WithLog(logger log.Logger) SubscriberOption {
	return func(s *Subscriber) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// NewSubscriber creates a new Kafka Subscriber.
func NewSubscriber(
	config *SubscriberConfig,
	opts ...SubscriberOption,
) (*Subscriber, error) {
	config.NackResendSleep = cmp.Or(config.NackResendSleep, time.Millisecond*100)
	config.ReconnectRetrySleep = cmp.Or(config.ReconnectRetrySleep, time.Second*1)
	config.Version = cmp.Or(config.Version, sarama.V3_6_0_0)
	config.ClientID = cmp.Or(config.ClientID, "watermill")
	config.Unmarshaler = cmp.Or[Unmarshaler](config.Unmarshaler, DefaultMarshaler{})
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
		logger:       log.NewNopLogger(),
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

	logger := log.With(s.logger, "provider", "kafka",
		"topic", topic,
		"consumerGroup", s.config.ConsumerGroup,
		"kafka_consumer_uuid", uuid.New())

	level.Info(logger).Log("msg", "Subscribing to Kafka topic")
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
			level.Info(logger).Log("msg", "Subscriber is closing")
		case <-baseCtx.Done():
			level.Info(logger).Log("msg", "Context canceled")
		case <-ctx.Done():
			level.Info(logger).Log("msg", "Context canceled")
			return
		}
		cancel()
	}()

	s.wg.Add(1)
	grp, err := sarama.NewConsumerGroupFromClient(s.config.ConsumerGroup, client)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cannot create consumer group: %w", err)
	}
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
		logger:          logger,
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
		// Retry grp.Consume on transient errors (e.g. topic-not-found during startup).
		for {
			if ctx.Err() != nil {
				return
			}
			if err := grp.Consume(ctx, []string{topic}, handler); err != nil {
				if ctx.Err() != nil {
					// Context canceled: shutdown, not an error.
					return
				}
				s.errorHandler(ctx, err)
				// Back off before retrying.
				sleep := s.config.ReconnectRetrySleep
				if sleep <= 0 {
					sleep = time.Second
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(sleep):
				}
			} else {
				// Consume returned nil — normal exit (context canceled or partition EOF).
				return
			}
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
	logger          log.Logger
}

func (c *consumerGroupHandler) Setup(session sarama.ConsumerGroupSession) error {
	return nil
}

func (c *consumerGroupHandler) Cleanup(session sarama.ConsumerGroupSession) error {
	return nil
}

func (c *consumerGroupHandler) ConsumeClaim(
	session sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
) error {
	baseCtx := c.ctx
	for kafkaMsg := range claim.Messages() {
		logger := log.With(c.logger, "kafka_partition_offset", kafkaMsg.Offset, "kafka_partition", kafkaMsg.Partition)
		level.Debug(logger).Log("msg", "Received message from Kafka")

		ctx := setPartitionToCtx(baseCtx, kafkaMsg.Partition)
		ctx = setPartitionOffsetToCtx(ctx, kafkaMsg.Offset)
		ctx = setMessageTimestampToCtx(ctx, kafkaMsg.Timestamp)
		ctx = setMessageKeyToCtx(ctx, kafkaMsg.Key)

		msg, err := c.unmarshaler.Unmarshal(kafkaMsg)
		if err != nil {
			return err
		}

		msg.SetContext(ctx)

		if err := c.send(ctx, session, msg, logger); err != nil {
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

func (c *consumerGroupHandler) send(
	msgCtx context.Context,
	session sarama.ConsumerGroupSession,
	msg *message.Message,
	logger log.Logger,
) error {
	for {
		select {
		case c.out <- msg:
			level.Debug(logger).Log("msg", "Message sent to Consumer")
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
			level.Debug(logger).Log("msg", "Message acked")
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

	// Build a dedicated admin config: disable background metadata refresh and
	// limit retries so we never attempt to reach a stale/previous broker address.
	adminCfg := *s.sconfig
	adminCfg.Metadata.RefreshFrequency = 0
	adminCfg.Metadata.Retry.Max = 5
	adminCfg.Metadata.Retry.Backoff = 500 * time.Millisecond

	// Kafka may not be fully ready immediately after the port is reachable.
	// Retry creating the cluster admin with backoff to handle transient startup failures.
	const maxAttempts = 5
	retryBackoff := 500 * time.Millisecond
	var clusterAdmin sarama.ClusterAdmin
	for i := range maxAttempts {
		clusterAdmin, err = sarama.NewClusterAdmin(s.config.Brokers, &adminCfg)
		if err == nil {
			break
		}
		if i < maxAttempts-1 {
			time.Sleep(retryBackoff)
			retryBackoff *= 2
		}
	}
	if err != nil {
		return fmt.Errorf("cannot create cluster admin: %w", err)
	}
	defer func() {
		if closeErr := clusterAdmin.Close(); closeErr != nil {
			err = multierror.Append(err, closeErr)
		}
	}()

	if err := clusterAdmin.CreateTopic(
		topic,
		s.config.InitializeTopicDetails,
		false,
	); err != nil &&
		!strings.Contains(err.Error(), "Topic with this name already exists") {
		return fmt.Errorf("cannot create topic: %w", err)
	}
	level.Info(s.logger).Log("msg", "Created Kafka topic", "topic", topic)

	return nil
}

func (s *Subscriber) waitForTopicCreation(ctx context.Context, clusterAdmin sarama.ClusterAdmin, topic string) error {
	logger := log.With(s.logger, "topic", topic)
	level.Debug(logger).Log("msg", "Waiting for topic creation to be confirmed")
	pollInterval := 500 * time.Millisecond
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for topic creation: %w", ctx.Err())
		default:
		}

		topics, err := clusterAdmin.ListTopics()
		if err != nil {
			level.Debug(logger).Log("msg", "Failed to list topics", "attempt", attempt+1, "err", err)
		} else {
			if topicDetail, exists := topics[topic]; exists {
				if s.verifyPartitionsReady(clusterAdmin, topic, topicDetail, logger, attempt) {
					level.Debug(logger).Log("msg", "Topic and partitions creation confirmed", "attempt", attempt+1)
					return nil
				}
			}
		}

		level.Debug(logger).
			Log("msg", "Topic not yet available, retrying", "attempt", attempt+1, "retry_in", pollInterval.String())

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("context cancelled while waiting for topic creation: %w", ctx.Err())
		case <-timer.C:
			// Continue to next attempt
		}

		attempt++
	}
}

func (s *Subscriber) verifyPartitionsReady(
	clusterAdmin sarama.ClusterAdmin,
	topic string,
	topicDetail sarama.TopicDetail,
	logger log.Logger,
	attempt int,
) bool {
	metadata, err := clusterAdmin.DescribeTopics([]string{topic})
	if err != nil {
		level.Debug(logger).Log("msg", "Failed to describe topic", "attempt", attempt+1, "error", err.Error())
		return false
	}

	if len(metadata) == 0 {
		level.Debug(logger).Log("msg", "No topic metadata returned", "attempt", attempt+1)
		return false
	}

	topicMeta := metadata[0]
	if !errors.Is(topicMeta.Err, sarama.ErrNoError) {
		level.Debug(logger).
			Log("msg", "Topic metadata contains error", "attempt", attempt+1, "error", topicMeta.Err.Error())
		return false
	}

	// Check that all expected partitions exist and have leaders
	expectedPartitions := topicDetail.NumPartitions
	if int32(len(topicMeta.Partitions)) < expectedPartitions {
		level.Debug(logger).
			Log("msg", "Not all partitions available yet", "attempt", attempt+1, "expected_partitions", expectedPartitions, "available_partitions", len(topicMeta.Partitions))
		return false
	}

	// Verify each partition has a leader
	for _, partition := range topicMeta.Partitions {
		if !errors.Is(partition.Err, sarama.ErrNoError) {
			level.Debug(logger).
				Log("msg", "Partition has error", "attempt", attempt+1, "partition", partition.ID, "error", partition.Err.Error())
			return false
		}
		if partition.Leader == -1 {
			level.Debug(logger).
				Log("msg", "Partition has no leader", "attempt", attempt+1, "partition", partition.ID)
			return false
		}
	}

	return true
}
