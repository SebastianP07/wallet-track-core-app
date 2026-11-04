package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

type StartMFAEnrollment struct {
	deps Deps
}

func NewStartMFAEnrollment(deps Deps) *StartMFAEnrollment {
	return &StartMFAEnrollment{deps: deps}
}

func (uc *StartMFAEnrollment) Execute(ctx context.Context, principal Principal, enrollmentSecret string, client domain.ClientInfo) (TOTPEnrollment, error) {
	user := principal.User
	if user.MFAEnabled {
		return TOTPEnrollment{}, domain.ErrMFAAlreadyEnabled
	}
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return TOTPEnrollment{}, err
	}
	if !user.AcceptsEnrollmentSecret(enrollmentSecret, uc.deps.Clock.Now()) {
		return TOTPEnrollment{}, uc.deps.deny(ctx, user.ID, domain.EventEnrollmentRejected, client)
	}

	enrollment, err := uc.deps.TOTP.Enroll(user.Email)
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("generate TOTP secret: %w", err)
	}
	ciphertext, keyID, err := uc.deps.Cipher.Encrypt([]byte(enrollment.Secret), user.ID)
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	err = uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		stored, err := repo.SetUnconfirmedTOTPSecret(ctx, user.ID, EncryptedSecret{Ciphertext: ciphertext, KeyID: keyID})
		if err != nil {
			return err
		}
		if !stored {
			return domain.ErrMFAAlreadyEnabled
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventEnrollmentStarted, Client: client})
	})
	if errors.Is(err, domain.ErrMFAAlreadyEnabled) {
		return TOTPEnrollment{}, err
	}
	if err != nil {
		return TOTPEnrollment{}, fmt.Errorf("store TOTP secret: %w", err)
	}
	return enrollment, nil
}

type ConfirmMFAEnrollment struct {
	deps Deps
}

func NewConfirmMFAEnrollment(deps Deps) *ConfirmMFAEnrollment {
	return &ConfirmMFAEnrollment{deps: deps}
}

type EnrollmentConfirmation struct {
	User          domain.User
	RecoveryCodes []string
	Session       IssuedSession
}

func (uc *ConfirmMFAEnrollment) Execute(ctx context.Context, principal Principal, code string, client domain.ClientInfo) (EnrollmentConfirmation, error) {
	user := principal.User
	if user.MFAEnabled {
		return EnrollmentConfirmation{}, domain.ErrMFAAlreadyEnabled
	}
	if err := uc.deps.rejectIfLocked(ctx, user, client); err != nil {
		return EnrollmentConfirmation{}, err
	}
	if !user.EnrollmentOpen(uc.deps.Clock.Now()) || len(user.TOTPSecretEnc) == 0 {
		return EnrollmentConfirmation{}, uc.deps.deny(ctx, user.ID, domain.EventEnrollmentRejected, client)
	}
	step, ok, err := uc.deps.matchTOTP(user, code)
	if err != nil {
		return EnrollmentConfirmation{}, err
	}
	if !ok {
		return EnrollmentConfirmation{}, uc.deps.deny(ctx, user.ID, domain.EventTOTPFailed, client)
	}

	result := EnrollmentConfirmation{User: user}
	err = uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		activated, err := repo.ActivateMFA(ctx, user.ID, step, uc.deps.Clock.Now())
		if err != nil {
			return err
		}
		if !activated {
			return domain.ErrInvalidCredentials
		}
		if result.RecoveryCodes, err = uc.deps.issueRecoveryCodes(ctx, repo, user.ID); err != nil {
			return err
		}
		result.Session, err = uc.deps.promote(ctx, repo, principal, domain.EventEnrollmentConfirmed, client)
		return err
	})
	if err != nil {
		return EnrollmentConfirmation{}, wrapUnlessGeneric("confirm enrollment", err)
	}
	result.User.MFAEnabled = true
	return result, nil
}
