# API Gateway Service

The API Gateway is the single public-facing entry point for the system. It exposes an **HTTP/JSON** API to clients, authenticates every protected request using JWT tokens, and forwards requests to the Auth Service and Task Service via internal gRPC calls.

---

## Responsibilities

- Expose a public HTTP API for account management and download task management
- Authenticate requests: validate JWT Bearer tokens by calling the Auth Service
- Enforce ownership: verify that the authenticated user owns the requested task
- Forward task operations to the Task Service over gRPC
- Serve file downloads using Redis-backed one-time tokens and MinIO object storage

---

## Architecture

```
cmd/apigateway/main.go
    └── internal/apigateway/
        ├── endpoint.go         ← go-kit endpoint wiring (auth + task endpoints)
        ├── middleware.go       ← JWT authentication middleware
        ├── owner_middleware.go ← Task ownership enforcement middleware
        └── transport.go        ← HTTP mux, request decoders, response encoders,
                                   /download handler
```

### Dependencies at runtime

| Component | Protocol | Address |
|-----------|---------|---------|
| Auth Service | gRPC | `authsvc:8081` |
| Task Service | gRPC | `task:8082` |
| Redis | TCP | `redis:6379` |
| MinIO | HTTP | `minio:9000` |

---

## HTTP API

Base path: `/api/v1`

### Auth (public – no token required)

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `POST` | `/api/v1/auth/create` | `{ "account_name", "password" }` | Register a new account |
| `POST` | `/api/v1/auth/session` | `{ "account_name", "password" }` | Login and receive a JWT token |

### Download Tasks (protected – Bearer token required)

| Method | Path | Query / Body | Description |
|--------|------|-------------|-------------|
| `POST` | `/api/v1/download-tasks/create` | body JSON | Create a new download task |
| `GET` | `/api/v1/download-tasks/get` | `?id=<taskId>` | Get a task by ID |
| `GET` | `/api/v1/download-tasks/list` | `?offset=&limit=` | List tasks for the authenticated user |
| `POST` | `/api/v1/download-tasks/delete` | `?id=<taskId>` | Delete a task |
| `POST` | `/api/v1/download-tasks/pause` | `?id=<taskId>` | Pause a task |
| `POST` | `/api/v1/download-tasks/resume` | `?id=<taskId>` | Resume a task |
| `POST` | `/api/v1/download-tasks/cancel` | `?id=<taskId>` | Cancel a task |
| `POST` | `/api/v1/download-tasks/retry` | `?id=<taskId>` | Retry a failed task |
| `GET` | `/api/v1/download-tasks/exists` | `?task_id=<id>` | Check if file is stored |
| `GET` | `/api/v1/download-tasks/progress` | `?task_id=<id>` | Get download progress |

### File Download

| Method | Path | Query | Description |
|--------|------|-------|-------------|
| `GET` | `/download` | `?token=<token>` | Stream file using a one-time download token |

---

## Authentication Flow

Every protected route is wrapped in `addTokenToContext`:

```
HTTP request
  → Extract Bearer token from Authorization header
  → Store token in context
  → Endpoint middleware calls AuthService.VerifySession(token)
  → On success: inject accountID into context
  → On failure: 401 Unauthenticated
```

Implementation: `internal/apigateway/middleware.go` (`NewAuthMiddleware`).

---

## Ownership Enforcement

For task operations (get, delete, pause, resume, cancel, retry), the gateway wraps the endpoint with `RequireTaskOwnerMiddleware`:

```
Authenticated endpoint
  → Retrieve accountID from context
  → Call TaskService.GetTask(taskID)
  → Compare task.OfAccountID == accountID
  → Mismatch → 403 Permission Denied
```

Implementation: `internal/apigateway/owner_middleware.go`.

---

## File Download Handler (`/download`)

The `/download` handler serves files that are too large or sensitive for direct presigned URLs:

```
GET /download?token=<uuid>
  1. Extract token from query string
  2. tokenStore.ConsumeToken(token)           ← Redis HMAC lookup + delete
  3. Validate token not expired
  4. Verify context userID == token.OwnerID   ← ACL check
  5. storage.Get(meta.Key)                    ← Stream from MinIO
  6. Set Content-Disposition + Content-Type headers
  7. io.Copy(responseWriter, reader)
```

Token one-time use is enforced by deleting the Redis key on first consumption.

---

## Endpoint Wiring (`internal/apigateway/endpoint.go`)

```go
type GatewayEndpoints struct {
    CreateTaskEndpoint      endpoint.Endpoint
    GetTaskEndpoint         endpoint.Endpoint
    ListTasksEndpoint       endpoint.Endpoint
    DeleteTaskEndpoint      endpoint.Endpoint
    PauseTaskEndpoint       endpoint.Endpoint
    ResumeTaskEndpoint      endpoint.Endpoint
    CancelTaskEndpoint      endpoint.Endpoint
    RetryTaskEndpoint       endpoint.Endpoint
    CheckFileExistsEndpoint endpoint.Endpoint
    GetTaskProgressEndpoint endpoint.Endpoint
    AuthCreateEndpoint      endpoint.Endpoint  // public
    AuthSessionEndpoint     endpoint.Endpoint  // public
}
```

`NewGatewayEndpoints` builds each endpoint by calling the appropriate downstream gRPC client method and applies middleware layers:
- `authMiddleware` on all task endpoints
- `RequireTaskOwnerMiddleware` on single-task operations

---

## Token Store (Redis)

The gateway holds a `task.TokenStore` (backed by Redis) to support server-side download tokens. Tokens are HMAC-signed with `config.APIGateway.TokenHMACSecret` before being stored, so raw tokens are never stored directly.

---

## Error Mapping

gRPC status codes are translated to HTTP status codes:

| gRPC code | HTTP status |
|-----------|------------|
| `Unauthenticated` | 401 |
| `PermissionDenied` | 403 |
| `NotFound` | 404 |
| `AlreadyExists` | 409 |
| `InvalidArgument` | 400 |
| `ResourceExhausted` | 429 |
| `Internal` / other | 500 |

---

## Configuration

```yaml
apigateway:
  http:
    address: "0.0.0.0:8080"
  token_hmac_secret: "dev-secret-123456"
  storage:
    minio:
      endpoint: "http://minio:9000"
      access_key: "minioadmin"
      secret_key: "minioadmin"
      bucket: "goload"
      use_ssl: false

authservice:
  grpc:
    address: "authsvc:8081"

downloadtaskservice:
  grpc:
    address: "task:8082"

redis:
  address: "redis:6379"
```

---

## Entry Point

`cmd/apigateway/main.go`

Startup sequence:
1. Load config.
2. Connect to Auth Service via gRPC (insecure).
3. Connect to Task Service via gRPC (insecure).
4. Build `AuthMiddleware` using the gRPC auth client as `SessionValidator`.
5. Build `GatewayEndpoints`.
6. Create Redis client → create `tokenStore` (HMAC-backed).
7. (Optional) Initialise MinIO backend for `/download` handler.
8. Build HTTP mux with `NewHTTPHandlerWithDownload`.
9. Start HTTP server on `config.APIGateway.HTTP.Address`.
10. Wait for `SIGINT`/`SIGTERM`.

---

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/go-kit/kit/transport/http` | HTTP transport layer |
| `google.golang.org/grpc` | gRPC client connections |
| `github.com/redis/go-redis/v9` | Redis client for token store |
| `github.com/minio/minio-go/v7` | MinIO file streaming |
| `github.com/oklog/run` | Composable run groups for graceful shutdown |

