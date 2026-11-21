package download

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
)

// fakeAttempts is a scripted AttemptCoordinator. It hands out one generation
// and records every call in order: "claim", "renew" (a heartbeat), "storing"
// (a heartbeat that reports the storing step), "complete", "fail", "release".
type fakeAttempts struct {
	mu sync.Mutex

	generation   uint64  // generation ClaimTask hands out
	claimErr     error   // returned by ClaimTask
	renewErr     error   // returned by every RenewLease while set
	storingErr   error   // returned by a RenewLease that reports Storing
	completeErrs []error // returned by CompleteAttempt, one per call, then nil
	failErr      error   // returned by FailAttempt
	releaseErr   error   // returned by ReleaseAttempt

	// leaseExpiresAt is returned by ClaimTask and a successful RenewLease.
	leaseExpiresAt time.Time

	calls     []string
	workers   []string
	completed []events.AttemptResult
	failures  []error

	gens    map[string][]uint64  // generation received per call name
	updates []events.LeaseUpdate // every RenewLease update, in order
}

// recordGen notes the generation an ownership call carried. Callers hold a.mu.
func (a *fakeAttempts) recordGen(call string, generation uint64) {
	if a.gens == nil {
		a.gens = map[string][]uint64{}
	}
	a.gens[call] = append(a.gens[call], generation)
}

// generations returns the generations received by a call name.
func (a *fakeAttempts) generations(call string) []uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]uint64(nil), a.gens[call]...)
}

// leaseUpdates returns every RenewLease update received.
func (a *fakeAttempts) leaseUpdates() []events.LeaseUpdate {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]events.LeaseUpdate(nil), a.updates...)
}

// requireGeneration fails unless the call was made at least once and every
// such call carried want.
func requireGeneration(t *testing.T, a *fakeAttempts, call string, want uint64) {
	t.Helper()
	got := a.generations(call)
	if len(got) == 0 {
		t.Fatalf("no %q call was recorded (calls = %v)", call, a.log())
	}
	for _, g := range got {
		if g != want {
			t.Fatalf("%q carried generation %d, want the claimed generation %d", call, g, want)
		}
	}
}

func (a *fakeAttempts) ClaimTask(_ context.Context, taskID uint64, workerID string) (*events.Attempt, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "claim")
	a.workers = append(a.workers, workerID)
	if a.claimErr != nil {
		return nil, a.claimErr
	}
	return &events.Attempt{TaskID: taskID, WorkerID: workerID, Generation: a.generation, LeaseExpiresAt: a.leaseExpiresAt}, nil
}

func (a *fakeAttempts) RenewLease(
	_ context.Context,
	_ uint64,
	workerID string,
	generation uint64,
	update events.LeaseUpdate,
) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.workers = append(a.workers, workerID)
	a.updates = append(a.updates, update)
	if update.Storing {
		a.calls = append(a.calls, "storing")
		a.recordGen("storing", generation)
	} else {
		a.calls = append(a.calls, "renew")
		a.recordGen("renew", generation)
	}
	if a.renewErr != nil {
		return time.Time{}, a.renewErr
	}
	if update.Storing && a.storingErr != nil {
		return time.Time{}, a.storingErr
	}
	return a.leaseExpiresAt, nil
}

func (a *fakeAttempts) CompleteAttempt(
	_ context.Context,
	_ uint64,
	workerID string,
	generation uint64,
	result events.AttemptResult,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "complete")
	a.recordGen("complete", generation)
	a.workers = append(a.workers, workerID)
	a.completed = append(a.completed, result)
	if len(a.completeErrs) == 0 {
		return nil
	}
	err := a.completeErrs[0]
	a.completeErrs = a.completeErrs[1:]
	return err
}

func (a *fakeAttempts) FailAttempt(
	_ context.Context,
	_ uint64,
	workerID string,
	generation uint64,
	cause error,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "fail")
	a.recordGen("fail", generation)
	a.workers = append(a.workers, workerID)
	a.failures = append(a.failures, cause)
	return a.failErr
}

