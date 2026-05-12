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
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/pkg/enforcement"
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
	pool       BillingPool
	costSource enforcement.CostStore // optional — T1.9 cost cap usage display
	logger     *slog.Logger
}

// NewBillingHandler constructs a billing handler that reads plan + usage
// from the subscriptions + usage_records tables.
func NewBillingHandler(pool BillingPool) *BillingHandler {
	return &BillingHandler{pool: pool, logger: slog.Default()}
}

// WithCostSource wires the cost cap counter source so GET /api/v1/billing
// returns the 3 T1.9 metrics (egress_bytes, docr_pulls, build_minutes)
// alongside their per-plan caps. Nil-safe — when not set, the usage map
// stays empty and CLI displays "no usage data".
func (h *BillingHandler) WithCostSource(src enforcement.CostStore) *BillingHandler {
	h.costSource = src
	return h
}

// BillingResponse is the JSON shape returned by GET /api/v1/billing.
// Field names are a stability contract per CLI Rule 5.
type BillingResponse struct {
	Plan  string                       `json:"plan"`
	Team  string                       `json:"team"`
	Usage map[string]BillingUsageEntry `json:"usage"`
}

// BillingUsageEntry pairs a metric's used count with its plan limit.
//
// CLI Rule 5 stability contract: field names are permanent. New fields
// can be added (e.g., `period` once usage is per-period from cost_counters)
// but `used` + `limit` are forever int. The CLI's billing.ts treats
// `limit >= 999999` as "unlimited" — keep that sentinel for back-compat
// when the metric is Enterprise/unbounded.
type BillingUsageEntry struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}

// unlimitedDisplay is the CLI's sentinel for unbounded caps (see billing.ts).
// Picked at int range that fits int64 caps too — 100 GB egress fits in int.
const unlimitedDisplay = 999999

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
		Usage: h.buildUsage(r.Context(), uc.TenantID, plan),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// buildUsage populates the 3 T1.9 cost cap metrics when costSource is wired.
// Returns empty map (preserving pre-T1.9 behavior) when costSource is nil
// or when every read fails — CLI shows "no usage data" gracefully.
//
// `used` is the current period counter value clamped to int range. `limit`
// is the per-plan cap (or `unlimitedDisplay` sentinel for Enterprise/unset).
// CostStore read errors are logged and the metric is skipped — fail-open
// matches the enforcement middleware posture (degraded aggregator must not
// break the billing display).
func (h *BillingHandler) buildUsage(ctx context.Context, tenantID, plan string) map[string]BillingUsageEntry {
	usage := make(map[string]BillingUsageEntry)
	if h.costSource == nil {
		return usage
	}

	planTyped := enforcement.Plan(plan)
	for _, metric := range []enforcement.CostMetric{
		enforcement.MetricEgressBytes,
		enforcement.MetricDOCRPulls,
		enforcement.MetricBuildMinutes,
	} {
		counter, found, err := h.costSource.GetCounter(ctx, tenantID, metric)
		if err != nil {
			h.logger.Warn("billing: cost counter read failed; skipping metric",
				"tenant", tenantID,
				"metric", string(metric),
				"error", err)
			continue
		}

		cap := enforcement.CapFor(planTyped, metric)
		limit := int(cap)
		if cap == enforcement.Unlimited {
			limit = unlimitedDisplay
		}
		used := 0
		if found {
			// Clamp to int — usage at 10 TB egress fits comfortably (10^13).
			if counter.Value > int64(int(^uint(0)>>1)) {
				used = int(^uint(0) >> 1) // MaxInt
			} else {
				used = int(counter.Value)
			}
		}
		usage[string(metric)] = BillingUsageEntry{Used: used, Limit: limit}
	}
	return usage
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
