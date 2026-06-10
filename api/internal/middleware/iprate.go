// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPRateLimiter is a fixed-window rate limiter keyed by client IP. Mounted
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
// In-memory fixed-window bucket: `count[key]` resets each `windowSize`.
// Sufficient for a single-replica API in front of CF; if/when the API runs
// multi-replica with sticky-session load balancing the per-pod limit is
// effectively per-pod, which is documented as acceptable for an edge gate
// (auth + headers carry the real per-tenant guard upstream of the bucket).
// A Redis-backed sliding window is the production upgrade path post-launch.

// IPRateLimiter holds the per-IP counter map.
type IPRateLimiter struct {
	mu       sync.Mutex
	counts   map[string]*ipCounter
	limit    int           // requests per window
	window   time.Duration // window size (e.g., 1 minute)
	cidrs    []*net.IPNet  // trusted proxy CIDRs (X-Forwarded-For honored ONLY from these)
	getNow   func() time.Time
}

type ipCounter struct {
	count       int
	windowStart time.Time
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
		counts: make(map[string]*ipCounter),
		limit:  limit,
		window: window,
		cidrs:  cfg.TrustedProxyCIDRs,
		getNow: time.Now,
	}
}

// Middleware returns the chi-compatible HTTP middleware.
func (l *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.keyFor(r)
		if key == "" {
			// Cannot determine peer — fail open (do not block legitimate
			// traffic on parsing failure; metrics + alerts catch this case).
			next.ServeHTTP(w, r)
			return
		}

		allowed, retryAfter := l.allow(key)
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", retryAfter.String())
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

// keyFor extracts the rate-limit key from the request. Returns the IPv4
// address verbatim or the IPv6 /64 prefix as `aaaa:bbbb:cccc:dddd::/64`.
// Honors X-Forwarded-For only when r.RemoteAddr's IP is inside cidrs.
func (l *IPRateLimiter) keyFor(r *http.Request) string {
	peerIP := parsePeerIP(r.RemoteAddr)
	if peerIP == nil {
		return ""
	}

	chosenIP := peerIP
	if l.isTrustedProxy(peerIP) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First entry in the comma-separated list is the original client.
			first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			if parsed := net.ParseIP(first); parsed != nil {
				chosenIP = parsed
			}
		}
	}

	return groupIPKey(chosenIP)
}

// isTrustedProxy reports whether the peer IP falls inside any configured
// trusted-proxy CIDR.
func (l *IPRateLimiter) isTrustedProxy(ip net.IP) bool {
	for _, cidr := range l.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// allow registers one request against the bucket and returns (allowed,
// retryAfter). Uses a fixed window — the simplest correct implementation;
// can be upgraded to sliding-window with Redis later.
func (l *IPRateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.getNow()
	c, ok := l.counts[key]
	if !ok || now.Sub(c.windowStart) >= l.window {
		l.counts[key] = &ipCounter{count: 1, windowStart: now}
		return true, 0
	}
	c.count++
	if c.count > l.limit {
		return false, l.window - now.Sub(c.windowStart)
	}
	return true, 0
}

// parsePeerIP extracts net.IP from r.RemoteAddr (typically "ip:port",
// possibly "[ipv6]:port", possibly bare "ip" in tests).
func parsePeerIP(remoteAddr string) net.IP {
	if remoteAddr == "" {
		return nil
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	return net.ParseIP(host)
}

// groupIPKey returns the canonical bucket key for `ip`. IPv4 stays as-is
// ("203.0.113.5"). IPv6 collapses to /64 prefix ("2001:db8:abcd:0::/64")
// per EC-13 of the edge-case review.
func groupIPKey(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	// IPv6: mask to /64.
	mask := net.CIDRMask(64, 128)
	prefix := ip.Mask(mask)
	return prefix.String() + "/64"
}

// ParseTrustedProxyCIDRs parses a comma-separated CIDR list (e.g., from
// LOOPZERO_TRUSTED_PROXY_CIDRS env). Invalid entries are silently skipped —
// the caller logs the result count so misconfiguration is observable.
func ParseTrustedProxyCIDRs(s string) []*net.IPNet {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []*net.IPNet
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, cidr, err := net.ParseCIDR(raw)
		if err != nil {
			continue
		}
		out = append(out, cidr)
	}
	return out
}
