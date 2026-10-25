package downloadtransport

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
	apperrors "github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/pkg/message"
)

type fakeDownloadService struct {
	executeFn func(ctx context.Context, req download.TaskRequest) error
	pauseFn   func(ctx context.Context, taskID uint64) error
}

func (f *fakeDownloadService) ExecuteTask(ctx context.Context, req download.TaskRequest) error {
	if f.executeFn != nil {
		return f.executeFn(ctx, req)
	}
	return nil
}

func (f *fakeDownloadService) PauseTask(ctx context.Context, taskID uint64) error {
	if f.pauseFn != nil {
		return f.pauseFn(ctx, taskID)
	}
	return nil
}

func (f *fakeDownloadService) ResumeTask(_ context.Context, _ uint64) error {
	return nil
}

func (f *fakeDownloadService) CancelTask(_ context.Context, _ uint64) error {
	return nil
}

func (f *fakeDownloadService) StreamFile(
	_ context.Context,
	_ download.FileStreamRequest,
) (*download.FileStreamResponse, error) {
	return nil, apperrors.ErrInternal
}

func (f *fakeDownloadService) GetActiveTaskCount(_ context.Context) int { return 0 }

func waitAckOrNack(t *testing.T, msg *message.Message) bool {
	t.Helper()
	select {
	case <-msg.Acked():
		return true
	case <-msg.Nacked():
		return false
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ack/nack")
		return false
	}
}

func TestProcessTaskCreatedEvents_AcksAfterExecuteTask(t *testing.T) {
	t.Parallel()

	executed := make(chan download.TaskRequest, 1)
	ec := NewEventConsumer(&fakeDownloadService{
		executeFn: func(_ context.Context, req download.TaskRequest) error {
			executed <- req
			return nil
		},
	}, nil, log.NewNopLogger())

	ch := make(chan *message.Message, 1)
	payload, err := json.Marshal(events.TaskCreatedEvent{TaskID: 42, SourceType: "HTTP"})
	require.NoError(t, err)
	msg := message.NewMessage("created-1", payload)
	ch <- msg
	close(ch)

	ec.processTaskCreatedEvents(t.Context(), ch)

	select {
	case req := <-executed:
		assert.Equal(t, uint64(42), req.TaskID)
	default:
		t.Fatal("ExecuteTask was not called")
	}
	assert.True(t, waitAckOrNack(t, msg))
}

func TestProcessTaskCreatedEvents_NacksTransientFailure(t *testing.T) {
	t.Parallel()

	ec := NewEventConsumer(&fakeDownloadService{
		executeFn: func(_ context.Context, _ download.TaskRequest) error {
			return &apperrors.Error{Code: apperrors.ErrCodeInternal, Message: "disk full"}
		},
	}, nil, log.NewNopLogger())

	ch := make(chan *message.Message, 1)
	payload, err := json.Marshal(events.TaskCreatedEvent{TaskID: 7, SourceType: "HTTP"})
	require.NoError(t, err)
	msg := message.NewMessage("created-fail", payload)
	ch <- msg
	close(ch)

	ec.processTaskCreatedEvents(t.Context(), ch)
	assert.False(t, waitAckOrNack(t, msg))
}

func TestProcessTaskPausedEvents_AcksNotFound(t *testing.T) {
	t.Parallel()

	ec := NewEventConsumer(&fakeDownloadService{
		pauseFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{Code: apperrors.ErrCodeNotFound, Message: "task not found in active tasks"}
		},
	}, nil, log.NewNopLogger())

	ch := make(chan *message.Message, 1)
	payload, err := json.Marshal(events.TaskPausedEvent{TaskID: 9})
	require.NoError(t, err)
	msg := message.NewMessage("paused-1", payload)
	ch <- msg
	close(ch)

	ec.processTaskPausedEvents(t.Context(), ch)
	assert.True(t, waitAckOrNack(t, msg))
}

func TestProcessTaskPausedEvents_NacksOtherErrors(t *testing.T) {
	t.Parallel()

	ec := NewEventConsumer(&fakeDownloadService{
		pauseFn: func(_ context.Context, _ uint64) error {
			return &apperrors.Error{Code: apperrors.ErrCodeInternal, Message: "pause failed"}
		},
	}, nil, log.NewNopLogger())

	ch := make(chan *message.Message, 1)
	payload, err := json.Marshal(events.TaskPausedEvent{TaskID: 9})
	require.NoError(t, err)
	msg := message.NewMessage("paused-fail", payload)
	ch <- msg
	close(ch)

	ec.processTaskPausedEvents(t.Context(), ch)
	assert.False(t, waitAckOrNack(t, msg))
}
