// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainshttp

import (
	"context"
	"errors"
	"fmt"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/policy"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"
	"github.com/getloopscale/loop-runtime/pkg/domain"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// DomainPlanLimiter is the plan question a new custom domain asks, answered
// by *enforcement.PlanLimiter. The plan caps custom domains per environment,
// so the handler asks about the count the environment would reach.
type DomainPlanLimiter interface {
	CheckValue(ctx context.Context, tenantID, metric string, requested int) (enforcement.LimitResult, error)
}

// customDomainsLimitMetric is the plan_limits metric a new domain counts against.
const customDomainsLimitMetric = "custom_domains_per_env"

// planUpgradePath is where the console sends a person to change plan; the
// plan gate of the other routes answers with the same path by default.
const planUpgradePath = "/billing"

// SetUpgradeURL sets where a plan limit refusal points the person: the address
// the platform is configured with (BILLING_UPGRADE_URL). Empty keeps the
// console's own path.
func (h *DomainHandler) SetUpgradeURL(upgradeURL string) {
	h.upgradeURL = upgradeURL
}

// planUpgradeURL is the address a plan limit refusal carries.
func (h *DomainHandler) planUpgradeURL() string {
	if h.upgradeURL == "" {
		return planUpgradePath
	}
	return h.upgradeURL
}

// SetPlanLimiter makes Create refuse a domain the tenant's plan has no room
// for. Without it no plan is enforced (no database: dev and tests).
func (h *DomainHandler) SetPlanLimiter(limiter DomainPlanLimiter) {
	h.planLimits = limiter
}

// allowedByPlan reports whether the plan has room for one more custom domain
// in the environment. When it answers false the refusal is already written:
// 402 PLAN_LIMIT_EXCEEDED, or a 5xx when the count or the plan cannot be read.
func (h *DomainHandler) allowedByPlan(ctx context.Context, w http.ResponseWriter, tenantID string, req domainCreateRequest) bool {
	if h.planLimits == nil {
		return true
	}
	// Only the total is read; one row is the least the store returns.
	_, existing, err := h.store.ListByTenant(ctx, tenantID, DomainListFilter{
		Project: req.Project, Environment: req.Environment, Limit: 1,
	})
	if err != nil {
		slog.ErrorContext(ctx, "domain create: cannot count the environment's domains for the plan gate",
			"tenant", tenantID, "project", req.Project, "env", req.Environment, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return false
	}
	result, err := h.planLimits.CheckValue(ctx, tenantID, customDomainsLimitMetric, existing+1)
	if err != nil {
		// An unreadable plan is a refusal, never a pass and never a guessed 402.
		slog.ErrorContext(ctx, "domain create: plan limit cannot be read; refusing the domain",
			"tenant", tenantID, "project", req.Project, "env", req.Environment, "error", err)
		var typed *loopzeroerrors.LoopZeroError
		if !errors.As(err, &typed) {
			typed = loopzeroerrors.ErrPlatformDegraded
		}
		httpio.WriteLoopZeroError(w, typed)
		return false
	}
	if result.Allowed {
		return true
	}
	httpio.WriteJSON(w, http.StatusPaymentRequired, map[string]any{
		"code": "PLAN_LIMIT_EXCEEDED",
		// Read by a person in the console and in the CLI: the limit, the
		// usage and what to do about it.
		"message": fmt.Sprintf("Plan limit reached: %s allows %d custom domains per environment, and %s has %d. %s",
			domain.PlanTier(result.Plan).DisplayName(), result.Limit, req.Environment, existing, policy.UpgradeAdvice(h.planUpgradeURL())),
		"current":     existing,
		"limit":       "custom_domains",
		"max":         result.Limit,
		"plan":        result.Plan,
		"upgrade_url": h.planUpgradeURL(),
	})
	return false
}
