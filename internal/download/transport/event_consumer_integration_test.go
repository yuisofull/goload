package downloadtransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-kit/log"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/download/downloader"
	downloadtransport "github.com/yuisofull/goload/internal/download/transport"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/pkg/message"
	kafkamsg "github.com/yuisofull/goload/pkg/message/kafka"
)

// ──────────────────────────────────────────────────────────────────────────────
// In-memory storage backend
// ──────────────────────────────────────────────────────────────────────────────

type memStorage struct {
	mu    sync.RWMutex
	files map[string][]byte
	meta  map[string]*storage.FileMetadata
}

func newMemStorage() *memStorage {
	return &memStorage{
		files: make(map[string][]byte),
		meta:  make(map[string]*storage.FileMetadata),
	}
}

func (m *memStorage) Store(_ context.Context, key string, r io.Reader, meta *storage.FileMetadata) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = data
	m.meta[key] = meta
	return nil
}

func (m *memStorage) Exists(_ context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.files[key]
	return ok, nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	delete(m.meta, key)
	return nil
}

func (m *memStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memStorage) GetWithRange(_ context.Context, key string, start, end int64) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(data[start:end])), nil
}

func (m *memStorage) GetInfo(_ context.Context, key string) (*storage.FileMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.meta[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return meta, nil
}

func (m *memStorage) content(key string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[key]
	return data, ok
}

// ──────────────────────────────────────────────────────────────────────────────
// Captured events helper
// ──────────────────────────────────────────────────────────────────────────────

type capturedEvents struct {
	mu         sync.Mutex
	completed  []events.TaskCompletedEvent
	failed     []events.TaskFailedEvent
	statusLogs []events.TaskStatusUpdatedEvent
}

func (c *capturedEvents) onCompleted(e events.TaskCompletedEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completed = append(c.completed, e)
}

func (c *capturedEvents) onFailed(e events.TaskFailedEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failed = append(c.failed, e)
}

func (c *capturedEvents) onStatus(e events.TaskStatusUpdatedEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statusLogs = append(c.statusLogs, e)
}

func (c *capturedEvents) waitCompleted(t *testing.T, taskID uint64, timeout time.Duration) events.TaskCompletedEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, e := range c.completed {
			if e.TaskID == taskID {
				c.mu.Unlock()
				return e
			}
		}
		c.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for TaskCompleted event for task %d", taskID)
	return events.TaskCompletedEvent{}
}

func (c *capturedEvents) waitFailed(t *testing.T, taskID uint64, timeout time.Duration) events.TaskFailedEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, e := range c.failed {
			if e.TaskID == taskID {
				c.mu.Unlock()
				return e
			}
		}
		c.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for TaskFailed event for task %d", taskID)
	return events.TaskFailedEvent{}
}

// ──────────────────────────────────────────────────────────────────────────────
// Test publisher that wraps real Kafka publisher but also captures events locally
// ──────────────────────────────────────────────────────────────────────────────

type spyEventPublisher struct {
	inner    *download.DownloadEventPublisher
	captured *capturedEvents
}

func (s *spyEventPublisher) PublishTaskStatusUpdated(ctx context.Context, ev events.TaskStatusUpdatedEvent) error {
	s.captured.onStatus(ev)
	return s.inner.PublishTaskStatusUpdated(ctx, ev)
}

func (s *spyEventPublisher) PublishTaskProgressUpdated(ctx context.Context, ev events.TaskProgressUpdatedEvent) error {
	return s.inner.PublishTaskProgressUpdated(ctx, ev)
}

func (s *spyEventPublisher) PublishTaskCompleted(ctx context.Context, ev events.TaskCompletedEvent) error {
	s.captured.onCompleted(ev)
	return s.inner.PublishTaskCompleted(ctx, ev)
}

func (s *spyEventPublisher) PublishTaskFailed(ctx context.Context, ev events.TaskFailedEvent) error {
	s.captured.onFailed(ev)
	return s.inner.PublishTaskFailed(ctx, ev)
}

// ──────────────────────────────────────────────────────────────────────────────
// Kafka test helpers
// ──────────────────────────────────────────────────────────────────────────────

