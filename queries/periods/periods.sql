-- name: ListPeriods :many
SELECT id, year, month, status, created_at, updated_at
FROM periods
ORDER BY year DESC, month DESC;

-- name: GetPeriod :one
SELECT id, year, month, status, created_at, updated_at
FROM periods
WHERE year = $1 AND month = $2;

-- name: LockPeriod :one
SELECT id, year, month, status, created_at, updated_at
FROM periods
WHERE year = $1 AND month = $2
FOR UPDATE;

-- name: CreatePeriod :one
INSERT INTO periods (year, month)
VALUES ($1, $2)
ON CONFLICT (year, month) DO NOTHING
RETURNING id, year, month, status, created_at, updated_at;

-- name: MarkPeriodClosed :one
UPDATE periods
SET status = 'closed'
WHERE id = $1
RETURNING id, year, month, status, created_at, updated_at;

-- name: MarkPendingItemsOverdue :exec
UPDATE period_items
SET status = 'overdue'
WHERE period_id = $1 AND status = 'pending' AND kind = ANY(@kinds::text[]);

-- name: AddItem :exec
INSERT INTO period_items (period_id, template_id, kind, name, amount, due_date, card_id, tag_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (period_id, template_id) WHERE template_id IS NOT NULL DO NOTHING;

-- name: ListTemplates :many
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at
FROM recurring_templates
WHERE sqlc.narg(active)::boolean IS NULL OR active = sqlc.narg(active)::boolean
ORDER BY name, id;

-- name: LockActiveTemplates :many
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at
FROM recurring_templates
WHERE active
ORDER BY id
FOR SHARE;

-- name: GetTemplate :one
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at
FROM recurring_templates
WHERE id = $1;

-- name: LockTemplate :one
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at
FROM recurring_templates
WHERE id = $1
FOR UPDATE;

-- name: CreateTemplate :one
INSERT INTO recurring_templates (kind, name, amount, due_day, card_id, tag_id, active)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at;

-- name: UpdateTemplate :one
UPDATE recurring_templates
SET name = $2, amount = $3, due_day = $4, card_id = $5, tag_id = $6, active = $7
WHERE id = $1
RETURNING id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at;

-- name: DeleteTemplate :execrows
DELETE FROM recurring_templates
WHERE id = $1;
