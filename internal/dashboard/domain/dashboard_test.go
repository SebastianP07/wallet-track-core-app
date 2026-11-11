package domain_test

import (
	"testing"

	"github.com/SebastianP07/wallet-track-core-app/internal/dashboard/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

func money(amount kernel.Money) *kernel.Money {
	return &amount
}

func TestComputeKpisOpeningBalance(t *testing.T) {
	// Surplus = 1,000,000 - 300,000 + 200,000 = 900,000; Balance = 500,000 - (250,000 + 100,000 + 50,000) = 100,000.
	accounts := domain.Accounts{CurrentTotal: 1_000_000}
	totals := domain.ItemTotals{
		Fixed:      domain.KindTotal{Total: 250_000},
		Occasional: domain.KindTotal{Total: 100_000},
		Overdue:    domain.KindTotal{Total: 50_000},
		Income:     domain.KindTotal{Total: 500_000},
		ToPay:      300_000,
		ToReceive:  200_000,
	}

	tests := []struct {
		name              string
		opening           *kernel.Money
		wantExpectedClose *kernel.Money
		wantMismatch      *kernel.Money
	}{
		{name: "unknown opening leaves expected close and mismatch unknown", opening: nil},
		{name: "a month that squares has no mismatch", opening: money(800_000), wantExpectedClose: money(900_000), wantMismatch: money(0)},
		{name: "more money than the items explain is a positive mismatch", opening: money(750_000), wantExpectedClose: money(850_000), wantMismatch: money(50_000)},
		{name: "less money than the items explain is a negative mismatch", opening: money(830_000), wantExpectedClose: money(930_000), wantMismatch: money(-30_000)},
		{name: "a negative opening counts as it is", opening: money(-100_000), wantExpectedClose: money(0), wantMismatch: money(900_000)},
		{name: "expected close stops at the money bound", opening: money(kernel.MaxMoney), wantExpectedClose: money(kernel.MaxMoney), wantMismatch: money(900_000 - kernel.MaxMoney)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kpis := domain.ComputeKpis(accounts, totals, tt.opening)

			if kpis.Surplus != 900_000 || kpis.Balance != 100_000 {
				t.Fatalf("surplus, balance = %d, %d, want 900000, 100000", kpis.Surplus, kpis.Balance)
			}
			assertMoney(t, "openingBalance", kpis.OpeningBalance, tt.opening)
			assertMoney(t, "expectedClose", kpis.ExpectedClose, tt.wantExpectedClose)
			assertMoney(t, "mismatch", kpis.Mismatch, tt.wantMismatch)
		})
	}
}

func TestComputeKpisCopiesTheOpeningBalance(t *testing.T) {
	opening := money(10)
	kpis := domain.ComputeKpis(domain.Accounts{}, domain.ItemTotals{}, opening)
	*opening = 20

	if *kpis.OpeningBalance != 10 {
		t.Errorf("openingBalance = %d after the caller changed its value, want 10", *kpis.OpeningBalance)
	}
}

func assertMoney(t *testing.T, field string, got, want *kernel.Money) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s = %v, want %v", field, got, want)
	case *got != *want:
		t.Errorf("%s = %d, want %d", field, *got, *want)
	}
}
