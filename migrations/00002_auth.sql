-- +goose Up
CREATE TABLE users (
  id                              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email                           TEXT NOT NULL UNIQUE CHECK (email = lower(email)),
  password_hash                   TEXT NOT NULL,
  totp_secret_enc                 BYTEA,
  totp_key_id                     TEXT,
  mfa_enabled                     BOOLEAN NOT NULL DEFAULT false,
  mfa_enrollment_token_hash       BYTEA CHECK (octet_length(mfa_enrollment_token_hash) = 32),
  mfa_enrollment_token_expires_at TIMESTAMPTZ,
  failed_login_attempts           INTEGER NOT NULL DEFAULT 0 CHECK (failed_login_attempts >= 0),
  locked_until                    TIMESTAMPTZ,
  last_totp_step                  BIGINT,
  created_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT users_totp_secret_has_key CHECK ((totp_secret_enc IS NULL) = (totp_key_id IS NULL)),
  CONSTRAINT users_mfa_needs_totp_secret CHECK (NOT mfa_enabled OR totp_secret_enc IS NOT NULL),
  CONSTRAINT users_mfa_enrollment_token_has_expiry
    CHECK ((mfa_enrollment_token_hash IS NULL) = (mfa_enrollment_token_expires_at IS NULL))
);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE sessions (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  token_hash      BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
  user_agent      TEXT,
  ip              INET,
  mfa_verified_at TIMESTAMPTZ,
  last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at      TIMESTAMPTZ NOT NULL,
  revoked_at      TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON sessions
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE recovery_codes (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  code_hash  TEXT NOT NULL,
  used_at    TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, code_hash)
);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON recovery_codes
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- user_id is optional because failed logins can name an unknown email, and the
-- audit trail must outlive the user it describes.
CREATE TABLE auth_events (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID REFERENCES users (id) ON DELETE SET NULL,
  event_type TEXT NOT NULL,
  user_agent TEXT,
  ip         INET,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX auth_events_user_id_idx ON auth_events (user_id);
CREATE INDEX auth_events_created_at_idx ON auth_events (created_at);
CREATE TRIGGER set_updated_at BEFORE UPDATE ON auth_events
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE auth_events;
DROP TABLE recovery_codes;
DROP TABLE sessions;
DROP TABLE users;
