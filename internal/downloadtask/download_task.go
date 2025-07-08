package downloadtask

type DownloadType int

const (
	DOWNLOAD_TYPE_UNSPECIFIED DownloadType = iota
	DOWNLOAD_TYPE_HTTP
)

type DownloadStatus int

const (
	DOWNLOAD_STATUS_UNSPECIFIED DownloadStatus = iota
	DOWNLOAD_STATUS_PENDING
	DOWNLOAD_STATUS_DOWNLOADING
	DOWNLOAD_STATUS_FAILED
	DOWNLOAD_STATUS_SUCCESS
)

type DownloadTask struct {
	Id             uint64
	OfAccountId    uint64
	Name           string
	DownloadType   DownloadType
	DownloadStatus DownloadStatus
	Url            string
	Metadata       map[string]any
}
