-- The kind column is kept but never read or written while savings is merged into Accounts (D9).

-- name: ListAccounts :many
SELECT id, name, balance, created_at, updated_at
FROM accounts
ORDER BY name, id;

-- name: GetAccount :one
SELECT id, name, balance, created_at, updated_at
FROM accounts
WHERE id = $1;

-- name: CreateAccount :one
INSERT INTO accounts (name, balance)
VALUES ($1, $2)
RETURNING id, name, balance, created_at, updated_at;

-- name: UpdateAccount :one
UPDATE accounts
SET name = $2, balance = $3
WHERE id = $1
RETURNING id, name, balance, created_at, updated_at;

-- name: DeleteAccount :exec
DELETE FROM accounts
WHERE id = $1;

-- name: LockAccountWrites :exec
SELECT pg_advisory_xact_lock(4301);
