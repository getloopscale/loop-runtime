// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// BuildQuota answers whether the tenant's plan has room for one more build.
type BuildQuota interface {
	CheckBuildQuota(ctx context.Context, tenantID string) (BuildQuotaResult, error)
}

// BuildQuotaResult is the plan's answer for one build. When Allowed is false,
// Message is the refusal in the words the deploy route's 402 uses, and
// UpgradeURL is where the plan is changed.
type BuildQuotaResult struct {
	Allowed    bool
	Message    string
	UpgradeURL string
}

// refusedByBuildQuota reports whether the plan refuses this push. A refused
// push builds nothing and leaves the reason on the commit; a plan that cannot
// be read is an error, never an allow.
func (d *AutoDeployer) refusedByBuildQuota(ctx context.Context, log *slog.Logger, req PushRequest, owner, repo string) (bool, error) {
	if d.cfg.BuildQuota == nil {
		return false, nil
	}
	result, err := d.cfg.BuildQuota.CheckBuildQuota(ctx, req.TenantID)
	if err != nil {
		return false, fmt.Errorf("auto-deploy: check the plan's build limit: %w", err)
	}
	if result.Allowed {
		return false, nil
	}
	log.Warn("auto-deploy: the plan's build limit is reached, nothing was built", "tenant_id", req.TenantID)
	d.markCommitOverBuildQuota(log, req, owner, repo, result)
	return true, nil
}

// markCommitOverBuildQuota tells the person who pushed that the plan refused
// the build and where to upgrade. No build exists to report on, so no later
// status would arrive.
func (d *AutoDeployer) markCommitOverBuildQuota(log *slog.Logger, req PushRequest, owner, repo string, result BuildQuotaResult) {
	if d.cfg.AppClient == nil {
		return
	}
	status := CommitStatus{
		State:       "failure",
		Description: fitCommitStatusDescription(result.Message),
		Context:     "loop-runtime/deploy",
	}
	// GitHub accepts only an absolute link.
	if strings.HasPrefix(result.UpgradeURL, "https://") || strings.HasPrefix(result.UpgradeURL, "http://") {
		status.TargetURL = result.UpgradeURL
	}
	// Detached for the reason markCommitPending gives.
	statusCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.cfg.AppClient.SetCommitStatus(statusCtx, req.InstallationID, owner, repo, req.CommitSHA, status); err != nil {
		log.Warn("auto-deploy: set commit status failed", "error", err)
	}
}
