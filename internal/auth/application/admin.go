package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
)

// EnrollmentGrant is a one-time secret to hand to the owner. Only its hash is stored.
type EnrollmentGrant struct {
	Secret    string
	ExpiresAt time.Time
}

type CreateOwner struct {
	deps Deps
}

func NewCreateOwner(deps Deps) *CreateOwner {
	return &CreateOwner{deps: deps}
}

func (uc *CreateOwner) Execute(ctx context.Context, rawEmail, password string) (EnrollmentGrant, error) {
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return EnrollmentGrant{}, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return EnrollmentGrant{}, err
	}
	passwordHash, err := uc.deps.Hasher.HashPassword(password)
	if err != nil {
		return EnrollmentGrant{}, fmt.Errorf("hash password: %w", err)
	}
	secret, secretHash, expiresAt := uc.deps.newEnrollmentSecret()

	err = uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		existing, err := repo.CountUsers(ctx)
		if err != nil {
			return err
		}
		if existing > 0 {
			return domain.ErrOwnerExists
		}
		user, err := repo.CreateUser(ctx, NewUser{
			Email:                     email,
			PasswordHash:              passwordHash,
			EnrollmentSecretHash:      secretHash,
			EnrollmentSecretExpiresAt: expiresAt,
		})
		if err != nil {
			return err
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventOwnerCreated})
	})
	if errors.Is(err, domain.ErrOwnerExists) {
		return EnrollmentGrant{}, err
	}
	if err != nil {
		return EnrollmentGrant{}, fmt.Errorf("create owner: %w", err)
	}
	return EnrollmentGrant{Secret: secret, ExpiresAt: expiresAt}, nil
}

type ResetPassword struct {
	deps Deps
}

func NewResetPassword(deps Deps) *ResetPassword {
	return &ResetPassword{deps: deps}
}

// Execute sets a new password, clears any lockout and revokes every session. If enrollment
// was never completed it also returns a fresh enrollment secret, which is the only way to
// continue once the one from create-owner has expired.
func (uc *ResetPassword) Execute(ctx context.Context, rawEmail, password string) (*EnrollmentGrant, error) {
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return nil, err
	}
	user, err := uc.deps.Repo.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	passwordHash, err := uc.deps.Hasher.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var grant *EnrollmentGrant
	err = uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := repo.UpdatePassword(ctx, user.ID, passwordHash); err != nil {
			return err
		}
		if err := repo.RevokeUserSessions(ctx, user.ID, uc.deps.Clock.Now()); err != nil {
			return err
		}
		if !user.MFAEnabled {
			secret, secretHash, expiresAt := uc.deps.newEnrollmentSecret()
			if err := repo.SetEnrollmentSecret(ctx, user.ID, secretHash, expiresAt); err != nil {
				return err
			}
			grant = &EnrollmentGrant{Secret: secret, ExpiresAt: expiresAt}
		}
		return repo.RecordEvent(ctx, domain.Event{UserID: user.ID, Type: domain.EventPasswordReset})
	})
	if err != nil {
		return nil, fmt.Errorf("reset password: %w", err)
	}
	return grant, nil
}

type RotateTOTPKey struct {
	deps Deps
}

func NewRotateTOTPKey(deps Deps) *RotateTOTPKey {
	return &RotateTOTPKey{deps: deps}
}

// Execute re-encrypts every secret still under an older key, in one transaction, and returns
// how many rows changed. Running it again changes nothing.
func (uc *RotateTOTPKey) Execute(ctx context.Context) (int, error) {
	rotated := 0
	err := uc.deps.Tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		secrets, err := repo.ListTOTPSecretsForUpdate(ctx)
		if err != nil {
			return err
		}
		for _, row := range secrets {
			if row.Secret.KeyID == uc.deps.Cipher.CurrentKeyID() {
				continue
			}
			plaintext, err := uc.deps.Cipher.Decrypt(row.Secret.Ciphertext, row.Secret.KeyID, row.UserID)
			if err != nil {
				return fmt.Errorf("decrypt secret under key %s: %w", row.Secret.KeyID, err)
			}
			ciphertext, keyID, err := uc.deps.Cipher.Encrypt(plaintext, row.UserID)
			if err != nil {
				return fmt.Errorf("encrypt secret: %w", err)
			}
			if err := repo.ReplaceTOTPSecret(ctx, row.UserID, EncryptedSecret{Ciphertext: ciphertext, KeyID: keyID}); err != nil {
				return err
			}
			if err := repo.RecordEvent(ctx, domain.Event{UserID: row.UserID, Type: domain.EventTOTPKeyRotated}); err != nil {
				return err
			}
			rotated++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("rotate TOTP key: %w", err)
	}
	return rotated, nil
}
