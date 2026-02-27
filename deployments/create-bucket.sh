#!/usr/bin/env sh
set -e

HOST_MINIO=${MINIO_ENDPOINT_HOST:-minio}
MINIO_PORT=${MINIO_ENDPOINT_PORT:-9000}
BUCKET=${MINIO_BUCKET:-goload}
ACCESS_KEY=${MINIO_ROOT_USER:-minioadmin}
SECRET_KEY=${MINIO_ROOT_PASSWORD:-minioadmin}

try_connect() {
  host=$1
  port=$2
  timeout=${3:-60}
  i=0
  while [ $i -lt $timeout ]; do
    if nc -z "$host" "$port" 2>/dev/null; then
      return 0
    fi
    i=$((i+1))
    sleep 1
  done
  return 1
}

echo "Waiting for MinIO at ${HOST_MINIO}:${MINIO_PORT}..."
if ! try_connect "$HOST_MINIO" "$MINIO_PORT" 60; then
  echo "MinIO not reachable at ${HOST_MINIO}:${MINIO_PORT}" >&2
  exit 1
fi

# download mc if not present
MC=/tmp/mc
if [ ! -x "$MC" ]; then
  echo "Downloading mc client..."
  wget -q -O /tmp/mc "https://dl.min.io/client/mc/release/linux-amd64/mc"
  chmod +x /tmp/mc
fi

echo "Configuring mc and creating bucket if missing"
/tmp/mc alias set local "http://${HOST_MINIO}:${MINIO_PORT}" "$ACCESS_KEY" "$SECRET_KEY" >/dev/null 2>&1 || true
/tmp/mc mb --ignore-existing local/${BUCKET}

echo "Bucket created successfully local/${BUCKET}"
