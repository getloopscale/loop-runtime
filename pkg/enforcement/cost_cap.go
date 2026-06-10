// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"errors"
	"fmt"
)

// T1.9 of plans/2026-05-12-launch-readiness-waves-plan.md (v1.4).
//
// Per-tenant cost cap enforcement. Distinct from PlanLimiter (which gates
// rate-style limits like builds-per-month and concurrent deploys): CostCap
// gates *quantity* metrics (bytes egressed, registry pulls, build minutes)
// against per-plan caps. Both share the HTTP 402 "Payment Required"
// surfacing pattern.
//
// Design decision (MEET-ASYNC-AMENDMENT-2026-05-12-001 A003): a separate
// `cost_counters` table from `usage_records`. See migration 000017.

// CostMetric identifies the kind of cost being tracked.
type CostMetric string

// Canonical cost metrics. Schema check constraint in migration_000017
// pins these — adding a new metric requires both the constant + the SQL
// CHECK clause to be updated.
const (
	MetricEgressBytes  CostMetric = "egress_bytes"
	MetricDOCRPulls    CostMetric = "docr_pulls"
	MetricBuildMinutes CostMetric = "build_minutes"
)

// Plan identifies a subscription tier. Mirrors subscriptions.plan column.
type Plan string

// Canonical plan tiers. LoopZero's pricing per MEET-2026-05-12 D4.
const (
	PlanStarter    Plan = "starter"
	PlanPro        Plan = "pro"
	PlanTeam       Plan = "team"
	PlanEnterprise Plan = "enterprise"
)

// Unlimited is the sentinel value for "no cap" on a metric. Returned for
// Enterprise tier and for metrics not yet gated on a given plan.
const Unlimited int64 = -1

// CapTable maps (plan, metric) → cap. Per MEET-2026-05-12 launch readiness
// plan T1.9. Values are integers in metric units:
//
//   - egress_bytes:  bytes per period
//   - docr_pulls:    count per period
//   - build_minutes: minutes per period
//
// Period is calendar month (UTC) — see CurrentPeriod().
//
// To raise a cap or add a metric, update both:
//   - this table
//   - CLAUDE.md § "Plan caps"
//   - cli/src/commands/billing/usage.ts (cached upper bound for display)
var capTable = map[Plan]map[CostMetric]int64{
	PlanStarter: {
		MetricEgressBytes:  100 * 1024 * 1024 * 1024,        // 100 GB
		MetricDOCRPulls:    1_000,                           // 1k
		MetricBuildMinutes: 100,                             // 100 min
	},
	PlanPro: {
		MetricEgressBytes:  1024 * 1024 * 1024 * 1024,       // 1 TB
		MetricDOCRPulls:    10_000,                          // 10k
		MetricBuildMinutes: 1_000,                           // 1000 min
	},
	PlanTeam: {
		MetricEgressBytes:  10 * 1024 * 1024 * 1024 * 1024,  // 10 TB
		MetricDOCRPulls:    100_000,                         // 100k
		MetricBuildMinutes: 10_000,                          // 10000 min
	},
	PlanEnterprise: {
		MetricEgressBytes:  Unlimited,
		MetricDOCRPulls:    Unlimited,
		MetricBuildMinutes: Unlimited,
	},
}

// CapFor returns the cap for (plan, metric). Returns Unlimited (-1) if the
// plan or metric is unknown — fail-open is the safer default during
// rollout; the aggregator + middleware never block a request because a
// new metric was added without a plan entry.
func CapFor(plan Plan, metric CostMetric) int64 {
	planCaps, ok := capTable[plan]
	if !ok {
		return Unlimited
	}
	cap, ok := planCaps[metric]
	if !ok {
		return Unlimited
	}
	return cap
}

// CostCounter is the read-side projection of the cost_counters row used by
// enforcement decisions.
type CostCounter struct {
	TenantID    string
	Plan        Plan
	Metric      CostMetric
	PeriodStart int64 // unix seconds
	PeriodEnd   int64
	Value       int64
}

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

// ErrCostCapExceeded is returned by Check when the tenant's counter has
// reached or exceeded its cap for the period.
var ErrCostCapExceeded = errors.New("cost cap exceeded")

// ExceededError carries the rich context expected by HTTP 402 responses.
// Wraps ErrCostCapExceeded so errors.Is works.
type ExceededError struct {
	TenantID string
	Plan     Plan
	Metric   CostMetric
	Cap      int64
	Current  int64
	Period   string // "2026-05" — calendar month, UTC
}

// Error implements error.
func (e *ExceededError) Error() string {
	return fmt.Sprintf("cost cap exceeded: tenant=%s plan=%s metric=%s cap=%d current=%d period=%s",
		e.TenantID, e.Plan, e.Metric, e.Cap, e.Current, e.Period)
}

// Unwrap returns the sentinel so errors.Is(err, ErrCostCapExceeded) holds.
func (e *ExceededError) Unwrap() error { return ErrCostCapExceeded }

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
// Note the >= : reaching the cap blocks the NEXT mutation. Aggregator
// latency (5 min) means tenant may slightly exceed cap until next tick —
// accepted bound per plan EC1.9.1.
func (c *CapChecker) Check(ctx context.Context, tenantID string, plan Plan, metric CostMetric) error {
	counter, found, err := c.store.GetCounter(ctx, tenantID, metric)
	if err != nil {
		// Fail-open on store errors. A noisy aggregator should not block
		// production traffic. The middleware logs the error; ops can see
		// it on the metric `loopzero_cost_cap_store_error_total`.
		return nil
	}

	cap := CapFor(plan, metric)
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
			Plan:     plan,
			Metric:   metric,
			Cap:      cap,
			Current:  counter.Value,
			Period:   formatPeriod(counter.PeriodStart),
		}
	}

	return nil
}

// formatPeriod converts a unix-seconds period start into "YYYY-MM" UTC.
// Hand-rolled to avoid time package dependency churn in tests.
func formatPeriod(unixSec int64) string {
	// 1970-01 is the epoch; we just need year + month UTC. Stdlib import
	// is fine here — only adding fmt overhead is the concern, not time.
	return formatPeriodImpl(unixSec)
}
