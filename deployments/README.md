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