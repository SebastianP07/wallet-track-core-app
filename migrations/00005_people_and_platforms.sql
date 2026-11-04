-- +goose Up
CREATE TABLE people (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name           TEXT NOT NULL,
  is_owner       BOOLEAN NOT NULL DEFAULT false,
  billing_months INTEGER NOT NULL DEFAULT 1 CHECK (billing_months BETWEEN 1 AND 120),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX people_single_owner_idx ON people (is_owner) WHERE is_owner;
CREATE TRIGGER set_updated_at BEFORE UPDATE ON people
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE platforms (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name         TEXT NOT NULL,
  monthly_cost BIGINT NOT NULL CHECK (monthly_cost >= 0),
  card_id      UUID REFERENCES credit_cards (id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX platforms_card_id_idx ON platforms (card_id);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON platforms
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE platform_members (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  platform_id UUID NOT NULL REFERENCES platforms (id) ON DELETE CASCADE,
  person_id   UUID NOT NULL REFERENCES people (id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (platform_id, person_id)
);
CREATE INDEX platform_members_person_id_idx ON platform_members (person_id);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON platform_members
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- covered_until is the last day of the last covered month, counting the payment's
-- own month, so a payment in month M for 6 months covers M..M+5 and the person owes
-- again in M+6 (D8). date_trunc needs the explicit timestamp cast to stay immutable.
CREATE TABLE platform_payments (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  person_id      UUID NOT NULL REFERENCES people (id) ON DELETE RESTRICT,
  paid_at        DATE NOT NULL,
  months_covered INTEGER NOT NULL CHECK (months_covered BETWEEN 1 AND 120),
  amount         BIGINT NOT NULL CHECK (amount >= 0),
  covered_until  DATE NOT NULL GENERATED ALWAYS AS (
    (date_trunc('month', paid_at::timestamp)
      + make_interval(months => months_covered)
      - interval '1 day')::date
  ) STORED,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX platform_payments_person_id_paid_at_idx ON platform_payments (person_id, paid_at DESC);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON platform_payments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE platform_payments;
DROP TABLE platform_members;
DROP TABLE platforms;
DROP TABLE people;
