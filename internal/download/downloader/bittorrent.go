package downloader

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/yuisofull/goload/internal/download"
	"github.com/yuisofull/goload/pkg/bittorrent"
)

// BitTorrentDownloader handles BitTorrent source metadata.
//
// It currently parses magnet links for metadata and registers a concrete
// downloader for BITTORRENT source type. Full swarm download behavior can be
// added later without changing service wiring.
type BitTorrentDownloader struct {
	client *bittorrent.Downloader
}

func NewBitTorrentDownloader() *BitTorrentDownloader {
	client, err := bittorrent.NewDownloader(bittorrent.Config{})
	if err != nil {
		// Construction is deterministic; panic keeps startup failure explicit.
		panic(err)
	}
	return &BitTorrentDownloader{client: client}
}

// SupportsResume returns false for now.
func (b *BitTorrentDownloader) SupportsResume() bool { return false }

// GetFileInfo extracts metadata from a magnet URI.
func (b *BitTorrentDownloader) GetFileInfo(
	ctx context.Context,
	rawURL string,
	_ *download.AuthConfig,
) (*download.FileMetadata, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse bittorrent url %q: %w", rawURL, err)
	}

	if strings.EqualFold(parsedURL.Scheme, "magnet") {
		query := parsedURL.Query()
		name := query.Get("dn")
		size := parseMagnetLength(query.Get("xl"))

		headers := map[string]string{}
		if xt := query.Get("xt"); xt != "" {
			headers["XT"] = xt
		}

		if name != "" || size > 0 {
			return &download.FileMetadata{
				FileName:    name,
				FileSize:    size,
				ContentType: "application/octet-stream",
				Headers:     headers,
			}, nil
		}

		torrentSource := bittorrent.ResolveTorrentSourceFromMagnet(parsedURL)
		if torrentSource == "" {
			return nil, fmt.Errorf("magnet link missing dn/xl metadata and xs/as torrent source")
		}

		tf, err := b.client.OpenTorrentFromSource(ctx, torrentSource)
		if err != nil {
			return nil, fmt.Errorf("open torrent metadata from magnet source: %w", err)
		}
		return &download.FileMetadata{
			FileName:    tf.Name,
			FileSize:    int64(tf.Length),
			ContentType: "application/octet-stream",
			Headers: map[string]string{
				"Announce": tf.Announce,
			},
		}, nil
	}

	if !isTorrentScheme(parsedURL.Scheme) {
		return nil, fmt.Errorf("unsupported bittorrent scheme %q", parsedURL.Scheme)
	}

	tf, err := b.client.OpenTorrentFromSource(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("open torrent metadata: %w", err)
	}

	return &download.FileMetadata{
		FileName:    tf.Name,
		FileSize:    int64(tf.Length),
		ContentType: "application/octet-stream",
		Headers: map[string]string{
			"Announce": tf.Announce,
		},
	}, nil
}

// Download is intentionally explicit while full BitTorrent transfer support is
// being added.
func (b *BitTorrentDownloader) Download(
	ctx context.Context,
	rawURL string,
	_ *download.AuthConfig,
	_ download.DownloadOptions,
) (io.ReadCloser, int64, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("parse bittorrent url %q: %w", rawURL, err)
	}

	if strings.EqualFold(parsedURL.Scheme, "magnet") {
		torrentSource := bittorrent.ResolveTorrentSourceFromMagnet(parsedURL)
		if torrentSource == "" {
			return nil, 0, fmt.Errorf("magnet download requires xs/as torrent source")
		}
		rawURL = torrentSource
	}

	parsedURL, err = url.Parse(rawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("parse bittorrent source url %q: %w", rawURL, err)
	}

	if !isTorrentScheme(parsedURL.Scheme) {
		return nil, 0, fmt.Errorf("unsupported bittorrent scheme %q", parsedURL.Scheme)
	}

	tf, err := b.client.OpenTorrentFromSource(ctx, rawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("open torrent metadata: %w", err)
	}

	res, err := b.client.Download(ctx, tf)
	if err != nil {
		return nil, 0, fmt.Errorf("bittorrent download failed: %w", err)
	}

	return &tempFileReader{
		file: res.File,
		path: res.Path,
	}, res.Size, nil
}

func isTorrentScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https") || strings.EqualFold(scheme, "data") || scheme == ""
}

type tempFileReader struct {
	file *os.File
	path string
}

func (r *tempFileReader) Read(p []byte) (int, error) {
	return r.file.Read(p)
}

func (r *tempFileReader) Close() error {
	closeErr := r.file.Close()
	removeErr := os.Remove(r.path)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func parseMagnetLength(raw string) int64 {
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}