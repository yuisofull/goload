package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
	task "github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/pkg/message/inmem"
)

// fakeClock is the task service clock in tests. Advance expires leases and
// passes retry backoffs without sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// attemptFixture is an AttemptService on a throwaway SQLite database with a
// controllable clock.
type attemptFixture struct {
	clock     *fakeClock
	repo      task.Repository
	attempts  task.AttemptRepository
	reconcile task.ReconcileRepository
	outbox    task.OutboxRepository
	tx        task.TxManager
	svc       *task.AttemptService
}

func newAttemptFixture(t *testing.T) *attemptFixture {
	t.Helper()

	pool := newTestPool(t)
	f := &attemptFixture{
		clock:     &fakeClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		repo:      NewTaskRepo(pool),
		attempts:  NewAttemptRepo(pool),
		reconcile: NewReconcileRepo(pool),
		outbox:    NewOutboxRepo(pool),
		tx:        NewTxManager(pool),
	}
	f.svc = task.NewAttemptService(f.attempts, f.tx, f.outbox, task.WithAttemptClock(f.clock.Now))
	return f
}

// newTask stores a PENDING task with max_retries 3 and returns its id.
func (f *attemptFixture) newTask(t *testing.T) uint64 {
	t.Helper()

	created, err := f.repo.Create(context.Background(), pendingTask(7))
	require.NoError(t, err)
	return created.ID
}

func (f *attemptFixture) state(t *testing.T, taskID uint64) *task.AttemptState {
	t.Helper()

	st, err := f.attempts.GetAttemptState(context.Background(), taskID)
	require.NoError(t, err)
	return st
}

func (f *attemptFixture) unsent(t *testing.T) []task.OutboxEvent {
	t.Helper()

	rows, err := f.outbox.FetchUnsent(context.Background(), task.OutboxCursor{}, 100)
	require.NoError(t, err)
	return rows
}

// claimStoring claims the task for workerID and moves the attempt to STORING.
func (f *attemptFixture) claimStoring(t *testing.T, taskID uint64, workerID string) *events.Attempt {
	t.Helper()

	ctx := context.Background()
	attempt, err := f.svc.ClaimTask(ctx, taskID, workerID)
	require.NoError(t, err)
	_, err = f.svc.RenewLease(ctx, taskID, workerID, attempt.Generation, events.LeaseUpdate{Storing: true})
	require.NoError(t, err)
	return attempt
}

// takeOver stages the fencing scenario: worker-a claims generation 1 and
// reports progress, then stops renewing (alive but blocked) until its lease
// has expired; worker-b claims generation 2 and moves to STORING.
func (f *attemptFixture) takeOver(t *testing.T, taskID uint64) (stale, winner *events.Attempt) {
	t.Helper()

	ctx := context.Background()
	stale, err := f.svc.ClaimTask(ctx, taskID, "worker-a")
	require.NoError(t, err)
	progress := events.AttemptProgress{Progress: 40, DownloadedBytes: 400, TotalBytes: 1000}
	_, err = f.svc.RenewLease(ctx, taskID, "worker-a", stale.Generation, events.LeaseUpdate{Progress: &progress})
	require.NoError(t, err)

	f.clock.Advance(task.DefaultLeaseDuration)
	winner = f.claimStoring(t, taskID, "worker-b")
	require.Equal(t, uint64(2), winner.Generation)
	return stale, winner
}

// attemptResult is the result an attempt reports: its own generation-scoped
// storage key and a checksum that differs per generation.
func attemptResult(taskID, generation uint64) events.AttemptResult {
	return events.AttemptResult{
		FileName:    "resolved.bin",
		FileSize:    1000,
		ContentType: "application/octet-stream",
		Checksum:    &events.ChecksumInfo{ChecksumType: "md5", ChecksumValue: fmt.Sprintf("sum-%d", generation)},
		StorageType: storage.TypeLocal,
		StorageKey:  fmt.Sprintf("%d/%d/resolved-0011223344556677.bin", taskID, generation),
	}
}

