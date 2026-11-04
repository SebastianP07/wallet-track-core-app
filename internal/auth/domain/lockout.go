package domain

import "time"

const (
	lockoutThreshold = 5
	lockoutBase      = time.Minute
	lockoutMax       = time.Hour
)

// LockoutFor returns how long to lock after the Nth consecutive failure:
// nothing below the threshold, then doubling from one minute up to one hour.
func LockoutFor(failedAttempts int) time.Duration {
	lockout := time.Duration(0)
	if failedAttempts >= lockoutThreshold {
		lockout = lockoutBase
	}
	for range failedAttempts - lockoutThreshold {
		lockout *= 2
		if lockout >= lockoutMax {
			return lockoutMax
		}
	}
	return lockout
}
