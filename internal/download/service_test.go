package download

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
)

func TestGenerateStorageKeyPrefersResolvedFileNameExtension(t *testing.T) {
	svc := &service{}
	key := svc.generateStorageKey(TaskRequest{
		TaskID:    1,
		FileName:  "cjrCRgMpxacNCRvmjfgpfwGR",
		SourceURL: "https://example.com/download/cjrCRgMpxacNCRvmjfgpfwGR?token=abc",
	}, "design_rationale_example_1.pdf")

	if !strings.HasPrefix(key, "1/design_rationale_example_1-") {
		t.Fatalf("expected key to use resolved filename, got %q", key)
	}
	if filepath.Ext(key) != ".pdf" {
		t.Fatalf("expected key to preserve .pdf extension, got %q", key)
	}
}

func TestGenerateStorageKeyFallsBackToURLPathWithoutQuery(t *testing.T) {
	svc := &service{}
	key := svc.generateStorageKey(TaskRequest{
		TaskID:    7,
		SourceURL: "https://example.com/files/archive.tar.gz?signature=abc",
	}, "")

	if !strings.HasPrefix(key, "7/archive.tar-") {
		t.Fatalf("expected key to use URL path filename, got %q", key)
	}
	if filepath.Ext(key) != ".gz" {
		t.Fatalf("expected key to preserve .gz extension, got %q", key)
	}
}

type fakeDownloader struct {
	downloads atomic.Int32
}

func (d *fakeDownloader) GetFileInfo(ctx context.Context, rawURL string, auth *AuthConfig) (*FileMetadata, error) {
	return &FileMetadata{
		FileName:    "file.txt",
		FileSize:    int64(len("content")),
		ContentType: "text/plain",
	}, nil
}

func (d *fakeDownloader) Download(
	ctx context.Context,
	rawURL string,
	auth *AuthConfig,
	opts DownloadOptions,
) (io.ReadCloser, int64, error) {
	d.downloads.Add(1)
	return io.NopCloser(strings.NewReader("content")), int64(len("content")), nil
}

func (d *fakeDownloader) SupportsResume() bool { return false }

type fakeStorage struct{}

func (s *fakeStorage) Store(ctx context.Context, key string, reader io.Reader, metadata *storage.FileMetadata) error {
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return err
	}
	return nil
}

func (s *fakeStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return nil, nil
}

func (s *fakeStorage) GetWithRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error) {
	return nil, nil
}

func (s *fakeStorage) GetInfo(ctx context.Context, key string) (*storage.FileMetadata, error) {
	return nil, nil
}

func (s *fakeStorage) Exists(ctx context.Context, key string) (bool, error) { return false, nil }
func (s *fakeStorage) Delete(ctx context.Context, key string) error         { return nil }

type fakePublisher struct {
	mu        sync.Mutex
	completed *events.TaskCompletedEvent
	failed    *events.TaskFailedEvent
	statusErr error
}

func (p *fakePublisher) PublishTaskStatusUpdated(ctx context.Context, event events.TaskStatusUpdatedEvent) error {
	return p.statusErr
}

func (p *fakePublisher) PublishTaskProgressUpdated(ctx context.Context, event events.TaskProgressUpdatedEvent) error {
	return nil
}

func (p *fakePublisher) PublishTaskCompleted(ctx context.Context, event events.TaskCompletedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.completed = &event
	return nil
}

func (p *fakePublisher) PublishTaskFailed(ctx context.Context, event events.TaskFailedEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failed = &event
	return nil
}

func TestExecuteTaskAttemptsDownloadWhenMaxRetriesZero(t *testing.T) {
	synctest.Test(t, testExecuteTaskAttemptsDownloadWhenMaxRetriesZero)
}

func testExecuteTaskAttemptsDownloadWhenMaxRetriesZero(t *testing.T) {
	store := &fakeStorage{}
	pub := &fakePublisher{}
	dl := &fakeDownloader{}
	svc := NewService(store, pub, WithStorageType(storage.TypeMinio))
	svc.RegisterDownloader("HTTP", dl)

	err := svc.ExecuteTask(context.Background(), TaskRequest{
		TaskID:     11,
		SourceURL:  "https://example.com/file.txt",
		SourceType: "HTTP",
		DownloadOptions: &DownloadOptions{
			MaxRetries: 0,
		},
	})
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}
	synctest.Wait()
	if dl.downloads.Load() != 1 {
		t.Fatalf("expected one download attempt, got %d", dl.downloads.Load())
	}
	if pub.completed == nil {
		t.Fatal("expected completion event")
	}
	if pub.completed.StorageType != storage.TypeMinio.String() {
		t.Fatalf("expected storage type %q, got %q", storage.TypeMinio, pub.completed.StorageType)
	}
}

type blockingDownloader struct {
	fakeDownloader
	started chan struct{}
	release <-chan struct{}
}

