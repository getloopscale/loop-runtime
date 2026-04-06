// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/billing"
)

// UsageMetric represents a single usage metric with current value and limit.
type UsageMetric struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// billingUser holds authenticated user info extracted from JWT.
type billingUser struct {
	Subject  string
	Email    string
	Name     string
	TenantID string
}

// BillingHandler handles billing-related endpoints.
// All endpoints require JWT (from Hydra for CLI, or session token for dashboard).
type BillingHandler struct {
	db           *sql.DB
	stripe       *billing.StripeClient
	webhook      *billing.WebhookHandler
	provisioner  *billing.Provisioner
	jwtValidator *auth.JWKSValidator
	sessValidator *auth.SessionTokenIssuer
	returnURL    string
}

// NewBillingHandler creates billing route handlers.
func NewBillingHandler(db *sql.DB, stripe *billing.StripeClient, webhook *billing.WebhookHandler, provisioner *billing.Provisioner, jwtValidator *auth.JWKSValidator, sessValidator *auth.SessionTokenIssuer, returnURL string) *BillingHandler {
	return &BillingHandler{
		db:           db,
		stripe:       stripe,
		webhook:      webhook,
		provisioner:  provisioner,
		jwtValidator: jwtValidator,
		sessValidator: sessValidator,
		returnURL:    returnURL,
	}
}

// authenticateRequest validates the Bearer JWT token.
// Tries Hydra JWKS first (CLI tokens), then HMAC session tokens (dashboard).
func (h *BillingHandler) authenticateRequest(r *http.Request) (*billingUser, error) {
	token := extractBearerToken(r)
	if token == "" {
		return nil, fmt.Errorf("not authenticated")
	}

	// Try 1: Hydra JWT (CLI — RSA signed)
	if h.jwtValidator != nil {
		claims, err := h.jwtValidator.Validate(token)
		if err == nil {
			email := claims.Email
			if email == "" { email = claims.Ext.Email }
			name := claims.Name
			if name == "" { name = claims.Ext.Name }
			tenantID := claims.TenantID
			if tenantID == "" { tenantID = "default" }
			return &billingUser{Subject: claims.RegisteredClaims.Subject, Email: email, Name: name, TenantID: tenantID}, nil
		}
	}

	// Try 2: Session token (dashboard — HMAC signed)
	if h.sessValidator != nil {
		claims, err := h.sessValidator.Validate(token)
		if err == nil {
			tenantID := claims.TenantID
			if tenantID == "" { tenantID = "default" }
			return &billingUser{Subject: claims.RegisteredClaims.Subject, Email: claims.Email, Name: claims.Name, TenantID: tenantID}, nil
		}
	}

	return nil, fmt.Errorf("invalid token")
}

// GetBilling returns the current plan and usage for the authenticated user.
// GET /v1/billing
func (h *BillingHandler) GetBilling(w http.ResponseWriter, r *http.Request) {
	user, err := h.authenticateRequest(r)
	if err != nil {
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
			plan = "starter" // No subscription row — default to starter
		}
		if ps.Valid {
			periodStart = &ps.Time
		}
		if pe.Valid {
			periodEnd = &pe.Time
		}
	}

	// Get limits from plan_limits table
	limits := h.getLimitsForPlan(r, plan)

	// Get usage from usage_records table
	usage := h.getUsageForTenant(r, tenantID)

	// Build response: merge usage with limits
	usageResponse := make(map[string]UsageMetric)
	for metric, limit := range limits {
		used := usage[metric]
		usageResponse[metric] = UsageMetric{Used: used, Limit: limit}
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
	user, err := h.authenticateRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	tenantID := user.TenantID

	// Lazy-create Stripe customer on first portal visit
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

	// Fallback: return dashboard billing page
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"url":     h.returnURL + "/billing",
		"message": "Manage your plan, payment methods, and billing history.",
	})
}

// Webhook handles incoming Stripe webhook events.
// POST /v1/webhooks/stripe
func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body.")
		return
	}

	// Verify Stripe signature
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

// getLimitsForPlan queries plan_limits table for the given plan.
// Returns map[metric]hard_limit. Falls back to starter defaults if DB unavailable.
func (h *BillingHandler) getLimitsForPlan(r *http.Request, plan string) map[string]int {
	defaults := map[string]int{
		"projects": 2, "builds_per_month": 100, "environments": 1,
		"seats": 1, "secrets_per_env": 10, "services_per_project": 3,
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

	result := make(map[string]int)
	for rows.Next() {
		var metric string
		var limit int
		if err := rows.Scan(&metric, &limit); err != nil {
			continue
		}
		result[metric] = limit
	}
	if len(result) == 0 {
		return defaults
	}
	return result
}

// getUsageForTenant queries usage_records for the current billing period.
// Returns map[metric]quantity.
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
