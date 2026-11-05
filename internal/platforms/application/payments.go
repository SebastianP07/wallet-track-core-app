package application

import (
	"context"
	"errors"

	"github.com/SebastianP07/wallet-track-core-app/internal/platforms/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type ListPersonPayments struct {
	repo Repository
}

func NewListPersonPayments(repo Repository) *ListPersonPayments {
	return &ListPersonPayments{repo: repo}
}

func (u *ListPersonPayments) Execute(ctx context.Context, personID string) ([]domain.Payment, error) {
	if _, err := u.repo.GetPerson(ctx, personID); err != nil {
		return nil, err
	}
	return u.repo.ListPersonPayments(ctx, personID)
}

type RecordPayment struct {
	tx    TxManager
	clock Clock
}

func NewRecordPayment(tx TxManager, clock Clock) *RecordPayment {
	return &RecordPayment{tx: tx, clock: clock}
}

// Execute also marks the person's income item of the payment's month received, if one is waiting, and
// links the two so that undoing either undoes both (D4, D8). A payment that overlaps earlier coverage is
// taken as it is: it counts from its own month and adds nothing past that, so paying early does not
// stack months.
func (u *RecordPayment) Execute(ctx context.Context, personID string, request domain.PaymentRequest) (domain.Payment, error) {
	var created domain.Payment
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		month := kernel.YearMonth{Year: request.PaidAt.Year(), Month: request.PaidAt.Month()}
		items := repo.IncomeItems()
		if err := items.LockMonth(ctx, month); err != nil {
			return err
		}
		if err := repo.LockPlatforms(ctx); err != nil {
			return err
		}
		person, err := repo.GetPerson(ctx, personID)
		if err != nil {
			return err
		}
		owed, err := owedBy(ctx, repo, person)
		if err != nil {
			return err
		}
		if owed == 0 {
			return domain.OwesNothing(person)
		}
		months := person.BillingMonths
		if request.MonthsCovered != nil {
			months = *request.MonthsCovered
		}
		amount, err := domain.PaymentAmount(owed, months)
		if err != nil {
			return domain.PaymentOutOfRange()
		}
		itemID, err := items.MarkReceived(ctx, personID, month, u.clock.Now())
		if err != nil {
			return err
		}
		created, err = repo.CreatePayment(ctx, domain.NewPayment{
			PersonID:      personID,
			PaidAt:        request.PaidAt,
			MonthsCovered: months,
			Amount:        amount,
			PeriodItemID:  itemID,
		})
		return err
	})
	return created, err
}

type DeletePayment struct {
	tx TxManager
}

func NewDeletePayment(tx TxManager) *DeletePayment {
	return &DeletePayment{tx: tx}
}

// Execute returns the income item this payment settled, if any, to what it was before.
func (u *DeletePayment) Execute(ctx context.Context, id string) error {
	return u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		payment, err := repo.GetPayment(ctx, id)
		if err != nil {
			return err
		}
		items := repo.IncomeItems()
		if payment.PeriodItemID != nil {
			if err := items.LockMonthOfItem(ctx, *payment.PeriodItemID); err != nil {
				return err
			}
		}
		if err := repo.LockPlatforms(ctx); err != nil {
			return err
		}
		found, err := repo.DeletePayment(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return domain.ErrPaymentNotFound
		}
		if payment.PeriodItemID == nil {
			return nil
		}
		return items.Unmark(ctx, *payment.PeriodItemID)
	})
}

// owedBy reports a sum out of range as is: writes keep it from happening, so here it is a defect.
func owedBy(ctx context.Context, repo Repository, person domain.Person) (kernel.Money, error) {
	platforms, err := repo.ListPlatforms(ctx)
	if err != nil {
		return 0, err
	}
	return domain.MonthlyOwed(person, platforms)
}

// ItemPayments is what the expenses module asks of this one when a person-linked income item is toggled
// (D4, D8). Built on a repository from WithinTx, it runs in that item's transaction.
type ItemPayments struct {
	repo  Repository
	clock Clock
}

func NewItemPayments(repo Repository, clock Clock) *ItemPayments {
	return &ItemPayments{repo: repo, clock: clock}
}

// RecordForItem records the payment for one income item. Its amount is what the contract specifies and
// not the item's: what is owed now, times the person's billing frequency. A person gone since the item
// was read has no payment to record, which leaves a plain income item.
func (p *ItemPayments) RecordForItem(ctx context.Context, personID, itemID string, month kernel.YearMonth) error {
	if err := p.repo.LockPlatforms(ctx); err != nil {
		return err
	}
	person, err := p.repo.GetPerson(ctx, personID)
	if errors.Is(err, domain.ErrPersonNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	owed, err := owedBy(ctx, p.repo, person)
	if err != nil {
		return err
	}
	amount, err := domain.PaymentAmount(owed, person.BillingMonths)
	if err != nil {
		return err
	}
	_, err = p.repo.CreatePayment(ctx, domain.NewPayment{
		PersonID:      personID,
		PaidAt:        domain.PaymentDateFor(month, kernel.DateInBogota(p.clock.Now())),
		MonthsCovered: person.BillingMonths,
		Amount:        amount,
		PeriodItemID:  &itemID,
	})
	return err
}

// RemoveForItem removes that item's payment and no other. An item with none is fine.
func (p *ItemPayments) RemoveForItem(ctx context.Context, itemID string) error {
	if err := p.repo.LockPlatforms(ctx); err != nil {
		return err
	}
	return p.repo.DeletePaymentOfItem(ctx, itemID)
}

// OwingMember is one person's income for a month (D4).
type OwingMember struct {
	PersonID string
	ItemName string
	Amount   kernel.Money
}

// MemberIncome is what the periods module asks of this one when opening a month. Built on a repository
// from WithinTx, it runs in that month's transaction.
type MemberIncome struct {
	repo Repository
}

func NewMemberIncome(repo Repository) *MemberIncome {
	return &MemberIncome{repo: repo}
}

// Owing decides by the month being opened, never by today: a month past the end of someone's coverage
// gets their item even when opened while that coverage still runs.
func (m *MemberIncome) Owing(ctx context.Context, month kernel.YearMonth) ([]OwingMember, error) {
	if err := m.repo.LockPlatforms(ctx); err != nil {
		return nil, err
	}
	people, err := m.repo.ListPeople(ctx)
	if err != nil {
		return nil, err
	}
	views, err := viewsOf(ctx, m.repo, people, &month)
	if err != nil {
		return nil, err
	}
	owing := make([]OwingMember, 0, len(views))
	for _, view := range views {
		if view.OwesMonth() {
			owing = append(owing, OwingMember{
				PersonID: view.ID,
				ItemName: domain.MemberItemName(view.Name),
				Amount:   view.MonthlyOwed,
			})
		}
	}
	return owing, nil
}
