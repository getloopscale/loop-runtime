// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.6 of deep-review-remediation-plan (closes find_billing_route_not_wired HIGH)
// + T2.7 of maturity-3-62-to-4-0-roadmap (Stripe Portal SDK).
//
// Wires GET /api/v1/billing and POST /api/v1/billing/portal so the CLI's
// `loopzero billing` and `loopzero billing portal` commands actually have an
// endpoint to call. Pre-T1.6 the CLI hit silent 404 because the routes
// were declared in the spec but no handler existed in router.go.
//
// Behavior:
//   - GET /api/v1/billing → {plan, team, usage}
//   - POST /api/v1/billing/portal:
//       * 400 NO_STRIPE_CUSTOMER for free tenants (no stripe_customer_id)
//       * 200 with real one-time-use Stripe Portal URL when SDK is wired
//         (production with STRIPE_SECRET_KEY env present)
//       * 503 BILLING_PORTAL_UNAVAILABLE when no portal issuer is wired
//       * 502 BILLING_PROVIDER_UNAVAILABLE if Stripe API returns error

// Package billinghttp serves the billing endpoints under /api/v1/billing: the
// checkout session, the customer portal, and the cost/usage read side.
//
// Seventh family to leave the flat `routes` package (B-080, slice-30).
//
// Extracting it removed a hand-rolled `strings.Contains` that had been declared
// in this family's test file and reached by two tests of the parent — a helper
// nobody would have written on purpose, kept alive by living in a package big
// enough that its declarations were invisible. Splitting the package is what
// made it visible; the stdlib replaced it in all three call sites.
//
// Depends on two leaves and no sibling family: `httpio` for the response
// writers, `routestest` for request helpers, and that one only from _test.go.
package billinghttp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/plantier"

	planpkg "github.com/getloopscale/loopzero-contracts/plan"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	pkgdomain "github.com/getloopscale/loopzero/pkg/domain"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// StripeCustomerStore is the port for the tenant's billing customer id.
// ("", nil) means the tenant has no subscription carrying one.
type StripeCustomerStore interface {
	StripeCustomerID(ctx context.Context, tenantID string) (string, error)
}

// PortalSessionCreator is the port that BillingHandler.Portal depends
// on for minting Stripe Customer Portal session URLs. T2.7 of
// maturity-3-62-to-4-0-roadmap planpkg. Production wiring uses
// billing.StripePortalSessionCreator (real SDK call). When the field
// is nil, the handler falls back to the legacy placeholder URL — this
// preserves back-compat for local dev + test environments without a
// Stripe key configured.
//
// DIP per CLAUDE.md Rule 13.5: handler depends on this small interface
// rather than the Stripe SDK directly, so (a) tests stay hermetic
// without mock-of-stripe-internals and (b) a future provider swap
// (Paddle, Lemon Squeezy) is a one-file change.
type PortalSessionCreator interface {
	CreateSession(ctx context.Context, customerID, returnURL string) (string, error)
}

// BillingHandler responds to GET /api/v1/billing + POST /api/v1/billing/portal.
type BillingHandler struct {
	plans           pkgdomain.PlanTierStore
	customers       StripeCustomerStore
	costSource      enforcement.CostStore // optional — T1.9 cost cap usage display
	portalCreator   PortalSessionCreator  // optional — T2.7 Stripe SDK; nil falls back to placeholder
	portalReturnURL string                // optional — where Stripe sends customer post-portal-close
	logger          *slog.Logger
}

// NewBillingHandler constructs a billing handler. plans is the plan read every
// plan gate shares (the tier shown is the tier enforced); customers resolves
// the billing customer the portal is opened for.
func NewBillingHandler(plans pkgdomain.PlanTierStore, customers StripeCustomerStore) *BillingHandler {
	return &BillingHandler{plans: plans, customers: customers, logger: slog.Default()}
}

// WithCostSource wires the cost cap counter source so GET /api/v1/billing
// returns the 3 T1.9 metrics (egress_bytes, docr_pulls, build_minutes)
// alongside their per-plan caps. Nil-safe — when not set, the usage map
// stays empty and CLI displays "no usage data".
func (h *BillingHandler) WithCostSource(src enforcement.CostStore) *BillingHandler {
	h.costSource = src
	return h
}

