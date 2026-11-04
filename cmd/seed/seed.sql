-- Invented fixture for local development: a closed month with one overdue item and an
-- open month with every kind of item. It creates no users; `create-owner` does that.
-- Rows use fixed ids and ON CONFLICT DO NOTHING, so re-running changes nothing and
-- never overwrites edits made through the app.

INSERT INTO accounts (id, name, kind, balance) VALUES
  ('a0000000-0000-4000-8000-000000000001', 'Main Checking', 'bank', 3250000),
  ('a0000000-0000-4000-8000-000000000002', 'Digital Wallet', 'wallet', 480000),
  ('a0000000-0000-4000-8000-000000000003', 'Pocket Cash', 'cash', 120000),
  ('a0000000-0000-4000-8000-000000000004', 'Meal Card', 'food_card', -15000),
  ('a0000000-0000-4000-8000-000000000005', 'Emergency Fund', 'savings', 5000000)
ON CONFLICT DO NOTHING;

INSERT INTO credit_cards (id, name, issuer, statement_day, due_day) VALUES
  ('c0000000-0000-4000-8000-000000000001', 'Aurora Visa', 'Aurora Bank', 15, 30),
  ('c0000000-0000-4000-8000-000000000002', 'Summit Mastercard', 'Summit Bank', 5, 20)
ON CONFLICT DO NOTHING;

INSERT INTO tags (id, name, color) VALUES
  ('d0000000-0000-4000-8000-000000000001', 'Household', '#3b82f6'),
  ('d0000000-0000-4000-8000-000000000002', 'Auto-debit', '#22c55e')
ON CONFLICT DO NOTHING;

INSERT INTO people (id, name, is_owner, billing_months) VALUES
  ('e0000000-0000-4000-8000-000000000001', 'Alex', true, 1),
  ('e0000000-0000-4000-8000-000000000002', 'Sam', false, 1),
  ('e0000000-0000-4000-8000-000000000003', 'Jamie', false, 6)
ON CONFLICT DO NOTHING;

INSERT INTO platforms (id, name, monthly_cost, card_id) VALUES
  ('f0000000-0000-4000-8000-000000000001', 'StreamFlix', 45000, 'c0000000-0000-4000-8000-000000000001'),
  ('f0000000-0000-4000-8000-000000000002', 'TuneBox', 24000, 'c0000000-0000-4000-8000-000000000002')
ON CONFLICT DO NOTHING;

INSERT INTO platform_members (platform_id, person_id)
SELECT platforms.id, people.id
FROM platforms
CROSS JOIN people
WHERE platforms.id IN ('f0000000-0000-4000-8000-000000000001', 'f0000000-0000-4000-8000-000000000002')
  AND people.id IN (
    'e0000000-0000-4000-8000-000000000001',
    'e0000000-0000-4000-8000-000000000002',
    'e0000000-0000-4000-8000-000000000003'
  )
ON CONFLICT DO NOTHING;

INSERT INTO platform_payments (id, person_id, paid_at, months_covered, amount) VALUES
  ('f1000000-0000-4000-8000-000000000001', 'e0000000-0000-4000-8000-000000000003', '2026-09-10', 6, 138000)
ON CONFLICT DO NOTHING;

INSERT INTO periods (year, month, status) VALUES
  (2026, 10, 'closed'),
  (2026, 11, 'open')
ON CONFLICT DO NOTHING;

INSERT INTO recurring_templates (id, kind, name, amount, due_day, card_id, tag_id) VALUES
  ('b0000000-0000-4000-8000-000000000001', 'fixed', 'Rent', 1800000, 5, NULL,
    (SELECT id FROM tags WHERE name = 'Household')),
  ('b0000000-0000-4000-8000-000000000002', 'fixed', 'Utilities', 240000, 12, NULL,
    (SELECT id FROM tags WHERE name = 'Household')),
  ('b0000000-0000-4000-8000-000000000003', 'fixed', 'Home Internet', 95000, 18, NULL,
    (SELECT id FROM tags WHERE name = 'Auto-debit')),
  ('b0000000-0000-4000-8000-000000000004', 'fixed', 'Phone Plan', 65000, 31, NULL, NULL),
  ('b0000000-0000-4000-8000-000000000005', 'income', 'Salary', 6200000, NULL, NULL, NULL),
  ('b0000000-0000-4000-8000-000000000006', 'card_charge', 'StreamFlix', 45000, NULL,
    'c0000000-0000-4000-8000-000000000001', NULL),
  ('b0000000-0000-4000-8000-000000000007', 'card_charge', 'CloudNotes', 28000, NULL,
    'c0000000-0000-4000-8000-000000000002', NULL)
