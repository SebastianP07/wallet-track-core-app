package application

import (
	"context"
	"fmt"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

const recoveryCodeCount = 10

// Principal is the user behind a valid session.
type Principal struct {
	User    domain.User
	Session domain.Session
}

// IssuedSession is a new session token, which exists only here and in the owner's cookie.
type IssuedSession struct {
	Token    string
	Lifetime time.Duration
}

// deny records a failed factor and returns the generic error the caller must surface.
func (d Deps) deny(ctx context.Context, userID string, eventType domain.EventType, client domain.ClientInfo) error {
	err := d.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		return recordFailure(ctx, repo, d.Clock.Now(), domain.Event{UserID: userID, Type: eventType, Client: client})
	})
	if err != nil {
		return fmt.Errorf("record failed attempt: %w", err)
	}
	return domain.ErrInvalidCredentials
}

func recordFailure(ctx context.Context, repo Repository, now time.Time, event domain.Event) error {
	attempts, err := repo.IncrementFailedAttempts(ctx, event.UserID)
	if err != nil {
		return err
	}
	if lockout := domain.LockoutFor(attempts); lockout > 0 {
		if err := repo.SetLockedUntil(ctx, event.UserID, now.Add(lockout)); err != nil {
			return err
		}
	}
	return repo.RecordEvent(ctx, event)
}

// rejectIfLocked audits the attempt and returns a *domain.LockedError while the account is locked.
func (d Deps) rejectIfLocked(ctx context.Context, user domain.User, client domain.ClientInfo) error {
	wait, locked := user.LockedFor(d.Clock.Now())
	if !locked {
		return nil
	}
	if err := d.Repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventLoginLocked, Client: client}); err != nil {
		return fmt.Errorf("record locked attempt: %w", err)
	}
	return &domain.LockedError{RetryAfter: wait}
}

func (d Deps) matchTOTP(user domain.User, code string) (int64, bool, error) {
	secret, err := d.Cipher.Decrypt(user.TOTPSecretEnc, user.TOTPKeyID, user.ID)
	if err != nil {
		return 0, false, fmt.Errorf("decrypt TOTP secret: %w", err)
	}
	step, ok, err := d.TOTP.Match(string(secret), code, d.Clock.Now())
	if err != nil {
		return 0, false, fmt.Errorf("match TOTP code: %w", err)
	}
	return step, ok, nil
}

// promote finishes a login inside the caller's transaction: it clears the failure count and
// replaces the session token, so a token known before the second factor stops working.
func (d Deps) promote(ctx context.Context, repo Repository, principal Principal, eventType domain.EventType, client domain.ClientInfo) (IssuedSession, error) {
	now := d.Clock.Now()
	token := d.Tokens.Token()
	if err := repo.ResetFailedAttempts(ctx, principal.User.ID); err != nil {
		return IssuedSession{}, err
	}
	promoted, err := repo.PromoteSession(ctx, SessionPromotion{
		SessionID: principal.Session.ID,
		TokenHash: domain.HashToken(token),
		Now:       now,
		ExpiresAt: now.Add(domain.SessionAbsoluteTimeout),
	})
	if err != nil {
		return IssuedSession{}, err
	}
	if !promoted {
		return IssuedSession{}, domain.ErrInvalidCredentials
	}
	if err := repo.RecordEvent(ctx, domain.Event{UserID: principal.User.ID, Type: eventType, Client: client}); err != nil {
		return IssuedSession{}, err
	}
	return IssuedSession{Token: token, Lifetime: domain.SessionAbsoluteTimeout}, nil
}

// issueRecoveryCodes replaces every code of the user inside the caller's transaction.
func (d Deps) issueRecoveryCodes(ctx context.Context, repo Repository, userID string) ([]string, error) {
	codes := make([]string, recoveryCodeCount)
	hashes := make([]string, recoveryCodeCount)
	for i := range codes {
		codes[i] = d.Tokens.RecoveryCode()
		hashes[i] = d.Hasher.HashRecoveryCode(domain.NormalizeRecoveryCode(codes[i]))
	}
	if err := repo.ReplaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// newEnrollmentSecret returns a one-time secret and its stored form.
func (d Deps) newEnrollmentSecret() (secret string, hash []byte, expiresAt time.Time) {
	secret = d.Tokens.Token()
	return secret, domain.HashToken(secret), d.Clock.Now().Add(domain.EnrollmentSecretLifetime)
}