func (a *fakeAttempts) ReleaseAttempt(_ context.Context, _ uint64, workerID string, generation uint64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "release")
	a.recordGen("release", generation)
	a.workers = append(a.workers, workerID)
	return a.releaseErr
}

func (a *fakeAttempts) setRenewErr(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.renewErr = err
}

func (a *fakeAttempts) log() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

// count returns how often a call was recorded.
func (a *fakeAttempts) count(call string) int {
	n := 0
	for _, c := range a.log() {
		if c == call {
			n++
		}
	}
	return n
}

// first returns the position of the first such call, or -1.
func (a *fakeAttempts) first(call string) int {
	for i, c := range a.log() {
		if c == call {
			return i
		}
	}
	return -1
}

func (a *fakeAttempts) results() []events.AttemptResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]events.AttemptResult(nil), a.completed...)
}

func (a *fakeAttempts) failureCauses() []error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]error(nil), a.failures...)
}

func (a *fakeAttempts) workerIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.workers...)
}

// rejectedErr is the fencing rejection as the task service returns it.
func rejectedErr() error {
	return &apperrors.Error{
		Code:    apperrors.ErrCodeConflict,
		Message: "attempt rejected",
		Cause:   events.ErrAttemptRejected,
	}
}

// notClaimableErr is a rejected claim as the task service returns it.
func notClaimableErr() error {
	return &apperrors.Error{
		Code:    apperrors.ErrCodeInvalidState,
		Message: "task is not claimable",
		Cause:   events.ErrTaskNotClaimable,
	}
}

// busSpy counts the lifecycle events the download service publishes itself.
type busSpy struct {
	published atomic.Int32
}

func (b *busSpy) PublishTaskStatusUpdated(context.Context, events.TaskStatusUpdatedEvent) error {
	b.published.Add(1)
	return nil
}

func (b *busSpy) PublishTaskProgressUpdated(context.Context, events.TaskProgressUpdatedEvent) error {
	b.published.Add(1)
	return nil
}

func (b *busSpy) PublishTaskCompleted(context.Context, events.TaskCompletedEvent) error {
	b.published.Add(1)
	return nil
}

func (b *busSpy) PublishTaskFailed(context.Context, events.TaskFailedEvent) error {
	b.published.Add(1)
	return nil
}

// attemptRequest is the dispatch the RunAttempt tests execute.
func attemptRequest() TaskRequest {
	return TaskRequest{TaskID: 9, SourceURL: "https://example.com/files/file.txt", SourceType: "HTTP"}
}

// attemptKey is the storage key RunAttempt must use for a generation of
// attemptRequest. fakeDownloader resolves the file name file.txt.
func attemptKey(svc *service, generation uint64) string {
	req := attemptRequest()
	req.Generation = generation
	return svc.generateStorageKey(req, "file.txt")
}

