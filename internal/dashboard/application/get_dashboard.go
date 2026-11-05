// Package application holds the Dashboard's one use case. It reaches other modules only through the ports
// below, wired in cmd/server.
package application

import (
	"context"
	"fmt"

	"github.com/SebastianP07/wallet-track-core-app/internal/dashboard/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

type PeriodReader interface {
	// Get returns domain.ErrPeriodNotFound for a month not opened.
	Get(ctx context.Context, month kernel.YearMonth) (domain.Period, error)
}

type AccountsReader interface {
	List(ctx context.Context) (domain.Accounts, error)
}

type ItemsReader interface {
	List(ctx context.Context, month kernel.YearMonth) (domain.Items, error)
	Overdue(ctx context.Context) (domain.OverdueItems, error)
}

type CardsReader interface {
	Totals(ctx context.Context, month kernel.YearMonth) (domain.CardCharges, error)
}

type PlatformsReader interface {
	Platforms(ctx context.Context) ([]domain.Platform, error)
	People(ctx context.Context, month kernel.YearMonth) ([]domain.Person, error)
}

type Readers struct {
	Periods   PeriodReader
	Accounts  AccountsReader
	Items     ItemsReader
	Cards     CardsReader
	Platforms PlatformsReader
}

// Dashboard is everything one month shows.
type Dashboard struct {
	Period         domain.Period
	Kpis           domain.Kpis
	ToPayBreakdown domain.ToPayBreakdown
	Accounts       domain.Accounts
	Items          domain.Items
	UpcomingDue    []domain.Item
	CardCharges    domain.CardCharges
	Platforms      []domain.Platform
	People         []domain.Person
	Overdue        domain.OverdueItems
}

type GetDashboard struct {
	readers Readers
}

func NewGetDashboard(readers Readers) *GetDashboard {
	return &GetDashboard{readers: readers}
}

func (u *GetDashboard) Execute(ctx context.Context, month kernel.YearMonth) (Dashboard, error) {
	period, err := u.readers.Periods.Get(ctx, month)
	if err != nil {
		return Dashboard{}, err
	}
	accounts, err := u.readers.Accounts.List(ctx)
	if err != nil {
		return Dashboard{}, fmt.Errorf("read accounts: %w", err)
	}
	items, err := u.readers.Items.List(ctx, month)
	if err != nil {
		return Dashboard{}, fmt.Errorf("read items: %w", err)
	}
	overdue, err := u.readers.Items.Overdue(ctx)
	if err != nil {
		return Dashboard{}, fmt.Errorf("read overdue items: %w", err)
	}
	cards, err := u.readers.Cards.Totals(ctx, month)
	if err != nil {
		return Dashboard{}, fmt.Errorf("read card totals: %w", err)
	}
	platforms, err := u.readers.Platforms.Platforms(ctx)
	if err != nil {
		return Dashboard{}, fmt.Errorf("read platforms: %w", err)
	}
	people, err := u.readers.Platforms.People(ctx, month)
	if err != nil {
		return Dashboard{}, fmt.Errorf("read people: %w", err)
	}
	return Dashboard{
		Period:         period,
		Kpis:           domain.ComputeKpis(accounts, items.Totals),
		ToPayBreakdown: domain.BreakdownOf(items.Totals),
		Accounts:       accounts,
		Items:          items,
		UpcomingDue:    domain.UpcomingDue(items.Items),
		CardCharges:    cards,
		Platforms:      platforms,
		People:         people,
		Overdue:        overdue,
	}, nil
}
