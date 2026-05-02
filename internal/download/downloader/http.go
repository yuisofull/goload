// Package downloader provides concrete Downloader implementations.
package downloader

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"golang.org/x/time/rate"

	"github.com/yuisofull/goload/internal/download"
)

const defaultUserAgent = "Mozilla/5.0 (compatible; GoLoad/1.0; +https://github.com/yuisofull/goload)"

// HTTPDownloader implements download.Downloader for HTTP and HTTPS sources.
// It supports:
//   - HEAD-based metadata retrieval with GET fallback
//   - Range requests for resume (when the server advertises Accept-Ranges)
//   - Per-task download speed throttling via a token-bucket rate limiter
//   - Custom request headers and Bearer/Basic authentication
type HTTPDownloader struct {
	client *http.Client
	logger log.Logger
}

// HTTPDownloaderOption configures an HTTPDownloader.
type HTTPDownloaderOption func(*HTTPDownloader)

// WithHTTPLogger sets the logger for the downloader.
func WithHTTPLogger(logger log.Logger) HTTPDownloaderOption {
	return func(h *HTTPDownloader) {
		if logger != nil {
			h.logger = logger
		}
	}
}

// NewHTTPDownloader returns an HTTPDownloader that uses the provided *http.Client.
// Pass nil to use a sensible default with a 30-second timeout.
func NewHTTPDownloader(client *http.Client, opts ...HTTPDownloaderOption) *HTTPDownloader {
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Minute, // long-running download; individual dial is handled by transport
			Transport: &http.Transport{
				MaxIdleConnsPerHost:   4,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	}
	h := &HTTPDownloader{
		client: client,
		logger: log.NewNopLogger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// SupportsResume returns true; the downloader will attempt range requests when
// the server advertises "Accept-Ranges: bytes".
func (h *HTTPDownloader) SupportsResume() bool { return true }

// GetFileInfo issues a HEAD request (falling back to a zero-byte GET) to
// retrieve the file name, size, and content-type without downloading the body.
func (h *HTTPDownloader) GetFileInfo(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
) (*download.FileMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build HEAD request: %w", err)
	}

	applyAuth(req, auth)

	resp, err := h.client.Do(req)
	if err != nil || shouldFallbackFromHead(resp.StatusCode) {
		// Some servers do not support HEAD; fall back to a range-0 GET.
		if resp != nil {
			resp.Body.Close()
		}
		return h.getFileInfoViaGet(ctx, rawURL, auth)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusForbidden {
			// Some hosts block metadata probes even when file retrieval may still be allowed.
			// Return best-effort metadata so the caller can proceed to the actual download attempt.
			return bestEffortMetadata(rawURL), nil
		}
		return nil, fmt.Errorf("HEAD %s: unexpected status %s", rawURL, resp.Status)
	}

	return buildMetadata(rawURL, resp.Header), nil
}

func shouldFallbackFromHead(status int) bool {
	// Some origins/CDNs block or do not implement HEAD while allowing GET.
	switch status {
	case http.StatusForbidden,
		http.StatusMethodNotAllowed,
		http.StatusNotImplemented,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// getFileInfoViaGet falls back to a GET with "Range: bytes=0-0" so we only
// pull one byte but can still read all the response headers.
func (h *HTTPDownloader) getFileInfoViaGet(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
) (*download.FileMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build GET request for file info: %w", err)
	}
	applyAuth(req, auth)
	req.Header.Set("Range", "bytes=0-0")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s for file info: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusForbidden {
			// Some origins reject ranged probes but allow a regular GET.
			meta, err := h.getFileInfoViaPlainGet(ctx, rawURL, auth)
			if err == nil {
				return meta, nil
			}
			return bestEffortMetadata(rawURL), nil
		}
		return nil, fmt.Errorf("GET %s: unexpected status %s", rawURL, resp.Status)
	}

	return buildMetadata(rawURL, resp.Header), nil
}

func (h *HTTPDownloader) getFileInfoViaPlainGet(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
) (*download.FileMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build plain GET request for file info: %w", err)
	}
	applyAuth(req, auth)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plain GET %s for file info: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusForbidden {
			return bestEffortMetadata(rawURL), nil
		}
		return nil, fmt.Errorf("GET %s: unexpected status %s", rawURL, resp.Status)
	}

	return buildMetadata(rawURL, resp.Header), nil
}

func bestEffortMetadata(rawURL string) *download.FileMetadata {
	return &download.FileMetadata{
		FileName: filenameFromURL(rawURL),
		Headers:  map[string]string{},
	}
}

