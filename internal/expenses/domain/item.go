// Package domain holds the rules of a month's expense and income items: urgency (D11), Por pagar and
// Por llegar (D1) and the done toggle (D7). Card charge rows are read and totaled, but Phase 8 owns
// writing them.
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

var incomeDueDateViolation = Violation{Field: "dueDate", Message: "income items have no due date"}

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

func isExpense(kind ItemKind) bool {
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
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// RequireWritable keeps card charge rows out of reach until Phase 8.
func (i Item) RequireWritable() error {
	if i.Kind != ItemCardCharge {
		return nil
	}
	return &ValidationError{Violations: []Violation{{Field: "kind", Message: "only fixed, occasional and income items can be changed here"}}}
}

// Done is idempotent: an item already done keeps the moment it was first done. Never touches a
// balance (D6), and an overdue item stays in its month (D7).
func (i Item) Done(now time.Time) Item {
	if i.Status == StatusDone {
		return i
	}
	i.Status = StatusDone
	i.DoneAt = &now
	return i
}

// Undone is idempotent: only a done item changes. In a closed month an expense or income item goes
// back to overdue, since it is what closing would have made of it; a card charge never is (D3).
func (i Item) Undone(period Period) Item {
	if i.Status != StatusDone {
		return i
	}
	i.DoneAt = nil
	i.Status = StatusPending
	if period.Status == PeriodClosed && i.Kind != ItemCardCharge {
		i.Status = StatusOverdue
	}
	return i
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
	TagID   *string
}

// NewItem is a valid pending item that has not been stored yet. Income has no DueDate.
type NewItem struct {
	Kind    ItemKind
	Name    string
	Amount  kernel.Money
	DueDate *time.Time
	TagID   *string
}

// CreateNewItem validates a new item. Card charges are refused until Phase 8.
func CreateNewItem(input NewItemInput) (NewItem, error) {
	var violations []Violation
	item := NewItem{TagID: input.TagID}
	kind := ItemKind(input.Kind)
	switch {
	case isExpense(kind):
		item.Kind = kind
		item.DueDate, violations = checkRequiredDueDate(input.DueDate, violations)
	case kind == ItemIncome:
		item.Kind = kind
		if input.DueDate != nil {
			violations = append(violations, incomeDueDateViolation)
		}
	default:
		violations = append(violations, Violation{Field: "kind", Message: "must be fixed, occasional or income"})
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
	if c.cardID.Value != nil {
		return Item{}, &ValidationError{Violations: []Violation{{Field: "cardId", Message: "only card charge items have a card"}}}
	}
	if c.dueDate != nil && item.Kind == ItemIncome {
		return Item{}, &ValidationError{Violations: []Violation{incomeDueDateViolation}}
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
