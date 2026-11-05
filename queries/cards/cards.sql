-- name: ListCards :many
SELECT id, name, issuer, statement_day, due_day, created_at, updated_at
FROM credit_cards
ORDER BY name, id;

-- name: GetCard :one
SELECT id, name, issuer, statement_day, due_day, created_at, updated_at
FROM credit_cards
WHERE id = $1;

-- name: CreateCard :one
INSERT INTO credit_cards (name, issuer, statement_day, due_day)
VALUES ($1, $2, $3, $4)
RETURNING id, name, issuer, statement_day, due_day, created_at, updated_at;

-- name: UpdateCard :one
UPDATE credit_cards
SET name = COALESCE(sqlc.narg(name), name),
  issuer = COALESCE(sqlc.narg(issuer), issuer),
  statement_day = COALESCE(sqlc.narg(statement_day), statement_day),
  due_day = COALESCE(sqlc.narg(due_day), due_day)
WHERE id = @id
RETURNING id, name, issuer, statement_day, due_day, created_at, updated_at;

-- name: DeleteCard :execrows
DELETE FROM credit_cards
WHERE id = $1;

-- name: GetPeriodID :one
SELECT id
FROM periods
WHERE year = $1 AND month = $2;

-- The full join gives every card a row, even with no charges, and gathers the charges whose card was
-- deleted into one row with a null id, which exists only when there are such charges.
-- name: SumCardCharges :many
SELECT c.id AS card_id, c.name AS card_name, COALESCE(SUM(charge.amount), 0)::bigint AS total
FROM credit_cards AS c
FULL JOIN (
  SELECT i.card_id, i.amount
  FROM period_items AS i
  WHERE i.period_id = $1 AND i.kind = 'card_charge'
) AS charge ON charge.card_id = c.id
GROUP BY c.id, c.name
ORDER BY c.name NULLS LAST, c.id;
