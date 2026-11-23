package task

import (
	"context"
	"fmt"
	"time"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
)

const (
	// DefaultLeaseDuration is how long a claim or a renewal keeps a task owned.
	DefaultLeaseDuration = 60 * time.Second
	// DefaultRetryBackoffBase is the delay before the first automatic retry.
	DefaultRetryBackoffBase = 5 * time.Second
	// DefaultRetryBackoffMax caps the delay between automatic retries.
	DefaultRetryBackoffMax = 5 * time.Minute
)

// AttemptState is the ownership view of one task row. The task service loads
// it under a row lock, applies exactly one transition and saves it back, all
// inside one transaction.
type AttemptState struct {
	TaskID uint64
	Status TaskStatus

	// OwnerID is the worker holding the task; empty means unowned.
	OwnerID string
	// LeaseExpiresAt is when the current claim expires unless renewed.
	LeaseExpiresAt *time.Time
	// Generation is the fencing token. It starts at 0 and grows by one on
	// every successful claim.
	Generation uint64
	// RetryCount is the number of failed attempts so far.
	RetryCount int
	// NextAttemptAt is the earliest time a retry may be claimed; nil when the
	// task is not waiting on a retry backoff.
	NextAttemptAt *time.Time
	// MaxRetries is the task's retry budget (tasks.max_retries). It is read
	// with the state and never written back.
	MaxRetries int
	// CreatedAt is the task's creation time (tasks.created_at), read with the
	// state and never written back. Used for the dispatch-wait metric.
	CreatedAt time.Time

	// Columns an attempt writes. Only complete attaches StorageType and
	// StoragePath.
	FileName    string
	StorageType storage.Type
	StoragePath string
	// ContentType is the committed result's content type.
	ContentType string
	// FileSize is the committed result's byte size. It is the result
	// identity, distinct from Progress.TotalBytes and DownloadedBytes, which
	// are live progress for display.
	FileSize     int64
	Checksum     *ChecksumInfo
	Progress     DownloadProgress
	ErrorMessage *string
	CompletedAt  *time.Time
}

// AttemptRepository loads and stores the ownership state of task rows.
type AttemptRepository interface {
	// GetAttemptState reads the state without locking. It returns
	// errors.ErrNotFound when the task does not exist.
	GetAttemptState(ctx context.Context, taskID uint64) (*AttemptState, error)
	// LockAttemptState locks the task row for the rest of the transaction
	// carried by ctx and returns its state. Call it before any other read in
	// that transaction. It fails when ctx carries no transaction started by
	// TxManager.DoInTx and returns errors.ErrNotFound for an unknown task.
	LockAttemptState(ctx context.Context, taskID uint64) (*AttemptState, error)
	// SaveAttemptState writes every field of state except TaskID and
	// MaxRetries back to the row. It must run in the transaction of the
	// LockAttemptState call that produced state.
	SaveAttemptState(ctx context.Context, state *AttemptState) error
}

// DefaultRetryBackoff returns the delay before retry number retryCount (1 for
// the first retry): 5s, 10s, 20s and so on, capped at 5 minutes.
func DefaultRetryBackoff(retryCount int) time.Duration {
	delay := DefaultRetryBackoffBase
	for i := 1; i < retryCount; i++ {
		delay *= 2
		if delay >= DefaultRetryBackoffMax {
			return DefaultRetryBackoffMax
		}
	}
	return delay
}

func notClaimable(format string, args ...any) error {
	return &errors.Error{
		Code:    errors.ErrCodeInvalidState,
		Message: fmt.Sprintf(format, args...),
		Cause:   events.ErrTaskNotClaimable,
	}
}

func attemptRejected(format string, args ...any) error {
	return &errors.Error{
		Code:    errors.ErrCodeConflict,
		Message: fmt.Sprintf(format, args...),
		Cause:   events.ErrAttemptRejected,
	}
}

func invalidTransition(message string) error {
	return &errors.Error{Code: errors.ErrCodeInvalidInput, Message: message}
}

// leaseActive reports whether the current claim is still valid at now. A
// lease is expired from the instant now reaches LeaseExpiresAt.
func (st *AttemptState) leaseActive(now time.Time) bool {
	return st.LeaseExpiresAt != nil && now.Before(*st.LeaseExpiresAt)
}

