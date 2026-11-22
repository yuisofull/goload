package task

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/yuisofull/goload/internal/errors"
)

const (
	// DefaultReconcileInterval is how often the reconciler sweeps.
	DefaultReconcileInterval = 5 * time.Second
	// DefaultRedispatchInterval is the minimum time between two dispatches of
	// a task whose attempt generation did not change in between.
	DefaultRedispatchInterval = 30 * time.Second
	// DefaultReconcileBatchSize is the page size of one candidate query.
	DefaultReconcileBatchSize = 100
	// DefaultReconcileMaxPerPass caps the dispatches of one sweep.
	DefaultReconcileMaxPerPass = 100
)

// Reconciler periodically dispatches tasks that should be running but have no
// live attempt: PENDING tasks that are unowned and due (a lost dispatch, a
// released task, or a retry whose backoff passed) and DOWNLOADING or STORING
// tasks whose lease expired (the worker died or stalled).
//
// It only writes a TaskCreated outbox row. Whether the task really runs is
// decided by ClaimTask, so a redundant dispatch is dropped by the worker.
//
// Redispatch is rate-limited twice: a task whose generation has not changed
// since its last dispatch is dispatched again only after the redispatch
// interval, and one sweep sends at most a fixed number of dispatches.
type Reconciler struct {
	candidates ReconcileRepository
	tasks      Repository
	outbox     OutboxRepository

	now                func() time.Time
	interval           time.Duration
	redispatchInterval time.Duration
	batchSize          int
	maxPerPass         int
	logger             log.Logger
	metrics            Metrics

	// last remembers the dispatches of earlier sweeps. Only Run and RunOnce
	// touch it; they must not be called concurrently.
	last map[uint64]redispatchMark
	// cursor is the id after which the next sweep resumes. It persists across
	// sweeps and wraps to 0 only when a sweep reaches the end of the list, so
	// a backlog larger than maxPerPass cannot starve its tail.
	cursor uint64
}

// redispatchMark is the last dispatch the reconciler wrote for a task.
type redispatchMark struct {
	generation uint64
	at         time.Time
}

// ReconcilerOption configures a Reconciler.
type ReconcilerOption func(*Reconciler)

// WithReconcileInterval overrides DefaultReconcileInterval. Values that are
// zero or negative are ignored.
func WithReconcileInterval(d time.Duration) ReconcilerOption {
	return func(r *Reconciler) {
		if d > 0 {
			r.interval = d
		}
	}
}

// WithRedispatchInterval overrides DefaultRedispatchInterval. Values that are
// zero or negative are ignored.
func WithRedispatchInterval(d time.Duration) ReconcilerOption {
	return func(r *Reconciler) {
		if d > 0 {
			r.redispatchInterval = d
		}
	}
}

// WithReconcileBatchSize overrides DefaultReconcileBatchSize. Values that are
// zero or negative are ignored.
func WithReconcileBatchSize(n int) ReconcilerOption {
	return func(r *Reconciler) {
		if n > 0 {
			r.batchSize = n
		}
	}
}

// WithReconcileMaxPerPass overrides DefaultReconcileMaxPerPass. Values that
// are zero or negative are ignored.
func WithReconcileMaxPerPass(n int) ReconcilerOption {
	return func(r *Reconciler) {
		if n > 0 {
			r.maxPerPass = n
		}
	}
}

// WithReconcilerClock overrides the clock that decides what is due and what
// is expired. It must be the clock the ownership API uses (WithAttemptClock).
// A nil clock is ignored.
func WithReconcilerClock(now func() time.Time) ReconcilerOption {
	return func(r *Reconciler) {
		if now != nil {
			r.now = now
		}
	}
}

// WithReconcilerMetrics injects a metrics recorder. Nil keeps the no-op default.
func WithReconcilerMetrics(m Metrics) ReconcilerOption {
	return func(r *Reconciler) {
		if m != nil {
			r.metrics = m
		}
	}
}

// WithReconcilerLogger sets the reconciler logger. A nil logger is ignored.
func WithReconcilerLogger(l log.Logger) ReconcilerOption {
	return func(r *Reconciler) {
		if l != nil {
			r.logger = l
		}
	}
}

