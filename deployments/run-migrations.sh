#!/usr/bin/env sh
set -e

MYSQL_HOST=${MYSQL_HOST:-mysql}
MYSQL_PORT=${MYSQL_PORT:-3306}
MYSQL_USER=${MYSQL_USER:-root}
MYSQL_PASSWORD=${MYSQL_PASSWORD:-example}
MYSQL_DB=${MYSQL_DB:-goload}
MIGRATIONS_DIR=${MIGRATIONS_DIR:-/app/migrations}

# wait for mysql
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

echo "Waiting for MySQL at ${MYSQL_HOST}:${MYSQL_PORT}..."
if ! try_connect "$MYSQL_HOST" "$MYSQL_PORT" 60; then
  echo "MySQL not reachable at ${MYSQL_HOST}:${MYSQL_PORT}" >&2
  exit 1
fi

if [ -d "${MIGRATIONS_DIR}/mysql" ]; then
  LOCK_NAME="goload_migrations"
  echo "Attempting to acquire migration lock '${LOCK_NAME}'"
  got_lock=$(mysql -sN -h "${MYSQL_HOST}" -P "${MYSQL_PORT}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" -e "SELECT GET_LOCK('${LOCK_NAME}', 0);" ${MYSQL_DB} 2>/dev/null || echo "0")
  if [ "${got_lock}" = "1" ]; then
    echo "Acquired migration lock; applying migrations"
    for f in $(ls -1 ${MIGRATIONS_DIR}/mysql/*.sql 2>/dev/null | sort); do
      if [ -f "$f" ]; then
        echo "Applying $f"
        mysql -h "${MYSQL_HOST}" -P "${MYSQL_PORT}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" "${MYSQL_DB}" < "$f" || {
          echo "Failed to apply migration $f" >&2
          mysql -h "${MYSQL_HOST}" -P "${MYSQL_PORT}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" -e "SELECT RELEASE_LOCK('${LOCK_NAME}');" ${MYSQL_DB} >/dev/null 2>&1 || true
          exit 1
        }
      fi
    done
    mysql -h "${MYSQL_HOST}" -P "${MYSQL_PORT}" -u "${MYSQL_USER}" -p"${MYSQL_PASSWORD}" -e "SELECT RELEASE_LOCK('${LOCK_NAME}');" ${MYSQL_DB} >/dev/null 2>&1 || true
    echo "Migrations applied"
  else
    echo "Migration lock not acquired; another migrator probably ran migrations"
  fi
else
  echo "No migrations directory found at ${MIGRATIONS_DIR}/mysql"
fi

exit 0
