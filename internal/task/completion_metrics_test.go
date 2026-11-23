package task

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
)

func completeTestService(fm Metrics, st AttemptState, now time.Time) *AttemptService {
	return NewAttemptService(newMemAttempts(st), fakeTxManager{}, &memOutbox{},
		WithAttemptClock(func() time.Time { return now }), WithMetrics(fm))
}

func TestCompleteAttempt_RecordsCompletedOnceDespiteLostResponseRetry(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	lease := now.Add(time.Minute)
	fm := &captureTaskMetrics{}
	svc := completeTestService(fm, AttemptState{TaskID: 9, Status: StatusStoring, OwnerID: "w", Generation: 1, LeaseExpiresAt: &lease}, now)

	result := events.AttemptResult{StorageKey: "9/1/file.bin", StorageType: storage.TypeLocal}
	require.NoError(t, svc.CompleteAttempt(context.Background(), 9, "w", 1, result))
	require.Equal(t, 1, fm.completed)

	// Lost response: the same completion is retried; it is idempotent and must not double count.
	require.NoError(t, svc.CompleteAttempt(context.Background(), 9, "w", 1, result))
	require.Equal(t, 1, fm.completed)
}

func TestFailAttempt_TerminalRecordsFailed(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	lease := now.Add(time.Minute)
	fm := &captureTaskMetrics{}
	svc := completeTestService(fm, AttemptState{TaskID: 9, Status: StatusDownloading, OwnerID: "w", Generation: 1, LeaseExpiresAt: &lease, MaxRetries: 0}, now)

	require.NoError(t, svc.FailAttempt(context.Background(), 9, "w", 1, stderrors.New("boom")))
	require.Equal(t, 1, fm.failed)
	require.Equal(t, 0, fm.retried)
}

func TestFailAttempt_NonTerminalRecordsRetried(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	lease := now.Add(time.Minute)
	fm := &captureTaskMetrics{}
	svc := completeTestService(fm, AttemptState{TaskID: 9, Status: StatusDownloading, OwnerID: "w", Generation: 1, LeaseExpiresAt: &lease, MaxRetries: 5}, now)

	require.NoError(t, svc.FailAttempt(context.Background(), 9, "w", 1, stderrors.New("boom")))
	require.Equal(t, 1, fm.retried)
	require.Equal(t, 0, fm.failed)
}
