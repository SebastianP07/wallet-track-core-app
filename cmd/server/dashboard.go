package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	accountspostgres "github.com/SebastianP07/wallet-track-core-app/internal/accounts/adapters/postgres"
	accountsapplication "github.com/SebastianP07/wallet-track-core-app/internal/accounts/application"
	authhttp "github.com/SebastianP07/wallet-track-core-app/internal/auth/adapters/http"
	cardspostgres "github.com/SebastianP07/wallet-track-core-app/internal/cards/adapters/postgres"
	cardsapplication "github.com/SebastianP07/wallet-track-core-app/internal/cards/application"
	dashboardhttp "github.com/SebastianP07/wallet-track-core-app/internal/dashboard/adapters/http"
	dashboardapplication "github.com/SebastianP07/wallet-track-core-app/internal/dashboard/application"
	dashboarddomain "github.com/SebastianP07/wallet-track-core-app/internal/dashboard/domain"
	expensespostgres "github.com/SebastianP07/wallet-track-core-app/internal/expenses/adapters/postgres"
	expensesapplication "github.com/SebastianP07/wallet-track-core-app/internal/expenses/application"
	expensesdomain "github.com/SebastianP07/wallet-track-core-app/internal/expenses/domain"
	periodspostgres "github.com/SebastianP07/wallet-track-core-app/internal/periods/adapters/postgres"
	periodsapplication "github.com/SebastianP07/wallet-track-core-app/internal/periods/application"
	periodsdomain "github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	platformspostgres "github.com/SebastianP07/wallet-track-core-app/internal/platforms/adapters/postgres"
	platformsapplication "github.com/SebastianP07/wallet-track-core-app/internal/platforms/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
	tagspostgres "github.com/SebastianP07/wallet-track-core-app/internal/tags/adapters/postgres"
	tagsapplication "github.com/SebastianP07/wallet-track-core-app/internal/tags/application"
)

func newDashboardHandler(pool *pgxpool.Pool, auth *authhttp.Handler, logger *slog.Logger) *dashboardhttp.Handler {
	expenses := expensespostgres.NewRepository(pool)
	people := platformspostgres.NewRepository(pool)
	clock := platform.SystemClock{}
	periods := periodReader{periodsapplication.NewGetPeriod(periodspostgres.NewRepository(pool))}
	accounts := accountsReader{accountsapplication.NewListAccounts(accountspostgres.NewRepository(pool))}
	items := itemsReader{
		list:    expensesapplication.NewListItems(expenses, clock),
		overdue: expensesapplication.NewListOverdueItems(expenses, clock),
	}
	return dashboardhttp.NewHandler(dashboardhttp.Config{
		Get: dashboardapplication.NewGetDashboard(dashboardapplication.Readers{
			Periods:  periods,
			Accounts: accounts,
			Items:    items,
			Cards:    cardsReader{cardsapplication.NewGetCardTotals(cardspostgres.NewRepository(pool))},
			Platforms: platformsReader{
				platforms: platformsapplication.NewListPlatforms(people),
				people:    platformsapplication.NewListPeople(people),
			},
		}),
		Export: dashboardapplication.NewExportMonth(dashboardapplication.ExportReaders{
			Periods:  periods,
			Accounts: accounts,
			Items:    items,
			Tags:     tagsReader{tagsapplication.NewListTags(tagspostgres.NewRepository(pool))},
		}),
		RequireSession: auth.RequireFullSession,
		Logger:         logger,
	})
}

// The readers below carry each module's answer in the Dashboard's types, as memberIncome does for periods.

type tagsReader struct {
	list *tagsapplication.ListTags
}

func (r tagsReader) List(ctx context.Context) ([]dashboarddomain.Tag, error) {
	tags, err := r.list.Execute(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]dashboarddomain.Tag, 0, len(tags))
	for _, tag := range tags {
		names = append(names, dashboarddomain.Tag{ID: tag.ID, Name: tag.Name})
	}
	return names, nil
}

type periodReader struct {
	get *periodsapplication.GetPeriod
}

func (r periodReader) Get(ctx context.Context, month kernel.YearMonth) (dashboarddomain.Period, error) {
	period, err := r.get.Execute(ctx, month)
	if errors.Is(err, periodsdomain.ErrPeriodNotFound) {
		return dashboarddomain.Period{}, dashboarddomain.ErrPeriodNotFound
	}
	if err != nil {
		return dashboarddomain.Period{}, err
	}
	return dashboarddomain.Period{
		ID:        period.ID,
		Month:     period.Month,
		Status:    string(period.Status),
		CreatedAt: period.CreatedAt,
		UpdatedAt: period.UpdatedAt,
	}, nil
}

type accountsReader struct {
	list *accountsapplication.ListAccounts
}

func (r accountsReader) List(ctx context.Context) (dashboarddomain.Accounts, error) {
	list, err := r.list.Execute(ctx)
	if err != nil {
		return dashboarddomain.Accounts{}, err
	}
	items := make([]dashboarddomain.Account, 0, len(list.Items))
	for _, account := range list.Items {
		items = append(items, dashboarddomain.Account{
			ID:        account.ID,
			Name:      account.Name,
			Balance:   account.Balance,
			CreatedAt: account.CreatedAt,
			UpdatedAt: account.UpdatedAt,
		})
	}
	return dashboarddomain.Accounts{Items: items, CurrentTotal: list.Totals.Current}, nil
}