// Download starts the HTTP download and returns a ReadCloser over the response
// body.  When auth.MaxSpeed is set a token-bucket limiter is wrapped around
// the body reader to honour the bytes-per-second cap.
func (h *HTTPDownloader) Download(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	opts download.DownloadOptions,
) (io.ReadCloser, int64, error) {
	if opts.Concurrency > 1 {
		meta, err := h.GetFileInfo(ctx, rawURL, auth)
		if err == nil && meta.FileSize > 0 &&
			strings.Contains(strings.ToLower(meta.Headers["Accept-Ranges"]), "bytes") {
			return h.downloadParallel(ctx, rawURL, auth, opts, meta.FileSize)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build GET request: %w", err)
	}

	applyAuth(req, auth)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("GET %s: %w", rawURL, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("GET %s: unexpected status %s", rawURL, resp.Status)
	}

	reader := resp.Body

	// Wrap with a rate-limiter when MaxSpeed is configured.
	if opts.MaxSpeed != nil && *opts.MaxSpeed > 0 {
		reader = newRateLimitedReader(ctx, resp.Body, *opts.MaxSpeed)
	}

	return reader, resp.ContentLength, nil
}

type chunkJob struct {
	index int
	start int64
	end   int64
}

type chunkResult struct {
	index int
	data  []byte
	err   error
}

func (h *HTTPDownloader) downloadParallel(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	opts download.DownloadOptions,
	fileSize int64,
) (io.ReadCloser, int64, error) {
	chunkSize := int64(5 * 1024 * 1024) // 5MB chunks
	numChunks := int((fileSize + chunkSize - 1) / chunkSize)

	level.Debug(h.logger).Log(
		"msg", "starting parallel chunk download",
		"file_size", fileSize,
		"chunk_size", chunkSize,
		"num_chunks", numChunks,
		"concurrency", opts.Concurrency,
	)

	// Start the first chunk synchronously to verify Range support
	firstJob := chunkJob{index: 0, start: 0, end: chunkSize - 1}
	if firstJob.end >= fileSize {
		firstJob.end = fileSize - 1
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	applyAuth(req, auth)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", firstJob.start, firstJob.end))

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode == http.StatusOK {
		level.Debug(h.logger).Log("msg", "server ignored range header, falling back to sequential download")
		// Server ignored Range header, fallback to normal download
		reader := resp.Body
		if opts.MaxSpeed != nil && *opts.MaxSpeed > 0 {
			reader = newRateLimitedReader(ctx, resp.Body, *opts.MaxSpeed)
		}
		return reader, resp.ContentLength, nil
	}

	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	firstData, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, 0, err
	}

	pr, pw := io.Pipe()

	jobs := make(chan chunkJob, numChunks)
	results := make(chan chunkResult, numChunks)

	// Push the first chunk result immediately
	results <- chunkResult{index: 0, data: firstData}

	for i := 1; i < numChunks; i++ {
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if end >= fileSize {
			end = fileSize - 1
		}
		jobs <- chunkJob{index: i, start: start, end: end}
	}
	close(jobs)

	cancelCtx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	for range opts.Concurrency {
		wg.Go(func() {
			for job := range jobs {
				level.Debug(h.logger).Log(
					"msg", "starting chunk download",
					"chunk_index", job.index,
					"start_byte", job.start,
					"end_byte", job.end,
				)

				select {
				case <-cancelCtx.Done():
					return
				default:
				}

				maxRetries := opts.MaxRetries
				if maxRetries <= 0 {
					maxRetries = 3
				}

				var data []byte
				var lastErr error

				for attempt := 0; attempt <= maxRetries; attempt++ {
					if attempt > 0 {
						backoff := time.Second * time.Duration(1<<attempt)
						jitter := time.Duration(time.Now().UnixNano() % int64(time.Second))

						level.Warn(h.logger).Log(
							"msg", "retrying chunk download",
							"chunk_index", job.index,
							"attempt", attempt,
							"max_retries", maxRetries,
							"backoff", backoff+jitter,
							"last_error", lastErr,
						)

						select {
						case <-time.After(backoff + jitter):
						case <-cancelCtx.Done():
							return
						}
					}

					req, err := http.NewRequestWithContext(cancelCtx, http.MethodGet, rawURL, nil)
					if err != nil {
						lastErr = err
						continue
					}
					applyAuth(req, auth)
					req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", job.start, job.end))

					resp, err := h.client.Do(req)
					if err != nil {
						lastErr = err
						continue
					}

					if resp.StatusCode != http.StatusPartialContent {
						resp.Body.Close()
						lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
						continue
					}

					data, err = io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil {
						lastErr = err
						continue
					}

					lastErr = nil
					break
				}

				if lastErr != nil {
					results <- chunkResult{index: job.index, err: lastErr}
					return
				}

				level.Debug(h.logger).Log(
					"msg", "finished chunk download",
					"chunk_index", job.index,
					"bytes_downloaded", len(data),
				)

				results <- chunkResult{index: job.index, data: data}
			}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		defer pw.Close()
		defer cancel()

		buffer := make(map[int][]byte)
		nextIndex := 0

		for res := range results {
			if res.err != nil {
				pw.CloseWithError(res.err)
				return
			}

			if res.index == nextIndex {
				level.Debug(h.logger).Log("msg", "writing chunk to pipe", "chunk_index", res.index)
				if _, err := pw.Write(res.data); err != nil {
					return
				}
				nextIndex++

				for {
					if data, ok := buffer[nextIndex]; ok {
						level.Debug(h.logger).Log("msg", "writing buffered chunk to pipe", "chunk_index", nextIndex)
						if _, err := pw.Write(data); err != nil {
							return
						}
						delete(buffer, nextIndex)
						nextIndex++
					} else {
						break
					}
				}
			} else {
				buffer[res.index] = res.data
			}
		}
	}()

	var reader io.ReadCloser = pr
	if opts.MaxSpeed != nil && *opts.MaxSpeed > 0 {
		reader = newRateLimitedReader(ctx, pr, *opts.MaxSpeed)
	}

	return reader, fileSize, nil
}

// applyAuth decorates a request with credentials from AuthConfig.
func applyAuth(req *http.Request, auth *download.AuthConfig) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", defaultUserAgent)
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}

	if auth == nil {
		return
	}

	switch strings.ToLower(auth.Type) {
	case "basic":
		req.SetBasicAuth(auth.Username, auth.Password)
	case "bearer", "token":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	}

	for k, v := range auth.Headers {
		req.Header.Set(k, v)
	}
}

// buildMetadata extracts file metadata from HTTP response headers.
func buildMetadata(rawURL string, h http.Header) *download.FileMetadata {
	meta := &download.FileMetadata{
		ContentType: h.Get("Content-Type"),
		Headers:     map[string]string{},
	}

	// For partial responses (206) the true total is in Content-Range.
	// e.g. "bytes 0-0/12345" → total = 12345
	if cr := h.Get("Content-Range"); cr != "" {
		if idx := strings.LastIndex(cr, "/"); idx >= 0 {
			if n, err := strconv.ParseInt(cr[idx+1:], 10, 64); err == nil {
				meta.FileSize = n
			}
		}
	}

	// Full responses carry the total in Content-Length.
	if meta.FileSize == 0 {
		if cl := h.Get("Content-Length"); cl != "" {
			if n, err := strconv.ParseInt(cl, 10, 64); err == nil {
				meta.FileSize = n
			}
		}
	}

	// Filename: prefer Content-Disposition, fall back to URL path.
	meta.FileName = filenameFromHeader(h.Get("Content-Disposition"))
	if meta.FileName == "" {
		meta.FileName = filenameFromURL(rawURL)
	}

	// Copy selected headers for callers that need them.
	for _, key := range []string{
		"Last-Modified", "ETag", "Accept-Ranges", "Content-Encoding",
	} {
		if v := h.Get(key); v != "" {
			meta.Headers[key] = v
		}
	}

	return meta
}

// filenameFromHeader parses the filename from a Content-Disposition header.
// e.g. "attachment; filename=\"report.pdf\""
func filenameFromHeader(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return params["filename"]
}

// filenameFromURL extracts the last path segment from a URL.
func filenameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	name := path.Base(u.Path)
	if name == "." || name == "/" {
		return ""
	}
	return name
}

