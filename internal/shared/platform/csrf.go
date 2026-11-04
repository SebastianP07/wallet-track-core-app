package platform

import (
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"time"
)

const (
	csrfCookie   = "csrf"
	csrfHeader   = "X-CSRF-Token"
	csrfLifetime = 30 * 24 * time.Hour
)

// CSRF implements double-submit: the token in the header must match the one in the cookie,
// and the __Host- prefix keeps a sibling subdomain from planting that cookie.
type CSRF struct {
	cookies CookiePolicy
	origins map[string]struct{}
}

func NewCSRF(cookies CookiePolicy, allowedOrigins []string) *CSRF {
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origins[origin] = struct{}{}
	}
	return &CSRF{cookies: cookies, origins: origins}
}

// Protect rejects a mutating request without a matching token, or from an origin not listed.
// A request with no Origin is not a cross-site browser request, so the token check decides it.
func (c *CSRF) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if _, allowed := c.origins[origin]; !allowed {
				WriteProblem(w, http.StatusForbidden, "Request rejected.")
				return
			}
		}
		cookie, err := r.Cookie(c.cookies.Name(csrfCookie))
		header := r.Header.Get(csrfHeader)
		if err != nil || cookie.Value == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(header)) != 1 {
			WriteProblem(w, http.StatusForbidden, "Request rejected.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type csrfTokenResponse struct {
	CSRFToken string `json:"csrfToken"`
}

// Issue hands out a token. A token already in the cookie is kept, so that a second tab
// asking for one does not invalidate the first tab's.
func (c *CSRF) Issue(w http.ResponseWriter, r *http.Request) {
	token := rand.Text()
	if cookie, err := r.Cookie(c.cookies.Name(csrfCookie)); err == nil && len(cookie.Value) == len(token) {
		token = cookie.Value
	}
	c.cookies.Set(w, csrfCookie, token, csrfLifetime)
	WriteJSON(w, http.StatusOK, csrfTokenResponse{CSRFToken: token})
}
