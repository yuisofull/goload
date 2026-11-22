package task

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutboxRelay_RunOnce_RecordsPublishOutcome(t *testing.T) {
	fm := &captureTaskMetrics{}
	outbox := seededOutbox(t, outboxEventFixture("evt-1", 1), outboxEventFixture("evt-2", 2))
	transport := &recordingPublisher{failFirst: map[string]int{"evt-1": 1}}
	relay := NewOutboxRelay(outbox, NewEventPublisher(transport), WithOutboxRelayMetrics(fm))

	_, _ = relay.RunOnce(context.Background())

	require.Equal(t, 1, fm.relayFail, "evt-1 publish failed once")
	require.Equal(t, 1, fm.relayOK, "evt-2 published")
}

func TestReconciler_RunOnce_RecordsRedispatches(t *testing.T) {
	fm := &captureTaskMetrics{}
	f := newReconcilerFixture([]uint64{1, 2}, WithReconcilerMetrics(fm))
	f.candidates.set(
		ReconcileCandidate{TaskID: 1, Generation: 0},
		ReconcileCandidate{TaskID: 2, Generation: 4},
	)

	n, err := f.reconciler.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, 2, fm.redispatched)
}
