#!/usr/bin/env sh
set -e

HOST_MINIO=${MINIO_ENDPOINT_HOST:-minio}
MINIO_PORT=${MINIO_ENDPOINT_PORT:-9000}
BUCKET=${MINIO_BUCKET:-goload}
ACCESS_KEY=${MINIO_ROOT_USER:-minioadmin}
SECRET_KEY=${MINIO_ROOT_PASSWORD:-minioadmin}

# Read-only service account used by the task service to sign presigned GET URLs.
# Override these via environment variables in production.
READER_ACCESS_KEY=${MINIO_READER_ACCESS_KEY:-goload-reader}
READER_SECRET_KEY=${MINIO_READER_SECRET_KEY:-goload-reader-secret}

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
/tmp/mc mb --ignore-existing local/"${BUCKET}"

# Create a read-only service account for presigning (GetObject only).
echo "Creating read-only presign service account '${READER_ACCESS_KEY}' on bucket '${BUCKET}'"

POLICY_FILE=/tmp/goload-reader-policy.json
printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}' \
  "${BUCKET}" > "${POLICY_FILE}"

# Remove existing account to allow re-running idempotently, then recreate.
/tmp/mc admin user svcacct rm local "${READER_ACCESS_KEY}" >/dev/null 2>&1 || true
/tmp/mc admin user svcacct add \
  --access-key "${READER_ACCESS_KEY}" \
  --secret-key "${READER_SECRET_KEY}" \
  --policy "${POLICY_FILE}" \
  local "${ACCESS_KEY}"

echo "Bucket created successfully: local/${BUCKET}"
echo "Read-only presign account created: ${READER_ACCESS_KEY}"
