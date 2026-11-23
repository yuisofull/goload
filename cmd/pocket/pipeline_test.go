package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-llsqlite/crawshaw/sqlitex"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
	downloadtransport "github.com/yuisofull/goload/internal/download/transport"
	"github.com/yuisofull/goload/internal/observability"
	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/internal/task"
	tasksqlite "github.com/yuisofull/goload/internal/task/sqlite"
	tasktransport "github.com/yuisofull/goload/internal/task/transport"
	"github.com/yuisofull/goload/pkg/message"
	"github.com/yuisofull/goload/pkg/message/inmem"
)

// scriptedDownloader serves a fixed body. Its first failInfo GetFileInfo calls
// fail, which fails those attempts as a whole before any download starts.
//
// With hold set it is blockable: Download signals entered, then waits until
// hold is closed (it proceeds) or its context ends (it returns the context
// error), so a test can keep a worker mid-download.
type scriptedDownloader struct {
	body     string
	failInfo atomic.Int32
	starts   atomic.Int32

	hold    chan struct{}
	entered chan struct{}
}

// newBlockableDownloader returns a downloader whose Download blocks until the
// test closes hold.
func newBlockableDownloader(body string) *scriptedDownloader {
	return &scriptedDownloader{
		body:    body,
		hold:    make(chan struct{}),
		entered: make(chan struct{}, 1),
	}
}

func (d *scriptedDownloader) GetFileInfo(
	context.Context,
	string,
	*download.AuthConfig,
) (*download.FileMetadata, error) {
	if d.failInfo.Add(-1) >= 0 {
		return nil, errors.New("source unavailable")
	}
	return &download.FileMetadata{
		FileName:    "file.bin",
		FileSize:    int64(len(d.body)),
		ContentType: "application/octet-stream",
	}, nil
}

