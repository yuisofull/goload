package download

import (
	"context"
	stderrors "errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
)

const (
	// DefaultRenewInterval is how often a running attempt renews its lease
	// when no progress report renews it earlier. It must stay well below the
	// task service's lease duration (task.DefaultLeaseDuration, 60s).
	DefaultRenewInterval = 5 * time.Second

	// leaseSafetyMargin is how long before its confirmed lease expiry a worker
	// that can no longer renew stops the transfer, to yield before another can claim.
	leaseSafetyMargin = 5 * time.Second

	// leaseWatchInterval is how often the watchdog checks the lease deadline.
	leaseWatchInterval = time.Second

	// attemptCallTimeout bounds one call to the ownership API.
	attemptCallTimeout = 10 * time.Second
	// completeTries is how often CompleteAttempt is tried while its outcome
	// is unknown (an error that is not a rejection).
	completeTries = 3
	// completeRetryDelay is the pause between those tries.
	completeRetryDelay = time.Second
)

// errOwnershipUnreachable: an ownership call failed transiently (not a fence),
// so hand the attempt back rather than fail the download.
var errOwnershipUnreachable = stderrors.New("ownership API unreachable")

// AttemptCoordinator is the ownership API a download worker needs. The task
// service's attempt service satisfies it directly; the shared types and
// sentinel errors live in internal/events. Mutating calls fail with
// events.ErrAttemptRejected unless the worker id and generation match and the
// lease is unexpired; ClaimTask fails with events.ErrTaskNotClaimable for a
// task that cannot be claimed.
type AttemptCoordinator interface {
	ClaimTask(ctx context.Context, taskID uint64, workerID string) (*events.Attempt, error)
	RenewLease(
		ctx context.Context,
		taskID uint64,
		workerID string,
		generation uint64,
		update events.LeaseUpdate,
	) (time.Time, error)
	CompleteAttempt(
		ctx context.Context,
		taskID uint64,
		workerID string,
		generation uint64,
		result events.AttemptResult,
	) error
	FailAttempt(ctx context.Context, taskID uint64, workerID string, generation uint64, cause error) error
	ReleaseAttempt(ctx context.Context, taskID uint64, workerID string, generation uint64) error
}

// AttemptRunner is implemented by a download service that executes dispatched
// tasks under the task service's ownership API (see WithAttemptCoordinator).
type AttemptRunner interface {
	// RunAttempt executes one dispatch and returns once its delivery is
	// settled. nil means the delivery can be acknowledged: the outcome is
	// durably recorded, or there was nothing to run. An error means no outcome
	// could be recorded.
	RunAttempt(ctx context.Context, req TaskRequest) error
}

// AdmittingRunner is an AttemptRunner whose download slots are taken by the
// caller. A consumer that Admits before it spawns a worker goroutine bounds
// the goroutines it has in flight and keeps backpressure on its input; the
// slot is owned by the service's semaphore alone and is released by
// RunAdmittedAttempt.
type AdmittingRunner interface {
	AttemptRunner
	// Admit blocks until a download slot is free. On nil the caller must pass
	// the dispatch to RunAdmittedAttempt, which releases the slot.
	Admit(ctx context.Context) error
	// RunAdmittedAttempt is RunAttempt for a caller that holds a slot from
	// Admit; the slot is released when it returns.
	RunAdmittedAttempt(ctx context.Context, req TaskRequest) error
}

var (
	_ AttemptRunner   = (*service)(nil)
	_ AdmittingRunner = (*service)(nil)
)

// WithAttemptCoordinator makes the service attempt-aware: RunAttempt claims every task
// through attempts before it executes it. workerID is the stable identity of
// this worker process; the task service stores it as the owner of each claim.
func WithAttemptCoordinator(attempts AttemptCoordinator, workerID string) Option {
	return func(s *service) {
		s.attempts = attempts
		s.workerID = workerID
	}
}

// WithRenewInterval overrides DefaultRenewInterval. Values that are zero or
// negative are ignored.
func WithRenewInterval(d time.Duration) Option {
	return func(s *service) {
		if d > 0 {
			s.renewInterval = d
		}
	}
}