func TestClaimTask_PendingTaskBecomesDownloading(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)

	attempt, err := f.svc.ClaimTask(context.Background(), id, "worker-a")
	require.NoError(t, err)

	wantLease := f.clock.Now().Add(task.DefaultLeaseDuration)
	require.Equal(t, id, attempt.TaskID)
	require.Equal(t, "worker-a", attempt.WorkerID)
	require.Equal(t, uint64(1), attempt.Generation)
	require.True(t, wantLease.Equal(attempt.LeaseExpiresAt))

	st := f.state(t, id)
	require.Equal(t, task.StatusDownloading, st.Status)
	require.Equal(t, "worker-a", st.OwnerID)
	require.Equal(t, uint64(1), st.Generation)
	require.NotNil(t, st.LeaseExpiresAt)
	require.True(t, wantLease.Equal(*st.LeaseExpiresAt))
	require.Empty(t, st.StoragePath, "claiming must not attach a storage key")
}

func TestClaimTask_LiveLeaseIsNotClaimable(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	_, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)

	// One nanosecond before the lease runs out it is still worker-a's task.
	f.clock.Advance(task.DefaultLeaseDuration - time.Nanosecond)
	_, err = f.svc.ClaimTask(ctx, id, "worker-b")

	require.ErrorIs(t, err, events.ErrTaskNotClaimable)
	require.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidState))
	st := f.state(t, id)
	require.Equal(t, "worker-a", st.OwnerID)
	require.Equal(t, uint64(1), st.Generation)
}

// T2 at the API level: the owner stops renewing (crash during download), the
// lease expires, the next claim gets a new generation and starts from zero.
func TestClaimTask_ExpiredLeaseIsReclaimedFromZero(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	first, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)
	progress := events.AttemptProgress{Progress: 40, DownloadedBytes: 400, TotalBytes: 1000}
	_, err = f.svc.RenewLease(ctx, id, "worker-a", first.Generation, events.LeaseUpdate{Progress: &progress})
	require.NoError(t, err)

	f.clock.Advance(task.DefaultLeaseDuration)
	second, err := f.svc.ClaimTask(ctx, id, "worker-b")
	require.NoError(t, err)

	require.Equal(t, uint64(2), second.Generation)
	st := f.state(t, id)
	require.Equal(t, task.StatusDownloading, st.Status)
	require.Equal(t, "worker-b", st.OwnerID)
	require.Equal(t, task.DownloadProgress{}, st.Progress, "the new attempt downloads from zero")
}

func TestAttemptService_UnknownTaskIsNotFound(t *testing.T) {
	f := newAttemptFixture(t)

	_, err := f.svc.ClaimTask(context.Background(), 999, "worker-a")

	require.True(t, apperrors.IsError(err, apperrors.ErrCodeNotFound))
}

func TestAttemptService_RejectsEmptyWorkerID(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()

	_, claimErr := f.svc.ClaimTask(ctx, id, "")
	_, renewErr := f.svc.RenewLease(ctx, id, "", 1, events.LeaseUpdate{})
	for name, err := range map[string]error{
		"claim":    claimErr,
		"renew":    renewErr,
		"complete": f.svc.CompleteAttempt(ctx, id, "", 1, attemptResult(id, 1)),
		"fail":     f.svc.FailAttempt(ctx, id, "", 1, errors.New("boom")),
		"release":  f.svc.ReleaseAttempt(ctx, id, "", 1),
	} {
		require.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput), name)
	}
	require.Equal(t, task.StatusPending, f.state(t, id).Status)
}

func TestRenewLease_ExtendsLeaseAndRecordsProgressAndStoring(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)

	f.clock.Advance(20 * time.Second)
	progress := events.AttemptProgress{Progress: 25, DownloadedBytes: 250, TotalBytes: 1000}
	lease, err := f.svc.RenewLease(ctx, id, "worker-a", attempt.Generation,
		events.LeaseUpdate{Progress: &progress, Storing: true})
	require.NoError(t, err)

	wantLease := f.clock.Now().Add(task.DefaultLeaseDuration)
	require.True(t, wantLease.Equal(lease))
	st := f.state(t, id)
	require.Equal(t, task.StatusStoring, st.Status)
	require.Equal(t, task.DownloadProgress(progress), st.Progress)
	require.NotNil(t, st.LeaseExpiresAt)
	require.True(t, wantLease.Equal(*st.LeaseExpiresAt))
	require.Equal(t, uint64(1), st.Generation)
}

