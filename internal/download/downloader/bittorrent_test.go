package downloader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
)

func TestBitTorrentDownloader_SupportsResume(t *testing.T) {
	dl := NewBitTorrentDownloader()
	assert.False(t, dl.SupportsResume())
}

func TestBitTorrentDownloader_GetFileInfo_Magnet(t *testing.T) {
	dl := NewBitTorrentDownloader()

	meta, err := dl.GetFileInfo(
		context.Background(),
		"magnet:?xt=urn:btih:abcdef1234567890&dn=ubuntu.iso&xl=12345",
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu.iso", meta.FileName)
	assert.Equal(t, int64(12345), meta.FileSize)
	assert.Equal(t, "urn:btih:abcdef1234567890", meta.Headers["XT"])
}

func TestBitTorrentDownloader_GetFileInfo_InvalidScheme(t *testing.T) {
	dl := NewBitTorrentDownloader()

	_, err := dl.GetFileInfo(context.Background(), "ftp://example.com/file.torrent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported bittorrent scheme")
}

func TestBitTorrentDownloader_Download_InvalidScheme(t *testing.T) {
	dl := NewBitTorrentDownloader()

	_, _, err := dl.Download(
		context.Background(),
		"ftp://example.com/file.torrent",
		nil,
		download.DownloadOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported bittorrent scheme")
}

func TestBitTorrentDownloader_Download_MagnetWithoutSource(t *testing.T) {
	dl := NewBitTorrentDownloader()

	_, _, err := dl.Download(
		context.Background(),
		"magnet:?xt=urn:btih:abcdef1234567890&dn=ubuntu.iso&xl=12345",
		nil,
		download.DownloadOptions{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magnet download requires xs/as torrent source")
}

func TestParseMagnetLength(t *testing.T) {
	assert.Equal(t, int64(0), parseMagnetLength(""))
	assert.Equal(t, int64(0), parseMagnetLength("abc"))
	assert.Equal(t, int64(0), parseMagnetLength("-1"))
	assert.Equal(t, int64(42), parseMagnetLength("42"))
}