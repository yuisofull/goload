# Goload - Documentation Index

Goload is a file-download manager built with Go. It can run in two modes:

- **Microservice mode**: API Gateway, Auth, Task, and Download services communicate over gRPC and Kafka, with MySQL, Redis, and MinIO for persistence.
- **Pocket edition**: a single local binary runs the API, web UI, SQLite persistence, in-memory events, and local filesystem storage.

---

## System Overview

```
                        ┌─────────────┐
  HTTP client ─────────►│ API Gateway │
                        │  :8080      │
                        └──────┬──────┘
                 gRPC          │           gRPC
          ┌──────────────┬─────┘──────────────────┐
          ▼              ▼                         ▼
  ┌───────────┐   ┌──────────────┐        (future services)
  │   Auth    │   │     Task     │
  │  Service  │   │   Service    │
  │  :8081    │   │   :8082      │
  └─────┬─────┘   └──────┬───────┘
        │                │  Kafka events
        │         ┌──────┴────────────────┐
        │         │                       │
        │  ┌──────▼──────┐        ┌───────▼──────┐
        │  │  Download   │        │    Kafka     │
        │  │   Service   │◄───────│   Broker     │
        │  └──────┬──────┘        └──────────────┘
        │         │ (store file)
        │  ┌──────▼──────┐
        │  │    MinIO    │
        │  │  Object     │
        │  │  Storage    │
        │  └─────────────┘
        │
  ┌─────▼─────┐   ┌────────┐
  │   MySQL   │   │ Redis  │
  └───────────┘   └────────┘
```

### Services

| Service | Docs | Entry point | Port | Protocol |
|---------|------|-------------|------|----------|
| **API Gateway** | [apigateway-service.md](./apigateway-service.md) | `cmd/apigateway/main.go` | 8080 | HTTP/JSON |
| **Auth Service** | [auth-service.md](./auth-service.md) | `cmd/auth/svc/main.go` | 8081 | gRPC |
| **Task Service** | [task-service.md](./task-service.md) | `cmd/task/main.go` | 8082 | gRPC |
| **Download Service** | [download-service.md](./download-service.md) | `cmd/download/main.go` | — | Kafka (event-driven) |
| **Pocket Edition** | [pocket-edition.md](./pocket-edition.md) | `cmd/pocket/main.go` | 8080 | HTTP/JSON + local services |

### Shared packages

| Package | Docs | Description |
|---------|------|-------------|
| `pkg/message` | [pkg-message.md](./pkg-message.md) | Pub/Sub abstraction + Kafka implementation |
| `pkg/cache` | — | Generic cache interface + Redis/in-memory implementations |
| `pkg/crypto` | — | bcrypt hasher and RSA key helpers |
| `internal/events` | — | Shared event struct definitions |
| `internal/storage` | — | Storage `Backend`/`Reader`/`Writer`/`Presigner` interfaces + MinIO and local filesystem implementations |
| `internal/errors` | — | Typed error codes and gRPC error encoder |

---

## Typical Request Flows

### Register & Login

```
Client → POST /api/v1/auth/create  → API Gateway → Auth Service (gRPC CreateAccount)
Client → POST /api/v1/auth/session → API Gateway → Auth Service (gRPC CreateSession) → JWT returned
```

### Create a Download Task

```
Client → POST /api/v1/tasks/create (Bearer <JWT>)
  → API Gateway: verify JWT (Auth Service.VerifySession)
  → API Gateway: forward to Task Service (gRPC CreateTask)
  → Task Service: insert task row, publish TaskCreated to Kafka
  → Download Service: consume TaskCreated → execute download
      → publish TaskStatusUpdated (DOWNLOADING)
      → publish TaskProgressUpdated (periodic)
      → publish TaskStatusUpdated (STORING)
      → upload file to storage backend
      → publish TaskCompleted
  → Task Service: consume TaskCompleted → mark task COMPLETED, save StoragePath
```

### Download a Completed File

```
Client → GET /api/v1/tasks/get?id=<id> (Bearer <JWT>)
  → API Gateway: verify JWT, check ownership
  → Task Service: GenerateDownloadURL (returns /download?token=<uuid> or presigned URL)

Client → GET /download?token=<uuid>
  → API Gateway: ConsumeToken from token store
  → Validate expiry + ownership
  → Stream file from storage backend
```

### Pocket: Reveal a Completed Local File

```
Client → POST /api/v1/pocket/tasks/reveal?id=<id>
  → Pocket server resolves the task storage key under POCKET_DATA_DIR
  → Launches the OS file manager for the stored file
  → No browser download copy is created
```

---

## Infrastructure

| Component | Image | Purpose |
|-----------|-------|---------|
| MySQL 9.3 | `mysql:9.3.0` | Persistent storage for accounts and tasks |
| Kafka 4.0 | `bitnamilegacy/kafka:4.0.0` | Inter-service event bus |
| Redis 8 | `redis:8.0.2` | Caching, token store |
| MinIO | `quay.io/minio/minio` | Object storage for downloaded files |
| SQLite | embedded | Pocket edition task/auth persistence |
| Local filesystem | host directory | Pocket edition downloaded-file storage |

See `deployments/docker-compose.yaml` for the full container configuration.

---

## Running Locally

```bash
cd deployments
docker-compose up
```

Run database migrations:

```bash
bash deployments/run-migrations.sh
```

## Running Pocket Edition

Pocket is built as a single local app. It serves the API and the compiled frontend from one process.

You can run Pocket in two ways:

**Option 1: Build from source (recommended for development)**

```bash
docker build -t goload-pocket-builder -f Dockerfile.pocketbuilder .
docker run --rm -v ./:/out goload-pocket-builder
```

This builds the backend and frontend, creates a `pocket-release.zip` file in the current directory. Extract it to get the compiled binaries for Linux and Windows.

**Option 2: Download pre-built binaries**

Download the latest pre-built `pocket-release.zip` from the [Release](https://github.com/yuisofull/goload/releases) section. Extract it and run the binary directly.

Common environment variables:

| Variable | Default | Purpose |
|----------|---------|---------|
| `HTTP_ADDRESS` | `0.0.0.0:8080` | Pocket HTTP listen address |
| `POCKET_DB_PATH` | `./goload.db` | SQLite database path |
| `POCKET_DATA_DIR` | `./data` | Local storage root for downloaded files |
| `POCKET_WEB_DIR` | `./public/dist` | Static frontend directory |

Pocket frontend builds set `VITE_GOLOAD_POCKET=true`, which enables the local **Show in folder** action. Non-pocket frontend builds keep **Download** as the primary completed-task action.

---

## Configuration

All services share a single `configs/config.yaml`. See each service's documentation for the relevant config keys.

