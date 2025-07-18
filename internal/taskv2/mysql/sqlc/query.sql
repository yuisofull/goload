-- name: CreateTask :execresult
INSERT INTO tasks (of_account_id, name, description, source_url, source_type, source_auth,
                   storage_type, storage_path, status, file_info, progress, options,
                   max_retries,
                   tags, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTaskById :one
SELECT *
FROM tasks
WHERE id = ?;

-- name: UpdateTaskFileInfo :exec
UPDATE tasks
SET file_info = ?
WHERE id = ?;

-- name: UpdateTaskProgress :exec
UPDATE tasks
SET progress = ?
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
SET error = ?
WHERE id = ?;

-- name: UpdateTaskRetryCount :exec
UPDATE tasks
SET retry_count = ?
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
