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
	// ListOwedItems is the expenses earlier months left unpaid that this month owes, paid since or not,
	// oldest month of origin first (D7).
	ListOwedItems(ctx context.Context, month kernel.YearMonth) ([]domain.Item, error)
	// ListOverdueIncome spans every closed month, oldest first.
	ListOverdueIncome(ctx context.Context) ([]domain.Item, error)
	GetItem(ctx context.Context, id string) (domain.Item, error)
	CreateItem(ctx context.Context, periodID string, item domain.NewItem) (domain.Item, error)
	// SetItemTemplate links an item to the template that repeats it.
	SetItemTemplate(ctx context.Context, itemID, templateID string) (domain.Item, error)
	UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error)
	SetItemStatus(ctx context.Context, item domain.Item) (domain.Item, error)
	DeleteItem(ctx context.Context, id string) (found bool, err error)
	// ListKindOrder is the month's items of one kind in their manual order (D13).
	ListKindOrder(ctx context.Context, periodID string, kind domain.ItemKind) ([]domain.OrderedItem, error)
	// SetItemOrder numbers the items from 1 in the order of ids.
	SetItemOrder(ctx context.Context, ids []string) error
	// GetMemberItem returns domain.ErrItemNotFound when the person has no item in that month, opened or not.
	GetMemberItem(ctx context.Context, personID string, month kernel.YearMonth) (domain.Item, error)
	// MemberPayments works inside this repository's transaction. Only a repository from WithinTx has it.
	MemberPayments() MemberPayments
	// Templates works inside this repository's transaction. Only a repository from WithinTx has it.
	Templates() Templates
}

// MemberPayments is what this module asks of the platforms module when a person-linked income item is
// toggled, so that the item and its payment change together (D4, D8). An out of range amount comes back
// as kernel.ErrMoneyOutOfRange.
type MemberPayments interface {
	RecordForItem(ctx context.Context, personID, itemID string, month kernel.YearMonth) error
	RemoveForItem(ctx context.Context, itemID string) error
}

// Templates is what this module asks of the periods module when an item is added to repeat monthly, or
// removed to stop repeating, so that the item and its template change together. It only knows templates
// of fixed and income items, the kinds that have them.
type Templates interface {
	// CreateFor stores an active template that clones the item into every month opened from now on.
	// dueDay is set only on a fixed item.
	CreateFor(ctx context.Context, kind, name string, amount kernel.Money, dueDay *int, tagID *string) (string, error)
	// Stop deactivates a template, so that no month opened from now on gets its item. One already inactive
	// or deleted is no error. Items it created stay as they are.
	Stop(ctx context.Context, id string) error
	// Reorder puts the templates in the order of ids, among the positions they hold now, so the next month opened
	// gets their items in that order (D13).
	Reorder(ctx context.Context, ids []string) error
}

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}
