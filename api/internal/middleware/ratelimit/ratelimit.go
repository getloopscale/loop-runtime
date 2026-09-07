// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package ratelimit counts requests over a window and refuses the ones above
// the quota.
//
// Four limiters share the package because they differ only in what they key
// on: the tenant (this file), the client IP (iprate.go, mounted ahead of auth
// so an unauthenticated flood dies at the edge), the calling cell of the
// secret broker (rate_limit_vault.go), and the (tenant, domain) pair of a
// domain re-verification (verify_rate_limit.go).
//
// Three of the four read the caller from auth, which is why the dependency
// runs ratelimit -> auth and never the other way (B-059). iprate.go is the
// exception, and deliberately so: it has to answer before anyone is
// authenticated.
//
// This file implements the fixed-window limiter keyed by tenant.
package ratelimit

import (
	"net/http"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth"
)

// RateLimiter implements a fixed-window rate limiter keyed by tenant ID.
// It uses a simple counter per window, resetting when the window expires.
// Stale buckets are cleaned up periodically to prevent unbounded memory growth.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    int
	window  time.Duration
	stop    chan struct{} // signals cleanupLoop to exit on shutdown
}

type bucket struct {
	count       int
	windowStart time.Time
	// prevCount is the count of the window immediately before windowStart. It is
	// what turns a fixed window into a sliding one (SD-04.3, audit 2026-09-07):
	// a fixed window resets to zero at the boundary, so a client could spend its
	// whole allowance at the end of one window and again at the start of the
	// next — twice the limit inside a span shorter than one window, and 4x across
	// two replicas, which is the burst case a limiter exists to stop.
	prevCount int
}

// NewRateLimiter creates a rate limiter that allows `rate` requests per `window`
// duration. A background goroutine cleans up stale entries every 5 minutes.
// Call Stop() during graceful shutdown to release the goroutine.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		window:  window,
		stop:    make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop signals the cleanup goroutine to exit. Safe to call multiple times.
func (rl *RateLimiter) Stop() {
	select {
	case <-rl.stop:
		// already stopped
	default:
		close(rl.stop)
	}
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for key, b := range rl.buckets {
				if now.Sub(b.windowStart) > rl.window*2 {
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Allow returns true if the request is within the rate limit for the given key.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.buckets[key]
	if !exists {
		rl.buckets[key] = &bucket{count: 1, windowStart: now}
		return true
	}

	// Roll the window forward. One window elapsed keeps the previous count as the
	// overlapping tail; two or more elapsed means nothing overlaps any more.
	switch elapsed := now.Sub(b.windowStart); {
	case elapsed >= 2*rl.window:
		b.prevCount = 0
		b.count = 0
		b.windowStart = now
	case elapsed >= rl.window:
		b.prevCount = b.count
		b.count = 0
		b.windowStart = b.windowStart.Add(rl.window)
	}

	// Weight the previous window by how much of it still falls inside the
	// trailing window. Immediately after a boundary the overlap is ~1, so the
	// client carries almost its whole previous spend; a full window later the
	// overlap is 0 and the allowance has recovered. This is the standard sliding
	// window counter — no shared store, so it fixes the boundary burst without
	// adding a dependency.
	overlap := 1 - (now.Sub(b.windowStart).Seconds() / rl.window.Seconds())
	if overlap < 0 {
		overlap = 0
	}
	weighted := float64(b.prevCount)*overlap + float64(b.count)

	if weighted >= float64(rl.rate) {
		return false
	}
	b.count++
	return true
}

// Middleware returns HTTP middleware that rate-limits by tenant:user composite key.
// Unauthenticated requests are keyed as "anonymous".
// Returns 429 Too Many Requests with a Retry-After header when the limit is exceeded.
//
// T2.3 of deep-review-2026-05-10-remediation-plan (find_sec_ratelimit_per_tenant MEDIUM):
// per-tenant keys let a single noisy user in a Team/Enterprise tenant
// exhaust the bucket for everyone in the same org. Composite key
// (tenantID:userID) gives per-user isolation without weakening the
// tenant-level invariant — the limit per user is still the same, so the
// aggregate per-tenant cap is N×users (intentional: paid plans buy more
// burst headroom).
func (rl *RateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uc := auth.GetUserContext(r.Context())
			key := "anonymous"
			if uc != nil {
				key = uc.TenantID + ":" + uc.UserID
			}
			if !rl.Allow(key) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":"RATE_LIMITED","message":"Too many requests. Try again later."}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
