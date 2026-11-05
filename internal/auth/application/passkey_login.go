package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type StartPasskeyLogin struct {
	deps Deps
}

func NewStartPasskeyLogin(deps Deps) *StartPasskeyLogin {
	return &StartPasskeyLogin{deps: deps}
}

// Execute returns the options for the browser. It checks no credential, so it costs no attempt.
func (uc *StartPasskeyLogin) Execute(ctx context.Context, principal Principal, client domain.ClientInfo) ([]byte, error) {
	user := principal.User
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return nil, err
	}
	passkeys, err := uc.deps.Repo.ListPasskeys(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	if !user.MFAEnabled || len(passkeys) == 0 {
		return nil, domain.ErrNoPasskeys
	}
	ceremony, err := uc.deps.Passkeys.BeginLogin(user, passkeys)
	if err != nil {
		return nil, fmt.Errorf("begin passkey login: %w", err)
	}
	if err := uc.deps.storeChallenge(ctx, uc.deps.Repo, principal.Session.ID, domain.ChallengeLogin, ceremony); err != nil {
		return nil, fmt.Errorf("store passkey challenge: %w", err)
	}
	return ceremony.Options, nil
}

type LoginWithPasskey struct {
	deps Deps
}

func NewLoginWithPasskey(deps Deps) *LoginWithPasskey {
	return &LoginWithPasskey{deps: deps}
}

func (uc *LoginWithPasskey) Execute(ctx context.Context, principal Principal, response []byte, client domain.ClientInfo) (IssuedSession, error) {
	user := principal.User
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return IssuedSession{}, err
	}
	state, found, err := uc.deps.Repo.ConsumePasskeyChallenge(ctx, principal.Session.ID, domain.ChallengeLogin, uc.deps.Clock.Now())
	if err != nil {
		return IssuedSession{}, fmt.Errorf("consume passkey challenge: %w", err)
	}
	if !found {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventPasskeyFailed, client)
	}
	passkeys, err := uc.deps.Repo.ListPasskeys(ctx, user.ID)
	if err != nil {
		return IssuedSession{}, fmt.Errorf("list passkeys: %w", err)
	}
	assertion, err := uc.deps.Passkeys.FinishLogin(user, passkeys, state, response)
	if errors.Is(err, domain.ErrInvalidCredentials) {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventPasskeyFailed, client)
	}
	if err != nil {
		return IssuedSession{}, fmt.Errorf("finish passkey login: %w", err)
	}
	if assertion.SignCountRegressed {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventPasskeySignCountRegressed, client)
	}

	var issued IssuedSession
	regressed := false
	err = uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		recorded, err := repo.RecordPasskeyUse(ctx, user.ID, assertion, uc.deps.Clock.Now())
		if err != nil {
			return err
		}
		if !recorded {
			regressed = true
			return nil
		}
		issued, err = uc.deps.promote(ctx, repo, principal, domain.EventLoginSucceededPasskey, client)
		return err
	})
	if err != nil {
		return IssuedSession{}, wrapUnlessGeneric("complete passkey login", err)
	}
	if regressed {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventPasskeySignCountRegressed, client)
	}
	return issued, nil
}

func (d Deps) storeChallenge(ctx context.Context, repo Repository, sessionID string, purpose domain.ChallengePurpose, ceremony PasskeyCeremony) error {
	now := d.Clock.Now()
	if err := repo.DeleteExpiredPasskeyChallenges(ctx, now); err != nil {
		return err
	}
	return repo.PutPasskeyChallenge(ctx, PasskeyChallenge{
		SessionID: sessionID,
		Purpose:   purpose,
		State:     ceremony.State,
		ExpiresAt: now.Add(domain.PasskeyChallengeLifetime),
	})
}
