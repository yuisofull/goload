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

const testTopic = "task.created"

func workQueueBinding() binding {
	return binding{queue: "task.dispatch", key: testTopic, exchange: "goload.dispatch"}
}

func newTestPublisher(t *testing.T, broker *fakeBroker, options ...PublisherOption) *Publisher {
	t.Helper()

	cfg := &Config{URL: testURL, ConfirmTimeout: 50 * time.Millisecond, ReconnectDelay: time.Millisecond}
	options = append([]PublisherOption{func(p *Publisher) { p.conn.dial = broker.dial }}, options...)

	pub, err := NewPublisher(cfg, options...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pub.Close() })
	return pub
}

func newTestMessage(uuid string) *message.Message {
	msg := message.NewMessage(uuid, []byte("payload-"+uuid))
	msg.Metadata.Set("eventType", "TaskCreated")
	return msg
}

func awaitReconnect(t *testing.T, conn *connectionWrapper, broker *fakeBroker, dials int) {
	t.Helper()
	require.Eventually(t, func() bool { return broker.dials() == dials }, waitFor, tick)
	requireConnected(t, conn, broker.connection(dials-1))
}

func TestNewPublisher_FailsWhenBrokerIsUnreachable(t *testing.T) {
	refused := errors.New("connection refused")
	broker := &fakeBroker{refusal: refused}

	_, err := NewPublisher(&Config{URL: testURL}, func(p *Publisher) { p.conn.dial = broker.dial })

	assert.ErrorIs(t, err, refused)
}

func TestNewPublisher_RejectsInvalidConfigBeforeDialing(t *testing.T) {
	broker := &fakeBroker{}

	_, err := NewPublisher(&Config{}, func(p *Publisher) { p.conn.dial = broker.dial })

	require.ErrorIs(t, err, ErrURLEmpty)
	assert.Zero(t, broker.dials())
}

func TestPublish_DeclaresTopologyAndConfirmModeBeforePublishing(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))

	ch := broker.channel(0)
	assert.True(t, ch.confirming, "channel must be in confirm mode")
	assert.Len(t, ch.exchanges, 2)
	assert.Len(t, ch.recordedQueues(), 2)
	assert.Contains(t, ch.recordedBindings(), workQueueBinding())
}

func TestPublish_SendsMandatoryPersistentMessageToTheExchange(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))

	publishes := broker.channel(0).recordedPublishes()
	require.Len(t, publishes, 1)
	assert.Equal(t, published{
		exchange:  "goload.dispatch",
		key:       testTopic,
		mandatory: true,
		publishing: amqp.Publishing{
			MessageId:    "uuid-1",
			Headers:      amqp.Table{"eventType": "TaskCreated"},
			DeliveryMode: amqp.Persistent,
			Body:         []byte("payload-uuid-1"),
		},
	}, publishes[0])
}

func TestPublish_DeclaresTopologyOncePerChannel(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1"), newTestMessage("uuid-2")))
	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-3")))

	assert.Equal(t, 1, broker.dials())
	assert.Equal(t, 1, broker.connection(0).channelCount())
	assert.Len(t, broker.channel(0).recordedQueues(), 2)
	assert.Len(t, broker.channel(0).recordedPublishes(), 3)
}

func TestPublish_FailsWhenBrokerNacks(t *testing.T) {
	broker := &fakeBroker{onPublish: (*fakeChannel).nack}
	pub := newTestPublisher(t, broker)

	err := pub.Publish(testTopic, newTestMessage("uuid-1"))

	assert.ErrorIs(t, err, ErrNacked)
}

func TestPublish_FailsWhenMessageIsUnroutable(t *testing.T) {
	broker := &fakeBroker{onPublish: (*fakeChannel).returnThenAck}
	pub := newTestPublisher(t, broker)

	err := pub.Publish(testTopic, newTestMessage("uuid-1"))

	require.ErrorIs(t, err, ErrUnroutable)
	assert.Contains(t, err.Error(), "NO_ROUTE")
}

