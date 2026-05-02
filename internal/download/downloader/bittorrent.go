package downloader

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/go-kit/log"

	"github.com/yuisofull/goload/internal/download"
)

// BitTorrentDownloader handles BitTorrent source metadata and downloads using anacrolix/torrent.
type BitTorrentDownloader struct {
	client  *torrent.Client
	logger  log.Logger
	dataDir string
}

type BitTorrentDownloaderOption func(*BitTorrentDownloader)

func WithBitTorrentLogger(logger log.Logger) BitTorrentDownloaderOption {
	return func(b *BitTorrentDownloader) {
		b.logger = logger
	}
}

func NewBitTorrentDownloader(opts ...BitTorrentDownloaderOption) (btDl *BitTorrentDownloader, closeFunc func(), err error) {
	b := &BitTorrentDownloader{
		logger: log.NewNopLogger(),
	}
	for _, opt := range opts {
		opt(b)
	}

	dataDir, err := os.MkdirTemp("", "goload-torrent-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create torrent data dir: %w", err)
	}
	b.dataDir = dataDir

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = dataDir

	client, err := torrent.NewClient(cfg)
	if err != nil {
		_ = os.RemoveAll(dataDir)
		return nil, nil, fmt.Errorf("failed to create torrent client: %w", err)
	}
	b.client = client

	return b, func() {
		b.Close()
	}, nil
}

func (b *BitTorrentDownloader) Close() {
	if b.client != nil {
		b.client.Close()
	}
	if b.dataDir != "" {
		os.RemoveAll(b.dataDir)
	}
}

func (b *BitTorrentDownloader) SupportsResume() bool { return false }

func (b *BitTorrentDownloader) addTorrent(ctx context.Context, rawURL string) (*torrent.Torrent, error) {
	if strings.HasPrefix(rawURL, "data:application/x-bittorrent;base64,") {
		encoded := strings.TrimPrefix(rawURL, "data:application/x-bittorrent;base64,")
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode base64 torrent: %w", err)
		}

		info, err := metainfo.Load(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("load metainfo: %w", err)
		}

		t, err := b.client.AddTorrent(info)
		if err != nil {
			return nil, fmt.Errorf("add torrent to client: %w", err)
		}
		return t, nil
	}

	if strings.HasPrefix(rawURL, "magnet:") {
		t, err := b.client.AddMagnet(rawURL)
		if err != nil {
			return nil, fmt.Errorf("add magnet: %w", err)
		}
		return t, nil
	}

	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create http request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch torrent file: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch torrent file failed: %s", resp.Status)
		}

		info, err := metainfo.Load(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("load metainfo from http: %w", err)
		}

		t, err := b.client.AddTorrent(info)
		if err != nil {
			return nil, fmt.Errorf("add torrent from http: %w", err)
		}
		return t, nil
	}

	if strings.HasPrefix(rawURL, "file://") {
		path := strings.TrimPrefix(rawURL, "file://")
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open torrent file: %w", err)
		}
		defer f.Close()

		info, err := metainfo.Load(f)
		if err != nil {
			return nil, fmt.Errorf("load metainfo from file: %w", err)
		}

		t, err := b.client.AddTorrent(info)
		if err != nil {
			return nil, fmt.Errorf("add torrent from file: %w", err)
		}
		return t, nil
	}

	return nil, fmt.Errorf("unsupported torrent url format: %s", truncateURL(rawURL, 50))
}

func truncateURL(url string, maxLen int) string {
	if len(url) <= maxLen {
		return url
	}
	return url[:maxLen] + "..."
}

func (b *BitTorrentDownloader) GetFileInfo(
	ctx context.Context,
	rawURL string,
	_ *download.AuthConfig,
) (*download.FileMetadata, error) {
	t, err := b.addTorrent(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	info := t.Info()

	var name string
	if info.Name != "" {
		name = info.Name
	} else {
		name = t.Name()
	}

	return &download.FileMetadata{
		FileName:    name,
		FileSize:    info.TotalLength(),
		ContentType: "application/octet-stream",
	}, nil
}

type wrappedTorrentReader struct {
	reader io.ReadCloser
	t      *torrent.Torrent
}

func (w *wrappedTorrentReader) Read(p []byte) (n int, err error) {
	return w.reader.Read(p)
}

func (w *wrappedTorrentReader) Close() error {
	err := w.reader.Close()
	w.t.Drop()
	return err
}

func (b *BitTorrentDownloader) Download(
	ctx context.Context,
	rawURL string,
	_ *download.AuthConfig,
	_ download.DownloadOptions,
) (io.ReadCloser, int64, error) {
	t, err := b.addTorrent(ctx, rawURL)
	if err != nil {
		return nil, 0, err
	}

	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}

	// Prioritize downloading all files
	t.DownloadAll()

	reader := t.NewReader()

	return &wrappedTorrentReader{
		reader: reader,
		t:      t,
	}, t.Info().TotalLength(), nil
}
