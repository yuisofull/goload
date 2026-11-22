# Local development with Docker Compose

This folder contains a docker-compose setup for local development of the goload stack.

Services included:
- MySQL
- Kafka (Bitnami)
- Redis
- MinIO
- apigateway (built from repo)
- download service (built from repo)
- auth service (built from repo)

Quick start
-----------

1. Generate code and build binaries locally (optional):

   make generate
   make build

2. Start the stack:

   make compose-up

   This will build images and start containers. The application images mount `configs/config.yaml` and `migrations/` from the repo so you can iterate quickly.

3. Run a quick smoke test:

   make smoke

Notes
-----
- The runtime images include a small `wait-and-init.sh` script that waits for dependent infra (MySQL, Redis, Kafka, MinIO), ensures MinIO bucket exists and applies SQL migrations under `migrations/mysql`.
- The `configs/config.yaml` file is mounted into containers at `/app/configs/config.yaml`. Edit that file locally and restart containers for the services to pick up changes.
- If you need to create the MinIO bucket manually, use the `mc` client or access the MinIO web UI at http://localhost:9000 (default creds: minioadmin/minioadmin).
- If you prefer not to mount the config, the images still include a baked copy at `/app/configs/config.yaml`.

Troubleshooting
---------------
- If MySQL fails to accept connections, check container logs: `docker compose logs mysql`.
- If the apigateway health endpoint is not responding, check `docker compose logs apigateway`.

*** End Patch
## Observability

`docker compose up` also starts Prometheus (loopback `:9090`) and Grafana
(loopback `:3000`, login `goload` / `goload-dev`). Grafana auto-provisions the
Prometheus datasource and the "goload dispatch" dashboard, which answers whether
a slow download waited for dispatch or spent its time transferring.

Each service exposes `/metrics`: task on `:9102`, auth on `:9101`, download and
apigateway on their existing HTTP ports (`:8083`, `:8080`). Prometheus finds
them by DNS (type A on the service name), so scaling a service is scraped
correctly:

```
docker compose up --scale download=2
```

Both download replicas are then scraped as separate targets. The DB-global
gauges (`goload_outbox_*`, `goload_tasks_by_status`) are exported by every task
replica, so their dashboard panels use `max(...)` to avoid counting them once
per replica; the rate panels already use `sum(rate(...))`. For production,
routing telemetry through an OTel Collector per service (a later feature)
replaces direct scraping.
