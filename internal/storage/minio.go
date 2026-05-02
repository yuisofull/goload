package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

// Minio implements storage.Backend and storage.Presigner using MinIO.
type Minio struct {
	client        *minio.Client
	bucket        string
	defaultExpiry time.Duration // 0 means no expiry policy
}

const userMetaExpiryAt = "expiry-at"

var keyHashSuffixRe = regexp.MustCompile(`^(.*)-[0-9a-fA-F]{8}$`)

// MinioOption configures a Minio backend.
type MinioOption func(*Minio)

// WithExpiry sets a default expiry duration for stored objects. When non-zero,
// a bucket lifecycle rule is applied so MinIO automatically deletes objects
// after the equivalent number of days. Individual Store calls can override
// on a per-object basis via FileMetadata.Expiry.
func WithExpiry(d time.Duration) MinioOption {
	return func(m *Minio) { m.defaultExpiry = d }
}

// NewMinioBackend creates a new MinioBackend. It will ensure the bucket exists.
// Pass WithExpiry to configure automatic object expiry via a lifecycle rule.
func NewMinioBackend(
	endpoint, accessKey, secretKey string,
	useSSL bool,
	bucket string,
	opts ...MinioOption,
) (*Minio, error) {
	// allow endpoints that are full URLs (e.g. http://minio:9000) by parsing and
	// extracting the host:port portion which the minio client expects.
	if u, err := url.Parse(endpoint); err == nil && u.Scheme != "" {
		if u.Host != "" {
			endpoint = u.Host
		}
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	// ensure bucket exists (best effort)
	ctx := context.Background()
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket exists: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}

	m := &Minio{client: client, bucket: bucket}
	for _, o := range opts {
		o(m)
	}

	// Apply bucket lifecycle policy when a default expiry is configured.
	if m.defaultExpiry > 0 {
		days := max(int(m.defaultExpiry.Hours()/24), 1)
		cfg := lifecycle.NewConfiguration()
		cfg.Rules = []lifecycle.Rule{
			{
				ID:     "auto-expire",
				Status: "Enabled",
				Expiration: lifecycle.Expiration{
					Days: lifecycle.ExpirationDays(days),
				},
			},
		}
		if err := client.SetBucketLifecycle(ctx, bucket, cfg); err != nil {
			return nil, fmt.Errorf("set bucket lifecycle (expiry %dd): %w", days, err)
		}
	}

	return m, nil
}

// NewMinioPresigner creates a Minio that is used only for presigning.
func NewMinioPresigner(
	endpoint, accessKey, secretKey string, useSSL bool, bucket string,
) (*Minio, error) {
	if u, err := url.Parse(endpoint); err == nil && u.Scheme != "" {
		if u.Host != "" {
			endpoint = u.Host
		}
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio presign client: %w", err)
	}

	return &Minio{
		client: client,
		bucket: bucket,
	}, nil
}

func (m *Minio) Store(ctx context.Context, key string, reader io.Reader, metadata *FileMetadata) error {
	opts := minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	}
	if metadata != nil && metadata.ContentType != "" {
		opts.ContentType = metadata.ContentType
	}
	if metadata != nil && metadata.FileName != "" {
		if opts.UserMetadata == nil {
			opts.UserMetadata = make(map[string]string)
		}
		opts.UserMetadata["filename"] = metadata.FileName
	}

	// Attach expiry as user-metadata so clients and cleanup jobs can inspect it.
	// Prefer the per-object Expiry from metadata; fall back to defaultExpiry.
	var expiry time.Time
	if metadata != nil && !metadata.Expiry.IsZero() {
		expiry = metadata.Expiry
	} else if m.defaultExpiry > 0 {
		expiry = time.Now().Add(m.defaultExpiry)
	}
	if !expiry.IsZero() {
		if opts.UserMetadata == nil {
			opts.UserMetadata = make(map[string]string)
		}
		// RFC3339 so any reader can parse it deterministically.
		// Note: MinIO/S3 disallow certain user-defined metadata names (e.g. "expires").
		opts.UserMetadata[userMetaExpiryAt] = expiry.UTC().Format(time.RFC3339)
	}

	_, err := m.client.PutObject(ctx, m.bucket, key, reader, -1, opts)
	return err
}

func (m *Minio) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// Verify object exists by reading stat
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return nil, errors.New("not found")
		}
		return nil, err
	}
	return obj, nil
}

func (m *Minio) GetWithRange(ctx context.Context, key string, start, end int64) (io.ReadCloser, error) {
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(start, end); err != nil {
		return nil, err
	}
	obj, err := m.client.GetObject(ctx, m.bucket, key, opts)
	if err != nil {
		return nil, err
	}
	// Do NOT call obj.Stat() here: for range GETs the minio client lazily issues
	// the HTTP request on first Read; calling Stat() first re-issues a HEAD
	// (without the Range header), resetting the object to a full read.
	// Trigger the actual request by reading 1 byte into a peek buffer.
	buf := make([]byte, 1)
	n, peekErr := obj.Read(buf)
	if peekErr != nil && !errors.Is(peekErr, io.EOF) {
		obj.Close()
		return nil, peekErr
	}
	return &prependReader{prefix: buf[:n], ReadCloser: obj}, nil
}

// prependReader wraps a ReadCloser and prepends already-read bytes.
type prependReader struct {
	io.ReadCloser

	prefix []byte
	pos    int
}

func (p *prependReader) Read(b []byte) (int, error) {
	if p.pos < len(p.prefix) {
		n := copy(b, p.prefix[p.pos:])
		p.pos += n
		return n, nil
	}
	return p.ReadCloser.Read(b)
}

func (m *Minio) GetInfo(ctx context.Context, key string) (*FileMetadata, error) {
	info, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}

	fm := &FileMetadata{
		FileName:     extractFileNameFromStorageKey(key),
		FileSize:     info.Size,
		ContentType:  info.ContentType,
		LastModified: info.LastModified,
		StorageKey:   key,
		Bucket:       m.bucket,
		Headers:      map[string]string{},
	}

	if storedFileName := userMetadataValueCaseInsensitive(info.UserMetadata, "filename"); storedFileName != "" {
		fm.FileName = storedFileName
	}

	// Restore expiry from user metadata if present.
	if expiresStr := userMetadataValueCaseInsensitive(info.UserMetadata, userMetaExpiryAt); expiresStr != "" {
		if t, err := time.Parse(time.RFC3339, expiresStr); err == nil {
			fm.Expiry = t
		}
	}

	return fm, nil
}

func (m *Minio) Exists(ctx context.Context, key string) (bool, error) {
	_, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (m *Minio) Delete(ctx context.Context, key string) error {
	return m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{})
}

func (m *Minio) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	presignedURL, err := m.client.PresignedGetObject(ctx, m.bucket, key, ttl, nil)
	if err != nil {
		return "", err
	}

	return presignedURL.String(), nil
}

func userMetadataValueCaseInsensitive(metadata map[string]string, key string) string {
	for k, v := range metadata {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func extractFileNameFromStorageKey(key string) string {
	base := path.Base(key)
	matches := keyHashSuffixRe.FindStringSubmatch(base)
	if len(matches) == 2 && matches[1] != "" {
		return matches[1]
	}
	return base
}
