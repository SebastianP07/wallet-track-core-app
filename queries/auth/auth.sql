-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: InsertUser :one
INSERT INTO users (email, password_hash, mfa_enrollment_token_hash, mfa_enrollment_token_expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: IncrementFailedAttempts :one
UPDATE users SET failed_login_attempts = failed_login_attempts + 1
WHERE id = $1
RETURNING failed_login_attempts;

-- name: SetLockedUntil :exec
UPDATE users SET locked_until = $2 WHERE id = $1;

-- name: ResetFailedAttempts :exec
UPDATE users SET failed_login_attempts = 0, locked_until = NULL WHERE id = $1;

-- name: UpdatePassword :exec
UPDATE users SET password_hash = $2, failed_login_attempts = 0, locked_until = NULL WHERE id = $1;

-- name: SetEnrollmentSecret :exec
UPDATE users SET mfa_enrollment_token_hash = $2, mfa_enrollment_token_expires_at = $3 WHERE id = $1;

-- name: SetUnconfirmedTOTPSecret :execrows
UPDATE users SET totp_secret_enc = $2, totp_key_id = $3 WHERE id = $1 AND NOT mfa_enabled;

-- name: ActivateMFA :execrows
UPDATE users
SET mfa_enabled = true,
    mfa_enrollment_token_hash = NULL,
    mfa_enrollment_token_expires_at = NULL,
    last_totp_step = sqlc.arg(step)
WHERE id = sqlc.arg(id)
  AND NOT mfa_enabled
  AND totp_secret_enc IS NOT NULL
  AND mfa_enrollment_token_expires_at > sqlc.arg(now);

-- name: ConsumeTOTPStep :execrows
UPDATE users SET last_totp_step = sqlc.arg(step)
WHERE id = sqlc.arg(id) AND (last_totp_step IS NULL OR last_totp_step < sqlc.arg(step));

-- name: ListUsersWithTOTPSecret :many
SELECT id, totp_secret_enc, totp_key_id FROM users WHERE totp_secret_enc IS NOT NULL FOR UPDATE;

-- name: ReplaceTOTPSecret :exec
UPDATE users SET totp_secret_enc = $2, totp_key_id = $3 WHERE id = $1;

-- name: InsertSession :one
INSERT INTO sessions (user_id, token_hash, user_agent, ip, last_seen_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetSessionAndUserByTokenHash :one
SELECT sqlc.embed(sessions), sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = $1;

-- name: PromoteSession :execrows
UPDATE sessions
SET token_hash = sqlc.arg(token_hash),
    mfa_verified_at = sqlc.arg(now),
    last_seen_at = sqlc.arg(now),
    expires_at = sqlc.arg(expires_at)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL AND mfa_verified_at IS NULL;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = $2 WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE user_id = $1;

-- name: InsertRecoveryCode :exec
INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2);

-- name: ConsumeRecoveryCode :execrows
UPDATE recovery_codes SET used_at = $3 WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL;

-- name: InsertAuthEvent :exec
INSERT INTO auth_events (user_id, event_type, user_agent, ip) VALUES ($1, $2, $3, $4);

-- name: ListActiveSessions :many
SELECT id, user_agent, ip, mfa_verified_at, last_seen_at, created_at
FROM sessions
WHERE user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(now)
  AND (mfa_verified_at IS NULL OR last_seen_at > sqlc.arg(idle_cutoff))
ORDER BY last_seen_at DESC, id;

-- name: RevokeOwnedSession :execrows
UPDATE sessions SET revoked_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(now);

-- name: CountPasskeys :one
SELECT count(*) FROM passkeys WHERE user_id = $1;

-- name: ListPasskeys :many
SELECT * FROM passkeys WHERE user_id = $1 ORDER BY created_at, id;

-- name: InsertPasskey :one
INSERT INTO passkeys (user_id, name, credential_id, public_key, transports, sign_count, backup_eligible, backup_state)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (credential_id) DO NOTHING
RETURNING *;

-- name: RenamePasskey :one
UPDATE passkeys SET name = $3 WHERE id = $1 AND user_id = $2 RETURNING *;

-- name: DeletePasskey :execrows
DELETE FROM passkeys WHERE id = $1 AND user_id = $2;

-- A counter must grow, unless the authenticator keeps none and it stays at zero. Checking here too
-- means two assertions racing with one count cannot both pass.
-- name: RecordPasskeyUse :execrows
UPDATE passkeys
SET sign_count = sqlc.arg(new_count), backup_state = sqlc.arg(backup_state), last_used_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
  AND (sign_count < sqlc.arg(new_count) OR (sign_count = 0 AND sqlc.arg(new_count) = 0));

-- name: PutPasskeyChallenge :exec
INSERT INTO passkey_challenges (session_id, purpose, ceremony_state, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (session_id, purpose)
DO UPDATE SET ceremony_state = EXCLUDED.ceremony_state, expires_at = EXCLUDED.expires_at;

-- name: ConsumePasskeyChallenge :one
DELETE FROM passkey_challenges
WHERE session_id = $1 AND purpose = $2
RETURNING ceremony_state, expires_at;

-- name: DeleteExpiredPasskeyChallenges :exec
DELETE FROM passkey_challenges WHERE expires_at <= $1;