// WithAttemptClock overrides the clock the lease watchdog measures against. It
// must match the ownership API's clock. A nil clock is ignored.
func WithAttemptClock(now func() time.Time) Option {
	return func(s *service) {
		if now != nil {
			s.now = now
		}
	}
}

// attemptRun is the state of one claimed attempt while it executes.
type attemptRun struct {
	svc        *service
	taskID     uint64
	generation uint64

	mu       sync.Mutex
	progress Progress
	storing  bool
	// deadline is the last confirmed lease expiry; the heartbeat self-aborts past it.
	deadline time.Time

	// wake asks the heartbeat for an early renewal that carries progress.
	wake chan struct{}
	// fenced is set once the task service rejected this attempt: it lost
	// ownership and must stop without forcing a result.
	fenced atomic.Bool
}

// reportProgress stores the latest progress and asks for an early renewal.
func (r *attemptRun) reportProgress(p Progress) {
	r.mu.Lock()
	r.progress = p
	r.mu.Unlock()

	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// renew sends one heartbeat with the current progress. storing reports that
// the source is drained (DOWNLOADING -> STORING); once reported it is repeated
// on every later heartbeat, which the task service treats as a no-op.
func (r *attemptRun) renew(ctx context.Context, storing bool) error {
	r.mu.Lock()
	if storing {
		r.storing = true
	}
	progress := events.AttemptProgress{
		Progress:        r.progress.Progress,
		DownloadedBytes: r.progress.DownloadedBytes,
		TotalBytes:      r.progress.TotalBytes,
	}
	update := events.LeaseUpdate{Progress: &progress, Storing: r.storing}
	r.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, attemptCallTimeout)
	defer cancel()
	expiry, err := r.svc.attempts.RenewLease(callCtx, r.taskID, r.svc.workerID, r.generation, update)
	if err == nil {
		r.mu.Lock()
		r.deadline = expiry
		r.mu.Unlock()
	}
	if stderrors.Is(err, events.ErrAttemptRejected) {
		r.fenced.Store(true)
	}
	return err
}

// watchLease stops the transfer once the confirmed lease is within the safety
// margin of expiring, measured on the service clock so a fake test clock stays
// consistent with the lease. It runs on its own goroutine, so it fires even
// while a renewal RPC blocks. A lost lease fences the attempt: settlement then
// abandons it and the reconciler recovers the task, rather than recording a
// failure against a lease that was merely unreachable.
func (r *attemptRun) watchLease(ctx context.Context, stop context.CancelFunc) {
	ticker := time.NewTicker(leaseWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.Lock()
			dl := r.deadline
			r.mu.Unlock()
			if !dl.IsZero() && r.svc.now().After(dl.Add(-leaseSafetyMargin)) {
				r.fenced.Store(true)
				stop()
				return
			}
		}
	}
}

// heartbeat renews the lease until ctx ends: every renew interval, and earlier
// whenever the transfer reports progress. A rejected renewal means the attempt
// lost ownership; stop is called so the transfer aborts.
//
// A watchdog stops the transfer when the confirmed lease is within the safety
// margin of expiring, even while a slow renewal RPC is in flight. Each
// confirmed renewal pushes it out; losing the lease lets it fire, which fences
// the attempt so settlement abandons it (the reconciler recovers the task)
// rather than recording a failure against a lease that was merely unreachable.
func (r *attemptRun) heartbeat(ctx context.Context, stop context.CancelFunc) {
	ticker := time.NewTicker(r.svc.renewInterval)
	defer ticker.Stop()

	go r.watchLease(ctx, stop)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}

		err := r.renew(ctx, false)
		if err == nil {
			continue
		}
		if r.fenced.Load() {
			stop()
			return
		}
		if ctx.Err() == nil {
			r.svc.errorHandler(
				ctx,
				fmt.Errorf("renew lease of task %d generation %d: %w", r.taskID, r.generation, err),
			)
		}
	}
}

// RunAttempt implements AttemptRunner: wait for a slot, claim, transfer under
// a heartbeat, then record the outcome.
func (s *service) RunAttempt(ctx context.Context, req TaskRequest) error {
	return s.runAttempt(ctx, req, false)
}

