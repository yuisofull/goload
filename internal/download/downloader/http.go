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

// defaultUserAgent mimics a common desktop browser. Many origins (CDNs, WAFs,
// parliamentary/gov sites) reject non-browser agents with HTTP 403 or HTTP/2
// INTERNAL_ERROR, while the same request with a browser UA succeeds.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"

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

// metadataProbe is one strategy for learning file metadata without a full download.
type metadataProbe struct {
	method     string
	rangeBytes string // optional; e.g. "bytes=0-0"
}

// GetFileInfo probes for file name, size, and content-type.
//
// Probe order: HEAD → ranged GET → plain GET. Many origins/CDNs/WAFs reject
// earlier probes while still allowing a later one (or the real download).
// Soft failures (transport errors, 403) fall through; if every probe soft-fails
// we return URL-derived best-effort metadata so Download can still be attempted.
// Hard failures (e.g. 404) stop the cascade and are returned to the caller.
func (h *HTTPDownloader) GetFileInfo(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
) (*download.FileMetadata, error) {
	probes := []metadataProbe{
		{method: http.MethodHead},
		{method: http.MethodGet, rangeBytes: "bytes=0-0"},
		{method: http.MethodGet},
	}

	var lastSoft error
	for _, probe := range probes {
		meta, soft, err := h.probeMetadata(ctx, rawURL, auth, probe)
		switch {
		case err == nil:
			return meta, nil
		case soft:
			lastSoft = err
			continue
		default:
			return nil, err
		}
	}
	if lastSoft != nil {
		level.Debug(h.logger).Log(
			"msg", "metadata probes soft-failed; using URL-derived metadata",
			"url", rawURL,
			"err", lastSoft,
		)
	}
	return bestEffortMetadata(rawURL), nil
}

// probeMetadata runs a single metadata probe.
// soft=true means the caller should try the next probe (or best-effort).
func (h *HTTPDownloader) probeMetadata(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	probe metadataProbe,
) (meta *download.FileMetadata, soft bool, err error) {
	req, err := http.NewRequestWithContext(ctx, probe.method, rawURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build %s request: %w", probe.method, err)
	}
	applyAuth(req, auth)
	if probe.rangeBytes != "" {
		req.Header.Set("Range", probe.rangeBytes)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("%s %s: %w", probe.method, rawURL, err)
	}
	defer resp.Body.Close()

	if isSuccessStatus(resp.StatusCode) {
		return buildMetadata(rawURL, resp.Header), false, nil
	}
	if isSoftProbeStatus(resp.StatusCode) {
		return nil, true, fmt.Errorf("%s %s: %s", probe.method, rawURL, resp.Status)
	}
	return nil, false, fmt.Errorf("%s %s: unexpected status %s", probe.method, rawURL, resp.Status)
}

func isSuccessStatus(code int) bool {
	return code >= 200 && code < 300
}

// httpStatusError returns a short, user-facing description of an HTTP failure.
func httpStatusError(code int, status string) error {
	if status == "" {
		status = fmt.Sprintf("%d", code)
	}
	switch code {
	case http.StatusUnauthorized:
		return fmt.Errorf("remote server requires authentication (%s)", status)
	case http.StatusForbidden:
		return fmt.Errorf("remote server denied access (%s)", status)
	case http.StatusNotFound:
		return fmt.Errorf("remote file not found (%s)", status)
	case http.StatusTooManyRequests:
		return fmt.Errorf("remote server rate-limited the download (%s)", status)
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Errorf("remote server temporarily unavailable (%s)", status)
	default:
		return fmt.Errorf("remote server returned %s", status)
	}
}

