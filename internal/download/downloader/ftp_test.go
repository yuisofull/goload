package downloader

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
)

func TestFTPDownloader_SupportsResume(t *testing.T) {
	dl := NewFTPDownloader(0)
	assert.False(t, dl.SupportsResume())
}

func TestFTPDownloader_Download_InvalidScheme(t *testing.T) {
	dl := NewFTPDownloader(0)

	_, _, err := dl.Download(context.Background(), "http://example.com/file.txt", nil, download.DownloadOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported ftp scheme")
}

func TestFTPDownloader_GetFileInfo_InvalidURL(t *testing.T) {
	dl := NewFTPDownloader(0)

	_, err := dl.GetFileInfo(context.Background(), "://bad-url", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse ftp url")
}

func TestFTPDownloader_GetFileInfo_MissingHost(t *testing.T) {
	dl := NewFTPDownloader(0)

	_, err := dl.GetFileInfo(context.Background(), "ftp:///file.txt", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing host")
}

func TestResolveFTPCredentials_AuthOverridesURL(t *testing.T) {
	u, err := parseURL("ftp://url-user:url-pass@example.com/file.txt")
	require.NoError(t, err)

	user, pass := resolveFTPCredentials(u, &download.AuthConfig{Username: "cfg-user", Password: "cfg-pass"})
	assert.Equal(t, "cfg-user", user)
	assert.Equal(t, "cfg-pass", pass)
}

func TestResolveFTPCredentials_URLCredentialsFallback(t *testing.T) {
	u, err := parseURL("ftp://url-user:url-pass@example.com/file.txt")
	require.NoError(t, err)

	user, pass := resolveFTPCredentials(u, nil)
	assert.Equal(t, "url-user", user)
	assert.Equal(t, "url-pass", pass)
}

func TestResolveFTPCredentials_AnonymousDefault(t *testing.T) {
	u, err := parseURL("ftp://example.com/file.txt")
	require.NoError(t, err)

	user, pass := resolveFTPCredentials(u, nil)
	assert.Equal(t, "anonymous", user)
	assert.Equal(t, "anonymous", pass)
}

func TestNewFTPDownloader_DefaultTimeout(t *testing.T) {
	dl := NewFTPDownloader(0)
	assert.Equal(t, 30*time.Second, dl.timeout)
}

func parseURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}
