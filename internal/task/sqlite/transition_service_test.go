package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	task "github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/pkg/message/inmem"
)

// transitionFixture adds the user-facing task service to attemptFixture, on
// the same database and clock.
type transitionFixture struct {
	*attemptFixture
	tasks task.Service
}

func newTransitionFixture(t *testing.T) *transitionFixture {
	t.Helper()

	f := newAttemptFixture(t)
	// Pause and cancel publish a notification; nobody subscribes here.
	msgPub, _ := inmem.NewPublisherAndSubscriber(log.NewNopLogger())
	return &transitionFixture{
		attemptFixture: f,
		tasks:          task.NewService(f.repo, *task.NewEventPublisher(msgPub), f.tx, f.outbox, f.attempts),
	}
}

// requireFencedOut asserts that every attempt-originated call of attempt is
// rejected.
func (f *transitionFixture) requireFencedOut(t *testing.T, taskID uint64, attempt *events.Attempt) {
	t.Helper()

	ctx := context.Background()
	worker, generation := attempt.WorkerID, attempt.Generation
	_, renewErr := f.svc.RenewLease(ctx, taskID, worker, generation, events.LeaseUpdate{})
	for name, err := range map[string]error{
		"renew":    renewErr,
		"complete": f.svc.CompleteAttempt(ctx, taskID, worker, generation, attemptResult(taskID, generation)),
		"fail":     f.svc.FailAttempt(ctx, taskID, worker, generation, errors.New("late failure")),
		"release":  f.svc.ReleaseAttempt(ctx, taskID, worker, generation),
	} {
		require.ErrorIs(t, err, events.ErrAttemptRejected, name)
	}
}

func TestCancelTask_ActiveAttemptIsFencedOut(t *testing.T) {
	f := newTransitionFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")

	require.NoError(t, f.tasks.CancelTask(ctx, id))

	st := f.state(t, id)
	require.Equal(t, task.StatusCancelled, st.Status)
	require.Empty(t, st.OwnerID)
	require.Nil(t, st.LeaseExpiresAt)
	f.requireFencedOut(t, id, attempt)
	_, err := f.svc.ClaimTask(ctx, id, "worker-b")
	require.ErrorIs(t, err, events.ErrTaskNotClaimable)
	require.Equal(t, task.StatusCancelled, f.state(t, id).Status)
	require.Empty(t, f.unsent(t))
}

func TestCancelTask_QueuedRetryStaysCancelled(t *testing.T) {
	f := newTransitionFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)
	require.NoError(t, f.svc.FailAttempt(ctx, id, "worker-a", attempt.Generation, errors.New("connection reset")))
	require.NotNil(t, f.state(t, id).NextAttemptAt, "the task is PENDING and waiting for its retry time")

	require.NoError(t, f.tasks.CancelTask(ctx, id))

	st := f.state(t, id)
	require.Equal(t, task.StatusCancelled, st.Status)
	require.Nil(t, st.NextAttemptAt)
	f.clock.Advance(task.DefaultRetryBackoff(1))
	_, err = f.svc.ClaimTask(ctx, id, "worker-b")
	require.ErrorIs(t, err, events.ErrTaskNotClaimable)
}

func TestCancelTask_RejectsCompletedTask(t *testing.T) {
	f := newTransitionFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, attemptResult(id, attempt.Generation)))
	committed := f.state(t, id)

	err := f.tasks.CancelTask(ctx, id)

	require.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
	require.Equal(t, committed, f.state(t, id))
}

func TestPauseTask_RetiresAttempt(t *testing.T) {
	f := newTransitionFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)

	require.NoError(t, f.tasks.PauseTask(ctx, id))

	st := f.state(t, id)
	require.Equal(t, task.StatusPaused, st.Status)
	require.Empty(t, st.OwnerID)
	require.Nil(t, st.LeaseExpiresAt)
	require.Equal(t, uint64(1), st.Generation)
	f.requireFencedOut(t, id, attempt)
	// A paused task is held by the user: nobody may claim it.
	_, err = f.svc.ClaimTask(ctx, id, "worker-b")
	require.ErrorIs(t, err, events.ErrTaskNotClaimable)
}

func TestPauseTask_OnlyDownloadingTasks(t *testing.T) {
	f := newTransitionFixture(t)
	id := f.newTask(t)
	ctx := context.Background()

	err := f.tasks.PauseTask(ctx, id) // still PENDING
	require.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
	require.Equal(t, task.StatusPending, f.state(t, id).Status)

	f.claimStoring(t, id, "worker-a")
	err = f.tasks.PauseTask(ctx, id) // STORING
	require.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
	st := f.state(t, id)
	require.Equal(t, task.StatusStoring, st.Status)
	require.Equal(t, "worker-a", st.OwnerID, "a rejected pause must not touch the attempt")
}

func TestResumeTask_RequeuesAsNewAttemptFromZero(t *testing.T) {
	f := newTransitionFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	first, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)
	progress := events.AttemptProgress{Progress: 40, DownloadedBytes: 400, TotalBytes: 1000}
	_, err = f.svc.RenewLease(ctx, id, "worker-a", first.Generation, events.LeaseUpdate{Progress: &progress})
	require.NoError(t, err)
	require.NoError(t, f.tasks.PauseTask(ctx, id))

	require.NoError(t, f.tasks.ResumeTask(ctx, id))

	st := f.state(t, id)
	require.Equal(t, task.StatusPending, st.Status)
	require.Empty(t, st.OwnerID)
	rows := f.unsent(t)
	require.Len(t, rows, 1, "resume re-dispatches through the outbox")
	require.Equal(t, id, rows[0].AggregateID)
	require.Equal(t, task.OutboxEventTaskCreated, rows[0].EventType)
	require.Equal(t, task.OutboxDestinationDispatch, rows[0].Destination)

	second, err := f.svc.ClaimTask(ctx, id, "worker-b")
	require.NoError(t, err)
	require.Equal(t, uint64(2), second.Generation)
	require.Equal(t, task.DownloadProgress{}, f.state(t, id).Progress, "the resumed download starts from zero")
	f.requireFencedOut(t, id, first)
}

func TestRetryTask_ResetsRetryBudget(t *testing.T) {
	f := newTransitionFixture(t)
	ctx := context.Background()
	noRetries := pendingTask(7)
	noRetries.DownloadOptions.MaxRetries = 0
	created, err := f.repo.Create(ctx, noRetries)
	require.NoError(t, err)
	id := created.ID

	// With max_retries 0 the first failure is terminal.
	attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)
	require.NoError(t, f.svc.FailAttempt(ctx, id, "worker-a", attempt.Generation, errors.New("boom")))
	failed := f.state(t, id)
	require.Equal(t, task.StatusFailed, failed.Status)
	require.Equal(t, 1, failed.RetryCount)

	require.NoError(t, f.tasks.RetryTask(ctx, id))

	st := f.state(t, id)
	require.Equal(t, task.StatusPending, st.Status)
	require.Zero(t, st.RetryCount, "a user retry grants a fresh retry budget")
	require.Nil(t, st.NextAttemptAt)
	rows := f.unsent(t)
	require.Len(t, rows, 2)
	require.Equal(t, task.OutboxEventTaskFailed, rows[0].EventType, "the terminal failure emits a lifecycle event")
	require.Equal(t, id, rows[1].AggregateID)
	require.Equal(t, task.OutboxEventTaskCreated, rows[1].EventType)

	next, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)
	require.Equal(t, uint64(2), next.Generation)
}
