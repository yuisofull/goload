package download

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// eagerDownloader reads the whole body inside Download (as the HTTP Range path
// does for small files) before returning the reader, so the fetch happens
// before the first Read of the returned reader.
type eagerDownloader struct{ fetchDelay time.Duration }

func (d *eagerDownloader) GetFileInfo(context.Context, string, *AuthConfig) (*FileMetadata, error) {
	return &FileMetadata{FileName: "file.txt", FileSize: int64(len("content")), ContentType: "text/plain"}, nil
}

func (d *eagerDownloader) Download(context.Context, string, *AuthConfig, DownloadOptions) (io.ReadCloser, int64, error) {
	time.Sleep(d.fetchDelay)
	return io.NopCloser(strings.NewReader("content")), int64(len("content")), nil
}

func (d *eagerDownloader) SupportsResume() bool { return false }

// An eager fetch inside Download must still be counted as transfer time, or a
// slow Range download of a small file would read as microseconds.
func TestTransferDurationCapturesEagerDownloadFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fm := runTransferFor(t,
			&eagerDownloader{fetchDelay: 2 * time.Second},
			func(context.Context) error { return nil },
		)
		if got := fm.only(t); got < 2*time.Second || got >= 3*time.Second {
			t.Fatalf("transfer duration %v should be ~2s (the eager fetch inside Download)", got)
		}
	})
}

type captureMetrics struct {
	mu        sync.Mutex
	transfers []time.Duration
	redeliv   int
}

func (m *captureMetrics) RecordTransfer(d time.Duration) {
	m.mu.Lock()
	m.transfers = append(m.transfers, d)
	m.mu.Unlock()
}

func (m *captureMetrics) IncRedelivered() {
	m.mu.Lock()
	m.redeliv++
	m.mu.Unlock()
}

func (m *captureMetrics) only(t *testing.T) time.Duration {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.transfers) != 1 {
		t.Fatalf("want exactly 1 transfer sample, got %d: %v", len(m.transfers), m.transfers)
	}
	return m.transfers[0]
}

// slowReader yields its bytes immediately, then spends delay before EOF, so the
// EOF (drain) lands delay after the first read.
type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
	slept bool
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.pos < len(r.data) {
		n := copy(p, r.data[r.pos:])
		r.pos += n
		return n, nil
	}
	if !r.slept {
		r.slept = true
		time.Sleep(r.delay)
	}
	return 0, io.EOF
}

func (r *slowReader) Close() error { return nil }

type slowDownloader struct {
	probeDelay  time.Duration
	streamDelay time.Duration
}

func (d *slowDownloader) GetFileInfo(ctx context.Context, rawURL string, auth *AuthConfig) (*FileMetadata, error) {
	if d.probeDelay > 0 {
		time.Sleep(d.probeDelay)
	}
	return &FileMetadata{FileName: "file.txt", FileSize: int64(len("content")), ContentType: "text/plain"}, nil
}

func (d *slowDownloader) Download(ctx context.Context, rawURL string, auth *AuthConfig, opts DownloadOptions) (io.ReadCloser, int64, error) {
	return &slowReader{data: []byte("content"), delay: d.streamDelay}, int64(len("content")), nil
}

func (d *slowDownloader) SupportsResume() bool { return false }

func runTransferFor(t *testing.T, dl Downloader, storing func(context.Context) error) *captureMetrics {
	t.Helper()
	fm := &captureMetrics{}
	svc := NewService(&fakeStorage{}, &fakePublisher{}, WithMetrics(fm))
	exec := &taskExecution{
		task:       TaskRequest{TaskID: 1, SourceURL: "https://example.com/f", SourceType: "HTTP"},
		ctx:        context.Background(),
		cancelFunc: func() {},
	}
	_, err := svc.transfer(exec, dl, transferHooks{
		downloading: func(context.Context) error { return nil },
		progress:    func(context.Context, Progress) {},
		storing:     storing,
	})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	return fm
}

// Transfer time must stop when the source drains, before confirmStoring runs,
// so a slow completion step does not inflate it.
func TestTransferDurationExcludesConfirmStoring(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fm := runTransferFor(t,
			&slowDownloader{streamDelay: 3 * time.Second},
			func(context.Context) error { time.Sleep(5 * time.Second); return nil },
		)
		if got := fm.only(t); got < 3*time.Second || got >= 4*time.Second {
			t.Fatalf("transfer duration %v should be ~3s (stream), excluding the 5s storing step", got)
		}
	})
}

// Transfer time must start at the first source read, after the metadata probe,
// so a slow probe does not inflate it.
func TestTransferDurationExcludesProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fm := runTransferFor(t,
			&slowDownloader{probeDelay: 5 * time.Second, streamDelay: time.Second},
			func(context.Context) error { return nil },
		)
		if got := fm.only(t); got < time.Second || got >= 2*time.Second {
			t.Fatalf("transfer duration %v should be ~1s (stream), excluding the 5s probe", got)
		}
	})
}
