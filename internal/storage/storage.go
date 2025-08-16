package storage

import (
	"context"
	"io"
	"strings"
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
	GetInfo(ctx context.Context, key string) (*FileMetadata, error)
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
	StorageKey   string            `json:"storage_key"`
	Bucket       string            `json:"bucket"`
	ChecksumType string            `json:"checksum_type"`
	Checksum     []byte            `json:"checksum"`
	Headers      map[string]string `json:"headers"`
}

// Type defines the type of storage backend
type Type string

const (
	TypeLocal  Type = "local"
	TypeS3     Type = "s3"
	TypeGCS    Type = "gcs"
	TypeAzure  Type = "azure"
	TypeFTP    Type = "ftp"
	TypeHTTP   Type = "http"
	TypeMemory Type = "memory"
	TypeMinio  Type = "minio"
)

func (s Type) String() string {
	return string(s)
}

func TypeValue(s string) Type {
	s = strings.ToLower(s)
	switch s {
	case "local":
		return TypeLocal
	case "s3":
		return TypeS3
	case "gcs":
		return TypeGCS
	case "azure":
		return TypeAzure
	case "ftp":
		return TypeFTP
	case "http":
		return TypeHTTP
	case "memory":
		return TypeMemory
	case "minio":
		return TypeMinio
	default:
		return TypeLocal
	}
}