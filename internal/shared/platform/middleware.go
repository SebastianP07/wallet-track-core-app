package platform

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first one listed runs first.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// The API serves only JSON, so its CSP forbids everything, with no nonce to issue. The nonce-based
// policy of the HTML pages belongs to the Next.js server.
const contentSecurityPolicy = "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		headers.Set("Content-Security-Policy", contentSecurityPolicy)
		headers.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("Referrer-Policy", "strict-origin")
		headers.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

const requestIDHeader = "X-Request-ID"

// An incoming ID is reused so the BFF can correlate logs, but only in a shape that is safe to log.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

type requestIDKey struct{}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !requestIDPattern.MatchString(id) {
			id = rand.Text()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestIDFrom returns the ID of the current request, for log lines.
func RequestIDFrom(ctx context.Context) string {
	id, ok := ctx.Value(requestIDKey{}).(string)
	if !ok {
		return ""
	}
	return id
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// LogRequests writes one line per request. It logs the path only: never a query string, a
// header or a body, which is where credentials would be.
func LogRequests(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", recorder.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", RequestIDFrom(r.Context())),
			)
		})
	}
}