// Admit implements AdmittingRunner.
func (s *service) Admit(ctx context.Context) error {
	return s.sem.Acquire(ctx, 1)
}

// RunAdmittedAttempt implements AdmittingRunner.
func (s *service) RunAdmittedAttempt(ctx context.Context, req TaskRequest) error {
	defer s.sem.Release(1)
	return s.runAttempt(ctx, req, true)
}

// runAttempt executes one dispatch. admitted reports that the caller already
// holds a download slot (and releases it); otherwise runAttempt waits for one.
func (s *service) runAttempt(ctx context.Context, req TaskRequest, admitted bool) error {
	if s.attempts == nil || s.workerID == "" {
		return &errors.Error{
			Code:    errors.ErrCodeInternal,
			Message: "download service has no attempt API or worker id configured",
		}
	}

	// queueCtx ends when the worker shuts down or when the task service stops
	// the task (pause, cancel) while it is queued or running.
	queueCtx, stop := context.WithCancel(ctx)
	defer stop()
	execution := &taskExecution{task: req, ctx: queueCtx, cancelFunc: stop}

	s.mu.Lock()
	if _, active := s.activeTasks[req.TaskID]; active {
		s.mu.Unlock()
		// This worker already queues or runs the task: a duplicate delivery.
		return nil
	}
	downloader, hasDownloader := s.downloaders[req.SourceType]
	s.activeTasks[req.TaskID] = execution
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.activeTasks, req.TaskID)
		s.mu.Unlock()
	}()

	// Take the slot before claiming: the lease then only has to cover the
	// execution, and a queued task stays PENDING.
	if !admitted {
		if err := s.sem.Acquire(queueCtx, 1); err != nil {
			if s.isStopped(execution) {
				return nil
			}
			return fmt.Errorf("failed to acquire download slot: %w", err)
		}
		defer s.sem.Release(1)
	}

	claimCtx, cancelClaim := context.WithTimeout(queueCtx, attemptCallTimeout)
	attempt, err := s.attempts.ClaimTask(claimCtx, req.TaskID, s.workerID)
	cancelClaim()
	if err != nil {
		if stderrors.Is(err, events.ErrTaskNotClaimable) ||
			errors.IsError(err, errors.ErrCodeNotFound) ||
			s.isStopped(execution) {
			// Terminal, paused, deleted, not due yet or leased by a live
			// attempt: drop the delivery. The reconciler dispatches the task
			// again when it becomes claimable.
			return nil
		}
		return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to claim task", Cause: err}
	}

	run := &attemptRun{
		svc:        s,
		taskID:     req.TaskID,
		generation: attempt.Generation,
		wake:       make(chan struct{}, 1),
		deadline:   attempt.LeaseExpiresAt,
	}

	// The claimed request carries the generation, which namespaces the
	// storage key of this attempt.
	claimed := req
	claimed.Generation = attempt.Generation
	execCtx, cancelExec := context.WithTimeout(queueCtx, s.taskTimeOut)
	defer cancelExec()

	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		run.heartbeat(heartbeatCtx, cancelExec)
	}()
	defer func() {
		stopHeartbeat()
		<-heartbeatDone
	}()

	var stored *transferResult
	if hasDownloader {
		stored, err = s.runTransfer(&taskExecution{task: claimed, ctx: execCtx, cancelFunc: stop}, downloader, run)
	} else {
		err = &errors.Error{
			Code:    errors.ErrCodeInvalidInput,
			Message: fmt.Sprintf("no downloader for source type %s", req.SourceType),
		}
	}
	if err != nil {
		return s.settleFailure(ctx, execution, run, err)
	}
	return s.settleSuccess(ctx, run, stored)
}

// runTransfer runs the transfer of a claimed attempt. Progress and the storing
// step go to the task service through the lease; nothing is published on the
// bus.
func (s *service) runTransfer(
	execution *taskExecution,
	downloader Downloader,
	run *attemptRun,
) (stored *transferResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			stored = nil
			err = fmt.Errorf("recovered from panic while executing task: %v", r)
		}
	}()

	return s.transfer(execution, downloader, transferHooks{
		downloading: func(context.Context) error { return nil },
		progress:    func(_ context.Context, p Progress) { run.reportProgress(p) },
		storing:     run.confirmStoring,
	})
}

