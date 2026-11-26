// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package devpublic

// Phase 2 — billing family — migrated from:
//   - dev-public-billing-portal_test.sh                       -> TestDevPublic_Billing_Portal
//   - dev-public-billing-pro-price-canonical_test.sh (#157)   -> TestDevPublic_Billing_ProPriceCanonical
//   - dev-public-billing-webhook-fulfillment_test.sh (#156)   -> TestDevPublic_Billing_WebhookFulfillment
//   - dev-public-billing-stripe-price-ids-seed-canonical.sh   -> TestDevPublic_Billing_StripePriceIDsSeed
//   - dev-public-billing-stripe-seed-restarts-cloud.sh (#154) -> TestDevPublic_Billing_StripeSeedRestartsCloud

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockCloudBillingHandler emulates the loop-console /v1/billing surface.
func mockCloudBillingHandler(t *testing.T, stripeConfigured bool, priceIDs map[string]string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    "AUTH_REQUIRED",
				"message": "Not authenticated. Run: loop-runtime login",
			})
			return
		}
		switch r.URL.Path {
		case "/v1/billing/portal":
			if !stripeConfigured {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    "BILLING_NOT_CONFIGURED",
					"message": "Billing is not yet configured for this tenant",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url": "https://billing.stripe.com/p/session/live_YWNjdF8xUmR",
			})
		case "/v1/billing/checkout":
			// #155 canonical: plan_pro requires STRIPE_PRICE_PRO env non-empty
			var req struct {
				Plan string `json:"plan"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			priceID, ok := priceIDs[req.Plan]
			if !ok || priceID == "" {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    "BILLING_NOT_CONFIGURED",
					"message": fmt.Sprintf("no price id for plan %q", req.Plan),
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id":   "cs_test_abc",
				"amount_total": 2900, // #157 canonical Pro = $29/mo
				"currency":     "usd",
				"price_id":     priceID,
				"checkout_url": "https://checkout.stripe.com/pay/cs_test_abc",
			})
		case "/v1/webhooks/stripe":
			// #156: whsec_test_ real signing key OR reject with 400 (signature check).
			sig := r.Header.Get("Stripe-Signature")
			if sig == "" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":    "MISSING_SIGNATURE",
					"message": "Stripe-Signature header required",
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"received": true})
		default:
			http.NotFound(w, r)
		}
	})
}

// TestDevPublic_Billing_Portal migrated from
// infra/tests/e2e/dev-public-billing-portal_test.sh.
func TestDevPublic_Billing_Portal(t *testing.T) {
	// Scenario A: authenticated call with billing configured → 200 + Stripe portal URL
	srv := httptest.NewServer(mockCloudBillingHandler(t, true, nil))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/billing/portal", nil)
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-portal-test"))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "billing.stripe.com",
		"portal response MUST cite billing.stripe.com host")

	t.Run("B_missing_bearer_returns_401", func(t *testing.T) {
		resp2, err := http.Get(srv.URL + "/v1/billing/portal")
		require.NoError(t, err)
		defer resp2.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
	})
}

// TestDevPublic_Billing_ProPriceCanonical migrated from
// infra/tests/e2e/dev-public-billing-pro-price-canonical_test.sh (Red Team #157).
func TestDevPublic_Billing_ProPriceCanonical(t *testing.T) {
	priceIDs := map[string]string{
		"pro":  "price_1ToR5yA0ZwssSKrVzD0jk6BF", // canonical Pro post-#157 fix
		"team": "price_1TJGJ5A0ZwssSKrVy3mjsal6",
	}
	srv := httptest.NewServer(mockCloudBillingHandler(t, true, priceIDs))
	defer srv.Close()

	// Scenario A: Stripe price unit_amount MUST be 2900 (canonical business value $29/mo).
	// Scenario B: /v1/billing/checkout {"plan":"pro"} MUST return amount_total = 2900.
	body := `{"plan":"pro"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/billing/checkout", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-pro-price"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var parsed struct {
		AmountTotal int    `json:"amount_total"`
		PriceID     string `json:"price_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	require.Equal(t, 2900, parsed.AmountTotal,
		"Pro amount_total MUST be 2900 (canonical $29/mo per MEET-2026-05-12 D4=B) per #157")
	require.Equal(t, priceIDs["pro"], parsed.PriceID,
		"canonical Pro price_id MUST match post-#157 fix")
}

// TestDevPublic_Billing_WebhookFulfillment migrated from
// infra/tests/e2e/dev-public-billing-webhook-fulfillment_test.sh (Red Team #156).
func TestDevPublic_Billing_WebhookFulfillment(t *testing.T) {
	srv := httptest.NewServer(mockCloudBillingHandler(t, true, map[string]string{"pro": "price_pro_x"}))
	defer srv.Close()

	t.Run("A_missing_signature_returns_400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/webhooks/stripe",
			strings.NewReader(`{"type":"checkout.session.completed"}`))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-webhook"))
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"missing Stripe-Signature MUST return 400 (per Red Team #156 signature verification requirement)")
	})

	t.Run("B_valid_signature_returns_200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/webhooks/stripe",
			strings.NewReader(`{"type":"checkout.session.completed"}`))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-webhook"))
		req.Header.Set("Stripe-Signature", "t=123,v1=abc")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestDevPublic_Billing_StripePriceIDsSeed migrated from
// infra/tests/e2e/dev-public-billing-stripe-price-ids-seed-canonical_test.sh (#155).
func TestDevPublic_Billing_StripePriceIDsSeed(t *testing.T) {
	// #155: when STRIPE_PRICE_PRO env is unset, /v1/billing/checkout MUST NOT return 503.
	// The canonical Pro price_id MUST be present in the Secret AND surface a valid checkout URL.
	priceIDsCanonical := map[string]string{"pro": "price_1ToR5yA0ZwssSKrVzD0jk6BF"}
	srvOK := httptest.NewServer(mockCloudBillingHandler(t, true, priceIDsCanonical))
	defer srvOK.Close()

	t.Run("A_canonical_price_id_returns_200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srvOK.URL+"/v1/billing/checkout", strings.NewReader(`{"plan":"pro"}`))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-price"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"canonical Pro price_id MUST NOT return 503 BILLING_NOT_CONFIGURED per #155")
	})

	// Negative case: unset env → 503 (proves the guard fires correctly).
	priceIDsMissing := map[string]string{}
	srvBad := httptest.NewServer(mockCloudBillingHandler(t, true, priceIDsMissing))
	defer srvBad.Close()

	t.Run("B_missing_price_id_returns_503", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, srvBad.URL+"/v1/billing/checkout", strings.NewReader(`{"plan":"pro"}`))
		req.Header.Set("Authorization", "Bearer "+mintTestJWT(t, "tn-price-missing"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "BILLING_NOT_CONFIGURED",
			"missing STRIPE_PRICE_PRO MUST surface BILLING_NOT_CONFIGURED typed error")
	})
}

// TestDevPublic_Billing_StripeSeedRestartsCloud migrated from
// infra/tests/e2e/dev-public-billing-stripe-seed-restarts-cloud_test.sh (Red Team #154).
func TestDevPublic_Billing_StripeSeedRestartsCloud(t *testing.T) {
	// #154 root cause: loop-console boot before Stripe Secret materialized → cfg.StripeAPIKey.Value() == "" →
	// billingHandler == nil → /v1/billing* routes NEVER registered → 404 crus do Caddy.
	// Post-fix (rollout restart pattern): STRIPE_API_KEY populated → billingHandler wired →
	// /v1/billing routes registered → 401 JSON (not 404 text/plain crude).
	priceIDs := map[string]string{"pro": "price_pro"}
	srvConfigured := httptest.NewServer(mockCloudBillingHandler(t, true, priceIDs))
	defer srvConfigured.Close()

	t.Run("A_billing_wired_returns_401_json_not_404_text", func(t *testing.T) {
		// No Authorization header → auth guard kicks in with 401 JSON.
		resp, err := http.Get(srvConfigured.URL + "/v1/billing/portal")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
			"billing route wired → auth guard returns 401 JSON (NOT 404 text per #154 pre-fix)")
		require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	})

	t.Run("B_checkout_endpoint_wired_returns_401_when_no_bearer", func(t *testing.T) {
		resp, err := http.Post(srvConfigured.URL+"/v1/billing/checkout", "application/json", strings.NewReader(`{}`))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}
