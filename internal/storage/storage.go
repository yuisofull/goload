package storage

import (
	"context"
	"github.com/yuisofull/goload/internal/task"
	"io"
	"time"
)

type Writer interface {
	Store(ctx context.Context, key string, reader io.Reader, metadata *FileMetadata) error
	Exists(ctx context.Context, key string) (bool, error)
	Delete(ctx context.Context, key string) error // optional, for cleanup
}

type Reader interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	GetWithRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error)
	GetInfo(ctx context.Context, key string) (*task.FileInfo, error)
	Exists(ctx context.Context, key string) (bool, error)
}

// Backend interface for different storage systems
type Backend interface {
	Writer
	Reader
}

// FileMetadata contains file metadata from source
type FileMetadata struct {
	FileName     string            `json:"file_name"`
	FileSize     int64             `json:"file_size"`
	ContentType  string            `json:"content_type"`
	LastModified time.Time         `json:"last_modified"`
	Headers      map[string]string `json:"headers"`
}
