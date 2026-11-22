package task

import (
	"context"
	"encoding/json"
	"time"

	"github.com/yuisofull/goload/internal/events"
)

const (
	// OutboxDestinationDispatch marks download commands (TaskCreated). Later
	// work routes this destination to a dedicated broker; for now every event
	// is published on the existing transport.
	OutboxDestinationDispatch = "dispatch"
	// OutboxDestinationLifecycle marks status, progress, completed and failed
	// notifications. Only the completion event is produced so far.
	OutboxDestinationLifecycle = "lifecycle"

	// OutboxEventTaskCreated is the event_type of a TaskCreated outbox row and
	// the "eventType" metadata value of the published message.
	OutboxEventTaskCreated = "TaskCreated"
	// OutboxEventTaskCompleted is the event_type of the lifecycle row written
	// by CompleteAttempt in the transaction that completes the task.
	OutboxEventTaskCompleted = "TaskCompleted"
	// OutboxEventTaskFailed is the event_type of the lifecycle row written by
	// FailAttempt in the transaction that marks the task terminally failed.
	OutboxEventTaskFailed = "TaskFailed"
)

// OutboxEvent is one row of the task_outbox table: an event that must be
// published after the transaction that wrote it commits.
type OutboxEvent struct {
	// ID is the stable unique event id (UUID). It is reused as the message
	// UUID on every publish so consumers can dedupe on it later.
	ID string
	// AggregateID is the id of the task the event belongs to.
	AggregateID uint64
	// Destination is OutboxDestinationDispatch or OutboxDestinationLifecycle.
	Destination string
	// EventType selects the topic, for example OutboxEventTaskCreated.
	EventType string
	// Payload is the serialized event body. It is published verbatim.
	Payload []byte
	// CreatedAt is when the event was recorded (UTC).
	CreatedAt time.Time
	// SentAt is nil until the relay has published the event.
	SentAt *time.Time
}

// OutboxCursor positions FetchUnsent. The zero value starts at the oldest row.
type OutboxCursor struct {
	CreatedAt time.Time
	ID        string
}

// OutboxRepository persists outbox events next to the task rows.
type OutboxRepository interface {
	// InsertUnsent stores event with sent_at NULL. When ctx carries a
	// transaction started by TxManager.DoInTx the insert joins it.
	InsertUnsent(ctx context.Context, event OutboxEvent) error
	// FetchUnsent returns unsent events whose (created_at, id) is strictly
	// greater than after (all unsent events when after is the zero value),
	// ordered by (created_at ASC, id ASC), up to limit. A limit that is zero
	// or negative returns no events and no error.
	FetchUnsent(ctx context.Context, after OutboxCursor, limit int) ([]OutboxEvent, error)
	// MarkSent sets sent_at for the event. It is a no-op for an unknown or
	// already-sent id.
	MarkSent(ctx context.Context, id string) error
	// OutboxBacklog returns the number of unsent events and the creation time
	// of the oldest one (zero time when there are none). It drives the backlog
	// gauges; a single COUNT + MIN, never derived from a fetched page.
	OutboxBacklog(ctx context.Context) (unsent int64, oldest time.Time, err error)
}

// NewTaskCreatedOutboxEvent builds the outbox row for a TaskCreated event. The
// payload is the events.TaskCreatedEvent JSON the download service already
// consumes from the task.created topic.
func NewTaskCreatedOutboxEvent(t *Task) (OutboxEvent, error) {
	payload, err := json.Marshal(events.TaskCreatedEvent{
		TaskID:          t.ID,
		OfAccountID:     t.OfAccountID,
		FileName:        t.FileName,
		SourceURL:       t.SourceURL,
		SourceType:      string(t.SourceType),
		SourceAuth:      convertAuthConfig(t.SourceAuth),
		DownloadOptions: convertDownloadOptions(t.DownloadOptions),
		Metadata:        t.Metadata,
		CreatedAt:       t.CreatedAt,
	})
	if err != nil {
		return OutboxEvent{}, err
	}

	return OutboxEvent{
		ID:          generateUUID(),
		AggregateID: t.ID,
		Destination: OutboxDestinationDispatch,
		EventType:   OutboxEventTaskCreated,
		Payload:     payload,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// NewTaskCompletedOutboxEvent builds the lifecycle outbox row announcing that
// an attempt completed a task. The payload is the events.TaskCompletedEvent
// JSON already used on the task.completed topic. It is a notification only:
// the task row is updated by the transaction that stores this event, and no
// consumer may apply it to the task row again.
func NewTaskFailedOutboxEvent(taskID uint64, message string, failedAt time.Time) (OutboxEvent, error) {
	payload, err := json.Marshal(events.TaskFailedEvent{TaskID: taskID, Error: message, FailedAt: failedAt})
	if err != nil {
		return OutboxEvent{}, err
	}
	return OutboxEvent{
		ID:          generateUUID(),
		AggregateID: taskID,
		Destination: OutboxDestinationLifecycle,
		EventType:   OutboxEventTaskFailed,
		Payload:     payload,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func NewTaskCompletedOutboxEvent(taskID uint64, result events.AttemptResult, completedAt time.Time) (OutboxEvent, error) {
	event := events.TaskCompletedEvent{
		TaskID:      taskID,
		FileName:    result.FileName,
		FileSize:    result.FileSize,
		ContentType: result.ContentType,
		StorageType: result.StorageType.String(),
		StorageKey:  result.StorageKey,
		CompletedAt: completedAt,
	}
	if result.Checksum != nil {
		event.Checksum = &events.ChecksumInfo{
			ChecksumType:  result.Checksum.ChecksumType,
			ChecksumValue: result.Checksum.ChecksumValue,
		}
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return OutboxEvent{}, err
	}

	return OutboxEvent{
		ID:          generateUUID(),
		AggregateID: taskID,
		Destination: OutboxDestinationLifecycle,
		EventType:   OutboxEventTaskCompleted,
		Payload:     payload,
		CreatedAt:   time.Now().UTC(),
	}, nil
}