func TestPublish_RedeclaresTopologyAfterTheBrokerRefusesAMessage(t *testing.T) {
	refusals := map[string]struct {
		onPublish reaction
		want      error
	}{
		"nacked":     {(*fakeChannel).nack, ErrNacked},
		"unroutable": {(*fakeChannel).returnThenAck, ErrUnroutable},
	}
	for name, refusal := range refusals {
		t.Run(name, func(t *testing.T) {
			broker := &fakeBroker{onPublish: refusal.onPublish}
			pub := newTestPublisher(t, broker)
			require.ErrorIs(t, pub.Publish(testTopic, newTestMessage("uuid-1")), refusal.want)

			broker.react(nil)
			require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-2")))

			assert.Equal(t, 1, broker.dials(), "a broker that answers keeps its connection")
			assert.Equal(t, 1, broker.connection(0).channelCount())
			assert.Len(t, broker.channel(0).recordedQueues(), 4, "topology must be redeclared after a refusal")
			assert.Len(t, broker.channel(0).recordedPublishes(), 2)
		})
	}
}

func TestPublish_FailsWhenConfirmDoesNotArrive(t *testing.T) {
	broker := &fakeBroker{onPublish: (*fakeChannel).ignore}
	pub := newTestPublisher(t, broker)

	err := pub.Publish(testTopic, newTestMessage("uuid-1"))

	require.ErrorIs(t, err, ErrNoConfirm)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, broker.connection(0).wasAborted())
}

func TestPublish_FailureDoesNotWaitForTheClosingHandshake(t *testing.T) {
	broker := &fakeBroker{silent: make(chan struct{}), onPublish: (*fakeChannel).ignore}
	pub := newTestPublisher(t, broker)
	t.Cleanup(func() { close(broker.silent) })
	broker.refuse(errors.New("connection refused"))

	failed := make(chan error, 1)
	go func() { failed <- pub.Publish(testTopic, newTestMessage("uuid-1")) }()

	select {
	case err := <-failed:
		require.ErrorIs(t, err, ErrNoConfirm)
	case <-time.After(waitFor):
		t.Fatal("Publish is stuck in the closing handshake")
	}
	assert.True(t, broker.connection(0).wasAborted())

	closed := make(chan error, 1)
	go func() { closed <- pub.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(waitFor):
		t.Fatal("Close is blocked after a failed publish")
	}
}

func TestPublisher_CloseSaysGoodbyeToAHealthyBroker(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	require.NoError(t, pub.Close())

	assert.True(t, broker.connection(0).isClosed())
	assert.False(t, broker.connection(0).wasAborted())
}

func TestPublish_FailsWithTheCloseReasonWhenChannelDiesBeforeConfirm(t *testing.T) {
	notFound := &amqp.Error{Code: amqp.NotFound, Reason: "NOT_FOUND - no exchange 'goload.dispatch'"}
	broker := &fakeBroker{onPublish: func(dying *fakeChannel, _ uint64) { dying.kill(notFound) }}
	pub := newTestPublisher(t, broker)

	err := pub.Publish(testTopic, newTestMessage("uuid-1"))

	require.ErrorIs(t, err, ErrNoConfirm)
	assert.Contains(t, err.Error(), "NOT_FOUND")
}

func TestPublish_FailsWhenMessageContextIsCancelled(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	msg := newTestMessage("uuid-1")
	msg.SetContext(ctx)

	err := pub.Publish(testTopic, msg)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestPublish_ReopensTheChannelTheBrokerClosedWhileIdle(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)
	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))

	broker.channel(0).kill(&amqp.Error{Code: amqp.PreconditionFailed, Reason: "channel exception"})

	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-2")))
	assert.Equal(t, 1, broker.dials(), "a lost channel must not cost the connection")
	require.Equal(t, 2, broker.connection(0).channelCount())
	reopened := broker.connection(0).channelAt(1)
	assert.True(t, reopened.confirming)
	assert.Len(t, reopened.recordedQueues(), 2, "topology must be redeclared on the new channel")
	assert.Len(t, reopened.recordedPublishes(), 1)
}

func TestPublish_ReconnectsAfterTheConnectionDrops(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)
	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))

	broker.connection(0).kill(brokerShutdown())
	awaitReconnect(t, pub.conn, broker, 2)

	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-2")))
	assert.Len(t, broker.channel(1).recordedQueues(), 2, "topology must be redeclared on the new connection")
	assert.Len(t, broker.channel(1).recordedPublishes(), 1)
}

func TestPublish_FailsFastWhileTheBrokerIsUnreachable(t *testing.T) {
	refused := errors.New("connection refused")
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)
	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))

	broker.refuse(refused)
	broker.connection(0).kill(brokerShutdown())

	require.Eventually(t, func() bool {
		return errors.Is(pub.Publish(testTopic, newTestMessage("uuid-2")), refused)
	}, waitFor, tick)
	require.ErrorIs(t, pub.Publish(testTopic, newTestMessage("uuid-2")), errNotConnected)
	assert.Len(t, broker.channel(0).recordedPublishes(), 1)
}

