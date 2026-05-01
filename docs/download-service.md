# Download Service

The Download Service is a **worker** process. It listens for task lifecycle events, physically downloads files from the configured source, streams them to the storage backend, and publishes progress/completion/failure events so the Task Service can update its state.

In microservice mode the event bus is Kafka and the storage backend is MinIO. In pocket mode the same download domain service runs inside `cmd/pocket`, using an in-memory event bus and local filesystem storage.

---

## Responsibilities

- Execute file downloads triggered by `TaskCreated` events
- Support pause, resume, and cancel of in-flight downloads
- Write downloaded files to the configured storage backend
- Compute MD5 checksum of the downloaded content
- Publish status, progress, completion, and failure events to Kafka
- Support concurrent task execution with a configurable concurrency limit

---

## Architecture

```
cmd/download/main.go
    └── internal/download/service.go       ← Business logic + concurrency control
        ├── internal/download/downloader.go       ← Downloader interface
        ├── internal/download/downloader/http.go  ← HTTP/HTTPS downloader
        ├── internal/download/event_publisher.go  ← Publishes events to Kafka
        ├── internal/download/request.go          ← Internal request/response types
        └── internal/download/transport/
            └── event_consumer.go                 ← Kafka consumer (receives task events)
```

### Layer description

| Layer | Package | Role |
|-------|---------|------|
| Domain | `internal/download` | `Service` interface, `Downloader` interface, internal data types |
| Downloader | `internal/download/downloader` | HTTP/HTTPS, FTP, and BitTorrent downloaders |
| Event publish | `internal/download/event_publisher.go` | Wraps `message.Publisher` to emit download events |
| Event consume | `internal/download/transport/event_consumer.go` | Subscribes to task events and dispatches to `Service` |
| Storage | `internal/storage` | `Backend` interface (MinIO and local filesystem implementations) |

---

## Service Interface

```go
type Service interface {
    ExecuteTask(ctx context.Context, req TaskRequest) error
    PauseTask(ctx context.Context, taskID uint64) error
    ResumeTask(ctx context.Context, taskID uint64) error
    CancelTask(ctx context.Context, taskID uint64) error
    StreamFile(ctx context.Context, req FileStreamRequest) (*FileStreamResponse, error)
    GetActiveTaskCount(ctx context.Context) int
}
```

---

## Download Execution Flow

When a `TaskCreated` event arrives:

```
EventConsumer.processTaskCreatedEvents
    └── service.ExecuteTask(req)
        1. Acquire semaphore slot (concurrency limit)
        2. Lookup Downloader by SourceType
        3. Publish status → DOWNLOADING
        4. Call downloader.GetFileInfo (get filename, size, content-type)
        5. Retry loop (configurable MaxRetries, exponential backoff + jitter):
               downloader.Download → io.ReadCloser
        6. Publish status → STORING
        7. Wrap reader in PausableProgressReader
           (publishes TaskProgressUpdated events periodically)
        8. Compute MD5 hash while streaming
        9. storage.Store(key, reader, metadata) → backend write
       10. Publish TaskCompleted (with StorageKey, checksum, size)
       11. On error: Publish TaskFailed
        └── Release semaphore slot
```

### Concurrency control

A `semaphore.Weighted` (from `golang.org/x/sync`) limits the number of simultaneous downloads. Default: **5**. Configurable with `WithMaxConcurrent(n)`.

### Retry strategy

- Default: **3 retries**
- Per-attempt backoff: `2^attempt` seconds + random jitter up to 1 second
- Each retry attempt calls `downloader.Download` again from the beginning

### Progress updates

A `PausableProgressReader` wraps the download `io.Reader` and fires a callback on each read. The callback publishes a `TaskProgressUpdated` event to Kafka (rate-limited to avoid flooding).

---

## Pause / Resume / Cancel

| Command | Event consumed | Action |
|---------|---------------|--------|
| Pause | `task.paused` | `service.PauseTask` — pauses the `PausableProgressReader` (blocks reads) |
| Resume | `task.resumed` | `service.ResumeTask` — resumes the reader |
| Cancel | `task.cancelled` | `service.CancelTask` — cancels the task context |

---

## Event Flow

### Consumed events (Kafka → Download Service)

| Topic | Event | Handler |
|-------|-------|---------|
| `task.created` | `TaskCreatedEvent` | Execute the download in a goroutine |
| `task.paused` | `TaskPausedEvent` | Pause the active download |
| `task.resumed` | `TaskResumedEvent` | Resume the paused download |
| `task.cancelled` | `TaskCancelledEvent` | Cancel and clean up |

### Published events (Download Service → Kafka)

| Topic | Event | When |
|-------|-------|------|
| `task.status.updated` | `TaskStatusUpdatedEvent` | Status transitions (DOWNLOADING, STORING) |
| `task.progress.updated` | `TaskProgressUpdatedEvent` | Periodic progress reports |
| `task.completed` | `TaskCompletedEvent` | Successful finish |
| `task.failed` | `TaskFailedEvent` | Any unrecoverable error |

---

## Downloader Interface

```go
type Downloader interface {
    Download(ctx, url, auth, opts) (reader io.ReadCloser, total int64, err error)
    GetFileInfo(ctx, url, auth) (metadata *FileMetadata, err error)
    SupportsResume() bool
}
```

Currently implemented:

- **HTTP/HTTPS** (`internal/download/downloader/http.go`)
- **FTP** (`internal/download/downloader/ftp.go`)
- **BitTorrent** (`internal/download/downloader/bittorrent.go`) for magnet links, `.torrent` URLs, and uploaded `.torrent` bytes

To add a new protocol (e.g. FTP, BitTorrent), implement this interface and register it with `service.RegisterDownloader("FTP", myDownloader)`.

---

## Storage Backend

The service uses `storage.Backend` (write + read). Microservice mode normally uses MinIO; pocket mode uses the local filesystem. The storage key format is:

```
{TaskID}/{safeFileName}-{sourceHash}{ext}
```

The filename is chosen from downloader metadata first, then task filename, then URL path. This preserves extensions for opaque or signed source URLs when metadata contains the real filename.

Example:

```
1/design_rationale_example_1-9e04fb677787202d.pdf
```

The backend stores the file and returns it for streaming via `storage.Reader.Get`. Local storage also writes a sidecar `.meta.json` file with the resolved filename, size, content type, storage key, and timestamps.

---

## Configuration

```yaml
apigateway:
  storage:
    minio:
      endpoint: "http://minio:9000"
      access_key: "minioadmin"
      secret_key: "minioadmin"
      bucket: "goload"
      use_ssl: false

messaging:
  kafka:
    brokers: ["broker:9092"]
    version: "4.0.0"
    consumer_group: "download-service-group"
```

> The Download Service reuses the `apigateway.storage.minio` config block for its MinIO backend.

---

## Entry Point

`cmd/download/main.go`

Startup sequence:
1. Load config.
2. Initialise MinIO backend (required in microservice mode — exits on failure).
3. Create Kafka publisher and subscriber (required — exits on failure).
4. Create `DownloadEventPublisher`.
5. Create `download.Service`.
6. Create `EventConsumer`.
7. Start `consumer.Start(ctx)` in run group (blocks until context cancelled).
8. Gracefully close Kafka publisher and subscriber on shutdown.

---

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `golang.org/x/sync/semaphore` | Bounded concurrency for downloads |
| `github.com/minio/minio-go/v7` | MinIO object storage client |
| `github.com/IBM/sarama` | Kafka client |
| `github.com/go-kit/log` | Structured logging |
| `crypto/md5` | Content checksum (stdlib) |

