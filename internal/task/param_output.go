package task

import (
	"strings"
	"time"

	"github.com/yuisofull/goload/internal/storage"
)

type CreateTaskParam struct {
	ID              uint64           `json:"id"`
	OfAccountID     uint64           `json:"of_account_id"`
	FileName        string           `json:"file_name"`
	SourceURL       string           `json:"source_url"`
	SourceType      SourceType       `json:"source_type"`
	SourceAuth      *AuthConfig      `json:"source_auth,omitempty"`
	Checksum        *ChecksumInfo    `json:"checksum,omitempty"`
	DownloadOptions *DownloadOptions `json:"download_options,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

type UpdateTaskParam struct {
	TaskID   uint64
	Status   TaskStatus
	Progress *DownloadProgress
	Error    string
	Checksum *ChecksumInfo
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

type (
	SourceType string
	TaskStatus string
)

const (
	// SourceType
	SourceHTTP       SourceType = "HTTP"
	SourceHTTPS      SourceType = "HTTPS"
	SourceFTP        SourceType = "FTP"
	SourceSFTP       SourceType = "SFTP"
	SourceBitTorrent SourceType = "BITTORRENT"

	// TaskStatus
	StatusPending     TaskStatus = "PENDING"
	StatusDownloading TaskStatus = "DOWNLOADING"
	StatusStoring     TaskStatus = "STORING"
	StatusCompleted   TaskStatus = "COMPLETED"
	StatusFailed      TaskStatus = "FAILED"
	StatusCancelled   TaskStatus = "CANCELLED"
	StatusPaused      TaskStatus = "PAUSED"
)

func ToSourceType(src string) SourceType {
	src = strings.ToUpper(src)
	switch src {
	case "HTTP":
		return SourceHTTP
	case "HTTPS":
		return SourceHTTPS
	case "FTP":
		return SourceFTP
	case "SFTP":
		return SourceSFTP
	case "BITTORRENT":
		return SourceBitTorrent
	default:
		return SourceHTTP
	}
}

// Task represents a file download and storage task
type Task struct {
	ID              uint64            `json:"id"`
	OfAccountID     uint64            `json:"of_account_id"`
	FileName        string            `json:"file_name"`
	SourceURL       string            `json:"source_url"`
	SourceType      SourceType        `json:"source_type"`
	SourceAuth      *AuthConfig       `json:"source_auth,omitempty"`
	StorageType     storage.Type      `json:"storage_type"`
	StoragePath     string            `json:"storage_path"`
	Checksum        *ChecksumInfo     `json:"checksum,omitempty"`
	DownloadOptions *DownloadOptions  `json:"download_options,omitempty"`
	Status          TaskStatus        `json:"status"`
	Progress        *DownloadProgress `json:"progress,omitempty"`
	ErrorMessage    *string           `json:"error_message,omitempty"`
	Metadata        map[string]any    `json:"metadata,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
}

// DownloadProgress tracks download progress
type DownloadProgress struct {
	Progress        float64 `json:"progress"`
	DownloadedBytes int64   `json:"downloaded_bytes"`
	TotalBytes      int64   `json:"total_bytes"`
}

// DownloadOptions configures download behavior
type DownloadOptions struct {
	Concurrency int    `json:"concurrency" db:"concurrency"`
	MaxSpeed    *int64 `json:"max_speed,omitempty" db:"max_speed"`
	MaxRetries  int    `json:"max_retries" db:"max_retries"`
	Timeout     *int   `json:"timeout,omitempty" db:"timeout"`
}

// AuthConfig for authenticated sources
type AuthConfig struct {
	Type     string            `json:"type"`
	Username string            `json:"username,omitempty"`
	Password string            `json:"password,omitempty"`
	Token    string            `json:"token,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
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

type ChecksumInfo struct {
	ChecksumType  string `json:"checksum_type"`
	ChecksumValue string `json:"checksum_value"`
}
