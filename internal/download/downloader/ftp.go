package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/yuisofull/goload/internal/download"
)

// FTPDownloader implements download.Downloader for FTP sources.
//
// It supports anonymous login by default and basic username/password
// authentication via AuthConfig or URL credentials.
type FTPDownloader struct {
	timeout time.Duration
}

// NewFTPDownloader creates an FTP downloader with a dial timeout.
// Pass 0 to use the default timeout.
func NewFTPDownloader(timeout time.Duration) *FTPDownloader {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &FTPDownloader{timeout: timeout}
}

// SupportsResume returns false because resume support is not currently implemented.
func (f *FTPDownloader) SupportsResume() bool { return false }

// GetFileInfo resolves metadata for an FTP path using SIZE when available.
func (f *FTPDownloader) GetFileInfo(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
) (*download.FileMetadata, error) {
	conn, filePath, parsedURL, err := f.connect(ctx, rawURL, auth)
	if err != nil {
		return nil, err
	}
	defer conn.Quit()

	fileSize, err := conn.FileSize(filePath)
	if err != nil {
		fileSize = 0
	}

	fileName := path.Base(parsedURL.Path)
	if fileName == "." || fileName == "/" {
		fileName = ""
	}

	return &download.FileMetadata{
		FileName:    fileName,
		FileSize:    fileSize,
		ContentType: mime.TypeByExtension(path.Ext(fileName)),
		Headers:     map[string]string{},
	}, nil
}

// Download retrieves an FTP file as a stream.
func (f *FTPDownloader) Download(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
	_ download.DownloadOptions,
) (io.ReadCloser, int64, error) {
	conn, filePath, _, err := f.connect(ctx, rawURL, auth)
	if err != nil {
		return nil, 0, err
	}

	fileSize, err := conn.FileSize(filePath)
	if err != nil {
		fileSize = 0
	}

	r, err := conn.Retr(filePath)
	if err != nil {
		_ = conn.Quit()
		return nil, 0, fmt.Errorf("ftp RETR %s: %w", filePath, err)
	}

	return &ftpReadCloser{reader: r, conn: conn}, fileSize, nil
}

func (f *FTPDownloader) connect(
	ctx context.Context,
	rawURL string,
	auth *download.AuthConfig,
) (*ftp.ServerConn, string, *url.URL, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", nil, fmt.Errorf("parse ftp url %q: %w", rawURL, err)
	}

	if !strings.EqualFold(parsedURL.Scheme, "ftp") {
		return nil, "", nil, fmt.Errorf("unsupported ftp scheme %q", parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return nil, "", nil, errors.New("ftp url missing host")
	}

	filePath := parsedURL.Path
	if filePath == "" || filePath == "/" {
		return nil, "", nil, errors.New("ftp url missing file path")
	}

	conn, err := ftp.Dial(
		parsedURL.Host,
		ftp.DialWithContext(ctx),
		ftp.DialWithTimeout(f.timeout),
	)
	if err != nil {
		return nil, "", nil, fmt.Errorf("dial ftp host %q: %w", parsedURL.Host, err)
	}

	username, password := resolveFTPCredentials(parsedURL, auth)
	if err := conn.Login(username, password); err != nil {
		_ = conn.Quit()
		return nil, "", nil, fmt.Errorf("ftp login failed for user %q: %w", username, err)
	}

	return conn, filePath, parsedURL, nil
}

func resolveFTPCredentials(parsedURL *url.URL, auth *download.AuthConfig) (string, string) {
	if auth != nil && (auth.Username != "" || auth.Password != "") {
		return auth.Username, auth.Password
	}

	if parsedURL.User != nil {
		password, _ := parsedURL.User.Password()
		return parsedURL.User.Username(), password
	}

	return "anonymous", "anonymous"
}

type ftpReadCloser struct {
	reader io.ReadCloser
	conn   *ftp.ServerConn
}

func (r *ftpReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *ftpReadCloser) Close() error {
	readErr := r.reader.Close()
	quitErr := r.conn.Quit()
	if readErr != nil {
		return readErr
	}
	return quitErr
}