type itemsReader struct {
	list    *expensesapplication.ListItems
	overdue *expensesapplication.ListOverdueItems
}

func (r itemsReader) List(ctx context.Context, month kernel.YearMonth) (dashboarddomain.Items, error) {
	list, err := r.list.Execute(ctx, month, expensesdomain.ItemFilter{})
	if err != nil {
		return dashboarddomain.Items{}, err
	}
	return dashboarddomain.Items{
		Items: dashboardItems(list.Items),
		Totals: dashboarddomain.ItemTotals{
			Fixed:      dashboarddomain.KindTotal(list.Totals.Fixed),
			Occasional: dashboarddomain.KindTotal(list.Totals.Occasional),
			Income:     dashboarddomain.KindTotal(list.Totals.Income),
			CardCharge: dashboarddomain.KindTotal(list.Totals.CardCharge),
			Overdue:    dashboarddomain.KindTotal(list.Totals.Overdue),
			ToPay:      list.Totals.ToPay,
			ToReceive:  list.Totals.ToReceive,
		},
	}, nil
}

func (r itemsReader) Overdue(ctx context.Context, month kernel.YearMonth) (dashboarddomain.OverdueItems, error) {
	list, err := r.overdue.Execute(ctx, month)
	if err != nil {
		return dashboarddomain.OverdueItems{}, err
	}
	return dashboarddomain.OverdueItems{
		Items:        dashboardItems(list.Items),
		ExpenseTotal: list.Totals.Expense,
		IncomeTotal:  list.Totals.Income,
	}, nil
}

func dashboardItems(views []expensesapplication.ItemView) []dashboarddomain.Item {
	items := make([]dashboarddomain.Item, 0, len(views))
	for _, view := range views {
		items = append(items, dashboarddomain.Item{
			ID:         view.ID,
			Month:      view.Month,
			Kind:       string(view.Kind),
			Name:       view.Name,
			Amount:     view.Amount,
			DueDate:    view.DueDate,
			TemplateID: view.TemplateID,
			PersonID:   view.PersonID,
			CardID:     view.CardID,
			TagID:      view.TagID,
			Status:     string(view.Status),
			DoneAt:     view.DoneAt,
			OwedIn:     view.OwedIn,
			Urgency:    string(view.Urgency),
			CreatedAt:  view.CreatedAt,
			UpdatedAt:  view.UpdatedAt,
		})
	}
	return items
}

type cardsReader struct {
	totals *cardsapplication.GetCardTotals
}

func (r cardsReader) Totals(ctx context.Context, month kernel.YearMonth) (dashboarddomain.CardCharges, error) {
	totals, err := r.totals.Execute(ctx, month)
	if err != nil {
		return dashboarddomain.CardCharges{}, err
	}
	items := make([]dashboarddomain.CardTotal, 0, len(totals.Items))
	for _, item := range totals.Items {
		items = append(items, dashboarddomain.CardTotal(item))
	}
	return dashboarddomain.CardCharges{Items: items, Total: totals.Total}, nil
}

type platformsReader struct {
	platforms *platformsapplication.ListPlatforms
	people    *platformsapplication.ListPeople
}

func (r platformsReader) Platforms(ctx context.Context) ([]dashboarddomain.Platform, error) {
	platforms, err := r.platforms.Execute(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]dashboarddomain.Platform, 0, len(platforms))
	for _, p := range platforms {
		members := make([]dashboarddomain.Member, 0, len(p.Members))
		for _, member := range p.Members {
			members = append(members, dashboarddomain.Member(member))
		}
		items = append(items, dashboarddomain.Platform{
			ID:             p.ID,
			Name:           p.Name,
			MonthlyCost:    p.MonthlyCost,
			CardID:         p.CardID,
			SharePerMember: p.SharePerMember(),
			Members:        members,
			CreatedAt:      p.CreatedAt,
			UpdatedAt:      p.UpdatedAt,
		})
	}
	return items, nil
}

func (r platformsReader) People(ctx context.Context, month kernel.YearMonth) ([]dashboarddomain.Person, error) {
	views, err := r.people.Execute(ctx, &month)
	if err != nil {
		return nil, err
	}
	people := make([]dashboarddomain.Person, 0, len(views))
	for _, view := range views {
		people = append(people, dashboarddomain.Person{
			ID:              view.ID,
			Name:            view.Name,
			IsOwner:         view.IsOwner,
			BillingMonths:   view.BillingMonths,
			MonthlyOwed:     view.MonthlyOwed,
			LastPaymentDate: view.LastPaymentDate,
			CoveredUntil:    view.CoveredUntil,
			CoveredInPeriod: view.CoveredInPeriod,
			PeriodPaymentID: view.PeriodPaymentID,
			CreatedAt:       view.CreatedAt,
			UpdatedAt:       view.UpdatedAt,
		})
	}
	return people, nil
}
