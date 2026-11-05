// Package kernel holds value objects shared by every module. It imports nothing
// from the outside.
package kernel

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

var (
	// ErrInvalidMoney is wrapped by every error ParseMoney returns.
	ErrInvalidMoney = errors.New("invalid money")
	// ErrMoneyOutOfRange means an amount or a sum lies beyond MaxMoney in either direction.
	ErrMoneyOutOfRange = errors.New("money out of range")
)

// Money is an amount of whole Colombian pesos.
type Money int64

// MaxMoney is 2^53-1, the largest integer a JSON number carries safely in a browser. The contract
// bounds every amount to it, in both directions.
const MaxMoney Money = 1<<53 - 1

const moneyGroupSize = 3

// NewMoney rejects amounts beyond MaxMoney in either direction.
func NewMoney(value int64) (Money, error) {
	if value > int64(MaxMoney) || value < -int64(MaxMoney) {
		return 0, fmt.Errorf("%w: %d", ErrMoneyOutOfRange, value)
	}
	return Money(value), nil
}

// SumMoney is exact at any length and in any order: only the final sum has to fit, so a large
// credit and a large debit may cancel out.
func SumMoney(amounts []Money) (Money, error) {
	sum := new(big.Int)
	for _, amount := range amounts {
		if _, err := NewMoney(int64(amount)); err != nil {
			return 0, err
		}
		sum.Add(sum, big.NewInt(int64(amount)))
	}
	if !sum.IsInt64() {
		return 0, fmt.Errorf("%w: sum %s", ErrMoneyOutOfRange, sum)
	}
	return NewMoney(sum.Int64())
}

// ParseMoney reads whole pesos with an optional leading minus and optional comma
// thousands separators: "1234", "1,234" and "-1,234,567". Separators must be
// complete and correctly placed. Dots are rejected rather than read as thousands
// separators, because "1.234" could equally be a decimal.
func ParseMoney(s string) (Money, error) {
	digits, negative := strings.CutPrefix(s, "-")
	plain, err := stripThousandsSeparators(digits)
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", ErrInvalidMoney, s, err)
	}
	if plain == "" || strings.Trim(plain, "0123456789") != "" {
		return 0, fmt.Errorf("%w: %q: expected whole pesos", ErrInvalidMoney, s)
	}
	if negative {
		plain = "-" + plain
	}
	value, err := strconv.ParseInt(plain, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", ErrInvalidMoney, s, err)
	}
	money, err := NewMoney(value)
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %w", ErrInvalidMoney, s, err)
	}
	return money, nil
}

func stripThousandsSeparators(digits string) (string, error) {
	if !strings.Contains(digits, ",") {
		return digits, nil
	}
	groups := strings.Split(digits, ",")
	first := groups[0]
	if first == "" || len(first) > moneyGroupSize || first[0] == '0' {
		return "", errors.New("misplaced thousands separator")
	}
	for _, group := range groups[1:] {
		if len(group) != moneyGroupSize {
			return "", errors.New("misplaced thousands separator")
		}
	}
	return strings.Join(groups, ""), nil
}

// String formats the amount with comma thousands separators, e.g. "-1,234,567".
func (m Money) String() string {
	digits, negative := strings.CutPrefix(strconv.FormatInt(int64(m), 10), "-")

	var formatted strings.Builder
	if negative {
		formatted.WriteByte('-')
	}
	lead := len(digits) % moneyGroupSize
	formatted.WriteString(digits[:lead])
	for i := lead; i < len(digits); i += moneyGroupSize {
		if i > 0 {
			formatted.WriteByte(',')
		}
		formatted.WriteString(digits[i : i+moneyGroupSize])
	}
	return formatted.String()
}
