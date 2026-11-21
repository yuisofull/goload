package rabbitmq

import (
	"errors"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	waitFor = 2 * time.Second
	tick    = 5 * time.Millisecond
	quiet   = 50 * time.Millisecond
)

func brokerShutdown() *amqp.Error {
	return &amqp.Error{Code: amqp.ConnectionForced, Reason: "broker shutdown"}
}

func newTestConnection(t *testing.T, broker *fakeBroker) *connectionWrapper {
	t.Helper()

	conn := newConnectionWrapper(Config{URL: testURL, ReconnectDelay: time.Millisecond}, broker.dial)
	require.NoError(t, conn.open())
	t.Cleanup(func() { _ = conn.close() })
	return conn
}

func requireConnected(t *testing.T, conn *connectionWrapper, want *fakeConnection) {
	t.Helper()
	require.Eventually(t, func() bool {
		got, err := conn.connection()
		return err == nil && got == amqpConnection(want)
	}, waitFor, tick)
}

func TestConnectionWrapper_OpenFailsWhenBrokerIsUnreachable(t *testing.T) {
	refused := errors.New("connection refused")
	broker := &fakeBroker{refusal: refused}

	err := newConnectionWrapper(Config{URL: testURL}, broker.dial).open()

	assert.ErrorIs(t, err, refused)
}

func TestConnectionWrapper_ReconnectsAfterTheConnectionDrops(t *testing.T) {
	broker := &fakeBroker{}
	conn := newTestConnection(t, broker)

	broker.connection(0).kill(brokerShutdown())

	require.Eventually(t, func() bool { return broker.dials() == 2 }, waitFor, tick)
	requireConnected(t, conn, broker.connection(1))
}

func TestConnectionWrapper_SaysWhyItIsDisconnected(t *testing.T) {
	refused := errors.New("connection refused")
	broker := &fakeBroker{}
	conn := newTestConnection(t, broker)

	broker.refuse(refused)
	broker.connection(0).kill(brokerShutdown())

	require.Eventually(t, func() bool {
		_, err := conn.connection()
		return errors.Is(err, errNotConnected) && errors.Is(err, refused)
	}, waitFor, tick)

	broker.accept()
	require.Eventually(t, func() bool { return broker.dials() == 2 }, waitFor, tick)
	requireConnected(t, conn, broker.connection(1))
}

func TestConnectionWrapper_AbortDropsTheConnectionWithoutAGoodbye(t *testing.T) {
	broker := &fakeBroker{silent: make(chan struct{})}
	t.Cleanup(func() { close(broker.silent) })
	conn := newTestConnection(t, broker)
	lost := errors.New("no broker confirm received")
	broker.refuse(errors.New("connection refused"))

	live, err := conn.connection()
	require.NoError(t, err)
	conn.abort(live, lost)

	assert.True(t, broker.connection(0).wasAborted())
	_, err = conn.connection()
	require.ErrorIs(t, err, errNotConnected)
	require.NoError(t, conn.close(), "an aborted connection has nobody to say goodbye to")
}

func TestConnectionWrapper_AbortOfAStaleConnectionKeepsTheLiveOne(t *testing.T) {
	broker := &fakeBroker{}
	conn := newTestConnection(t, broker)
	stale := broker.connection(0)
	stale.kill(brokerShutdown())
	require.Eventually(t, func() bool { return broker.dials() == 2 }, waitFor, tick)
	requireConnected(t, conn, broker.connection(1))

	conn.abort(stale, errors.New("late failure"))

	got, err := conn.connection()
	require.NoError(t, err)
	assert.Equal(t, amqpConnection(broker.connection(1)), got)
}

func TestConnectionWrapper_CloseSaysGoodbyeAndStopsReconnecting(t *testing.T) {
	broker := &fakeBroker{}
	conn := newTestConnection(t, broker)

	require.NoError(t, conn.close())
	require.NoError(t, conn.close(), "close must be idempotent")

	assert.True(t, broker.connection(0).isClosed())
	assert.False(t, broker.connection(0).wasAborted())
	assert.Never(t, func() bool { return broker.dials() > 1 }, quiet, tick)
}

func TestConnectionWrapper_CloseGivesUpOnASilentBroker(t *testing.T) {
	broker := &fakeBroker{silent: make(chan struct{})}
	t.Cleanup(func() { close(broker.silent) })
	conn := newTestConnection(t, broker)
	conn.closeTimeout = quiet

	closed := make(chan error, 1)
	go func() { closed <- conn.close() }()

	select {
	case <-closed:
	case <-time.After(waitFor):
		t.Fatal("close is stuck in the closing handshake")
	}
	assert.True(t, broker.connection(0).wasAborted())
}

func TestConnectionWrapper_CloseDuringARedialDropsTheLateConnection(t *testing.T) {
	broker := &fakeBroker{}
	conn := newTestConnection(t, broker)

	redialing := make(chan struct{})
	release := make(chan struct{})
	broker.holdDials(func() {
		close(redialing)
		<-release
	})
	broker.connection(0).kill(brokerShutdown())
	select {
	case <-redialing:
	case <-time.After(waitFor):
		t.Fatal("the dropped connection was not redialed")
	}

	require.NoError(t, conn.close())
	close(release)

	require.Eventually(t, func() bool { return broker.dials() == 2 }, waitFor, tick)
	assert.Eventually(t, broker.connection(1).isClosed, waitFor, tick,
		"a connection that arrives after close must not leak")
}
