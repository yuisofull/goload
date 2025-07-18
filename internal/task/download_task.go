package task

import "github.com/yuisofull/goload/internal/file"

type DownloadTask struct {
	Id             uint64
	OfAccountId    uint64
	Name           string
	DownloadType   file.DownloadType
	DownloadStatus file.DownloadStatus
	Url            string
	Metadata       map[string]any
}