func (d *blockingDownloader) Download(
	ctx context.Context,
	rawURL string,
	auth *AuthConfig,
	opts DownloadOptions,
) (io.ReadCloser, int64, error) {
	d.started <- struct{}{}
	select {
	case <-d.release:
		return d.fakeDownloader.Download(ctx, rawURL, auth, opts)
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

func TestExecuteTaskConcurrencyLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
	}{
		{name: "one at a time", limit: 1},
		{name: "concurrent downloads", limit: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				release := make(chan struct{})
				dl := &blockingDownloader{started: make(chan struct{}, tc.limit+1), release: release}
				svc := NewService(&fakeStorage{}, &fakePublisher{}, WithMaxConcurrent(tc.limit))
				svc.RegisterDownloader("HTTP", dl)

				for taskID := 1; taskID <= tc.limit; taskID++ {
					if err := svc.ExecuteTask(ctx, TaskRequest{TaskID: uint64(taskID), SourceType: "HTTP"}); err != nil {
						t.Fatalf("ExecuteTask() error = %v", err)
					}
				}
				synctest.Wait()
				if got := len(dl.started); got != tc.limit {
					t.Fatalf("started %d downloads, want %d", got, tc.limit)
				}

				// A duplicate must be rejected even while all download slots are busy.
				err := svc.ExecuteTask(ctx, TaskRequest{TaskID: 1, SourceType: "HTTP"})
				if !apperrors.IsError(err, apperrors.ErrCodeConflict) {
					t.Fatalf("duplicate ExecuteTask() error = %v, want conflict", err)
				}

				accepted := make(chan error, 1)
				go func() {
					req := TaskRequest{TaskID: uint64(tc.limit + 1), SourceType: "HTTP"}
					accepted <- svc.ExecuteTask(ctx, req)
				}()
				synctest.Wait()
				select {
				case err := <-accepted:
					t.Fatalf("ExecuteTask() returned while all slots were busy: %v", err)
				default:
				}
				if got := len(dl.started); got != tc.limit {
					t.Fatalf("concurrency limit exceeded: started %d downloads", got)
				}

				release <- struct{}{}
				synctest.Wait()
				if err := <-accepted; err != nil {
					t.Fatalf("waiting ExecuteTask() error = %v", err)
				}
				if got := len(dl.started); got != tc.limit+1 {
					t.Fatalf("started %d downloads after freeing a slot, want %d", got, tc.limit+1)
				}

				close(release)
				synctest.Wait()
				if got := svc.GetActiveTaskCount(ctx); got != 0 {
					t.Fatalf("active tasks after completion = %d, want 0", got)
				}
			})
		})
	}
}

func TestExecuteTaskCancellationWhileWaitingForSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		release := make(chan struct{})
		dl := &blockingDownloader{started: make(chan struct{}, 2), release: release}
		svc := NewService(&fakeStorage{}, &fakePublisher{}, WithMaxConcurrent(1))
		svc.RegisterDownloader("HTTP", dl)
		if err := svc.ExecuteTask(ctx, TaskRequest{TaskID: 1, SourceType: "HTTP"}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		waitingCtx, cancelWaiting := context.WithCancel(ctx)
		defer cancelWaiting()
		result := make(chan error, 1)
		go func() {
			result <- svc.ExecuteTask(waitingCtx, TaskRequest{TaskID: 2, SourceType: "HTTP"})
		}()
		synctest.Wait()
		cancelWaiting()
		synctest.Wait()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("ExecuteTask() error = %v, want context cancellation", err)
		}
		if got := svc.GetActiveTaskCount(ctx); got != 1 {
			t.Fatalf("active tasks after cancelling waiting task = %d, want 1", got)
		}
		if got := len(dl.started); got != 1 {
			t.Fatalf("started %d downloads, want 1", got)
		}
		close(release)
		synctest.Wait()
		if got := svc.GetActiveTaskCount(ctx); got != 0 {
			t.Fatalf("active tasks after completion = %d, want 0", got)
		}
	})
}

func TestExecuteTaskReportsAsynchronousFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pub := &fakePublisher{statusErr: errors.New("publisher unavailable")}
		reported := make(chan error, 1)
		svc := NewService(&fakeStorage{}, pub,
			WithMaxConcurrent(1),
			WithErrorHandler(func(_ context.Context, err error) { reported <- err }),
		)
		svc.RegisterDownloader("HTTP", &fakeDownloader{})
		if err := svc.ExecuteTask(t.Context(), TaskRequest{TaskID: 1, SourceType: "HTTP"}); err != nil {
			t.Fatalf("ExecuteTask() error = %v", err)
		}
		synctest.Wait()
		if pub.failed == nil || pub.failed.TaskID != 1 {
			t.Fatalf("expected task failure event, got %v", pub.failed)
		}
		if len(reported) == 0 {
			t.Fatal("expected error handler to report asynchronous failure")
		}
		if got := svc.GetActiveTaskCount(t.Context()); got != 0 {
			t.Fatalf("active tasks after failure = %d, want 0", got)
		}

		// A failed task must release its slot for the next download.
		pub.statusErr = nil
		if err := svc.ExecuteTask(t.Context(), TaskRequest{TaskID: 2, SourceType: "HTTP"}); err != nil {
			t.Fatalf("ExecuteTask() after failure error = %v", err)
		}
		synctest.Wait()
		if pub.completed == nil || pub.completed.TaskID != 2 {
			t.Fatalf("expected next task to complete, got %v", pub.completed)
		}
	})
}

func TestPausableProgressReader_ConcurrentPauseAndRead(t *testing.T) {
	reader := NewPausableProgressReader(strings.NewReader(strings.Repeat("x", 1<<20)), func(int64) {})
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			_, err := reader.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	for range 1000 {
		reader.Pause()
		reader.Resume()
	}
	reader.Resume()
	<-done
}
