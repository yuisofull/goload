package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Local implements Backend and Presigner using the local filesystem.
// It is intended for the pocket edition where objects live under a user-owned
// data directory instead of MinIO/S3.
type Local struct {
	root string
}

type localObjectMetadata struct {
	FileMetadata

	StoredAt time.Time `json:"stored_at"`
}

// NewLocalBackend creates a filesystem-backed storage backend rooted at dir.
func NewLocalBackend(root string) (*Local, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("local storage root is empty")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create local storage root: %w", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage root: %w", err)
	}
	return &Local{root: absRoot}, nil
}

func (l *Local) Store(ctx context.Context, key string, reader io.Reader, metadata *FileMetadata) error {
	objectPath, err := l.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		return fmt.Errorf("create local storage directory: %w", err)
	}

	tmpPath := objectPath + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create local storage object: %w", err)
	}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write local storage object: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close local storage object: %w", closeErr)
	}
	if err := os.Rename(tmpPath, objectPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("commit local storage object: %w", err)
	}

	meta := localObjectMetadata{StoredAt: time.Now()}
	if metadata != nil {
		meta.FileMetadata = *metadata
	}
	if meta.FileName == "" {
		meta.FileName = filepath.Base(key)
	}
	meta.StorageKey = key
	meta.LastModified = time.Now()
	if meta.FileSize <= 0 {
		meta.FileSize = written
	}
	if meta.ContentType == "" {
		meta.ContentType = "application/octet-stream"
	}
	return l.writeMetadata(objectPath, &meta)
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	objectPath, err := l.objectPath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(objectPath)
}

func (l *Local) GetWithRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error) {
	if start < 0 || (end >= 0 && end < start) {
		return nil, fmt.Errorf("invalid range %d-%d", start, end)
	}
	objectPath, err := l.objectPath(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(objectPath)
	if err != nil {
		return nil, err
	}
	if end < 0 {
		stat, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		end = stat.Size() - 1
		if start > end {
			file.Close()
			return nil, fmt.Errorf("range start %d beyond file size %d", start, stat.Size())
		}
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	return &localRangeReadCloser{file: file, remaining: end - start + 1}, nil
}

func (l *Local) GetInfo(ctx context.Context, key string) (*FileMetadata, error) {
	objectPath, err := l.objectPath(key)
	if err != nil {
		return nil, err
	}
	stat, err := os.Stat(objectPath)
	if err != nil {
		return nil, err
	}

	meta := &FileMetadata{
		FileName:     filepath.Base(key),
		FileSize:     stat.Size(),
		ContentType:  "application/octet-stream",
		LastModified: stat.ModTime(),
		StorageKey:   key,
		Bucket:       "local",
		Headers:      map[string]string{},
	}
	loaded, err := l.readMetadata(objectPath)
	if err == nil && loaded != nil {
		meta = loaded
	}
	return meta, nil
}

func (l *Local) Exists(ctx context.Context, key string) (bool, error) {
	objectPath, err := l.objectPath(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(objectPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (l *Local) Delete(ctx context.Context, key string) error {
	objectPath, err := l.objectPath(key)
	if err != nil {
		return err
	}
	_ = os.Remove(objectPath)
	_ = os.Remove(l.metadataPath(objectPath))
	return nil
}

func (l *Local) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	objectPath, err := l.objectPath(key)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(objectPath)
	absPath = filepath.ToSlash(absPath)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: absPath}).String(), nil
}

// PathForKey returns the absolute filesystem path for a stored object key.
func (l *Local) PathForKey(key string) (string, error) {
	objectPath, err := l.objectPath(key)
	if err != nil {
		return "", err
	}
	return filepath.Abs(objectPath)
}

type localRangeReadCloser struct {
	file      *os.File
	remaining int64
}

func (r *localRangeReadCloser) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.file.Read(p)
	r.remaining -= int64(n)
	if err == nil && r.remaining <= 0 {
		return n, io.EOF
	}
	return n, err
}

func (r *localRangeReadCloser) Close() error { return r.file.Close() }

func (l *Local) objectPath(key string) (string, error) {
	cleaned, err := l.cleanKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(l.root, filepath.FromSlash(cleaned)), nil
}

func (l *Local) metadataPath(objectPath string) string { return objectPath + ".meta.json" }

func (l *Local) writeMetadata(objectPath string, meta *localObjectMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode local metadata: %w", err)
	}
	return os.WriteFile(l.metadataPath(objectPath), data, 0o644)
}

func (l *Local) readMetadata(objectPath string) (*FileMetadata, error) {
	data, err := os.ReadFile(l.metadataPath(objectPath))
	if err != nil {
		return nil, err
	}
	var meta localObjectMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.Headers == nil {
		meta.Headers = map[string]string{}
	}
	if meta.Bucket == "" {
		meta.Bucket = "local"
	}
	return &meta.FileMetadata, nil
}

func (l *Local) cleanKey(key string) (string, error) {
	key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	if key == "" {
		return "", errors.New("storage key is empty")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid storage key %q", key)
		}
	}
	cleaned := filepath.Clean("/" + key)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	return cleaned, nil
}
