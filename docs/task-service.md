# Task Service

The Task Service is the central state-management service for file-download tasks. It persists task records in MySQL, publishes lifecycle events to Kafka, consumes completion/progress events back from the Download Service, and exposes a **gRPC** API to the API Gateway.

---

## Responsibilities

- CRUD operations for download tasks (create, get, list, delete)
- Task lifecycle management: pause, resume, cancel, retry
- Receive progress/completion/failure updates from the Download Service (via Kafka)
- Generate download URLs for completed tasks (presigned or token-based)
- Track storage path and checksum of downloaded files

---

## Architecture

```
cmd/task/main.go
    └── internal/task/service.go         ← Business logic
        ├── internal/task/mysql/          ← MySQL persistence (via sqlc)
        ├── internal/task/event_publisher.go ← Publishes events to Kafka
        ├── internal/task/tokenstore.go   ← Redis-backed token store for download URLs
        ├── internal/task/endpoint/       ← go-kit endpoint set
        └── internal/task/transport/
            ├── grpc.go                   ← gRPC server & client
            ├── event_consumer.go         ← Kafka consumer (progress/complete/fail)
            └── log.go                    ← Request/response logging middleware
```

### Layer description

| Layer | Package | Role |
|-------|---------|------|
| Domain | `internal/task` | `Service` interface, `Task` struct, status/source-type constants, error codes |
| Persistence | `internal/task/mysql` | `Repository`, `TxManager` backed by MySQL via `sqlc` |
| Messaging (publish) | `internal/task/event_publisher.go` | Wraps `message.Publisher` to emit task lifecycle events |
| Messaging (consume) | `internal/task/transport/event_consumer.go` | Subscribes to download-service events and calls `Service` update methods |
| Token Store | `internal/task/tokenstore.go` | HMAC-signed tokens stored in Redis for one-time download URLs |
| Endpoint | `internal/task/endpoint` | go-kit endpoint set, per-endpoint rate limiting |
| Transport | `internal/task/transport/grpc.go` | gRPC server + client; maps protobuf ↔ endpoint types |

---

## gRPC API

Proto file: `api/task.proto`  
Package: `task.v1.TaskService`

### Task Management

| Method | Description |
|--------|-------------|
| `CreateTask` | Create a new download task (triggers `TaskCreated` Kafka event) |
| `GetTask` | Fetch a single task by ID |
| `ListTasks` | Paginated list with optional filter (status, source type, date range, search) |
| `DeleteTask` | Remove a task record |
| `PauseTask` | Signal the download worker to pause |
| `ResumeTask` | Signal the download worker to resume |
| `CancelTask` | Cancel an in-progress or pending task |
| `RetryTask` | Re-queue a failed task |

### Internal (called by Download Service)

| Method | Description |
|--------|-------------|
| `UpdateTaskStatus` | Update `status` field |
| `UpdateTaskProgress` | Update progress percentage and byte counters |
| `UpdateTaskError` | Record an error message |
| `CompleteTask` | Mark as COMPLETED, set `completedAt` |
| `UpdateTaskStoragePath` | Save the storage key after upload |
| `UpdateTaskChecksum` | Save checksum info |
| `UpdateTaskMetadata` | Store arbitrary key-value metadata |
| `CheckFileExists` | Check if the file is present in storage |
| `GetTaskProgress` | Return the latest `DownloadProgress` |
| `GenerateDownloadURL` | Return a presigned or token-based download URL |

---

## Task Lifecycle

```
PENDING
  │  (TaskCreated event → Download Service picks it up)
  ▼
DOWNLOADING
  │  (progress updates via Kafka)
  ▼
STORING
  │  (file is being written to MinIO)
  ▼
COMPLETED
```

Other states: `PAUSED`, `CANCELLED`, `FAILED`

---

## Domain Model

