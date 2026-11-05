package domain

import (
	"fmt"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

const (
	dateLayout = "2006-01-02"
	// A payment's coverage must end on a date the contract can carry, at most 120 months after it.
	firstPaidYear = 2000
	lastPaidYear  = 9989
)

// PaidAtMessage is the one wording of the rule for a payment date.
const PaidAtMessage = "must be a date as YYYY-MM-DD, from 2000-01-01 to 9989-12-31"

// Payment records money a person gave. CoveredUntil is derived by the database, counting the month of
// PaidAt (D8). PeriodItemID is the income item it settled, if any (D4).
type Payment struct {
	ID            string
	PersonID      string
	PaidAt        time.Time
	MonthsCovered int
	Amount        kernel.Money
	CoveredUntil  time.Time
	PeriodItemID  *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Covers holds for the months from that of PaidAt to that of CoveredUntil, and no others: a payment says
// nothing about the months before it, even when they are opened after it.
func (p Payment) Covers(month kernel.YearMonth) bool {
	firstDay := month.DateOn(1)
	lastDay := month.DateOn(31)
	return !p.PaidAt.After(lastDay) && !p.CoveredUntil.Before(firstDay)
}

// PaymentRequest is a validated request to record a payment. A nil MonthsCovered means the person's
// billing frequency.
type PaymentRequest struct {
	PaidAt        time.Time
	MonthsCovered *int
}

func NewPaymentRequest(paidAt string, monthsCovered *int) (PaymentRequest, error) {
	var violations []Violation
	date, err := time.Parse(dateLayout, paidAt)
	if err != nil || date.Year() < firstPaidYear || date.Year() > lastPaidYear {
		violations = append(violations, Violation{Field: "paidAt", Message: PaidAtMessage})
	}
	violations = checkMonths(monthsCovered, "monthsCovered", violations)
	if len(violations) > 0 {
		return PaymentRequest{}, &ValidationError{Violations: violations}
	}
	return PaymentRequest{PaidAt: date, MonthsCovered: monthsCovered}, nil
}

// NewPayment is a payment that has not been stored yet.
type NewPayment struct {
	PersonID      string
	PaidAt        time.Time
	MonthsCovered int
	Amount        kernel.Money
	PeriodItemID  *string
}

// PaymentAmount is what the contract says a payment is: the monthly amount owed, times the months it
// covers. It returns kernel.ErrMoneyOutOfRange rather than a sum the contract cannot carry.
func PaymentAmount(monthlyOwed kernel.Money, monthsCovered int) (kernel.Money, error) {
	if monthsCovered > 0 && monthlyOwed > kernel.MaxMoney/kernel.Money(monthsCovered) {
		return 0, fmt.Errorf("%w: %d pesos for %d months", kernel.ErrMoneyOutOfRange, monthlyOwed, monthsCovered)
	}
	return monthlyOwed * kernel.Money(monthsCovered), nil
}

// PaymentDateFor anchors a payment made through an income item to that item's month: today when today
// falls in it, or else its nearest day. Coverage counts from the month of the payment date (D8), so a
// month settled late still covers itself and not the month it happened to be settled in.
func PaymentDateFor(month kernel.YearMonth, today time.Time) time.Time {
	firstDay := month.DateOn(1)
	lastDay := month.DateOn(31)
	switch {
	case today.Before(firstDay):
		return firstDay
	case today.After(lastDay):
		return lastDay
	default:
		return today
	}
}

// OwesNothing is what a direct payment gets back from someone with nothing to pay: it would otherwise
// buy free coverage for when they do owe.
func OwesNothing(person Person) error {
	message := "owes nothing right now, so there is nothing to pay"
	if person.IsOwner {
		message = "the owner owes nothing, so has no payments"
	}
	return &ValidationError{Violations: []Violation{{Field: "id", Message: message}}}
}

// PaymentOutOfRange is what a direct payment gets back when its amount would pass the bound.
func PaymentOutOfRange() error {
	return &ValidationError{Violations: []Violation{{
		Field:   "monthsCovered",
		Message: "would take this payment's amount beyond 9007199254740991",
	}}}
}

// PersonView is a person with everything only the backend computes (D1, D8, D10).
type PersonView struct {
	Person
	MonthlyOwed     kernel.Money
	LastPaymentDate *time.Time
	CoveredUntil    *time.Time
	// Whether any payment covers the month asked about. Nil when no month was asked about.
	CoveredInPeriod *bool
	// The most recent payment that covers that month.
	PeriodPaymentID *string
}

// NewPersonView reads its payments in any order. Coverage is the furthest any payment reaches and the
// month is covered by any payment that includes it, so a shorter payment made later takes nothing away
// from a longer one made before it.
func NewPersonView(person Person, platforms []Platform, payments []Payment, month *kernel.YearMonth) (PersonView, error) {
	owed, err := MonthlyOwed(person, platforms)
	if err != nil {
		return PersonView{}, err
	}
	view := PersonView{Person: person, MonthlyOwed: owed}
	covered := false
	var coveringPaidAt time.Time
	for _, payment := range payments {
		if payment.PersonID != person.ID {
			continue
		}
		if view.LastPaymentDate == nil || payment.PaidAt.After(*view.LastPaymentDate) {
			view.LastPaymentDate = &payment.PaidAt
		}
		if view.CoveredUntil == nil || payment.CoveredUntil.After(*view.CoveredUntil) {
			view.CoveredUntil = &payment.CoveredUntil
		}
		if month != nil && payment.Covers(*month) && (!covered || payment.PaidAt.After(coveringPaidAt)) {
			covered = true
			coveringPaidAt = payment.PaidAt
			view.PeriodPaymentID = &payment.ID
		}
	}
	if month != nil {
		view.CoveredInPeriod = &covered
	}
	return view, nil
}

// OwesMonth says whether opening that month gives this person an income item (D4).
func (v PersonView) OwesMonth() bool {
	return !v.IsOwner && v.MonthlyOwed > 0 && v.CoveredInPeriod != nil && !*v.CoveredInPeriod
}
