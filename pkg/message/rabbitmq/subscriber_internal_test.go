package rabbitmq

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/pkg/message"
)

const testPrefetch = 3

type errorLog struct {
	mu   sync.Mutex
	errs []error
}

func (l *errorLog) handle(_ context.Context, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, err)
}

func (l *errorLog) contains(target error) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, err := range l.errs {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func newTestSubscriber(t *testing.T, broker *fakeBroker, opts ...SubscriberOption) *Subscriber {
	t.Helper()

	cfg := &Config{URL: testURL, Prefetch: testPrefetch, ReconnectDelay: time.Millisecond}
	opts = append([]SubscriberOption{func(s *Subscriber) { s.conn.dial = broker.dial }}, opts...)

	sub, err := NewSubscriber(cfg, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}

func subscribe(t *testing.T, sub *Subscriber) <-chan *message.Message {
	t.Helper()
	messages, err := sub.Subscribe(t.Context(), testTopic)
	require.NoError(t, err)
	return messages
}

func receive(t *testing.T, messages <-chan *message.Message) *message.Message {
	t.Helper()
	select {
	case msg, ok := <-messages:
		require.True(t, ok, "subscription closed unexpectedly")
		return msg
	case <-time.After(waitFor):
		t.Fatal("timed out waiting for a message")
		return nil
	}
}

func requireClosed(t *testing.T, messages <-chan *message.Message) {
	t.Helper()
	select {
	case _, ok := <-messages:
		require.False(t, ok, "expected the subscription to be closed")
	case <-time.After(waitFor):
		t.Fatal("timed out waiting for the subscription to close")
	}
}

func testDelivery(tag uint64, uuid string) amqp.Delivery {
	return amqp.Delivery{
		DeliveryTag: tag,
		MessageId:   uuid,
		Headers:     amqp.Table{"eventType": "TaskCreated"},
		Body:        []byte("payload-" + uuid),
	}
}

func TestSubscribe_DeclaresTopologyAndConsumesWithManualAckUnderPrefetch(t *testing.T) {
	broker := &fakeBroker{}
	sub := newTestSubscriber(t, broker)

	subscribe(t, sub)

	ch := broker.channel(0)
	assert.Len(t, ch.exchanges, 2)
	assert.Len(t, ch.recordedQueues(), 2)
	assert.Contains(t, ch.recordedBindings(), workQueueBinding())
	assert.Equal(t, []qos{{count: testPrefetch, size: 0, global: false}}, ch.qos)
	assert.Equal(t, []consumer{{queue: "task.dispatch", autoAck: false}}, ch.recordedConsumers())
}

func TestSubscribe_DeliversTheMessage(t *testing.T) {
	broker := &fakeBroker{}
	messages := subscribe(t, newTestSubscriber(t, broker))

	broker.channel(0).deliver(testDelivery(1, "uuid-1"))
	msg := receive(t, messages)

	assert.Equal(t, "uuid-1", msg.UUID)
	assert.Equal(t, message.Payload("payload-uuid-1"), msg.Payload)
	assert.Equal(t, "TaskCreated", msg.Metadata.Get("eventType"))

	redelivered, ok := MessageRedeliveredFromCtx(msg.Context())
	assert.True(t, ok)
	assert.False(t, redelivered)
}

func TestSubscribe_FlagsRedeliveries(t *testing.T) {
	broker := &fakeBroker{}
	messages := subscribe(t, newTestSubscriber(t, broker))

	redelivery := testDelivery(1, "uuid-1")
	redelivery.Redelivered = true
	broker.channel(0).deliver(redelivery)

	redelivered, ok := MessageRedeliveredFromCtx(receive(t, messages).Context())
	assert.True(t, ok)
	assert.True(t, redelivered)
}

func TestSubscribe_AcksTheDeliveryOnlyWhenTheMessageIsAcked(t *testing.T) {
	broker := &fakeBroker{}
	messages := subscribe(t, newTestSubscriber(t, broker))
	ch := broker.channel(0)

	ch.deliver(testDelivery(7, "uuid-1"))
	msg := receive(t, messages)

	assert.Never(t, func() bool { return len(ch.recordedSettlements()) > 0 }, quiet, tick)

	msg.Ack()

	assert.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]settlement{{tag: 7, ack: true}}, ch.recordedSettlements())
	}, waitFor, tick)
}

func TestSubscribe_NackRequeuesTheDelivery(t *testing.T) {
	broker := &fakeBroker{}
	messages := subscribe(t, newTestSubscriber(t, broker))
	ch := broker.channel(0)

	ch.deliver(testDelivery(7, "uuid-1"))
	receive(t, messages).Nack()

	assert.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]settlement{{tag: 7, ack: false, requeue: true}}, ch.recordedSettlements())
	}, waitFor, tick)
}

