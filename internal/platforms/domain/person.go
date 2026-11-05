// Package domain holds the rules of shared platforms: rounded shares (D10), what a person owes, and
// payments and the months they cover (D8).
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
	nameMaxChars     = 120
	billingMonthsMin = 1
	billingMonthsMax = 120
)

// BillingMonthsMessage is the one wording of the bound, also for values that never reach the domain.
const BillingMonthsMessage = "must be a whole number between 1 and 120"

var (
	ErrPersonNotFound   = errors.New("person not found")
	ErrPlatformNotFound = errors.New("platform not found")
	ErrPaymentNotFound  = errors.New("payment not found")
	// ErrOwnerExists means another person is already the owner.
	ErrOwnerExists = errors.New("another person is the owner")
	// ErrPersonHasPayments means the person's payments are kept as history, so the person stays too.
	ErrPersonHasPayments = errors.New("person has payments")
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
	return "platform values are not valid"
}

type Person struct {
	ID            string
	Name          string
	IsOwner       bool
	BillingMonths int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PersonInput is what a request asks for, not yet validated. On an edit, nil leaves a field as it is.
type PersonInput struct {
	Name          *string
	IsOwner       *bool
	BillingMonths *int
}

// NewPerson is a valid person that has not been stored yet.
type NewPerson struct {
	Name          string
	IsOwner       bool
	BillingMonths int
}

// CreateNewPerson validates a new person. The adapter has already answered for a missing name.
func CreateNewPerson(input PersonInput) (NewPerson, error) {
	changes, err := NewPersonChanges(input)
	if err != nil {
		return NewPerson{}, err
	}
	person := NewPerson{Name: *changes.Name, BillingMonths: 1}
	if changes.IsOwner != nil {
		person.IsOwner = *changes.IsOwner
	}
	if changes.BillingMonths != nil {
		person.BillingMonths = *changes.BillingMonths
	}
	return person, nil
}

// PersonChanges are the validated fields of an edit; nil leaves a field as it is.
type PersonChanges struct {
	Name          *string
	IsOwner       *bool
	BillingMonths *int
}

func NewPersonChanges(input PersonInput) (PersonChanges, error) {
	changes := PersonChanges{IsOwner: input.IsOwner, BillingMonths: input.BillingMonths}
	var violations []Violation
	changes.Name, violations = checkName(input.Name, violations)
	violations = checkMonths(input.BillingMonths, "billingMonths", violations)
	if len(violations) > 0 {
		return PersonChanges{}, &ValidationError{Violations: violations}
	}
	return changes, nil
}

func (c PersonChanges) ApplyTo(person Person) Person {
	if c.Name != nil {
		person.Name = *c.Name
	}
	if c.IsOwner != nil {
		person.IsOwner = *c.IsOwner
	}
	if c.BillingMonths != nil {
		person.BillingMonths = *c.BillingMonths
	}
	return person
}

func checkName(raw *string, violations []Violation) (*string, []Violation) {
	if raw == nil {
		return nil, violations
	}
	name := strings.TrimSpace(*raw)
	length := utf8.RuneCountInString(name)
	if length < 1 || length > nameMaxChars {
		return nil, append(violations, Violation{Field: "name", Message: "must have between 1 and 120 characters"})
	}
	// PostgreSQL refuses NUL in TEXT, and no control character belongs in a name.
	if strings.ContainsFunc(name, unicode.IsControl) {
		return nil, append(violations, Violation{Field: "name", Message: "must not contain control characters"})
	}
	return &name, violations
}

func checkMonths(months *int, field string, violations []Violation) []Violation {
	if months != nil && (*months < billingMonthsMin || *months > billingMonthsMax) {
		return append(violations, Violation{Field: field, Message: BillingMonthsMessage})
	}
	return violations
}

// memberItemPrefix names a member's income item (D4). The contract bounds an item's name to 120
// characters, as it does a person's, so a long name loses its tail here.
const memberItemPrefix = "Platforms — "

// MemberItemName is the name of a person's income item.
func MemberItemName(personName string) string {
	room := nameMaxChars - utf8.RuneCountInString(memberItemPrefix)
	runes := []rune(personName)
	if len(runes) > room {
		runes = runes[:room]
	}
	return memberItemPrefix + strings.TrimSpace(string(runes))
}

// OwedOutOfRange is what a write gets back when it would leave someone owing a sum the contract cannot
// carry, which would make every list of people answer 500.
func OwedOutOfRange() error {
	return &ValidationError{Violations: []Violation{{
		Field:   "monthlyCost",
		Message: "would take what someone owes beyond 9007199254740991",
	}}}
}

// MonthlyOwed is a person's sum of rounded shares (D10). The owner owes nothing, but still counts as a
// member in everyone else's share.
func MonthlyOwed(person Person, platforms []Platform) (kernel.Money, error) {
	if person.IsOwner {
		return 0, nil
	}
	var shares []kernel.Money
	for _, platform := range platforms {
		if platform.HasMember(person.ID) {
			shares = append(shares, platform.SharePerMember())
		}
	}
	return kernel.SumMoney(shares)
}