func TestRunAttempt_ClaimsStoresUnderTheGenerationKeyAndCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &keyedStorage{objects: map[string]bool{}}
		attempts := &fakeAttempts{generation: 3}
		bus := &busSpy{}
		svc := NewService(store, bus, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", &fakeDownloader{})

		if err := svc.RunAttempt(t.Context(), attemptRequest()); err != nil {
			t.Fatalf("RunAttempt() error = %v", err)
		}

		wantKey := attemptKey(svc, 3)
		if !strings.HasPrefix(wantKey, "9/3/file-") {
			t.Fatalf("the attempt key must carry the claimed generation, got %q", wantKey)
		}
		if !store.has(wantKey) {
			t.Fatalf("object %q was not stored", wantKey)
		}
		want := events.AttemptResult{
			FileName:    "file.txt",
			FileSize:    7,
			ContentType: "text/plain",
			Checksum:    &events.ChecksumInfo{ChecksumType: "md5", ChecksumValue: "9a0364b9e99bb480dd25e1f0284c8555"},
			StorageType: storage.TypeLocal,
			StorageKey:  wantKey,
		}
		results := attempts.results()
		if len(results) != 1 || !reflect.DeepEqual(results[0], want) {
			t.Fatalf("CompleteAttempt results = %+v, want exactly %+v", results, want)
		}
		if got := attempts.first("claim"); got != 0 {
			t.Fatalf("calls = %v, want the claim first", attempts.log())
		}
		storing, complete := attempts.first("storing"), attempts.first("complete")
		if storing == -1 || storing > complete {
			t.Fatalf("calls = %v, want the storing step reported before the completion", attempts.log())
		}
		requireGeneration(t, attempts, "storing", 3)
		requireGeneration(t, attempts, "complete", 3)
		for _, g := range attempts.generations("renew") {
			if g != 3 {
				t.Fatalf("renew carried generation %d, want the claimed generation 3", g)
			}
		}
		progressed := false
		for _, u := range attempts.leaseUpdates() {
			if u.Progress != nil && u.Progress.DownloadedBytes > 0 {
				progressed = true
			}
		}
		if !progressed {
			t.Fatalf("no RenewLease carried progress: %+v", attempts.leaseUpdates())
		}
		for _, worker := range attempts.workerIDs() {
			if worker != "worker-1" {
				t.Fatalf("every ownership call must carry the worker id, got %q", worker)
			}
		}
		if got := attempts.count("fail") + attempts.count("release"); got != 0 {
			t.Fatalf("calls = %v, want no fail and no release", attempts.log())
		}
		if got := bus.published.Load(); got != 0 {
			t.Fatalf("an attempt must not publish lifecycle events itself, published %d", got)
		}
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks after the attempt = %d, want 0", got)
		}
	})
}

func TestRunAttempt_ClaimOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		claimErr error
		wantErr  bool
	}{
		{name: "a task that is not claimable is dropped", claimErr: notClaimableErr()},
		{
			name:     "a deleted task is dropped",
			claimErr: &apperrors.Error{Code: apperrors.ErrCodeNotFound, Message: "Task not found"},
		},
		{name: "an unreachable task service is reported", claimErr: errors.New("database is locked"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				dl := &fakeDownloader{}
				attempts := &fakeAttempts{generation: 1, claimErr: tc.claimErr}
				svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
				svc.RegisterDownloader("HTTP", dl)

				err := svc.RunAttempt(t.Context(), attemptRequest())

				if (err != nil) != tc.wantErr {
					t.Fatalf("RunAttempt() error = %v, wantErr %v", err, tc.wantErr)
				}
				if got := dl.downloads.Load(); got != 0 {
					t.Fatalf("started %d downloads for a task that was not claimed", got)
				}
				if got := attempts.log(); len(got) != 1 {
					t.Fatalf("calls = %v, want only the claim", got)
				}
				if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
					t.Fatalf("active tasks = %d, want 0", got)
				}
			})
		})
	}
}

// brokenInfoDownloader cannot reach its source.
type brokenInfoDownloader struct{ fakeDownloader }

func (d *brokenInfoDownloader) GetFileInfo(context.Context, string, *AuthConfig) (*FileMetadata, error) {
	return nil, errors.New("404 not found")
}

func TestRunAttempt_FailureIsRecordedWithFailAttempt(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sourceType  string
		wantCode    apperrors.Code
		wantMessage string
	}{
		{"a source error", "HTTP", apperrors.ErrCodeInternal, "failed to get file info"},
		{"an unknown source type", "GOPHER", apperrors.ErrCodeInvalidInput, "no downloader for source type GOPHER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				attempts := &fakeAttempts{generation: 4}
				bus := &busSpy{}
				svc := NewService(&fakeStorage{}, bus, WithAttemptCoordinator(attempts, "worker-1"))
				svc.RegisterDownloader("HTTP", &brokenInfoDownloader{})
				req := attemptRequest()
				req.SourceType = tc.sourceType

				if err := svc.RunAttempt(t.Context(), req); err != nil {
					t.Fatalf("RunAttempt() error = %v, want nil: the failure is recorded", err)
				}

				failures := attempts.failureCauses()
				if len(failures) != 1 {
					t.Fatalf("FailAttempt calls = %d, want 1 (calls = %v)", len(failures), attempts.log())
				}
				cause := apperrors.AsError(failures[0])
				if cause == nil || cause.Code != tc.wantCode || cause.Message != tc.wantMessage {
					t.Fatalf("FailAttempt cause = %v, want %s: %s", failures[0], tc.wantCode, tc.wantMessage)
				}
				requireGeneration(t, attempts, "fail", 4)
				if got := attempts.count("complete") + attempts.count("release"); got != 0 {
					t.Fatalf("calls = %v, want no complete and no release", attempts.log())
				}
				if got := bus.published.Load(); got != 0 {
					t.Fatalf("a failed attempt must not publish failed itself, published %d", got)
				}
			})
		})
	}
}

