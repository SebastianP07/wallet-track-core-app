// Package domain holds the rules of monthly periods and recurring templates (D3, D7, D11).
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
	ID        string
	Month     kernel.YearMonth
	Status    PeriodStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ItemKind string

const (
	ItemFixed      ItemKind = "fixed"
	ItemOccasional ItemKind = "occasional"
	ItemIncome     ItemKind = "income"
	ItemCardCharge ItemKind = "card_charge"
)

// KindsOverdueOnClose are the kinds a closing month marks overdue when still pending (D7).
// Card charges are informational and stay as they are (D3).
func KindsOverdueOnClose() []ItemKind {
	return []ItemKind{ItemFixed, ItemOccasional, ItemIncome}
}

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
