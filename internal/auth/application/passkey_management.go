package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type StartPasskeyRegistration struct {
	deps Deps
}

func NewStartPasskeyRegistration(deps Deps) *StartPasskeyRegistration {
	return &StartPasskeyRegistration{deps: deps}
}

// Execute stores a challenge only in the transaction that spends the proof, so holding a
// registration challenge is what shows the proof was given in this flow.
func (uc *StartPasskeyRegistration) Execute(ctx context.Context, principal Principal, proof SecondFactorProof, client domain.ClientInfo) ([]byte, error) {
	var options []byte
	err := uc.deps.withProof(ctx, principal.User, proof, client, func(ctx context.Context, repo Repository) error {
		passkeys, err := repo.ListPasskeys(ctx, principal.User.ID)
		if err != nil {
			return err
		}
		ceremony, err := uc.deps.Passkeys.BeginRegistration(principal.User, passkeys)
		if err != nil {
			return err
		}
		options = ceremony.Options
		return uc.deps.storeChallenge(ctx, repo, principal.Session.ID, domain.ChallengeRegistration, ceremony)
	})
	if err != nil {
		return nil, wrapUnlessExpected("start passkey registration", err)
	}
	return options, nil
}

type FinishPasskeyRegistration struct {
	deps Deps
}

func NewFinishPasskeyRegistration(deps Deps) *FinishPasskeyRegistration {
	return &FinishPasskeyRegistration{deps: deps}
}

// Execute expects a name already normalized with domain.NormalizePasskeyName. A rejection here is
// audited and does not count toward the lockout: the owner already gave a proof, and spent it.
func (uc *FinishPasskeyRegistration) Execute(ctx context.Context, principal Principal, name string, response []byte, client domain.ClientInfo) (domain.Passkey, error) {
	user := principal.User
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return domain.Passkey{}, err
	}
	state, found, err := uc.deps.Repo.ConsumePasskeyChallenge(ctx, principal.Session.ID, domain.ChallengeRegistration, uc.deps.Clock.Now())
	if err != nil {
		return domain.Passkey{}, fmt.Errorf("consume passkey challenge: %w", err)
	}
	if !found {
		return domain.Passkey{}, uc.reject(ctx, user.ID, client)
	}
	credential, err := uc.deps.Passkeys.FinishRegistration(user, state, response)
	if errors.Is(err, domain.ErrInvalidCredentials) {
		return domain.Passkey{}, uc.reject(ctx, user.ID, client)
	}
	if err != nil {
		return domain.Passkey{}, fmt.Errorf("finish passkey registration: %w", err)
	}

	var passkey domain.Passkey
	err = uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if passkey, err = repo.CreatePasskey(ctx, user.ID, name, credential); err != nil {
			return err
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventPasskeyRegistered, Client: client})
	})
	if err != nil {
		return domain.Passkey{}, wrapUnlessExpected("store passkey", err)
	}
	return passkey, nil
}

func (uc *FinishPasskeyRegistration) reject(ctx context.Context, userID string, client domain.ClientInfo) error {
	err := uc.deps.Repo.RecordEvent(ctx, domain.Event{UserID: userID, Type: domain.EventPasskeyRegistrationFailed, Client: client})
	if err != nil {
		return fmt.Errorf("record failed registration: %w", err)
	}
	return domain.ErrInvalidCredentials
}

type ListPasskeys struct {
	deps Deps
}

func NewListPasskeys(deps Deps) *ListPasskeys {
	return &ListPasskeys{deps: deps}
}

func (uc *ListPasskeys) Execute(ctx context.Context, userID string) ([]domain.Passkey, error) {
	passkeys, err := uc.deps.Repo.ListPasskeys(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	return passkeys, nil
}

type RenamePasskey struct {
	deps Deps
}

func NewRenamePasskey(deps Deps) *RenamePasskey {
	return &RenamePasskey{deps: deps}
}

// Execute expects a name already normalized with domain.NormalizePasskeyName.
func (uc *RenamePasskey) Execute(ctx context.Context, userID, passkeyID, name string) (domain.Passkey, error) {
	passkey, err := uc.deps.Repo.RenamePasskey(ctx, userID, passkeyID, name)
	if err != nil {
		return domain.Passkey{}, wrapUnlessExpected("rename passkey", err)
	}
	return passkey, nil
}

type RemovePasskey struct {
	deps Deps
}

func NewRemovePasskey(deps Deps) *RemovePasskey {
	return &RemovePasskey{deps: deps}
}

// Execute returns domain.ErrNotFound for a passkey the owner does not have, without spending the proof.
func (uc *RemovePasskey) Execute(ctx context.Context, principal Principal, passkeyID string, proof SecondFactorProof, client domain.ClientInfo) error {
	userID := principal.User.ID
	err := uc.deps.withProof(ctx, principal.User, proof, client, func(ctx context.Context, repo Repository) error {
		deleted, err := repo.DeletePasskey(ctx, userID, passkeyID)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: userID, Type: domain.EventPasskeyRemoved, Client: client})
	})
	return wrapUnlessExpected("remove passkey", err)
}

// wrapUnlessExpected keeps the errors the HTTP adapter maps to a response recognizable, and nil nil.
func wrapUnlessExpected(action string, err error) error {
	var locked *domain.LockedError
	switch {
	case err == nil,
		errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrNotFound),
		errors.Is(err, domain.ErrPasskeyExists),
		errors.As(err, &locked):
		return err
	default:
		return fmt.Errorf("%s: %w", action, err)
	}
}