func TestRunAttempt_HeartbeatRenewsTheLeaseWhileDownloading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		dl := &blockingDownloader{started: make(chan struct{}, 1), release: release}
		attempts := &fakeAttempts{generation: 5}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(t.Context(), attemptRequest()) }()
		synctest.Wait()
		if got := attempts.count("renew"); got != 0 {
			t.Fatalf("renewals before the first interval = %d, want 0", got)
		}

		// The source stays silent for three renew intervals: only the timer
		// keeps the lease alive.
		time.Sleep(3*DefaultRenewInterval + time.Second)
		synctest.Wait()
		if got := attempts.count("renew"); got != 3 {
			t.Fatalf("renewals after three intervals = %d, want 3", got)
		}
		requireGeneration(t, attempts, "renew", 5)

		close(release)
		if err := <-done; err != nil {
			t.Fatalf("RunAttempt() error = %v", err)
		}
		if got := attempts.count("complete"); got != 1 {
			t.Fatalf("calls = %v, want one completion", attempts.log())
		}
	})
}

func TestRunAttempt_RejectedHeartbeatStopsWithoutForcingAResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &keyedStorage{objects: map[string]bool{}}
		dl := &blockingDownloader{started: make(chan struct{}, 1), release: make(chan struct{})}
		attempts := &fakeAttempts{generation: 1}
		svc := NewService(store, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(t.Context(), attemptRequest()) }()
		synctest.Wait()

		// Another worker took the task over (or the user paused or cancelled
		// it): every later call of this attempt is rejected.
		attempts.setRenewErr(rejectedErr())
		time.Sleep(DefaultRenewInterval + time.Second)
		synctest.Wait()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("RunAttempt() error = %v, want nil: a fenced attempt just stops", err)
			}
		default:
			t.Fatal("RunAttempt() is still running after its heartbeat was rejected")
		}
		if got := attempts.count("complete") + attempts.count("fail") + attempts.count("release"); got != 0 {
			t.Fatalf("calls = %v: a fenced attempt must not force any outcome", attempts.log())
		}
		if store.has(attemptKey(svc, 1)) {
			t.Fatal("a fenced attempt must not leave an object behind")
		}
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks = %d, want 0", got)
		}
	})
}

func TestRunAttempt_UnreachableRenewalStopsWhenLeaseExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &keyedStorage{objects: map[string]bool{}}
		dl := &blockingDownloader{started: make(chan struct{}, 1), release: make(chan struct{})}
		// Claim grants a 30s lease; renewals then fail transiently (not fenced).
		attempts := &fakeAttempts{generation: 1, leaseExpiresAt: time.Now().Add(30 * time.Second)}
		svc := NewService(store, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(t.Context(), attemptRequest()) }()
		synctest.Wait()

		attempts.setRenewErr(errors.New("task service unreachable"))
		time.Sleep(30*time.Second + DefaultRenewInterval + time.Second)
		synctest.Wait()

		select {
		case <-done:
		default:
			t.Fatal("RunAttempt() still running after its lease expired and renewals kept failing")
		}
		if got := attempts.count("complete") + attempts.count("fail"); got != 0 {
			t.Fatalf("complete+fail calls = %d: a lost lease must neither commit nor fail the task (the reconciler recovers it)", got)
		}
	})
}

