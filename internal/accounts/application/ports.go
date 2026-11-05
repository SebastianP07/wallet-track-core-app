// Package application holds the accounts use cases. It reaches infrastructure only through the ports below.
package application

import (
	"context"

	"github.com/SebastianP07/wallet-track-core-app/internal/accounts/domain"
)

type Repository interface {
	List(ctx context.Context) ([]domain.Account, error)
	Get(ctx context.Context, id string) (domain.Account, error)
	Create(ctx context.Context, account domain.NewAccount) (domain.Account, error)
	Update(ctx context.Context, account domain.Account) (domain.Account, error)
	Delete(ctx context.Context, id string) error
	// LockWrites holds until the transaction ends, so that no two writes can each pass the total
	// check against the same accounts.
	LockWrites(ctx context.Context) error
}

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}
