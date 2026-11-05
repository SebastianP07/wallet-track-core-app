-- +goose Up
-- The income item a payment settled (D4, D8), so that undoing the item removes exactly this payment and
-- deleting this payment returns exactly that item. Deleting the item keeps the payment, as history.
ALTER TABLE platform_payments
  ADD COLUMN period_item_id UUID UNIQUE REFERENCES period_items (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE platform_payments DROP COLUMN period_item_id;
