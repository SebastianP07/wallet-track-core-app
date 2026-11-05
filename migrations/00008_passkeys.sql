-- +goose Up
-- Only what verifying an assertion needs: no attestation statement, no AAGUID. A passkey is never
-- the only factor, so losing one, or this table, cannot lock the owner out.
CREATE TABLE passkeys (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name            TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
  credential_id   BYTEA NOT NULL UNIQUE CHECK (octet_length(credential_id) BETWEEN 1 AND 1023),
  public_key      BYTEA NOT NULL,
  transports      TEXT[] NOT NULL DEFAULT '{}',
  sign_count      BIGINT NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
  backup_eligible BOOLEAN NOT NULL,
  backup_state    BOOLEAN NOT NULL,
  last_used_at    TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX passkeys_user_id_idx ON passkeys (user_id);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON passkeys
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One open ceremony of each purpose per session. ceremony_state is the WebAuthn library's own record,
-- opaque here, and it holds the challenge. A row is deleted by the statement that reads it.
CREATE TABLE passkey_challenges (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id     UUID NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
  purpose        TEXT NOT NULL CHECK (purpose IN ('login', 'registration')),
  ceremony_state BYTEA NOT NULL,
  expires_at     TIMESTAMPTZ NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (session_id, purpose)
);
CREATE INDEX passkey_challenges_expires_at_idx ON passkey_challenges (expires_at);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON passkey_challenges
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE passkey_challenges;
DROP TABLE passkeys;
