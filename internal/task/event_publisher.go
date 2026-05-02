package task

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/pkg/message"
)

// Publisher publishes task-related events
type Publisher struct {
	publisher message.Publisher
}

// NewEventPublisher creates a new event publisher for task service
func NewEventPublisher(publisher message.Publisher) *Publisher {
	return &Publisher{
		publisher: publisher,
	}
}

// PublishTaskCreated publishes a task created event
func (ep *Publisher) PublishTaskCreated(ctx context.Context, task *Task) error {
	event := events.TaskCreatedEvent{
		TaskID:          task.ID,
		OfAccountID:     task.OfAccountID,
		FileName:        task.FileName,
		SourceURL:       task.SourceURL,
		SourceType:      string(task.SourceType),
		SourceAuth:      ep.convertAuthConfig(task.SourceAuth),
		DownloadOptions: ep.convertDownloadOptions(task.DownloadOptions),
		Metadata:        task.Metadata,
		CreatedAt:       task.CreatedAt,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskCreated",
			"taskID":    formatTaskID(task.ID),
		},
	}

	return ep.publisher.Publish("task.created", msg)
}

// PublishTaskStatusUpdated publishes a task status update event
func (ep *Publisher) PublishTaskStatusUpdated(ctx context.Context, taskID uint64, status TaskStatus) error {
	event := events.TaskStatusUpdatedEvent{
		TaskID:    taskID,
		Status:    events.TaskStatusValue(string(status)),
		UpdatedAt: time.Now(),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskStatusUpdated",
			"taskID":    formatTaskID(taskID),
		},
	}

	return ep.publisher.Publish("task.status.updated", msg)
}

// PublishTaskPaused publishes a task paused event
func (ep *Publisher) PublishTaskPaused(ctx context.Context, taskID uint64) error {
	event := events.TaskPausedEvent{
		TaskID:   taskID,
		PausedAt: time.Now(),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskPaused",
			"taskID":    formatTaskID(taskID),
		},
	}

	return ep.publisher.Publish("task.paused", msg)
}

// PublishTaskResumed publishes a task resumed event
func (ep *Publisher) PublishTaskResumed(ctx context.Context, taskID uint64) error {
	event := events.TaskResumedEvent{
		TaskID:    taskID,
		ResumedAt: time.Now(),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskResumed",
			"taskID":    formatTaskID(taskID),
		},
	}

	return ep.publisher.Publish("task.resumed", msg)
}

// PublishTaskCancelled publishes a task cancelled event
func (ep *Publisher) PublishTaskCancelled(ctx context.Context, taskID uint64) error {
	event := events.TaskCancelledEvent{
		TaskID:      taskID,
		CancelledAt: time.Now(),
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskCancelled",
			"taskID":    formatTaskID(taskID),
		},
	}

	return ep.publisher.Publish("task.cancelled", msg)
}

// Helper methods for converting task types to event types
func (ep *Publisher) convertAuthConfig(auth *AuthConfig) *events.AuthConfig {
	if auth == nil {
		return nil
	}
	return &events.AuthConfig{
		Type:     auth.Type,
		Username: auth.Username,
		Password: auth.Password,
		Token:    auth.Token,
		Headers:  auth.Headers,
	}
}

func (ep *Publisher) convertDownloadOptions(opts *DownloadOptions) *events.DownloadOptions {
	if opts == nil {
		return nil
	}
	return &events.DownloadOptions{
		Concurrency: opts.Concurrency,
		MaxSpeed:    opts.MaxSpeed,
		MaxRetries:  opts.MaxRetries,
		Timeout:     opts.Timeout,
	}
}

func generateUUID() string {
	return uuid.New().String()
}

func formatTaskID(taskID uint64) string {
	return strconv.FormatUint(taskID, 10)
}
