// Package domain holds the rules of a month's items: urgency (D11), Por pagar and Por llegar (D1), the
// done toggle and the overdue items a month owes (D7), and card charges, which are informational and stay
// out of every total (D3).
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
	dateLayout   = "2006-01-02"
	firstDueYear = 2000
)

// AmountOutOfRangeMessage is the one wording of the bound, also for values too large to reach the domain.
const AmountOutOfRangeMessage = "must be whole pesos between 0 and 9007199254740991"

const dueDateMessage = "must be a date as YYYY-MM-DD, from 2000-01-01 on"

const totalOutOfRangeMessage = "would take this month's total beyond 9007199254740991"

var (
	incomeDueDateViolation     = Violation{Field: "dueDate", Message: "income items have no due date"}
	cardChargeDueDateViolation = Violation{Field: "dueDate", Message: "card charge items have no due date"}
	cardOnlyOnChargeViolation  = Violation{Field: "cardId", Message: "only card charge items have a card"}
)

var (
	ErrPeriodNotFound = errors.New("period not found")
	ErrItemNotFound   = errors.New("item not found")
	// ErrMonthClosed means the month takes no new items.
	ErrMonthClosed = errors.New("month is closed")
	// ErrTotalOutOfRange means a month's total lies beyond kernel.MaxMoney.
	ErrTotalOutOfRange = errors.New("item total out of range")
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
	return "item values are not valid"
}

// TotalViolation is what a write gets back when it would leave a total the contract cannot carry.
func TotalViolation() error {
	return &ValidationError{Violations: []Violation{{Field: "amount", Message: totalOutOfRangeMessage}}}
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
}

type ItemKind string

const (
	ItemFixed      ItemKind = "fixed"
	ItemOccasional ItemKind = "occasional"
	ItemIncome     ItemKind = "income"
	ItemCardCharge ItemKind = "card_charge"
)

// ParseItemKind accepts every kind of the contract, for reading.
func ParseItemKind(raw string) (ItemKind, bool) {
	switch kind := ItemKind(raw); kind {
	case ItemFixed, ItemOccasional, ItemIncome, ItemCardCharge:
		return kind, true
	default:
		return "", false
	}
}

// IsExpense says whether the kind is a fixed or occasional expense, the only kinds a later month can owe (D7).
func IsExpense(kind ItemKind) bool {
	return kind == ItemFixed || kind == ItemOccasional
}

type ItemStatus string

const (
	StatusPending ItemStatus = "pending"
	StatusDone    ItemStatus = "done"
	StatusOverdue ItemStatus = "overdue"
)

func ParseItemStatus(raw string) (ItemStatus, bool) {
	switch status := ItemStatus(raw); status {
	case StatusPending, StatusDone, StatusOverdue:
		return status, true
	default:
		return "", false
	}
}

