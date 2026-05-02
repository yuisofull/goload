package downloader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
)

func TestBitTorrentDownloader_SupportsResume(t *testing.T) {
	dl, close, err := NewBitTorrentDownloader()
	if err != nil {
		t.Skipf("bittorrent client unavailable: %v", err)
	}
	defer close()
	assert.False(t, dl.SupportsResume())
}

func TestBitTorrentDownloader_GetFileInfo_InvalidScheme(t *testing.T) {
	dl, close, err := NewBitTorrentDownloader()
	if err != nil {
		t.Skipf("bittorrent client unavailable: %v", err)
	}
	defer close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err = dl.GetFileInfo(ctx, "ftp://example.com/file.torrent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported torrent url format")
}

func TestBitTorrentDownloader_Download_InvalidScheme(t *testing.T) {
	dl, close, err := NewBitTorrentDownloader()
	if err != nil {
		t.Skipf("bittorrent client unavailable: %v", err)
	}
	defer close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, _, err = dl.Download(
		ctx,
		"ftp://example.com/file.torrent",
		nil,
		download.DownloadOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported torrent url format")
}
