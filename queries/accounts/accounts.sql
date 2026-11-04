-- name: ListAccounts :many
SELECT id, name, kind, balance, created_at, updated_at
FROM accounts
ORDER BY name;