type Item struct {
	ID         string
	Month      kernel.YearMonth
	TemplateID *string
	PersonID   *string
	Kind       ItemKind
	Name       string
	Amount     kernel.Money
	DueDate    *time.Time
	CardID     *string
	TagID      *string
	Status     ItemStatus
	DoneAt     *time.Time
	// OwedIn is the month whose Overdue section lists this expense, set once a closed month left it
	// unpaid (D7). Month stays its month of origin. Once paid it keeps the month that paid it.
	OwedIn    *kernel.YearMonth
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Done is idempotent: an item already done keeps the moment it was first done. Never touches a
// balance (D6). An overdue expense keeps OwedIn, so it stays in that month's Se va (D2, D7).
func (i Item) Done(now time.Time) Item {
	if i.Status == StatusDone {
		return i
	}
	i.Status = StatusDone
	i.DoneAt = &now
	return i
}

// Undone is idempotent: only a done item changes. In a closed month an expense or income item goes
// back to overdue, since it is what closing would have made of it; a card charge never is (D3). An
// expense that goes back to overdue is owed in owedIn, which the caller finds with FirstMonthNotClosed.
func (i Item) Undone(period Period, owedIn kernel.YearMonth) Item {
	if i.Status != StatusDone {
		return i
	}
	i.DoneAt = nil
	i.Status = StatusPending
	i.OwedIn = nil
	if period.Status == PeriodClosed && i.Kind != ItemCardCharge {
		i.Status = StatusOverdue
		if IsExpense(i.Kind) {
			i.OwedIn = &owedIn
		}
	}
	return i
}

// UndoneOwedFrom is the first month that could owe this expense again if it went back to overdue: the
// month that owed it when it was paid, or else the month after its own (D7).
func (i Item) UndoneOwedFrom() kernel.YearMonth {
	if i.OwedIn != nil {
		return *i.OwedIn
	}
	return i.Month.Next()
}

// NeedsOwedMonth says whether undoing this item makes a later month owe it, so the caller has to find one.
func (i Item) NeedsOwedMonth(period Period) bool {
	return i.Status == StatusDone && period.Status == PeriodClosed && IsExpense(i.Kind)
}

// Matches says whether an item passes a list's filters. A nil field passes everything.
type ItemFilter struct {
	Kind   *ItemKind
	Status *ItemStatus
	TagID  *string
}

func (f ItemFilter) Matches(item Item) bool {
	if f.Kind != nil && item.Kind != *f.Kind {
		return false
	}
	if f.Status != nil && item.Status != *f.Status {
		return false
	}
	if f.TagID != nil && (item.TagID == nil || *item.TagID != *f.TagID) {
		return false
	}
	return true
}

// NewItemInput is what a request asks for, not yet validated.
type NewItemInput struct {
	Kind    string
	Name    string
	Amount  int64
	DueDate *string
	CardID  OptionalID
	TagID   *string
}

// NewItem is a valid pending item that has not been stored yet. Only expenses have a DueDate, and
// only card charges a CardID.
type NewItem struct {
	Kind    ItemKind
	Name    string
	Amount  kernel.Money
	DueDate *time.Time
	CardID  *string
	TagID   *string
}

// CreateNewItem validates a new item.
func CreateNewItem(input NewItemInput) (NewItem, error) {
	var violations []Violation
	item := NewItem{TagID: input.TagID}
	kind := ItemKind(input.Kind)
	switch {
	case IsExpense(kind):
		item.Kind = kind
		item.DueDate, violations = checkRequiredDueDate(input.DueDate, violations)
	case kind == ItemIncome:
		item.Kind = kind
		if input.DueDate != nil {
			violations = append(violations, incomeDueDateViolation)
		}
	case kind == ItemCardCharge:
		item.Kind = kind
		item.CardID = input.CardID.Value
		if input.DueDate != nil {
			violations = append(violations, cardChargeDueDateViolation)
		}
		if item.CardID == nil {
			violations = append(violations, Violation{Field: "cardId", Message: "is required on a card charge item"})
		}
	default:
		violations = append(violations, Violation{Field: "kind", Message: "must be one of fixed, occasional, income, card_charge"})
	}
	if input.CardID.Set && kind != ItemCardCharge {
		violations = append(violations, cardOnlyOnChargeViolation)
	}
	item.Name, violations = checkName(input.Name, violations)
	item.Amount, violations = checkAmount(input.Amount, violations)
	if len(violations) > 0 {
		return NewItem{}, &ValidationError{Violations: violations}
	}
	return item, nil
}

// OptionalID is a nullable field of an edit: unset leaves it as it is, and a nil Value clears it.
type OptionalID struct {
	Set   bool
	Value *string
}

// ItemChangesInput is what an edit asks for, not yet validated. Kind and status never change here.
type ItemChangesInput struct {
	Name    *string
	Amount  *int64
	DueDate *string
	CardID  OptionalID
	TagID   OptionalID
}

// ItemChanges are the validated fields of an edit; unset fields stay as they are.
type ItemChanges struct {
	name    *string
	amount  *kernel.Money
	dueDate *time.Time
	cardID  OptionalID
	tagID   OptionalID
}

// NewItemChanges validates each value on its own. ApplyTo checks them against the item.
func NewItemChanges(input ItemChangesInput) (ItemChanges, error) {
	changes := ItemChanges{cardID: input.CardID, tagID: input.TagID}
	var violations []Violation
	if input.Name != nil {
		var checked string
		checked, violations = checkName(*input.Name, violations)
		changes.name = &checked
	}
	if input.Amount != nil {
		var checked kernel.Money
		checked, violations = checkAmount(*input.Amount, violations)
		changes.amount = &checked
	}
	if input.DueDate != nil {
		var checked time.Time
		checked, violations = checkDueDate(*input.DueDate, violations)
		changes.dueDate = &checked
	}
	if len(violations) > 0 {
		return ItemChanges{}, &ValidationError{Violations: violations}
	}
	return changes, nil
}

// ApplyTo returns the item with the changes made, or why they do not fit its kind.
func (c ItemChanges) ApplyTo(item Item) (Item, error) {
	switch {
	case item.Kind == ItemCardCharge && c.cardID.Set && c.cardID.Value == nil:
		return Item{}, &ValidationError{Violations: []Violation{{Field: "cardId", Message: "a card charge item cannot have its card removed"}}}
	case item.Kind != ItemCardCharge && c.cardID.Value != nil:
		return Item{}, &ValidationError{Violations: []Violation{cardOnlyOnChargeViolation}}
	case c.dueDate != nil && item.Kind == ItemIncome:
		return Item{}, &ValidationError{Violations: []Violation{incomeDueDateViolation}}
	case c.dueDate != nil && item.Kind == ItemCardCharge:
		return Item{}, &ValidationError{Violations: []Violation{cardChargeDueDateViolation}}
	}
	if c.name != nil {
		item.Name = *c.name
	}
	if c.amount != nil {
		item.Amount = *c.amount
	}
	if c.dueDate != nil {
		item.DueDate = c.dueDate
	}
	if c.cardID.Set && item.Kind == ItemCardCharge {
		item.CardID = c.cardID.Value
	}
	if c.tagID.Set {
		item.TagID = c.tagID.Value
	}
	return item, nil
}

func checkName(raw string, violations []Violation) (string, []Violation) {
	name := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(name)
	if length < 1 || length > nameMaxChars {
		return "", append(violations, Violation{Field: "name", Message: "must have between 1 and 120 characters"})
	}
	// PostgreSQL refuses NUL in TEXT, and no control character belongs in a name.
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", append(violations, Violation{Field: "name", Message: "must not contain control characters"})
	}
	return name, violations
}

func checkAmount(raw int64, violations []Violation) (kernel.Money, []Violation) {
	amount, err := kernel.NewMoney(raw)
	if err != nil || amount < 0 {
		return 0, append(violations, Violation{Field: "amount", Message: AmountOutOfRangeMessage})
	}
	return amount, violations
}

func checkRequiredDueDate(raw *string, violations []Violation) (*time.Time, []Violation) {
	if raw == nil {
		return nil, append(violations, Violation{Field: "dueDate", Message: "is required"})
	}
	date, violations := checkDueDate(*raw, violations)
	return &date, violations
}

func checkDueDate(raw string, violations []Violation) (time.Time, []Violation) {
	date, err := time.Parse(dateLayout, raw)
	if err != nil || date.Year() < firstDueYear {
		return time.Time{}, append(violations, Violation{Field: "dueDate", Message: dueDateMessage})
	}
	return date, violations
}
