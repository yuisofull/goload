package kafka_test

import (
	"context"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"

	"github.com/yuisofull/goload/pkg/message"
	"github.com/yuisofull/goload/pkg/message/kafka"
)

var version = sarama.V4_0_0_0

// startKafka starts a Kafka container and returns the broker address and a cleanup function.
func startKafka(t *testing.T) (brokers []string, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	kafkaContainer, err := tckafka.Run(ctx, "confluentinc/confluent-local:8.0.4")
	require.NoError(t, err, "failed to start Kafka container")

	brokerAddr, err := kafkaContainer.Brokers(ctx)
	require.NoError(t, err, "failed to get Kafka broker address")

	return brokerAddr, func() {
		if err := kafkaContainer.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate Kafka container: %v", err)
		}
	}
}

// TestPublishSubscribe verifies that a message published to a topic is received
// by the subscriber with the same UUID, payload, and metadata intact.
func TestPublishSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanup := startKafka(t)
	defer cleanup()

	topic := "test.publish.subscribe"
	consumerGroup := "test-group-pubsub"

	// Create subscriber first so it joins the group before publishing.
	sub, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
		Brokers:             brokers,
		ConsumerGroup:       consumerGroup,
		Version:             version,
		NackResendSleep:     50 * time.Millisecond,
		ReconnectRetrySleep: 200 * time.Millisecond,
		InitializeTopicDetails: &sarama.TopicDetail{
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	})
	require.NoError(t, err)
	defer sub.Close()

	// Initialize the topic so the subscriber can join before the publisher sends.
	require.NoError(t, sub.SubscribeInitialize(topic))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, topic)
	require.NoError(t, err)

	// Publish a message.
	pub, err := kafka.NewPublisher(&kafka.PublisherConfig{
		BrokerHosts: brokers,
		Version:     sarama.V3_0_0_0,
		MaxRetry:    3,
	})
	require.NoError(t, err)
	defer pub.Close()

	want := message.NewMessage("test-uuid-1", []byte(`{"hello":"world"}`))
	want.Metadata.Set("service", "test")

	require.NoError(t, pub.Publish(topic, want))

	// Wait for the message.
	select {
	case got, ok := <-msgCh:
		require.True(t, ok, "channel closed unexpectedly")
		assert.Equal(t, want.UUID, got.UUID)
		assert.Equal(t, want.Payload, got.Payload)
		assert.Equal(t, "test", got.Metadata.Get("service"))
		got.Ack()
	case <-ctx.Done():
		t.Fatal("timed out waiting for message")
	}
}

// TestPublishMultipleMessages verifies that multiple messages are delivered in order.
func TestPublishMultipleMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanup := startKafka(t)
	defer cleanup()

	topic := "test.multiple.messages"
	consumerGroup := "test-group-multi"

	sub, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
		Brokers:             brokers,
		ConsumerGroup:       consumerGroup,
		Version:             version,
		NackResendSleep:     50 * time.Millisecond,
		ReconnectRetrySleep: 200 * time.Millisecond,
		InitializeTopicDetails: &sarama.TopicDetail{
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	})
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, sub.SubscribeInitialize(topic))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, topic)
	require.NoError(t, err)

	pub, err := kafka.NewPublisher(&kafka.PublisherConfig{
		BrokerHosts: brokers,
		Version:     version,
		MaxRetry:    3,
	})
	require.NoError(t, err)
	defer pub.Close()

	messages := []*message.Message{
		message.NewMessage("uuid-1", []byte("payload-1")),
		message.NewMessage("uuid-2", []byte("payload-2")),
		message.NewMessage("uuid-3", []byte("payload-3")),
	}
	require.NoError(t, pub.Publish(topic, messages...))

	for i, expected := range messages {
		select {
		case got, ok := <-msgCh:
			require.True(t, ok, "channel closed unexpectedly at message %d", i)
			assert.Equal(t, expected.UUID, got.UUID, "UUID mismatch at position %d", i)
			assert.Equal(t, expected.Payload, got.Payload, "Payload mismatch at position %d", i)
			got.Ack()
		case <-ctx.Done():
			t.Fatalf("timed out waiting for message %d", i)
		}
	}
}

// TestNackRedelivery verifies that Nacking a message causes it to be redelivered.
func TestNackRedelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanup := startKafka(t)
	defer cleanup()

	topic := "test.nack.redelivery"
	consumerGroup := "test-group-nack"

	sub, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
		Brokers:             brokers,
		ConsumerGroup:       consumerGroup,
		Version:             version,
		NackResendSleep:     10 * time.Millisecond, // fast resend for tests
		ReconnectRetrySleep: 200 * time.Millisecond,
		InitializeTopicDetails: &sarama.TopicDetail{
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	})
	require.NoError(t, err)
	defer sub.Close()

	// require.NoError(t, sub.SubscribeInitialize(topic))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, topic)
	require.NoError(t, err)

	pub, err := kafka.NewPublisher(&kafka.PublisherConfig{
		BrokerHosts: brokers,
		Version:     version,
		MaxRetry:    3,
	})
	require.NoError(t, err)
	defer pub.Close()

	original := message.NewMessage("nack-uuid", []byte("nack-payload"))
	require.NoError(t, pub.Publish(topic, original))

	// First delivery — Nack it.
	select {
	case got, ok := <-msgCh:
		require.True(t, ok)
		assert.Equal(t, original.UUID, got.UUID)
		got.Nack()
	case <-ctx.Done():
		t.Fatal("timed out waiting for first delivery")
	}

	// Second delivery — Ack it.
	select {
	case got, ok := <-msgCh:
		require.True(t, ok)
		assert.Equal(t, original.UUID, got.UUID)
		got.Ack()
	case <-ctx.Done():
		t.Fatal("timed out waiting for redelivery after Nack")
	}
}

