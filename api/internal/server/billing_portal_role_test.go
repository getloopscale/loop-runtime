// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/billing/billinghttp"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	ratelimit "github.com/getloopscale/loopzero/api/internal/middleware/ratelimit"
)

type roleMember struct{ role string }

func (m roleMember) GetMemberships(_ context.Context, _ string) ([]auth.TenantMembership, error) {
	return []auth.TenantMembership{{TenantID: "tn-fixed", Role: m.role}}, nil
}

// Opening the billing portal session shows invoices, payment method and
// history, and loopzero-cloud requires `owner` for the same capability. The engine
// route must require the same role; an authenticated member must be refused
// before the handler runs.
func TestBillingPortalRequiresTheOwnerRole(t *testing.T) {
	r := NewRouter(Config{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		TokenValidator: fixedTokenValidator{},
		MemberStore:    roleMember{role: "member"},
		BillingHandler: &billinghttp.BillingHandler{},
		Guards: RouteGuards{
			RateLimiter:       ratelimit.NewRateLimiter(1000, time.Minute),
			IPRateLimit:       ratelimit.NewIPRateLimiter(ratelimit.IPRateLimitConfig{}),
			VerifyRateLimiter: ratelimit.NewVerifyRateLimiter(1000, time.Minute),
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req.Header.Set("Authorization", "Bearer any")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a member opening the billing portal got %d, want 403 (body: %s)", rec.Code, rec.Body.String())
	}
	// The 403 must come from the role gate, not from any earlier refusal.
	if !strings.Contains(rec.Body.String(), "INSUFFICIENT_ROLE") {
		t.Errorf("403 did not come from the role check: %s", rec.Body.String())
	}
}
