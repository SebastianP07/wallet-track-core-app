// Package kernel holds value objects shared by every module. It imports nothing
// from the outside.
package kernel

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidMoney is wrapped by every error ParseMoney returns.
var ErrInvalidMoney = errors.New("invalid money")

// Money is an amount of whole Colombian pesos.
type Money int64

const moneyGroupSize = 3

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
	return Money(value), nil
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