func TestSubscribe_CancelsTheMessageContextOnceSettled(t *testing.T) {
	broker := &fakeBroker{}
	messages := subscribe(t, newTestSubscriber(t, broker))

	broker.channel(0).deliver(testDelivery(1, "uuid-1"))
	msg := receive(t, messages)
	require.NoError(t, msg.Context().Err(), "the context must stay live while the message is being worked on")

	msg.Ack()

	select {
	case <-msg.Context().Done():
	case <-time.After(waitFor):
		t.Fatal("the message context is still live after the message was settled")
	}
}

func TestSubscribe_DeliversTheNextMessageBeforeThePreviousIsSettled(t *testing.T) {
	broker := &fakeBroker{}
	messages := subscribe(t, newTestSubscriber(t, broker))
	ch := broker.channel(0)

	for tag := uint64(1); tag <= testPrefetch; tag++ {
		ch.deliver(testDelivery(tag, "uuid"))
	}

	for range testPrefetch {
		receive(t, messages)
	}
	assert.Empty(t, ch.recordedSettlements())
}

func TestSubscribe_ResubscribesAndRedeclaresAfterChannelLoss(t *testing.T) {
	broker := &fakeBroker{}
	failures := &errorLog{}
	sub := newTestSubscriber(t, broker, WithErrorHandler(failures.handle))
	messages := subscribe(t, sub)

	broker.channel(0).deliver(testDelivery(1, "uuid-1"))
	lost := receive(t, messages)

	timeout := &amqp.Error{Code: amqp.PreconditionFailed, Reason: "delivery acknowledgement on channel 1 timed out"}
	broker.channel(0).kill(timeout)

	conn := broker.connection(0)
	require.Eventually(t, func() bool { return conn.channelCount() == 2 }, waitFor, tick)
	assert.Equal(t, 1, broker.dials(), "a lost channel must not cost the connection")
	assert.Eventually(t, func() bool { return failures.contains(timeout) }, waitFor, tick,
		"the close reason must reach the error handler")

	replacement := conn.channelAt(1)
	require.Eventually(t, func() bool { return len(replacement.recordedConsumers()) == 1 }, waitFor, tick)
	redelivery := testDelivery(1, "uuid-1")
	redelivery.Redelivered = true
	replacement.deliver(redelivery)
	receive(t, messages).Ack()

	require.Eventually(t, func() bool { return len(replacement.recordedSettlements()) == 1 }, waitFor, tick)
	assert.Len(t, replacement.recordedQueues(), 2, "topology must be redeclared on resubscribe")
	assert.Equal(t, []qos{{count: testPrefetch}}, replacement.qos)

	lost.Ack()
	require.NoError(t, sub.Close())
	assert.Equal(t, []settlement{{tag: 1, ack: true}}, replacement.recordedSettlements())
}

func TestSubscribe_KeepsResubscribingUntilTheBrokerIsBack(t *testing.T) {
	refused := errors.New("connection refused")
	broker := &fakeBroker{}
	failures := &errorLog{}
	messages := subscribe(t, newTestSubscriber(t, broker, WithErrorHandler(failures.handle)))

	broker.refuse(refused)
	broker.connection(0).kill(brokerShutdown())
	require.Eventually(t, func() bool { return failures.contains(refused) }, waitFor, tick,
		"a failed resubscribe must reach the error handler with its cause")

	broker.accept()
	require.Eventually(t, func() bool {
		return broker.dials() == 2 && broker.connection(1).channelCount() == 1 &&
			len(broker.channel(1).recordedConsumers()) == 1
	}, waitFor, tick)

	broker.channel(1).deliver(testDelivery(1, "uuid-1"))
	assert.Equal(t, "uuid-1", receive(t, messages).UUID)
	assert.Len(t, broker.channel(1).recordedQueues(), 2, "topology must be redeclared on the new connection")
}

func TestSubscribe_ContextCancelClosesTheOutputAndTheChannel(t *testing.T) {
	broker := &fakeBroker{}
	sub := newTestSubscriber(t, broker)
	ctx, cancel := context.WithCancel(t.Context())
	messages, err := sub.Subscribe(ctx, testTopic)
	require.NoError(t, err)

	cancel()

	requireClosed(t, messages)
	assert.Eventually(t, broker.channel(0).isClosed, waitFor, tick)
	assert.False(t, broker.connection(0).isClosed(), "the connection outlives one subscription")
	assert.Never(t, func() bool { return broker.connection(0).channelCount() > 1 }, quiet, tick,
		"a cancelled subscription must not resubscribe")
}

