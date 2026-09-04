// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package middleware provides HTTP middleware for the LoopZero API.
// This file implements a simple fixed-window rate limiter per tenant.
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
	if !exists || now.Sub(b.windowStart) > rl.window {
		rl.buckets[key] = &bucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= rl.rate {
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
