package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

const (
	nameMaxChars = 120
	dueDayMin    = 1
	dueDayMax    = 31
)

// AmountOutOfRangeMessage is the one wording of the bound, also for values too large to reach the domain.
const AmountOutOfRangeMessage = "must be whole pesos between 0 and 9007199254740991"

// DueDayOutOfRangeMessage is the one wording of the bound, also for values too large to reach the domain.
const DueDayOutOfRangeMessage = "must be a whole number between 1 and 31"

const (
	dueDayOnlyOnFixedMessage = "only fixed templates have a due day"
	cardOnlyOnChargeMessage  = "only card charge templates have a card"
	activeChargeCardMessage  = "an active card charge template must have a card"
)

type TemplateKind string

const (
	TemplateFixed      TemplateKind = "fixed"
	TemplateIncome     TemplateKind = "income"
	TemplateCardCharge TemplateKind = "card_charge"
)

func parseTemplateKind(raw string) (TemplateKind, bool) {
	switch kind := TemplateKind(raw); kind {
	case TemplateFixed, TemplateIncome, TemplateCardCharge:
		return kind, true
	default:
		return "", false
	}
}

type Template struct {
	ID        string
	Kind      TemplateKind
	Name      string
	Amount    kernel.Money
	DueDay    *int
	CardID    *string
	TagID     *string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTemplateInput is what a request asks for, not yet validated.
type NewTemplateInput struct {
	Kind   string
	Name   string
	Amount int64
	DueDay *int
	CardID *string
	TagID  *string
	Active *bool
}

// NewTemplate is a valid template that has not been stored yet.
type NewTemplate struct {
	Kind   TemplateKind
	Name   string
	Amount kernel.Money
	DueDay *int
	CardID *string
	TagID  *string
	Active bool
}

// CreateNewTemplate validates a new template. An absent active means true.
func CreateNewTemplate(input NewTemplateInput) (NewTemplate, error) {
	var (
		template   = NewTemplate{DueDay: input.DueDay, CardID: input.CardID, TagID: input.TagID, Active: true}
		violations []Violation
		kindOK     bool
	)
	if input.Active != nil {
		template.Active = *input.Active
	}
	template.Name, violations = checkName(input.Name, violations)
	template.Amount, violations = checkAmount(input.Amount, violations)
	violations = checkDueDay(input.DueDay, violations)

	template.Kind, kindOK = parseTemplateKind(input.Kind)
	switch {
	case !kindOK:
		violations = append(violations, Violation{Field: "kind", Message: "must be one of fixed, income, card_charge"})
	case template.Kind == TemplateFixed && input.DueDay == nil:
		violations = append(violations, Violation{Field: "dueDay", Message: "is required on a fixed template"})
	case template.Kind == TemplateCardCharge && input.CardID == nil:
		violations = append(violations, Violation{Field: "cardId", Message: "is required on a card charge template"})
	}
	if kindOK {
		violations = checkKindOwnsFields(template.Kind, input.DueDay, input.CardID, violations)
	}
	if len(violations) > 0 {
		return NewTemplate{}, &ValidationError{Violations: violations}
	}
	return template, nil
}

// OptionalID is a nullable field of an edit: unset leaves it as it is, and a nil Value clears it.
type OptionalID struct {
	Set   bool
	Value *string
}

// TemplateChangesInput is what an edit asks for, not yet validated. The kind never changes.
type TemplateChangesInput struct {
	Name   *string
	Amount *int64
	DueDay *int
	CardID OptionalID
	TagID  OptionalID
	Active *bool
}

// TemplateChanges are the validated fields of an edit; unset fields stay as they are.
type TemplateChanges struct {
	name   *string
	amount *kernel.Money
	dueDay *int
	cardID OptionalID
	tagID  OptionalID
	active *bool
}

// NewTemplateChanges validates each value on its own. ApplyTo checks them against the template.
func NewTemplateChanges(input TemplateChangesInput) (TemplateChanges, error) {
	changes := TemplateChanges{dueDay: input.DueDay, cardID: input.CardID, tagID: input.TagID, active: input.Active}
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
	violations = checkDueDay(input.DueDay, violations)
	if len(violations) > 0 {
		return TemplateChanges{}, &ValidationError{Violations: violations}
	}
	return changes, nil
}

// ApplyTo returns the template with the changes made, or why they do not fit its kind.
func (c TemplateChanges) ApplyTo(template Template) (Template, error) {
	violations := checkKindOwnsFields(template.Kind, c.dueDay, c.cardID.Value, nil)
	if c.name != nil {
		template.Name = *c.name
	}
	if c.amount != nil {
		template.Amount = *c.amount
	}
	if c.dueDay != nil {
		template.DueDay = c.dueDay
	}
	if c.cardID.Set {
		template.CardID = c.cardID.Value
	}
	if c.tagID.Set {
		template.TagID = c.tagID.Value
	}
	if c.active != nil {
		template.Active = *c.active
	}
	if template.Kind == TemplateCardCharge && template.Active && template.CardID == nil {
		violations = append(violations, Violation{Field: "cardId", Message: activeChargeCardMessage})
	}
	if len(violations) > 0 {
		return Template{}, &ValidationError{Violations: violations}
	}
	return template, nil
}

// TotalsOutOfRange is what opening a month gets back when its items would add up past the bound.
func TotalsOutOfRange() error {
	return &ValidationError{Violations: []Violation{{Field: "period", Message: "active templates and platform members add up to more than 9007199254740991"}}}
}

// CheckOpeningTotals keeps a month from opening with a total the contract cannot carry: every list of
// that month would answer 500, with no way to reach its items and fix them.
func CheckOpeningTotals(templates []Template, owing []OwingMember) error {
	amounts := make(map[TemplateKind][]kernel.Money)
	for _, template := range templates {
		amounts[template.Kind] = append(amounts[template.Kind], template.Amount)
	}
	for _, member := range owing {
		amounts[TemplateIncome] = append(amounts[TemplateIncome], member.Amount)
	}
	for _, kindAmounts := range amounts {
		if _, err := kernel.SumMoney(kindAmounts); err != nil {
			return TotalsOutOfRange()
		}
	}
	return nil
}

func checkKindOwnsFields(kind TemplateKind, dueDay *int, cardID *string, violations []Violation) []Violation {
	if dueDay != nil && kind != TemplateFixed {
		violations = append(violations, Violation{Field: "dueDay", Message: dueDayOnlyOnFixedMessage})
	}
	if cardID != nil && kind != TemplateCardCharge {
		violations = append(violations, Violation{Field: "cardId", Message: cardOnlyOnChargeMessage})
	}
	return violations
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

func checkDueDay(dueDay *int, violations []Violation) []Violation {
	if dueDay != nil && (*dueDay < dueDayMin || *dueDay > dueDayMax) {
		return append(violations, Violation{Field: "dueDay", Message: DueDayOutOfRangeMessage})
	}
	return violations
}
