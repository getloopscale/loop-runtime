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
// The tenant, IP and domain limiters count through package slidingwindow
// ("N per window"); the broker limiter is a token bucket from
// golang.org/x/time/rate, where a configured burst is the point. This file is
// the one keyed by tenant and user.
package ratelimit

import (
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/slidingwindow"
)

// RateLimiter is a sliding-window rate limiter keyed by an arbitrary string.
// Idle keys are evicted by the underlying counter, so it owns no goroutine.
type RateLimiter struct {
	counter *slidingwindow.Counter
}

// NewRateLimiter creates a rate limiter that allows `rate` requests per `window`.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	return &RateLimiter{counter: slidingwindow.New(rate, window, nil)}
}

// Allow returns true if the request is within the rate limit for the given key.
func (rl *RateLimiter) Allow(key string) bool {
	ok, _ := rl.counter.Allow(key)
	return ok
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
			if ok, retryAfter := rl.counter.Allow(key); !ok {
				w.Header().Set("Content-Type", "application/json")
				slidingwindow.SetRetryAfter(w, retryAfter)
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":"RATE_LIMITED","message":"Too many requests. Try again later."}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
