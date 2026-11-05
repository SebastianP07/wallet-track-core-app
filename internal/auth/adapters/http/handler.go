// Package http is the inbound HTTP adapter of the auth module.
package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/application"
	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const (
	basePath      = "/api/v1"
	sessionCookie = "session"

	genericUnauthorized = "Authentication failed."
)

var totpCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

// UseCases are the operations this adapter exposes.
type UseCases struct {
	Authenticate    *application.AuthenticateSession
	LoginPassword   *application.LoginWithPassword
	LoginTOTP       *application.LoginWithTOTP
	LoginRecovery   *application.LoginWithRecoveryCode
	StartEnrollment *application.StartMFAEnrollment
	ConfirmEnroll   *application.ConfirmMFAEnrollment
	RecoveryStatus  *application.GetRecoveryCodeStatus
	RegenerateCodes *application.RegenerateRecoveryCodes
	Logout          *application.Logout
	LogoutAll       *application.LogoutAll
}

// Limits are the rate limiters of the auth routes.
type Limits struct {
	// CredentialsByIP guards every route that checks a credential.
	CredentialsByIP *platform.TokenBucket
	// CSRFByIP guards token issuing, apart, so that fetching a token never costs a login attempt.
	CSRFByIP *platform.TokenBucket
	// ByAccount is keyed by user ID and guards only steps behind the password. Keying the password
	// step by email would let anyone drain the owner's bucket.
	ByAccount *platform.TokenBucket
}

type Config struct {
	UseCases UseCases
	Limits   Limits
	Cookies  platform.CookiePolicy
	CSRF     *platform.CSRF
	ClientIP platform.ClientIPResolver
	Logger   *slog.Logger
}

type Handler struct {
	Config
}

func NewHandler(cfg Config) *Handler {
	return &Handler{Config: cfg}
}

// Register adds the auth routes of the contract, each with exactly the checks it needs.
func (h *Handler) Register(mux *http.ServeMux) {
	limited := platform.LimitByIP(h.Limits.CredentialsByIP, h.ClientIP)
	csrfLimited := platform.LimitByIP(h.Limits.CSRFByIP, h.ClientIP)

	route := func(pattern string, handler http.HandlerFunc, middlewares ...platform.Middleware) {
		mux.Handle(pattern, platform.Chain(handler, middlewares...))
	}

	route("GET "+basePath+"/auth/csrf", h.CSRF.Issue, csrfLimited)
	route("POST "+basePath+"/auth/login/password", h.loginWithPassword, limited, h.CSRF.Protect)
	route("POST "+basePath+"/auth/login/totp", h.loginWithTOTP, limited, h.CSRF.Protect, h.requirePendingSession)
	route("POST "+basePath+"/auth/login/recovery-code", h.loginWithRecoveryCode, limited, h.CSRF.Protect, h.requirePendingSession)
	// Enrollment accepts any session so that a full one gets the contract's 409, not a 401.
	route("POST "+basePath+"/auth/mfa/enrollment", h.startMFAEnrollment, limited, h.CSRF.Protect, h.requireAnySession)
	route("POST "+basePath+"/auth/mfa/enrollment/confirm", h.confirmMFAEnrollment, limited, h.CSRF.Protect, h.requireAnySession)
	route("GET "+basePath+"/auth/recovery-codes", h.getRecoveryCodeStatus, h.RequireFullSession)
	route("POST "+basePath+"/auth/recovery-codes/regenerate", h.regenerateRecoveryCodes, limited, h.CSRF.Protect, h.RequireFullSession)
	route("POST "+basePath+"/auth/logout", h.logout, h.CSRF.Protect, h.RequireFullSession)
	route("POST "+basePath+"/auth/logout-all", h.logoutAll, h.CSRF.Protect, h.RequireFullSession)
	route("GET "+basePath+"/me", h.getMe, h.RequireFullSession)
	mux.HandleFunc(basePath+"/", func(w http.ResponseWriter, _ *http.Request) {
		platform.WriteProblem(w, http.StatusNotFound, "No such resource.")
	})
}

type principalKey struct{}

func (h *Handler) requirePendingSession(next http.Handler) http.Handler {
	return h.requireSession(next, func(session domain.Session) bool { return !session.FullyAuthenticated() })
}

func (h *Handler) requireAnySession(next http.Handler) http.Handler {
	return h.requireSession(next, func(domain.Session) bool { return true })
}

// RequireFullSession also lets cmd/ protect other modules' routes without them importing this one.
func (h *Handler) RequireFullSession(next http.Handler) http.Handler {
	return h.requireSession(next, domain.Session.FullyAuthenticated)
}

func (h *Handler) requireSession(next http.Handler, accepts func(domain.Session) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(h.Cookies.Name(sessionCookie))
		if err != nil {
			platform.WriteProblem(w, http.StatusUnauthorized, genericUnauthorized)
			return
		}
		principal, err := h.UseCases.Authenticate.Execute(r.Context(), cookie.Value)
		if err != nil {
			h.writeError(w, r, err)
			return
		}
		if !accepts(principal.Session) {
			platform.WriteProblem(w, http.StatusUnauthorized, genericUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}

func principalFrom(r *http.Request) application.Principal {
	principal, ok := r.Context().Value(principalKey{}).(application.Principal)
	if !ok {
		// Unreachable behind requireSession; an empty principal matches no row, so it fails closed.
		return application.Principal{}
	}
	return principal
}

func (h *Handler) client(r *http.Request) domain.ClientInfo {
	return domain.ClientInfo{IP: h.ClientIP.Resolve(r), UserAgent: r.UserAgent()}
}

func (h *Handler) allowPrincipal(w http.ResponseWriter, r *http.Request) bool {
	ok, retryAfter := h.Limits.ByAccount.Allow(principalFrom(r).User.ID)
	if !ok {
		platform.WriteTooManyRequests(w, retryAfter)
	}
	return ok
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var locked *domain.LockedError
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		platform.WriteProblem(w, http.StatusUnauthorized, genericUnauthorized)
	case errors.As(err, &locked):
		platform.WriteTooManyRequests(w, locked.RetryAfter)
	case errors.Is(err, domain.ErrMFAAlreadyEnabled):
		platform.WriteProblem(w, http.StatusConflict, "MFA is already enabled.")
	default:
		h.Logger.LogAttrs(r.Context(), slog.LevelError, "auth request failed",
			slog.String("error", err.Error()),
			slog.String("path", r.URL.Path),
			slog.String("request_id", platform.RequestIDFrom(r.Context())),
		)
		platform.WriteProblem(w, http.StatusInternalServerError, "Something went wrong.")
	}
}

func writeBadRequest(w http.ResponseWriter, fields ...platform.FieldError) {
	platform.WriteProblem(w, http.StatusBadRequest, "The request is not valid.", fields...)
}

func (h *Handler) setSession(w http.ResponseWriter, session application.IssuedSession) {
	h.Cookies.Set(w, sessionCookie, session.Token, session.Lifetime)
}

func (h *Handler) clearSession(w http.ResponseWriter) {
	h.Cookies.Clear(w, sessionCookie)
}

// Clock is the production application.Clock.
type Clock struct{}

func (Clock) Now() time.Time {
	return time.Now()
}
