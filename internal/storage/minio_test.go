package storage_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/yuisofull/goload/internal/storage"
)

const (
	testBucket    = "test-bucket"
	minioUser     = "minioadmin"
	minioPassword = "minioadmin"
)

// startMinio starts a MinIO container and returns a *storage.Minio and a cleanup function.
func startMinio(t *testing.T) (*storage.Minio, func()) {
	t.Helper()
	ctx := context.Background()
	minioContainer, err := tcminio.Run(ctx,
		"minio/minio:RELEASE.2024-01-16T16-07-38Z",
		tcminio.WithUsername(minioUser),
		tcminio.WithPassword(minioPassword),
	)
	require.NoError(t, err, "failed to start MinIO container")
	endpoint, err := minioContainer.ConnectionString(ctx)
	require.NoError(t, err, "failed to get MinIO endpoint")
	backend, err := storage.NewMinioBackend(endpoint, minioUser, minioPassword, false, testBucket)
	require.NoError(t, err, "failed to create minio backend")
	return backend, func() {
		if err := minioContainer.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate MinIO container: %v", err)
		}
	}
}

func TestMinio_StoreAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/hello.txt"
	content := "hello, minio!"
	err := backend.Store(ctx, key, strings.NewReader(content), &storage.FileMetadata{
		ContentType: "text/plain",
	})
	require.NoError(t, err, "Store should succeed")
	rc, err := backend.Get(ctx, key)
	require.NoError(t, err, "Get should succeed")
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}

func TestMinio_GetNonExistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	_, err := backend.Get(ctx, "does/not/exist.txt")
	assert.Error(t, err, "Get on missing key should return an error")
}

func TestMinio_Exists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/exists.txt"
	// Should not exist yet.
	exists, err := backend.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists, "key should not exist before storing")
	err = backend.Store(ctx, key, strings.NewReader("data"), nil)
	require.NoError(t, err)
	// Should exist now.
	exists, err = backend.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists, "key should exist after storing")
}

func TestMinio_Delete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/delete-me.txt"
	err := backend.Store(ctx, key, strings.NewReader("bye"), nil)
	require.NoError(t, err)
	exists, err := backend.Exists(ctx, key)
	require.NoError(t, err)
	require.True(t, exists, "key should exist before delete")
	err = backend.Delete(ctx, key)
	require.NoError(t, err, "Delete should succeed")
	exists, err = backend.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists, "key should not exist after delete")
}

func TestMinio_GetInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/info.txt"
	content := "some content for info"
	err := backend.Store(ctx, key, strings.NewReader(content), &storage.FileMetadata{
		ContentType: "text/plain",
	})
	require.NoError(t, err)
	info, err := backend.GetInfo(ctx, key)
	require.NoError(t, err, "GetInfo should succeed")
	assert.Equal(t, int64(len(content)), info.FileSize)
	assert.Equal(t, testBucket, info.Bucket)
	assert.Equal(t, key, info.StorageKey)
	assert.Equal(t, "text/plain", info.ContentType)
}

func TestMinio_GetWithRange(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/range.txt"
	content := "0123456789"
	err := backend.Store(ctx, key, strings.NewReader(content), nil)
	require.NoError(t, err)
	// Read bytes [2, 5] => "2345"
	rc, err := backend.GetWithRange(ctx, key, 2, 5)
	require.NoError(t, err, "GetWithRange should succeed")
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "2345", string(got))
}

func TestMinio_StoreWithNoMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/no-metadata.bin"
	data := []byte{0x01, 0x02, 0x03}
	err := backend.Store(ctx, key, bytes.NewReader(data), nil)
	require.NoError(t, err, "Store with nil metadata should succeed")
	rc, err := backend.Get(ctx, key)
	require.NoError(t, err)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestMinio_PresignGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/presign.txt"
	err := backend.Store(ctx, key, strings.NewReader("presigned content"), nil)
	require.NoError(t, err)
	url, err := backend.PresignGet(ctx, key, 15*time.Minute)
	require.NoError(t, err, "PresignGet should succeed")
	assert.NotEmpty(t, url, "presigned URL should not be empty")
	assert.Contains(t, url, key, "presigned URL should contain the object key")
}

func TestMinio_PresignGet_EmptyBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/presign-default-bucket.txt"
	err := backend.Store(ctx, key, strings.NewReader("data"), nil)
	require.NoError(t, err)
	// Passing empty bucket should fall back to the configured bucket.
	url, err := backend.PresignGet(ctx, key, 15*time.Minute)
	require.NoError(t, err, "PresignGet with empty bucket should use default bucket")
	assert.NotEmpty(t, url)
}

func TestMinio_OverwriteExistingKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	backend, cleanup := startMinio(t)
	defer cleanup()
	ctx := context.Background()
	key := "test/overwrite.txt"
	require.NoError(t, backend.Store(ctx, key, strings.NewReader("original"), nil))
	require.NoError(t, backend.Store(ctx, key, strings.NewReader("updated"), nil))
	rc, err := backend.Get(ctx, key)
	require.NoError(t, err)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "updated", string(got), "overwritten object should return latest content")
}
