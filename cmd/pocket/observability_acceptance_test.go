package main

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/task"
)

type pocketTaskMetrics struct {
	mu            sync.Mutex
	dispatchWaits []time.Duration
}

func (m *pocketTaskMetrics) RecordDispatchWait(d time.Duration) {
	m.mu.Lock()
	m.dispatchWaits = append(m.dispatchWaits, d)
	m.mu.Unlock()
}
func (m *pocketTaskMetrics) IncCompleted()           {}
func (m *pocketTaskMetrics) IncFailed()              {}
func (m *pocketTaskMetrics) IncRetried()             {}
func (m *pocketTaskMetrics) RecordRelayPublish(bool) {}
func (m *pocketTaskMetrics) IncRedispatched()        {}

func (m *pocketTaskMetrics) max() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out time.Duration
	for _, d := range m.dispatchWaits {
		if d > out {
			out = d
		}
	}
	return out
}

func (m *pocketTaskMetrics) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.dispatchWaits)
}

type pocketDlMetrics struct {
	mu        sync.Mutex
	transfers []time.Duration
}

func (m *pocketDlMetrics) RecordTransfer(d time.Duration) {
	m.mu.Lock()
	m.transfers = append(m.transfers, d)
	m.mu.Unlock()
}
func (m *pocketDlMetrics) IncRedelivered() {}

func (m *pocketDlMetrics) max() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out time.Duration
	for _, d := range m.transfers {
		if d > out {
			out = d
		}
	}
	return out
}

// slowStreamDownloader streams its body, then spends delay before EOF, so the
// slow time is spent inside the byte stream (transfer), not the probe or claim.
type slowStreamDownloader struct {
	body  string
	delay time.Duration
}

func (d *slowStreamDownloader) GetFileInfo(context.Context, string, *download.AuthConfig) (*download.FileMetadata, error) {
	return &download.FileMetadata{FileName: "file.bin", FileSize: int64(len(d.body)), ContentType: "application/octet-stream"}, nil
}

func (d *slowStreamDownloader) Download(context.Context, string, *download.AuthConfig, download.DownloadOptions) (io.ReadCloser, int64, error) {
	return &slowStreamReader{data: []byte(d.body), delay: d.delay}, int64(len(d.body)), nil
}

func (d *slowStreamDownloader) SupportsResume() bool { return false }

type slowStreamReader struct {
	data  []byte
	pos   int
	delay time.Duration
	slept bool
}

func (r *slowStreamReader) Read(p []byte) (int, error) {
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

func (r *slowStreamReader) Close() error { return nil }

// A download whose bytes arrive slowly must show up as transfer time, not
// dispatch wait: the two clocks measure different things end to end.
func TestPocketPipeline_SlowTransferShowsAsTransferNotDispatchWait(t *testing.T) {
	taskM := &pocketTaskMetrics{}
	dlM := &pocketDlMetrics{}
	dl := &slowStreamDownloader{body: "payload", delay: 1500 * time.Millisecond}

	p := startPipeline(t, dl, pipelineOpts{taskMetrics: taskM, dlMetrics: dlM})

	id := p.create(t)
	p.waitFor(t, id, task.StatusCompleted)

	transfer := dlM.max()
	dispatch := taskM.max()

	require.GreaterOrEqual(t, transfer, time.Second,
		"the slow byte stream must be attributed to transfer duration")
	require.GreaterOrEqual(t, taskM.count(), 1, "dispatch wait must be recorded on the first claim")
	require.Less(t, dispatch, transfer,
		"dispatch was prompt; the injected delay is transfer, not dispatch wait")
}

// The mirror case: when the claim is held (dispatch blocked), the delay shows
// as dispatch wait, not transfer. Together with the slow-transfer test this
// pins the two clocks to the two different causes.
func TestPocketPipeline_HeldDispatchShowsAsDispatchWaitNotTransfer(t *testing.T) {
	taskM := &pocketTaskMetrics{}
	dlM := &pocketDlMetrics{}
	dl := &scriptedDownloader{body: "payload"} // fast download

	p := startPipeline(t, dl, pipelineOpts{taskMetrics: taskM, dlMetrics: dlM, gated: true})

	// Hold the worker's claim so dispatch wait accrues, then release it.
	p.gate.down.Store(true)
	id := p.create(t)
	time.Sleep(1500 * time.Millisecond)
	p.gate.down.Store(false)
	p.waitFor(t, id, task.StatusCompleted)

	dispatch := taskM.max()
	transfer := dlM.max()

	require.GreaterOrEqual(t, dispatch, time.Second,
		"the held dispatch must be attributed to dispatch wait")
	require.Less(t, transfer, dispatch,
		"the fast download's transfer is well under the dispatch wait")
}
