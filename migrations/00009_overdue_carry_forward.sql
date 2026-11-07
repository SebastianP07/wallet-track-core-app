-- +goose Up
-- D7 (2026-11-07): an unpaid fixed or occasional item of a closed month is owed in a later month and listed
-- in that month's Overdue section, where it counts toward Por pagar and Se va. The item keeps its own month
-- (period_id, its month of origin); owed_year and owed_month say which month owes it now. Income and card
-- charges are never owed elsewhere.
ALTER TABLE period_items
  ADD COLUMN owed_year  INTEGER,
  ADD COLUMN owed_month INTEGER CHECK (owed_month BETWEEN 1 AND 12);

-- Every expense already overdue moves to the first month after its own that is not closed: the month a close
-- would carry it to today. A month not opened yet counts as not closed, and lists the item once it opens.
-- The last month that exists plus one is never closed, so every item finds one.
UPDATE period_items AS i
SET owed_year = EXTRACT(YEAR FROM target.owed)::INTEGER,
    owed_month = EXTRACT(MONTH FROM target.owed)::INTEGER
FROM (
  SELECT item.id, (
    SELECT min(candidate.month)
    FROM generate_series(
      make_date(origin.year, origin.month, 1) + INTERVAL '1 month',
      (SELECT max(make_date(year, month, 1)) FROM periods) + INTERVAL '1 month',
      INTERVAL '1 month'
    ) AS candidate (month)
    WHERE NOT EXISTS (
      SELECT 1 FROM periods AS closed
      WHERE closed.status = 'closed'
        AND closed.year = EXTRACT(YEAR FROM candidate.month)
        AND closed.month = EXTRACT(MONTH FROM candidate.month)
    )
  ) AS owed
  FROM period_items AS item
  JOIN periods AS origin ON origin.id = item.period_id
  WHERE item.status = 'overdue' AND item.kind IN ('fixed', 'occasional')
) AS target
WHERE i.id = target.id;

ALTER TABLE period_items
  ADD CONSTRAINT period_items_owed_month_pair CHECK ((owed_year IS NULL) = (owed_month IS NULL)),
  ADD CONSTRAINT period_items_owed_only_on_expense
    CHECK (owed_year IS NULL OR kind IN ('fixed', 'occasional')),
  ADD CONSTRAINT period_items_overdue_expense_is_owed
    CHECK (status <> 'overdue' OR kind NOT IN ('fixed', 'occasional') OR owed_year IS NOT NULL),
  ADD CONSTRAINT period_items_pending_is_not_owed CHECK (status <> 'pending' OR owed_year IS NULL);

CREATE INDEX period_items_owed_idx ON period_items (owed_year, owed_month) WHERE owed_year IS NOT NULL;

-- +goose Down
-- Statuses stay as they are, which is the earlier model: an overdue item listed from its own closed month.
DROP INDEX period_items_owed_idx;
ALTER TABLE period_items
  DROP CONSTRAINT period_items_pending_is_not_owed,
  DROP CONSTRAINT period_items_overdue_expense_is_owed,
  DROP CONSTRAINT period_items_owed_only_on_expense,
  DROP CONSTRAINT period_items_owed_month_pair,
  DROP COLUMN owed_month,
  DROP COLUMN owed_year;
