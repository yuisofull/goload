package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/internal/task"
	"golang.org/x/time/rate"
)

type HTTP struct {
	client *http.Client
}

// NewHTTPDownloader creates a new HTTP downloader.
// If client is nil, a sane default client is used.
func NewHTTPDownloader(client *http.Client) *HTTP {
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	return &HTTP{client: client}
}

func (d *HTTP) SupportsResume() bool {
	// Protocol supports Range requests. Actual server capability
	// is detected in GetFileInfo via Accept-Ranges/Content-Range.
	return true
}

// GetFileInfo attempts a HEAD request first; if the server doesn’t support it,
// falls back to a 1-byte ranged GET to infer size and whether resume is supported.
func (d *HTTP) GetFileInfo(ctx context.Context, rawURL string, auth *task.AuthConfig) (*download.FileMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return nil, err
	}
	applyAuthAndHeaders(req, auth)

	resp, err := d.client.Do(req)
	if err != nil || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound {
		// Fallback: GET with Range: bytes=0-0
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		var fbErr error
		return d.probeWithRangedGet(ctx, rawURL, auth, &fbErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HEAD %s returned status %d", rawURL, resp.StatusCode)
	}

	filename := pickFileName(resp.Header.Get("Content-Disposition"), rawURL)
	size := resp.ContentLength
	if size < 0 {
		// Some servers omit Content-Length on HEAD. Try ranged GET as a last resort.
		var fbErr error
		meta, err := d.probeWithRangedGet(ctx, rawURL, auth, &fbErr)
		if err == nil {
			// carry over headers from HEAD if ranged succeeded but keep filename from HEAD first
			if filename == "" {
				filename = meta.FileName
			}
			meta.FileName = filename
			return meta, nil
		}
		// Keep size unknown; still return basic info.
	}

	return &download.FileMetadata{
		FileName:    filename,
		FileSize:    size,
		ContentType: resp.Header.Get("Content-Type"),
		Headers:     cloneHeader(resp.Header),
	}, nil
}

// Download issues a GET request and returns a streaming body and the total size if known.
// Caller must Close the returned ReadCloser.
func (d *HTTP) Download(ctx context.Context, rawURL string, auth *task.AuthConfig, opts task.DownloadOptions) (io.ReadCloser, int64, error) {
	client := *d.client
	if opts.Timeout != nil && *opts.Timeout > 0 {
		client.Timeout = time.Duration(*opts.Timeout) * time.Second
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, -1, err
	}
	applyAuthAndHeaders(req, auth)

	resp, err := client.Do(req)
	if err != nil {
		return nil, -1, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, -1, fmt.Errorf("GET %s returned status %d", rawURL, resp.StatusCode)
	}

	total := resp.ContentLength
	reader := io.ReadCloser(resp.Body)
	if opts.MaxSpeed != nil && *opts.MaxSpeed > 0 {
		reader = newRateLimitedReadCloser(resp.Body, *opts.MaxSpeed)
	}

	return reader, total, nil
}

func (d *HTTP) probeWithRangedGet(ctx context.Context, rawURL string, auth *task.AuthConfig, outErr *error) (*download.FileMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		*outErr = err
		return nil, err
	}
	req.Header.Set("Range", "bytes=0-0")
	applyAuthAndHeaders(req, auth)

	resp, err := d.client.Do(req)
	if err != nil {
		*outErr = err
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		*outErr = fmt.Errorf("ranged GET returned status %d", resp.StatusCode)
		return nil, *outErr
	}

	// Try to infer total size from Content-Range: bytes 0-0/12345
	total := int64(-1)
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if n, ok := parseTotalFromContentRange(cr); ok {
			total = n
		}
	}

	filename := pickFileName(resp.Header.Get("Content-Disposition"), rawURL)

	return &download.FileMetadata{
		FileName:    filename,
		FileSize:    total,
		ContentType: resp.Header.Get("Content-Type"),
		Headers:     cloneHeader(resp.Header),
	}, nil
}

