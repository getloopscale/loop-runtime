// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loopzero/pkg/domain"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	errorshttp "github.com/getloopscale/loopzero/pkg/errors/http"
)

// PlanLimitCheck names the metric an operation is limited by and how the
// tenant's current usage of it is counted.
type PlanLimitCheck struct {
	Metric string                   // e.g. "builds_per_month", "seats"
	Count  enforcement.UsageCounter // current usage for the tenant
}

// PlanLimits returns the HTTP middleware that gates an operation on the
// tenant's plan limit for check.Metric.
//
//   - nil limiter or no tenant on the request → pass through
//   - usage at or over the hard limit → 402 PLAN_LIMIT_EXCEEDED (wire contract)
//   - usage or plan unreadable → 503 PLATFORM_DEGRADED, never an allow
//
// The limit policy lives in pkg/enforcement; this adapter owns only the
// transport: extracting the tenant, the status codes and the 402 body.
func PlanLimits(
	limiter *enforcement.PlanLimiter,
	check PlanLimitCheck,
	tenantFrom func(r *http.Request) string,
	upgradeURL string,
	logger *slog.Logger,
) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}
			tenantID := tenantFrom(r)
			if tenantID == "" {
				next.ServeHTTP(w, r)
				return
			}

			result, err := limiter.CheckUsage(r.Context(), tenantID, check.Metric, check.Count)
			if err != nil {
				logger.Error("plan limit check failed; refusing the request",
					"tenant", tenantID, "metric", check.Metric, "error", err)
				errorshttp.WriteJSON(w, r, err)
				return
			}
			if !result.Allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				if encErr := json.NewEncoder(w).Encode(planLimitExceededBody(result, upgradeURL)); encErr != nil {
					logger.Error("write plan limit response", "tenant", tenantID, "error", encErr)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// planLimitExceededBody is the 402 body. Its field names are a stability
// contract with the CLI and the dashboard: add fields, never rename.
func planLimitExceededBody(result enforcement.LimitResult, upgradeURL string) map[string]interface{} {
	return map[string]interface{}{
		"code": "PLAN_LIMIT_EXCEEDED",
		"message": fmt.Sprintf("Plan limit reached: %s allows %d %s (current: %d). Upgrade your plan to continue.",
			domain.PlanTier(result.Plan).DisplayName(), result.Limit, limitMetricDisplayName(result.Metric), result.Current),
		"plan":        result.Plan,
		"limit":       result.Metric,
		"current":     result.Current,
		"max":         result.Limit,
		"upgrade_url": upgradeURL,
	}
}

// limitMetricDisplayName is the product wording of a plan-limit metric.
func limitMetricDisplayName(metric string) string {
	switch metric {
	case "services_per_project":
		return "services per project"
	case "builds_per_month":
		return "builds per month"
	case "seats":
		return "team members"
	case "secrets_per_env":
		return "secrets per environment"
	default:
		return metric // "projects", "environments" and unknown metrics read as-is
	}
}
