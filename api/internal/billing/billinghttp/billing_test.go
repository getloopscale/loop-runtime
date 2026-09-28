// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billinghttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// fakePlanStore is the plan read the handler shares with every plan gate.
type fakePlanStore struct {
	plan string
	err  error
}

func (f fakePlanStore) GetTenantPlan(context.Context, string) (string, error) { return f.plan, f.err }

// fakeCustomerStore answers the billing customer lookup.
type fakeCustomerStore struct {
	id  string
	err error
}

func (f fakeCustomerStore) StripeCustomerID(context.Context, string) (string, error) {
	return f.id, f.err
}

func TestBilling_Get_HappyPath(t *testing.T) {
	h := NewBillingHandler(fakePlanStore{plan: "pro"}, fakeCustomerStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{}) // no plan set → ErrNoRows
	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{})
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
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{}) // customerID nil → ErrNoRows
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
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

// TestBilling_Portal_WithIssuer_ReturnsTheIssuedSession — the correct path.
//
// This test used to assert that a URL assembled from the customer id was a valid
// answer (the "legacy fallback"). Since ADR 2026-08-11 it is not: without an issuer
// the engine fails, and with one it returns what the issuer gave. The distinction
// matters because an assembled URL LOOKS like it works — which is how it survived
// on app-dev without anyone noticing.
func TestBilling_Portal_WithIssuer_ReturnsTheIssuedSession(t *testing.T) {
	custID := "cus_TestCustomerID"
	const issuedSession = "https://billing.stripe.com/p/session/live_YWNjdF8xTes"

	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{id: custID}).
		WithPortalSessionCreator(fakeIssuer{url: issuedSession}, "https://app.example/billing")

	req := routestest.WithAuth(httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil), routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Portal(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("with an issuer configured, want 200; got %d body=%s", w.Code, w.Body.String())
	}
	var resp PortalResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.URL != issuedSession {
		t.Errorf("the URL must be the one the issuer returned, not one assembled here.\n  want %q\n  got  %q",
			issuedSession, resp.URL)
	}
	if strings.Contains(resp.URL, custID) {
		t.Errorf("the issued session must not carry the customer's billing id: %q", resp.URL)
	}
}

// fakeIssuer replaces the provider SDK in tests: what matters here is that the
// handler returns what the issuer gave, not that it can talk to the provider.
type fakeIssuer struct{ url string }

func (e fakeIssuer) CreateSession(_ context.Context, _, _ string) (string, error) {
	return e.url, nil
}

// fakePortalCreator is a test double for PortalSessionCreator. Records
// the customerID + returnURL it was called with so tests can assert
// the handler passed correct arguments.
type fakePortalCreator struct {
	url        string
	err        error
	gotCust    string
	gotReturn  string
	gotContext context.Context
}

func (f *fakePortalCreator) CreateSession(ctx context.Context, customerID, returnURL string) (string, error) {
	f.gotCust = customerID
	f.gotReturn = returnURL
	f.gotContext = ctx
	if f.err != nil {
		return "", f.err
	}
	return f.url, nil
}

// T2.7: when SDK is wired, handler calls creator.CreateSession with the
// tenant's stripe_customer_id and the configured return URL, returning
// the real one-time-use portal URL.
func TestBilling_Portal_WithSDK_HappyPath(t *testing.T) {
	custID := "cus_TestSDKPath"
	creator := &fakePortalCreator{url: "https://billing.stripe.com/p/session/test_xxx"}
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{id: custID}).
		WithPortalSessionCreator(creator, "https://app.loopzero.cloud/billing")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp PortalResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.URL != "https://billing.stripe.com/p/session/test_xxx" {
		t.Errorf("want real portal URL from SDK, got %q", resp.URL)
	}
	if creator.gotCust != custID {
		t.Errorf("creator received customer %q, want %q", creator.gotCust, custID)
	}
	if creator.gotReturn != "https://app.loopzero.cloud/billing" {
		t.Errorf("creator received return URL %q, want app.loopzero.cloud/billing", creator.gotReturn)
	}
}

// T2.7: SDK errors (Stripe rate limit, invalid key, network) map to
// 502 BILLING_PROVIDER_UNAVAILABLE with product-language message
// (Rule 3: no "Stripe" leak in user-facing string).
func TestBilling_Portal_WithSDK_StripeError_Returns502(t *testing.T) {
	custID := "cus_TestSDKError"
	creator := &fakePortalCreator{err: errSDKFailure}
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{id: custID}).
		WithPortalSessionCreator(creator, "https://app.loopzero.cloud/billing")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502 (BILLING_PROVIDER_UNAVAILABLE), got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["error"] != "BILLING_PROVIDER_UNAVAILABLE" {
		t.Errorf("want error=BILLING_PROVIDER_UNAVAILABLE, got %q", body["error"])
	}
	// Rule 3 guard: message must not leak "Stripe" or "stripe" to user.
	msg := body["message"]
	if strings.Contains(msg, "Stripe") || strings.Contains(msg, "stripe") {
		t.Errorf("Rule 3 violation: message leaks internal vendor name: %q", msg)
	}
}

