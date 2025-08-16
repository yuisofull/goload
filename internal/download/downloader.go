package download

import (
	"context"
	"io"
)

// Downloader interface for different download sources
type Downloader interface {
	Download(ctx context.Context, url string, sourceAuth *AuthConfig, opts DownloadOptions) (reader io.ReadCloser, total int64, err error)
	GetFileInfo(ctx context.Context, url string, sourceAuth *AuthConfig) (metadata *FileMetadata, err error)
	SupportsResume() bool
}