// WithPortalSessionCreator wires the Stripe Customer Portal SDK so
// POST /api/v1/billing/portal returns real one-time-use session URLs
// (TTL=30min) instead of the placeholder login URL. T2.7 of
// maturity-3-62-to-4-0-roadmap. Pass returnURL = where Stripe sends
// the customer when they close the portal tab (typically dashboard
// /billing path). Nil/empty preserves legacy placeholder behavior.
func (h *BillingHandler) WithPortalSessionCreator(creator PortalSessionCreator, returnURL string) *BillingHandler {
	h.portalCreator = creator
	h.portalReturnURL = returnURL
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
//
// `measured` says whether `used` is a MEASUREMENT or an ABSENCE. It is the
// added field B-033 needed on the read side: with the cost aggregator off,
// `cost_counters` holds zero rows, and every metric used to be served as
// `{"used": 0}` — a protection that has never evaluated anything, rendered as
// a customer who has consumed nothing. The gate already tells operators apart
// via `loopzero_cost_cap_decisions_total{reason="unmeasured"}`; this tells the
// reader of the response.
//
// Polarity is deliberate: Go's zero value is `false`, so any decoder that
// ignores the field reads the PESSIMISTIC claim ("not measured"), never the
// optimistic one. The failure mode this item exists for is an inert
// protection that looks healthy.
type BillingUsageEntry struct {
	Used     int  `json:"used"`
	Limit    int  `json:"limit"`
	Measured bool `json:"measured"`
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
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	if uc.TenantID == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}

	tier, terr := plantier.Resolve(r.Context(), h.plans, uc.TenantID)
	if terr != nil {
		httpio.WriteLoopZeroError(w, terr)
		return
	}
	plan := tier.String()

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
//
// `measured` carries the store's `found` flag straight through (B-033). A
// missing counter row still displays `used: 0`, because zero is what the
// tenant is charged for; what changes is that the response no longer CLAIMS
// the zero was measured. With the aggregator disabled — the state measured on
// app-dev on 2026-08-11 and again on 2026-09-05, `cost_counters` at 0 rows —
// every metric took this branch and the panel read "0 of 100 used" for a cap
// that had never evaluated anything.
func (h *BillingHandler) buildUsage(ctx context.Context, tenantID, plan string) map[string]BillingUsageEntry {
	usage := make(map[string]BillingUsageEntry)
	if h.costSource == nil {
		return usage
	}

	planTyped := planpkg.Plan(plan)
	for _, metric := range []planpkg.CostMetric{
		planpkg.MetricEgressBytes,
		planpkg.MetricDOCRPulls,
		planpkg.MetricBuildMinutes,
	} {
		counter, found, err := h.costSource.GetCounter(ctx, tenantID, metric)
		if err != nil {
			h.logger.Warn("billing: cost counter read failed; skipping metric",
				"tenant", tenantID,
				"metric", string(metric),
				"error", err)
			continue
		}

		cap := planpkg.CapFor(planTyped, metric)
		limit := int(cap)
		if cap == planpkg.Unlimited {
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
		usage[string(metric)] = BillingUsageEntry{Used: used, Limit: limit, Measured: found}
	}
	return usage
}

// Portal handles POST /api/v1/billing/portal — returns a Stripe Customer
// Portal session URL. EC-8 of edge-case review: tenants without a Stripe
// customer ID (Starter free tier) MUST get 400 NO_STRIPE_CUSTOMER with a
// product-language message, NOT 500.
func (h *BillingHandler) Portal(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	if uc.TenantID == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden)
		return
	}

	customerID, err := h.customers.StripeCustomerID(r.Context(), uc.TenantID)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "billing: customer lookup failed", "tenant", uc.TenantID, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if customerID == "" {
		httpio.WriteErrorJSON(w, http.StatusBadRequest, "NO_STRIPE_CUSTOMER",
			"This account does not yet have a payment method on file. Upgrade to a paid plan first.")
		return
	}

	// NO ISSUER, NO URL. The previous path assembled
	// `https://billing.stripe.com/p/login/` + customerID and returned it as the
	// portal session. It is not a session the provider issued: it is a URL with
	// the customer's billing identifier inside, travelling through browser
	// history, proxy logs and the address bar.
	//
	// Not hypothetical. Measured on app-dev 2026-08-11: the provider Secret is
	// `optional: true` and absent, so this was the ACTIVE path — every
	// `loopzero billing portal` returned the assembled URL.
	//
	// Per ADR 2026-08-11, loopzero-cloud is the only portal-session issuer. When the
	// engine cannot issue, it FAILS saying where to go — a URL that looks like it
	// works hides the problem until someone investigates why the portal did not open.
	if h.portalCreator == nil {
		httpio.WriteErrorJSON(w, http.StatusServiceUnavailable, "BILLING_PORTAL_UNAVAILABLE",
			"Billing is managed from the dashboard. Open it at "+h.portalReturnURL+"/billing")
		return
	}
	var url string
	{
		realURL, sdkErr := h.portalCreator.CreateSession(r.Context(), customerID, h.portalReturnURL)
		if sdkErr != nil {
			h.logger.Error("billing: portal session create failed",
				"tenant", uc.TenantID,
				"customer", customerID,
				"error", sdkErr)
			httpio.WriteErrorJSON(w, http.StatusBadGateway, "BILLING_PROVIDER_UNAVAILABLE",
				"The billing provider is temporarily unavailable. Try again in a few moments.")
			return
		}
		url = realURL
	}

	resp := PortalResponse{URL: url}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
