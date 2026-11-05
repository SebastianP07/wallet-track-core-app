-- name: ListCards :many
SELECT id, name, issuer, statement_day, due_day, created_at, updated_at
FROM credit_cards
ORDER BY name, id;
