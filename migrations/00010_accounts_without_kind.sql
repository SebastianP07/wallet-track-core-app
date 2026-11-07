-- +goose Up
-- D9 (2026-11-07): savings is merged into Accounts for now. The app no longer reads or writes an account's
-- kind, and every balance counts toward Total actual. The column and its stored values stay, so savings
-- can come back without sorting the accounts again; accounts created from now on get the default.
ALTER TABLE accounts ALTER COLUMN kind SET DEFAULT 'bank';

-- +goose Down
ALTER TABLE accounts ALTER COLUMN kind DROP DEFAULT;
