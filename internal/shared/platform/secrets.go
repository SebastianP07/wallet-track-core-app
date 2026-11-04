package platform

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const minPepperBytes = 32

// AppConfig holds what any process touching auth data needs. Printing or logging it is never
// correct. Errors name the variable and never its value.
type AppConfig struct {
	DatabaseURL     string
	TOTPKey         []byte
	PreviousTOTPKey []byte
	PasswordPepper  []byte
}

func LoadAppConfig(getenv func(string) string) (AppConfig, error) {
	cfg := AppConfig{DatabaseURL: getenv("DATABASE_URL")}
	if cfg.DatabaseURL == "" {
		return AppConfig{}, errors.New("DATABASE_URL is not set")
	}
	var err error
	if cfg.TOTPKey, err = decodeBase64Env(getenv, "TOTP_ENC_KEY"); err != nil {
		return AppConfig{}, err
	}
	if cfg.TOTPKey == nil {
		return AppConfig{}, errors.New("TOTP_ENC_KEY is not set")
	}
	if cfg.PreviousTOTPKey, err = decodeBase64Env(getenv, "TOTP_ENC_KEY_PREVIOUS"); err != nil {
		return AppConfig{}, err
	}
	if cfg.PasswordPepper, err = decodeBase64Env(getenv, "PASSWORD_PEPPER"); err != nil {
		return AppConfig{}, err
	}
	if len(cfg.PasswordPepper) < minPepperBytes {
		return AppConfig{}, fmt.Errorf("PASSWORD_PEPPER: want at least %d bytes", minPepperBytes)
	}
	return cfg, nil
}

func decodeBase64Env(getenv func(string) string, name string) ([]byte, error) {
	raw := getenv(name)
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: not valid base64", name)
	}
	return decoded, nil
}

// EdgeConfig describes what the API trusts about its surroundings. Every default is the strict one.
type EdgeConfig struct {
	// TrustedProxies are the peers whose X-Forwarded-For is believed. Empty ignores the header.
	TrustedProxies []netip.Prefix
	// AllowedOrigins are the exact Origin values accepted on mutating requests.
	AllowedOrigins []string
	// InsecureDevCookies drops the __Host- prefix and Secure, for browsers on plain HTTP.
	InsecureDevCookies bool
}

func LoadEdgeConfig(getenv func(string) string) (EdgeConfig, error) {
	cfg := EdgeConfig{AllowedOrigins: splitList(getenv("ALLOWED_ORIGINS"))}
	for _, raw := range splitList(getenv("TRUSTED_PROXIES")) {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return EdgeConfig{}, fmt.Errorf("TRUSTED_PROXIES: %w", err)
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, prefix.Masked())
	}
	if raw := getenv("INSECURE_DEV_COOKIES"); raw != "" {
		insecure, err := strconv.ParseBool(raw)
		if err != nil {
			return EdgeConfig{}, fmt.Errorf("INSECURE_DEV_COOKIES: %w", err)
		}
		cfg.InsecureDevCookies = insecure
	}
	return cfg, nil
}

func splitList(raw string) []string {
	var values []string
	for value := range strings.SplitSeq(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}
