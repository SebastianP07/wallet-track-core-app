package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// PasskeyChallengeLifetime is also the time the browser gets to finish a ceremony.
	PasskeyChallengeLifetime = 2 * time.Minute

	passkeyNameMaxLength = 120
)

type ChallengePurpose string

const (
	ChallengeLogin        ChallengePurpose = "login"
	ChallengeRegistration ChallengePurpose = "registration"
)

// Passkey is a registered WebAuthn credential, holding only what verifying an assertion needs.
type Passkey struct {
	ID             string
	Name           string
	CredentialID   []byte
	PublicKey      []byte
	Transports     []string
	SignCount      uint32
	BackupEligible bool
	BackupState    bool
	LastUsedAt     time.Time
	CreatedAt      time.Time
}

// NormalizePasskeyName returns the name to store, the owner's own label for a device.
func NormalizePasskeyName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(name)
	if length < 1 || length > passkeyNameMaxLength || strings.ContainsFunc(name, unicode.IsControl) {
		return "", ErrInvalidPasskeyName
	}
	return name, nil
}
