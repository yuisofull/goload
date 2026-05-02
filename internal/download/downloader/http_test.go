package downloader_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/download/downloader"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func newDL() *downloader.HTTPDownloader {
	return downloader.NewHTTPDownloader(nil)
}

// serve returns a test server that always responds with body and the given
// status, content-type and optional extra headers.
func serve(
	t *testing.T,
	status int,
	body string,
	contentType string,
	extraHeaders map[string]string,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		for k, v := range extraHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
}

// ─────────────────────────────────────────────────────────────────────────────
// GetFileInfo
// ─────────────────────────────────────────────────────────────────────────────

func TestGetFileInfo_HEADSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", "1024")
		w.Header().Set("Content-Disposition", `attachment; filename="archive.zip"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	meta, err := newDL().GetFileInfo(context.Background(), srv.URL+"/archive.zip", nil)

	require.NoError(t, err)
	assert.Equal(t, "application/zip", meta.ContentType)
	assert.Equal(t, int64(1024), meta.FileSize)
	assert.Equal(t, "archive.zip", meta.FileName)
}

func TestGetFileInfo_FallsBackToGETWhenHEADNotAllowed(t *testing.T) {
	headCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			headCalled = true
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// GET response
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello")
	}))
	defer srv.Close()

	meta, err := newDL().GetFileInfo(context.Background(), srv.URL+"/file.txt", nil)

	require.NoError(t, err)
	assert.True(t, headCalled)
	assert.Equal(t, "text/plain", meta.ContentType)
	assert.Equal(t, int64(5), meta.FileSize)
}

func TestGetFileInfo_FallsBackToGETWhenHEADForbidden(t *testing.T) {
	headCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			headCalled = true
			w.WriteHeader(http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "11")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "hello world")
	}))
	defer srv.Close()

	meta, err := newDL().GetFileInfo(context.Background(), srv.URL+"/paper.pdf", nil)

	require.NoError(t, err)
	assert.True(t, headCalled)
	assert.Equal(t, "application/pdf", meta.ContentType)
	assert.Equal(t, int64(11), meta.FileSize)
	assert.Equal(t, "paper.pdf", meta.FileName)
}

func TestGetFileInfo_FallsBackToPlainGETWhenRangeProbeForbidden(t *testing.T) {
	headCalled := false
	rangeGetCalled := false
	plainGetCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			headCalled = true
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodGet:
			if r.Header.Get("Range") != "" {
				rangeGetCalled = true
				w.WriteHeader(http.StatusForbidden)
				return
			}
			plainGetCalled = true
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "pdf")
		}
	}))
	defer srv.Close()

	meta, err := newDL().GetFileInfo(context.Background(), srv.URL+"/paper.pdf", nil)

	require.NoError(t, err)
	assert.True(t, headCalled)
	assert.True(t, rangeGetCalled)
	assert.True(t, plainGetCalled)
	assert.Equal(t, "application/pdf", meta.ContentType)
	assert.Equal(t, int64(3), meta.FileSize)
}

func TestGetFileInfo_ReturnsBestEffortMetadataWhenAllProbesForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	meta, err := newDL().GetFileInfo(context.Background(), srv.URL+"/blocked.pdf", nil)

	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "blocked.pdf", meta.FileName)
	assert.Equal(t, int64(0), meta.FileSize)
}

func TestGetFileInfo_FilenameFromURL_WhenNoContentDisposition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	meta, err := newDL().GetFileInfo(context.Background(), srv.URL+"/videos/clip.mp4", nil)

	require.NoError(t, err)
	assert.Equal(t, "clip.mp4", meta.FileName)
}

func TestGetFileInfo_ErrorOn4xx(t *testing.T) {
	srv := serve(t, http.StatusNotFound, "", "", nil)
	defer srv.Close()

	_, err := newDL().GetFileInfo(context.Background(), srv.URL+"/missing", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestGetFileInfo_ContentRangeFallback(t *testing.T) {
	// Server returns 206 with Content-Range but no Content-Length — total from range
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Range", "bytes 0-0/99999")
		w.WriteHeader(http.StatusPartialContent)
		fmt.Fprint(w, "x")
	}))
	defer srv.Close()

	meta, err := newDL().GetFileInfo(context.Background(), srv.URL+"/big.bin", nil)

	require.NoError(t, err)
	assert.Equal(t, int64(99999), meta.FileSize)
}

// ─────────────────────────────────────────────────────────────────────────────
// Download
// ─────────────────────────────────────────────────────────────────────────────

func TestDownload_ReadsBodyCorrectly(t *testing.T) {
	const payload = "the quick brown fox"
	srv := serve(t, http.StatusOK, payload, "text/plain", nil)
	defer srv.Close()

	rc, total, err := newDL().Download(context.Background(), srv.URL+"/file.txt", nil, download.DownloadOptions{})

	require.NoError(t, err)
	defer rc.Close()
	assert.Equal(t, int64(len(payload)), total)

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, payload, string(got))
}

func TestDownload_BearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "secret")
	}))
	defer srv.Close()

	rc, _, err := newDL().Download(
		context.Background(), srv.URL+"/secure",
		&download.AuthConfig{Type: "bearer", Token: "my-token"},
		download.DownloadOptions{},
	)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	assert.Equal(t, "secret", string(got))
}

func TestDownload_SetsDefaultUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		if ua == "" {
			http.Error(w, "missing user-agent", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	rc, _, err := newDL().Download(context.Background(), srv.URL+"/ua", nil, download.DownloadOptions{})
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	assert.Equal(t, "ok", string(got))
}

func TestDownload_BasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "alice" || p != "pass" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	rc, _, err := newDL().Download(
		context.Background(), srv.URL+"/protected",
		&download.AuthConfig{Type: "basic", Username: "alice", Password: "pass"},
		download.DownloadOptions{},
	)
	require.NoError(t, err)
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	assert.Equal(t, "ok", string(got))
}

func TestDownload_CustomHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key123" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data")
	}))
	defer srv.Close()

	rc, _, err := newDL().Download(
		context.Background(), srv.URL+"/api",
		&download.AuthConfig{Headers: map[string]string{"X-Api-Key": "key123"}},
		download.DownloadOptions{},
	)
	require.NoError(t, err)
	defer rc.Close()
}

func TestDownload_ErrorOn4xx(t *testing.T) {
	srv := serve(t, http.StatusNotFound, "not found", "text/plain", nil)
	defer srv.Close()

	_, _, err := newDL().Download(context.Background(), srv.URL+"/missing", nil, download.DownloadOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestDownload_ErrorOn5xx(t *testing.T) {
	srv := serve(t, http.StatusInternalServerError, "oops", "text/plain", nil)
	defer srv.Close()

	_, _, err := newDL().Download(context.Background(), srv.URL+"/error", nil, download.DownloadOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestDownload_ContextCancellation(t *testing.T) {
	// Server blocks until the request context is cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write the status line so the client establishes the connection, then
		// block until the client cancels — this lets us observe the cancellation.
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	rc, _, err := newDL().Download(ctx, srv.URL+"/block", nil, download.DownloadOptions{})
	// Either the dial/connect or the body read must return an error.
	if err == nil {
		// If we got a reader, reading from it must eventually fail.
		_, readErr := io.ReadAll(rc)
		rc.Close()
		require.Error(t, readErr, "expected an error when context is cancelled during body read")
	} else {
		require.Error(t, err)
	}
}

func TestDownload_LargePayload(t *testing.T) {
	const size = 512 * 1024 // 512 KB
	body := strings.Repeat("a", size)
	srv := serve(t, http.StatusOK, body, "application/octet-stream", nil)
	defer srv.Close()

	rc, total, err := newDL().Download(context.Background(), srv.URL+"/large", nil, download.DownloadOptions{})
	require.NoError(t, err)
	defer rc.Close()

	assert.Equal(t, int64(size), total)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Len(t, got, size)
}

// ─────────────────────────────────────────────────────────────────────────────
// Speed throttling
// ─────────────────────────────────────────────────────────────────────────────

func TestDownload_SpeedThrottle_SlowerThanUnthrottled(t *testing.T) {
	const size = 32 * 1024 // 32 KB
	body := strings.Repeat("x", size)
	srv := serve(t, http.StatusOK, body, "application/octet-stream", nil)
	defer srv.Close()

	// Throttle to 16 KB/s — reading 32 KB should take at least ~1 s.
	maxSpeed := int64(16 * 1024)
	opts := download.DownloadOptions{MaxSpeed: &maxSpeed}

	start := time.Now()
	rc, _, err := newDL().Download(context.Background(), srv.URL+"/throttled", nil, opts)
	require.NoError(t, err)
	defer rc.Close()
	_, err = io.ReadAll(rc)
	require.NoError(t, err)
	elapsed := time.Since(start)

	// With throttling we expect it to take longer than 0.5 s.
	assert.Greater(t, elapsed, 500*time.Millisecond,
		"throttled download should take longer than 500ms, got %s", elapsed)
}

func TestDownload_NoThrottle_WhenMaxSpeedIsNil(t *testing.T) {
	const size = 32 * 1024
	body := strings.Repeat("y", size)
	srv := serve(t, http.StatusOK, body, "application/octet-stream", nil)
	defer srv.Close()

	// No throttle — should complete well under 1 s on loopback.
	start := time.Now()
	rc, _, err := newDL().Download(context.Background(), srv.URL+"/fast", nil, download.DownloadOptions{})
	require.NoError(t, err)
	defer rc.Close()
	_, err = io.ReadAll(rc)
	require.NoError(t, err)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 2*time.Second,
		"unthrottled loopback download should finish quickly, took %s", elapsed)
}

// ─────────────────────────────────────────────────────────────────────────────
// SupportsResume
// ─────────────────────────────────────────────────────────────────────────────

func TestSupportsResume(t *testing.T) {
	assert.True(t, newDL().SupportsResume())
}
