// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/billing"
)

// BillingHandler handles billing-related endpoints.
type BillingHandler struct {
	stripe    *billing.StripeClient
	webhook   *billing.WebhookHandler
	validator *auth.JWKSValidator
	returnURL string
}

// NewBillingHandler creates billing route handlers.
func NewBillingHandler(stripe *billing.StripeClient, webhook *billing.WebhookHandler, validator *auth.JWKSValidator, returnURL string) *BillingHandler {
	return &BillingHandler{
		stripe:    stripe,
		webhook:   webhook,
		validator: validator,
		returnURL: returnURL,
	}
}

// GetBilling returns the current plan and usage for the authenticated user.
// GET /v1/billing
func (h *BillingHandler) GetBilling(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	claims, err := h.validator.Validate(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "Session expired. Run: loopzero login")
		return
	}

	// Return plan info — usage details will be added in Phase 7 (metering)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"plan":    billing.PlanDisplayName(claims.TenantID), // Will be from DB in production
		"team":    claims.TenantID,
		"usage":   map[string]interface{}{},
		"message": "Manage your plan at the billing portal.",
	})
}

// Portal creates a Stripe Customer Portal session and returns the URL.
// GET /v1/billing/portal
func (h *BillingHandler) Portal(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Not authenticated. Run: loopzero login")
		return
	}

	_, err := h.validator.Validate(token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "Session expired. Run: loopzero login")
		return
	}

	// For now, return a placeholder — real portal session requires Stripe customer ID
	// which will be available after tenant auto-creation wires Stripe customer.
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
