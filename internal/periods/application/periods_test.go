package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/periods/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/periods/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

var november = kernel.YearMonth{Year: 2026, Month: time.November}

// openingRepo answers only what opening a month with nothing to clone asks; any other call panics on the
// embedded nil interface.
type openingRepo struct {
	application.Repository
	exists   bool
	total    kernel.Money
	totalErr error
	period   domain.Period
}

func (r *openingRepo) CreatePeriod(_ context.Context, month kernel.YearMonth) (domain.Period, bool, error) {
	if r.exists {
		return domain.Period{}, false, nil
	}
	r.period = domain.Period{ID: "p1", Month: month, Status: domain.PeriodOpen}
	return r.period, true, nil
}

func (r *openingRepo) GetPeriod(context.Context, kernel.YearMonth) (domain.Period, error) {
	return r.period, nil
}

func (r *openingRepo) LockActiveTemplates(context.Context) ([]domain.Template, error) {
	return nil, nil
}

func (r *openingRepo) MemberIncome() application.MemberIncome { return r }

func (r *openingRepo) Owing(context.Context, kernel.YearMonth) ([]domain.OwingMember, error) {
	return nil, nil
}

func (r *openingRepo) PlatformCharges() application.PlatformCharges { return r }

func (r *openingRepo) WithCard(context.Context) ([]domain.PlatformCharge, error) { return nil, nil }

func (r *openingRepo) AccountBalances() application.AccountBalances { return r }

func (r *openingRepo) CurrentTotal(context.Context) (kernel.Money, error) {
	return r.total, r.totalErr
}

func (r *openingRepo) SetOpeningBalance(_ context.Context, _ kernel.YearMonth, amount *kernel.Money) (domain.Period, error) {
	r.period.OpeningBalance = amount
	return r.period, nil
}

type txManager struct {
	repo application.Repository
}

func (m txManager) WithinTx(ctx context.Context, fn func(ctx context.Context, repo application.Repository) error) error {
	return fn(ctx, m.repo)
}

func TestOpenPeriodSnapshotsTheOpeningBalance(t *testing.T) {
	outOfRange := fmt.Errorf("total accounts: %w", kernel.ErrMoneyOutOfRange)

	tests := []struct {
		name        string
		repo        *openingRepo
		wantOpening *kernel.Money
		wantErr     bool
	}{
		{name: "a month it creates keeps Total actual", repo: &openingRepo{total: 13_956_609}, wantOpening: ptr(13_956_609)},
		{name: "a negative total is kept as it is", repo: &openingRepo{total: -1}, wantOpening: ptr(-1)},
		{name: "a total past the bound leaves it unknown", repo: &openingRepo{totalErr: outOfRange}},
		{name: "a month already there is left as it is", repo: &openingRepo{exists: true, total: 99}},
		{name: "a failed read fails the open", repo: &openingRepo{totalErr: errors.New("boom")}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opened, err := application.NewOpenPeriod(txManager{tt.repo}).Execute(t.Context(), november)

			if tt.wantErr {
				if err == nil {
					t.Fatal("err = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			got := opened.Period.OpeningBalance
			switch {
			case got == nil && tt.wantOpening == nil:
			case got == nil || tt.wantOpening == nil || *got != *tt.wantOpening:
				t.Errorf("openingBalance = %v, want %v", got, tt.wantOpening)
			}
		})
	}
}

func ptr(amount kernel.Money) *kernel.Money {
	return &amount
}
