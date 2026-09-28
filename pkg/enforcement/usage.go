// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import "context"

// UsageCounter returns a tenant's current usage for one metric. An error means
// the usage could not be measured; CheckUsage fails closed on it, because
// reading a failed count as zero would let every quota pass during an outage.
type UsageCounter func(ctx context.Context, tenantID string) (int, error)

// CheckUsage counts the tenant's usage of metric and checks it against the
// plan limit. Counting is skipped in dev mode (no store), where every check
// allows. A counting failure returns the PLATFORM_DEGRADED error.
func (l *PlanLimiter) CheckUsage(ctx context.Context, tenantID, metric string, count UsageCounter) (LimitResult, error) {
	if !l.hasStore() {
		return l.CheckLimit(ctx, tenantID, metric, 0)
	}
	current, err := count(ctx, tenantID)
	if err != nil {
		return LimitResult{}, errLimitsUnavailable(tenantID, "count "+metric+" usage", err)
	}
	return l.CheckLimit(ctx, tenantID, metric, current)
}
