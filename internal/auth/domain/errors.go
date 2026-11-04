package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidCredentials covers every rejected credential or session. Callers must not be
	// able to tell which check failed.
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrMFAAlreadyEnabled  = errors.New("MFA is already enabled")
	ErrOwnerExists        = errors.New("an owner already exists")
	ErrNotFound           = errors.New("not found")
	ErrInvalidEmail       = errors.New("email is not valid")
	ErrPasswordPolicy     = fmt.Errorf("password must have between %d and %d characters", PasswordMinLength, PasswordMaxLength)
)

type LockedError struct {
	RetryAfter time.Duration
}

func (e *LockedError) Error() string {
	return "account temporarily locked"
}