func TestRunAttempt_RejectedStoringStepRemovesOnlyItsOwnObject(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &keyedStorage{objects: map[string]bool{}}
		attempts := &fakeAttempts{generation: 2, storingErr: rejectedErr()}
		svc := NewService(store, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", &fakeDownloader{})
		// The attempt that took the task over already stored its object.
		winnerKey := attemptKey(svc, 3)
		store.objects[winnerKey] = true

		if err := svc.RunAttempt(t.Context(), attemptRequest()); err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil: a fenced attempt just stops", err)
		}

		ownKey := attemptKey(svc, 2)
		if got := store.deletedKeys(); len(got) != 1 || got[0] != ownKey {
			t.Fatalf("deleted keys = %v, want only the attempt's own key %q", got, ownKey)
		}
		if !store.has(winnerKey) {
			t.Fatal("the fenced attempt deleted the winner's object")
		}
		if got := attempts.count("complete") + attempts.count("fail") + attempts.count("release"); got != 0 {
			t.Fatalf("calls = %v: a fenced attempt must not force any outcome", attempts.log())
		}
	})
}

func TestRunAttempt_TransientStoringErrorReleasesInsteadOfFailing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &keyedStorage{objects: map[string]bool{}}
		// A transient (non-fence) storing error must not fail a healthy download.
		attempts := &fakeAttempts{generation: 2, storingErr: errors.New("task service unavailable")}
		svc := NewService(store, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", &fakeDownloader{})

		if err := svc.RunAttempt(t.Context(), attemptRequest()); err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil", err)
		}
		if got := attempts.count("fail"); got != 0 {
			t.Fatalf("fail calls = %d: a transient storing error must not fail a healthy download", got)
		}
		if got := attempts.count("release"); got != 1 {
			t.Fatalf("release calls = %d, want 1: the task is handed back, not failed", got)
		}
	})
}

func TestRunAttempt_TaskTimeoutDuringStoringFailsInsteadOfReleasing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &keyedStorage{objects: map[string]bool{}}
		// Storing keeps failing while the task's own deadline expires: a real timeout.
		attempts := &fakeAttempts{generation: 2, storingErr: errors.New("task service unavailable")}
		svc := NewService(store, &busSpy{},
			WithAttemptCoordinator(attempts, "worker-1"),
			WithTaskTimeout(500*time.Millisecond),
		)
		svc.RegisterDownloader("HTTP", &fakeDownloader{})

		_ = svc.RunAttempt(t.Context(), attemptRequest())

		if got := attempts.count("release"); got != 0 {
			t.Fatalf("release calls = %d: a task that exceeded its own deadline must be failed, not handed back", got)
		}
		if got := attempts.count("fail"); got != 1 {
			t.Fatalf("fail calls = %d, want 1", got)
		}
	})
}

func TestRunAttempt_CompletionOutcomes(t *testing.T) {
	lost := errors.New("connection reset before the response")
	for _, tc := range []struct {
		name         string
		completeErrs []error
		wantErr      bool
		wantCalls    int
		wantKept     bool
	}{
		{
			name:         "a lost response is retried with the same result",
			completeErrs: []error{lost},
			wantCalls:    2,
			wantKept:     true,
		},
		{
			name:         "an unknown outcome keeps the object and reports no failure",
			completeErrs: []error{lost, lost, lost},
			wantErr:      true,
			wantCalls:    3,
			wantKept:     true,
		},
		{
			name:         "a rejection on the first try removes the object",
			completeErrs: []error{rejectedErr()},
			wantCalls:    1,
		},
		{
			name:         "a rejection after an unanswered try keeps the object",
			completeErrs: []error{lost, rejectedErr()},
			wantCalls:    2,
			wantKept:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := &keyedStorage{objects: map[string]bool{}}
				dl := &fakeDownloader{}
				attempts := &fakeAttempts{generation: 1, completeErrs: tc.completeErrs}
				svc := NewService(store, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
				svc.RegisterDownloader("HTTP", dl)

				err := svc.RunAttempt(t.Context(), attemptRequest())

				if (err != nil) != tc.wantErr {
					t.Fatalf("RunAttempt() error = %v, wantErr %v", err, tc.wantErr)
				}
				results := attempts.results()
				if len(results) != tc.wantCalls {
					t.Fatalf("CompleteAttempt calls = %d, want %d", len(results), tc.wantCalls)
				}
				for _, result := range results[1:] {
					if !reflect.DeepEqual(result, results[0]) {
						t.Fatalf("a completion retry must resend the same result: %+v vs %+v", result, results[0])
					}
				}
				if got := dl.downloads.Load(); got != 1 {
					t.Fatalf("downloads = %d, want 1: a completion retry must not download again", got)
				}
				if got := store.has(attemptKey(svc, 1)); got != tc.wantKept {
					t.Fatalf("object kept = %v, want %v", got, tc.wantKept)
				}
				if got := attempts.count("fail") + attempts.count("release"); got != 0 {
					t.Fatalf("calls = %v: a stored attempt must not be failed or released", attempts.log())
				}
			})
		})
	}
}

