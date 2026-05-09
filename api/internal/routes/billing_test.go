// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeBillingPool implements BillingPool for unit tests.
type fakeBillingPool struct {
	plan       string
	customerID *string
	planErr    error
	custErr    error
}

type fakeRow struct {
	target any
	err    error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.target == nil {
		return pgx.ErrNoRows
	}
	switch v := dest[0].(type) {
	case *string:
		*v = r.target.(string)
	case **string:
		s := r.target.(string)
		*v = &s
	}
	return nil
}

func (p *fakeBillingPool) QueryRow(_ context.Context, sql string, _ ...any) BillingRow {
	if sql == `SELECT plan FROM subscriptions WHERE tenant_id = $1 LIMIT 1` {
		if p.planErr != nil {
			return &fakeRow{err: p.planErr}
		}
		if p.plan == "" {
			return &fakeRow{err: pgx.ErrNoRows}
		}
		return &fakeRow{target: p.plan}
	}
	if sql == `SELECT stripe_customer_id FROM subscriptions WHERE tenant_id = $1 LIMIT 1` {
		if p.custErr != nil {
			return &fakeRow{err: p.custErr}
		}
		if p.customerID == nil {
			return &fakeRow{err: pgx.ErrNoRows}
		}
		return &fakeRow{target: *p.customerID}
	}
	return &fakeRow{err: pgx.ErrNoRows}
}

func TestBilling_Get_HappyPath(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{plan: "pro"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp BillingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plan != "pro" {
		t.Errorf("want plan=pro, got %q", resp.Plan)
	}
}

func TestBilling_Get_NoSubscription_DefaultsToStarter(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{}) // no plan set → ErrNoRows
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp BillingResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Plan != "starter" {
		t.Errorf("want plan=starter (default), got %q", resp.Plan)
	}
}

func TestBilling_Get_Unauthenticated(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for missing auth, got %d", w.Code)
	}
}

// EC-8 of edge-case review: tenant without stripe_customer_id (Starter
// free tier) MUST get 400 NO_STRIPE_CUSTOMER, NOT 500.
func TestBilling_Portal_NoStripeCustomer_Returns400(t *testing.T) {
	h := NewBillingHandler(&fakeBillingPool{}) // customerID nil → ErrNoRows
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (NO_STRIPE_CUSTOMER), got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "NO_STRIPE_CUSTOMER" {
		t.Errorf("want error=NO_STRIPE_CUSTOMER, got %q", body["error"])
	}
}

func TestBilling_Portal_WithStripeCustomer_ReturnsURL(t *testing.T) {
	custID := "cus_TestCustomerID"
	h := NewBillingHandler(&fakeBillingPool{customerID: &custID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = withAuth(req, defaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp PortalResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.URL == "" {
		t.Error("expected non-empty portal URL")
	}
	if !contains(resp.URL, custID) {
		t.Errorf("expected URL to include customer ID %q, got %q", custID, resp.URL)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
