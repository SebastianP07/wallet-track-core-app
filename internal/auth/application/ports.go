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

// PasskeyVerifier runs the WebAuthn ceremonies. Options and responses are JSON the browser reads
// and writes, and state is whatever the verifier needs back at the end of a ceremony: none of it
// means anything to the use cases.
type PasskeyVerifier interface {
	BeginLogin(user domain.User, passkeys []domain.Passkey) (PasskeyCeremony, error)
	// FinishLogin returns domain.ErrInvalidCredentials if the assertion is rejected. A sign count
	// that went backwards is not one of those: it is reported in the result, for the audit log.
	FinishLogin(user domain.User, passkeys []domain.Passkey, state, response []byte) (PasskeyAssertion, error)
	BeginRegistration(user domain.User, passkeys []domain.Passkey) (PasskeyCeremony, error)
	// FinishRegistration returns domain.ErrInvalidCredentials if the attestation is rejected.
	FinishRegistration(user domain.User, state, response []byte) (NewPasskeyCredential, error)
}

type PasskeyCeremony struct {
	Options []byte
	State   []byte
}

type PasskeyAssertion struct {
	PasskeyID          string
	SignCount          uint32
	BackupState        bool
	SignCountRegressed bool
}

type NewPasskeyCredential struct {
	CredentialID   []byte
	PublicKey      []byte
	Transports     []string
	SignCount      uint32
	BackupEligible bool
	BackupState    bool
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
	ListActiveSessions(ctx context.Context, userID string, now time.Time) ([]domain.ActiveSession, error)
	// RevokeOwnedSession returns false if the user has no such active session.
	RevokeOwnedSession(ctx context.Context, userID, sessionID string, now time.Time) (bool, error)

	ReplaceRecoveryCodes(ctx context.Context, userID string, codeHashes []string) error
	// ConsumeRecoveryCode returns false if the code does not exist or was already used.
	ConsumeRecoveryCode(ctx context.Context, userID, codeHash string, now time.Time) (bool, error)
	CountUnusedRecoveryCodes(ctx context.Context, userID string) (int64, error)

	CountPasskeys(ctx context.Context, userID string) (int64, error)
	ListPasskeys(ctx context.Context, userID string) ([]domain.Passkey, error)
	// CreatePasskey returns domain.ErrPasskeyExists if that credential is already registered.
	CreatePasskey(ctx context.Context, userID, name string, credential NewPasskeyCredential) (domain.Passkey, error)
	// RenamePasskey returns domain.ErrNotFound if the user has no such passkey.
	RenamePasskey(ctx context.Context, userID, passkeyID, name string) (domain.Passkey, error)
	// DeletePasskey returns false if the user has no such passkey.
	DeletePasskey(ctx context.Context, userID, passkeyID string) (bool, error)
	// RecordPasskeyUse returns false if the passkey is gone or its counter did not grow.
	RecordPasskeyUse(ctx context.Context, userID string, assertion PasskeyAssertion, now time.Time) (bool, error)
	// PutPasskeyChallenge replaces any challenge the session has for that purpose.
	PutPasskeyChallenge(ctx context.Context, challenge PasskeyChallenge) error
	// ConsumePasskeyChallenge deletes the session's challenge in the one statement that reads it, so
	// it can be used once whatever happens next. It returns false if there is none, or it has expired.
	ConsumePasskeyChallenge(ctx context.Context, sessionID string, purpose domain.ChallengePurpose, now time.Time) ([]byte, bool, error)
	DeleteExpiredPasskeyChallenges(ctx context.Context, now time.Time) error

	RecordEvent(ctx context.Context, event domain.Event) error
}

type PasskeyChallenge struct {
	SessionID string
	Purpose   domain.ChallengePurpose
	State     []byte
	ExpiresAt time.Time
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
	Passkeys PasskeyVerifier
}
