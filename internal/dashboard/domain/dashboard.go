// Package domain holds what the Dashboard reads from other modules, in its own types, and the KPIs
// derived from it (D1).
package domain

import (
	"cmp"
	"errors"
	"slices"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

// ErrPeriodNotFound means the month has not been opened.
var ErrPeriodNotFound = errors.New("period not found")

const (
	KindFixed      = "fixed"
	KindOccasional = "occasional"
	StatusPending  = "pending"
)

type Period struct {
	ID        string
	Month     kernel.YearMonth
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Account struct {
	ID        string
	Name      string
	Balance   kernel.Money
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Accounts are current values: no month changes them (D6).
type Accounts struct {
	Items        []Account
	CurrentTotal kernel.Money
}

type Item struct {
	ID         string
	Month      kernel.YearMonth
	Kind       string
	Name       string
	Amount     kernel.Money
	DueDate    *time.Time
	TemplateID *string
	PersonID   *string
	CardID     *string
	TagID      *string
	Status     string
	DoneAt     *time.Time
	OwedIn     *kernel.YearMonth
	Urgency    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type KindTotal struct {
	Total   kernel.Money
	Pending kernel.Money
}

type ItemTotals struct {
	Fixed      KindTotal
	Occasional KindTotal
	Income     KindTotal
	CardCharge KindTotal
	// Overdue is the expenses earlier months left unpaid that this month owes (D7).
	Overdue   KindTotal
	ToPay     kernel.Money
	ToReceive kernel.Money
}

type Items struct {
	Items  []Item
	Totals ItemTotals
}

type OverdueItems struct {
	Items        []Item
	ExpenseTotal kernel.Money
	IncomeTotal  kernel.Money
}

type CardTotal struct {
	CardID   *string
	CardName *string
	Total    kernel.Money
}

type CardCharges struct {
	Items []CardTotal
	Total kernel.Money
}

type Member struct {
	PersonID string
	Name     string
}

type Platform struct {
	ID             string
	Name           string
	MonthlyCost    kernel.Money
	CardID         *string
	SharePerMember kernel.Money
	Members        []Member
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Person struct {
	ID              string
	Name            string
	IsOwner         bool
	BillingMonths   int
	MonthlyOwed     kernel.Money
	LastPaymentDate *time.Time
	CoveredUntil    *time.Time
	CoveredInPeriod *bool
	PeriodPaymentID *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Kpis are the month's numbers. Card charges reach none of them (D3). Balances are today's whichever month
// is asked about, and every account counts toward CurrentTotal (D6, D9). The overdue expenses this month owes are in ToPay while unpaid and in Outgoing
// whatever their status, like its own expenses (D2, D7). This month's own items that closing made overdue
// stay in Incoming and Outgoing, which read every status, and leave ToPay and ToReceive: the next month
// owes those expenses, and overdue income counts toward no month's totals.
type Kpis struct {
	CurrentTotal kernel.Money
	ToPay        kernel.Money
	ToReceive    kernel.Money
	Surplus      kernel.Money
	Incoming     kernel.Money
	Outgoing     kernel.Money
	Balance      kernel.Money
}

type ToPayBreakdown struct {
	Fixed      kernel.Money
	Occasional kernel.Money
	Overdue    kernel.Money
}

// ComputeKpis stops at the money bound instead of failing. Writes keep each module's own sums within it,
// but no write sees balances and items together, so Surplus can pass it with nothing wrong in either, and
// an error would take down the page where the user fixes that. Every input lies within the bound, so the
// arithmetic itself fits an int64.
func ComputeKpis(accounts Accounts, totals ItemTotals) Kpis {
	outgoing := totals.Fixed.Total + totals.Occasional.Total + totals.Overdue.Total
	return Kpis{
		CurrentTotal: accounts.CurrentTotal,
		ToPay:        totals.ToPay,
		ToReceive:    totals.ToReceive,
		Surplus:      withinBound(accounts.CurrentTotal - totals.ToPay + totals.ToReceive),
		Incoming:     totals.Income.Total,
		Outgoing:     withinBound(outgoing),
		Balance:      withinBound(totals.Income.Total - outgoing),
	}
}

func withinBound(amount kernel.Money) kernel.Money {
	return min(max(amount, -kernel.MaxMoney), kernel.MaxMoney)
}

func BreakdownOf(totals ItemTotals) ToPayBreakdown {
	return ToPayBreakdown{Fixed: totals.Fixed.Pending, Occasional: totals.Occasional.Pending, Overdue: totals.Overdue.Pending}
}

// UpcomingDue is the pending fixed and occasional items, soonest due first. Name and id settle a tie, so the
// order holds between refetches.
func UpcomingDue(items []Item) []Item {
	upcoming := make([]Item, 0, len(items))
	for _, item := range items {
		if item.Status == StatusPending && (item.Kind == KindFixed || item.Kind == KindOccasional) {
			upcoming = append(upcoming, item)
		}
	}
	slices.SortFunc(upcoming, func(a, b Item) int {
		return cmp.Or(compareDue(a.DueDate, b.DueDate), cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
	return upcoming
}

// compareDue puts an item with no date last. The contract sets one on both kinds, but the column allows null.
func compareDue(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	default:
		return a.Compare(*b)
	}
}
