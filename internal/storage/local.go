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
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// Local implements Backend and Presigner using the local filesystem.
// It is intended for the pocket edition where objects live under a user-owned
// data directory instead of MinIO/S3.
type Local struct {
	root string

	defaultExpiry time.Duration // 0 means no expiry policy
	logger        log.Logger
}

// LocalOption configures a Local backend.
type LocalOption func(*Local)

// WithLocalExpiry sets a default expiry duration for stored objects. When non-zero,
// the backend will automatically delete objects after the equivalent number of days.
// Individual Store calls can override this on a per-object basis via FileMetadata.Expiry.
func WithLocalExpiry(d time.Duration) LocalOption {
	return func(l *Local) { l.defaultExpiry = d }
}

func WithLocalLogger(logger log.Logger) LocalOption {
	return func(l *Local) {
		l.logger = logger
	}
}

type localObjectMetadata struct {
	FileMetadata

	StoredAt time.Time `json:"stored_at"`
}

// NewLocalBackend creates a filesystem-backed storage backend rooted at dir.
func NewLocalBackend(root string, opts ...LocalOption) (*Local, error) {
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
	l := &Local{root: absRoot}
	for _, o := range opts {
		o(l)
	}
	return l, nil
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
	if meta.Headers == nil {
		meta.Headers = map[string]string{}
	}
	if meta.ExpireAt.IsZero() && l.defaultExpiry > 0 {
		meta.ExpireAt = meta.StoredAt.Add(l.defaultExpiry)
	}

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			deletedFiles, err := l.Reap(context.Background())
			if err != nil {
				level.Error(l.logger).Log("msg", "error during local storage reap", "err", err)
			} else {
				var deletedFilesStr string
				if len(deletedFiles) > 0 {
					deletedFilesStr = strings.Join(deletedFiles, ", ")
				}
				level.Info(l.logger).Log("msg", "finished reaping expired objects", "deletedFiles", deletedFilesStr)
			}
		}
	}()

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

// Reap traverses the storage root and deletes objects that have expired.
// It returns the list of deleted files and any error encountered during traversal.
func (l *Local) Reap(ctx context.Context) ([]string, error) {
	var deletedFiles []string
	now := time.Now()

	err := filepath.WalkDir(l.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and metadata files
		if d.IsDir() || strings.HasSuffix(path, ".meta.json") {
			return nil
		}

		// Read metadata for the current file
		// Note: readMetadata expects the object path, which we have here.
		meta, err := l.readInternalMetadata(path)
		if err != nil {
			// If metadata is missing or corrupt, we skip it to avoid
			// deleting data that might still be valid but orphaned.
			return nil
		}

		if meta.ExpireAt.IsZero() && l.defaultExpiry > 0 {
			meta.ExpireAt = meta.StoredAt.Add(l.defaultExpiry)
		}

		if now.After(meta.ExpireAt) {
			// Extract key to use the existing Delete logic
			relPath, err := filepath.Rel(l.root, path)
			if err != nil {
				return nil
			}
			key := filepath.ToSlash(relPath)

			if err := l.Delete(ctx, key); err == nil {
				deletedFiles = append(deletedFiles, key)
			}
		}

		return nil
	})

	return deletedFiles, err
}

// Internal helper to get the full metadata including StoredAt
func (l *Local) readInternalMetadata(objectPath string) (*localObjectMetadata, error) {
	data, err := os.ReadFile(l.metadataPath(objectPath))
	if err != nil {
		return nil, err
	}
	var meta localObjectMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

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
	if slices.Contains(strings.Split(key, "/"), "..") {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	cleaned := filepath.Clean("/" + key)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("invalid storage key %q", key)
	}
	return cleaned, nil
}
