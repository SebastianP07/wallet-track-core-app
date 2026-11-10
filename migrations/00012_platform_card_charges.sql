-- +goose Up
-- D14 (2026-11-10): a platform with a card is the source of one card charge item in every month that is open,
-- so the Card charges section totals each card without the owner entering the subscription twice. platform_id
-- links the item to that platform; such an item never comes from a template, so no charge is counted twice.
-- Deleting the platform keeps the item, unlinked, as history: the app removes only the pending ones of open
-- months before it deletes the platform.
ALTER TABLE period_items
  ADD COLUMN platform_id UUID REFERENCES platforms (id) ON DELETE SET NULL,
  ADD CONSTRAINT period_items_platform_only_on_card_charge
    CHECK (platform_id IS NULL OR (kind = 'card_charge' AND template_id IS NULL));

-- At most one item per platform per month, which also keeps opening a month and every platform write
-- idempotent.
CREATE UNIQUE INDEX period_items_period_platform_key ON period_items (period_id, platform_id)
  WHERE platform_id IS NOT NULL;
CREATE INDEX period_items_platform_id_idx ON period_items (platform_id);

-- Deleting a card takes it off its platforms (platforms.card_id is ON DELETE SET NULL), which for D14 is removing
-- the card: their pending charges leave the open months, as when the owner clears the card on the platform.
-- Done charges and closed months keep theirs, listed under a deleted card like any other charge.
-- +goose StatementBegin
CREATE FUNCTION remove_card_platform_charges() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  DELETE FROM period_items AS item
  USING periods AS period
  WHERE item.period_id = period.id
    AND period.status = 'open'
    AND item.platform_id IS NOT NULL
    AND item.card_id = OLD.id
    AND item.status = 'pending';
  RETURN OLD;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER remove_card_platform_charges BEFORE DELETE ON credit_cards
  FOR EACH ROW EXECUTE FUNCTION remove_card_platform_charges();

-- Every open month gets the charge of each platform that has a card today, pending, at the end of its card
-- charges in platform name order (D13). Rows already there stay exactly as they are.
INSERT INTO period_items (period_id, platform_id, kind, name, amount, card_id)
SELECT period.id, platform.id, 'card_charge', platform.name, platform.monthly_cost, platform.card_id
FROM periods AS period
CROSS JOIN platforms AS platform
WHERE period.status = 'open' AND platform.card_id IS NOT NULL
ORDER BY period.year, period.month, platform.name, platform.id
ON CONFLICT (period_id, platform_id) WHERE platform_id IS NOT NULL DO NOTHING;

-- +goose Down
DROP TRIGGER remove_card_platform_charges ON credit_cards;
DROP FUNCTION remove_card_platform_charges();
-- The pending charges of open months are what Up and the app add; every other linked item stays, as a manual
-- card charge.
DELETE FROM period_items AS item
USING periods AS period
WHERE item.period_id = period.id
  AND period.status = 'open'
  AND item.platform_id IS NOT NULL
  AND item.status = 'pending';
DROP INDEX period_items_platform_id_idx;
DROP INDEX period_items_period_platform_key;
ALTER TABLE period_items
  DROP CONSTRAINT period_items_platform_only_on_card_charge,
  DROP COLUMN platform_id;
