// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainshttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/routestest"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	ratelimit "github.com/getloopscale/loop-runtime/api/internal/middleware/ratelimit"
)

// T3.1 of plan m3-verify-rate-limit-middleware (v1.1 SHIPPABLE 90.4).
// Integration test wiring VerifyRateLimiter + real DomainHandler under a
// chi.Router mounted route — closes the gap between the unit tests
// (middleware in isolation) and the production wire-up in
// api/internal/server/router.go (composition test).
//
// Per J11D7 LOCKED ADR: 1 reverify per 30 seconds per (tenant, domain).

func mountVerifyRoute(t *testing.T, vrl *ratelimit.VerifyRateLimiter, h *DomainHandler) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.With(vrl.Middleware()).Post("/api/v1/domains/{domain}/verify", h.Verify)
	return r
}

// TestVerifyRateLimit_IntegrationFirstAllowedSecondDenied composes
// VerifyRateLimiter + real DomainHandler under a chi router (exact mirror of
// the wiring in api/internal/server/router.go) and asserts:
//
//  1. The 1st request within the 30s window passes the middleware AND reaches
//     DomainHandler.Verify (handler responds 200 OK with verified status).
//  2. The 2nd request within the window is short-circuited by the middleware
//     and returns 429 with the canonical RATE_LIMITED JSON body and
//     Retry-After: 30 header.
//
// This proves the middleware is wired BEFORE the handler in the route chain.
func TestVerifyRateLimit_IntegrationFirstAllowedSecondDenied(t *testing.T) {
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:            "example.com",
			Status:            "pending_verification",
			VerificationToken: "REDACTED_SECRET_055",
		},
	}
	verifier := &fakeDomainVerifier{verified: true}
	h := NewDomainHandler(store, verifier, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	vrl := ratelimit.NewVerifyRateLimiter(1, 30*time.Second)

	router := mountVerifyRoute(t, vrl, h)
	ctx := context.WithValue(context.Background(),
		auth.UserContextKeyPublic(), routestest.DefaultUserContext())

	// 1st call — passes middleware, reaches handler
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil).WithContext(ctx)
	rr1 := httptest.NewRecorder()
	router.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("1st request: expected 200 (handler reached), got %d body=%s", rr1.Code, rr1.Body.String())
	}
	if !strings.Contains(rr1.Body.String(), "verified") {
		t.Fatalf("1st request: expected handler body with verified status, got %s", rr1.Body.String())
	}

	// 2nd call — middleware short-circuits (handler MUST NOT be invoked)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/domains/example.com/verify", nil).WithContext(ctx)
	rr2 := httptest.NewRecorder()
	router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("2nd request: expected 429 (middleware short-circuit), got %d", rr2.Code)
	}
	if rr2.Header().Get("Retry-After") != "30" {
		t.Fatalf("2nd request: expected Retry-After=30 header, got %q", rr2.Header().Get("Retry-After"))
	}
	if !strings.Contains(rr2.Body.String(), "RATE_LIMITED") {
		t.Fatalf("2nd request: expected RATE_LIMITED body, got %s", rr2.Body.String())
	}
}

// TestVerifyRateLimit_IntegrationPerDomainIsolation proves per-(tenant, domain)
// keying — the same tenant can verify TWO different domains back-to-back
// because they hold independent buckets.
func TestVerifyRateLimit_IntegrationPerDomainIsolation(t *testing.T) {
	// fakeDomainStore returns the same fixture for every lookup — that's
	// acceptable here because we only assert the middleware does NOT block
	// the second call when the domain differs.
	store := &fakeDomainStore{
		domain: &CustomDomain{
			Domain:            "a.example.com",
			Status:            "pending_verification",
			VerificationToken: "REDACTED_SECRET_055",
		},
	}
	verifier := &fakeDomainVerifier{verified: true}
	h := NewDomainHandler(store, verifier, &fakeDomainValidator{}, "apps.dev.getloopscale.app")

	vrl := ratelimit.NewVerifyRateLimiter(1, 30*time.Second)

	router := mountVerifyRoute(t, vrl, h)
	ctx := context.WithValue(context.Background(),
		auth.UserContextKeyPublic(), routestest.DefaultUserContext())

	for _, domain := range []string{"a.example.com", "b.example.com"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/domains/"+domain+"/verify", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("domain=%s: per-(tenant, domain) isolation broken — got 429", domain)
		}
	}
}
