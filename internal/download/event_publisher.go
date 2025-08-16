package download

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/pkg/message"
)

// DownloadEventPublisher publishes download-related events
type DownloadEventPublisher struct {
	publisher message.Publisher
}

// NewDownloadEventPublisher creates a new event publisher for download service
func NewDownloadEventPublisher(publisher message.Publisher) *DownloadEventPublisher {
	return &DownloadEventPublisher{
		publisher: publisher,
	}
}

// PublishTaskStatusUpdated publishes a task status update event
func (dep *DownloadEventPublisher) PublishTaskStatusUpdated(ctx context.Context, event events.TaskStatusUpdatedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskStatusUpdated",
			"taskID":    string(rune(event.TaskID)),
		},
	}

	return dep.publisher.Publish("task.status.updated", msg)
}

// PublishTaskProgressUpdated publishes a task progress update event
func (dep *DownloadEventPublisher) PublishTaskProgressUpdated(ctx context.Context, event events.TaskProgressUpdatedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskProgressUpdated",
			"taskID":    string(rune(event.TaskID)),
		},
	}

	return dep.publisher.Publish("task.progress.updated", msg)
}

// PublishTaskCompleted publishes a task completion event
func (dep *DownloadEventPublisher) PublishTaskCompleted(ctx context.Context, event events.TaskCompletedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskCompleted",
			"taskID":    string(rune(event.TaskID)),
		},
	}

	return dep.publisher.Publish("task.completed", msg)
}

// PublishTaskFailed publishes a task failure event
func (dep *DownloadEventPublisher) PublishTaskFailed(ctx context.Context, event events.TaskFailedEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := &message.Message{
		UUID:    generateUUID(),
		Payload: payload,
		Metadata: message.Metadata{
			"eventType": "TaskFailed",
			"taskID":    string(rune(event.TaskID)),
		},
	}

	return dep.publisher.Publish("task.failed", msg)
}

// generateUUID generates a simple UUID for messages
// In a real implementation, you might want to use a proper UUID library
func generateUUID() string {
	return time.Now().Format("20060102150405.000000")
}
