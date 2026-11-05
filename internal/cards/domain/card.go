// Package domain holds what the app knows about credit cards and the informational charges tracked
// against them (D3).
package domain

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

const (
	nameMaxChars = 120
	dayMin       = 1
	dayMax       = 31
)

// DayOutOfRangeMessage is the one wording of the bound, also for values too large to reach the domain.
const DayOutOfRangeMessage = "must be a whole number between 1 and 31"

var (
	ErrNotFound       = errors.New("card not found")
	ErrPeriodNotFound = errors.New("period not found")
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
	return "card values are not valid"
}

type Card struct {
	ID           string
	Name         string
	Issuer       string
	StatementDay int
	DueDay       int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CardInput is what a request asks for, not yet validated. On an edit, nil leaves a field as it is.
type CardInput struct {
	Name         *string
	Issuer       *string
	StatementDay *int
	DueDay       *int
}

// NewCard is a valid card that has not been stored yet.
type NewCard struct {
	Name         string
	Issuer       string
	StatementDay int
	DueDay       int
}

// CreateNewCard validates a new card. The adapter has already answered for any missing field.
func CreateNewCard(input CardInput) (NewCard, error) {
	changes, err := NewChanges(input)
	if err != nil {
		return NewCard{}, err
	}
	return NewCard{
		Name:         *changes.Name,
		Issuer:       *changes.Issuer,
		StatementDay: *changes.StatementDay,
		DueDay:       *changes.DueDay,
	}, nil
}

// Changes are the validated fields of an edit; nil leaves a field as it is.
type Changes struct {
	Name         *string
	Issuer       *string
	StatementDay *int
	DueDay       *int
}

func NewChanges(input CardInput) (Changes, error) {
	changes := Changes{StatementDay: input.StatementDay, DueDay: input.DueDay}
	var violations []Violation
	changes.Name, violations = checkName(input.Name, "name", violations)
	changes.Issuer, violations = checkName(input.Issuer, "issuer", violations)
	violations = checkDay(input.StatementDay, "statementDay", violations)
	violations = checkDay(input.DueDay, "dueDay", violations)
	if len(violations) > 0 {
		return Changes{}, &ValidationError{Violations: violations}
	}
	return changes, nil
}

func checkName(raw *string, field string, violations []Violation) (*string, []Violation) {
	if raw == nil {
		return nil, violations
	}
	name := strings.TrimSpace(*raw)
	length := utf8.RuneCountInString(name)
	if length < 1 || length > nameMaxChars {
		return nil, append(violations, Violation{Field: field, Message: "must have between 1 and 120 characters"})
	}
	// PostgreSQL refuses NUL in TEXT, and no control character belongs in a name.
	if strings.ContainsFunc(name, unicode.IsControl) {
		return nil, append(violations, Violation{Field: field, Message: "must not contain control characters"})
	}
	return &name, violations
}

func checkDay(day *int, field string, violations []Violation) []Violation {
	if day != nil && (*day < dayMin || *day > dayMax) {
		return append(violations, Violation{Field: field, Message: DayOutOfRangeMessage})
	}
	return violations
}

// CardTotal is what one card was charged in a month. A nil CardID gathers the charges whose card was
// deleted, so their money stays visible and in the month's total.
type CardTotal struct {
	CardID   *string
	CardName *string
	Total    kernel.Money
}

// MonthTotals are a month's card charges, whatever their status: all of them reach the statement.
// They are informational and never part of Por pagar, Se va or Balance (D3).
type MonthTotals struct {
	Items []CardTotal
	Total kernel.Money
}

func NewMonthTotals(items []CardTotal) (MonthTotals, error) {
	amounts := make([]kernel.Money, 0, len(items))
	for _, item := range items {
		amounts = append(amounts, item.Total)
	}
	total, err := kernel.SumMoney(amounts)
	if err != nil {
		return MonthTotals{}, err
	}
	return MonthTotals{Items: items, Total: total}, nil
}