// T2.7: no-Stripe-customer EC-8 path still 400 even with SDK wired —
// SDK only mints session for valid customers; missing customer is a
// front-of-handler check that does NOT call the SDK.
func TestBilling_Portal_NoCustomer_DoesNotCallSDK(t *testing.T) {
	creator := &fakePortalCreator{url: "should-never-be-returned"}
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{}). // no customer ID
									WithPortalSessionCreator(creator, "https://app.loopzero.cloud/billing")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 NO_STRIPE_CUSTOMER, got %d", w.Code)
	}
	if creator.gotCust != "" {
		t.Errorf("SDK was called for customer-less tenant (got customer=%q); should short-circuit", creator.gotCust)
	}
}

// errSDKFailure is a sentinel for SDK error simulation. Defined as
// package-level var so the test reads naturally without inline new.
var errSDKFailure = stripeErr("test sdk failure")

type stripeErr string

func (e stripeErr) Error() string { return string(e) }

// TestBilling_Portal_WithoutIssuer_DoesNotAssembleAURL closes B-032.
//
// MEASURED 2026-08-11: without a provider key, the handler fell into a path that
// assembled `https://billing.stripe.com/p/login/` + the customer id and returned it
// as if it were the portal session. It is not a session the provider issued — it is a
// URL carrying the customer's billing identifier, which travels through browser
// history, proxy logs and the address bar.
//
// Nor was it hypothetical: on app-dev the provider Secret is `optional: true` and
// absent, so this was the ACTIVE path. The original comment said the fallback served
// "local dev + tests"; the intent was local, the effect was public.
//
// The decision (ADR 2026-08-11) is that loopzero-cloud is the only issuer. When the
// engine cannot issue, it FAILS saying where to go — a URL that looks like it works
// hides the problem until someone investigates why the portal did not open.
func TestBilling_Portal_WithoutIssuer_DoesNotAssembleAURL(t *testing.T) {
	custID := "cus_TestCustomerID"
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{id: custID}) // no WithPortalSessionCreator

	req := routestest.WithAuth(httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil), routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Portal(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("with no issuer configured the engine must NOT answer 200 with an assembled URL — "+
			"it carries the customer's billing id and is not an issued session. "+
			"Got %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), custID) {
		t.Errorf("the response carries the customer's billing id (%s): %s", custID, w.Body.String())
	}
}

// A canceled subscription is not a plan: the plan read is the one every plan
// gate shares, which counts active and trialing subscriptions only, so a store
// with no current subscription answers "" and billing shows Starter — the same
// tier enforcement applies. Billing used to read the table itself, unfiltered,
// and showed a canceled Pro tenant as "pro".
func TestBilling_Get_ShowsTheTierEnforcementApplies(t *testing.T) {
	h := NewBillingHandler(fakePlanStore{plan: ""}, fakeCustomerStore{id: "cus_old"})
	req := routestest.WithAuth(httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil), routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Get(w, req)

	var resp BillingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plan != "starter" {
		t.Errorf("plan = %q, want starter", resp.Plan)
	}
}

// A plan value the platform does not know is not shown as-is, and a failed
// read is not a 500 with nothing to act on: both are PLATFORM_DEGRADED, the
// answer every other plan read gives.
func TestBilling_Get_UnresolvablePlanIsDegraded(t *testing.T) {
	for name, store := range map[string]fakePlanStore{
		"unknown plan value": {plan: "platinum"},
		"store failure":      {err: errors.New("connection refused")},
	} {
		t.Run(name, func(t *testing.T) {
			h := NewBillingHandler(store, fakeCustomerStore{})
			req := routestest.WithAuth(httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil), routestest.DefaultUserContext())
			w := httptest.NewRecorder()

			h.Get(w, req)

			if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "PLATFORM_DEGRADED") {
				t.Fatalf("want 503 PLATFORM_DEGRADED, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestBilling_Portal_CustomerLookupFailureIs500(t *testing.T) {
	h := NewBillingHandler(fakePlanStore{}, fakeCustomerStore{err: errors.New("connection refused")})
	req := routestest.WithAuth(httptest.NewRequest(http.MethodPost, "/api/v1/billing/portal", nil), routestest.DefaultUserContext())
	w := httptest.NewRecorder()

	h.Portal(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d body=%s", w.Code, w.Body.String())
	}
}