func pickFileName(contentDisp, rawURL string) string {
	// Content-Disposition: attachment; filename="name.ext"; filename*=UTF-8''name.ext
	if contentDisp != "" {
		// Attempt RFC 5987 filename* first
		if i := strings.Index(strings.ToLower(contentDisp), "filename*="); i >= 0 {
			v := contentDisp[i+10:]
			if j := strings.Index(v, ";"); j >= 0 {
				v = v[:j]
			}
			// Expect UTF-8''encoded
			if k := strings.Index(v, "''"); k >= 0 && k+2 < len(v) {
				decoded, err := url.QueryUnescape(strings.Trim(v[k+2:], "\""))
				if err == nil && decoded != "" {
					return decoded
				}
			}
		}
		// Fallback to filename=
		if i := strings.Index(strings.ToLower(contentDisp), "filename="); i >= 0 {
			v := strings.TrimSpace(contentDisp[i+9:])
			v = strings.Trim(v, "\"")
			if v != "" {
				return v
			}
		}
	}
	// Fallback to URL path base
	u, err := url.Parse(rawURL)
	if err != nil {
		return "download"
	}
	base := path.Base(u.Path)
	if base == "" || base == "/" || base == "." {
		return "download"
	}
	// strip query suffix like "?x=y"
	if idx := strings.IndexByte(base, '?'); idx >= 0 {
		base = base[:idx]
	}
	return base
}

func parseTotalFromContentRange(v string) (int64, bool) {
	// Example: "bytes 0-0/12345"
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "bytes") {
		return -1, false
	}
	if idx := strings.LastIndex(v, "/"); idx >= 0 && idx+1 < len(v) {
		n, err := strconv.ParseInt(v[idx+1:], 10, 64)
		if err == nil {
			return n, true
		}
	}
	return -1, false
}

func applyAuthAndHeaders(req *http.Request, auth *task.AuthConfig) {
	if auth == nil {
		return
	}
	for k, v := range auth.Headers {
		if k == "" || v == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	typ := strings.ToLower(strings.TrimSpace(auth.Type))
	switch typ {
	case "basic":
		if auth.Username != "" || auth.Password != "" {
			req.SetBasicAuth(auth.Username, auth.Password)
		}
	case "bearer", "token":
		if auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+auth.Token)
		}
	default:
		// If only username/password is set, assume basic
		if auth.Username != "" || auth.Password != "" {
			req.SetBasicAuth(auth.Username, auth.Password)
		}
		// If only token is set, assume bearer
		if req.Header.Get("Authorization") == "" && auth.Token != "" {
			req.Header.Set("Authorization", "Bearer "+auth.Token)
		}
	}
}

func cloneHeader(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vv := range h {
		if len(vv) > 0 {
			out[k] = vv[0]
		}
	}
	return out
}

type rateLimitedReadCloser struct {
	rc      io.ReadCloser
	bps     int64 // Bytes per second rate limit
	limiter *rate.Limiter
}

func newRateLimitedReadCloser(rc io.ReadCloser, bps int64) *rateLimitedReadCloser {
	return &rateLimitedReadCloser{
		rc:      rc,
		bps:     bps,
		limiter: rate.NewLimiter(rate.Limit(bps), int(bps)),
	}
}

func (r *rateLimitedReadCloser) Read(p []byte) (int, error) {
	if r.bps <= 0 {
		return r.rc.Read(p)
	}

	// Limit chunk size to roughly 1/10 second worth to smooth throughput
	maxChunk := r.bps / 10
	if maxChunk <= 0 {
		maxChunk = 1
	}

	if int64(len(p)) > maxChunk {
		p = p[:maxChunk]
	}

	n, err := r.rc.Read(p)

	if n > 0 && err == nil {
		if err := r.limiter.WaitN(context.Background(), n); err != nil {
			return n, err
		}
	}

	return n, err
}

func (r *rateLimitedReadCloser) Close() error {
	return r.rc.Close()
}
