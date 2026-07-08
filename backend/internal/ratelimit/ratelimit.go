// Package ratelimit provides per-IP rate limiting and login brute-force
// lockout with bounded memory, plus safe client-IP extraction.
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ClientIP returns the caller's IP. X-Forwarded-For is trusted only behind a
// trusted proxy, and then the right-most entry (added by our proxy) is used —
// never the spoofable left-most one.
func ClientIP(r *http.Request, trustedProxy bool) string {
	if trustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if r.RemoteAddr != "" {
			return r.RemoteAddr
		}
		return "unknown"
	}
	return host
}

// RateLimiter is a fixed-window limiter: at most `limit` hits per `window`.
type RateLimiter struct {
	limit   int
	window  time.Duration
	maxKeys int
	mu      sync.Mutex
	hits    map[string][]time.Time
}

// NewRateLimiter builds a limiter allowing `limit` requests per `window`.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, maxKeys: 20000, hits: map[string][]time.Time{}}
}

func (r *RateLimiter) sweep(now time.Time) {
	for k, ts := range r.hits {
		fresh := ts[:0]
		for _, t := range ts {
			if now.Sub(t) < r.window {
				fresh = append(fresh, t)
			}
		}
		if len(fresh) == 0 {
			delete(r.hits, k)
		} else {
			r.hits[k] = fresh
		}
	}
}

// Allow records a hit for key and reports whether it is within the limit.
func (r *RateLimiter) Allow(key string) bool {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	fresh := make([]time.Time, 0, len(r.hits[key])+1)
	for _, t := range r.hits[key] {
		if now.Sub(t) < r.window {
			fresh = append(fresh, t)
		}
	}
	fresh = append(fresh, now)
	r.hits[key] = fresh
	if len(r.hits) > r.maxKeys {
		r.sweep(now)
	}
	return len(fresh) <= r.limit
}

// LoginGuard locks out an IP after too many failed logins.
type LoginGuard struct {
	maxFails int
	window   time.Duration
	lockout  time.Duration
	maxKeys  int
	mu       sync.Mutex
	fails    map[string][]time.Time
	locked   map[string]time.Time
}

// NewLoginGuard creates a guard: maxFails within window -> locked for lockout.
func NewLoginGuard(maxFails int, window, lockout time.Duration) *LoginGuard {
	return &LoginGuard{
		maxFails: maxFails, window: window, lockout: lockout, maxKeys: 20000,
		fails: map[string][]time.Time{}, locked: map[string]time.Time{},
	}
}

func (g *LoginGuard) sweep(now time.Time) {
	for k, ts := range g.fails {
		fresh := ts[:0]
		for _, t := range ts {
			if now.Sub(t) < g.window {
				fresh = append(fresh, t)
			}
		}
		if len(fresh) == 0 {
			delete(g.fails, k)
		} else {
			g.fails[k] = fresh
		}
	}
	for k, until := range g.locked {
		if !until.After(now) {
			delete(g.locked, k)
		}
	}
}

// LockedSeconds returns how many seconds key remains locked (0 if not locked).
func (g *LoginGuard) LockedSeconds(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	rem := time.Until(g.locked[key])
	if rem > 0 {
		return int(rem.Seconds())
	}
	return 0
}

// RecordFail registers a failed attempt and locks the key if over threshold.
func (g *LoginGuard) RecordFail(key string) {
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	fresh := make([]time.Time, 0, len(g.fails[key])+1)
	for _, t := range g.fails[key] {
		if now.Sub(t) < g.window {
			fresh = append(fresh, t)
		}
	}
	fresh = append(fresh, now)
	g.fails[key] = fresh
	if len(fresh) >= g.maxFails {
		g.locked[key] = now.Add(g.lockout)
		g.fails[key] = nil
	}
	if len(g.fails)+len(g.locked) > g.maxKeys {
		g.sweep(now)
	}
}

// Clear resets the failure/lock state for key (called on a successful login).
func (g *LoginGuard) Clear(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, key)
	delete(g.locked, key)
}