// Happy path: claim -> storing -> complete commits the result and the
// lifecycle event together, and the relay publishes the event.
func TestCompleteAttempt_CommitsResultAndLifecycleEventTogether(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)

	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))

	st := f.state(t, id)
	require.Equal(t, task.StatusCompleted, st.Status)
	require.Empty(t, st.OwnerID)
	require.Nil(t, st.LeaseExpiresAt)
	require.Equal(t, result.StorageKey, st.StoragePath)
	require.Equal(t, storage.TypeLocal, st.StorageType)
	require.Equal(t, "resolved.bin", st.FileName)
	require.Equal(t, &task.ChecksumInfo{ChecksumType: result.Checksum.ChecksumType, ChecksumValue: result.Checksum.ChecksumValue}, st.Checksum)
	require.Equal(t, task.DownloadProgress{Progress: 100, DownloadedBytes: 1000, TotalBytes: 1000}, st.Progress)
	require.NotNil(t, st.CompletedAt)
	require.True(t, f.clock.Now().Equal(*st.CompletedAt))

	// The existing read path sees the same committed row.
	got, err := f.repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, task.StatusCompleted, got.Status)
	require.Equal(t, result.StorageKey, got.StoragePath)
	require.Equal(t, "resolved.bin", got.FileName)
	require.NotNil(t, got.CompletedAt)

	// Exactly one lifecycle event waits in the outbox.
	rows := f.unsent(t)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].AggregateID)
	require.Equal(t, task.OutboxDestinationLifecycle, rows[0].Destination)
	require.Equal(t, task.OutboxEventTaskCompleted, rows[0].EventType)
	var payload events.TaskCompletedEvent
	require.NoError(t, json.Unmarshal(rows[0].Payload, &payload))
	require.Equal(t, id, payload.TaskID)
	require.Equal(t, result.StorageKey, payload.StorageKey)
	require.Equal(t, "local", payload.StorageType)
	require.Equal(t, int64(1000), payload.FileSize)

	// The relay publishes it on the task.completed topic.
	msgPub, msgSub := inmem.NewPublisherAndSubscriber(log.NewNopLogger())
	ch, err := msgSub.Subscribe(t.Context(), "task.completed")
	require.NoError(t, err)
	sent, err := task.NewOutboxRelay(f.outbox, task.NewEventPublisher(msgPub)).RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, sent)
	select {
	case msg := <-ch:
		require.NotNil(t, msg)
		require.Equal(t, rows[0].ID, msg.UUID)
		require.Equal(t, "TaskCompleted", msg.Metadata.Get("eventType"))
		msg.Ack()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for task.completed message")
	}
	require.Empty(t, f.unsent(t))
}

func TestCompleteAttempt_OutboxFailureRollsBackCompletion(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)
	boom := errors.New("outbox insert failed")
	broken := task.NewAttemptService(
		f.attempts, f.tx, failAfterInsertOutbox{OutboxRepository: f.outbox, err: boom},
		task.WithAttemptClock(f.clock.Now),
	)

	err := broken.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result)

	require.ErrorIs(t, err, boom)
	require.True(t, apperrors.IsError(err, apperrors.ErrCodeInternal))
	st := f.state(t, id)
	require.Equal(t, task.StatusStoring, st.Status, "the completion must roll back with the outbox row")
	require.Equal(t, "worker-a", st.OwnerID)
	require.Empty(t, st.StoragePath)
	require.Empty(t, f.unsent(t))

	// The attempt still owns the task and completes once the outbox works.
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))
	require.Len(t, f.unsent(t), 1)
}

func TestCompleteAttempt_RequiresStoring(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)

	err = f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, attemptResult(id, attempt.Generation))

	require.ErrorIs(t, err, events.ErrAttemptRejected)
	require.Equal(t, task.StatusDownloading, f.state(t, id).Status)
	require.Empty(t, f.unsent(t))
}

