-- +goose Up
-- The money the accounts held when the month began, so the dashboard can tell the month's expected close
-- (opening + Balance) from Sobrante. Opening a month snapshots Total actual into it; the owner can set or clear
-- it at any time, closed months included, because it is informational and changes no total. Months opened
-- before this column stay NULL until the owner sets them.
ALTER TABLE periods
  ADD COLUMN opening_balance BIGINT
    CONSTRAINT periods_opening_balance_within_bound
    CHECK (opening_balance BETWEEN -9007199254740991 AND 9007199254740991);

-- +goose Down
ALTER TABLE periods DROP COLUMN opening_balance;
