package application

import (
	"context"
	"errors"
	"fmt"

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

// Execute clones the active templates and adds one income item per owing platform member (D4) only when
// it creates the month, so a retry changes nothing, not even in a month that a template, a platform or a
// payment changed after. Concurrent opens wait on the period's unique key, and only the first one to
// commit adds anything.
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
		if err := domain.CheckOpeningTotals(templates, owing); err != nil {
			return err
		}
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

// Execute leaves a month that is already closed exactly as it is.
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
		if err := repo.MarkPendingItemsOverdue(ctx, period.ID, domain.KindsOverdueOnClose()); err != nil {
			return fmt.Errorf("mark items overdue: %w", err)
		}
		closed, err = repo.MarkPeriodClosed(ctx, period.ID)
		return err
	})
	return closed, err
}
