// Package domain holds the rules of monthly periods and recurring templates (D3, D7, D11, D14).
package domain

import (
	"errors"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

var (
	ErrPeriodNotFound   = errors.New("period not found")
	ErrTemplateNotFound = errors.New("template not found")
)

// Violation names one rejected value.
type Violation struct {
	Field   string
	Message string
}

// ValidationError lists every rejected value of a request.
type ValidationError struct {
	Violations []Violation
}

func (e *ValidationError) Error() string {
	return "period or template values are not valid"
}

type PeriodStatus string

const (
	PeriodOpen   PeriodStatus = "open"
	PeriodClosed PeriodStatus = "closed"
)

type Period struct {
	ID     string
	Month  kernel.YearMonth
	Status PeriodStatus
	// OpeningBalance is what the accounts held when the month began: Total actual when the month was opened, or
	// what the owner set. Nil when nobody knows it. It is informational and changes no total.
	OpeningBalance *kernel.Money
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// OpeningBalanceMessage is the one wording of the opening balance's bound.
const OpeningBalanceMessage = "must be whole pesos between -9007199254740991 and 9007199254740991, or null"

// NewOpeningBalance checks a value the owner typed; nil clears the month's opening balance. It may be negative,
// like Total actual.
func NewOpeningBalance(raw *int64) (*kernel.Money, error) {
	if raw == nil {
		return nil, nil
	}
	amount, err := kernel.NewMoney(*raw)
	if err != nil {
		return nil, &ValidationError{Violations: []Violation{{Field: "openingBalance", Message: OpeningBalanceMessage}}}
	}
	return &amount, nil
}

type ItemKind string

const (
	ItemFixed      ItemKind = "fixed"
	ItemOccasional ItemKind = "occasional"
	ItemIncome     ItemKind = "income"
	ItemCardCharge ItemKind = "card_charge"
)

// NewItem is a pending item that has not been stored yet.
type NewItem struct {
	PeriodID   string
	TemplateID string
	Kind       ItemKind
	Name       string
	Amount     kernel.Money
	DueDate    *time.Time
	CardID     *string
	TagID      *string
}

// ItemFrom clones a template into a month. Only fixed items carry a due date (D11).
func ItemFrom(template Template, period Period) NewItem {
	item := NewItem{
		PeriodID:   period.ID,
		TemplateID: template.ID,
		Kind:       ItemKind(template.Kind),
		Name:       template.Name,
		Amount:     template.Amount,
		CardID:     template.CardID,
		TagID:      template.TagID,
	}
	if template.Kind == TemplateFixed && template.DueDay != nil {
		due := period.Month.DateOn(*template.DueDay)
		item.DueDate = &due
	}
	return item
}

// OwingMember is a platform member who owes a month, as the platforms module says (D4).
type OwingMember struct {
	PersonID string
	ItemName string
	Amount   kernel.Money
}

// NewMemberItem is a member's pending income item that has not been stored yet.
type NewMemberItem struct {
	PeriodID string
	PersonID string
	Name     string
	Amount   kernel.Money
}

func MemberItemFrom(member OwingMember, period Period) NewMemberItem {
	return NewMemberItem{PeriodID: period.ID, PersonID: member.PersonID, Name: member.ItemName, Amount: member.Amount}
}

// PlatformCharge is a platform with a card, as the platforms module says: the source of one card charge in
// every month opened (D14).
type PlatformCharge struct {
	PlatformID string
	Name       string
	Amount     kernel.Money
	CardID     string
}

// NewPlatformItem is a platform's pending card charge that has not been stored yet.
type NewPlatformItem struct {
	PeriodID   string
	PlatformID string
	Name       string
	Amount     kernel.Money
	CardID     string
}

func PlatformItemFrom(charge PlatformCharge, period Period) NewPlatformItem {
	return NewPlatformItem{
		PeriodID:   period.ID,
		PlatformID: charge.PlatformID,
		Name:       charge.Name,
		Amount:     charge.Amount,
		CardID:     charge.CardID,
	}
}
