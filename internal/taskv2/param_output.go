package task

import (
	"time"
)

type CreateTaskParam struct {
	OfAccountID uint64                 `json:"of_account_id"`
	Name        string                 `json:"name" validate:"required"`
	Description string                 `json:"description"`
	SourceURL   string                 `json:"source_url" validate:"required"`
	SourceType  SourceType             `json:"source_type" validate:"required"`
	SourceAuth  *AuthConfig            `json:"source_auth,omitempty"`
	Options     *DownloadOptions       `json:"options"`
	MaxRetries  int32                  `json:"max_retries"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type UpdateTaskParam struct {
	TaskID     uint64
	Status     TaskStatus
	Progress   *DownloadProgress
	FileInfo   *FileInfo
	Error      string
	RetryCount int32
}

type ListTasksParam struct {
	Filter  *TaskFilter
	Offset  int32
	Limit   int32
	SortBy  string
	SortAsc bool
}

type PauseTaskParam struct {
	ID          uint64
	OfAccountID uint64
}

type TaskOutput struct {
	Task *Task
}

type ListTasksOutput struct {
	Tasks []*Task
	Total int32
}

type SourceType string
type StorageType string
type TaskStatus string

const (
	// SourceType
	SourceHTTP       SourceType = "HTTP"
	SourceHTTPS      SourceType = "HTTPS"
	SourceFTP        SourceType = "FTP"
	SourceSFTP       SourceType = "SFTP"
	SourceBitTorrent SourceType = "BITTORRENT"

	// StorageType
	StorageLocal StorageType = "LOCAL"
	StorageMinIO StorageType = "MINIO"
	StorageS3    StorageType = "S3"

	// TaskStatus
	StatusPending     TaskStatus = "PENDING"
	StatusDownloading TaskStatus = "DOWNLOADING"
	StatusStoring     TaskStatus = "STORING"
	StatusCompleted   TaskStatus = "COMPLETED"
	StatusFailed      TaskStatus = "FAILED"
	StatusCancelled   TaskStatus = "CANCELLED"
	StatusPaused      TaskStatus = "PAUSED"
)

// Task represents a file download and storage task
type Task struct {
	ID          uint64                 `json:"id"`
	OfAccountID uint64                 `json:"of_account_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	SourceURL   string                 `json:"source_url"`
	SourceType  SourceType             `json:"source_type"`
	SourceAuth  *AuthConfig            `json:"source_auth,omitempty"`
	StorageType StorageType            `json:"storage_type"`
	StoragePath string                 `json:"storage_path"`
	Status      TaskStatus             `json:"status"`
	FileInfo    *FileInfo              `json:"file_info,omitempty"`
	Progress    *DownloadProgress      `json:"progress,omitempty"`
	Options     *DownloadOptions       `json:"options"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Error       string                 `json:"error,omitempty"`
	RetryCount  int32                  `json:"retry_count"`
	MaxRetries  int32                  `json:"max_retries"`
	Tags        []string               `json:"tags"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// FileInfo contains information about the stored file
type FileInfo struct {
	FileName    string    `json:"file_name"`
	FileSize    int64     `json:"file_size"`
	ContentType string    `json:"content_type"`
	MD5Hash     string    `json:"md5_hash"`
	StorageKey  string    `json:"storage_key"`
	StoredAt    time.Time `json:"stored_at"`
}

// DownloadProgress tracks download progress
type DownloadProgress struct {
	BytesDownloaded int64         `json:"bytes_downloaded"`
	TotalBytes      int64         `json:"total_bytes"`
	Speed           int64         `json:"speed"` // bytes per second
	ETA             time.Duration `json:"eta"`
	Percentage      float64       `json:"percentage"`
}

// DownloadOptions configures download behavior
type DownloadOptions struct {
	ChunkSize    int64         `json:"chunk_size"`
	MaxRetries   int           `json:"max_retries"`
	Timeout      time.Duration `json:"timeout"`
	Resume       bool          `json:"resume"`
	ChecksumType string        `json:"checksum_type"`
}

// AuthConfig for authenticated sources
type AuthConfig struct {
	Username string            `json:"username"`
	Password string            `json:"password"`
	Token    string            `json:"token"`
	Headers  map[string]string `json:"headers"`
}

// TaskFilter TaskFilter for querying tasks
type TaskFilter struct {
	OfAccountID uint64
	Status      []TaskStatus `json:"status"`
	Tags        []string     `json:"tags"`
	SourceType  []SourceType `json:"source_type"`
	CreatedAt   *TimeRange   `json:"created_at"`
	Search      string       `json:"search"`
}

// TimeRange for date filtering
type TimeRange struct {
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`
}

// StorageStats contains storage statistics
type StorageStats struct {
	TotalFiles int64 `json:"total_files"`
	TotalSize  int64 `json:"total_size"`
	UsedSpace  int64 `json:"used_space"`
}

// StorageConfig contains storage configuration
type StorageConfig struct {
	BasePath      string `json:"base_path"`
	BucketName    string `json:"bucket_name"`
	MaxFileSize   int64  `json:"max_file_size"`
	RetentionDays int    `json:"retention_days"`
}

// TaskEvent represents events published by the task service
type Event struct {
	Type      EventType              `json:"type"`
	TaskID    string                 `json:"task_id"`
	Task      *Task                  `json:"task,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EventType enum for task events
type EventType string

const (
	EventTaskCreated   EventType = "task_created"
	EventTaskStarted   EventType = "task_started"
	EventTaskPaused    EventType = "task_paused"
	EventTaskResumed   EventType = "task_resumed"
	EventTaskCancelled EventType = "task_cancelled"
	EventTaskRetried   EventType = "task_retried"
	EventTaskCompleted EventType = "task_completed"
	EventTaskFailed    EventType = "task_failed"
)
