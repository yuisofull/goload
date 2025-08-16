package download

import "time"

// TaskRequest is the internal download service representation of a task to execute.
type TaskRequest struct {
	TaskID          uint64
	OfAccountID     uint64
	FileName        string
	SourceURL       string
	SourceType      string
	SourceAuth      *AuthConfig
	DownloadOptions *DownloadOptions
	Metadata        map[string]any
	Checksum        *ChecksumInfo
	CreatedAt       time.Time
}

// AuthConfig is internal auth config for the download service.
type AuthConfig struct {
	Type     string
	Username string
	Password string
	Token    string
	Headers  map[string]string
}

// DownloadOptions configures download behaviour inside download service.
type DownloadOptions struct {
	Concurrency int
	MaxSpeed    *int64
	MaxRetries  int
	Timeout     *int
}

// ChecksumInfo mirrors checksum info for internal use.
type ChecksumInfo struct {
	ChecksumType  string
	ChecksumValue string
}

// Progress is internal representation of progress updates.
type Progress struct {
	Progress        float64
	DownloadedBytes int64
	TotalBytes      int64
	UpdatedAt       time.Time
}
