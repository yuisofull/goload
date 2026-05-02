package download

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

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
	downloads int
}

func (d *fakeDownloader) GetFileInfo(ctx context.Context, rawURL string, auth *AuthConfig) (*FileMetadata, error) {
	return &FileMetadata{
		FileName:    "file.txt",
		FileSize:    int64(len("content")),
		ContentType: "text/plain",
	}, nil
}

func (d *fakeDownloader) Download(ctx context.Context, rawURL string, auth *AuthConfig, opts DownloadOptions) (io.ReadCloser, int64, error) {
	d.downloads++
	return io.NopCloser(strings.NewReader("content")), int64(len("content")), nil
}

func (d *fakeDownloader) SupportsResume() bool { return false }

type fakeStorage struct {
	metadata *storage.FileMetadata
}

func (s *fakeStorage) Store(ctx context.Context, key string, reader io.Reader, metadata *storage.FileMetadata) error {
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return err
	}
	s.metadata = metadata
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
	completed *events.TaskCompletedEvent
}

func (p *fakePublisher) PublishTaskStatusUpdated(ctx context.Context, event events.TaskStatusUpdatedEvent) error {
	return nil
}

func (p *fakePublisher) PublishTaskProgressUpdated(ctx context.Context, event events.TaskProgressUpdatedEvent) error {
	return nil
}

func (p *fakePublisher) PublishTaskCompleted(ctx context.Context, event events.TaskCompletedEvent) error {
	p.completed = &event
	return nil
}

func (p *fakePublisher) PublishTaskFailed(ctx context.Context, event events.TaskFailedEvent) error {
	return nil
}

func TestExecuteTaskAttemptsDownloadWhenMaxRetriesZero(t *testing.T) {
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
	if dl.downloads != 1 {
		t.Fatalf("expected one download attempt, got %d", dl.downloads)
	}
	if pub.completed == nil {
		t.Fatal("expected completion event")
	}
	if pub.completed.StorageType != storage.TypeMinio.String() {
		t.Fatalf("expected storage type %q, got %q", storage.TypeMinio, pub.completed.StorageType)
	}
}