func TestAwaitSettle_NackDuringShutdownLeavesTheDeliveryForRequeue(t *testing.T) {
	ch := newFakeChannel()
	session := &subscription{subscriber: newTestSubscriber(t, &fakeBroker{}), lost: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// select picks at random among ready cases, hence the repeats.
	for range 200 {
		msg := message.NewMessage("uuid-1", nil)
		msg.Nack()
		delivery := testDelivery(1, "uuid-1")
		delivery.Acknowledger = ch

		session.awaitSettle(ctx, delivery, msg)
	}

	assert.Empty(t, ch.recordedSettlements())
}

func TestSubscribe_UnmarshalFailureDeadLettersTheDelivery(t *testing.T) {
	malformed := errors.New("malformed delivery")
	broker := &fakeBroker{}
	failures := &errorLog{}
	sub := newTestSubscriber(t, broker,
		WithUnmarshaler(failingUnmarshaler{err: malformed}),
		WithErrorHandler(failures.handle),
	)
	messages := subscribe(t, sub)
	ch := broker.channel(0)

	ch.deliver(testDelivery(9, "uuid-1"))

	assert.Eventually(t, func() bool {
		return assert.ObjectsAreEqual([]settlement{{tag: 9, ack: false, requeue: false}}, ch.recordedSettlements())
	}, waitFor, tick)
	assert.True(t, failures.contains(malformed))
	select {
	case msg := <-messages:
		t.Fatalf("a delivery that cannot be unmarshaled must not reach the consumer, got %+v", msg)
	default:
	}
}

type failingUnmarshaler struct{ err error }

func (u failingUnmarshaler) Unmarshal(amqp.Delivery) (*message.Message, error) {
	return nil, u.err
}

func TestNewSubscriber_FailsWhenBrokerIsUnreachable(t *testing.T) {
	refused := errors.New("connection refused")
	broker := &fakeBroker{refusal: refused}

	_, err := NewSubscriber(&Config{URL: testURL}, func(s *Subscriber) { s.conn.dial = broker.dial })

	assert.ErrorIs(t, err, refused)
}

func TestSubscribe_FailsWhileTheBrokerIsUnreachable(t *testing.T) {
	refused := errors.New("connection refused")
	broker := &fakeBroker{}
	sub := newTestSubscriber(t, broker)

	broker.refuse(refused)
	broker.connection(0).kill(brokerShutdown())

	require.Eventually(t, func() bool {
		_, err := sub.Subscribe(t.Context(), testTopic)
		return errors.Is(err, errNotConnected) && errors.Is(err, refused)
	}, waitFor, tick)
}

func TestSubscribe_RejectsASecondTopic(t *testing.T) {
	broker := &fakeBroker{}
	sub := newTestSubscriber(t, broker)
	subscribe(t, sub)

	_, err := sub.Subscribe(t.Context(), "task.paused")

	require.ErrorIs(t, err, ErrTopicMismatch)
	assert.Equal(t, 1, broker.connection(0).channelCount())
}

func TestSubscriber_CloseClosesSubscriptionsAndRejectsSubscribe(t *testing.T) {
	broker := &fakeBroker{}
	sub := newTestSubscriber(t, broker)
	messages := subscribe(t, sub)

	require.NoError(t, sub.Close())
	require.NoError(t, sub.Close(), "Close must be idempotent")

	requireClosed(t, messages)
	assert.True(t, broker.connection(0).isClosed())
	assert.False(t, broker.connection(0).wasAborted())
	_, err := sub.Subscribe(t.Context(), testTopic)
	assert.ErrorIs(t, err, ErrSubscriberClosed)
}

func TestSubscriber_CloseGivesUpOnASilentBroker(t *testing.T) {
	broker := &fakeBroker{silent: make(chan struct{})}
	t.Cleanup(func() { close(broker.silent) })
	sub := newTestSubscriber(t, broker)
	sub.conn.closeTimeout = quiet
	messages := subscribe(t, sub)
	broker.refuse(errors.New("connection refused"))

	closed := make(chan error, 1)
	go func() { closed <- sub.Close() }()

	select {
	case <-closed:
	case <-time.After(waitFor):
		t.Fatal("Close is stuck giving a channel back to a silent broker")
	}
	requireClosed(t, messages)
	assert.True(t, broker.connection(0).wasAborted())
}

func TestSubscribeInitialize_DeclaresTopologyAndGivesTheChannelBack(t *testing.T) {
	broker := &fakeBroker{}
	sub := newTestSubscriber(t, broker)

	require.NoError(t, sub.SubscribeInitialize(testTopic))

	ch := broker.channel(0)
	assert.Len(t, ch.recordedQueues(), 2)
	assert.Contains(t, ch.recordedBindings(), workQueueBinding())
	assert.Empty(t, ch.recordedConsumers())
	assert.True(t, ch.isClosed())
	assert.False(t, broker.connection(0).isClosed())
}

func TestNewSubscriber_RejectsInvalidConfig(t *testing.T) {
	_, err := NewSubscriber(&Config{})

	assert.ErrorIs(t, err, ErrURLEmpty)
}

func TestSubscriberOptions(t *testing.T) {
	logger := log.NewSyncLogger(log.NewLogfmtLogger(&strings.Builder{}))
	unmarshaler := failingUnmarshaler{}

	sub := newTestSubscriber(t, &fakeBroker{}, WithLog(logger), WithLog(nil), WithUnmarshaler(unmarshaler))

	assert.Equal(t, loggerAdapter{logger}, sub.conn.logger, "a nil logger must not replace the configured one")
	assert.Equal(t, unmarshaler, sub.unmarshaler)
}

var (
	_ message.Publisher            = (*Publisher)(nil)
	_ message.Subscriber           = (*Subscriber)(nil)
	_ message.SubscribeInitializer = (*Subscriber)(nil)
)
