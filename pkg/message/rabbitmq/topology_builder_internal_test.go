package rabbitmq

import (
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultConfig(t *testing.T) Config {
	t.Helper()
	cfg, err := resolveConfig(&Config{URL: testURL})
	require.NoError(t, err)
	return cfg
}

func TestBuildTopology_DeclaresWorkQueueAndDeadLetterParkingLot(t *testing.T) {
	ch := newFakeChannel()

	require.NoError(t, buildTopology(ch, defaultConfig(t), "task.created"))

	assert.Equal(t, []declaredExchange{
		{name: "goload.dispatch.dlx", kind: "fanout", durable: true},
		{name: "goload.dispatch", kind: "direct", durable: true},
	}, ch.exchanges)
	assert.Equal(t, []declaredQueue{
		{name: "task.dispatch.dlq", durable: true, args: amqp.Table{"x-queue-type": "quorum"}},
		{name: "task.dispatch", durable: true, args: amqp.Table{
			"x-queue-type":           "quorum",
			"x-dead-letter-exchange": "goload.dispatch.dlx",
			"x-delivery-limit":       5,
			"x-dead-letter-strategy": "at-least-once",
			"x-overflow":             "reject-publish",
		}},
	}, ch.queues)
	assert.Equal(t, []binding{
		{queue: "task.dispatch.dlq", key: "", exchange: "goload.dispatch.dlx"},
		{queue: "task.dispatch", key: "task.created", exchange: "goload.dispatch"},
	}, ch.bindings)
}

func TestBuildTopology_NamesTheEntityThatFailed(t *testing.T) {
	inequivalent := errors.New("PRECONDITION_FAILED - inequivalent arg 'x-queue-type'")
	ch := newFakeChannel()
	ch.failQueue = "task.dispatch"
	ch.declareErr = inequivalent

	err := buildTopology(ch, defaultConfig(t), "task.created")

	require.ErrorIs(t, err, inequivalent)
	assert.Contains(t, err.Error(), "task.dispatch")
}