func TestPublish_LateConfirmIsNotCreditedToTheNextMessage(t *testing.T) {
	broker := &fakeBroker{onPublish: (*fakeChannel).ignore}
	pub := newTestPublisher(t, broker)
	require.ErrorIs(t, pub.Publish(testTopic, newTestMessage("uuid-1")), ErrNoConfirm)

	require.True(t, broker.channel(0).isClosed(), "the channel that lost a confirm must be abandoned")

	broker.react((*fakeChannel).nack)
	awaitReconnect(t, pub.conn, broker, 2)
	err := pub.Publish(testTopic, newTestMessage("uuid-2"))

	require.ErrorIs(t, err, ErrNacked)
	assert.Len(t, broker.channel(1).recordedPublishes(), 1)
}

func TestPublish_StopsAtTheFirstFailure(t *testing.T) {
	nackSecond := func(ch *fakeChannel, tag uint64) {
		if tag == 2 {
			ch.nack(tag)
			return
		}
		ch.ack(tag)
	}
	broker := &fakeBroker{onPublish: nackSecond}
	pub := newTestPublisher(t, broker)

	err := pub.Publish(testTopic, newTestMessage("uuid-1"), newTestMessage("uuid-2"), newTestMessage("uuid-3"))

	require.ErrorIs(t, err, ErrNacked)
	assert.Contains(t, err.Error(), "uuid-2")
	assert.Len(t, broker.channel(0).recordedPublishes(), 2)
}

func TestPublish_RejectsASecondTopic(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)
	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))

	err := pub.Publish("task.paused", newTestMessage("uuid-2"))

	require.ErrorIs(t, err, ErrTopicMismatch)
	assert.Len(t, broker.channel(0).recordedPublishes(), 1)
}

func TestPublish_RejectsAnEmptyTopic(t *testing.T) {
	pub := newTestPublisher(t, &fakeBroker{})

	assert.ErrorIs(t, pub.Publish("", newTestMessage("uuid-1")), ErrTopicEmpty)
}

func TestPublish_MarshalFailureKeepsTheChannel(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	err := pub.Publish(testTopic, newTestMessage(strings.Repeat("a", maxShortStringBytes+1)))
	require.ErrorIs(t, err, ErrShortStringTooLong)

	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))
	assert.Equal(t, 1, broker.dials())
	assert.Equal(t, 1, broker.connection(0).channelCount())
}

func TestPublisher_CloseClosesTheConnectionAndRejectsPublish(t *testing.T) {
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	require.NoError(t, pub.Close())
	require.NoError(t, pub.Close(), "Close must be idempotent")

	assert.True(t, broker.connection(0).isClosed())
	assert.ErrorIs(t, pub.Publish(testTopic, newTestMessage("uuid-1")), ErrPublisherClosed)
}

func TestPublish_IsSafeForConcurrentUse(t *testing.T) {
	const publishers, perPublisher = 8, 25
	broker := &fakeBroker{}
	pub := newTestPublisher(t, broker)

	var wg sync.WaitGroup
	errs := make(chan error, publishers*perPublisher)
	for range publishers {
		wg.Go(func() {
			for range perPublisher {
				errs <- pub.Publish(testTopic, newTestMessage("uuid"))
			}
		})
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, broker.dials())
	assert.Len(t, broker.channel(0).recordedPublishes(), publishers*perPublisher)
}

type stubMarshaler struct{ publishing amqp.Publishing }

func (m stubMarshaler) Marshal(*message.Message) (amqp.Publishing, error) {
	return m.publishing, nil
}

func TestPublisherOptions(t *testing.T) {
	broker := &fakeBroker{}
	logger := log.NewSyncLogger(log.NewLogfmtLogger(&strings.Builder{}))
	custom := stubMarshaler{publishing: amqp.Publishing{MessageId: "from-custom-marshaler"}}

	pub := newTestPublisher(t, broker, WithLogger(logger), WithMarshaler(custom))
	require.NoError(t, pub.Publish(testTopic, newTestMessage("uuid-1")))

	assert.Equal(t, loggerAdapter{logger}, pub.conn.logger)
	assert.Equal(t, "from-custom-marshaler", broker.channel(0).recordedPublishes()[0].publishing.MessageId)
}
