-- +goose Up
-- D13 (2026-11-09): the owner orders the items of each kind in a month by hand, and a month opened later gets its
-- items from templates in the same order. position orders the items of one kind in one month (period_items) and
-- the templates (recurring_templates); ties fall back to the id, so the order is always total.
ALTER TABLE period_items ADD COLUMN position INTEGER;
ALTER TABLE recurring_templates ADD COLUMN position INTEGER;

-- Filling the column is not an edit of the row, so updated_at keeps its value.
ALTER TABLE period_items DISABLE TRIGGER set_updated_at;
ALTER TABLE recurring_templates DISABLE TRIGGER set_updated_at;

-- The order the app showed until now: by due date (none last), then name, then id.
UPDATE period_items AS i
SET position = ordered.position
FROM (
  SELECT id, row_number() OVER (PARTITION BY period_id, kind ORDER BY due_date NULLS LAST, name, id)::INTEGER AS position
  FROM period_items
) AS ordered
WHERE i.id = ordered.id;

-- A template follows its item in the latest month that has one, so the next month opens in the order the owner sees
-- now; a template with no item yet follows by due day (none last), then name, then id.
UPDATE recurring_templates AS t
SET position = ordered.position
FROM (
  SELECT template.id, row_number() OVER (
    ORDER BY latest.position NULLS LAST, template.due_day NULLS LAST, template.name, template.id
  )::INTEGER AS position
  FROM recurring_templates AS template
  LEFT JOIN LATERAL (
    SELECT item.position
    FROM period_items AS item
    JOIN periods AS period ON period.id = item.period_id
    WHERE item.template_id = template.id
    ORDER BY period.year DESC, period.month DESC
    LIMIT 1
  ) AS latest ON true
) AS ordered
WHERE t.id = ordered.id;

ALTER TABLE period_items ENABLE TRIGGER set_updated_at;
ALTER TABLE recurring_templates ENABLE TRIGGER set_updated_at;

ALTER TABLE period_items ALTER COLUMN position SET NOT NULL;
ALTER TABLE recurring_templates ALTER COLUMN position SET NOT NULL;

CREATE INDEX period_items_period_kind_position_idx ON period_items (period_id, kind, position);

-- A new row goes to the end of its list unless the insert names a position, whoever writes it: an item added
-- from its section, a month opened from templates (in template order), a member's income item, the dev seed.
-- Every app write that adds an item holds its month's row, so two inserts never race for the same end.
-- +goose StatementBegin
CREATE FUNCTION append_period_item() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.position IS NULL THEN
    SELECT COALESCE(max(position), 0) + 1 INTO NEW.position
    FROM period_items
    WHERE period_id = NEW.period_id AND kind = NEW.kind;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER append_period_item BEFORE INSERT ON period_items
  FOR EACH ROW EXECUTE FUNCTION append_period_item();

-- +goose StatementBegin
CREATE FUNCTION append_recurring_template() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.position IS NULL THEN
    SELECT COALESCE(max(position), 0) + 1 INTO NEW.position FROM recurring_templates;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER append_recurring_template BEFORE INSERT ON recurring_templates
  FOR EACH ROW EXECUTE FUNCTION append_recurring_template();

-- +goose Down
DROP TRIGGER append_recurring_template ON recurring_templates;
DROP FUNCTION append_recurring_template();
DROP TRIGGER append_period_item ON period_items;
DROP FUNCTION append_period_item();
DROP INDEX period_items_period_kind_position_idx;
ALTER TABLE recurring_templates DROP COLUMN position;
ALTER TABLE period_items DROP COLUMN position;
