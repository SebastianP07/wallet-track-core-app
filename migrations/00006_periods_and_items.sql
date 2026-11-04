-- +goose Up
CREATE TABLE periods (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  year       INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
  month      INTEGER NOT NULL CHECK (month BETWEEN 1 AND 12),
  status     TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (year, month)
);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON periods
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE recurring_templates (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind       TEXT NOT NULL CHECK (kind IN ('fixed', 'income', 'card_charge')),
  name       TEXT NOT NULL,
  amount     BIGINT NOT NULL CHECK (amount >= 0),
  due_day    SMALLINT CHECK (due_day BETWEEN 1 AND 31),
  card_id    UUID REFERENCES credit_cards (id) ON DELETE SET NULL,
  tag_id     UUID REFERENCES tags (id) ON DELETE SET NULL,
  active     BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT recurring_templates_fixed_has_due_day CHECK (kind <> 'fixed' OR due_day IS NOT NULL),
  CONSTRAINT recurring_templates_active_card_charge_has_card
    CHECK (kind <> 'card_charge' OR NOT active OR card_id IS NOT NULL)
);
CREATE INDEX recurring_templates_card_id_idx ON recurring_templates (card_id);
CREATE INDEX recurring_templates_tag_id_idx ON recurring_templates (tag_id);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON recurring_templates
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Runs before the foreign key's ON DELETE SET NULL, which would otherwise leave an active
-- card_charge template without a card and fail recurring_templates_active_card_charge_has_card.
-- +goose StatementBegin
CREATE FUNCTION deactivate_card_charge_templates() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  UPDATE recurring_templates
  SET active = false
  WHERE card_id = OLD.id AND kind = 'card_charge' AND active;
  RETURN OLD;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER deactivate_card_charge_templates BEFORE DELETE ON credit_cards
  FOR EACH ROW EXECUTE FUNCTION deactivate_card_charge_templates();

CREATE TABLE period_items (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  period_id   UUID NOT NULL REFERENCES periods (id) ON DELETE RESTRICT,
  template_id UUID REFERENCES recurring_templates (id) ON DELETE SET NULL,
  person_id   UUID REFERENCES people (id) ON DELETE SET NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('fixed', 'occasional', 'income', 'card_charge')),
  name        TEXT NOT NULL,
  amount      BIGINT NOT NULL CHECK (amount >= 0),
  due_date    DATE,
  card_id     UUID REFERENCES credit_cards (id) ON DELETE SET NULL,
  tag_id      UUID REFERENCES tags (id) ON DELETE SET NULL,
  status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'overdue')),
  done_at     TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT period_items_expense_has_due_date CHECK (kind NOT IN ('fixed', 'occasional') OR due_date IS NOT NULL),
  CONSTRAINT period_items_person_only_on_income CHECK (person_id IS NULL OR kind = 'income'),
  CONSTRAINT period_items_done_at_matches_status CHECK ((status = 'done') = (done_at IS NOT NULL))
);
CREATE INDEX period_items_period_id_kind_idx ON period_items (period_id, kind);
CREATE INDEX period_items_overdue_idx ON period_items (period_id) WHERE status = 'overdue';
CREATE INDEX period_items_template_id_idx ON period_items (template_id);
CREATE INDEX period_items_person_id_idx ON period_items (person_id);
CREATE INDEX period_items_card_id_idx ON period_items (card_id);
CREATE INDEX period_items_tag_id_idx ON period_items (tag_id);
-- Opening a month (Phase 5) and generating member income (D4, Phase 9) stay idempotent on retry.
CREATE UNIQUE INDEX period_items_period_template_key ON period_items (period_id, template_id)
  WHERE template_id IS NOT NULL;
CREATE UNIQUE INDEX period_items_period_person_income_key ON period_items (period_id, person_id)
  WHERE kind = 'income' AND person_id IS NOT NULL;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON period_items
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER deactivate_card_charge_templates ON credit_cards;
DROP FUNCTION deactivate_card_charge_templates();
DROP TABLE period_items;
DROP TABLE recurring_templates;
DROP TABLE periods;
