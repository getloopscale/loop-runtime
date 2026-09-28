// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
)

// LimitsStore is the read-only port PlanLimiter uses to look up a tenant's
// plan and the (hard, soft) limit pair for a given metric (ADR-0033).
//
// Implementations MUST:
//
//   - Return ("", nil) from GetTenantPlan when the tenant has no active
//     subscription. That is the normal state of a tenant that just signed up,
//     not a platform fault: domain.ResolvePlanTier maps "" to Starter. Any
//     other failure (transport, partial read) MUST be a non-nil error.
//
//   - Return (-1, _, nil) for hardLimit when the metric has no
//     enforced cap for the plan (NULL hard_limit row).
//
//   - Return (0, _, nil) when the (plan, metric) row is missing; the limiter
//     denies the request (fail closed). Any other failure MUST be a non-nil
//     error.
//
// The SQL adapter lives with the schema it reads, in the api module
// (store.PlanLimitsStore); this package holds the policy only.
type LimitsStore interface {
	// GetTenantPlan returns the raw plan column (e.g., "starter", "pro")
	// of the tenant's active subscription, or ("", nil) when none exists.
	GetTenantPlan(ctx context.Context, tenantID string) (string, error)

	// GetLimits returns the (hard, soft) limits for the given (plan,
	// metric) pair. Soft <= 0 means no soft cap; hard < 0 means
	// unlimited; hard == 0 means missing row (fail closed).
	GetLimits(ctx context.Context, plan, metric string) (hardLimit, softLimit int, err error)
}
