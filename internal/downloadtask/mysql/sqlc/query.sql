-- name: CreateDownloadTask :execresult
INSERT INTO download_tasks (of_account_id, download_type, url, download_status, metadata)
VALUES (?, ?, ?, ?, ?);

-- name: GetDownloadTaskByID :one
SELECT *
FROM download_tasks
WHERE id = ?;

-- name: GetDownloadTaskByIDWithLock :one
SELECT *
FROM download_tasks
WHERE id = ? FOR
UPDATE;

-- name: GetDownloadTaskListOfUser :many
SELECT *
FROM download_tasks
WHERE of_account_id = ?
ORDER BY id DESC
LIMIT ? OFFSET ?;

-- name: GetDownloadTaskCountOfUser :one
SELECT COUNT(*) AS count
FROM download_tasks
WHERE of_account_id = ?;

-- name: UpdateDownloadTask :exec
UPDATE download_tasks
SET url = ?
WHERE id = ?;

-- name: DeleteDownloadTask :exec
DELETE
FROM download_tasks
WHERE id = ?;
