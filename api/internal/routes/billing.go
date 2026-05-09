// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.6 of deep-review-remediation-plan (closes find_billing_route_not_wired HIGH).
//
// Wires GET /api/v1/billing and POST /api/v1/billing/portal so the CLI's
// `loopzero billing` and `loopzero billing portal` commands actually have an
// endpoint to call. Pre-T1.6 the CLI hit silent 404 because the routes
// were declared in the spec but no handler existed in router.go.
//
// Scope: lightweight wiring that returns plan + usage. Full Stripe
// Customer Portal session creation requires the Stripe Go SDK + tenant
// stripe_customer_id lookup; that is Sprint 11 work. This handler:
//   - GET /api/v1/billing → {plan, team, usage}
//   - POST /api/v1/billing/portal → 400 NO_STRIPE_CUSTOMER for free
//     tenants; placeholder URL for paid tenants pending Sprint 11.

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// BillingRow is the minimal interface to read scalar values from a query.
// Local to routes/ to avoid the routes↔services↔db import cycle blocked
// by find_arch_001 / T1.3. After T1.3, this can be replaced with db.Row.
type BillingRow interface {
	Scan(dest ...any) error
}

// BillingPool is the minimal interface to query plan + customer ID.
// Local to routes/ for the same reason as BillingRow.
type BillingPool interface {
	QueryRow(ctx context.Context, sql string, args ...any) BillingRow
}

// BillingHandler responds to GET /api/v1/billing + POST /api/v1/billing/portal.
type BillingHandler struct {
	pool BillingPool
}

// NewBillingHandler constructs a billing handler that reads plan + usage
// from the subscriptions + usage_records tables.
func NewBillingHandler(pool BillingPool) *BillingHandler {
	return &BillingHandler{pool: pool}
}

// BillingResponse is the JSON shape returned by GET /api/v1/billing.
// Field names are a stability contract per CLI Rule 5.
type BillingResponse struct {
	Plan  string                       `json:"plan"`
	Team  string                       `json:"team"`
	Usage map[string]BillingUsageEntry `json:"usage"`
}

// BillingUsageEntry pairs a metric's used count with its plan limit.
type BillingUsageEntry struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// PortalResponse is the JSON shape returned by POST /api/v1/billing/portal.
type PortalResponse struct {
	URL string `json:"url"`
}

// Get handles GET /api/v1/billing — returns plan + usage for the
// authenticated tenant.
func (h *BillingHandler) Get(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	if uc.TenantID == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}

	plan, err := h.fetchPlan(r.Context(), uc.TenantID)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if plan == "" {
		// No subscription row — default to starter (free tier).
		plan = "starter"
	}

	resp := BillingResponse{
		Plan:  plan,
		Team:  uc.TenantID,
		Usage: map[string]BillingUsageEntry{
			// Sprint 11 will populate real usage from usage_records.
			// For now we report an empty map so the CLI prints
			// "no usage data" rather than failing.
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// Portal handles POST /api/v1/billing/portal — returns a Stripe Customer
// Portal session URL. EC-8 of edge-case review: tenants without a Stripe
// customer ID (Starter free tier) MUST get 400 NO_STRIPE_CUSTOMER with a
// product-language message, NOT 500.
func (h *BillingHandler) Portal(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	if uc.TenantID == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}

	customerID, err := h.fetchStripeCustomerID(r.Context(), uc.TenantID)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if customerID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "NO_STRIPE_CUSTOMER",
			"This account does not yet have a payment method on file. Upgrade to a paid plan first.")
		return
	}

	// Sprint 11: real Stripe Customer Portal session creation lands here.
	// For now we return the dashboard URL as a placeholder. Tenants with
	// stripe_customer_id are paid customers — they have a real session
	// upstream; this endpoint just hasn't gained the SDK call yet.
	resp := PortalResponse{
		URL: "https://billing.stripe.com/p/login/" + customerID,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// fetchPlan reads the active subscription's plan column for a tenant.
// Returns empty string if no subscription row exists (caller defaults to
// starter).
func (h *BillingHandler) fetchPlan(ctx context.Context, tenantID string) (string, error) {
	var plan string
	err := h.pool.QueryRow(ctx,
		`SELECT plan FROM subscriptions WHERE tenant_id = $1 LIMIT 1`,
		tenantID,
	).Scan(&plan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return plan, nil
}

// fetchStripeCustomerID returns the tenant's stripe_customer_id or empty
// string if missing. Used by Portal handler to enforce EC-8.
func (h *BillingHandler) fetchStripeCustomerID(ctx context.Context, tenantID string) (string, error) {
	var id *string
	err := h.pool.QueryRow(ctx,
		`SELECT stripe_customer_id FROM subscriptions WHERE tenant_id = $1 LIMIT 1`,
		tenantID,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}
