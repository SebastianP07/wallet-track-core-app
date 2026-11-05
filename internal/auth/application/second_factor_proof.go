package application

import (
	"context"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type ProofMethod string

const (
	ProofTOTP         ProofMethod = "totp"
	ProofRecoveryCode ProofMethod = "recovery_code"
)

// SecondFactorProof is what a full session must add before changing the owner's passkeys, so that a
// stolen session alone cannot leave a persistent credential behind or take one away.
type SecondFactorProof struct {
	Method ProofMethod
	Code   string
}

// withProof runs action in the transaction that consumes the proof: a failing action leaves the code
// unspent, and an action never runs on a spent code. A rejected proof counts toward the lockout.
func (d Deps) withProof(ctx context.Context, user domain.User, proof SecondFactorProof, client domain.ClientInfo, action func(ctx context.Context, repo Repository) error) error {
	if err := d.rejectIfLocked(ctx, user, client); err != nil {
		return err
	}
	consume, failure, err := d.proofConsumer(user, proof)
	if err != nil {
		return err
	}
	if consume == nil {
		return d.deny(ctx, user.ID, failure, client)
	}

	rejected := false
	err = d.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		fresh, err := consume(ctx, repo)
		if err != nil {
			return err
		}
		if !fresh {
			rejected = true
			return nil
		}
		if err := repo.ResetFailedAttempts(ctx, user.ID); err != nil {
			return err
		}
		return action(ctx, repo)
	})
	if err != nil {
		return err
	}
	if rejected {
		if proof.Method == ProofTOTP {
			failure = domain.EventTOTPReplayed
		}
		return d.deny(ctx, user.ID, failure, client)
	}
	return nil
}

type proofConsumer func(ctx context.Context, repo Repository) (bool, error)

// proofConsumer returns a nil consumer when the proof is already known to be wrong, with the event
// that a rejection of this method records.
func (d Deps) proofConsumer(user domain.User, proof SecondFactorProof) (proofConsumer, domain.EventType, error) {
	switch proof.Method {
	case ProofTOTP:
		step, ok, err := d.matchTOTP(user, proof.Code)
		if err != nil {
			return nil, "", err
		}
		if !ok {
			return nil, domain.EventTOTPFailed, nil
		}
		return func(ctx context.Context, repo Repository) (bool, error) {
			return repo.ConsumeTOTPStep(ctx, user.ID, step)
		}, domain.EventTOTPFailed, nil
	case ProofRecoveryCode:
		codeHash := d.Hasher.HashRecoveryCode(domain.NormalizeRecoveryCode(proof.Code))
		return func(ctx context.Context, repo Repository) (bool, error) {
			return repo.ConsumeRecoveryCode(ctx, user.ID, codeHash, d.Clock.Now())
		}, domain.EventRecoveryCodeFailed, nil
	default:
		return nil, "", fmt.Errorf("unknown proof method %q", proof.Method)
	}
}