ON CONFLICT DO NOTHING;

WITH october AS (SELECT id FROM periods WHERE year = 2026 AND month = 10)
INSERT INTO period_items (id, period_id, kind, name, amount, due_date, status, done_at)
SELECT item.id::uuid, october.id, item.kind, item.name, item.amount, item.due_date::date, item.status, item.done_at::timestamptz
FROM october
CROSS JOIN (VALUES
  ('10000000-0000-4000-8000-000000000001', 'fixed', 'Rent', 1800000::bigint, '2026-10-05', 'done', '2026-10-04 09:00:00-05'),
  ('10000000-0000-4000-8000-000000000002', 'occasional', 'Dentist Co-pay', 85000::bigint, '2026-10-22', 'overdue', NULL)
) AS item (id, kind, name, amount, due_date, status, done_at)
ON CONFLICT DO NOTHING;

WITH november AS (SELECT id FROM periods WHERE year = 2026 AND month = 11)
INSERT INTO period_items (id, period_id, template_id, kind, name, amount, due_date, card_id, tag_id, status, done_at)
SELECT item.id::uuid, november.id, template.id, template.kind, template.name, template.amount, item.due_date::date,
  template.card_id, template.tag_id, item.status, item.done_at::timestamptz
FROM november
CROSS JOIN (VALUES
  ('11000000-0000-4000-8000-000000000001', 'b0000000-0000-4000-8000-000000000001', '2026-11-05', 'done', '2026-11-03 09:00:00-05'),
  ('11000000-0000-4000-8000-000000000002', 'b0000000-0000-4000-8000-000000000002', '2026-11-12', 'pending', NULL),
  ('11000000-0000-4000-8000-000000000003', 'b0000000-0000-4000-8000-000000000003', '2026-11-18', 'pending', NULL),
  ('11000000-0000-4000-8000-000000000004', 'b0000000-0000-4000-8000-000000000004', '2026-11-30', 'pending', NULL),
  ('11000000-0000-4000-8000-000000000005', 'b0000000-0000-4000-8000-000000000005', NULL, 'pending', NULL),
  ('11000000-0000-4000-8000-000000000006', 'b0000000-0000-4000-8000-000000000006', NULL, 'pending', NULL),
  ('11000000-0000-4000-8000-000000000007', 'b0000000-0000-4000-8000-000000000007', NULL, 'pending', NULL)
) AS item (id, template_uuid, due_date, status, done_at)
JOIN recurring_templates AS template ON template.id = item.template_uuid::uuid
ON CONFLICT DO NOTHING;

WITH november AS (SELECT id FROM periods WHERE year = 2026 AND month = 11)
INSERT INTO period_items (id, period_id, kind, name, amount, due_date, person_id)
SELECT item.id::uuid, november.id, item.kind, item.name, item.amount, item.due_date::date, item.person_id::uuid
FROM november
CROSS JOIN (VALUES
  ('12000000-0000-4000-8000-000000000001', 'occasional', 'Aurora Visa Statement', 640000::bigint, '2026-11-30', NULL),
  ('12000000-0000-4000-8000-000000000002', 'occasional', 'Car Tax', 310000::bigint, '2026-11-08', NULL),
  ('12000000-0000-4000-8000-000000000003', 'income', 'Platforms — Sam', 23000::bigint, NULL, 'e0000000-0000-4000-8000-000000000002')
) AS item (id, kind, name, amount, due_date, person_id)
ON CONFLICT DO NOTHING;