// NewReconciler creates a reconciler. candidates, tasks and outbox must be
// repositories of the same database.
func NewReconciler(
	candidates ReconcileRepository,
	tasks Repository,
	outbox OutboxRepository,
	opts ...ReconcilerOption,
) *Reconciler {
	r := &Reconciler{
		candidates:         candidates,
		tasks:              tasks,
		outbox:             outbox,
		now:                func() time.Time { return time.Now().UTC() },
		interval:           DefaultReconcileInterval,
		redispatchInterval: DefaultRedispatchInterval,
		batchSize:          DefaultReconcileBatchSize,
		maxPerPass:         DefaultReconcileMaxPerPass,
		logger:             log.NewNopLogger(),
		metrics:            nopMetrics{},
		last:               make(map[uint64]redispatchMark),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Run sweeps until ctx is cancelled and then returns ctx.Err(). The first
// sweep runs immediately, so tasks stranded by a previous process are
// dispatched on startup.
func (r *Reconciler) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		dispatched, err := r.RunOnce(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			_ = level.Warn(r.logger).
				Log("msg", "reconciler sweep incomplete, will retry", "dispatched", dispatched, "err", err)
		case dispatched > 0:
			_ = level.Info(r.logger).Log("msg", "reconciler dispatched tasks again", "dispatched", dispatched)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// RunOnce performs one sweep. It returns how many tasks it dispatched and the
// first error it met. A task whose dispatch failed is not remembered, so the
// next sweep tries it again.
func (r *Reconciler) RunOnce(ctx context.Context) (int, error) {
	var (
		now        = r.now()
		seen       = make(map[uint64]redispatchMark)
		dispatched int
		firstErr   error
		startID    = r.cursor
		afterID    = startID
		complete   bool
	)

sweep:
	for {
		if err := ctx.Err(); err != nil {
			firstErr = err
			break
		}
		page, err := r.candidates.ListReconcilable(ctx, now, afterID, r.batchSize)
		if err != nil {
			firstErr = fmt.Errorf("list reconcilable tasks: %w", err)
			break
		}

		for _, candidate := range page {
			mark, known := r.last[candidate.TaskID]
			if known && mark.generation == candidate.Generation && now.Sub(mark.at) < r.redispatchInterval {
				// Dispatched for this generation a moment ago: that dispatch
				// is still on its way or waits for a download slot.
				seen[candidate.TaskID] = mark
				afterID = candidate.TaskID
				continue
			}
			if dispatched >= r.maxPerPass {
				// The rest waits for the next sweep.
				break sweep
			}

			sent, err := r.redispatch(ctx, candidate.TaskID)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				afterID = candidate.TaskID
				continue
			}
			afterID = candidate.TaskID
			if sent {
				seen[candidate.TaskID] = redispatchMark{generation: candidate.Generation, at: now}
				dispatched++
				r.metrics.IncRedispatched()
			}
		}

		if len(page) < r.batchSize {
			complete = true
			break
		}
	}

	if complete {
		// The end of the list was reached: forget the tasks after the start
		// cursor that left the list, then wrap to the head next sweep.
		for id := range r.last {
			if _, ok := seen[id]; !ok && id > startID {
				delete(r.last, id)
			}
		}
		r.cursor = 0
	} else {
		r.cursor = afterID
	}
	for id, mark := range seen {
		r.last[id] = mark
	}
	return dispatched, firstErr
}

// redispatch enqueues a TaskCreated outbox row for the task, the same
// dispatch CreateTask writes. It reports false without an error when the task
// no longer exists.
func (r *Reconciler) redispatch(ctx context.Context, taskID uint64) (bool, error) {
	t, err := r.tasks.GetByID(ctx, taskID)
	if err != nil {
		if stderrors.Is(err, errors.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("load task %d for dispatch: %w", taskID, err)
	}

	event, err := NewTaskCreatedOutboxEvent(t)
	if err != nil {
		return false, fmt.Errorf("build dispatch event for task %d: %w", taskID, err)
	}
	if err := r.outbox.InsertUnsent(ctx, event); err != nil {
		return false, fmt.Errorf("enqueue dispatch event for task %d: %w", taskID, err)
	}
	return true, nil
}
