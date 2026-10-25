package downloadtransport

import (
	"context"
	"encoding/json"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/pkg/message"
)

// EventConsumer handles incoming events for the download service.
type EventConsumer struct {
	service    download.Service
	subscriber message.Subscriber
	logger     log.Logger
}

// NewEventConsumer creates a new event consumer for the download service.
func NewEventConsumer(service download.Service, subscriber message.Subscriber, logger log.Logger) *EventConsumer {
	return &EventConsumer{
		service:    service,
		subscriber: subscriber,
		logger:     logger,
	}
}

// Start begins consuming events.
func (ec *EventConsumer) Start(ctx context.Context) error {
	taskCreatedCh, err := ec.subscriber.Subscribe(ctx, string(events.EventTaskCreated))
	if err != nil {
		return err
	}
	level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.created")

	taskPausedCh, err := ec.subscriber.Subscribe(ctx, string(events.EventTaskPaused))
	if err != nil {
		return err
	}
	level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.paused")

	taskResumedCh, err := ec.subscriber.Subscribe(ctx, string(events.EventTaskResumed))
	if err != nil {
		return err
	}
	level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.resumed")

	taskCancelledCh, err := ec.subscriber.Subscribe(ctx, string(events.EventTaskCancelled))
	if err != nil {
		return err
	}
	level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.cancelled")

	go ec.processTaskCreatedEvents(ctx, taskCreatedCh)
	go ec.processTaskPausedEvents(ctx, taskPausedCh)
	go ec.processTaskResumedEvents(ctx, taskResumedCh)
	go ec.processTaskCancelledEvents(ctx, taskCancelledCh)

	level.Info(ec.logger).Log("msg", "event consumer running, awaiting context done")
	<-ctx.Done()
	level.Info(ec.logger).Log("msg", "event consumer stopping: context canceled")
	return ctx.Err()
}

func (ec *EventConsumer) processTaskCreatedEvents(ctx context.Context, ch <-chan *message.Message) {
	for msg := range ch {
		var event events.TaskCreatedEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			level.Error(ec.logger).Log("msg", "failed to unmarshal TaskCreatedEvent", "err", err)
			msg.Nack()
			continue
		}

		req := taskRequestFromCreatedEvent(event)
		level.Debug(ec.logger).Log(
			"msg", "executing task",
			"task_id", req.TaskID,
			"source_type", req.SourceType,
			"source_url_len", len(req.SourceURL),
		)

		if err := ec.service.ExecuteTask(ctx, req); err != nil {
			level.Error(ec.logger).Log("msg", "failed to execute task", "task_id", event.TaskID, "err", err)
			if isPermanentDownloadError(err) {
				msg.Ack()
				continue
			}
			msg.Nack()
			continue
		}
		// ExecuteTask returns after starting the download; completion is reported by task events.
		msg.Ack()
	}
}

func (ec *EventConsumer) processTaskPausedEvents(ctx context.Context, ch <-chan *message.Message) {
	ec.processControlEvents(ch, "TaskPausedEvent", func(taskID uint64) error {
		return ec.service.PauseTask(ctx, taskID)
	})
}

func (ec *EventConsumer) processTaskResumedEvents(ctx context.Context, ch <-chan *message.Message) {
	ec.processControlEvents(ch, "TaskResumedEvent", func(taskID uint64) error {
		return ec.service.ResumeTask(ctx, taskID)
	})
}

func (ec *EventConsumer) processTaskCancelledEvents(ctx context.Context, ch <-chan *message.Message) {
	ec.processControlEvents(ch, "TaskCancelledEvent", func(taskID uint64) error {
		return ec.service.CancelTask(ctx, taskID)
	})
}

func (ec *EventConsumer) processControlEvents(
	ch <-chan *message.Message,
	eventName string,
	handle func(taskID uint64) error,
) {
	for msg := range ch {
		var event struct {
			TaskID uint64 `json:"task_id"`
		}
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			level.Error(ec.logger).Log("msg", "failed to unmarshal "+eventName, "err", err)
			msg.Nack()
			continue
		}

		if err := handle(event.TaskID); err != nil {
			level.Error(ec.logger).Log("msg", "failed to handle "+eventName, "task_id", event.TaskID, "err", err)
			if errors.IsError(err, errors.ErrCodeNotFound) {
				msg.Ack()
				continue
			}
			msg.Nack()
			continue
		}
		msg.Ack()
	}
}

func taskRequestFromCreatedEvent(event events.TaskCreatedEvent) download.TaskRequest {
	var req download.TaskRequest
	if event.SourceAuth != nil {
		req.SourceAuth = &download.AuthConfig{
			Type:     event.SourceAuth.Type,
			Username: event.SourceAuth.Username,
			Password: event.SourceAuth.Password,
			Token:    event.SourceAuth.Token,
		}
	}
	if event.DownloadOptions != nil {
		req.DownloadOptions = &download.DownloadOptions{
			Concurrency: event.DownloadOptions.Concurrency,
			MaxRetries:  event.DownloadOptions.MaxRetries,
		}
	}
	req.TaskID = event.TaskID
	req.OfAccountID = event.OfAccountID
	req.FileName = event.FileName
	req.SourceURL = event.SourceURL
	req.SourceType = event.SourceType
	req.Metadata = event.Metadata
	req.Checksum = nil
	req.CreatedAt = event.CreatedAt
	return req
}

func isPermanentDownloadError(err error) bool {
	return errors.IsError(err, errors.ErrCodeInvalidInput) || errors.IsError(err, errors.ErrCodeConflict)
}
