package domain

import (
	"errors"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

// KindTotal is what one kind of item adds up to in a month.
type KindTotal struct {
	Total   kernel.Money
	Pending kernel.Money
}

// Totals are the month's sums the backend owes the frontend (D1). Tags never touch them (D5).
type Totals struct {
	Fixed      KindTotal
	Occasional KindTotal
	Income     KindTotal
	CardCharge KindTotal
	// ToPay is Por pagar: pending fixed + occasional. Done, overdue (D7) and card charge (D3) items
	// are out of it.
	ToPay kernel.Money
	// ToReceive is Por llegar: pending income. Received and overdue (D7) items are out of it.
	ToReceive kernel.Money
}

// ComputeTotals returns ErrTotalOutOfRange rather than a number the contract cannot carry. It checks
// the sum of every expense too, not only of the pending ones, so that no later toggle can overflow.
func ComputeTotals(items []Item) (Totals, error) {
	var totals Totals
	var err error
	if totals.Fixed, err = totalOf(items, ItemFixed); err != nil {
		return Totals{}, err
	}
	if totals.Occasional, err = totalOf(items, ItemOccasional); err != nil {
		return Totals{}, err
	}
	if totals.Income, err = totalOf(items, ItemIncome); err != nil {
		return Totals{}, err
	}
	if totals.CardCharge, err = totalOf(items, ItemCardCharge); err != nil {
		return Totals{}, err
	}
	if _, err = kernel.SumMoney([]kernel.Money{totals.Fixed.Total, totals.Occasional.Total}); err != nil {
		return Totals{}, errors.Join(ErrTotalOutOfRange, err)
	}
	totals.ToPay, err = kernel.SumMoney([]kernel.Money{totals.Fixed.Pending, totals.Occasional.Pending})
	if err != nil {
		return Totals{}, errors.Join(ErrTotalOutOfRange, err)
	}
	totals.ToReceive = totals.Income.Pending
	return totals, nil
}

func totalOf(items []Item, kind ItemKind) (KindTotal, error) {
	var all, pending []kernel.Money
	for _, item := range items {
		if item.Kind != kind {
			continue
		}
		all = append(all, item.Amount)
		if item.Status == StatusPending {
			pending = append(pending, item.Amount)
		}
	}
	total, err := kernel.SumMoney(all)
	if err != nil {
		return KindTotal{}, errors.Join(ErrTotalOutOfRange, err)
	}
	pendingTotal, err := kernel.SumMoney(pending)
	if err != nil {
		return KindTotal{}, errors.Join(ErrTotalOutOfRange, err)
	}
	return KindTotal{Total: total, Pending: pendingTotal}, nil
}

// OverdueTotals are the sums of the overdue list, across every closed month (D7).
type OverdueTotals struct {
	Expense kernel.Money
	Income  kernel.Money
}

// ComputeOverdueTotals stops at kernel.MaxMoney instead of failing. Writes keep each month within that
// bound, but nothing bounds the months together, and an error here would hide the very rows that bring
// the sum back down.
func ComputeOverdueTotals(items []Item) OverdueTotals {
	var totals OverdueTotals
	for _, item := range items {
		switch {
		case isExpense(item.Kind):
			totals.Expense = addCapped(totals.Expense, item.Amount)
		case item.Kind == ItemIncome:
			totals.Income = addCapped(totals.Income, item.Amount)
		}
	}
	return totals
}

func addCapped(sum, amount kernel.Money) kernel.Money {
	if amount > kernel.MaxMoney-sum {
		return kernel.MaxMoney
	}
	return sum + amount
}
