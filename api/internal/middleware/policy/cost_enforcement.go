// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package policy holds the request gates that do not turn on who is calling.
//
// A plan's cost cap (this file), a replayed idempotency key (idempotency.go),
// a maintenance window (maintenance.go), a database pool that has stopped
// answering (pgcircuitbreaker.go), and the security headers every response
// must carry (security_headers.go): each is a rule the transport applies to a
// request whose identity is already settled.
//
// Identity itself lives in auth, which this package imports and which imports
// nothing back. Quotas live in ratelimit rather than here, because a quota is
// counted over a window instead of decided per request (B-059).
package policy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"

	planpkg "github.com/getloopscale/contracts/plan"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
)

// CostEnforcement is the HTTP middleware that consults a CapChecker
// before allowing mutations. Returns HTTP 402 Payment Required when a
// tenant has reached its per-metric cap.
//
// T1.9 of plans/2026-05-12-launch-readiness-waves-plan.md (v1.4).
//
// Each protected route maps to a single metric:
//
//   - POST /api/v1/builds           → MetricBuildMinutes (rejected at submit time;
//                                      actual minutes accrue during execution)
//   - POST /api/v1/source/upload    → MetricEgressBytes (proxy proxy for app upload size)
//
// Other routes are not currently gated by cost cap (rate-style PlanLimiter
// remains responsible for projects/builds/seats — see pkg/enforcement/limiter.go).
//
// Wire via api/cmd/setup_server.go AFTER Auth middleware so UserContext is
// available.

// PlanReader resolves the current plan for a tenant. Avoids tight coupling
// to the subscriptions store at the middleware layer.
type PlanReader interface {
	GetTenantPlan(ctx context.Context, tenantID string) (planpkg.Plan, error)
}

// CostEnforcer composes a CapChecker with a PlanReader to make a
// middleware-ready enforcement gate.
type CostEnforcer struct {
	checker *enforcement.CapChecker
	plans   PlanReader
	logger  *slog.Logger
}

// NewCostEnforcer constructs the gate.
func NewCostEnforcer(checker *enforcement.CapChecker, plans PlanReader, logger *slog.Logger) *CostEnforcer {
	if logger == nil {
		logger = slog.Default()
	}
	return &CostEnforcer{checker: checker, plans: plans, logger: logger}
}

// Middleware returns an http.Handler middleware that gates the given
// metric. Pass a different middleware instance for each route+metric pair.
//
// Nil-safe: when the receiver is nil, the middleware is a no-op pass-through.
// This keeps boot-time wiring tolerant during the rollout window.
func (e *CostEnforcer) Middleware(metric planpkg.CostMetric) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// nil receiver = pass-through. Lets cmd/setup_server.go wire
			// CostEnforcement before the cloud control plane is fully up.
			//
			// B-033: passing through is still right — blocking here would break the
			// tolerant boot this bypass exists to allow. What was wrong was passing
			// SILENTLY: the decision is emitted with `reason="disabled"`, so that "the
			// protection does not exist" stops being indistinguishable from "there
			// was no request" — system state, not a log line at another process's boot.
			if e == nil || e.checker == nil {
				metrics.CostCapDecisionsTotal.WithLabelValues(string(metric), "disabled").Inc()
				next.ServeHTTP(w, r)
				return
			}

			uc := auth.GetUserContext(r.Context())
			if uc == nil {
				// Defense in depth — Auth middleware should run first. If
				// somehow not, deny.
				writeCostError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required.")
				return
			}

			plan, err := e.plans.GetTenantPlan(r.Context(), uc.TenantID)
			if err != nil {
				e.logger.Warn("cost enforcer: plan lookup failed; failing open",
					"tenant", uc.TenantID,
					"error", err)
				next.ServeHTTP(w, r)
				return
			}

			reason, err := e.checker.CheckReason(r.Context(), uc.TenantID, plan, metric)

			// B-033: the decision becomes observable state. A high `unmeasured` count
			// means the protection is evaluating nothing — the state measured on
			// app-dev, where the aggregator is off and `cost_counters` has zero rows.
			// Before this, the only signal was a log line at ANOTHER process's boot.
			metrics.CostCapDecisionsTotal.WithLabelValues(string(metric), reason.String()).Inc()

			if err != nil {
				var ex *planpkg.ExceededError
				if errors.As(err, &ex) {
					writeCostExceeded(w, ex, e.logger)
					return
				}
				// Non-ExceededError errors fail open (same posture as Check).
				e.logger.Warn("cost enforcer: check returned unexpected error; failing open",
					"tenant", uc.TenantID,
					"metric", string(metric),
					"error", err)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeCostExceeded surfaces the cap exceedance as HTTP 402 with the
// product-language message expected by CLI.
//
// Message follows the upgrade-call-to-action pattern from PlanLimiter:
// the user receives an actionable next step, not a technical dump.
func writeCostExceeded(w http.ResponseWriter, ex *planpkg.ExceededError, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)

	body := map[string]any{
		"code":    "COST_CAP_EXCEEDED",
		"message": friendlyCostMessage(ex),
		// machine-readable fields for CLI to render structured output:
		"metric":      string(ex.Metric),
		"cap":         ex.Cap,
		"current":     ex.Current,
		"period":      ex.Period,
		"plan":        string(ex.Plan),
		"upgrade_url": "https://getloopscale.com/pricing",
	}
	if err := json.NewEncoder(w).Encode(body); err != nil && logger != nil {
		logger.Warn("cost enforcer: write response failed", "error", err)
	}
}

// friendlyCostMessage returns a product-language message tailored to the
// metric. Plan downgrade case (EC-NEW-13b T1.9 flavor) is mentioned only
// when current > cap × 1.5 (heuristic — sustained overage suggests
// downgrade, not in-period spike).
func friendlyCostMessage(ex *planpkg.ExceededError) string {
	var b strings.Builder
	b.WriteString("Your team has reached the ")

	switch ex.Metric {
	case planpkg.MetricEgressBytes:
		b.WriteString("egress bandwidth limit for the current billing period (")
	case planpkg.MetricDOCRPulls:
		b.WriteString("registry pull limit for the current billing period (")
	case planpkg.MetricBuildMinutes:
		b.WriteString("build minutes limit for the current billing period (")
	default:
		b.WriteString("usage limit for the current billing period (")
	}

	b.WriteString(ex.Period)
	b.WriteString("). Upgrade your plan or wait for the next cycle. ")
	if ex.Cap > 0 && ex.Current > ex.Cap+ex.Cap/2 {
		// Heuristic: 1.5× cap or more usually indicates plan downgrade
		// mid-period. EC-NEW-13b documents the non-retroactive reset.
		b.WriteString("Note: if you recently changed plans, the cap reflects your current plan; usage from before the change is not refunded.")
	}
	return b.String()
}

// writeCostError emits a generic JSON error envelope for non-402 paths.
func writeCostError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
