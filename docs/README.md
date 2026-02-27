# Goload — Documentation Index

Goload is a distributed file-download manager built with Go. Users submit download tasks through the API Gateway; dedicated workers fetch files from HTTP/HTTPS sources, store them in MinIO object storage, and report progress back through Kafka.

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

### Shared packages

| Package | Docs | Description |
|---------|------|-------------|
| `pkg/message` | [pkg-message.md](./pkg-message.md) | Pub/Sub abstraction + Kafka implementation |
| `pkg/cache` | — | Generic cache interface + Redis/in-memory implementations |
| `pkg/crypto` | — | bcrypt hasher and RSA key helpers |
| `internal/events` | — | Shared event struct definitions |
| `internal/storage` | — | Storage `Backend`/`Reader`/`Writer`/`Presigner` interfaces + MinIO impl |
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
Client → POST /api/v1/download-tasks/create (Bearer <JWT>)
  → API Gateway: verify JWT (Auth Service.VerifySession)
  → API Gateway: forward to Task Service (gRPC CreateTask)
  → Task Service: insert task row, publish TaskCreated to Kafka
  → Download Service: consume TaskCreated → execute download
      → publish TaskStatusUpdated (DOWNLOADING)
      → publish TaskProgressUpdated (periodic)
      → publish TaskStatusUpdated (STORING)
      → upload file to MinIO
      → publish TaskCompleted
  → Task Service: consume TaskCompleted → mark task COMPLETED, save StoragePath
```

### Download a Completed File

```
Client → GET /api/v1/download-tasks/get?id=<id> (Bearer <JWT>)
  → API Gateway: verify JWT, check ownership
  → Task Service: GenerateDownloadURL (returns /download?token=<uuid> or presigned URL)

Client → GET /download?token=<uuid> (Bearer <JWT>)
  → API Gateway: ConsumeToken from Redis (HMAC check + delete)
  → Validate expiry + ownership
  → Stream file from MinIO
```

---

## Infrastructure

| Component | Image | Purpose |
|-----------|-------|---------|
| MySQL 9.3 | `mysql:9.3.0` | Persistent storage for accounts and tasks |
| Kafka 4.0 | `bitnamilegacy/kafka:4.0.0` | Inter-service event bus |
| Redis 8 | `redis:8.0.2` | Caching, token store |
| MinIO | `quay.io/minio/minio` | Object storage for downloaded files |

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

---

## Configuration

All services share a single `configs/config.yaml`. See each service's documentation for the relevant config keys.

