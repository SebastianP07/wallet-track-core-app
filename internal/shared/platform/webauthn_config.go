package platform

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const localRPHost = "localhost"

// WebAuthnConfig names the relying party that passkeys are bound to. There is no default: a credential
// registered under the wrong RP ID would be useless once corrected.
type WebAuthnConfig struct {
	RPID      string
	RPOrigins []string
}

// LoadWebAuthnConfig refuses anything a browser would refuse later, so the mistake shows at startup
// rather than when the owner first tries a passkey.
func LoadWebAuthnConfig(getenv func(string) string) (WebAuthnConfig, error) {
	rpID := getenv("WEBAUTHN_RP_ID")
	if rpID == "" {
		return WebAuthnConfig{}, errors.New("WEBAUTHN_RP_ID is not set")
	}
	if err := validateRPID(rpID); err != nil {
		return WebAuthnConfig{}, fmt.Errorf("WEBAUTHN_RP_ID: %w", err)
	}
	origins := splitList(getenv("WEBAUTHN_RP_ORIGINS"))
	if len(origins) == 0 {
		return WebAuthnConfig{}, errors.New("WEBAUTHN_RP_ORIGINS is not set")
	}
	for _, origin := range origins {
		if err := validateRPOrigin(origin, rpID); err != nil {
			return WebAuthnConfig{}, fmt.Errorf("WEBAUTHN_RP_ORIGINS: %w", err)
		}
	}
	return WebAuthnConfig{RPID: rpID, RPOrigins: origins}, nil
}

func validateRPID(rpID string) error {
	if rpID != strings.ToLower(rpID) || strings.ContainsAny(rpID, ":/ ") {
		return fmt.Errorf("%q must be a lowercase host name, with no scheme or port", rpID)
	}
	if net.ParseIP(rpID) != nil {
		return fmt.Errorf("%q is an IP address, which WebAuthn does not allow", rpID)
	}
	if rpID != localRPHost && !strings.Contains(rpID, ".") {
		return fmt.Errorf("%q is a single label, and only %s may be one", rpID, localRPHost)
	}
	return nil
}

func validateRPOrigin(origin, rpID string) error {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || origin != parsed.Scheme+"://"+parsed.Host {
		return fmt.Errorf("%q is not an exact origin, like https://example.com", origin)
	}
	host := parsed.Hostname()
	if host != rpID && !strings.HasSuffix(host, "."+rpID) {
		return fmt.Errorf("origin %q does not belong to the RP ID %q", origin, rpID)
	}
	switch {
	case parsed.Scheme == "https":
		return nil
	case parsed.Scheme == "http" && host == localRPHost:
		return nil
	default:
		return fmt.Errorf("origin %q must use https unless its host is %s", origin, localRPHost)
	}
}
