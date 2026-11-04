// Package application holds the authentication use cases. It reaches infrastructure only through the ports below.
package application

import (
	"context"
	"net/netip"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type Clock interface {
	Now() time.Time
}

type Hasher interface {
	HashPassword(password string) (string, error)
	VerifyPassword(encoded, password string) (bool, error)
	HashRecoveryCode(code string) string
}

type TOTPVerifier interface {
	Enroll(accountName string) (TOTPEnrollment, error)
	// Match returns the time step a code belongs to, so the caller can reject replays.
	Match(secret, code string, now time.Time) (step int64, ok bool, err error)
}

type TOTPEnrollment struct {
	Secret     string
	OTPAuthURI string
}

type TokenGenerator interface {
	Token() string
	RecoveryCode() string
}

// SecretCipher encrypts TOTP secrets at rest. A ciphertext is bound to context, so it
// cannot be moved to another row.
type SecretCipher interface {
	Encrypt(plaintext []byte, context string) (ciphertext []byte, keyID string, err error)
	Decrypt(ciphertext []byte, keyID, context string) ([]byte, error)
	CurrentKeyID() string
}

// PasswordAttempts counts password failures per email and client IP, whether or not the email
// has an account. Failures from one address never affect a login from another.
type PasswordAttempts interface {
	LockedFor(email string, ip netip.Addr, now time.Time) (time.Duration, bool)
	RecordFailure(email string, ip netip.Addr, now time.Time)
	Clear(email string, ip netip.Addr)
}

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}

type Repository interface {
	CountUsers(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, user NewUser) (domain.User, error)
	FindUserByEmail(ctx context.Context, email string) (domain.User, error)
	IncrementFailedAttempts(ctx context.Context, userID string) (int, error)
	SetLockedUntil(ctx context.Context, userID string, until time.Time) error
	ResetFailedAttempts(ctx context.Context, userID string) error
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
	SetEnrollmentSecret(ctx context.Context, userID string, secretHash []byte, expiresAt time.Time) error
	// SetUnconfirmedTOTPSecret returns false if MFA was enabled in the meantime.
	SetUnconfirmedTOTPSecret(ctx context.Context, userID string, secret EncryptedSecret) (bool, error)
	// ActivateMFA returns false unless enrollment is still open at now.
	ActivateMFA(ctx context.Context, userID string, step int64, now time.Time) (bool, error)
	// ConsumeTOTPStep returns false if this step or a later one was already used.
	ConsumeTOTPStep(ctx context.Context, userID string, step int64) (bool, error)
	// ListTOTPSecretsForUpdate must lock the rows until the transaction ends.
	ListTOTPSecretsForUpdate(ctx context.Context) ([]UserSecret, error)
	ReplaceTOTPSecret(ctx context.Context, userID string, secret EncryptedSecret) error

	CreateSession(ctx context.Context, session NewSession) error
	FindSessionByTokenHash(ctx context.Context, tokenHash []byte) (domain.User, domain.Session, error)
	// PromoteSession replaces the token of a pending session and marks it full. It returns
	// false if the session is no longer pending.
	PromoteSession(ctx context.Context, promotion SessionPromotion) (bool, error)
	TouchSession(ctx context.Context, sessionID string, now time.Time) error
	RevokeSession(ctx context.Context, sessionID string, now time.Time) error
	RevokeUserSessions(ctx context.Context, userID string, now time.Time) error

	ReplaceRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error
	// ConsumeRecoveryCode returns false if the code does not exist or was already used.
	ConsumeRecoveryCode(ctx context.Context, userID, codeHash string, now time.Time) (bool, error)
	CountUnusedRecoveryCodes(ctx context.Context, userID string) (int64, error)

	RecordEvent(ctx context.Context, event domain.Event) error
}

type NewUser struct {
	Email                     string
	PasswordHash              string
	EnrollmentSecretHash      []byte
	EnrollmentSecretExpiresAt time.Time
}

type NewSession struct {
	UserID    string
	TokenHash []byte
	Client    domain.ClientInfo
	Now       time.Time
	ExpiresAt time.Time
}

type SessionPromotion struct {
	SessionID string
	TokenHash []byte
	Now       time.Time
	ExpiresAt time.Time
}

type EncryptedSecret struct {
	Ciphertext []byte
	KeyID      string
}

type UserSecret struct {
	UserID string
	Secret EncryptedSecret
}

// Deps is what the use cases are built from. Each use case needs a subset of it.
type Deps struct {
	Repo     Repository
	Tx       TxManager
	Clock    Clock
	Hasher   Hasher
	TOTP     TOTPVerifier
	Tokens   TokenGenerator
	Cipher   SecretCipher
	Attempts PasswordAttempts
}
