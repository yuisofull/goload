package task

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/storage"
	"github.com/yuisofull/goload/pkg/message"
)

type fakeTxManager struct{}

func (fakeTxManager) DoInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type fakeRepo struct {
	created *Task
	updated *Task
	listed  []*Task
	count   uint64
}

func (r *fakeRepo) Create(ctx context.Context, task *Task) (*Task, error) {
	r.created = task
	// mimic DB-assigned ID
	cloned := *task
	cloned.ID = 123
	return &cloned, nil
}

func (r *fakeRepo) GetByID(ctx context.Context, id uint64) (*Task, error) { return nil, nil }
func (r *fakeRepo) Update(ctx context.Context, task *Task) (*Task, error) {
	r.updated = task
	return task, nil
}

func (r *fakeRepo) ListByAccountID(ctx context.Context, filter TaskFilter, limit, offset uint32) ([]*Task, error) {
	return r.listed, nil
}

func (r *fakeRepo) GetTaskCountOfAccount(ctx context.Context, ofAccountID uint64) (uint64, error) {
	return r.count, nil
}
func (r *fakeRepo) Delete(ctx context.Context, id uint64) error { return nil }

type fakeMessagePublisher struct {
	topic  string
	msgs   []*message.Message
	closed bool
}

func (p *fakeMessagePublisher) Publish(topic string, messages ...*message.Message) error {
	p.topic = topic
	p.msgs = append(p.msgs, messages...)
	return nil
}

func (p *fakeMessagePublisher) Close() error {
	p.closed = true
	return nil
}

type fakeWriter struct {
	key      string
	content  []byte
	metadata *storage.FileMetadata
}

func (w *fakeWriter) Store(ctx context.Context, key string, reader io.Reader, metadata *storage.FileMetadata) error {
	w.key = key
	b, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	w.content = b
	w.metadata = metadata
	return nil
}

func (w *fakeWriter) Exists(ctx context.Context, key string) (bool, error) { return false, nil }
func (w *fakeWriter) Delete(ctx context.Context, key string) error         { return nil }

type fakePresigner struct{}

func (fakePresigner) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "http://example.test/task-sources/" + key + "?X-Amz-Expires=" + ttl.String(), nil
}

func TestCreateTask_MovesTorrentDataURLToMinioBeforeDBAndKafka(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("torrent-bytes"))
	dataURL := bittorrentDataURLPrefix + encoded

	repo := &fakeRepo{}
	tx := fakeTxManager{}
	writer := &fakeWriter{}
	presigner := fakePresigner{}
	fakeMsgPub := &fakeMessagePublisher{}
	eventPub := NewEventPublisher(fakeMsgPub)

	svc := NewService(repo, *eventPub, tx,
		WithTaskSourceStore(writer),
		WithTaskSourcePresigner(presigner),
	)

	created, err := svc.CreateTask(context.Background(), &CreateTaskParam{
		OfAccountID: 7,
		FileName:    "upload.torrent",
		SourceURL:   dataURL,
		SourceType:  SourceBitTorrent,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	require.NotNil(t, repo.created)

	// Ensure SourceURL was replaced before repo.Create
	require.False(t, strings.HasPrefix(repo.created.SourceURL, bittorrentDataURLPrefix))
	t.Logf("repo SourceURL: %s", repo.created.SourceURL)
	require.True(t, strings.HasPrefix(repo.created.SourceURL, "http://"))
	require.Contains(t, repo.created.SourceURL, "/task-sources/")

	// Ensure Kafka payload also contains only the presigned URL
	require.Equal(t, "task.created", fakeMsgPub.topic)
	require.Len(t, fakeMsgPub.msgs, 1)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(fakeMsgPub.msgs[0].Payload, &payload))
	sourceURLAny, ok := payload["source_url"]
	require.True(t, ok)
	sourceURL, _ := sourceURLAny.(string)
	require.False(t, strings.HasPrefix(sourceURL, bittorrentDataURLPrefix))
	t.Logf("event SourceURL: %s", sourceURL)
	require.True(t, strings.HasPrefix(sourceURL, "http://"))
	require.Contains(t, sourceURL, "/task-sources/")

	// Ensure we uploaded the decoded bytes
	require.Equal(t, []byte("torrent-bytes"), writer.content)
	require.NotEmpty(t, writer.key)
	require.NotNil(t, writer.metadata)
	require.Equal(t, "application/x-bittorrent", writer.metadata.ContentType)
	require.NotZero(t, writer.metadata.Expiry)
}

func TestListTasks_RequiresFilter(t *testing.T) {
	svc := NewService(&fakeRepo{}, Publisher{}, fakeTxManager{})

	_, err := svc.ListTasks(context.Background(), &ListTasksParam{})
	require.Error(t, err)
}

func TestListTasks_ReturnsRepositoryTotal(t *testing.T) {
	repo := &fakeRepo{
		count: 10,
		listed: []*Task{
			{ID: 1, OfAccountID: 7},
			{ID: 2, OfAccountID: 7},
		},
	}
	svc := NewService(repo, Publisher{}, fakeTxManager{})

	out, err := svc.ListTasks(context.Background(), &ListTasksParam{
		Filter: &TaskFilter{OfAccountID: 7},
		Limit:  2,
	})
	require.NoError(t, err)
	require.Len(t, out.Tasks, 2)
	require.Equal(t, int32(10), out.Total)
}

func TestUpdateTaskErrorStoresErrorMessage(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, Publisher{}, fakeTxManager{})

	err := svc.UpdateTaskError(context.Background(), 42, errors.New("boom"))
	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	require.Equal(t, StatusFailed, repo.updated.Status)
	require.NotNil(t, repo.updated.ErrorMessage)
	require.Equal(t, "boom", *repo.updated.ErrorMessage)
}

func TestCreateTask_TorrentDataURLRequiresStorageConfig(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("torrent-bytes"))
	dataURL := bittorrentDataURLPrefix + encoded
	fakeMsgPub := &fakeMessagePublisher{}
	eventPub := NewEventPublisher(fakeMsgPub)

	svc := NewService(&fakeRepo{}, *eventPub, fakeTxManager{})
	_, err := svc.CreateTask(context.Background(), &CreateTaskParam{
		OfAccountID: 1,
		FileName:    "upload.torrent",
		SourceURL:   dataURL,
		SourceType:  SourceBitTorrent,
	})
	require.Error(t, err)
}
