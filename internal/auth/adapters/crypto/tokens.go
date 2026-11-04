package crypto

import (
	"crypto/rand"
	"time"
)

const (
	recoveryGroups    = 4
	recoveryGroupSize = 4
)

type Tokens struct{}

// Token returns two rand.Text values, 260 random bits in total, for a 256-bit minimum.
func (Tokens) Token() string {
	return rand.Text() + rand.Text()
}

// RecoveryCode returns 80 random bits of base32, as XXXX-XXXX-XXXX-XXXX.
func (Tokens) RecoveryCode() string {
	text := rand.Text()
	code := make([]byte, 0, recoveryGroups*(recoveryGroupSize+1))
	for group := range recoveryGroups {
		if group > 0 {
			code = append(code, '-')
		}
		code = append(code, text[group*recoveryGroupSize:(group+1)*recoveryGroupSize]...)
	}
	return string(code)
}

// Clock is the system clock.
type Clock struct{}

func (Clock) Now() time.Time {
	return time.Now()
}
