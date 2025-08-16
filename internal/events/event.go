package events

import "time"

// TaskCreatedEvent represents events published by the task service
type TaskCreatedEvent struct {
	TaskID          uint64           `json:"task_id"`
	OfAccountID     uint64           `json:"of_account_id"`
	FileName        string           `json:"file_name"`
	SourceURL       string           `json:"source_url"`
	SourceType      string           `json:"source_type"`
	SourceAuth      *AuthConfig      `json:"source_auth,omitempty"`
	DownloadOptions *DownloadOptions `json:"download_options,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
	Checksum        *ChecksumInfo    `json:"checksum,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
}

// TaskStatusUpdatedEvent represents status changes from download service
type TaskStatusUpdatedEvent struct {
	TaskID    uint64     `json:"task_id"`
	Status    TaskStatus `json:"status"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// TaskProgressUpdatedEvent represents progress updates from download service
type TaskProgressUpdatedEvent struct {
	TaskID          uint64    `json:"task_id"`
	Progress        float64   `json:"progress"`
	DownloadedBytes int64     `json:"downloaded_bytes"`
	TotalBytes      int64     `json:"total_bytes"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TaskCompletedEvent represents task completion from download service
type TaskCompletedEvent struct {
	TaskID      uint64        `json:"task_id"`
	FileName    string        `json:"file_name"`
	FileSize    int64         `json:"file_size"`
	ContentType string        `json:"content_type"`
	Checksum    *ChecksumInfo `json:"checksum,omitempty"`
	StorageType string        `json:"storage_type"`
	StorageKey  string        `json:"storage_key"`
	CompletedAt time.Time     `json:"completed_at"`
}

// TaskFailedEvent represents task failure from download service
type TaskFailedEvent struct {
	TaskID   uint64    `json:"task_id"`
	Error    string    `json:"error"`
	FailedAt time.Time `json:"failed_at"`
}

// TaskRetriedEvent represents a retry attempt for a task
type TaskRetriedEvent struct {
	TaskID     uint64    `json:"task_id"`
	RetryCount uint32    `json:"retry_count"`
	Reason     string    `json:"reason"`
	RetriedAt  time.Time `json:"retried_at"`
}

// TaskPausedEvent represents task pause requests
type TaskPausedEvent struct {
	TaskID   uint64    `json:"task_id"`
	PausedAt time.Time `json:"paused_at"`
}

// TaskResumedEvent represents task resume requests
type TaskResumedEvent struct {
	TaskID    uint64    `json:"task_id"`
	ResumedAt time.Time `json:"resumed_at"`
}

// TaskCancelledEvent represents task cancellation requests
type TaskCancelledEvent struct {
	TaskID      uint64    `json:"task_id"`
	CancelledAt time.Time `json:"cancelled_at"`
}

// EventType enum for task events
type (
	EventType  string
	TaskStatus string
)

func (e EventType) String() string {
	return string(e)
}

func (s TaskStatus) String() string {
	return string(s)
}

func TaskStatusValue(status string) TaskStatus {
	switch status {
	case "PENDING":
		return StatusPending
	case "DOWNLOADING":
		return StatusDownloading
	case "STORING":
		return StatusStoring
	case "COMPLETED":
		return StatusCompleted
	case "FAILED":
		return StatusFailed
	case "CANCELLED":
		return StatusCancelled
	case "PAUSED":
		return StatusPaused
	default:
		return TaskStatus(status)
	}
}

const (
	EventTaskCreated         EventType = "task_created"
	EventTaskStatusUpdated   EventType = "task_status_updated"
	EventTaskProgressUpdated EventType = "task_progress_updated"
	EventTaskCompleted       EventType = "task_completed"
	EventTaskFailed          EventType = "task_failed"
	EventTaskPaused          EventType = "task_paused"
	EventTaskResumed         EventType = "task_resumed"
	EventTaskCancelled       EventType = "task_cancelled"
	EventTaskRetried         EventType = "task_retried"

	StatusPending     TaskStatus = "PENDING"
	StatusDownloading TaskStatus = "DOWNLOADING"
	StatusStoring     TaskStatus = "STORING"
	StatusCompleted   TaskStatus = "COMPLETED"
	StatusFailed      TaskStatus = "FAILED"
	StatusCancelled   TaskStatus = "CANCELLED"
	StatusPaused      TaskStatus = "PAUSED"
)

// DownloadOptions configures download behavior
type DownloadOptions struct {
	Concurrency int    `json:"concurrency"`
	MaxSpeed    *int64 `json:"max_speed,omitempty"`
	MaxRetries  int    `json:"max_retries"`
	Timeout     *int   `json:"timeout,omitempty"`
}

// AuthConfig for authenticated sources
type AuthConfig struct {
	Type     string            `json:"type"`
	Username string            `json:"username,omitempty"`
	Password string            `json:"password,omitempty"`
	Token    string            `json:"token,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

type ChecksumInfo struct {
	ChecksumType  string `json:"checksum_type"`
	ChecksumValue string `json:"checksum_value"`
}