func TestRunAttempt_PauseStopsTheAttemptWithoutReportingFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dl := &blockingDownloader{started: make(chan struct{}, 1), release: make(chan struct{})}
		attempts := &fakeAttempts{generation: 1}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(t.Context(), attemptRequest()) }()
		synctest.Wait()

		// The task service already moved the task to PAUSED and retired the
		// attempt; the worker only has to stop.
		if err := svc.PauseTask(t.Context(), 9); err != nil {
			t.Fatalf("PauseTask() error = %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil", err)
		}

		if got := attempts.count("complete") + attempts.count("fail") + attempts.count("release"); got != 0 {
			t.Fatalf("calls = %v: a paused attempt must not report any outcome", attempts.log())
		}
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks after pause = %d, want 0", got)
		}
	})
}

func TestRunAttempt_PauseInterruptsABlockedSourceRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// A source whose Read blocks and ignores ctx: only closing the reader
		// unblocks it. This is the case cmd/pocket faces with FTP and
		// BitTorrent. On pause the transfer must still stop, free its slot and
		// report no outcome; otherwise it holds the slot and the heartbeat
		// renews the lease forever, so the reconciler can never recover it.
		reader := newReadBlockingReader()
		dl := &readBlockingDownloader{reader: reader}
		attempts := &fakeAttempts{generation: 1}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		req := attemptRequest()
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(t.Context(), req) }()
		synctest.Wait()

		// The worker is now blocked inside Store, reading the stuck source.
		if got := dl.downloads.Load(); got != 1 {
			t.Fatalf("downloads = %d, want 1 (the attempt should be mid-transfer)", got)
		}

		if err := svc.PauseTask(t.Context(), req.TaskID); err != nil {
			t.Fatalf("PauseTask() error = %v", err)
		}
		// On the fixed code the pause closes the reader, so Read returns and
		// RunAttempt unwinds. On the unfixed code this receive deadlocks: the
		// Read never returns, which synctest reports as a stuck bubble.
		if err := <-done; err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil", err)
		}

		if !reader.closedFlag.Load() {
			t.Fatal("the source reader was never closed on pause")
		}
		if got := attempts.count("complete") + attempts.count("fail") + attempts.count("release"); got != 0 {
			t.Fatalf("calls = %v: a paused attempt must not report any outcome", attempts.log())
		}
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks after pause = %d, want 0", got)
		}
	})
}