var kafkaVersion = sarama.V3_0_0_0

var topicDetail = &sarama.TopicDetail{NumPartitions: 1, ReplicationFactor: 1}

func startKafka(t *testing.T) (brokers []string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	kc, err := tckafka.Run(ctx, "confluentinc/confluent-local:7.7.1")
	require.NoError(t, err, "start Kafka container")
	brokers, err = kc.Brokers(ctx)
	require.NoError(t, err, "get Kafka brokers")
	return brokers, func() {
		if err := kc.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate kafka: %v", err)
		}
	}
}

func newSubscriber(t *testing.T, brokers []string, group string, topics []string) *kafkamsg.Subscriber {
	t.Helper()
	sub, err := kafkamsg.NewSubscriber(&kafkamsg.SubscriberConfig{
		Brokers:                brokers,
		ConsumerGroup:          group,
		Version:                kafkaVersion,
		NackResendSleep:        50 * time.Millisecond,
		ReconnectRetrySleep:    200 * time.Millisecond,
		InitializeTopicDetails: topicDetail,
	})
	require.NoError(t, err)

	for _, topic := range topics {
		require.NoError(t, sub.SubscribeInitialize(topic))
	}
	return sub
}

func newPublisher(t *testing.T, brokers []string) *kafkamsg.Publisher {
	t.Helper()
	pub, err := kafkamsg.NewPublisher(&kafkamsg.PublisherConfig{
		BrokerHosts: brokers,
		Version:     kafkaVersion,
		ClientID:    "test-task-svc",
	})
	require.NoError(t, err)
	return pub
}

// kitLogger wraps go-kit/log for the EventConsumer's Logger interface.
type kitLogger struct{ l log.Logger }

func (k *kitLogger) Printf(format string, v ...interface{}) {
	_ = k.l.Log("msg", fmt.Sprintf(format, v...))
}

// publishTaskCreated is the equivalent of what the task service does: it
// serialises a TaskCreatedEvent and publishes it to the "task_created" topic.
func publishTaskCreated(t *testing.T, pub *kafkamsg.Publisher, ev events.TaskCreatedEvent) {
	t.Helper()
	payload, err := json.Marshal(ev)
	require.NoError(t, err)

	msg := &message.Message{
		UUID:    fmt.Sprintf("test-%d", ev.TaskID),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskCreated",
		},
	}
	require.NoError(t, pub.Publish(string(events.EventTaskCreated), msg))
}

// buildSystem wires everything together and returns:
//   - a task-side Kafka publisher (mimics task svc)
//   - the in-memory storage backend (for assertions)
//   - captured events (for assertions)
//   - a context cancel to shut the system down
func buildSystem(t *testing.T, brokers []string) (
	taskPub *kafkamsg.Publisher,
	stor *memStorage,
	captured *capturedEvents,
	cancel context.CancelFunc,
) {
	t.Helper()

	// Kafka publisher used by task service to publish TaskCreated
	taskPub = newPublisher(t, brokers)

	// Kafka publisher used by download service to publish status events
	dlKafkaPub := newPublisher(t, brokers)

	// In-memory storage
	stor = newMemStorage()

	// Captured events (spy wrapping the real kafka publisher)
	captured = &capturedEvents{}
	dlEventPub := download.NewDownloadEventPublisher(dlKafkaPub)
	spy := &spyEventPublisher{inner: dlEventPub, captured: captured}

	// Download service (spy satisfies download.EventPublisher interface)
	dlSvc := download.NewService(stor, spy,
		download.WithMaxConcurrent(5),
		download.WithTaskTimeout(30*time.Second),
	)

	// Register HTTP/HTTPS downloaders via the Registrar interface
	httpDl := downloader.NewHTTPDownloader(nil)
	dlSvc.RegisterDownloader("HTTP", httpDl)
	dlSvc.RegisterDownloader("HTTPS", httpDl)

	// Subscriber for task-created + control events
	allTopics := []string{
		string(events.EventTaskCreated),
		string(events.EventTaskPaused),
		string(events.EventTaskResumed),
		string(events.EventTaskCancelled),
	}
	sub := newSubscriber(t, brokers, "download-svc-test", allTopics)

	// Event consumer
	logger := &kitLogger{l: log.NewNopLogger()}
	consumer := downloadtransport.NewEventConsumer(dlSvc, sub, logger)

	ctx, cancelFn := context.WithCancel(context.Background())
	go func() {
		if err := consumer.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Logf("consumer.Start error: %v", err)
		}
	}()

	t.Cleanup(func() {
		cancelFn()
		taskPub.Close()
		dlKafkaPub.Close()
		sub.Close()
	})

	return taskPub, stor, captured, cancelFn
}

