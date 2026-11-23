package task

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type captureTaskMetrics struct {
	dispatchWaits                            []time.Duration
	completed, failed, retried, redispatched int
	relayOK, relayFail                       int
}

func (m *captureTaskMetrics) RecordDispatchWait(d time.Duration) {
	m.dispatchWaits = append(m.dispatchWaits, d)
}
func (m *captureTaskMetrics) IncCompleted() { m.completed++ }
func (m *captureTaskMetrics) IncFailed()    { m.failed++ }
func (m *captureTaskMetrics) IncRetried()   { m.retried++ }
func (m *captureTaskMetrics) RecordRelayPublish(ok bool) {
	if ok {
		m.relayOK++
	} else {
		m.relayFail++
	}
}
func (m *captureTaskMetrics) IncRedispatched() { m.redispatched++ }

func TestClaimTask_RecordsDispatchWaitOnceOnFirstClaim(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	nowAt := created.Add(10 * time.Second)
	clock := func() time.Time { return nowAt }

	attempts := newMemAttempts(AttemptState{TaskID: 7, Status: StatusPending, Generation: 0, CreatedAt: created})
	fm := &captureTaskMetrics{}
	svc := NewAttemptService(attempts, fakeTxManager{}, &memOutbox{},
		WithAttemptClock(clock), WithLeaseDuration(time.Minute), WithMetrics(fm))

	if _, err := svc.ClaimTask(context.Background(), 7, "worker-a"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	require.Equal(t, []time.Duration{10 * time.Second}, fm.dispatchWaits)

	// Re-claim after the lease expires: generation 2, not a dispatch wait.
	nowAt = nowAt.Add(2 * time.Minute)
	if _, err := svc.ClaimTask(context.Background(), 7, "worker-b"); err != nil {
		t.Fatalf("second claim: %v", err)
	}
	require.Len(t, fm.dispatchWaits, 1, "re-claim must not record dispatch wait again")
}

func TestClaimTask_SkipsNegativeDispatchWait(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	nowAt := created.Add(-time.Hour) // clock/timezone skew: the service clock is behind created_at
	clock := func() time.Time { return nowAt }

	attempts := newMemAttempts(AttemptState{TaskID: 8, Status: StatusPending, Generation: 0, CreatedAt: created})
	fm := &captureTaskMetrics{}
	svc := NewAttemptService(attempts, fakeTxManager{}, &memOutbox{},
		WithAttemptClock(clock), WithLeaseDuration(time.Minute), WithMetrics(fm))

	if _, err := svc.ClaimTask(context.Background(), 8, "w"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	require.Empty(t, fm.dispatchWaits, "a negative (skewed) dispatch wait must not be recorded")
}
