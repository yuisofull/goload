-- name: CreateAccount :execresult
INSERT INTO accounts (account_name)
VALUES (?);

-- name: GetAccountByID :one
SELECT id, account_name
FROM accounts
WHERE id = ?;

-- name: GetAccountByAccountName :one
SELECT id, account_name
FROM accounts
WHERE account_name = ?;

-- name: CreateAccountPassword :exec
INSERT INTO account_passwords (of_account_id, hashed_password)
VALUES (?, ?);

-- name: UpdateAccountPassword :exec
UPDATE account_passwords
SET hashed_password = ?
WHERE of_account_id = ?;