package platform

import (
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const (
	maxRateLimitKeys = 10_000
	ipv6ClientBits   = 64
)

// TokenBucket limits each key to a burst of capacity, refilled at one token per interval.
type TokenBucket struct {
	capacity float64
	interval time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens  float64
	updated time.Time
}

func NewTokenBucket(capacity int, interval time.Duration) *TokenBucket {
	return &TokenBucket{capacity: float64(capacity), interval: interval, buckets: make(map[string]*bucket)}
}

// Allow takes a token for key, or reports how long until one is available.
func (t *TokenBucket) Allow(key string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	entry, known := t.buckets[key]
	if !known {
		t.makeRoom(now)
		entry = &bucket{tokens: t.capacity, updated: now}
		t.buckets[key] = entry
	}
	entry.tokens = t.refilled(entry, now)
	entry.updated = now
	if entry.tokens < 1 {
		return false, time.Duration((1 - entry.tokens) * float64(t.interval))
	}
	entry.tokens--
	return true, 0
}

func (t *TokenBucket) refilled(entry *bucket, now time.Time) float64 {
	return min(t.capacity, entry.tokens+float64(now.Sub(entry.updated))/float64(t.interval))
}

// makeRoom keeps memory bounded under a flood of distinct keys. A full bucket is the same as
// no bucket, so those go first.
func (t *TokenBucket) makeRoom(now time.Time) {
	if len(t.buckets) < maxRateLimitKeys {
		return
	}
	for key, entry := range t.buckets {
		if t.refilled(entry, now) >= t.capacity {
			delete(t.buckets, key)
		}
	}
	if len(t.buckets) < maxRateLimitKeys {
		return
	}
	for key := range t.buckets {
		delete(t.buckets, key)
		return
	}
}

// ClientIPKey identifies a client for limiting. An IPv6 client usually controls a whole /64, so
// that is its key: any narrower one would let it walk through addresses to dodge a limit.
func ClientIPKey(addr netip.Addr) string {
	addr = addr.Unmap()
	if !addr.Is6() {
		return addr.String()
	}
	prefix, err := addr.Prefix(ipv6ClientBits)
	if err != nil {
		// Unreachable: 64 is always a valid length for an IPv6 address.
		return addr.String()
	}
	return prefix.String()
}

// LimitByIP answers 429 once a client address has spent its bucket.
func LimitByIP(limiter *TokenBucket, resolver ClientIPResolver) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, retryAfter := limiter.Allow(ClientIPKey(resolver.Resolve(r))); !ok {
				WriteTooManyRequests(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
