package domain

import "time"

const (
	secondsPerDay  = 24 * 60 * 60
	yellowDaysLeft = 10
	redDaysLeft    = 3
)

type Urgency string

const (
	UrgencyNone   Urgency = "none"
	UrgencyYellow Urgency = "yellow"
	UrgencyRed    Urgency = "red"
)

// UrgencyOf applies D11. Both dates are calendar dates carried at midnight UTC; today comes from
// kernel.DateInBogota. Seconds rather than time.Sub, whose Duration overflows past 292 years.
func UrgencyOf(status ItemStatus, dueDate *time.Time, today time.Time) Urgency {
	if status == StatusDone || dueDate == nil {
		return UrgencyNone
	}
	daysLeft := (dueDate.Unix() - today.Unix()) / secondsPerDay
	switch {
	case daysLeft <= redDaysLeft:
		return UrgencyRed
	case daysLeft <= yellowDaysLeft:
		return UrgencyYellow
	default:
		return UrgencyNone
	}
}
