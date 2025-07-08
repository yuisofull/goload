-- name: CreateDownloadTask :execresult
INSERT INTO download_tasks (of_account_id, download_type, url, download_status, metadata)
VALUES (?, ?, ?, ?, ?);

-- name: GetDownloadTaskListOfUser :many
SELECT id, of_account_id, download_type, url, download_status
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
DELETE FROM download_tasks
WHERE id = ?;
