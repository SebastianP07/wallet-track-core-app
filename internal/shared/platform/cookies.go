package platform

import (
	"net/http"
	"time"
)

const (
	secureCookiePrefix   = "__Host-"
	insecureCookiePrefix = "dev-"
)

// CookiePolicy is the one place that decides cookie names and attributes.
type CookiePolicy struct {
	insecure bool
}

func NewCookiePolicy(insecure bool) CookiePolicy {
	return CookiePolicy{insecure: insecure}
}

// Name returns the full name of a cookie. Browsers refuse the __Host- prefix without Secure.
func (p CookiePolicy) Name(base string) string {
	if p.insecure {
		return insecureCookiePrefix + base
	}
	return secureCookiePrefix + base
}

func (p CookiePolicy) Set(w http.ResponseWriter, base, value string, lifetime time.Duration) {
	http.SetCookie(w, p.cookie(base, value, int(lifetime/time.Second)))
}

func (p CookiePolicy) Clear(w http.ResponseWriter, base string) {
	http.SetCookie(w, p.cookie(base, "", -1))
}

func (p CookiePolicy) cookie(base, value string, maxAge int) *http.Cookie {
	//nolint:gosec // Secure is on unless the operator opts out with INSECURE_DEV_COOKIES.
	return &http.Cookie{
		Name:     p.Name(base),
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !p.insecure,
		SameSite: http.SameSiteLaxMode,
	}
}
