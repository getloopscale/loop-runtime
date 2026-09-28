// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/api/internal/middleware/clientip"
	"github.com/getloopscale/loopzero/api/internal/middleware/slidingwindow"
)

// IPRateLimiter is a sliding-window rate limiter keyed by client IP. Mounted
// BEFORE the auth middleware so unauthenticated brute-force attempts on
// /api/v1/auth/* endpoints (or any other route) are dropped at the edge
// without ever consuming downstream resources.
//
// T2.4 of deep-review-remediation-plan + ADR D4 (find_sec_ratelimit_post_auth_only).
//
// Trust model
// -----------
// Every request lands here on `r.RemoteAddr` (the TCP peer). When the API
// runs behind a trusted proxy (Cloudflare Tunnel + ingress), the original
// client IP arrives in `X-Forwarded-For`. EC-3 of the edge-case review
// requires that the header is honored ONLY when the immediate peer is
// inside `trustedProxyCIDRs`. Otherwise an attacker would set
// `X-Forwarded-For: 1.2.3.4` and get the bucket reset on every request.
//
// IPv6 handling (EC-13)
// ---------------------
// Single-host attackers on IPv6 trivially get a /64 subnet from any
// consumer ISP. Per-address keying turns the bucket into infinite buckets.
// We group IPv6 by /64 prefix as the smallest "shared NAT-like" unit.
//
// Implementation
// --------------
// In-memory SLIDING-window counter (package slidingwindow), weighted by the
// overlap of the previous window (SD-04.3, audit 2026-09-07 — it was a fixed
// window, and a fixed window admits twice the limit across a boundary). Keys
// idle for two windows are evicted: this limiter runs before auth, so its key
// space is chosen by whoever sends traffic.
//
// The state is still PER PROCESS, so with replicaCount 2 the effective limit is
// twice what is configured (SD-04.7). Two things bound that, and neither makes
// it disappear:
//   - the Traefik `rate-limit` Middleware is now attached to the public route
//     (ingressRoute.middlewares in charts/loopzero-api/values-bootstrap.yaml), and the edge is the one
//     place where a single instance of the limit exists;
//   - auth and per-tenant limits upstream carry the real guard.
//
// A shared store remains the only thing that makes the per-process multiplication
// go away for traffic that does not pass the edge. It is deliberately not added
// here: it would put a network round-trip on every request to fix a factor of
// two that the edge already covers on the exposed path.

// IPRateLimiter holds the per-IP counters.
type IPRateLimiter struct {
	counter  *slidingwindow.Counter
	resolver clientip.Resolver
}

// IPRateLimitConfig configures the limiter.
type IPRateLimitConfig struct {
	// Limit is the max requests per Window per client IP. Default: 120.
	Limit int
	// Window is the rolling window size. Default: 1 minute.
	Window time.Duration
	// TrustedProxyCIDRs lists CIDR blocks whose `X-Forwarded-For` header
	// the limiter trusts. Typically Cloudflare egress ranges in production.
	// Empty list = never trust X-Forwarded-For (treat r.RemoteAddr as truth).
	TrustedProxyCIDRs []*net.IPNet
	// Clock is the time source; nil means time.Now.
	Clock func() time.Time
}

// NewIPRateLimiter builds a limiter with the given config.
func NewIPRateLimiter(cfg IPRateLimitConfig) *IPRateLimiter {
	limit := cfg.Limit
	if limit == 0 {
		limit = 120
	}
	window := cfg.Window
	if window == 0 {
		window = 1 * time.Minute
	}
	return &IPRateLimiter{
		counter:  slidingwindow.New(limit, window, cfg.Clock),
		resolver: clientip.NewResolver(cfg.TrustedProxyCIDRs),
	}
}

// Middleware returns the chi-compatible HTTP middleware.
func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientip.BucketKey(l.resolver.IP(r))
		if key == "" {
			// Cannot determine peer — fail open (do not block legitimate
			// traffic on parsing failure; metrics + alerts catch this case).
			next.ServeHTTP(w, r)
			return
		}

		allowed, retryAfter := l.counter.Allow(key)
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			slidingwindow.SetRetryAfter(w, retryAfter)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":    "RATE_LIMITED",
				"message": "Too many requests. Please retry after the Retry-After window.",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}
