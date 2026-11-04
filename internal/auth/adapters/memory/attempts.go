// Package memory holds in-process adapters of the auth ports.
package memory

import (
	"net/netip"
	"sync"
	"time"

	"github.com/SebastianP07/wallet-track-core-app/internal/auth/domain"
	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

const maxTrackedPairs = 10_000

type pair struct {
	email string
	ip    string
}

type attempt struct {
	failures int
	// lockedUntil holds the time of the last failure while below the lockout threshold, so in
	// every case the entry with the earliest value is the one whose eviction forgives least.
	lockedUntil time.Time
}

// PasswordAttempts counts password failures per email and client IP. A restart forgets them.
type PasswordAttempts struct {
	mu      sync.Mutex
	entries map[pair]attempt
}

func NewPasswordAttempts() *PasswordAttempts {
	return &PasswordAttempts{entries: make(map[pair]attempt)}
}

func (a *PasswordAttempts) LockedFor(email string, ip netip.Addr, now time.Time) (time.Duration, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	lockedUntil := a.entries[newPair(email, ip)].lockedUntil
	if now.Before(lockedUntil) {
		return lockedUntil.Sub(now), true
	}
	return 0, false
}

func (a *PasswordAttempts) RecordFailure(email string, ip netip.Addr, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := newPair(email, ip)
	entry, known := a.entries[key]
	if !known && len(a.entries) >= maxTrackedPairs {
		a.evictEarliest()
	}
	entry.failures++
	entry.lockedUntil = now.Add(domain.LockoutFor(entry.failures))
	a.entries[key] = entry
}

func (a *PasswordAttempts) Clear(email string, ip netip.Addr) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.entries, newPair(email, ip))
}

// evictEarliest bounds memory under a flood of distinct pairs. A running lock always outlasts an
// idle counter, so a live lock goes only once every tracked pair holds one, soonest to end first.
func (a *PasswordAttempts) evictEarliest() {
	var victim pair
	var earliest time.Time
	for key, entry := range a.entries {
		if earliest.IsZero() || entry.lockedUntil.Before(earliest) {
			victim, earliest = key, entry.lockedUntil
		}
	}
	delete(a.entries, victim)
}

func newPair(email string, ip netip.Addr) pair {
	return pair{email: email, ip: platform.ClientIPKey(ip)}
}
