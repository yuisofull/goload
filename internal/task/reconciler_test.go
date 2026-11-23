package task

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
)

// scriptedCandidates is a ReconcileRepository that serves a fixed list in id
// order and honors afterID and limit.
type scriptedCandidates struct {
	mu   sync.Mutex
	list []ReconcileCandidate
	err  error
}

func (c *scriptedCandidates) ListReconcilable(
	_ context.Context,
	_ time.Time,
	afterID uint64,
	limit int,
) ([]ReconcileCandidate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	var page []ReconcileCandidate
	for _, candidate := range c.list {
		if candidate.TaskID > afterID && len(page) < limit {
			page = append(page, candidate)
		}
	}
	return page, nil
}

func (c *scriptedCandidates) set(list ...ReconcileCandidate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = list
}

func (c *scriptedCandidates) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

// taskMapRepo is fakeRepo that serves the given task ids from GetByID.
type taskMapRepo struct {
	fakeRepo
	tasks map[uint64]*Task
}

func newTaskMapRepo(ids ...uint64) *taskMapRepo {
	r := &taskMapRepo{tasks: make(map[uint64]*Task, len(ids))}
	for _, id := range ids {
		t := sampleOutboxTask()
		t.ID = id
		r.tasks[id] = t
	}
	return r
}