func TestCompleteAttempt_RequiresStorageKey(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)
	result.StorageKey = ""

	err := f.svc.CompleteAttempt(context.Background(), id, "worker-a", attempt.Generation, result)

	require.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
	require.Equal(t, task.StatusStoring, f.state(t, id).Status)
}

func TestCompleteAttempt_RequiresStorageType(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)
	result.StorageType = ""

	err := f.svc.CompleteAttempt(context.Background(), id, "worker-a", attempt.Generation, result)

	require.True(t, apperrors.IsError(err, apperrors.ErrCodeInvalidInput))
	require.Equal(t, task.StatusStoring, f.state(t, id).Status)
}

// T3 at the API level: worker-a is alive but blocked from renewing; worker-b
// claims a new generation and completes; then worker-a wakes up.
func TestFencing_StaleWorkerCannotTouchWinnersResult(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	stale, winner := f.takeOver(t, id)
	winnerResult := attemptResult(id, winner.Generation)
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-b", winner.Generation, winnerResult))
	committed := f.state(t, id)

	_, renewErr := f.svc.RenewLease(ctx, id, "worker-a", stale.Generation, events.LeaseUpdate{})
	for name, err := range map[string]error{
		"renew":    renewErr,
		"complete": f.svc.CompleteAttempt(ctx, id, "worker-a", stale.Generation, attemptResult(id, stale.Generation)),
		"fail":     f.svc.FailAttempt(ctx, id, "worker-a", stale.Generation, errors.New("stale failure")),
		"release":  f.svc.ReleaseAttempt(ctx, id, "worker-a", stale.Generation),
	} {
		require.ErrorIs(t, err, events.ErrAttemptRejected, name)
		require.True(t, apperrors.IsError(err, apperrors.ErrCodeConflict), name)
	}
	// It cannot claim the completed task again either.
	_, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.ErrorIs(t, err, events.ErrTaskNotClaimable)

	require.Equal(t, committed, f.state(t, id), "worker-b's result must be untouched")
	require.Equal(t, winnerResult.StorageKey, committed.StoragePath)
	require.Equal(t, &task.ChecksumInfo{ChecksumType: winnerResult.Checksum.ChecksumType, ChecksumValue: winnerResult.Checksum.ChecksumValue}, committed.Checksum)
	rows := f.unsent(t)
	require.Len(t, rows, 1, "exactly one completion event: worker-b's")
	var payload events.TaskCompletedEvent
	require.NoError(t, json.Unmarshal(rows[0].Payload, &payload))
	require.Equal(t, winnerResult.StorageKey, payload.StorageKey)
}

// T3b at the API level: worker-a's stale failure and stale progress arrive
// after worker-b completed.
func TestFencing_StaleFailureAndProgressAreRejectedAfterCompletion(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	stale, winner := f.takeOver(t, id)
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-b", winner.Generation, attemptResult(id, winner.Generation)))
	committed := f.state(t, id)

	failErr := f.svc.FailAttempt(ctx, id, "worker-a", stale.Generation, errors.New("stale failure"))
	regress := events.AttemptProgress{Progress: 10, DownloadedBytes: 100, TotalBytes: 1000}
	_, progressErr := f.svc.RenewLease(ctx, id, "worker-a", stale.Generation, events.LeaseUpdate{Progress: &regress})

	require.ErrorIs(t, failErr, events.ErrAttemptRejected)
	require.ErrorIs(t, progressErr, events.ErrAttemptRejected)
	after := f.state(t, id)
	require.Equal(t, committed, after)
	require.Equal(t, task.StatusCompleted, after.Status, "no overwrite to FAILED")
	require.Nil(t, after.ErrorMessage)
	require.Zero(t, after.RetryCount)
	require.InDelta(t, 100, after.Progress.Progress, 0, "no progress regression")
}

