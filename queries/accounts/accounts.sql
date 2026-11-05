-- name: ListAccounts :many
SELECT id, name, kind, balance, created_at, updated_at
FROM accounts
ORDER BY name, id;

-- name: GetAccount :one
SELECT id, name, kind, balance, created_at, updated_at
FROM accounts
WHERE id = $1;

-- name: CreateAccount :one
INSERT INTO accounts (name, kind, balance)
VALUES ($1, $2, $3)
RETURNING id, name, kind, balance, created_at, updated_at;

-- name: UpdateAccount :one
UPDATE accounts
SET name = $2, kind = $3, balance = $4
WHERE id = $1
RETURNING id, name, kind, balance, created_at, updated_at;

-- name: DeleteAccount :exec
DELETE FROM accounts
WHERE id = $1;

-- name: LockAccountWrites :exec
SELECT pg_advisory_xact_lock(4301);
