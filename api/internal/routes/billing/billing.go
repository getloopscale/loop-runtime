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
//       * 200 with legacy placeholder URL when SDK is unwired (local dev)
//       * 502 BILLING_PROVIDER_UNAVAILABLE if Stripe API returns error

// Package billing serves the billing endpoints under /api/v1/billing: the
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
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/routes/httpio"

	"github.com/jackc/pgx/v5"

	planpkg "github.com/getloopscale/loopzero-contracts/plan"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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
	pool            BillingPool
	costSource      enforcement.CostStore // optional — T1.9 cost cap usage display
	portalCreator   PortalSessionCreator  // optional — T2.7 Stripe SDK; nil falls back to placeholder
	portalReturnURL string                // optional — where Stripe sends customer post-portal-close
	logger          *slog.Logger
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

	plan, err := h.fetchPlan(r.Context(), uc.TenantID)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
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

	customerID, err := h.fetchStripeCustomerID(r.Context(), uc.TenantID)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if customerID == "" {
		httpio.WriteErrorJSON(w, http.StatusBadRequest, "NO_STRIPE_CUSTOMER",
			"This account does not yet have a payment method on file. Upgrade to a paid plan first.")
		return
	}

	// SEM EMISSOR, NAO HA URL. O caminho anterior montava
	// `https://billing.stripe.com/p/login/` + customerID e devolvia isso como se fosse a
	// sessao do portal. Nao e sessao emitida pelo Stripe: e uma URL construida com o
	// identificador de cobranca do cliente dentro, que trafega em historico de navegador,
	// log de proxy e barra de enderecos.
	//
	// E nao era hipotese. Medido no app-dev em 2026-08-11: o Secret `loopzero-api-stripe` e
	// `optional: true` e NAO EXISTE, entao este era o caminho ATIVO — todo
	// `loopzero billing portal` devolvia a URL construida. O comentario original dizia que o
	// fallback servia para "local dev + tests"; a intencao era local, o efeito era publico.
	//
	// Por ADR 2026-08-11, o loopzero-cloud e o unico emissor de sessao de portal. Quando o
	// engine nao pode emitir, ele FALHA dizendo onde ir — uma URL que PARECE funcionar
	// esconde o problema ate alguem investigar por que o portal nao abriu.
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