func (r *taskMapRepo) GetByID(_ context.Context, id uint64) (*Task, error) {
	t, ok := r.tasks[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return t, nil
}

// reconcilerFixture is a Reconciler on fakes with a clock the test moves.
type reconcilerFixture struct {
	candidates *scriptedCandidates
	outbox     *memOutbox
	now        time.Time
	reconciler *Reconciler
}

func newReconcilerFixture(taskIDs []uint64, opts ...ReconcilerOption) *reconcilerFixture {
	f := &reconcilerFixture{
		candidates: &scriptedCandidates{},
		outbox:     &memOutbox{},
		now:        time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	opts = append([]ReconcilerOption{WithReconcilerClock(func() time.Time { return f.now })}, opts...)
	f.reconciler = NewReconciler(f.candidates, newTaskMapRepo(taskIDs...), f.outbox, opts...)
	return f
}

// sweep runs one sweep that must succeed and returns how many tasks it
// dispatched.
func (f *reconcilerFixture) sweep(t *testing.T) int {
	t.Helper()

	dispatched, err := f.reconciler.RunOnce(context.Background())
	require.NoError(t, err)
	return dispatched
}

// dispatched returns the task ids of the dispatch rows in the outbox, in the
// order they were written.
func (f *reconcilerFixture) dispatched(t *testing.T) []uint64 {
	t.Helper()

	ids := []uint64{}
	for _, row := range f.outbox.snapshot() {
		require.Equal(t, OutboxEventTaskCreated, row.EventType)
		require.Equal(t, OutboxDestinationDispatch, row.Destination)
		require.Nil(t, row.SentAt)
		ids = append(ids, row.AggregateID)
	}
	return ids
}

func TestNewReconciler_DefaultsAndInvalidOptions(t *testing.T) {
	require.Equal(t, 5*time.Second, DefaultReconcileInterval)
	require.Equal(t, 30*time.Second, DefaultRedispatchInterval)
	require.Equal(t, 100, DefaultReconcileBatchSize)
	require.Equal(t, 100, DefaultReconcileMaxPerPass)

	r := NewReconciler(&scriptedCandidates{}, newTaskMapRepo(), &memOutbox{},
		WithReconcileInterval(0),
		WithRedispatchInterval(-time.Second),
		WithReconcileBatchSize(0),
		WithReconcileMaxPerPass(-1),
		WithReconcilerClock(nil),
		WithReconcilerLogger(nil),
	)

	require.Equal(t, DefaultReconcileInterval, r.interval)
	require.Equal(t, DefaultRedispatchInterval, r.redispatchInterval)
	require.Equal(t, DefaultReconcileBatchSize, r.batchSize)
	require.Equal(t, DefaultReconcileMaxPerPass, r.maxPerPass)
	require.NotNil(t, r.now)
	require.NotNil(t, r.logger)
}

func TestReconciler_RunOnce_RedispatchesCandidatesThroughTheOutbox(t *testing.T) {
	f := newReconcilerFixture([]uint64{1, 2})
	f.candidates.set(
		ReconcileCandidate{TaskID: 1, Generation: 0}, // never claimed: its dispatch was lost
		ReconcileCandidate{TaskID: 2, Generation: 4}, // its lease expired or its retry is due
	)

	require.Equal(t, 2, f.sweep(t))

	require.Equal(t, []uint64{1, 2}, f.dispatched(t))
	// The row is the TaskCreated dispatch CreateTask writes: the payload the
	// download worker already consumes.
	var payload events.TaskCreatedEvent
	require.NoError(t, json.Unmarshal(f.outbox.snapshot()[1].Payload, &payload))
	require.Equal(t, uint64(2), payload.TaskID)
	require.Equal(t, "https://example.test/file.bin", payload.SourceURL)
	require.Equal(t, "HTTPS", payload.SourceType)
}

func TestReconciler_RunOnce_UnchangedCandidateIsRateLimited(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	f.candidates.set(ReconcileCandidate{TaskID: 1, Generation: 0})

	require.Equal(t, 1, f.sweep(t))
	// The task is still PENDING and unowned, for example because it waits for
	// a download slot. The same dispatch is not repeated on every sweep.
	require.Zero(t, f.sweep(t))
	f.now = f.now.Add(DefaultRedispatchInterval - time.Nanosecond)
	require.Zero(t, f.sweep(t))

	f.now = f.now.Add(time.Nanosecond)
	require.Equal(t, 1, f.sweep(t), "after the redispatch interval the dispatch is presumed lost")
	require.Equal(t, []uint64{1, 1}, f.dispatched(t))
}

func TestReconciler_RunOnce_NewGenerationIsRedispatchedAtOnce(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	f.candidates.set(ReconcileCandidate{TaskID: 1, Generation: 1})
	require.Equal(t, 1, f.sweep(t))

	// That dispatch was claimed (generation 2) and the attempt failed: the
	// retry is due. It must not wait for the redispatch interval.
	f.candidates.set(ReconcileCandidate{TaskID: 1, Generation: 2})
	f.now = f.now.Add(time.Second)

	require.Equal(t, 1, f.sweep(t))
	require.Equal(t, []uint64{1, 1}, f.dispatched(t))
}

func TestReconciler_RunOnce_CapsEachSweepAndCatchesUp(t *testing.T) {
	f := newReconcilerFixture([]uint64{1, 2, 3, 4, 5}, WithReconcileBatchSize(2), WithReconcileMaxPerPass(2))
	f.candidates.set(
		ReconcileCandidate{TaskID: 1},
		ReconcileCandidate{TaskID: 2},
		ReconcileCandidate{TaskID: 3},
		ReconcileCandidate{TaskID: 4},
		ReconcileCandidate{TaskID: 5},
	)

	// Every sweep sends at most two dispatches and continues where the last
	// one stopped, so the tail is not starved by the head.
	require.Equal(t, 2, f.sweep(t))
	require.Equal(t, 2, f.sweep(t))
	require.Equal(t, 1, f.sweep(t))
	require.Zero(t, f.sweep(t))
	require.Equal(t, []uint64{1, 2, 3, 4, 5}, f.dispatched(t))
}

func TestReconciler_RunOnce_ForgetsTasksThatLeftTheList(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	f.candidates.set(ReconcileCandidate{TaskID: 1, Generation: 0})
	require.Equal(t, 1, f.sweep(t))

	// The task was claimed and completed: it is no candidate any more.
	f.candidates.set()
	require.Zero(t, f.sweep(t))

	require.Empty(t, f.reconciler.last, "the dispatch memory must not grow with every task ever seen")
}

func TestReconciler_RunOnce_DeletedTaskIsSkipped(t *testing.T) {
	f := newReconcilerFixture(nil)
	// The task was deleted between the listing and the dispatch.
	f.candidates.set(ReconcileCandidate{TaskID: 404})

	require.Zero(t, f.sweep(t))
	require.Empty(t, f.dispatched(t))
}

func TestReconciler_RunOnce_FailedDispatchIsRetriedOnTheNextSweep(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	f.candidates.set(ReconcileCandidate{TaskID: 1})
	outboxDown := errors.New("outbox insert failed")
	f.outbox.insertErr = outboxDown

	dispatched, err := f.reconciler.RunOnce(context.Background())

	require.ErrorIs(t, err, outboxDown)
	require.Zero(t, dispatched)

	// The failed dispatch left no mark, so the next sweep tries again at once.
	f.outbox.insertErr = nil
	require.Equal(t, 1, f.sweep(t))
}

func TestReconciler_RunOnce_ListFailureKeepsTheDispatchMemory(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	f.candidates.set(ReconcileCandidate{TaskID: 1})
	require.Equal(t, 1, f.sweep(t))

	listDown := errors.New("database is locked")
	f.candidates.fail(listDown)
	dispatched, err := f.reconciler.RunOnce(context.Background())
	require.ErrorIs(t, err, listDown)
	require.Zero(t, dispatched)

	// An aborted sweep must not forget what was dispatched a moment ago.
	f.candidates.fail(nil)
	require.Zero(t, f.sweep(t))
	require.Equal(t, []uint64{1}, f.dispatched(t))
}

func TestReconciler_RunOnce_StopsWhenCancelled(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	f.candidates.set(ReconcileCandidate{TaskID: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dispatched, err := f.reconciler.RunOnce(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, dispatched)
	require.Empty(t, f.dispatched(t))
}

func TestReconciler_Run_SweepsAtStartAndThenPeriodically(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		candidates := &scriptedCandidates{}
		candidates.set(ReconcileCandidate{TaskID: 1, Generation: 0})
		outbox := &memOutbox{}
		r := NewReconciler(candidates, newTaskMapRepo(1), outbox)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		stopped := make(chan error, 1)
		go func() { stopped <- r.Run(ctx) }()

		synctest.Wait()
		require.Len(t, outbox.snapshot(), 1, "the first sweep runs at startup: tasks stranded by a crash")

		// The dispatch was claimed and the attempt failed: the retry is due.
		candidates.set(ReconcileCandidate{TaskID: 1, Generation: 1})
		time.Sleep(DefaultReconcileInterval)
		synctest.Wait()
		require.Len(t, outbox.snapshot(), 2, "the next sweep runs one interval later")

		cancel()
		require.ErrorIs(t, <-stopped, context.Canceled)
	})
}

func TestReconciler_RunOnce_PersistentBacklogDoesNotStarveTheTail(t *testing.T) {
	const total = 700
	ids := make([]uint64, 0, total)
	list := make([]ReconcileCandidate, 0, total)
	for id := uint64(1); id <= total; id++ {
		ids = append(ids, id)
		list = append(list, ReconcileCandidate{TaskID: id})
	}
	f := newReconcilerFixture(ids, WithReconcileBatchSize(100), WithReconcileMaxPerPass(100))
	// Every task stays eligible forever, and the head becomes re-eligible
	// after the redispatch interval, so a sweep that restarted at the head
	// would never get past the first hundred.
	f.candidates.set(list...)

	seen := make(map[uint64]bool, total)
	for sweep := 0; sweep < 40 && len(seen) < total; sweep++ {
		f.sweep(t)
		f.now = f.now.Add(DefaultReconcileInterval)
		for _, id := range f.dispatched(t) {
			seen[id] = true
		}
	}

	for id := uint64(1); id <= total; id++ {
		require.True(t, seen[id], "task %d was starved", id)
	}
}

// A redispatch joins the trace of the task's last dispatch, so a duplicate or a
// retry stays in the download's trace.
func TestReconciler_RunOnce_RedispatchKeepsTheTaskTrace(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	carrier := map[string]string{"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}
	f.outbox.rows = []OutboxEvent{{ID: "first", AggregateID: 1, Destination: OutboxDestinationDispatch, EventType: OutboxEventTaskCreated, TraceContext: carrier}}
	f.candidates.set(ReconcileCandidate{TaskID: 1})

	require.Equal(t, 1, f.sweep(t))

	rows := f.outbox.snapshot()
	require.Len(t, rows, 2)
	require.Equal(t, carrier, rows[1].TraceContext)
}

func TestReconciler_RunOnce_TraceLookupFailureStillRedispatches(t *testing.T) {
	f := newReconcilerFixture([]uint64{1})
	f.outbox.traceErr = errors.New("trace lookup failed")
	f.candidates.set(ReconcileCandidate{TaskID: 1})

	require.Equal(t, 1, f.sweep(t))
	require.Equal(t, []uint64{1}, f.dispatched(t))
}
