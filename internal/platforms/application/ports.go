// Package application holds the platforms use cases. It reaches infrastructure, and the expenses module,
// only through the ports below.
package application

import (
	"context"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type Clock interface {
	Now() time.Time
}

// IncomeItems is what this module asks of a month's person-linked income items (D4). The lock methods
// exist because every writer takes the month's row before the platforms lock: a payment and a toggle of
// its item take turns, and cannot deadlock. A month not opened, or an item gone, is no error.
type IncomeItems interface {
	LockMonth(ctx context.Context, month kernel.YearMonth) error
	LockMonthOfItem(ctx context.Context, itemID string) error
	// MarkReceived marks the person's item of that month received, and returns its id. It returns nil
	// when there is no such item, or it was already received and so stays with its own payment.
	MarkReceived(ctx context.Context, personID string, month kernel.YearMonth, now time.Time) (*string, error)
	// Unmark returns the item to pending, or overdue in a closed month.
	Unmark(ctx context.Context, itemID string) error
}

// CardCharges is what this module asks of the open months' card charges when a platform changes (D14). Every
// writer takes the months' rows before the platforms lock, so LockOpenMonths comes first.
type CardCharges interface {
	LockOpenMonths(ctx context.Context) error
	// Sync gives every open month the platform's pending charge, as it is now, or removes the pending ones when
	// it has no card. Done charges and closed months stay as they are. It returns kernel.ErrMoneyOutOfRange when
	// an open month's card charges would add up beyond the bound.
	Sync(ctx context.Context, platformID, name string, amount kernel.Money, cardID *string) error
	// Remove deletes the platform's pending charges of open months.
	Remove(ctx context.Context, platformID string) error
}

type Repository interface {
	// LockPlatforms holds every platform, membership and person until the transaction ends. Each write
	// to them takes it first, so checks of what people owe and a month's items see one state.
	LockPlatforms(ctx context.Context) error
	// IncomeItems works inside this repository's transaction. Only a repository from WithinTx has it.
	IncomeItems() IncomeItems
	// CardCharges works inside this repository's transaction. Only a repository from WithinTx has it.
	CardCharges() CardCharges

	ListPeople(ctx context.Context) ([]domain.Person, error)
	GetPerson(ctx context.Context, id string) (domain.Person, error)
	CreatePerson(ctx context.Context, person domain.NewPerson) (domain.Person, error)
	UpdatePerson(ctx context.Context, person domain.Person) (domain.Person, error)
	// DeletePerson returns domain.ErrPersonHasPayments for a person with payments.
	DeletePerson(ctx context.Context, id string) (found bool, err error)

	ListPlatforms(ctx context.Context) ([]domain.Platform, error)
	GetPlatform(ctx context.Context, id string) (domain.Platform, error)
	CreatePlatform(ctx context.Context, platform domain.NewPlatform) (domain.Platform, error)
	UpdatePlatform(ctx context.Context, platform domain.Platform) (domain.Platform, error)
	DeletePlatform(ctx context.Context, id string) (found bool, err error)
	AddMember(ctx context.Context, platformID, personID string) error
	RemoveMember(ctx context.Context, platformID, personID string) error

	ListPayments(ctx context.Context) ([]domain.Payment, error)
	ListPersonPayments(ctx context.Context, personID string) ([]domain.Payment, error)
	GetPayment(ctx context.Context, id string) (domain.Payment, error)
	CreatePayment(ctx context.Context, payment domain.NewPayment) (domain.Payment, error)
	DeletePayment(ctx context.Context, id string) (found bool, err error)
	// DeletePaymentOfItem removes only the payment linked to that item, if any.
	DeletePaymentOfItem(ctx context.Context, itemID string) error
}

type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repo Repository) error) error
}
