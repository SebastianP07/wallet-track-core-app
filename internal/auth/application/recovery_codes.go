package application

import (
	"context"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type GetRecoveryCodeStatus struct {
	deps Deps
}

func NewGetRecoveryCodeStatus(deps Deps) *GetRecoveryCodeStatus {
	return &GetRecoveryCodeStatus{deps: deps}
}

func (uc *GetRecoveryCodeStatus) Execute(ctx context.Context, userID string) (int64, error) {
	remaining, err := uc.deps.Repo.CountUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count recovery codes: %w", err)
	}
	return remaining, nil
}

type RegenerateRecoveryCodes struct {
	deps Deps
}

func NewRegenerateRecoveryCodes(deps Deps) *RegenerateRecoveryCodes {
	return &RegenerateRecoveryCodes{deps: deps}
}

func (uc *RegenerateRecoveryCodes) Execute(ctx context.Context, principal Principal, code string, client domain.ClientInfo) ([]string, error) {
	user := principal.User
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return nil, err
	}
	step, ok, err := uc.deps.matchTOTP(user, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, uc.deps.deny(ctx, user.ID, domain.EventTOTPFailed, client)
	}

	var codes []string
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
		if err := repo.ResetFailedAttempts(ctx, user.ID); err != nil {
			return err
		}
		if codes, err = uc.deps.issueRecoveryCodes(ctx, repo, user.ID); err != nil {
			return err
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventRecoveryCodesRegenerated, Client: client})
	})
	if err != nil {
		return nil, fmt.Errorf("regenerate recovery codes: %w", err)
	}
	if replayed {
		return nil, uc.deps.deny(ctx, user.ID, domain.EventTOTPReplayed, client)
	}
	return codes, nil
}
