package storage_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/storage"
)

func TestLocalBackend_StoreGetRangeInfoDelete(t *testing.T) {
	root := t.TempDir()
	backend, err := storage.NewLocalBackend(root)
	require.NoError(t, err)

	ctx := context.Background()
	key := "downloads/example.txt"
	content := "0123456789"
	meta := &storage.FileMetadata{FileName: "example.txt", ContentType: "text/plain"}

	require.NoError(t, backend.Store(ctx, key, strings.NewReader(content), meta))

	exists, err := backend.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)

	rc, err := backend.Get(ctx, key)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	_ = rc.Close()
	assert.Equal(t, content, string(got))

	rangeRC, err := backend.GetWithRange(ctx, key, 2, 5)
	require.NoError(t, err)
	rangeGot, err := io.ReadAll(rangeRC)
	require.NoError(t, err)
	_ = rangeRC.Close()
	assert.Equal(t, "2345", string(rangeGot))

	info, err := backend.GetInfo(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "example.txt", info.FileName)
	assert.Equal(t, int64(len(content)), info.FileSize)
	assert.Equal(t, "text/plain", info.ContentType)
	assert.Equal(t, key, info.StorageKey)
	assert.Equal(t, "local", info.Bucket)
	assert.WithinDuration(t, time.Now(), info.LastModified, 2*time.Second)

	presigned, err := backend.PresignGet(ctx, key, time.Minute)
	require.NoError(t, err)
	assert.Contains(t, presigned, "file:")
	assert.Contains(t, presigned, "example.txt")

	require.NoError(t, backend.Delete(ctx, key))
	exists, err = backend.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestLocalBackend_RejectsTraversalKeys(t *testing.T) {
	backend, err := storage.NewLocalBackend(t.TempDir())
	require.NoError(t, err)

	err = backend.Store(context.Background(), "../escape.txt", strings.NewReader("x"), nil)
	assert.Error(t, err)
}