func TestFencing_StaleProgressCannotMoveLiveAttempt(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	stale, winner := f.takeOver(t, id)
	live := events.AttemptProgress{Progress: 10, DownloadedBytes: 100, TotalBytes: 1000}
	_, err := f.svc.RenewLease(ctx, id, "worker-b", winner.Generation, events.LeaseUpdate{Progress: &live})
	require.NoError(t, err)

	ahead := events.AttemptProgress{Progress: 90, DownloadedBytes: 900, TotalBytes: 1000}
	_, progressErr := f.svc.RenewLease(ctx, id, "worker-a", stale.Generation, events.LeaseUpdate{Progress: &ahead})
	failErr := f.svc.FailAttempt(ctx, id, "worker-a", stale.Generation, errors.New("stale failure"))

	require.ErrorIs(t, progressErr, events.ErrAttemptRejected)
	require.ErrorIs(t, failErr, events.ErrAttemptRejected)
	st := f.state(t, id)
	require.Equal(t, task.DownloadProgress(live), st.Progress)
	require.Equal(t, "worker-b", st.OwnerID)
	require.Equal(t, task.StatusStoring, st.Status)
	require.Zero(t, st.RetryCount)
	require.Nil(t, st.ErrorMessage)
}

// T4 at the API level: CompleteAttempt commits, the response is lost, the
// same attempt retries.
func TestCompleteAttempt_LostResponseRetryIsIdempotent(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)

	// The first call commits, but the worker never sees the response.
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))
	committed := f.state(t, id)

	// It retries, even long after its lease would have expired.
	f.clock.Advance(time.Hour)
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))

	require.Equal(t, committed, f.state(t, id), "the retry must not change the committed result")
	require.Len(t, f.unsent(t), 1, "the retry must not write a second completion event")
}

func TestCompleteAttempt_RetryWithOtherGenerationOrResultIsRejected(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))
	committed := f.state(t, id)

	other := result
	other.StorageKey = fmt.Sprintf("%d/9/other-0011223344556677.bin", id)
	otherResultErr := f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, other)
	otherGenerationErr := f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation+1, result)

	require.ErrorIs(t, otherResultErr, events.ErrAttemptRejected)
	require.ErrorIs(t, otherGenerationErr, events.ErrAttemptRejected)
	require.Equal(t, committed, f.state(t, id))
	require.Len(t, f.unsent(t), 1)
}

func TestCompleteAttempt_PersistsResultIdentity(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)

	require.NoError(t, f.svc.CompleteAttempt(context.Background(), id, "worker-a", attempt.Generation, result))

	st := f.state(t, id)
	require.Equal(t, "application/octet-stream", st.ContentType)
	require.Equal(t, int64(1000), st.FileSize)
}

func TestCompleteAttempt_RetryWithOtherContentTypeOrSizeIsRejected(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))
	committed := f.state(t, id)

	otherType := result
	otherType.ContentType = "text/plain"
	otherSize := result
	otherSize.FileSize = result.FileSize + 1

	require.ErrorIs(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, otherType), events.ErrAttemptRejected)
	require.ErrorIs(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, otherSize), events.ErrAttemptRejected)
	require.Equal(t, committed, f.state(t, id), "a rejected retry must not change the result")
	require.Len(t, f.unsent(t), 1)

	// The faithful retry is still accepted without a second event.
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))
	require.Len(t, f.unsent(t), 1)
}

func TestCompleteAttempt_EmptyFileRetryIsIdempotent(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")
	result := attemptResult(id, attempt.Generation)
	result.FileSize = 0
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))
	committed := f.state(t, id)
	require.Equal(t, int64(0), committed.FileSize)

	f.clock.Advance(time.Hour)
	require.NoError(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, result))

	require.Equal(t, committed, f.state(t, id))
	require.Len(t, f.unsent(t), 1)

	nonEmpty := result
	nonEmpty.FileSize = 5
	require.ErrorIs(t, f.svc.CompleteAttempt(ctx, id, "worker-a", attempt.Generation, nonEmpty), events.ErrAttemptRejected)
}

