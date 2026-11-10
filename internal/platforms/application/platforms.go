package application

import (
	"context"
	"errors"

	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type ListPlatforms struct {
	repo Repository
}

func NewListPlatforms(repo Repository) *ListPlatforms {
	return &ListPlatforms{repo: repo}
}

func (u *ListPlatforms) Execute(ctx context.Context) ([]domain.Platform, error) {
	return u.repo.ListPlatforms(ctx)
}

type GetPlatform struct {
	repo Repository
}

func NewGetPlatform(repo Repository) *GetPlatform {
	return &GetPlatform{repo: repo}
}

func (u *GetPlatform) Execute(ctx context.Context, id string) (domain.Platform, error) {
	return u.repo.GetPlatform(ctx, id)
}

type CreatePlatform struct {
	tx TxManager
}

func NewCreatePlatform(tx TxManager) *CreatePlatform {
	return &CreatePlatform{tx: tx}
}

// Execute also gives every open month the platform's card charge when it has a card (D14).
func (u *CreatePlatform) Execute(ctx context.Context, platform domain.NewPlatform) (domain.Platform, error) {
	var created domain.Platform
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := lockWithOpenMonths(ctx, repo); err != nil {
			return err
		}
		var err error
		created, err = repo.CreatePlatform(ctx, platform)
		if err != nil {
			return err
		}
		return syncCharges(ctx, repo, created)
	})
	return created, err
}

type UpdatePlatform struct {
	tx TxManager
}

func NewUpdatePlatform(tx TxManager) *UpdatePlatform {
	return &UpdatePlatform{tx: tx}
}

// Execute affects members' income in months opened from now on: income items already in a month keep their
// amount, as with templates. The platform's card charge follows it in every open month while pending (D14).
func (u *UpdatePlatform) Execute(ctx context.Context, id string, changes domain.PlatformChanges) (domain.Platform, error) {
	var updated domain.Platform
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := lockWithOpenMonths(ctx, repo); err != nil {
			return err
		}
		platform, err := repo.GetPlatform(ctx, id)
		if err != nil {
			return err
		}
		updated, err = repo.UpdatePlatform(ctx, changes.ApplyTo(platform))
		if err != nil {
			return err
		}
		if err := checkOwed(ctx, repo); err != nil {
			return err
		}
		return syncCharges(ctx, repo, updated)
	})
	return updated, err
}

type DeletePlatform struct {
	tx TxManager
}

func NewDeletePlatform(tx TxManager) *DeletePlatform {
	return &DeletePlatform{tx: tx}
}

// Execute removes the platform's pending card charges of open months first; done ones stay, unlinked, as
// history (D14).
func (u *DeletePlatform) Execute(ctx context.Context, id string) error {
	return u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := lockWithOpenMonths(ctx, repo); err != nil {
			return err
		}
		if err := repo.CardCharges().Remove(ctx, id); err != nil {
			return err
		}
		found, err := repo.DeletePlatform(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return domain.ErrPlatformNotFound
		}
		return nil
	})
}

type AddMember struct {
	tx TxManager
}

func NewAddMember(tx TxManager) *AddMember {
	return &AddMember{tx: tx}
}

// Execute is idempotent, and like any change to a platform it reaches only months opened from now on.
func (u *AddMember) Execute(ctx context.Context, platformID, personID string) (domain.Platform, error) {
	return changeMembers(ctx, u.tx, platformID, personID, func(ctx context.Context, repo Repository) error {
		return repo.AddMember(ctx, platformID, personID)
	})
}

type RemoveMember struct {
	tx TxManager
}

func NewRemoveMember(tx TxManager) *RemoveMember {
	return &RemoveMember{tx: tx}
}

// Execute is idempotent, and like any change to a platform it reaches only months opened from now on.
func (u *RemoveMember) Execute(ctx context.Context, platformID, personID string) (domain.Platform, error) {
	return changeMembers(ctx, u.tx, platformID, personID, func(ctx context.Context, repo Repository) error {
		return repo.RemoveMember(ctx, platformID, personID)
	})
}

func changeMembers(
	ctx context.Context,
	tx TxManager,
	platformID, personID string,
	change func(ctx context.Context, repo Repository) error,
) (domain.Platform, error) {
	var platform domain.Platform
	err := tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		if err := repo.LockPlatforms(ctx); err != nil {
			return err
		}
		if _, err := repo.GetPlatform(ctx, platformID); err != nil {
			return err
		}
		if _, err := repo.GetPerson(ctx, personID); err != nil {
			return err
		}
		if err := change(ctx, repo); err != nil {
			return err
		}
		if err := checkOwed(ctx, repo); err != nil {
			return err
		}
		var err error
		platform, err = repo.GetPlatform(ctx, platformID)
		return err
	})
	return platform, err
}

// lockWithOpenMonths takes the open months' rows before the platforms lock, the order every writer follows. A
// month opened while this waited for the platforms lock read the platforms as they were, so it is locked too,
// and the write brings its charge up to date.
func lockWithOpenMonths(ctx context.Context, repo Repository) error {
	charges := repo.CardCharges()
	if err := charges.LockOpenMonths(ctx); err != nil {
		return err
	}
	if err := repo.LockPlatforms(ctx); err != nil {
		return err
	}
	return charges.LockOpenMonths(ctx)
}

func syncCharges(ctx context.Context, repo Repository, platform domain.Platform) error {
	err := repo.CardCharges().Sync(ctx, platform.ID, platform.Name, platform.MonthlyCost, platform.CardID)
	if errors.Is(err, kernel.ErrMoneyOutOfRange) {
		return domain.CardChargesOutOfRange()
	}
	return err
}

// ChargingPlatforms is what the periods module asks of this one when opening a month (D14). Built on a
// repository from WithinTx, it runs in that month's transaction.
type ChargingPlatforms struct {
	repo Repository
}

func NewChargingPlatforms(repo Repository) *ChargingPlatforms {
	return &ChargingPlatforms{repo: repo}
}

// WithCard is every platform with a card, by name, as the platforms lock holds them.
func (c *ChargingPlatforms) WithCard(ctx context.Context) ([]domain.Platform, error) {
	if err := c.repo.LockPlatforms(ctx); err != nil {
		return nil, err
	}
	platforms, err := c.repo.ListPlatforms(ctx)
	if err != nil {
		return nil, err
	}
	withCard := make([]domain.Platform, 0, len(platforms))
	for _, platform := range platforms {
		if platform.CardID != nil {
			withCard = append(withCard, platform)
		}
	}
	return withCard, nil
}
