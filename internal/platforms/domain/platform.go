package domain

import (
	"slices"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

// MoneyMessage is the one wording of the bound, also for values too large to reach the domain.
const MoneyMessage = "must be whole pesos between 0 and 9007199254740991"

type Member struct {
	PersonID string
	Name     string
}

type Platform struct {
	ID          string
	Name        string
	MonthlyCost kernel.Money
	CardID      *string
	Members     []Member
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (p Platform) HasMember(personID string) bool {
	return slices.ContainsFunc(p.Members, func(member Member) bool { return member.PersonID == personID })
}

// SharePerMember is the cost split among every member, the owner included, rounded up to the whole
// peso in integer arithmetic (D10). A platform without members has no share.
func (p Platform) SharePerMember() kernel.Money {
	members := kernel.Money(len(p.Members))
	if members == 0 {
		return 0
	}
	share := p.MonthlyCost / members
	if p.MonthlyCost%members != 0 {
		share++
	}
	return share
}

// OptionalCard tells a card left out of an edit from one set to null.
type OptionalCard struct {
	Set   bool
	Value *string
}

// PlatformInput is what a request asks for, not yet validated. On an edit, nil leaves a field as it is.
type PlatformInput struct {
	Name        *string
	MonthlyCost *int64
	CardID      OptionalCard
}

// NewPlatform is a valid platform that has not been stored yet.
type NewPlatform struct {
	Name        string
	MonthlyCost kernel.Money
	CardID      *string
}

// CreateNewPlatform validates a new platform. The adapter has already answered for a missing field.
func CreateNewPlatform(input PlatformInput) (NewPlatform, error) {
	changes, err := NewPlatformChanges(input)
	if err != nil {
		return NewPlatform{}, err
	}
	return NewPlatform{Name: *changes.name, MonthlyCost: *changes.monthlyCost, CardID: changes.cardID.Value}, nil
}

// PlatformChanges are the validated fields of an edit.
type PlatformChanges struct {
	name        *string
	monthlyCost *kernel.Money
	cardID      OptionalCard
}

func NewPlatformChanges(input PlatformInput) (PlatformChanges, error) {
	changes := PlatformChanges{cardID: input.CardID}
	var violations []Violation
	changes.name, violations = checkName(input.Name, violations)
	if input.MonthlyCost != nil {
		cost, err := kernel.NewMoney(*input.MonthlyCost)
		if err != nil || cost < 0 {
			violations = append(violations, Violation{Field: "monthlyCost", Message: MoneyMessage})
		} else {
			changes.monthlyCost = &cost
		}
	}
	if len(violations) > 0 {
		return PlatformChanges{}, &ValidationError{Violations: violations}
	}
	return changes, nil
}

func (c PlatformChanges) ApplyTo(platform Platform) Platform {
	if c.name != nil {
		platform.Name = *c.name
	}
	if c.monthlyCost != nil {
		platform.MonthlyCost = *c.monthlyCost
	}
	if c.cardID.Set {
		platform.CardID = c.cardID.Value
	}
	return platform
}

// CardChargesOutOfRange is what a write gets back when it would take an open month's card charges to a sum the
// contract cannot carry (D14).
func CardChargesOutOfRange() error {
	return &ValidationError{Violations: []Violation{{
		Field:   "monthlyCost",
		Message: "would take an open month's card charges beyond 9007199254740991",
	}}}
}
