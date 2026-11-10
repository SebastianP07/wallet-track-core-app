package application

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type ListPeriods struct {
	repo Repository
}

func NewListPeriods(repo Repository) *ListPeriods {
	return &ListPeriods{repo: repo}
}

func (u *ListPeriods) Execute(ctx context.Context) ([]domain.Period, error) {
	return u.repo.ListPeriods(ctx)
}

type GetPeriod struct {
	repo Repository
}

func NewGetPeriod(repo Repository) *GetPeriod {
	return &GetPeriod{repo: repo}
}

func (u *GetPeriod) Execute(ctx context.Context, month kernel.YearMonth) (domain.Period, error) {
	return u.repo.GetPeriod(ctx, month)
}

// OpenedPeriod says whether this call created the month or found it there.
type OpenedPeriod struct {
	Period  domain.Period
	Created bool
}

type OpenPeriod struct {
	tx TxManager
}

func NewOpenPeriod(tx TxManager) *OpenPeriod {
	return &OpenPeriod{tx: tx}
}

// Execute clones the active templates, adds one income item per owing platform member (D4) and one card
// charge per platform with a card (D14) only when it creates the month, so a retry changes nothing, not even
// in a month that a template, a platform or a payment changed after. Concurrent opens wait on the period's
// unique key, and only the first one to commit adds anything.
func (u *OpenPeriod) Execute(ctx context.Context, month kernel.YearMonth) (OpenedPeriod, error) {
	var opened OpenedPeriod
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		period, created, err := repo.CreatePeriod(ctx, month)
		if err != nil {
			return fmt.Errorf("create period: %w", err)
		}
		if !created {
			existing, err := repo.GetPeriod(ctx, month)
			if err != nil {
				return fmt.Errorf("get period: %w", err)
			}
			opened = OpenedPeriod{Period: existing}
			return nil
		}
		templates, err := repo.LockActiveTemplates(ctx)
		if err != nil {
			return fmt.Errorf("list active templates: %w", err)
		}
		owing, err := repo.MemberIncome().Owing(ctx, month)
		if errors.Is(err, kernel.ErrMoneyOutOfRange) {
			return domain.TotalsOutOfRange()
		}
		if err != nil {
			return fmt.Errorf("list owing members: %w", err)
		}
		charges, err := repo.PlatformCharges().WithCard(ctx)
		if err != nil {
			return fmt.Errorf("list platforms with a card: %w", err)
		}
		if err := domain.CheckOpeningTotals(templates, owing, charges); err != nil {
			return err
		}
		// Each item goes to the end of its list as it is stored, so cloning in template order gives the month
		// that order (D13). The rows stay locked in id order.
		slices.SortStableFunc(templates, func(a, b domain.Template) int {
			return cmp.Or(cmp.Compare(a.Position, b.Position), cmp.Compare(a.ID, b.ID))
		})
		for _, template := range templates {
			if err := repo.AddItem(ctx, domain.ItemFrom(template, period)); err != nil {
				return fmt.Errorf("clone template: %w", err)
			}
		}
		for _, member := range owing {
			if err := repo.AddMemberItem(ctx, domain.MemberItemFrom(member, period)); err != nil {
				return fmt.Errorf("add member item: %w", err)
			}
		}
		// After the template items, so the platforms' charges end the month's card charges (D13).
		for _, charge := range charges {
			if err := repo.AddPlatformItem(ctx, domain.PlatformItemFrom(charge, period)); err != nil {
				return fmt.Errorf("add platform item: %w", err)
			}
		}
		opened = OpenedPeriod{Period: period, Created: true}
		return nil
	})
	return opened, err
}

type ClosePeriod struct {
	tx TxManager
}

func NewClosePeriod(tx TxManager) *ClosePeriod {
	return &ClosePeriod{tx: tx}
}

// Execute leaves a month that is already closed exactly as it is. Otherwise every unpaid fixed and
// occasional item of the month, and every overdue expense it owed, moves to the Overdue section of the first
// later month that is not closed, opened or not (D7). One item stays one row: carrying only changes the month
// that owes it. Unreceived income turns overdue and stays here, and card charges stay as they are (D3).
func (u *ClosePeriod) Execute(ctx context.Context, month kernel.YearMonth) (domain.Period, error) {
	var closed domain.Period
	err := u.tx.WithinTx(ctx, func(ctx context.Context, repo Repository) error {
		period, err := repo.LockPeriod(ctx, month)
		if err != nil {
			return err
		}
		if period.Status == domain.PeriodClosed {
			closed = period
			return nil
		}
		// Months lock in ascending order, here and when an undo looks for a month to owe an item, so no
		// item can land in a month that closed meanwhile.
		target, err := kernel.FirstMonthNotClosed(month.Next(), func(next kernel.YearMonth) (bool, error) {
			later, err := repo.LockPeriod(ctx, next)
			if errors.Is(err, domain.ErrPeriodNotFound) {
				return false, nil
			}
			return later.Status == domain.PeriodClosed, err
		})
		if err != nil {
			return fmt.Errorf("find the month that owes what is unpaid: %w", err)
		}
		if err := repo.CarryOwedExpenses(ctx, month, target); err != nil {
			return fmt.Errorf("carry overdue expenses: %w", err)
		}
		if err := repo.CarryPendingExpenses(ctx, period.ID, target); err != nil {
			return fmt.Errorf("carry unpaid expenses: %w", err)
		}
		if err := repo.MarkPendingIncomeOverdue(ctx, period.ID); err != nil {
			return fmt.Errorf("mark income overdue: %w", err)
		}
		closed, err = repo.MarkPeriodClosed(ctx, period.ID)
		return err
	})
	return closed, err
}
