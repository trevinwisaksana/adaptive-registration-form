// Package ratelimit is a minimal in-memory token bucket, keyed by an
// arbitrary string (device+IP for session creation, session id for submits —
// plan.md §5's rate-limit table). POC-grade: in production this becomes
// Redis + the gateway tier, per plan.md §5's own note ("limits are config,
// not code").
package ratelimit

import (
	"sync"
	"time"
)

type bucket struct {
	tokens     float64
	lastRefill time.Time
}

// Limiter is a token bucket per key with a fixed capacity and refill rate.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	capacity float64
	refill   float64 // tokens per second
}

// New creates a limiter allowing `capacity` requests, refilling at
// capacity/window (e.g. New(5, 24*time.Hour) => ~5 per day per key).
func New(capacity int, window time.Duration) *Limiter {
	return &Limiter{
		buckets:  map[string]*bucket{},
		capacity: float64(capacity),
		refill:   float64(capacity) / window.Seconds(),
	}
}

// Allow reports whether a request for key may proceed, consuming one token
// if so. Also returns a retry-after hint in seconds when denied.
func (l *Limiter) Allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.capacity, lastRefill: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens = min(l.capacity, b.tokens+elapsed*l.refill)
	b.lastRefill = now

	if b.tokens < 1 {
		retryAfter := int((1 - b.tokens) / l.refill)
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter
	}
	b.tokens--
	return true, 0
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
