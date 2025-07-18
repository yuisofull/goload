// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: query.sql

package sqlc

import (
	"context"
	"database/sql"
	"encoding/json"
)

const createDownloadTask = `-- name: CreateDownloadTask :execresult
INSERT INTO download_tasks (of_account_id, download_type, url, download_status, metadata)
VALUES (?, ?, ?, ?, ?)
`

type CreateDownloadTaskParams struct {
	OfAccountID    uint64          `json:"of_account_id"`
	DownloadType   int16           `json:"download_type"`
	Url            string          `json:"url"`
	DownloadStatus int16           `json:"download_status"`
	Metadata       json.RawMessage `json:"metadata"`
}

func (q *Queries) CreateDownloadTask(ctx context.Context, arg CreateDownloadTaskParams) (sql.Result, error) {
	return q.db.ExecContext(ctx, createDownloadTask,
		arg.OfAccountID,
		arg.DownloadType,
		arg.Url,
		arg.DownloadStatus,
		arg.Metadata,
	)
}

const deleteDownloadTask = `-- name: DeleteDownloadTask :exec
DELETE
FROM download_tasks
WHERE id = ?
`

func (q *Queries) DeleteDownloadTask(ctx context.Context, id uint64) error {
	_, err := q.db.ExecContext(ctx, deleteDownloadTask, id)
	return err
}

const getDownloadTaskByID = `-- name: GetDownloadTaskByID :one
SELECT id, of_account_id, download_type, url, download_status, metadata
FROM download_tasks
WHERE id = ?
`

func (q *Queries) GetDownloadTaskByID(ctx context.Context, id uint64) (DownloadTask, error) {
	row := q.db.QueryRowContext(ctx, getDownloadTaskByID, id)
	var i DownloadTask
	err := row.Scan(
		&i.ID,
		&i.OfAccountID,
		&i.DownloadType,
		&i.Url,
		&i.DownloadStatus,
		&i.Metadata,
	)
	return i, err
}

const getDownloadTaskByIDWithLock = `-- name: GetDownloadTaskByIDWithLock :one
SELECT id, of_account_id, download_type, url, download_status, metadata
FROM download_tasks
WHERE id = ? FOR
UPDATE
`

func (q *Queries) GetDownloadTaskByIDWithLock(ctx context.Context, id uint64) (DownloadTask, error) {
	row := q.db.QueryRowContext(ctx, getDownloadTaskByIDWithLock, id)
	var i DownloadTask
	err := row.Scan(
		&i.ID,
		&i.OfAccountID,
		&i.DownloadType,
		&i.Url,
		&i.DownloadStatus,
		&i.Metadata,
	)
	return i, err
}

const getDownloadTaskCountOfUser = `-- name: GetDownloadTaskCountOfUser :one
SELECT COUNT(*) AS count
FROM download_tasks
WHERE of_account_id = ?
`

func (q *Queries) GetDownloadTaskCountOfUser(ctx context.Context, ofAccountID uint64) (int64, error) {
	row := q.db.QueryRowContext(ctx, getDownloadTaskCountOfUser, ofAccountID)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const getDownloadTaskListOfUser = `-- name: GetDownloadTaskListOfUser :many
SELECT id, of_account_id, download_type, url, download_status, metadata
FROM download_tasks
WHERE of_account_id = ?
ORDER BY id DESC
LIMIT ? OFFSET ?
`

type GetDownloadTaskListOfUserParams struct {
	OfAccountID uint64 `json:"of_account_id"`
	Limit       int32  `json:"limit"`
	Offset      int32  `json:"offset"`
}

func (q *Queries) GetDownloadTaskListOfUser(ctx context.Context, arg GetDownloadTaskListOfUserParams) ([]DownloadTask, error) {
	rows, err := q.db.QueryContext(ctx, getDownloadTaskListOfUser, arg.OfAccountID, arg.Limit, arg.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []DownloadTask
	for rows.Next() {
		var i DownloadTask
		if err := rows.Scan(
			&i.ID,
			&i.OfAccountID,
			&i.DownloadType,
			&i.Url,
			&i.DownloadStatus,
			&i.Metadata,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateDownloadTask = `-- name: UpdateDownloadTask :exec
UPDATE download_tasks
SET url = ?
WHERE id = ?
`

type UpdateDownloadTaskParams struct {
	Url string `json:"url"`
	ID  uint64 `json:"id"`
}

func (q *Queries) UpdateDownloadTask(ctx context.Context, arg UpdateDownloadTaskParams) error {
	_, err := q.db.ExecContext(ctx, updateDownloadTask, arg.Url, arg.ID)
	return err
}
