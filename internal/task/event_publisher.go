package task

import (
	"context"
	"encoding/json"
	"time"

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
		SourceURL:       task.SourceURL,
		SourceType:      string(task.SourceType),
		SourceAuth:      ep.convertAuthConfig(task.SourceAuth),
		DownloadOptions: ep.convertDownloadOptions(task.DownloadOptions),
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
			"taskID":    string(rune(task.ID)),
		},
	}

	return ep.publisher.Publish("task.created", msg)
}

// PublishTaskStatusUpdated publishes a task status update event
func (ep *Publisher) PublishTaskStatusUpdated(ctx context.Context, taskID uint64, status TaskStatus) error {
	event := events.TaskStatusUpdatedEvent{
		TaskID:    taskID,
		Status:    string(status),
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
			"taskID":    string(rune(taskID)),
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
			"taskID":    string(rune(taskID)),
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
			"taskID":    string(rune(taskID)),
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
			"taskID":    string(rune(taskID)),
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

// generateUUID generates a simple UUID for messages
// In a real implementation, you might want to use a proper UUID library
func generateUUID() string {
	return time.Now().Format("20060102150405.000000")
}
