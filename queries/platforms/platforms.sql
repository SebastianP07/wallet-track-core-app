-- name: LockPlatforms :exec
SELECT pg_advisory_xact_lock(4302);

-- name: ListPeople :many
SELECT id, name, is_owner, billing_months, created_at, updated_at
FROM people
ORDER BY name, id;

-- name: GetPerson :one
SELECT id, name, is_owner, billing_months, created_at, updated_at
FROM people
WHERE id = $1;

-- name: CreatePerson :one
INSERT INTO people (name, is_owner, billing_months)
VALUES ($1, $2, $3)
RETURNING id, name, is_owner, billing_months, created_at, updated_at;

-- name: UpdatePerson :one
UPDATE people
SET name = $2, is_owner = $3, billing_months = $4
WHERE id = $1
RETURNING id, name, is_owner, billing_months, created_at, updated_at;

-- name: DeletePerson :execrows
DELETE FROM people
WHERE id = $1;

-- name: ListPlatforms :many
SELECT id, name, monthly_cost, card_id, created_at, updated_at
FROM platforms
ORDER BY name, id;

-- name: GetPlatform :one
SELECT id, name, monthly_cost, card_id, created_at, updated_at
FROM platforms
WHERE id = $1;

-- name: CreatePlatform :one
INSERT INTO platforms (name, monthly_cost, card_id)
VALUES ($1, $2, $3)
RETURNING id, name, monthly_cost, card_id, created_at, updated_at;

-- name: UpdatePlatform :one
UPDATE platforms
SET name = $2, monthly_cost = $3, card_id = $4
WHERE id = $1
RETURNING id, name, monthly_cost, card_id, created_at, updated_at;

-- name: DeletePlatform :execrows
DELETE FROM platforms
WHERE id = $1;

-- name: ListMembers :many
SELECT m.platform_id, m.person_id, p.name
FROM platform_members AS m
JOIN people AS p ON p.id = m.person_id
ORDER BY p.name, p.id;

-- name: AddMember :exec
INSERT INTO platform_members (platform_id, person_id)
VALUES ($1, $2)
ON CONFLICT (platform_id, person_id) DO NOTHING;

-- name: RemoveMember :exec
DELETE FROM platform_members
WHERE platform_id = $1 AND person_id = $2;

-- name: ListPayments :many
SELECT id, person_id, paid_at, months_covered, amount, covered_until, created_at, updated_at, period_item_id
FROM platform_payments
ORDER BY paid_at DESC, created_at DESC, id;

-- name: ListPersonPayments :many
SELECT id, person_id, paid_at, months_covered, amount, covered_until, created_at, updated_at, period_item_id
FROM platform_payments
WHERE person_id = $1
ORDER BY paid_at DESC, created_at DESC, id;

-- name: GetPayment :one
SELECT id, person_id, paid_at, months_covered, amount, covered_until, created_at, updated_at, period_item_id
FROM platform_payments
WHERE id = $1;

-- name: CreatePayment :one
INSERT INTO platform_payments (person_id, paid_at, months_covered, amount, period_item_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, person_id, paid_at, months_covered, amount, covered_until, created_at, updated_at, period_item_id;

-- name: DeletePayment :execrows
DELETE FROM platform_payments
WHERE id = $1;

-- name: DeletePaymentOfItem :execrows
DELETE FROM platform_payments
WHERE period_item_id = $1;
