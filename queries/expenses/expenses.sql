-- name: GetPeriod :one
SELECT id, year, month, status
FROM periods
WHERE year = $1 AND month = $2;

-- name: LockPeriod :one
SELECT id, year, month, status
FROM periods
WHERE year = $1 AND month = $2
FOR UPDATE;

-- name: LockPeriodOfItem :one
SELECT p.id, p.year, p.month, p.status
FROM periods AS p
JOIN period_items AS i ON i.period_id = p.id
WHERE i.id = $1
FOR UPDATE OF p;

-- name: ListItems :many
SELECT i.id, p.year, p.month, i.template_id, i.person_id, i.kind, i.name, i.amount, i.due_date,
  i.card_id, i.tag_id, i.status, i.done_at, i.created_at, i.updated_at
FROM period_items AS i
JOIN periods AS p ON p.id = i.period_id
WHERE i.period_id = $1
ORDER BY i.due_date NULLS LAST, i.name, i.id;

-- name: GetItem :one
SELECT i.id, p.year, p.month, i.template_id, i.person_id, i.kind, i.name, i.amount, i.due_date,
  i.card_id, i.tag_id, i.status, i.done_at, i.created_at, i.updated_at
FROM period_items AS i
JOIN periods AS p ON p.id = i.period_id
WHERE i.id = $1;

-- name: CreateItem :one
INSERT INTO period_items (period_id, kind, name, amount, due_date, tag_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: UpdateItem :execrows
UPDATE period_items
SET name = $2, amount = $3, due_date = $4, card_id = $5, tag_id = $6
WHERE id = $1;

-- name: SetItemStatus :execrows
UPDATE period_items
SET status = $2, done_at = $3
WHERE id = $1;

-- name: DeleteItem :execrows
DELETE FROM period_items
WHERE id = $1;
