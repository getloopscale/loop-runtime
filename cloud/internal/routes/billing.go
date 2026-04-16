// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/billing"
)

// UsageMetric represents a single usage metric with current value and limit.
// Limit is nil when the metric is unlimited for the plan.
type UsageMetric struct {
	Used  int  `json:"used"`
	Limit *int `json:"limit"` // nil = unlimited
}

// BillingHandler handles billing-related endpoints.
// All endpoints require JWT (validated by auth.RequireJWT middleware).
type BillingHandler struct {
	db          *sql.DB
	stripe      *billing.StripeClient
	webhook     *billing.WebhookHandler
	provisioner *billing.Provisioner
	returnURL   string
}

// NewBillingHandler creates billing route handlers.
func NewBillingHandler(db *sql.DB, stripe *billing.StripeClient, webhook *billing.WebhookHandler, provisioner *billing.Provisioner, returnURL string) *BillingHandler {
	return &BillingHandler{
		db:          db,
		stripe:      stripe,
		webhook:     webhook,
		provisioner: provisioner,
		returnURL:   returnURL,
	}
}

// GetBilling returns the current plan and usage for the authenticated user.
// GET /v1/billing
func (h *BillingHandler) GetBilling(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	tenantID := user.TenantID

	// Lazy provisioning: ensure starter plan exists for this tenant
	if h.provisioner != nil {
		_ = h.provisioner.ProvisionStarter(r.Context(), user.Subject, user.Email, tenantID)
	}

	// Determine plan from DB (fallback to starter)
	plan := "starter"
	var periodStart, periodEnd *time.Time
	if h.db != nil {
		var ps, pe sql.NullTime
		err := h.db.QueryRowContext(r.Context(),
			"SELECT plan, current_period_start, current_period_end FROM subscriptions WHERE tenant_id = $1 AND status = 'active' LIMIT 1",
			tenantID,
		).Scan(&plan, &ps, &pe)
		if err != nil {
			plan = "starter"
		}
		if ps.Valid {
			periodStart = &ps.Time
		}
		if pe.Valid {
			periodEnd = &pe.Time
		}
	}

	limits := h.getLimitsForPlan(r, plan)
	usage := h.getUsageForTenant(r, tenantID)

	usageResponse := make(map[string]UsageMetric)
	for metric, limit := range limits {
		used := usage[metric]
		usageResponse[metric] = UsageMetric{Used: used, Limit: limit}
	}
	// Ensure all usage metrics appear even without configured limits
	for metric, used := range usage {
		if _, ok := usageResponse[metric]; !ok {
			usageResponse[metric] = UsageMetric{Used: used, Limit: nil}
		}
	}

	resp := map[string]interface{}{
		"plan":  billing.PlanDisplayName(plan),
		"team":  tenantID,
		"usage": usageResponse,
	}
	if periodStart != nil {
		resp["period_start"] = periodStart.Format(time.RFC3339)
	}
	if periodEnd != nil {
		resp["period_end"] = periodEnd.Format(time.RFC3339)
	}

	writeJSON(w, http.StatusOK, resp)
}

// Portal creates a Stripe Customer Portal session and returns the URL.
// GET /v1/billing/portal
func (h *BillingHandler) Portal(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	tenantID := user.TenantID

	if h.provisioner != nil && h.stripe != nil {
		customerID, err := h.provisioner.EnsureStripeCustomer(r.Context(), tenantID, user.Email, user.Name)
		if err == nil && customerID != "" {
			session, err := h.stripe.CreatePortalSession(customerID, h.returnURL+"/billing")
			if err == nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"url":     session.URL,
					"message": "Manage your plan, payment methods, and billing history.",
				})
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"url":     h.returnURL + "/billing",
		"message": "Manage your plan, payment methods, and billing history.",
	})
}

// Checkout creates a Stripe Checkout session for upgrading to a paid plan.
// POST /v1/billing/checkout  body: {"plan": "pro" | "team"}
func (h *BillingHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	var body struct {
		Plan string `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Plan == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Missing plan. Specify: pro or team")
		return
	}

	priceID := billing.LoadPlanConfig().PriceIDForPlan(body.Plan)
	if priceID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_PLAN", "Invalid plan. Available: pro, team")
		return
	}

	if h.provisioner == nil || h.stripe == nil {
		writeError(w, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "Billing is not configured.")
		return
	}

	customerID, err := h.provisioner.EnsureStripeCustomer(r.Context(), user.TenantID, user.Email, user.Name)
	if err != nil || customerID == "" {
		writeError(w, http.StatusInternalServerError, "BILLING_ERROR", "Failed to set up billing. Try again.")
		return
	}

	session, err := h.stripe.CreateCheckoutSession(
		customerID,
		priceID,
		h.returnURL+"/billing?success=true",
		h.returnURL+"/billing?canceled=true",
		user.TenantID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CHECKOUT_ERROR", "Failed to create checkout. Try again.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"url": session.URL,
	})
}

// Webhook handles incoming Stripe webhook events.
// POST /v1/webhooks/stripe — NO auth middleware (Stripe signs with its own secret).
func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body.")
		return
	}

	sig := r.Header.Get("Stripe-Signature")
	if err := billing.VerifySignature(body, sig, h.webhook.WebhookSecret()); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SIGNATURE", "Invalid webhook signature.")
		return
	}

	var event billing.StripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid webhook payload.")
		return
	}

	if err := h.webhook.ProcessEvent(event); err != nil {
		writeError(w, http.StatusInternalServerError, "PROCESSING_ERROR", "Failed to process webhook event.")
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"received":true}`))
}

// getLimitsForPlan returns limits per metric. nil value = unlimited (§1.5).
func (h *BillingHandler) getLimitsForPlan(r *http.Request, plan string) map[string]*int {
	v := func(n int) *int { return &n }
	defaults := map[string]*int{
		"projects": v(2), "builds_per_month": v(100), "environments": v(1),
		"seats": v(1), "secrets_per_env": v(10), "services_per_project": v(3),
	}
	if h.db == nil {
		return defaults
	}

	rows, err := h.db.QueryContext(r.Context(),
		"SELECT metric, hard_limit FROM plan_limits WHERE plan = $1", plan)
	if err != nil {
		return defaults
	}
	defer rows.Close()

	result := make(map[string]*int)
	for rows.Next() {
		var metric string
		var limit sql.NullInt64
		if err := rows.Scan(&metric, &limit); err != nil {
			continue
		}
		if limit.Valid {
			val := int(limit.Int64)
			result[metric] = &val
		} else {
			result[metric] = nil // NULL = unlimited
		}
	}
	if len(result) == 0 {
		return defaults
	}
	return result
}

func (h *BillingHandler) getUsageForTenant(r *http.Request, tenantID string) map[string]int {
	if h.db == nil {
		return map[string]int{}
	}

	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	rows, err := h.db.QueryContext(r.Context(),
		"SELECT metric, COALESCE(SUM(quantity), 0) FROM usage_records WHERE tenant_id = $1 AND period_start >= $2 GROUP BY metric",
		tenantID, periodStart)
	if err != nil {
		return map[string]int{}
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var metric string
		var qty int
		if err := rows.Scan(&metric, &qty); err != nil {
			continue
		}
		result[metric] = qty
	}
	return result
}
