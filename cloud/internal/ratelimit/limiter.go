// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package ratelimit provides rate limiting with per-identity and per-IP support.
// §2.3: Interface-based design allows swapping to distributed implementation (Redis).
package ratelimit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Limiter is the rate limiting interface. Implementations can be in-memory or distributed.
type Limiter interface {
	// Allow checks if a request is allowed for the given key.
	// Returns (allowed, remaining, resetAt).
	Allow(key string) (allowed bool, remaining int, resetAt time.Time)
}

// InMemoryLimiter implements sliding-window rate limiting in memory.
// Safe for concurrent use. Suitable for single-replica deployments.
type InMemoryLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	maxCalls int
	window   time.Duration
}

// NewInMemoryLimiter creates an in-memory rate limiter.
func NewInMemoryLimiter(maxCalls int, window time.Duration) *InMemoryLimiter {
	return &InMemoryLimiter{
		attempts: make(map[string][]time.Time),
		maxCalls: maxCalls,
		window:   window,
	}
}

// Allow checks if the key is within the rate limit.
func (l *InMemoryLimiter) Allow(key string) (bool, int, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-l.window)

	// Prune old entries
	attempts := l.attempts[key]
	pruned := attempts[:0]
	for _, t := range attempts {
		if t.After(windowStart) {
			pruned = append(pruned, t)
		}
	}

	resetAt := now.Add(l.window)
	if len(pruned) > 0 {
		resetAt = pruned[0].Add(l.window)
	}

	if len(pruned) >= l.maxCalls {
		l.attempts[key] = pruned
		return false, 0, resetAt
	}

	pruned = append(pruned, now)
	l.attempts[key] = pruned
	return true, l.maxCalls - len(pruned), resetAt
}

// Key builds a rate limit key from user identity and IP.
// Uses identity when available (authenticated requests), IP as fallback.
func Key(userID, ip string) string {
	if userID != "" {
		return fmt.Sprintf("user:%s", userID)
	}
	return fmt.Sprintf("ip:%s", ip)
}

// SetHeaders adds standard rate limit headers to the response.
func SetHeaders(w http.ResponseWriter, limit, remaining int, resetAt time.Time) {
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))
}

// Middleware returns chi middleware that rate-limits requests using a key
// derived by keyFn. keyFn lets the caller choose the identity source — the
// authenticated user for protected routes, or the client IP for pre-auth routes.
// This keeps the ratelimit package decoupled from the auth package.
func Middleware(l Limiter, limit int, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, remaining, resetAt := l.Allow(keyFn(r))
			SetHeaders(w, limit, remaining, resetAt)
			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"code":    "RATE_LIMITED",
					"message": "Too many requests. Please wait a moment and try again.",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
