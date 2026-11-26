// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// TestIPRateLimit_120PerMinute_ReturnsLast40As429 — T2.4 DoD test.
func TestIPRateLimit_120PerMinute_ReturnsLast40As429(t *testing.T) {
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 120, Window: 1 * time.Minute, Clock: fixedClock})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	successCount, rateLimitCount := 0, 0
	for i := 0; i < 200; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		switch rec.Code {
		case http.StatusOK:
			successCount++
		case http.StatusTooManyRequests:
			rateLimitCount++
		}
	}
	if successCount != 120 {
		t.Errorf("want 120 successes, got %d", successCount)
	}
	if rateLimitCount != 80 {
		t.Errorf("want 80 rate-limited, got %d", rateLimitCount)
	}
}

// TestIPRateLimit_RejectsXForwardedForFromUntrustedSource — EC-3.
// Without trusted proxy CIDRs, X-Forwarded-For MUST be ignored. Otherwise
// an attacker rotates header value to bypass the bucket.
func TestIPRateLimit_RejectsXForwardedForFromUntrustedSource(t *testing.T) {
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 5, Window: 1 * time.Minute, Clock: fixedClock})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 10 requests from same untrusted peer with rotating spoofed XFF.
	successCount := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:54321"
		req.Header.Set("X-Forwarded-For", "1.2.3."+strconv.Itoa(i))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			successCount++
		}
	}
	// Limit is 5; 10 requests = 5 OK + 5 429. If XFF were honored, all 10 OK.
	if successCount != 5 {
		t.Errorf("XFF must be ignored from untrusted peer; want 5 successes, got %d", successCount)
	}
}

// TestIPRateLimit_HonorsXForwardedForFromCloudflareEgress — trusted-proxy path.
func TestIPRateLimit_HonorsXForwardedForFromCloudflareEgress(t *testing.T) {
	_, cfCIDR, _ := net.ParseCIDR("173.245.48.0/20")
	limiter := NewIPRateLimiter(IPRateLimitConfig{
		Limit:             5,
		Window:            1 * time.Minute,
		TrustedProxyCIDRs: []*net.IPNet{cfCIDR},
		Clock:             fixedClock,
	})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Same trusted-proxy peer, but rotating XFF — each XFF gets its own bucket.
	successCount := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "173.245.48.10:54321" // inside the CF CIDR
		req.Header.Set("X-Forwarded-For", "1.2.3."+strconv.Itoa(i))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			successCount++
		}
	}
	if successCount != 10 {
		t.Errorf("XFF from trusted CF egress must be keyed per-XFF; want 10 successes, got %d", successCount)
	}
}

// TestIPRateLimit_IPv6PrefixGrouping — EC-13: IPv6 /64 prefix bucket.
func TestIPRateLimit_IPv6PrefixGrouping(t *testing.T) {
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 5, Window: 1 * time.Minute, Clock: fixedClock})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 10 requests from different addresses inside the same /64 → same bucket.
	successCount := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "[2001:db8:abcd:0::" + strconv.Itoa(i) + "]:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			successCount++
		}
	}
	if successCount != 5 {
		t.Errorf("IPv6 /64 prefix must collapse to single bucket; want 5 successes, got %d", successCount)
	}
}

func fixedClock() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) }

// Retry-After is RFC 9110 delay-seconds: an integer. "45.3s" is not one, and a
// client that cannot parse it retries at once.
func TestIPRateLimit_RetryAfterIsIntegerSecondsLeftInTheWindow(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 1, Window: time.Minute, Clock: func() time.Time { return now }})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	serve := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	serve()
	now = now.Add(14700 * time.Millisecond)
	rec := serve()

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "46" {
		t.Errorf("Retry-After = %q, want \"46\" (45.3s left, rounded up to whole seconds)", got)
	}
}

// The limiter runs before auth, so its keys are whatever addresses send
// traffic. Keys idle for two windows must be evicted, or rotating /64s grows
// the map without bound.
func TestIPRateLimit_EvictsAddressesIdleForTwoWindows(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 5, Window: time.Minute, Clock: func() time.Time { return now }})
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	serveFrom := func(addr string) {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = addr
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	for i := 0; i < 500; i++ {
		serveFrom(fmt.Sprintf("[2001:db8:%x::1]:443", i))
	}
	now = now.Add(2*time.Minute + time.Second)
	serveFrom("203.0.113.5:1")

	if got := limiter.counter.Len(); got != 1 {
		t.Errorf("tracked %d addresses after the rotating /64s went idle for two windows, want 1", got)
	}
}
