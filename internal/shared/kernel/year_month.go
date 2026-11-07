package kernel

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// The contract's YearMonth, which also keeps the year inside the range the periods table accepts.
var yearMonthPattern = regexp.MustCompile(`^[2-9][0-9]{3}-(0[1-9]|1[0-2])$`)

// YearMonth is one calendar month.
type YearMonth struct {
	Year  int
	Month time.Month
}

// ParseYearMonth reads "YYYY-MM", from the year 2000 on.
func ParseYearMonth(s string) (YearMonth, error) {
	if !yearMonthPattern.MatchString(s) {
		return YearMonth{}, fmt.Errorf("invalid month %q", s)
	}
	year, err := strconv.Atoi(s[:4])
	if err != nil {
		return YearMonth{}, fmt.Errorf("invalid month %q: %w", s, err)
	}
	month, err := strconv.Atoi(s[5:])
	if err != nil {
		return YearMonth{}, fmt.Errorf("invalid month %q: %w", s, err)
	}
	return YearMonth{Year: year, Month: time.Month(month)}, nil
}

func (m YearMonth) String() string {
	return fmt.Sprintf("%04d-%02d", m.Year, int(m.Month))
}

// DateOn is that day of the month, or the month's last day when it has fewer days (D11).
// The result is a calendar date, carried at midnight UTC.
func (m YearMonth) DateOn(day int) time.Time {
	lastDay := time.Date(m.Year, m.Month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(m.Year, m.Month, min(day, lastDay), 0, 0, 0, 0, time.UTC)
}

// Next is the calendar month after this one.
func (m YearMonth) Next() YearMonth {
	if m.Month == time.December {
		return YearMonth{Year: m.Year + 1, Month: time.January}
	}
	return YearMonth{Year: m.Year, Month: m.Month + 1}
}

// FirstMonthNotClosed is from, or the first month after it that isClosed says no to (D7). A month not
// opened yet is not closed. Callers lock each month as they ask, in ascending order, so two closes
// cannot strand an item in a month that closed meanwhile.
func FirstMonthNotClosed(from YearMonth, isClosed func(YearMonth) (bool, error)) (YearMonth, error) {
	month := from
	for {
		closed, err := isClosed(month)
		if err != nil || !closed {
			return month, err
		}
		month = month.Next()
	}
}