// confirmStoring drives the DOWNLOADING -> STORING transition at EOF. The bytes
// are already fetched, so a transient ownership error here must not fail the
// task: it is retried within the lease, and a persistent one is reported as
// errOwnershipUnreachable so settlement hands the task back without spending a
// retry (the reconciler recovers it). A fence is returned as-is: the attempt
// lost ownership and settlement abandons it.
func (r *attemptRun) confirmStoring(ctx context.Context) error {
	var err error
	for try := 1; try <= completeTries; try++ {
		if err = r.renew(ctx, true); err == nil {
			return nil
		}
		if r.fenced.Load() {
			return err
		}
		// A task deadline or cancel (even one that expired during the renewal) is
		// a real failure, not an ownership outage.
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if try < completeTries {
			time.Sleep(completeRetryDelay)
		}
	}
	return fmt.Errorf("%w: %v", errOwnershipUnreachable, err)
}

// settleSuccess commits the stored object as the task's result.
func (s *service) settleSuccess(ctx context.Context, run *attemptRun, stored *transferResult) error {
	result := events.AttemptResult{
		FileName:    stored.FileName,
		FileSize:    stored.FileSize,
		ContentType: stored.ContentType,
		Checksum:    &events.ChecksumInfo{ChecksumType: "md5", ChecksumValue: stored.MD5},
		StorageType: s.storageType,
		StorageKey:  stored.StorageKey,
	}

	var err error
	for try := 1; try <= completeTries; try++ {
		// A completion must not be cut short by a shutdown: the object is
		// already stored.
		callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attemptCallTimeout)
		err = s.attempts.CompleteAttempt(callCtx, run.taskID, s.workerID, run.generation, result)
		cancel()
		if err == nil {
			return nil
		}
		if stderrors.Is(err, events.ErrAttemptRejected) {
			if try == 1 {
				// Nothing of this attempt was ever committed, so no result
				// references its object: remove it. After an unanswered try
				// the earlier call may have committed, and the object stays.
				_ = s.storage.Delete(context.Background(), stored.StorageKey)
			}
			return nil
		}
		s.errorHandler(ctx, fmt.Errorf(
			"complete task %d generation %d (try %d of %d): %w",
			run.taskID, run.generation, try, completeTries, err,
		))
		if try < completeTries {
			time.Sleep(completeRetryDelay)
		}
	}

	// The outcome is unknown: the completion may have committed. Keep the
	// object and report no failure; the lease decides what happens next.
	return &errors.Error{Code: errors.ErrCodeInternal, Message: "completion outcome unknown", Cause: err}
}

// settleFailure records why the attempt ended without a result.
func (s *service) settleFailure(ctx context.Context, execution *taskExecution, run *attemptRun, cause error) error {
	if run.fenced.Load() || s.isStopped(execution) {
		// The task service already retired this attempt (pause, cancel, a
		// newer generation or an expired lease). transfer removed the
		// attempt's own partial object; there is nothing to report or force.
		return nil
	}

	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attemptCallTimeout)
	defer cancel()

	// Hand the task back (no retry spent) rather than fail it when the download
	// did not fail: the worker is shutting down, or an ownership call was
	// unreachable (e.g. the storing transition after the bytes were fetched).
	// Otherwise the transfer genuinely failed, so count the retry.
	handBack := ctx.Err() != nil || stderrors.Is(cause, errOwnershipUnreachable)

	var err error
	if handBack {
		err = s.attempts.ReleaseAttempt(callCtx, run.taskID, s.workerID, run.generation)
	} else {
		s.errorHandler(ctx, cause)
		err = s.attempts.FailAttempt(callCtx, run.taskID, s.workerID, run.generation, cause)
	}
	if err == nil || stderrors.Is(err, events.ErrAttemptRejected) {
		return nil
	}
	return &errors.Error{Code: errors.ErrCodeInternal, Message: "failed to record attempt outcome", Cause: err}
}
