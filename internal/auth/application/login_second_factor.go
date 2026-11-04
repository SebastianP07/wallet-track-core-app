package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type LoginWithTOTP struct {
	deps Deps
}

func NewLoginWithTOTP(deps Deps) *LoginWithTOTP {
	return &LoginWithTOTP{deps: deps}
}

func (uc *LoginWithTOTP) Execute(ctx context.Context, principal Principal, code string, client domain.ClientInfo) (IssuedSession, error) {
	user := principal.User
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return IssuedSession{}, err
	}
	if !user.MFAEnabled {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventTOTPFailed, client)
	}
	step, ok, err := uc.deps.matchTOTP(user, code)
	if err != nil {
		return IssuedSession{}, err
	}
	if !ok {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventTOTPFailed, client)
	}

	var issued IssuedSession
	replayed := false
	err = uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		fresh, err := repo.ConsumeTOTPStep(ctx, user.ID, step)
		if err != nil {
			return err
		}
		if !fresh {
			replayed = true
			return nil
		}
		issued, err = uc.deps.promote(ctx, repo, principal, domain.EventLoginSucceededTOTP, client)
		return err
	})
	if err != nil {
		return IssuedSession{}, wrapUnlessGeneric("complete TOTP login", err)
	}
	if replayed {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventTOTPReplayed, client)
	}
	return issued, nil
}

type LoginWithRecoveryCode struct {
	deps Deps
}

func NewLoginWithRecoveryCode(deps Deps) *LoginWithRecoveryCode {
	return &LoginWithRecoveryCode{deps: deps}
}

func (uc *LoginWithRecoveryCode) Execute(ctx context.Context, principal Principal, code string, client domain.ClientInfo) (IssuedSession, error) {
	user := principal.User
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return IssuedSession{}, err
	}
	codeHash := uc.deps.Hasher.HashRecoveryCode(domain.NormalizeRecoveryCode(code))

	var issued IssuedSession
	rejected := false
	err := uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		consumed, err := repo.ConsumeRecoveryCode(ctx, user.ID, codeHash, uc.deps.Clock.Now())
		if err != nil {
			return err
		}
		if !consumed {
			rejected = true
			return nil
		}
		issued, err = uc.deps.promote(ctx, repo, principal, domain.EventLoginSucceededRecovery, client)
		return err
	})
	if err != nil {
		return IssuedSession{}, wrapUnlessGeneric("complete recovery login", err)
	}
	if rejected {
		return IssuedSession{}, uc.deps.deny(ctx, user.ID, domain.EventRecoveryCodeFailed, client)
	}
	return issued, nil
}

func wrapUnlessGeneric(action string, err error) error {
	if errors.Is(err, domain.ErrInvalidCredentials) {
		return err
	}
	return fmt.Errorf("%s: %w", action, err)
}
