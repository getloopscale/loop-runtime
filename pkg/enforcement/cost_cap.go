// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"

	"github.com/loopzero/loopzero/pkg/plan"
)

// T1.9 of plans/2026-05-12-launch-readiness-waves-plan.md (v1.4).
//
// Per-tenant cost cap enforcement. Distinct from PlanLimiter (which gates
// rate-style limits like builds-per-month and concurrent deploys): CostCap
// gates *quantity* metrics (bytes egressed, registry pulls, build minutes)
// against per-plan caps. Both share the HTTP 402 "Payment Required"
// surfacing pattern.
//
// M1.2 (plan m1-decouple-in-place D2/D4): the VALUE VOCABULARY (CostMetric,
// Plan, CostCounter, CapFor, Unlimited, ExceededError, ErrCostCapExceeded)
// moved to pkg/plan — the contracts-bound surface. The aliases below are
// transitional re-exports (strangler-fig) so existing call-sites stay GREEN
// while they migrate to pkg/plan directly; cap-enforcement LOGIC (CostStore,
// CapChecker) stays here (ISP).

// --- transitional vocabulary aliases (re-export from pkg/plan) ---

// CostMetric identifies the kind of cost being tracked. Alias of plan.CostMetric.
type CostMetric = plan.CostMetric

// Canonical cost metrics (re-exported).
const (
	MetricEgressBytes  = plan.MetricEgressBytes
	MetricDOCRPulls    = plan.MetricDOCRPulls
	MetricBuildMinutes = plan.MetricBuildMinutes
)

// Plan identifies a subscription tier. Alias of plan.Plan.
type Plan = plan.Plan

// Canonical plan tiers (re-exported).
const (
	PlanStarter    = plan.PlanStarter
	PlanPro        = plan.PlanPro
	PlanTeam       = plan.PlanTeam
	PlanEnterprise = plan.PlanEnterprise
)

// Unlimited is the sentinel for "no cap" (re-exported).
const Unlimited = plan.Unlimited

// CapFor returns the cap for (plan, metric) (re-exported).
var CapFor = plan.CapFor

// CostCounter is the read-side projection (alias of plan.CostCounter).
type CostCounter = plan.CostCounter

// ErrCostCapExceeded is the sentinel (re-exported).
var ErrCostCapExceeded = plan.ErrCostCapExceeded

// ExceededError carries HTTP 402 context (alias of plan.ExceededError) — a
// TRUE alias so errors.As(err, &enforcement.ExceededError{}) keeps matching.
type ExceededError = plan.ExceededError

// --- cap-enforcement logic (stays engine-side, ISP) ---

// CostStore is the DIP interface enforcement uses to read counters.
// Production implementation lives in api/internal/db (or services); tests
// implement in-memory.
type CostStore interface {
	// GetCounter returns the current counter value for (tenant, metric)
	// in the current period. Returns (counter, true, nil) on hit,
	// (zero, false, nil) on miss (no counter row yet — treat as 0),
	// (zero, false, err) on DB error.
	GetCounter(ctx context.Context, tenantID string, metric CostMetric) (CostCounter, bool, error)
}

// CapChecker is the high-level enforcement type. Consumes a CostStore and
// returns ExceededError when a counter has reached its cap.
type CapChecker struct {
	store CostStore
}

// NewCapChecker constructs a CapChecker bound to a store.
func NewCapChecker(store CostStore) *CapChecker {
	return &CapChecker{store: store}
}

// Check verifies that the tenant is under cap for (metric). Returns nil
// when allowed; *ExceededError when blocked.
//
// Semantics:
//   - Counter missing (no row yet) → ALLOW (current value 0).
//   - Cap = Unlimited (Enterprise or unknown plan) → ALLOW.
//   - Counter value < cap → ALLOW.
//   - Counter value >= cap → BLOCK with ExceededError.
//
// Note the >= : reaching the cap blocks the NEXT mutation.
func (c *CapChecker) Check(ctx context.Context, tenantID string, p Plan, metric CostMetric) error {
	counter, found, err := c.store.GetCounter(ctx, tenantID, metric)
	if err != nil {
		// Fail-open on store errors. A noisy aggregator should not block
		// production traffic. The middleware logs the error.
		return nil
	}

	cap := CapFor(p, metric)
	if cap == Unlimited {
		return nil
	}

	// Missing counter row = 0 usage so far this period = allow.
	if !found {
		return nil
	}

	if counter.Value >= cap {
		return &ExceededError{
			TenantID: tenantID,
			Plan:     p,
			Metric:   metric,
			Cap:      cap,
			Current:  counter.Value,
			Period:   formatPeriod(counter.PeriodStart),
		}
	}

	return nil
}

// formatPeriod converts a unix-seconds period start into "YYYY-MM" UTC.
func formatPeriod(unixSec int64) string {
	return formatPeriodImpl(unixSec)
}