func (st *AttemptState) clearOwnership() {
	st.OwnerID = ""
	st.LeaseExpiresAt = nil
}

// claim implements ClaimTask. Claimable: PENDING, unowned and due; or
// DOWNLOADING/STORING whose lease expired (the previous owner is presumed
// dead). A successful claim always starts a new generation.
func (st *AttemptState) claim(workerID string, now time.Time, lease time.Duration) error {
	switch st.Status {
	case StatusPending:
		if st.OwnerID != "" {
			return notClaimable("task %d is PENDING but still owned by %s", st.TaskID, st.OwnerID)
		}
		if st.NextAttemptAt != nil && now.Before(*st.NextAttemptAt) {
			return notClaimable(
				"task %d retry is not due until %s",
				st.TaskID, st.NextAttemptAt.UTC().Format(time.RFC3339),
			)
		}
	case StatusDownloading, StatusStoring:
		if st.leaseActive(now) {
			return notClaimable("task %d is leased by %s (generation %d)", st.TaskID, st.OwnerID, st.Generation)
		}
	default:
		return notClaimable("task %d is %s", st.TaskID, st.Status)
	}

	expires := now.Add(lease)
	st.Status = StatusDownloading
	st.OwnerID = workerID
	st.LeaseExpiresAt = &expires
	st.Generation++
	st.NextAttemptAt = nil
	// Every attempt downloads from zero.
	st.Progress = DownloadProgress{}
	return nil
}

// checkOwned is the fencing check shared by renew, complete, fail and
// release: the task must be in an owned state, held by exactly this worker
// and generation, with an unexpired lease.
func (st *AttemptState) checkOwned(workerID string, generation uint64, now time.Time) error {
	if st.Status != StatusDownloading && st.Status != StatusStoring {
		return attemptRejected("task %d is %s", st.TaskID, st.Status)
	}
	if st.OwnerID != workerID || st.Generation != generation {
		return attemptRejected(
			"task %d is owned by generation %d, not by generation %d of %s",
			st.TaskID, st.Generation, generation, workerID,
		)
	}
	if !st.leaseActive(now) {
		return attemptRejected("lease for task %d generation %d has expired", st.TaskID, generation)
	}
	return nil
}

// renew implements RenewLease: the heartbeat, optionally carrying progress
// and the DOWNLOADING -> STORING step.
func (st *AttemptState) renew(
	workerID string,
	generation uint64,
	now time.Time,
	lease time.Duration,
	update events.LeaseUpdate,
) error {
	if err := st.checkOwned(workerID, generation, now); err != nil {
		return err
	}
	expires := now.Add(lease)
	st.LeaseExpiresAt = &expires
	if update.Storing && st.Status == StatusDownloading {
		st.Status = StatusStoring
	}
	if update.Progress != nil {
		st.Progress = DownloadProgress{
			Progress:        update.Progress.Progress,
			DownloadedBytes: update.Progress.DownloadedBytes,
			TotalBytes:      update.Progress.TotalBytes,
		}
	}
	return nil
}

// matchesResult reports whether result is exactly the result this completed
// task already stores. A faithful lost-response retry resends the identical
// result, so the comparison is strict: an omitted or changed FileName or
// Checksum is a different result and does not match.
//
// The full result identity (StorageKey, StorageType, FileName, Checksum,
// ContentType, FileSize) is persisted and compared. ContentType and FileSize
// live in dedicated result columns written unconditionally by complete, not in
// total_bytes (which is live progress gated on FileSize > 0), so a faithful
// FileSize==0 retry stores and matches 0 and is not falsely rejected.
func (st *AttemptState) matchesResult(result events.AttemptResult) bool {
	if st.StoragePath != result.StorageKey || st.StorageType != result.StorageType {
		return false
	}
	if st.FileName != result.FileName {
		return false
	}
	if st.ContentType != result.ContentType || st.FileSize != result.FileSize {
		return false
	}
	if (st.Checksum == nil) != (result.Checksum == nil) {
		return false
	}
	if st.Checksum != nil &&
		(st.Checksum.ChecksumType != result.Checksum.ChecksumType ||
			st.Checksum.ChecksumValue != result.Checksum.ChecksumValue) {
		return false
	}
	return true
}

