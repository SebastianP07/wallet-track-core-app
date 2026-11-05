// Package domain holds the rules of accounts and savings (D6, D9).
package domain

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/kernel"
)

const nameMaxChars = 120

var (
	ErrNotFound = errors.New("account not found")
	// ErrTotalOutOfRange means a change would take Total actual or Ahorro beyond kernel.MaxMoney.
	ErrTotalOutOfRange = errors.New("account total out of range")
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
	return "account values are not valid"
}

type Kind string

const (
	KindBank     Kind = "bank"
	KindWallet   Kind = "wallet"
	KindCash     Kind = "cash"
	KindFoodCard Kind = "food_card"
	KindSavings  Kind = "savings"
)

func parseKind(raw string) (Kind, bool) {
	switch kind := Kind(raw); kind {
	case KindBank, KindWallet, KindCash, KindFoodCard, KindSavings:
		return kind, true
	default:
		return "", false
	}
}

type Account struct {
	ID        string
	Name      string
	Kind      Kind
	Balance   kernel.Money
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewAccount is a valid account that has not been stored yet.
type NewAccount struct {
	Name    string
	Kind    Kind
	Balance kernel.Money
}

// CreateNewAccount validates the values of a new account.
func CreateNewAccount(name, kind string, balance int64) (NewAccount, error) {
	var (
		account    NewAccount
		violations []Violation
	)
	account.Name, violations = checkName(name, violations)
	account.Kind, violations = checkKind(kind, violations)
	account.Balance, violations = checkBalance(balance, violations)
	if len(violations) > 0 {
		return NewAccount{}, &ValidationError{Violations: violations}
	}
	return account, nil
}

// Changes are the validated fields of an edit; nil leaves a field as it is.
type Changes struct {
	name    *string
	kind    *Kind
	balance *kernel.Money
}

// NewChanges validates an edit. The balance is only ever typed by the user (D6).
func NewChanges(name, kind *string, balance *int64) (Changes, error) {
	var (
		changes    Changes
		violations []Violation
	)
	if name != nil {
		var checked string
		checked, violations = checkName(*name, violations)
		changes.name = &checked
	}
	if kind != nil {
		var checked Kind
		checked, violations = checkKind(*kind, violations)
		changes.kind = &checked
	}
	if balance != nil {
		var checked kernel.Money
		checked, violations = checkBalance(*balance, violations)
		changes.balance = &checked
	}
	if len(violations) > 0 {
		return Changes{}, &ValidationError{Violations: violations}
	}
	return changes, nil
}

// ApplyTo returns the account with the changes made.
func (c Changes) ApplyTo(account Account) Account {
	if c.name != nil {
		account.Name = *c.name
	}
	if c.kind != nil {
		account.Kind = *c.kind
	}
	if c.balance != nil {
		account.Balance = *c.balance
	}
	return account
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

func checkKind(raw string, violations []Violation) (Kind, []Violation) {
	kind, ok := parseKind(raw)
	if !ok {
		return "", append(violations, Violation{Field: "kind", Message: "must be one of bank, wallet, cash, food_card, savings"})
	}
	return kind, violations
}

// BalanceOutOfRangeMessage is the one wording of the bound, also for values too large to reach the domain.
const BalanceOutOfRangeMessage = "must be whole pesos between -9007199254740991 and 9007199254740991"

func checkBalance(raw int64, violations []Violation) (kernel.Money, []Violation) {
	balance, err := kernel.NewMoney(raw)
	if err != nil {
		return 0, append(violations, Violation{Field: "balance", Message: BalanceOutOfRangeMessage})
	}
	return balance, violations
}

// Totals are the two sums the backend owes the frontend (D1).
type Totals struct {
	// Current is Total actual: the sum of non-savings balances (D6, D9).
	Current kernel.Money
	// Savings is Ahorro: the sum of savings balances (D9).
	Savings kernel.Money
}

// ComputeTotals returns ErrTotalOutOfRange rather than a number the contract cannot carry.
func ComputeTotals(accounts []Account) (Totals, error) {
	var current, savings []kernel.Money
	for _, account := range accounts {
		if account.Kind == KindSavings {
			savings = append(savings, account.Balance)
		} else {
			current = append(current, account.Balance)
		}
	}
	currentTotal, err := kernel.SumMoney(current)
	if err != nil {
		return Totals{}, errors.Join(ErrTotalOutOfRange, err)
	}
	savingsTotal, err := kernel.SumMoney(savings)
	if err != nil {
		return Totals{}, errors.Join(ErrTotalOutOfRange, err)
	}
	return Totals{Current: currentTotal, Savings: savingsTotal}, nil
}
