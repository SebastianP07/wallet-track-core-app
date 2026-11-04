// Package crypto implements the cryptographic ports of the auth use cases.
package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP's minimum for Argon2id. The parameters travel with each hash, so they can grow later.
const (
	argonMemoryKiB   = 19 * 1024
	argonTime        = 2
	argonParallelism = 1
	argonSaltBytes   = 16
	argonKeyBytes    = 32

	passwordLabel     = "password\x00"
	recoveryCodeLabel = "recovery-code\x00"
)

// Hasher applies the pepper through HMAC, because x/crypto's Argon2 exposes no secret input.
// A database leak alone is therefore not enough to attack the hashes offline.
type Hasher struct {
	pepper []byte
}

func NewHasher(pepper []byte) *Hasher {
	return &Hasher{pepper: pepper}
}

func (h *Hasher) HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey(h.pepperedPassword(password), salt, argonTime, argonMemoryKiB, argonParallelism, argonKeyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (h *Hasher) VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, fmt.Errorf("parse password hash parameters: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode password hash salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode password hash: %w", err)
	}
	got := argon2.IDKey(h.pepperedPassword(password), salt, iterations, memory, parallelism, uint32(len(want))) //nolint:gosec // bounded by the stored hash.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// HashRecoveryCode uses HMAC rather than Argon2 because the codes are random 80-bit values,
// and a deterministic hash lets a single UPDATE both find and consume one atomically.
func (h *Hasher) HashRecoveryCode(code string) string {
	return hex.EncodeToString(h.mac(recoveryCodeLabel, code))
}

func (h *Hasher) pepperedPassword(password string) []byte {
	return h.mac(passwordLabel, password)
}

func (h *Hasher) mac(label, value string) []byte {
	mac := hmac.New(sha256.New, h.pepper)
	mac.Write([]byte(label))
	mac.Write([]byte(value))
	return mac.Sum(nil)
}
