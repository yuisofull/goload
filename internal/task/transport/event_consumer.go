package tasktransport

import (
	"context"
	"encoding/json"
	"log"

	"github.com/yuisofull/goload/internal/errors"
	"github.com/yuisofull/goload/internal/events"
	"github.com/yuisofull/goload/internal/task"
	"github.com/yuisofull/goload/pkg/message"
)

// EventConsumer handles events from other services
type EventConsumer struct {
	taskService task.Service
	subscriber  message.Subscriber
}

// NewEventConsumer creates a new event consumer for task service
func NewEventConsumer(taskService task.Service, subscriber message.Subscriber) *EventConsumer {
	return &EventConsumer{
		taskService: taskService,
		subscriber:  subscriber,
	}
}

// Start begins consuming events
func (ec *EventConsumer) Start(ctx context.Context) error {
	progressCh, err := ec.subscriber.Subscribe(ctx, "task.progress.updated")
	if err != nil {
		return err
	}

	completedCh, err := ec.subscriber.Subscribe(ctx, "task.completed")
	if err != nil {
		return err
	}

	failedCh, err := ec.subscriber.Subscribe(ctx, "task.failed")
	if err != nil {
		return err
	}

	// Start goroutines to handle each event type
	go ec.handleProgressUpdates(ctx, progressCh)
	go ec.handleCompletions(ctx, completedCh)
	go ec.handleFailures(ctx, failedCh)

	return nil
}

// handleProgressUpdates processes progress update messages
func (ec *EventConsumer) handleProgressUpdates(ctx context.Context, ch <-chan *message.Message) {
	for msg := range ch {
		if err := ec.handleTaskProgressUpdated(ctx, msg); err != nil {
			log.Printf("Error handling progress update: %v", err)
			msg.Nack()
		} else {
			msg.Ack()
		}
	}
}

// handleCompletions processes completion messages
func (ec *EventConsumer) handleCompletions(ctx context.Context, ch <-chan *message.Message) {
	for msg := range ch {
		if err := ec.handleTaskCompleted(ctx, msg); err != nil {
			log.Printf("Error handling task completion: %v", err)
			msg.Nack()
		} else {
			msg.Ack()
		}
	}
}

// handleFailures processes failure messages
func (ec *EventConsumer) handleFailures(ctx context.Context, ch <-chan *message.Message) {
	for msg := range ch {
		if err := ec.handleTaskFailed(ctx, msg); err != nil {
			log.Printf("Error handling task failure: %v", err)
			msg.Nack()
		} else {
			msg.Ack()
		}
	}
}

// handleTaskProgressUpdated processes progress updates from download service
func (ec *EventConsumer) handleTaskProgressUpdated(ctx context.Context, msg *message.Message) error {
	var event events.TaskProgressUpdatedEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		log.Printf("Failed to unmarshal TaskProgressUpdatedEvent: %v", err)
		return err
	}

	// Update task progress in the database
	progress := task.DownloadProgress{
		Progress:        event.Progress,
		DownloadedBytes: event.DownloadedBytes,
		TotalBytes:      event.TotalBytes,
	}

	if err := ec.taskService.UpdateTaskProgress(ctx, event.TaskID, progress); err != nil {
		log.Printf("Failed to update task progress for task %d: %v", event.TaskID, err)
		return err
	}

	return nil
}

// handleTaskCompleted processes task completion events from download service
func (ec *EventConsumer) handleTaskCompleted(ctx context.Context, msg *message.Message) error {
	var event events.TaskCompletedEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		log.Printf("Failed to unmarshal TaskCompletedEvent: %v", err)
		return err
	}

	// Mark task as completed - the service interface only needs taskID
	if err := ec.taskService.CompleteTask(ctx, event.TaskID); err != nil {
		log.Printf("Failed to complete task %d: %v", event.TaskID, err)
		return err
	}

	// Store additional file metadata using UpdateTaskMetadata
	metadata := map[string]interface{}{
		"fileName":    event.FileName,
		"fileSize":    event.FileSize,
		"contentType": event.ContentType,
		"checksum":    event.Checksum,
		"storageKey":  event.StorageKey,
		"completedAt": event.CompletedAt,
	}

	if err := ec.taskService.UpdateTaskMetadata(ctx, event.TaskID, metadata); err != nil {
		log.Printf("Failed to update task metadata for task %d: %v", event.TaskID, err)
		return err
	}

	return nil
}

// handleTaskFailed processes task failure events from download service
func (ec *EventConsumer) handleTaskFailed(ctx context.Context, msg *message.Message) error {
	var event events.TaskFailedEvent
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		log.Printf("Failed to unmarshal TaskFailedEvent: %v", err)
		return err
	}

	// Create error from the event
	taskErr := &errors.Error{
		Code:    errors.ErrCodeInternal,
		Message: event.Error,
	}

	if err := ec.taskService.UpdateTaskError(ctx, event.TaskID, taskErr); err != nil {
		log.Printf("Failed to update task error for task %d: %v", event.TaskID, err)
		return err
	}

	return nil
}