func TestRunAttempt_StopReturnsEvenIfReaderCloseHangs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The source Read unblocks when the reader is closed, but Close itself
		// then hangs (an FTP Quit waiting on a silent control connection). The
		// attempt must still unwind: free its slot and stop renewing the lease
		// without waiting for Close. Otherwise a silent FTP server pins the
		// slot and the heartbeat renews the lease forever.
		reader := newHangingCloseReader()
		dl := &hangingCloseDownloader{reader: reader}
		attempts := &fakeAttempts{generation: 1}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		req := attemptRequest()
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(t.Context(), req) }()
		synctest.Wait()

		if got := dl.downloads.Load(); got != 1 {
			t.Fatalf("downloads = %d, want 1 (the attempt should be mid-transfer)", got)
		}

		if err := svc.PauseTask(t.Context(), req.TaskID); err != nil {
			t.Fatalf("PauseTask() error = %v", err)
		}
		// On the fixed code RunAttempt returns while Close is still blocked. On
		// the superseded sync.Once version the deferred close blocked on the
		// watcher's in-flight Close, so this receive deadlocked.
		if err := <-done; err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil", err)
		}
		if !reader.closeStarted.Load() {
			t.Fatal("Close was never called on the stuck reader")
		}
		renewsAtReturn := attempts.count("renew")
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks after stop = %d, want 0", got)
		}

		// Let the hung Close finish so the watcher goroutine exits and the
		// synctest bubble drains; no further renewal may happen after return.
		reader.releaseQuit()
		synctest.Wait()
		if got := attempts.count("renew"); got != renewsAtReturn {
			t.Fatalf("lease renewed %d more time(s) after the attempt returned", got-renewsAtReturn)
		}
		if got := attempts.count("complete") + attempts.count("fail") + attempts.count("release"); got != 0 {
			t.Fatalf("calls = %v: a stopped attempt must not report any outcome", attempts.log())
		}
	})
}

func TestRunAttempt_WorkerShutdownReleasesInsteadOfFailing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dl := &blockingDownloader{started: make(chan struct{}, 1), release: make(chan struct{})}
		attempts := &fakeAttempts{generation: 6}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(ctx, attemptRequest()) }()
		synctest.Wait()

		cancel() // the worker process shuts down
		if err := <-done; err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil: the release was recorded", err)
		}

		if got := attempts.count("release"); got != 1 {
			t.Fatalf("calls = %v, want one release", attempts.log())
		}
		requireGeneration(t, attempts, "release", 6)
		if got := attempts.count("fail") + attempts.count("complete"); got != 0 {
			t.Fatalf("calls = %v: a shutdown must not spend a retry or force a result", attempts.log())
		}
	})
}

func TestRunAttempt_QueuedTaskIsNotClaimedAndDuplicatesAreDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		dl := &blockingDownloader{started: make(chan struct{}, 2), release: release}
		attempts := &fakeAttempts{generation: 1}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"), WithMaxConcurrent(1))
		svc.RegisterDownloader("HTTP", dl)

		running := attemptRequest()
		queued := attemptRequest()
		queued.TaskID = 10
		done := make(chan error, 2)
		go func() { done <- svc.RunAttempt(t.Context(), running) }()
		synctest.Wait()
		go func() { done <- svc.RunAttempt(t.Context(), queued) }()
		synctest.Wait()

		if got := attempts.count("claim"); got != 1 {
			t.Fatalf("claims = %d, want 1: a task waiting for a slot must stay unclaimed", got)
		}
		// The reconciler may dispatch both tasks again while this worker
		// still holds them.
		for _, req := range []TaskRequest{running, queued} {
			if err := svc.RunAttempt(t.Context(), req); err != nil {
				t.Fatalf("duplicate RunAttempt(%d) error = %v, want nil (dropped)", req.TaskID, err)
			}
		}
		if got := attempts.count("claim"); got != 1 {
			t.Fatalf("claims after the duplicates = %d, want 1", got)
		}

		close(release)
		for range 2 {
			if err := <-done; err != nil {
				t.Fatalf("RunAttempt() error = %v", err)
			}
		}
		if got := attempts.count("claim"); got != 2 {
			t.Fatalf("claims = %d, want 2: the queued task claims once it has a slot", got)
		}
		if got := attempts.count("complete"); got != 2 {
			t.Fatalf("calls = %v, want two completions", attempts.log())
		}
	})
}

// sizedStorage reads exactly the announced number of bytes, like a backend
// that uploads with a known content length. It never reads io.EOF.
type sizedStorage struct{ fakeStorage }

func (s *sizedStorage) Store(_ context.Context, _ string, reader io.Reader, metadata *storage.FileMetadata) error {
	_, err := io.CopyN(io.Discard, reader, metadata.FileSize)
	return err
}