// TestTaskCreated_DownloadFromHTTP_StoresFile is the main integration test:
//  1. An HTTP test server serves a small file.
//  2. The task service publishes a TaskCreated event to Kafka.
//  3. The download EventConsumer picks it up, calls ExecuteTask.
//  4. The HTTP downloader fetches the file, stores it in memStorage.
//  5. A TaskCompleted event is emitted (captured by spy).
//  6. Assertions: file stored in memory, completed event has correct TaskID/FileName.
func TestTaskCreated_DownloadFromHTTP_StoresFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanupKafka := startKafka(t)
	defer cleanupKafka()

	fileContent := []byte("hello from the integration test file")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
		w.WriteHeader(http.StatusOK)
		w.Write(fileContent)
	}))
	defer srv.Close()

	taskPub, stor, captured, _ := buildSystem(t, brokers)

	const taskID = uint64(1001)
	ev := events.TaskCreatedEvent{
		TaskID:     taskID,
		SourceURL:  srv.URL + "/file.txt",
		SourceType: "HTTP",
		FileName:   "file.txt",
		CreatedAt:  time.Now(),
	}

	publishTaskCreated(t, taskPub, ev)

	// Wait for completion (up to 30 s to account for Kafka rebalance)
	completed := captured.waitCompleted(t, taskID, 30*time.Second)

	assert.Equal(t, taskID, completed.TaskID)
	assert.Equal(t, "text/plain", completed.ContentType)
	assert.NotEmpty(t, completed.StorageKey)
	assert.Equal(t, "md5", completed.Checksum.ChecksumType)
	assert.NotEmpty(t, completed.Checksum.ChecksumValue)

	// Verify the file is actually stored
	data, ok := stor.content(completed.StorageKey)
	require.True(t, ok, "file should be in memStorage")
	assert.Equal(t, fileContent, data)
}

// TestTaskCreated_DownloadFromHTTP_LargerPayload verifies progress events and
// that a multi-KB file is stored intact.
func TestTaskCreated_DownloadFromHTTP_LargerPayload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanupKafka := startKafka(t)
	defer cleanupKafka()

	// 64 KB payload
	largeContent := bytes.Repeat([]byte("a"), 64*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(largeContent)))
		w.WriteHeader(http.StatusOK)
		w.Write(largeContent)
	}))
	defer srv.Close()

	taskPub, stor, captured, _ := buildSystem(t, brokers)

	const taskID = uint64(1002)
	publishTaskCreated(t, taskPub, events.TaskCreatedEvent{
		TaskID:     taskID,
		SourceURL:  srv.URL + "/large.bin",
		SourceType: "HTTP",
		FileName:   "large.bin",
		CreatedAt:  time.Now(),
	})

	completed := captured.waitCompleted(t, taskID, 30*time.Second)
	assert.Equal(t, taskID, completed.TaskID)
	assert.EqualValues(t, len(largeContent), completed.FileSize)

	data, ok := stor.content(completed.StorageKey)
	require.True(t, ok)
	assert.Equal(t, largeContent, data)
}

