// Package domain holds the rules of authentication, free of any infrastructure.
package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	PasswordMinLength = 12
	PasswordMaxLength = 1024

	EnrollmentSecretLifetime = 24 * time.Hour

	emailMaxLength = 254
)

type User struct {
	ID                        string
	Email                     string
	PasswordHash              string
	TOTPSecretEnc             []byte
	TOTPKeyID                 string
	MFAEnabled                bool
	EnrollmentSecretHash      []byte
	EnrollmentSecretExpiresAt time.Time
	LockedUntil               time.Time
}

// LockedFor reports how long the account stays locked at now.
func (u User) LockedFor(now time.Time) (time.Duration, bool) {
	if now.Before(u.LockedUntil) {
		return u.LockedUntil.Sub(now), true
	}
	return 0, false
}

func (u User) EnrollmentOpen(now time.Time) bool {
	return len(u.EnrollmentSecretHash) > 0 && now.Before(u.EnrollmentSecretExpiresAt)
}

func (u User) AcceptsEnrollmentSecret(secret string, now time.Time) bool {
	return u.EnrollmentOpen(now) && subtle.ConstantTimeCompare(u.EnrollmentSecretHash, HashToken(secret)) == 1
}

// NormalizeEmail returns the canonical form under which an email is stored, tracked and rate limited.
func NormalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > emailMaxLength {
		return "", ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < PasswordMinLength || length > PasswordMaxLength {
		return ErrPasswordPolicy
	}
	return nil
}

// HashToken is the stored form of a high-entropy token: a fast hash is enough because
// the input cannot be brute forced, and it lets the token be looked up directly.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NormalizeRecoveryCode lets the owner type a code in any case, with or without its separators.
func NormalizeRecoveryCode(raw string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(raw))
}
