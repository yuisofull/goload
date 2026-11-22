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
	// Generation is the attempt generation returned by the task service's
	// ClaimTask (1 for the first claim). It namespaces the storage key. Zero
	// means the task was not claimed: the legacy event-driven path.
	Generation uint64
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
	// KnownMetadata, when set, is metadata the caller already fetched. A
	// downloader reuses it instead of making its own probe (HEAD) request,
	// which would otherwise count toward the transfer timer.
	KnownMetadata *FileMetadata
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
