-- name: CreateTask :execresult
INSERT INTO tasks (of_account_id, file_name, source_url, source_type, source_auth, headers,
                   storage_type, storage_path, status,
                   checksum_type, checksum_value,
                   concurrency, max_speed, max_retries, timeout, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

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