// isSoftProbeStatus reports statuses where a later probe (or the real download)
// may still succeed. Origins often block HEAD/Range while allowing GET.
func isSoftProbeStatus(code int) bool {
	switch code {
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
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}

	if !isSuccessStatus(resp.StatusCode) {
		resp.Body.Close()
		return nil, 0, httpStatusError(resp.StatusCode, resp.Status)
	}

	return maybeRateLimited(ctx, resp.Body, opts), resp.ContentLength, nil
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

const parallelChunkSize = int64(5 * 1024 * 1024) // 5MB

func (h *HTTPDownloader) downloadParallel(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	opts download.DownloadOptions,
	fileSize int64,
) (io.ReadCloser, int64, error) {
	if fileSize <= 0 {
		return nil, 0, fmt.Errorf("invalid file size %d for parallel download", fileSize)
	}

	numChunks64 := fileSize / parallelChunkSize
	if fileSize%parallelChunkSize != 0 {
		numChunks64++
	}

	const maxChunks = 100000
	if numChunks64 > maxChunks {
		return nil, 0, fmt.Errorf("file size %d exceeds the maximum allowed for parallel download", fileSize)
	}

	numChunks := int(numChunks64)

	level.Debug(h.logger).Log(
		"msg", "starting parallel chunk download",
		"file_size", fileSize,
		"chunk_size", parallelChunkSize,
		"num_chunks", numChunks,
		"concurrency", opts.Concurrency,
	)

	firstData, fallback, err := h.fetchFirstChunk(ctx, rawURL, auth, opts, fileSize)
	if err != nil {
		return nil, 0, err
	}
	if fallback != nil {
		return fallback.reader, fallback.total, nil
	}

	pr, pw := io.Pipe()
	jobs := make(chan chunkJob, numChunks)
	results := make(chan chunkResult, numChunks)

	results <- chunkResult{index: 0, data: firstData}
	for i := 1; i < numChunks; i++ {
		jobs <- chunkBounds(i, fileSize)
	}
	close(jobs)

	cancelCtx, cancel := context.WithCancel(ctx)
	h.startChunkWorkers(cancelCtx, rawURL, auth, opts, jobs, results)

	go func() {
		defer pw.Close()
		defer cancel()
		writeOrderedChunks(h.logger, pw, results)
	}()

	return maybeRateLimited(ctx, pr, opts), fileSize, nil
}

type sequentialFallback struct {
	reader io.ReadCloser
	total  int64
}

// fetchFirstChunk verifies Range support. On 200 (Range ignored) it returns a
// sequential fallback reader; on 206 it returns the first chunk bytes.
func (h *HTTPDownloader) fetchFirstChunk(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	opts download.DownloadOptions,
	fileSize int64,
) (firstData []byte, fallback *sequentialFallback, err error) {
	job := chunkBounds(0, fileSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	applyAuth(req, auth)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", job.start, job.end))

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode == http.StatusOK {
		level.Debug(h.logger).Log("msg", "server ignored range header, falling back to sequential download")
		return nil, &sequentialFallback{
			reader: maybeRateLimited(ctx, resp.Body, opts),
			total:  resp.ContentLength,
		}, nil
	}

	if resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, nil, httpStatusError(resp.StatusCode, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	return data, nil, nil
}

func chunkBounds(index int, fileSize int64) chunkJob {
	start := int64(index) * parallelChunkSize
	end := start + parallelChunkSize - 1
	if end >= fileSize {
		end = fileSize - 1
	}
	return chunkJob{index: index, start: start, end: end}
}

func (h *HTTPDownloader) startChunkWorkers(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	opts download.DownloadOptions,
	jobs <-chan chunkJob,
	results chan<- chunkResult,
) {
	var wg sync.WaitGroup
	for range opts.Concurrency {
		wg.Go(func() {
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				data, err := h.downloadChunkWithRetry(ctx, rawURL, auth, opts, job)
				if err != nil {
					results <- chunkResult{index: job.index, err: err}
					return
				}
				results <- chunkResult{index: job.index, data: data}
			}
		})
	}
	go func() {
		wg.Wait()
		close(results)
	}()
}

func (h *HTTPDownloader) downloadChunkWithRetry(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	opts download.DownloadOptions,
	job chunkJob,
) ([]byte, error) {
	level.Debug(h.logger).Log(
		"msg", "starting chunk download",
		"chunk_index", job.index,
		"start_byte", job.start,
		"end_byte", job.end,
	)

	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			if err := waitChunkRetry(ctx, h.logger, job.index, attempt, maxRetries, lastErr); err != nil {
				return nil, err
			}
		}

		data, err := h.fetchChunkRange(ctx, rawURL, auth, job)
		if err != nil {
			lastErr = err
			continue
		}

		level.Debug(h.logger).Log(
			"msg", "finished chunk download",
			"chunk_index", job.index,
			"bytes_downloaded", len(data),
		)
		return data, nil
	}
	return nil, lastErr
}

func waitChunkRetry(
	ctx context.Context,
	logger log.Logger,
	chunkIndex, attempt, maxRetries int,
	lastErr error,
) error {
	backoff := time.Second * time.Duration(1<<attempt)
	jitter := time.Duration(time.Now().UnixNano() % int64(time.Second))

	level.Warn(logger).Log(
		"msg", "retrying chunk download",
		"chunk_index", chunkIndex,
		"attempt", attempt,
		"max_retries", maxRetries,
		"backoff", backoff+jitter,
		"last_error", lastErr,
	)

	select {
	case <-time.After(backoff + jitter):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *HTTPDownloader) fetchChunkRange(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	job chunkJob,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	applyAuth(req, auth)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", job.start, job.end))

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		return nil, httpStatusError(resp.StatusCode, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// writeOrderedChunks writes chunk results to pw in index order, buffering
// out-of-order arrivals until the next expected index is available.
func writeOrderedChunks(logger log.Logger, pw *io.PipeWriter, results <-chan chunkResult) {
	buffer := make(map[int][]byte)
	nextIndex := 0

	for res := range results {
		if res.err != nil {
			pw.CloseWithError(res.err)
			return
		}

		if res.index != nextIndex {
			buffer[res.index] = res.data
			continue
		}

		if err := writeChunk(logger, pw, res.index, res.data); err != nil {
			return
		}
		nextIndex++

		for {
			data, ok := buffer[nextIndex]
			if !ok {
				break
			}
			if err := writeChunk(logger, pw, nextIndex, data); err != nil {
				return
			}
			delete(buffer, nextIndex)
			nextIndex++
		}
	}
}

func writeChunk(logger log.Logger, pw *io.PipeWriter, index int, data []byte) error {
	level.Debug(logger).Log("msg", "writing chunk to pipe", "chunk_index", index)
	_, err := pw.Write(data)
	return err
}

func maybeRateLimited(ctx context.Context, r io.ReadCloser, opts download.DownloadOptions) io.ReadCloser {
	if opts.MaxSpeed != nil && *opts.MaxSpeed > 0 {
		return newRateLimitedReader(ctx, r, *opts.MaxSpeed)
	}
	return r
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

// rateLimitedReader wraps an [io.ReadCloser] and throttles reads to maxBytesPerSec.
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
