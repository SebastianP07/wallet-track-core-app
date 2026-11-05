package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type NextStep string

const (
	NextStepPasskey   NextStep = "passkey"
	NextStepTOTP      NextStep = "totp"
	NextStepEnrollMFA NextStep = "enroll_mfa"
)

type PasswordLoginResult struct {
	NextStep NextStep
	Session  IssuedSession
}

type LoginWithPassword struct {
	deps      Deps
	dummyHash string
}

// NewLoginWithPassword computes the hash verified for unknown emails, so that they cost
// the same time as a real account.
func NewLoginWithPassword(deps Deps) (*LoginWithPassword, error) {
	dummyHash, err := deps.Hasher.HashPassword(deps.Tokens.Token())
	if err != nil {
		return nil, fmt.Errorf("hash dummy password: %w", err)
	}
	return &LoginWithPassword{deps: deps, dummyHash: dummyHash}, nil
}

// Execute expects an email already normalized with domain.NormalizeEmail. Known and unknown
// emails take the same path, and the account's own lockout is deliberately left out of it: that
// one belongs to the second factor, so nobody without the password can lock the owner out.
func (uc *LoginWithPassword) Execute(ctx context.Context, email, password string, client domain.ClientInfo) (PasswordLoginResult, error) {
	user, err := uc.deps.Repo.FindUserByEmail(ctx, email)
	known := err == nil
	if !known && !errors.Is(err, domain.ErrNotFound) {
		return PasswordLoginResult{}, fmt.Errorf("find user: %w", err)
	}
	passwordHash := uc.dummyHash
	if known {
		passwordHash = user.PasswordHash
	}

	now := uc.deps.Clock.Now()
	if wait, locked := uc.deps.Attempts.LockedFor(email, client.IP, now); locked {
		if err := uc.deps.Repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventLoginLocked, Client: client}); err != nil {
			return PasswordLoginResult{}, fmt.Errorf("record locked attempt: %w", err)
		}
		return PasswordLoginResult{}, &domain.LockedError{RetryAfter: wait}
	}
	valid, err := uc.deps.Hasher.VerifyPassword(passwordHash, password)
	if err != nil {
		return PasswordLoginResult{}, fmt.Errorf("verify password: %w", err)
	}
	if !known || !valid {
		uc.deps.Attempts.RecordFailure(email, client.IP, now)
		if err := uc.deps.Repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventLoginFailed, Client: client}); err != nil {
			return PasswordLoginResult{}, fmt.Errorf("record failed attempt: %w", err)
		}
		return PasswordLoginResult{}, domain.ErrInvalidCredentials
	}

	uc.deps.Attempts.Clear(email, client.IP)
	return uc.startPendingSession(ctx, user, client)
}

func (uc *LoginWithPassword) startPendingSession(ctx context.Context, user domain.User, client domain.ClientInfo) (PasswordLoginResult, error) {
	now := uc.deps.Clock.Now()
	token := uc.deps.Tokens.Token()
	err := uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		err := repo.CreateSession(ctx, NewSession{
			UserID:    user.ID,
			TokenHash: domain.HashToken(token),
			Client:    client,
			Now:       now,
			ExpiresAt: now.Add(domain.PendingSessionLifetime),
		})
		if err != nil {
			return err
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventPasswordAccepted, Client: client})
	})
	if err != nil {
		return PasswordLoginResult{}, fmt.Errorf("create session: %w", err)
	}

	next, err := uc.nextStep(ctx, user)
	if err != nil {
		return PasswordLoginResult{}, err
	}
	return PasswordLoginResult{
		NextStep: next,
		Session:  IssuedSession{Token: token, Lifetime: domain.PendingSessionLifetime},
	}, nil
}

func (uc *LoginWithPassword) nextStep(ctx context.Context, user domain.User) (NextStep, error) {
	if !user.MFAEnabled {
		return NextStepEnrollMFA, nil
	}
	passkeys, err := uc.deps.Repo.CountPasskeys(ctx, user.ID)
	if err != nil {
		return "", fmt.Errorf("count passkeys: %w", err)
	}
	if passkeys > 0 {
		return NextStepPasskey, nil
	}
	return NextStepTOTP, nil
}
