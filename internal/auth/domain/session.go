package domain

import (
	"net/netip"
	"time"
)

const (
	SessionIdleTimeout     = 7 * 24 * time.Hour
	SessionAbsoluteTimeout = 30 * 24 * time.Hour
	PendingSessionLifetime = 10 * time.Minute
)

type Session struct {
	ID            string
	UserID        string
	MFAVerifiedAt time.Time
	LastSeenAt    time.Time
	ExpiresAt     time.Time
	RevokedAt     time.Time
}

// FullyAuthenticated is false while the second factor is still pending.
func (s Session) FullyAuthenticated() bool {
	return !s.MFAVerifiedAt.IsZero()
}

// ValidAt applies revocation, the absolute expiry and, for full sessions, the idle timeout.
// A pending session has only its short expiry.
func (s Session) ValidAt(now time.Time) bool {
	if !s.RevokedAt.IsZero() || !now.Before(s.ExpiresAt) {
		return false
	}
	return !s.FullyAuthenticated() || now.Sub(s.LastSeenAt) < SessionIdleTimeout
}

// ActiveSession describes a session to its owner. It has no token, and no hash of one.
type ActiveSession struct {
	ID         string
	UserAgent  string
	IP         netip.Addr
	Pending    bool
	CreatedAt  time.Time
	LastSeenAt time.Time
}
