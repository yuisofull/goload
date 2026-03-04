package downloadtransport

import (
	"context"
	"encoding/json"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/yuisofull/goload/internal/download"
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
	// Subscribe to task created events
	taskCreatedCh, err := ec.subscriber.Subscribe(ctx, string(events.EventTaskCreated))
	if err != nil {
		return err
	}
	level.Info(ec.logger).Log("msg", "subscription started", "topic", "task.created")

	// Subscribe to task control events
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

	// Process events in separate goroutines
	go ec.processTaskCreatedEvents(ctx, taskCreatedCh)
	go ec.processTaskPausedEvents(ctx, taskPausedCh)
	go ec.processTaskResumedEvents(ctx, taskResumedCh)
	go ec.processTaskCancelledEvents(ctx, taskCancelledCh)

	// Block here until context cancellation so Start acts as a long-running process
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

		// Map event to internal TaskRequest and execute the task in a separate goroutine
		go func(event events.TaskCreatedEvent) {
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

			if err := ec.service.ExecuteTask(ctx, req); err != nil {
				level.Error(ec.logger).Log("msg", "failed to execute task", "task_id", event.TaskID, "err", err)
			}
		}(event)

		msg.Ack()
	}
}

func (ec *EventConsumer) processTaskPausedEvents(ctx context.Context, ch <-chan *message.Message) {
	for msg := range ch {
		var event events.TaskPausedEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			level.Error(ec.logger).Log("msg", "failed to unmarshal TaskPausedEvent", "err", err)
			msg.Nack()
			continue
		}

		if err := ec.service.PauseTask(ctx, event.TaskID); err != nil {
			level.Error(ec.logger).Log("msg", "failed to pause task", "task_id", event.TaskID, "err", err)
		}

		msg.Ack()
	}
}

func (ec *EventConsumer) processTaskResumedEvents(ctx context.Context, ch <-chan *message.Message) {
	for msg := range ch {
		var event events.TaskResumedEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			level.Error(ec.logger).Log("msg", "failed to unmarshal TaskResumedEvent", "err", err)
			msg.Nack()
			continue
		}

		if err := ec.service.ResumeTask(ctx, event.TaskID); err != nil {
			level.Error(ec.logger).Log("msg", "failed to resume task", "task_id", event.TaskID, "err", err)
		}

		msg.Ack()
	}
}

func (ec *EventConsumer) processTaskCancelledEvents(ctx context.Context, ch <-chan *message.Message) {
	for msg := range ch {
		var event events.TaskCancelledEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			level.Error(ec.logger).Log("msg", "failed to unmarshal TaskCancelledEvent", "err", err)
			msg.Nack()
			continue
		}

		if err := ec.service.CancelTask(ctx, event.TaskID); err != nil {
			level.Error(ec.logger).Log("msg", "failed to cancel task", "task_id", event.TaskID, "err", err)
		}

		msg.Ack()
	}
}