func (d *scriptedDownloader) Download(
	ctx context.Context,
	_ string,
	_ *download.AuthConfig,
	_ download.DownloadOptions,
) (io.ReadCloser, int64, error) {
	d.starts.Add(1)
	if d.hold != nil {
		select {
		case d.entered <- struct{}{}:
		default:
		}
		select {
		case <-d.hold:
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	return io.NopCloser(strings.NewReader(d.body)), int64(len(d.body)), nil
}

func (d *scriptedDownloader) SupportsResume() bool { return false }

// pocketPipeline is the background half of pocket mode, wired the way main
// wires it: SQLite, the in-memory bus, the outbox relay, the notification-only
// lifecycle consumer, the attempt-aware download worker and the reconciler.
// Only the intervals are shortened.
type pocketPipeline struct {
	tasks    task.Service
	attempts task.AttemptRepository
	outbox   task.OutboxRepository
	store    *storage.Local

	// Crash-recovery harness.
	pool      *sqlitex.Pool
	pub       message.Publisher
	reconcile task.ReconcileRepository
	// rawAttempts is the real ownership API, never gated.
	rawAttempts *task.AttemptService
	// gate sits between the worker and rawAttempts when pipelineOpts.gated.
	gate     *attemptGate
	dataDir  string
	workerID string

	stopWorker  context.CancelFunc
	stopControl context.CancelFunc
	running     sync.WaitGroup // worker goroutines
	control     sync.WaitGroup // lifecycle consumer, relay, reconciler
}

// pipelineOpts customises a pipeline for the crash-recovery tests. The zero
// value is the plain pocket pipeline on a fresh temp database and the real
// clock.
type pipelineOpts struct {
	// dbPath and dataDir let two pipelines share one SQLite file and one
	// object store, which is what crash + restart means.
	dbPath, dataDir string
	workerID        string
	// clock and lease make lease expiry deterministic. Nil clock is the real
	// one; zero lease is the default.
	clock func() time.Time
	lease time.Duration
	// noReconciler leaves the reconciler out.
	noReconciler bool
	// gated routes the worker's ownership calls through an attemptGate.
	gated bool
	// taskMetrics and dlMetrics inject metrics recorders into the task and
	// download services for the observability acceptance test.
	taskMetrics task.Metrics
	dlMetrics   download.Metrics
	tracer      *observability.Tracer
}

func startPocketPipeline(t *testing.T, dl download.Downloader) *pocketPipeline {
	t.Helper()
	return startPipeline(t, dl, pipelineOpts{})
}

func startPipeline(t *testing.T, dl download.Downloader, o pipelineOpts) *pocketPipeline {
	t.Helper()

	if o.dbPath == "" {
		o.dbPath = filepath.Join(t.TempDir(), "pocket_pipeline.db")
	}
	if o.dataDir == "" {
		o.dataDir = filepath.Join(t.TempDir(), "data")
	}
	if o.workerID == "" {
		o.workerID = "worker-test"
	}

	pool, err := sqlitex.Open(o.dbPath, 0, 10)
	require.NoError(t, err)
	require.NoError(t, runMigrations(pool))
	store, err := storage.NewLocalBackend(o.dataDir)
	require.NoError(t, err)

	logger := log.NewNopLogger()
	pub, sub := inmem.NewPublisherAndSubscriber(logger)

	taskRepo := tasksqlite.NewTaskRepo(pool)
	tx := tasksqlite.NewTxManager(pool)
	outboxRepo := tasksqlite.NewOutboxRepo(pool)
	attemptRepo := tasksqlite.NewAttemptRepo(pool)
	reconcileRepo := tasksqlite.NewReconcileRepo(pool)

	attemptOpts := []task.AttemptServiceOption{
		task.WithRetryBackoff(func(int) time.Duration { return 20 * time.Millisecond }),
	}
	reconcilerOpts := []task.ReconcilerOption{task.WithReconcileInterval(20 * time.Millisecond)}
	if o.clock != nil {
		attemptOpts = append(attemptOpts, task.WithAttemptClock(o.clock))
		reconcilerOpts = append(reconcilerOpts, task.WithReconcilerClock(o.clock))
	}
	if o.lease > 0 {
		attemptOpts = append(attemptOpts, task.WithLeaseDuration(o.lease))
	}
	if o.taskMetrics != nil {
		attemptOpts = append(attemptOpts, task.WithMetrics(o.taskMetrics))
	}

	// Mirror the pocket binary: the in-memory bus drops a message with no
	// subscriber, so outbox delivery must wait for the download consumer.
	taskPub := task.NewEventPublisher(pub, task.WithOutboxTransport(task.NewRequireSubscriberTransport(pub)))
	relayOpts := []task.OutboxRelayOption{task.WithOutboxPollInterval(10 * time.Millisecond)}
	var svcOpts []task.ServiceOption
	if o.tracer != nil {
		relayOpts = append(relayOpts, task.WithOutboxRelayTracing(o.tracer))
		svcOpts = append(svcOpts, task.WithTracing(o.tracer))
		attemptOpts = append(attemptOpts, task.WithAttemptTracing(o.tracer))
	}
	relay := task.NewOutboxRelay(outboxRepo, taskPub, relayOpts...)
	taskSvc := task.NewService(taskRepo, *taskPub, tx, outboxRepo, attemptRepo, svcOpts...)
	attemptSvc := task.NewAttemptService(attemptRepo, tx, outboxRepo, attemptOpts...)
	reconciler := task.NewReconciler(reconcileRepo, taskRepo, outboxRepo, reconcilerOpts...)
	lifecycle := tasktransport.NewEventConsumer(taskSvc, sub, func(context.Context, error) {})

	p := &pocketPipeline{
		tasks:       taskSvc,
		attempts:    attemptRepo,
		outbox:      outboxRepo,
		store:       store,
		pool:        pool,
		pub:         pub,
		reconcile:   reconcileRepo,
		rawAttempts: attemptSvc,
		dataDir:     o.dataDir,
		workerID:    o.workerID,
	}
	var workerAttempts download.AttemptCoordinator = attemptSvc
	if o.gated {
		p.gate = &attemptGate{inner: workerAttempts}
		workerAttempts = p.gate
	}

	dlOpts := []download.Option{
		download.WithStorageType(storage.TypeLocal),
		download.WithAttemptCoordinator(workerAttempts, o.workerID),
	}
	if o.clock != nil {
		// The lease watchdog must read the same clock the ownership API stamps
		// leases with, or it would misread a fake-clock deadline.
		dlOpts = append(dlOpts, download.WithAttemptClock(o.clock))
	}
	if o.dlMetrics != nil {
		dlOpts = append(dlOpts, download.WithMetrics(o.dlMetrics))
	}
	var workerOpts []downloadtransport.Option
	if o.tracer != nil {
		dlOpts = append(dlOpts, download.WithTracing(o.tracer))
		workerOpts = append(workerOpts, downloadtransport.WithTracing(o.tracer))
	}
	dlSvc := download.NewService(store, download.NewDownloadEventPublisher(pub), dlOpts...)
	dlSvc.RegisterDownloader("HTTPS", dl)
	workerOpts = append(workerOpts, downloadtransport.WithAttemptRunner(dlSvc))
	worker := downloadtransport.NewEventConsumer(dlSvc, sub, logger, workerOpts...)

	workerCtx, stopWorker := context.WithCancel(context.Background())
	controlCtx, stopControl := context.WithCancel(context.Background())
	p.stopWorker, p.stopControl = stopWorker, stopControl

	controlRuns := []func(context.Context) error{lifecycle.Start, relay.Run}
	if !o.noReconciler {
		controlRuns = append(controlRuns, reconciler.Run)
	}
	for _, run := range controlRuns {
		p.control.Add(1)
		go func() {
			defer p.control.Done()
			_ = run(controlCtx)
		}()
	}
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		_ = worker.Start(workerCtx)
	}()

	t.Cleanup(func() {
		p.stopAll()
		_ = store.Close()
		_ = pool.Close()
	})
	return p
}

// stopAll cancels every goroutine of the pipeline and waits for them. It is
// safe to call more than once.
func (p *pocketPipeline) stopAll() {
	p.stopWorker()
	p.stopControl()
	p.running.Wait()
	p.control.Wait()
}

// create stores a task the way the HTTP API does.
func (p *pocketPipeline) create(t *testing.T) uint64 {
	t.Helper()
	return p.createIn(t, context.Background())
}

// createIn stores a task within ctx, e.g. inside a request span.
func (p *pocketPipeline) createIn(t *testing.T, ctx context.Context) uint64 {
	t.Helper()

	created, err := p.tasks.CreateTask(ctx, &task.CreateTaskParam{
		OfAccountID: 7,
		FileName:    "file.bin",
		SourceURL:   "https://example.test/file.bin",
	})
	require.NoError(t, err)
	return created.ID
}

func (p *pocketPipeline) state(t *testing.T, taskID uint64) *task.AttemptState {
	t.Helper()

	st, err := p.attempts.GetAttemptState(context.Background(), taskID)
	require.NoError(t, err)
	return st
}

// waitFor waits until the task reaches status and returns its state.
func (p *pocketPipeline) waitFor(t *testing.T, taskID uint64, status task.TaskStatus) *task.AttemptState {
	t.Helper()

	require.Eventually(t, func() bool {
		st, err := p.attempts.GetAttemptState(context.Background(), taskID)
		return err == nil && st.Status == status
	}, 10*time.Second, 10*time.Millisecond, "task %d never reached %s", taskID, status)
	return p.state(t, taskID)
}

// Happy path: dispatch -> claim -> complete -> lifecycle completion event
// published through the relay.
func TestPocketPipeline_DispatchClaimCompleteAndNotify(t *testing.T) {
	dl := &scriptedDownloader{body: "pocket payload"}
	p := startPocketPipeline(t, dl)
	ctx := context.Background()

	id := p.create(t)
	done := p.waitFor(t, id, task.StatusCompleted)

	require.Equal(t, uint64(1), done.Generation, "the first claim is generation 1")
	require.Empty(t, done.OwnerID)
	require.Nil(t, done.LeaseExpiresAt)
	require.Zero(t, done.RetryCount)
	require.Equal(t, storage.TypeLocal, done.StorageType)
	require.True(t, strings.HasPrefix(done.StoragePath, strconv.FormatUint(id, 10)+"/1/file-"),
		"the result is the attempt's own generation-scoped key, got %q", done.StoragePath)
	sum := md5.Sum([]byte(dl.body))
	wantChecksum := &task.ChecksumInfo{ChecksumType: "md5", ChecksumValue: hex.EncodeToString(sum[:])}
	require.Equal(t, wantChecksum, done.Checksum)
	require.Equal(t, task.DownloadProgress{Progress: 100, DownloadedBytes: 14, TotalBytes: 14}, done.Progress)
	require.Equal(t, int32(1), dl.starts.Load())

	stored, err := p.store.Get(ctx, done.StoragePath)
	require.NoError(t, err)
	body, err := io.ReadAll(stored)
	require.NoError(t, stored.Close())
	require.NoError(t, err)
	require.Equal(t, dl.body, string(body))

	// CompleteAttempt wrote the task.completed row with the result. The relay
	// publishes it because the lifecycle consumer is subscribed, and the
	// consumer only acknowledges it: the committed row stays as it is.
	require.Eventually(t, func() bool {
		unsent, err := p.outbox.FetchUnsent(ctx, task.OutboxCursor{}, 10)
		return err == nil && len(unsent) == 0
	}, 5*time.Second, 10*time.Millisecond, "the outbox never drained")
	require.Equal(t, done, p.state(t, id))
}

// A failed attempt goes back to PENDING with a retry time. Nothing but the
// reconciler dispatches it again.
func TestPocketPipeline_FailedAttemptIsRetriedByTheReconciler(t *testing.T) {
	dl := &scriptedDownloader{body: "second time lucky"}
	dl.failInfo.Store(1)
	p := startPocketPipeline(t, dl)

	id := p.create(t)
	done := p.waitFor(t, id, task.StatusCompleted)

	require.Equal(t, uint64(2), done.Generation, "the retry is a new claim")
	require.Equal(t, 1, done.RetryCount)
	require.Nil(t, done.ErrorMessage, "the completion clears the failure message")
	require.True(t, strings.HasPrefix(done.StoragePath, strconv.FormatUint(id, 10)+"/2/file-"),
		"the result is the second attempt's key, got %q", done.StoragePath)
	require.Equal(t, int32(1), dl.starts.Load(), "the failed attempt never started a download")
}