// rateLimitedReader wraps an io.ReadCloser and throttles reads to maxBytesPerSec.
type rateLimitedReader struct {
	ctx     context.Context
	inner   io.ReadCloser
	limiter *rate.Limiter
}

// newRateLimitedReader creates a reader that caps throughput to maxBytesPerSec.
// The token bucket is initialised with a burst of up to 64 KB so small reads
// are served immediately while the long-run average is honoured.
func newRateLimitedReader(ctx context.Context, r io.ReadCloser, maxBytesPerSec int64) *rateLimitedReader {
	const maxBurst = 64 * 1024 // 64 KB burst
	burst := int(maxBytesPerSec)
	if burst <= 0 {
		burst = 1
	}
	if burst > maxBurst {
		burst = maxBurst
	}
	return &rateLimitedReader{
		ctx:     ctx,
		inner:   r,
		limiter: rate.NewLimiter(rate.Limit(maxBytesPerSec), burst),
	}
}

func (r *rateLimitedReader) Read(p []byte) (int, error) {
	// Reserve the bytes we are about to read; wait if the bucket is empty.
	if len(p) > r.limiter.Burst() {
		p = p[:r.limiter.Burst()]
	}
	if err := r.limiter.WaitN(r.ctx, len(p)); err != nil {
		return 0, err
	}
	return r.inner.Read(p)
}

func (r *rateLimitedReader) Close() error {
	return r.inner.Close()
}
