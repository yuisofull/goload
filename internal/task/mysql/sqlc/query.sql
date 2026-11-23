-- name: CreateTask :execresult
INSERT INTO tasks (of_account_id, file_name, source_url, source_type, source_auth, headers,
                   storage_type, storage_path, status,
                   checksum_type, checksum_value,
                   concurrency, max_speed, max_retries, timeout, metadata, expiration_days)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTaskById :one
SELECT *
FROM tasks
WHERE id = ?;

-- name: UpdateTaskStatus :exec
UPDATE tasks
SET status = ?
WHERE id = ?;

-- name: UpdateTaskCompletedAt :exec
UPDATE tasks
SET completed_at = ?
WHERE id = ?;

-- name: UpdateFileName :exec
UPDATE tasks
SET file_name = ?
WHERE id = ?;

-- name: UpdateTaskError :exec
UPDATE tasks
SET error_message = ?
WHERE id = ?;

-- name: UpdateTaskDownloadedBytes :exec
UPDATE tasks
SET downloaded_bytes = ?
WHERE id = ?;

-- name: UpdateTaskTotalBytes :exec
UPDATE tasks
SET total_bytes = ?
WHERE id = ?;

-- name: UpdateTaskMetadata :exec
UPDATE tasks
SET metadata = ?
WHERE id = ?;

-- name: UpdateTaskProgress :exec
UPDATE tasks
SET progress = ?
WHERE id = ?;

-- name: UpdateStorageInfo :exec
UPDATE tasks
SET storage_type = ?, storage_path = ?
WHERE id = ?;

-- name: UpdateFileChecksum :exec
UPDATE tasks
SET checksum_type = ?, checksum_value = ?
WHERE id = ?;

-- name: ListTasks :many
SELECT *
FROM tasks
WHERE of_account_id = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: GetTaskCountByAccountId :one
SELECT COUNT(*)
FROM tasks
WHERE of_account_id = ?;

-- name: DeleteTask :exec
DELETE
FROM tasks
WHERE id = ?;

-- name: InsertOutbox :execresult
INSERT INTO task_outbox (id, aggregate_id, destination, event_type, payload, created_at, trace_context)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: FetchUnsentOutbox :many
SELECT *
FROM task_outbox
WHERE sent_at IS NULL
ORDER BY created_at ASC, id ASC
LIMIT ?;

-- name: FetchUnsentOutboxAfter :many
SELECT *
FROM task_outbox
WHERE sent_at IS NULL AND (created_at > ? OR (created_at = ? AND id > ?))
ORDER BY created_at ASC, id ASC
LIMIT ?;

-- name: MarkOutboxSent :exec
UPDATE task_outbox
SET sent_at = ?
WHERE id = ? AND sent_at IS NULL;

-- name: LatestOutboxTraceContext :one
SELECT trace_context
FROM task_outbox
WHERE aggregate_id = ? AND event_type = ?
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: OutboxBacklog :one
SELECT COUNT(*) AS unsent, MIN(created_at) AS oldest
FROM task_outbox
WHERE sent_at IS NULL;

-- name: CountTasksByStatus :many
SELECT status, COUNT(*) AS count
FROM tasks
GROUP BY status;

-- name: GetAttemptState :one
SELECT status, owner_id, lease_expires_at, attempt_generation, retry_count, next_attempt_at,
       max_retries, file_name, storage_type, storage_path, checksum_type, checksum_value,
       progress, downloaded_bytes, total_bytes, error_message, completed_at,
       result_content_type, result_file_size, created_at
FROM tasks
WHERE id = ?;

-- name: LockAttemptState :one
SELECT status, owner_id, lease_expires_at, attempt_generation, retry_count, next_attempt_at,
       max_retries, file_name, storage_type, storage_path, checksum_type, checksum_value,
       progress, downloaded_bytes, total_bytes, error_message, completed_at,
       result_content_type, result_file_size, created_at
FROM tasks
WHERE id = ?
FOR UPDATE;

-- name: SaveAttemptState :exec
UPDATE tasks
SET status = ?, owner_id = ?, lease_expires_at = ?, attempt_generation = ?, retry_count = ?, next_attempt_at = ?,
    file_name = ?, storage_type = ?, storage_path = ?, checksum_type = ?, checksum_value = ?,
    progress = ?, downloaded_bytes = ?, total_bytes = ?, error_message = ?, completed_at = ?,
    result_content_type = ?, result_file_size = ?
WHERE id = ?;

-- name: ListReconcilable :many
SELECT id, attempt_generation
FROM tasks
WHERE id > ?
  AND (
        (status = 'PENDING' AND owner_id IS NULL AND (next_attempt_at IS NULL OR next_attempt_at <= ?))
     OR (status IN ('DOWNLOADING', 'STORING') AND (lease_expires_at IS NULL OR lease_expires_at <= ?))
      )
ORDER BY id ASC
LIMIT ?;
