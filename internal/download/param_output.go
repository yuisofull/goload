package download

import (
	"io"
)

// FileStreamRequest for streaming files to clients
type FileStreamRequest struct {
	TaskID     uint64            `json:"task_id"`
	Range      *RangeRequest     `json:"range,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	ClientInfo *ClientInfo       `json:"client_info,omitempty"`
}

// RangeRequest for partial content requests
type RangeRequest struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

// ClientInfo contains client information
type ClientInfo struct {
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	ClientID  string `json:"client_id"`
}

// FileStreamResponse contains streaming response data
type FileStreamResponse struct {
	Reader      io.ReadCloser     `json:"-"`
	ContentType string            `json:"content_type"`
	FileSize    int64             `json:"file_size"`
	FileName    string            `json:"file_name"`
	Headers     map[string]string `json:"headers"`
	StatusCode  int               `json:"status_code"`
}

type FileMetadata struct {
	FileName    string
	FileSize    int64
	ContentType string
	MD5Hash     string
	LocalPath   string
	Headers     map[string]string
}
