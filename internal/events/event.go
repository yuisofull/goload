package events

// TaskCreatedEvent represents events published by the task service
type TaskCreatedEvent struct {
	SourceURL  string                 `json:"source_url"`
	SourceAuth *AuthConfig            `json:"source_auth,omitempty"`
	TaskID     uint64                 `json:"task_id"`
	FileName   string                 `json:"file_name"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Checksum   *ChecksumInfo          `json:"checksum,omitempty"`
}

// EventType enum for task events
type EventType string

const (
	EventTaskStarted   EventType = "task_started"
	EventTaskCreated   EventType = "task_created"
	EventTaskPaused    EventType = "task_paused"
	EventTaskResumed   EventType = "task_resumed"
	EventTaskCancelled EventType = "task_cancelled"
	EventTaskRetried   EventType = "task_retried"
	EventTaskCompleted EventType = "task_completed"
	EventTaskFailed    EventType = "task_failed"
)

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
