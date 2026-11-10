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

-- name: MarkPendingIncomeOverdue :exec
UPDATE period_items
SET status = 'overdue'
WHERE period_id = $1 AND status = 'pending' AND kind = 'income';

-- name: CarryPendingExpenses :exec
UPDATE period_items
SET status = 'overdue', owed_year = @owed_year, owed_month = @owed_month
WHERE period_id = $1 AND status = 'pending' AND kind IN ('fixed', 'occasional');

-- name: CarryOwedExpenses :exec
UPDATE period_items
SET owed_year = @to_year, owed_month = @to_month
WHERE status = 'overdue' AND owed_year = @from_year AND owed_month = @from_month;

-- name: AddItem :exec
INSERT INTO period_items (period_id, template_id, kind, name, amount, due_date, card_id, tag_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (period_id, template_id) WHERE template_id IS NOT NULL DO NOTHING;

-- name: AddMemberItem :exec
INSERT INTO period_items (period_id, person_id, kind, name, amount)
VALUES ($1, $2, 'income', $3, $4)
ON CONFLICT (period_id, person_id) WHERE kind = 'income' AND person_id IS NOT NULL DO NOTHING;

-- name: ListTemplates :many
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at, position
FROM recurring_templates
WHERE sqlc.narg(active)::boolean IS NULL OR active = sqlc.narg(active)::boolean
ORDER BY name, id;

-- name: LockActiveTemplates :many
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at, position
FROM recurring_templates
WHERE active
ORDER BY id
FOR SHARE;

-- name: GetTemplate :one
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at, position
FROM recurring_templates
WHERE id = $1;

-- name: LockTemplate :one
SELECT id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at, position
FROM recurring_templates
WHERE id = $1
FOR UPDATE;

-- name: CreateTemplate :one
INSERT INTO recurring_templates (kind, name, amount, due_day, card_id, tag_id, active)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at, position;

-- name: UpdateTemplate :one
UPDATE recurring_templates
SET name = $2, amount = $3, due_day = $4, card_id = $5, tag_id = $6, active = $7
WHERE id = $1
RETURNING id, kind, name, amount, due_day, card_id, tag_id, active, created_at, updated_at, position;

-- name: DeleteTemplate :execrows
DELETE FROM recurring_templates
WHERE id = $1;

-- name: LockTemplatesByID :exec
SELECT id
FROM recurring_templates
WHERE id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: PlaceTemplates :exec
-- The listed templates take, in the listed order, the positions they hold between them now (D13).
UPDATE recurring_templates AS t
SET position = slots.position
FROM (
  SELECT listed.template_id, held.position
  FROM unnest(@ids::uuid[]) WITH ORDINALITY AS listed (template_id, ord)
  JOIN (
    SELECT position, row_number() OVER (ORDER BY position, id) AS ord
    FROM recurring_templates
    WHERE id = ANY(@ids::uuid[])
  ) AS held ON held.ord = listed.ord
) AS slots
WHERE t.id = slots.template_id;

-- name: AddPlatformItem :exec
INSERT INTO period_items (period_id, platform_id, kind, name, amount, card_id)
VALUES ($1, $2, 'card_charge', $3, $4, $5)
ON CONFLICT (period_id, platform_id) WHERE platform_id IS NOT NULL DO NOTHING;