// complete implements CompleteAttempt. alreadyDone is true when the task was
// already completed by this same generation with this same result: the first
// call committed and only its response was lost. That case is evaluated
// first, before the ordinary ownership and lease rejection, because the
// committed row has no owner and no lease any more.
func (st *AttemptState) complete(
	workerID string,
	generation uint64,
	now time.Time,
	result events.AttemptResult,
) (alreadyDone bool, err error) {
	if st.Status == StatusCompleted {
		if st.Generation == generation && st.matchesResult(result) {
			return true, nil
		}
		return false, attemptRejected("task %d is already COMPLETED by generation %d", st.TaskID, st.Generation)
	}
	if err := st.checkOwned(workerID, generation, now); err != nil {
		return false, err
	}
	if st.Status != StatusStoring {
		return false, attemptRejected("task %d generation %d has not entered STORING", st.TaskID, generation)
	}

	st.Status = StatusCompleted
	st.clearOwnership()
	st.NextAttemptAt = nil
	if result.FileName != "" {
		st.FileName = result.FileName
	}
	st.StorageType = result.StorageType
	st.StoragePath = result.StorageKey
	// Result identity: stored unconditionally (an empty file has FileSize 0).
	st.ContentType = result.ContentType
	st.FileSize = result.FileSize
	if result.Checksum != nil {
		st.Checksum = &ChecksumInfo{
			ChecksumType:  result.Checksum.ChecksumType,
			ChecksumValue: result.Checksum.ChecksumValue,
		}
	}
	if result.FileSize > 0 {
		st.Progress.TotalBytes = result.FileSize
		st.Progress.DownloadedBytes = result.FileSize
	}
	st.Progress.Progress = 100
	st.ErrorMessage = nil
	completedAt := now
	st.CompletedAt = &completedAt
	return false, nil
}

// fail implements FailAttempt: back to PENDING with a retry time while the
// retry budget lasts, terminal FAILED once it is exhausted.
func (st *AttemptState) fail(
	workerID string,
	generation uint64,
	now time.Time,
	message string,
	backoff func(retryCount int) time.Duration,
) error {
	if err := st.checkOwned(workerID, generation, now); err != nil {
		return err
	}
	st.clearOwnership()
	st.RetryCount++
	st.ErrorMessage = &message
	if st.RetryCount > st.MaxRetries {
		st.Status = StatusFailed
		st.NextAttemptAt = nil
		return nil
	}
	next := now.Add(backoff(st.RetryCount))
	st.Status = StatusPending
	st.NextAttemptAt = &next
	return nil
}

// release implements ReleaseAttempt: a voluntary hand-back that is not a
// failure. The task is claimable again immediately.
func (st *AttemptState) release(workerID string, generation uint64, now time.Time) error {
	if err := st.checkOwned(workerID, generation, now); err != nil {
		return err
	}
	st.Status = StatusPending
	st.clearOwnership()
	st.NextAttemptAt = nil
	return nil
}

// cancel implements the user's CancelTask. An active attempt is fenced out:
// its next renew, complete, fail or release is rejected.
func (st *AttemptState) cancel() error {
	if st.Status == StatusCompleted || st.Status == StatusCancelled {
		return invalidTransition("Completed or cancelled tasks cannot be cancelled")
	}
	st.Status = StatusCancelled
	st.clearOwnership()
	st.NextAttemptAt = nil
	return nil
}

// pause implements the user's PauseTask. The attempt is retired; its bytes
// are discarded (no byte-level resume in stage 1).
func (st *AttemptState) pause() error {
	if st.Status != StatusDownloading {
		return invalidTransition("Only downloading tasks can be paused")
	}
	st.Status = StatusPaused
	st.clearOwnership()
	return nil
}

// resume implements the user's ResumeTask. The task becomes claimable and
// is downloaded again from zero by a new attempt.
func (st *AttemptState) resume() error {
	if st.Status != StatusPaused {
		return invalidTransition("Only paused tasks can be resumed")
	}
	st.Status = StatusPending
	st.clearOwnership()
	st.NextAttemptAt = nil
	return nil
}

// retry implements the user's RetryTask. It grants a fresh retry budget.
func (st *AttemptState) retry() error {
	if st.Status != StatusFailed {
		return invalidTransition("Only failed tasks can be retried")
	}
	st.Status = StatusPending
	st.clearOwnership()
	st.RetryCount = 0
	st.NextAttemptAt = nil
	return nil
}
