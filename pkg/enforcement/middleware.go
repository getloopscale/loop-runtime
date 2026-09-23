// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"encoding/json"
	"net/http"

	loopzeroerrorshttp "github.com/getloopscale/loopzero/pkg/errors/http"
)

// UsageCounter returns a tenant's current usage for one metric. An error means
// the usage could not be measured, and the middleware fails closed on it:
// reading a failed count as zero would let every quota pass during an outage.
type UsageCounter func(ctx context.Context, tenantID string) (int, error)

// OperationCheck defines what metric to check for a given operation.
type OperationCheck struct {
	Metric  string       // e.g. "projects", "builds_per_month"
	CountFn UsageCounter // Returns current usage for this tenant
}

// Middleware returns an HTTP middleware that checks plan limits.
// The tenantExtractor function extracts the tenant ID from the request.
// The upgradeURL is included in 402 responses. When the usage or the limits
// cannot be read the request is refused with 503 PLATFORM_DEGRADED.
func Middleware(
	limiter *PlanLimiter,
	check OperationCheck,
	tenantExtractor func(r *http.Request) string,
	upgradeURL string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			tenantID := tenantExtractor(r)
			if tenantID == "" {
				next.ServeHTTP(w, r)
				return
			}

			result, err := limiter.checkUsage(r.Context(), tenantID, check)
			if err != nil {
				limiter.logger.Error("plan limit check failed; refusing the request",
					"tenant", tenantID, "metric", check.Metric, "error", err)
				loopzeroerrorshttp.WriteJSON(w, r, err)
				return
			}

			if !result.Allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				if encErr := json.NewEncoder(w).Encode(FormatLimitError(result, upgradeURL)); encErr != nil {
					limiter.logger.Error("write plan limit response", "tenant", tenantID, "error", encErr)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// checkUsage counts the tenant's usage and checks it against the plan limit.
// Counting is skipped in dev mode (no store), where every check allows.
func (l *PlanLimiter) checkUsage(ctx context.Context, tenantID string, check OperationCheck) (LimitResult, error) {
	if !l.hasStore() {
		return l.CheckLimit(ctx, tenantID, check.Metric, 0)
	}
	current, err := check.CountFn(ctx, tenantID)
	if err != nil {
		return LimitResult{}, errLimitsUnavailable(tenantID, "count "+check.Metric+" usage", err)
	}
	return l.CheckLimit(ctx, tenantID, check.Metric, current)
}
