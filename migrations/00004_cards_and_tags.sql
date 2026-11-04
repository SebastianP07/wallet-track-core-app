-- +goose Up
CREATE TABLE credit_cards (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name          TEXT NOT NULL,
  issuer        TEXT NOT NULL,
  statement_day SMALLINT NOT NULL CHECK (statement_day BETWEEN 1 AND 31),
  due_day       SMALLINT NOT NULL CHECK (due_day BETWEEN 1 AND 31),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON credit_cards
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE tags (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT NOT NULL UNIQUE,
  color      TEXT NOT NULL CHECK (color ~ '^#[0-9a-fA-F]{6}$'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON tags
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE tags;
DROP TABLE credit_cards;
