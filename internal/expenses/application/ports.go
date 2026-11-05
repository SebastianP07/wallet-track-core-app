// Package application holds the expense item use cases. It reaches infrastructure only through the
// ports below.
package application

import (
	"context"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/expenses/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type Clock interface {
	Now() time.Time
}

type Repository interface {
	GetPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error)
	// LockPeriod and LockPeriodOfItem hold the month's row until the transaction ends. Every item
	// write takes it first, so writes cannot race a close, nor each pass the total check on the same
	// items: this row does what the accounts module needs an advisory lock for.
	LockPeriod(ctx context.Context, month kernel.YearMonth) (domain.Period, error)
	LockPeriodOfItem(ctx context.Context, itemID string) (domain.Period, error)

	ListItems(ctx context.Context, periodID string) ([]domain.Item, error)
	GetItem(ctx context.Context, id string) (domain.Item, error)
	CreateItem(ctx context.Context, periodID string, item domain.NewItem) (domain.Item, error)
	UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error)
	SetItemStatus(ctx context.Context, item domain.Item) (domain.Item, error)
	DeleteItem(ctx context.Context, id string) (found bool, err error)
	// GetMemberItem returns domain.ErrItemNotFound when the person has no item in that month, opened or not.
	GetMemberItem(ctx context.Context, personID string, month kernel.YearMonth) (domain.Item, error)
	// MemberPayments works inside this repository's transaction. Only a repository from WithinTx has it.
	MemberPayments() MemberPayments
}

// MemberPayments is what this module asks of the platforms module when a person-linked income item is
// toggled, so that the item and its payment change together (D4, D8). An out of range amount comes back
// as kernel.ErrMoneyOutOfRange.
type MemberPayments interface {
	RecordForItem(ctx context.Context, personID, itemID string, month kernel.YearMonth) error
	RemoveForItem(ctx context.Context, itemID string) error
}

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}
