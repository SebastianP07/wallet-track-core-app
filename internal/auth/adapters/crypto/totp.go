package crypto

import (
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
)

const (
	totpIssuer     = "Wallet Track"
	totpPeriod     = 30
	totpSkewSteps  = 1
	totpSecretSize = 20
)

type TOTP struct{}

func (TOTP) Enroll(accountName string) (application.TOTPEnrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: accountName,
		Period:      totpPeriod,
		SecretSize:  totpSecretSize,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return application.TOTPEnrollment{}, fmt.Errorf("generate key: %w", err)
	}
	return application.TOTPEnrollment{Secret: key.Secret(), OTPAuthURI: key.URL()}, nil
}

// Match checks the current step and one on either side, for clock drift. The library's own
// validator does not say which step matched, and replay protection needs it.
func (TOTP) Match(secret, code string, now time.Time) (int64, bool, error) {
	options := totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	current := now.Unix() / totpPeriod
	matched := int64(0)
	found := 0
	for offset := int64(-totpSkewSteps); offset <= totpSkewSteps; offset++ {
		step := current + offset
		candidate, err := totp.GenerateCodeCustom(secret, time.Unix(step*totpPeriod, 0).UTC(), options)
		if err != nil {
			return 0, false, fmt.Errorf("generate code: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			matched = step
			found = 1
		}
	}
	return matched, found == 1, nil
}