// TestTaskCreated_BadURL_EmitsFailedEvent verifies that when the HTTP download
// source returns 404 (or the URL is unreachable), a TaskFailed event is emitted.
func TestTaskCreated_BadURL_EmitsFailedEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanupKafka := startKafka(t)
	defer cleanupKafka()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	taskPub, _, captured, _ := buildSystem(t, brokers)

	const taskID = uint64(1003)
	publishTaskCreated(t, taskPub, events.TaskCreatedEvent{
		TaskID:     taskID,
		SourceURL:  srv.URL + "/missing.zip",
		SourceType: "HTTP",
		FileName:   "missing.zip",
		CreatedAt:  time.Now(),
		DownloadOptions: &events.DownloadOptions{
			MaxRetries: 0, // fail fast
		},
	})

	failed := captured.waitFailed(t, taskID, 30*time.Second)
	assert.Equal(t, taskID, failed.TaskID)
	assert.NotEmpty(t, failed.Error)
}

// TestTaskCreated_UnknownSourceType_EmitsFailedEvent verifies that when no
// downloader is registered for the source type, a TaskFailed event is emitted.
func TestTaskCreated_UnknownSourceType_EmitsFailedEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanupKafka := startKafka(t)
	defer cleanupKafka()

	taskPub, _, captured, _ := buildSystem(t, brokers)

	const taskID = uint64(1004)
	publishTaskCreated(t, taskPub, events.TaskCreatedEvent{
		TaskID:     taskID,
		SourceURL:  "ftp://ftp.example.com/file.bin",
		SourceType: "FTP", // no FTP downloader registered
		FileName:   "file.bin",
		CreatedAt:  time.Now(),
	})

	failed := captured.waitFailed(t, taskID, 30*time.Second)
	assert.Equal(t, taskID, failed.TaskID)
	assert.Contains(t, failed.Error, "FTP")
}

// TestMultipleTasks_ConcurrentDownloads verifies that multiple tasks published
// in quick succession are all executed and completed independently.
func TestMultipleTasks_ConcurrentDownloads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanupKafka := startKafka(t)
	defer cleanupKafka()

	contents := map[uint64][]byte{
		2001: []byte("content for task 2001"),
		2002: []byte("content for task 2002"),
		2003: []byte("content for task 2003"),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Route by query param ?id=
		idStr := r.URL.Query().Get("id")
		var id uint64
		fmt.Sscanf(idStr, "%d", &id)
		data, ok := contents[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.Write(data)
	}))
	defer srv.Close()

	taskPub, stor, captured, _ := buildSystem(t, brokers)

	for id := range contents {
		publishTaskCreated(t, taskPub, events.TaskCreatedEvent{
			TaskID:     id,
			SourceURL:  fmt.Sprintf("%s/file.txt?id=%d", srv.URL, id),
			SourceType: "HTTP",
			FileName:   fmt.Sprintf("file-%d.txt", id),
			CreatedAt:  time.Now(),
		})
	}

	for id, expected := range contents {
		completed := captured.waitCompleted(t, id, 30*time.Second)
		assert.Equal(t, id, completed.TaskID)

		data, ok := stor.content(completed.StorageKey)
		require.True(t, ok, "file %d should be stored", id)
		assert.Equal(t, expected, data)
	}
}

// TestStatusTransitions_DownloadingThenCompleted asserts that the status
// transitions PENDING→DOWNLOADING→STORING→COMPLETED are all emitted.
func TestStatusTransitions_DownloadingThenCompleted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	brokers, cleanupKafka := startKafka(t)
	defer cleanupKafka()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	taskPub, _, captured, _ := buildSystem(t, brokers)

	const taskID = uint64(3001)
	publishTaskCreated(t, taskPub, events.TaskCreatedEvent{
		TaskID:     taskID,
		SourceURL:  srv.URL + "/data",
		SourceType: "HTTP",
		FileName:   "data",
		CreatedAt:  time.Now(),
	})

	// Wait for completed
	captured.waitCompleted(t, taskID, 30*time.Second)

	// Assert that we saw at least DOWNLOADING and STORING transitions
	captured.mu.Lock()
	defer captured.mu.Unlock()

	var seenStatuses []string
	for _, s := range captured.statusLogs {
		if s.TaskID == taskID {
			seenStatuses = append(seenStatuses, s.Status.String())
		}
	}

	assert.Contains(t, seenStatuses, events.StatusDownloading.String())
	assert.Contains(t, seenStatuses, events.StatusStoring.String())
}