// TestPublisherClose verifies that a closed publisher returns an error on Publish.
func TestPublisherClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanup := startKafka(t)
	defer cleanup()

	pub, err := kafka.NewPublisher(&kafka.PublisherConfig{
		BrokerHosts: brokers,
		Version:     version,
	})
	require.NoError(t, err)

	require.NoError(t, pub.Close())

	// Publishing to a closed producer should return an error.
	msg := message.NewMessage("closed-uuid", []byte("payload"))
	err = pub.Publish("some-topic", msg)
	assert.Error(t, err, "expected error publishing to closed producer")
}

// TestSubscriberCloseStopsChannel verifies that Close causes the message channel to be closed.
func TestSubscriberCloseStopsChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanup := startKafka(t)
	defer cleanup()

	topic := "test.subscriber.close"
	consumerGroup := "test-group-close"

	sub, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
		Brokers:             brokers,
		ConsumerGroup:       consumerGroup,
		Version:             version,
		NackResendSleep:     50 * time.Millisecond,
		ReconnectRetrySleep: 200 * time.Millisecond,
		InitializeTopicDetails: &sarama.TopicDetail{
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	})
	require.NoError(t, err)

	require.NoError(t, sub.SubscribeInitialize(topic))

	ctx := context.Background()
	msgCh, err := sub.Subscribe(ctx, topic)
	require.NoError(t, err)

	// Close subscriber — channel should be closed eventually.
	require.NoError(t, sub.Close())

	select {
	case _, ok := <-msgCh:
		assert.False(t, ok, "expected channel to be closed after subscriber Close")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for message channel to close")
	}
}

// TestSubscriberRequiresConsumerGroup validates that NewSubscriber returns an error
// when no consumer group is provided.
func TestSubscriberRequiresConsumerGroup(t *testing.T) {
	_, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
		Brokers: []string{"localhost:9092"},
	})
	require.ErrorIs(t, err, kafka.ErrConsumerGroupEmpty)
}

// TestMessageMetadataRoundtrip verifies that all metadata key-value pairs survive
// the Marshal/Unmarshal round-trip through Kafka.
func TestMessageMetadataRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanup := startKafka(t)
	defer cleanup()

	topic := "test.metadata.roundtrip"
	consumerGroup := "test-group-meta"

	sub, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
		Brokers:             brokers,
		ConsumerGroup:       consumerGroup,
		Version:             version,
		NackResendSleep:     50 * time.Millisecond,
		ReconnectRetrySleep: 200 * time.Millisecond,
		InitializeTopicDetails: &sarama.TopicDetail{
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	})
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, sub.SubscribeInitialize(topic))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, topic)
	require.NoError(t, err)

	pub, err := kafka.NewPublisher(&kafka.PublisherConfig{
		BrokerHosts: brokers,
		Version:     version,
	})
	require.NoError(t, err)
	defer pub.Close()

	want := message.NewMessage("meta-uuid", []byte("meta-payload"))
	want.Metadata.Set("key1", "value1")
	want.Metadata.Set("key2", "value2")
	want.Metadata.Set("eventType", "TaskCreated")

	require.NoError(t, pub.Publish(topic, want))

	select {
	case got, ok := <-msgCh:
		require.True(t, ok)
		assert.Equal(t, "value1", got.Metadata.Get("key1"))
		assert.Equal(t, "value2", got.Metadata.Get("key2"))
		assert.Equal(t, "TaskCreated", got.Metadata.Get("eventType"))
		got.Ack()
	case <-ctx.Done():
		t.Fatal("timed out waiting for message")
	}
}

// TestContextValuesInjected verifies that Kafka partition, offset, and timestamp
// are injected into the message context by the subscriber.
func TestContextValuesInjected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanup := startKafka(t)
	defer cleanup()

	topic := "test.context.values"
	consumerGroup := "test-group-ctx"

	sub, err := kafka.NewSubscriber(&kafka.SubscriberConfig{
		Brokers:             brokers,
		ConsumerGroup:       consumerGroup,
		Version:             version,
		NackResendSleep:     50 * time.Millisecond,
		ReconnectRetrySleep: 200 * time.Millisecond,
		InitializeTopicDetails: &sarama.TopicDetail{
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	})
	require.NoError(t, err)
	defer sub.Close()

	require.NoError(t, sub.SubscribeInitialize(topic))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msgCh, err := sub.Subscribe(ctx, topic)
	require.NoError(t, err)

	pub, err := kafka.NewPublisher(&kafka.PublisherConfig{
		BrokerHosts: brokers,
		Version:     version,
	})
	require.NoError(t, err)
	defer pub.Close()

	require.NoError(t, pub.Publish(topic, message.NewMessage("ctx-uuid", []byte("ctx-payload"))))

	select {
	case got, ok := <-msgCh:
		require.True(t, ok)
		msgCtx := got.Context()

		_, partOk := kafka.MessagePartitionFromCtx(msgCtx)
		assert.True(t, partOk, "expected partition in context")

		_, offOk := kafka.MessagePartitionOffsetFromCtx(msgCtx)
		assert.True(t, offOk, "expected offset in context")

		ts, tsOk := kafka.MessageTimestampFromCtx(msgCtx)
		assert.True(t, tsOk, "expected timestamp in context")
		assert.False(t, ts.IsZero(), "expected non-zero timestamp")

		got.Ack()
	case <-ctx.Done():
		t.Fatal("timed out waiting for message")
	}
}
