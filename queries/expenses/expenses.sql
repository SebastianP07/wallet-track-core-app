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
  i.card_id, i.tag_id, i.status, i.done_at, i.owed_year, i.owed_month, i.created_at, i.updated_at, i.platform_id
FROM period_items AS i
JOIN periods AS p ON p.id = i.period_id
WHERE i.period_id = $1
-- Section by section (04 fixed, 05 occasional, 06 income, then card charges), each in its manual order (D13).
ORDER BY CASE i.kind WHEN 'fixed' THEN 1 WHEN 'occasional' THEN 2 WHEN 'income' THEN 3 ELSE 4 END, i.position, i.id;

-- name: GetItem :one
SELECT i.id, p.year, p.month, i.template_id, i.person_id, i.kind, i.name, i.amount, i.due_date,
  i.card_id, i.tag_id, i.status, i.done_at, i.owed_year, i.owed_month, i.created_at, i.updated_at, i.platform_id
FROM period_items AS i
JOIN periods AS p ON p.id = i.period_id
WHERE i.id = $1;

-- name: CreateItem :one
INSERT INTO period_items (period_id, kind, name, amount, due_date, card_id, tag_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: SetItemTemplate :execrows
UPDATE period_items
SET template_id = $2
WHERE id = $1;

-- name: UpdateItem :execrows
UPDATE period_items
SET name = $2, amount = $3, due_date = $4, card_id = $5, tag_id = $6
WHERE id = $1;

-- name: SetItemStatus :execrows
UPDATE period_items
SET status = $2, done_at = $3, owed_year = $4, owed_month = $5
WHERE id = $1;

-- name: DeleteItem :execrows
DELETE FROM period_items
WHERE id = $1;

-- name: GetMemberItem :one
SELECT i.id, p.year, p.month, i.template_id, i.person_id, i.kind, i.name, i.amount, i.due_date,
  i.card_id, i.tag_id, i.status, i.done_at, i.owed_year, i.owed_month, i.created_at, i.updated_at, i.platform_id
FROM period_items AS i
JOIN periods AS p ON p.id = i.period_id
WHERE p.year = $1 AND p.month = $2 AND i.person_id = $3 AND i.kind = 'income';

-- name: ListOwedItems :many
SELECT i.id, p.year, p.month, i.template_id, i.person_id, i.kind, i.name, i.amount, i.due_date,
  i.card_id, i.tag_id, i.status, i.done_at, i.owed_year, i.owed_month, i.created_at, i.updated_at, i.platform_id
FROM period_items AS i
JOIN periods AS p ON p.id = i.period_id
WHERE i.owed_year = @owed_year AND i.owed_month = @owed_month
ORDER BY p.year, p.month, i.due_date NULLS LAST, i.name, i.id;

-- name: ListOverdueIncome :many
SELECT i.id, p.year, p.month, i.template_id, i.person_id, i.kind, i.name, i.amount, i.due_date,
  i.card_id, i.tag_id, i.status, i.done_at, i.owed_year, i.owed_month, i.created_at, i.updated_at, i.platform_id
FROM period_items AS i
JOIN periods AS p ON p.id = i.period_id
WHERE i.status = 'overdue' AND p.status = 'closed' AND i.kind = 'income'
ORDER BY p.year, p.month, i.name, i.id;

-- name: ListKindOrder :many
SELECT id, template_id
FROM period_items
WHERE period_id = $1 AND kind = $2
ORDER BY position, id;

-- name: SetItemPositions :exec
UPDATE period_items AS i
SET position = ordered.ord::INTEGER
FROM unnest(@ids::uuid[]) WITH ORDINALITY AS ordered (item_id, ord)
WHERE i.id = ordered.item_id;

-- name: LockOpenPeriods :many
-- In ascending order, as a close and an undo take months, so these writers cannot deadlock.
SELECT id, year, month, status
FROM periods
WHERE status = 'open'
ORDER BY year, month
FOR UPDATE;

-- name: UpdatePlatformCharges :exec
-- A charge already done keeps what it was done with, and a row that would not change keeps its updated_at (D14).
UPDATE period_items AS i
SET name = @name::text, amount = @amount::bigint, card_id = @card_id::uuid
FROM periods AS p
WHERE p.id = i.period_id AND p.status = 'open'
  AND i.platform_id = @platform_id::uuid AND i.status = 'pending'
  AND (i.name, i.amount, i.card_id) IS DISTINCT FROM (@name::text, @amount::bigint, @card_id::uuid);

-- name: AddPlatformCharges :exec
-- An open month that already has the platform's charge, pending or done, keeps it (D14).
INSERT INTO period_items (period_id, platform_id, kind, name, amount, card_id)
SELECT p.id, @platform_id::uuid, 'card_charge', @name::text, @amount::bigint, @card_id::uuid
FROM periods AS p
WHERE p.status = 'open'
ORDER BY p.year, p.month
ON CONFLICT (period_id, platform_id) WHERE platform_id IS NOT NULL DO NOTHING;

-- name: RemovePlatformCharges :exec
-- Only pending charges of open months go; done ones and closed months stay as history (D14).
DELETE FROM period_items AS i
USING periods AS p
WHERE p.id = i.period_id AND p.status = 'open'
  AND i.platform_id = @platform_id::uuid AND i.status = 'pending';

-- name: OpenCardChargesOutOfRange :one
-- Whether some open month's card charges add up beyond the bound the contract can carry.
SELECT EXISTS (
  SELECT 1
  FROM period_items AS i
  JOIN periods AS p ON p.id = i.period_id
  WHERE p.status = 'open' AND i.kind = 'card_charge'
  GROUP BY i.period_id
  HAVING sum(i.amount) > @max_money::bigint
);