func TestFailAttempt_RetriesWithBackoffThenTerminal(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t) // max_retries 3
	ctx := context.Background()

	for retry := 1; retry <= 3; retry++ {
		attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
		require.NoError(t, err)
		require.Equal(t, uint64(retry), attempt.Generation)
		require.NoError(t, f.svc.FailAttempt(ctx, id, "worker-a", attempt.Generation, errors.New("connection reset")))

		st := f.state(t, id)
		require.Equal(t, task.StatusPending, st.Status)
		require.Empty(t, st.OwnerID)
		require.Nil(t, st.LeaseExpiresAt)
		require.Equal(t, retry, st.RetryCount)
		require.NotNil(t, st.ErrorMessage)
		require.Equal(t, "connection reset", *st.ErrorMessage)
		require.Empty(t, st.StoragePath, "a failed attempt must not attach a storage key")
		wantNext := f.clock.Now().Add(task.DefaultRetryBackoff(retry))
		require.NotNil(t, st.NextAttemptAt)
		require.True(t, wantNext.Equal(*st.NextAttemptAt))

		// Not claimable until the backoff has fully passed.
		f.clock.Advance(task.DefaultRetryBackoff(retry) - time.Nanosecond)
		_, err = f.svc.ClaimTask(ctx, id, "worker-b")
		require.ErrorIs(t, err, events.ErrTaskNotClaimable)
		f.clock.Advance(time.Nanosecond)
	}

	// The fourth failure exhausts the budget of 3 retries.
	attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)
	require.NoError(t, f.svc.FailAttempt(ctx, id, "worker-a", attempt.Generation, errors.New("still broken")))

	st := f.state(t, id)
	require.Equal(t, task.StatusFailed, st.Status)
	require.Equal(t, 4, st.RetryCount)
	require.Nil(t, st.NextAttemptAt)
	require.Empty(t, st.OwnerID)
	require.NotNil(t, st.ErrorMessage)
	require.Equal(t, "still broken", *st.ErrorMessage)
	_, err = f.svc.ClaimTask(ctx, id, "worker-a")
	require.ErrorIs(t, err, events.ErrTaskNotClaimable)
	rows := f.unsent(t)
	require.Len(t, rows, 1, "only the terminal failure writes a lifecycle event (retries write none)")
	require.Equal(t, task.OutboxEventTaskFailed, rows[0].EventType)
}

func TestFailAttempt_NilCauseStoresUnknownError(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt, err := f.svc.ClaimTask(ctx, id, "worker-a")
	require.NoError(t, err)

	require.NoError(t, f.svc.FailAttempt(ctx, id, "worker-a", attempt.Generation, nil))

	st := f.state(t, id)
	require.NotNil(t, st.ErrorMessage)
	require.Equal(t, "unknown error", *st.ErrorMessage)
}

func TestReleaseAttempt_ReturnsTaskToPendingImmediatelyClaimable(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)
	ctx := context.Background()
	attempt := f.claimStoring(t, id, "worker-a")

	require.NoError(t, f.svc.ReleaseAttempt(ctx, id, "worker-a", attempt.Generation))

	st := f.state(t, id)
	require.Equal(t, task.StatusPending, st.Status)
	require.Empty(t, st.OwnerID)
	require.Nil(t, st.LeaseExpiresAt)
	require.Nil(t, st.NextAttemptAt)
	require.Zero(t, st.RetryCount, "a release is not a failure")
	require.Empty(t, st.StoragePath)

	next, err := f.svc.ClaimTask(ctx, id, "worker-b")
	require.NoError(t, err)
	require.Equal(t, uint64(2), next.Generation)

	// The released attempt is fenced out like any other stale attempt.
	err = f.svc.ReleaseAttempt(ctx, id, "worker-a", attempt.Generation)
	require.ErrorIs(t, err, events.ErrAttemptRejected)
}

func TestClaimTask_ConcurrentClaimsExactlyOneWins(t *testing.T) {
	f := newAttemptFixture(t)
	id := f.newTask(t)

	const workers = 8
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = f.svc.ClaimTask(context.Background(), id, fmt.Sprintf("worker-%d", i))
		}()
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		if err == nil {
			won++
			continue
		}
		require.ErrorIs(t, err, events.ErrTaskNotClaimable,
			"a losing claim must be a clean rejection, not a database error")
	}
	require.Equal(t, 1, won)
	require.Equal(t, uint64(1), f.state(t, id).Generation)
}