func TestRunAttempt_StoringStepIsReportedEvenWhenTheBackendNeverReadsEOF(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := &fakeAttempts{generation: 1}
		svc := NewService(&sizedStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", &fakeDownloader{})

		if err := svc.RunAttempt(t.Context(), attemptRequest()); err != nil {
			t.Fatalf("RunAttempt() error = %v", err)
		}

		storing, complete := attempts.first("storing"), attempts.first("complete")
		if storing == -1 || complete == -1 || storing > complete {
			t.Fatalf("calls = %v, want the storing step reported before the completion", attempts.log())
		}
	})
}

func TestRunAttempt_WithoutAttemptAPIIsAnError(t *testing.T) {
	svc := NewService(&fakeStorage{}, &busSpy{})
	svc.RegisterDownloader("HTTP", &fakeDownloader{})

	err := svc.RunAttempt(t.Context(), attemptRequest())

	if !apperrors.IsError(err, apperrors.ErrCodeInternal) {
		t.Fatalf("RunAttempt() error = %v, want INTERNAL", err)
	}
}

func TestRunAttempt_RejectedFailAttemptIsNotAnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := &fakeAttempts{generation: 4, failErr: rejectedErr()}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", &brokenInfoDownloader{})

		if err := svc.RunAttempt(t.Context(), attemptRequest()); err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil: a fenced attempt just stops", err)
		}

		if got := attempts.count("fail"); got != 1 {
			t.Fatalf("calls = %v, want exactly one fail", attempts.log())
		}
		requireGeneration(t, attempts, "fail", 4)
		if got := attempts.count("complete") + attempts.count("release"); got != 0 {
			t.Fatalf("calls = %v: a rejected failure must not force another outcome", attempts.log())
		}
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks = %d, want 0", got)
		}
	})
}

func TestRunAttempt_RejectedReleaseIsNotAnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dl := &blockingDownloader{started: make(chan struct{}, 1), release: make(chan struct{})}
		attempts := &fakeAttempts{generation: 6, releaseErr: rejectedErr()}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"))
		svc.RegisterDownloader("HTTP", dl)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- svc.RunAttempt(ctx, attemptRequest()) }()
		synctest.Wait()

		cancel() // the worker process shuts down
		if err := <-done; err != nil {
			t.Fatalf("RunAttempt() error = %v, want nil: a fenced attempt just stops", err)
		}

		if got := attempts.count("release"); got != 1 {
			t.Fatalf("calls = %v, want exactly one release", attempts.log())
		}
		requireGeneration(t, attempts, "release", 6)
		if got := attempts.count("fail") + attempts.count("complete"); got != 0 {
			t.Fatalf("calls = %v: a rejected release must not force another outcome", attempts.log())
		}
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks = %d, want 0", got)
		}
	})
}

func TestRunAdmittedAttempt_ReleasesTheSlotTheCallerAdmitted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := &fakeAttempts{generation: 1}
		svc := NewService(&fakeStorage{}, &busSpy{}, WithAttemptCoordinator(attempts, "worker-1"), WithMaxConcurrent(1))
		svc.RegisterDownloader("HTTP", &blockingDownloader{started: make(chan struct{}, 1), release: closedChan()})

		if err := svc.Admit(t.Context()); err != nil {
			t.Fatalf("Admit() error = %v", err)
		}
		// The only slot is held: a second Admit waits.
		second := make(chan error, 1)
		go func() { second <- svc.Admit(t.Context()) }()
		synctest.Wait()
		select {
		case <-second:
			t.Fatal("Admit returned while the only slot was held")
		default:
		}

		// RunAdmittedAttempt must not acquire again, and frees the slot.
		if err := svc.RunAdmittedAttempt(t.Context(), attemptRequest()); err != nil {
			t.Fatalf("RunAdmittedAttempt() error = %v", err)
		}
		if err := <-second; err != nil {
			t.Fatalf("second Admit() error = %v", err)
		}
		if got := attempts.count("complete"); got != 1 {
			t.Fatalf("calls = %v, want one completion", attempts.log())
		}
	})
}

func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}
