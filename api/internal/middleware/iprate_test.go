// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestIPRateLimit_120PerMinute_ReturnsLast40As429 — T2.4 DoD test.
func TestIPRateLimit_120PerMinute_ReturnsLast40As429(t *testing.T) {
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 120, Window: 1 * time.Minute})
	limiter.getNow = func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) }
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
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 5, Window: 1 * time.Minute})
	limiter.getNow = func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) }
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 10 requests from same untrusted peer with rotating spoofed XFF.
	successCount := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:54321"
		req.Header.Set("X-Forwarded-For", "1.2.3."+itoa(i))
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
	})
	limiter.getNow = func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) }
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Same trusted-proxy peer, but rotating XFF — each XFF gets its own bucket.
	successCount := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "173.245.48.10:54321" // inside the CF CIDR
		req.Header.Set("X-Forwarded-For", "1.2.3."+itoa(i))
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
	limiter := NewIPRateLimiter(IPRateLimitConfig{Limit: 5, Window: 1 * time.Minute})
	limiter.getNow = func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) }
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 10 requests from different addresses inside the same /64 → same bucket.
	successCount := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "[2001:db8:abcd:0::" + itoa(i) + "]:54321"
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

func TestParseTrustedProxyCIDRs(t *testing.T) {
	cidrs := ParseTrustedProxyCIDRs("173.245.48.0/20, 103.21.244.0/22, garbage, ")
	if len(cidrs) != 2 {
		t.Fatalf("want 2 valid CIDRs, got %d", len(cidrs))
	}
	if cidrs[0].String() != "173.245.48.0/20" {
		t.Errorf("first CIDR: want 173.245.48.0/20, got %s", cidrs[0])
	}
	if !cidrs[0].Contains(net.ParseIP("173.245.48.10")) {
		t.Error("first CIDR must contain 173.245.48.10")
	}
}

func TestGroupIPKey_IPv4Verbatim(t *testing.T) {
	got := groupIPKey(net.ParseIP("203.0.113.5"))
	if got != "203.0.113.5" {
		t.Errorf("IPv4 must stay verbatim, got %q", got)
	}
}

func TestGroupIPKey_IPv6Slash64(t *testing.T) {
	got := groupIPKey(net.ParseIP("2001:db8:abcd:0::dead:beef"))
	want := "2001:db8:abcd::/64"
	if got != want {
		t.Errorf("IPv6 must collapse to /64; want %q, got %q", want, got)
	}
}

// itoa avoids strconv import noise in tests.
func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
