# Auth Service

The Auth Service handles account registration, session creation (login), and JWT token verification. It exposes a **gRPC** API and is consumed by the API Gateway.

---

## Responsibilities

- Create user accounts with hashed passwords
- Validate credentials and issue JWT tokens (RS512)
- Verify JWTs and return the associated account ID
- Cache account name uniqueness checks and token public keys in Redis

---

## Architecture

```
cmd/auth/svc/main.go
    └── internal/auth/service.go          ← Business logic
        ├── internal/auth/mysql/           ← MySQL persistence (via sqlc)
        ├── internal/auth/cache/           ← Redis caching layer
        ├── internal/auth/endpoint/        ← go-kit endpoints (rate-limited)
        └── internal/auth/transport/grpc.go ← gRPC server & client
```

### Layer description

| Layer | Package | Role |
|-------|---------|------|
| Domain | `internal/auth` | Service interface, domain structs (`Account`, `AccountPassword`), error codes |
| Persistence | `internal/auth/mysql` | `AccountStore`, `AccountPasswordStore`, `TokenPublicKeyStore`, `TxManager` backed by MySQL via `sqlc` |
| Cache | `internal/auth/cache` | Redis-backed decorators for `AccountStore` (account-name set) and `TokenPublicKeyStore` |
| Endpoint | `internal/auth/endpoint` | `go-kit` endpoint set, per-endpoint rate limiting (100 req/s burst) |
| Transport | `internal/auth/transport` | gRPC server + client; maps protobuf ↔ endpoint types |

---

## gRPC API

Proto file: `api/auth.proto`  
Package: `auth.v1.AuthService`

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `CreateAccount` | `accountName`, `password` | `accountId` | Register a new account |
| `CreateSession` | `accountName`, `password` | `token`, `account` | Authenticate and get JWT |
| `VerifySession` | `token` | `accountId` | Validate a JWT and return its owner |

---

## Key Components

### Service (`internal/auth/service.go`)

```go
type Service interface {
    CreateAccount(ctx, CreateAccountParams) (CreateAccountOutput, error)
    CreateSession(ctx, CreateSessionParams) (CreateSessionOutput, error)
    VerifySession(ctx, VerifySessionParams) (VerifySessionOutput, error)
}
```

**CreateAccount flow**:
1. Check Redis set cache for duplicate account name.
2. Hash the password with bcrypt.
3. In a single MySQL transaction: insert `Account` row → insert `AccountPassword` row.
4. Return the new account ID.

**CreateSession flow**:
1. Fetch account by name from MySQL.
2. Fetch hashed password from MySQL.
3. Verify plaintext password against bcrypt hash.
4. Sign a JWT (RS512) with the account ID as subject.

**VerifySession flow**:
1. Parse & verify the JWT signature against the stored RSA public key (`kid`-based lookup).
2. Check token expiry.
3. Return the embedded `accountId`.

### TokenManager (`internal/auth/token_manager.go`)

Uses **RS512** JWTs. On startup the service:
1. Generates an RSA key pair (key size configurable in `config.yaml`).
2. Serialises the public key to PEM and stores it in the DB, obtaining a `kid`.
3. Signs all tokens with that `kid` in the header.

Verification fetches the public key by `kid` from the DB (cached in Redis).

### Password Hashing (`internal/auth/hash.go`, `pkg/crypto/bcrypt`)

Passwords are hashed with bcrypt at a configurable cost (default `10`).

### Caching

| Cache | Key type | Stored value | Purpose |
|-------|----------|--------------|---------|
| `accountStoreCache` | `auth:account_name:{name}` (Redis set) | account names | Fast duplicate-name check on account creation |
| `tokenPublicKeyStoreCache` | `auth:token_public_key:{kid}` | PEM public key bytes | Avoid DB round-trip on every JWT verification |

---

## Error Codes

| Code | Meaning |
|------|---------|
| `INVALID_PASSWORD` | Wrong password during login |
| `INVALID_TOKEN` | JWT verification failed or token expired |
| `ALREADY_EXISTS` | Account name already taken |
| `NOT_FOUND` | Account not found |
| `INTERNAL` | Unexpected server error |

---

## Configuration

Relevant section in `config.yaml`:

```yaml
auth:
  hash:
    bcrypt:
      hash_cost: 10
  token:
    jwt_rs512:
      rsa_bits: 2048
    expires_in: 24h
    regenerate_token_before_expiry: 1h

authservice:
  grpc:
    address: "authsvc:8081"

mysql:
  host: mysql
  port: 3306
  username: root
  password: example
  database: goload

redis:
  address: "redis:6379"
```

---

## Entry Point

`cmd/auth/svc/main.go`

Startup sequence:
1. Load config.
2. Connect to Redis (fail-fast ping).
3. Connect to MySQL (5-retry loop).
4. Build `authmysql.Store` (wraps all MySQL stores).
5. Wrap stores with Redis cache decorators.
6. Generate RSA key pair → create `JWTTokenManager`.
7. Build bcrypt hasher → create `auth.Service`.
8. Create go-kit endpoint set.
9. Start gRPC server (with `go-kit` interceptor).
10. Wait for `SIGINT`/`SIGTERM`.

---

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/golang-jwt/jwt/v5` | JWT signing and parsing |
| `github.com/go-sql-driver/mysql` | MySQL driver |
| `github.com/redis/go-redis/v9` | Redis client |
| `github.com/go-kit/kit` | Service, endpoint, transport framework |
| `golang.org/x/crypto/bcrypt` | Password hashing (via `pkg/crypto/bcrypt`) |
| `sqlc` | Type-safe MySQL query generation |