```go
type Task struct {
    ID              uint64
    OfAccountID     uint64
    FileName        string
    SourceURL       string
    SourceType      SourceType       // HTTP, HTTPS, FTP, SFTP, BITTORRENT
    SourceAuth      *AuthConfig
    StorageType     storage.Type
    StoragePath     string
    Checksum        *ChecksumInfo
    DownloadOptions *DownloadOptions // Concurrency, MaxSpeed, MaxRetries, Timeout
    Status          TaskStatus
    Progress        *DownloadProgress
    ErrorMessage    *string
    Metadata        map[string]any
    CreatedAt       time.Time
    UpdatedAt       time.Time
    CompletedAt     *time.Time
}
```

---

## Event Flow

### Published events (Task → Kafka)

| Topic | Event struct | Trigger |
|-------|-------------|---------|
| `task.created` | `TaskCreatedEvent` | `CreateTask` |
| `task.status.updated` | `TaskStatusUpdatedEvent` | Status change methods |
| `task.paused` | `TaskPausedEvent` | `PauseTask` |
| `task.resumed` | `TaskResumedEvent` | `ResumeTask` |
| `task.cancelled` | `TaskCancelledEvent` | `CancelTask` |

### Consumed events (Kafka → Task Service)

| Topic | Event struct | Handler action |
|-------|-------------|---------------|
| `task.progress.updated` | `TaskProgressUpdatedEvent` | `UpdateTaskProgress` |
| `task.completed` | `TaskCompletedEvent` | `CompleteTask` + `UpdateStorageInfo` |
| `task.failed` | `TaskFailedEvent` | `UpdateTaskError` + `UpdateTaskStatus(FAILED)` |

---

## Download URL Generation

`GenerateDownloadURL(ctx, taskID, ttl, oneTime)` returns a URL in one of two modes:

1. **Presigned** (direct=true): if the storage backend implements `storage.Presigner` and `oneTime=false`, returns a MinIO presigned GET URL.
2. **Token-based** (direct=false): generates a UUID token → HMAC-signs it → stores `TokenMetadata` in Redis with TTL → returns `/download?token=<uuid>`. The API Gateway handles the `/download` route.

---

## Caching & Storage

| Store | Technology | Key | TTL | Purpose |
|-------|-----------|-----|-----|---------|
| Token store | Redis (HMAC key) | `HMAC256(token)` | configurable | One-time download tokens |

---

## Error Codes

Standard codes from `internal/errors`:

| Code | Meaning |
|------|---------|
| `INVALID_INPUT` | Missing/bad field (e.g. empty SourceURL) |
| `NOT_FOUND` | Task not found |
| `INVALID_STATE` | Operation not valid for current task state |
| `CONFLICT` | Task already running |
| `INTERNAL` | Unexpected server error |

---

## Configuration

```yaml
downloadtaskservice:
  grpc:
    address: "task:8082"

mysql:
  host: mysql
  port: 3306
  username: root
  password: example
  database: goload

messaging:
  kafka:
    brokers: ["broker:9092"]
    version: "4.0.0"
    consumer_group: "download-service-group"
```

---

## Entry Point

`cmd/task/main.go`

Startup sequence:
1. Load config.
2. Connect to MySQL (5-retry loop).
3. Create `taskmysql.TaskRepo` and `TxManager`.
4. (Optional) Create Kafka publisher.
5. Wrap publisher in `task.Publisher` event publisher.
6. Create `task.Service`.
7. Build go-kit endpoint set.
8. Start gRPC server.
9. Wait for `SIGINT`/`SIGTERM`.

> **Note:** The Kafka event consumer (`EventConsumer`) is wired in `cmd/task/main.go` but the `Start` call must be added to the `run.Group` if consuming events inside the Task Service process. Currently, the Task Service acts purely as a gRPC server; events from Download Service are pushed via gRPC calls in the current setup.

---

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/go-kit/kit` | Service/endpoint/transport framework |
| `github.com/IBM/sarama` | Kafka client |
| `github.com/redis/go-redis/v9` | Redis client |
| `github.com/google/uuid` | Token UUID generation |
| `sqlc` | Type-safe MySQL queries |

