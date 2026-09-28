// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package previewadapter

import (
	"context"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/k8s/workload"
	"github.com/getloopscale/loopzero/api/internal/preview"
)

// PreviewAppsLookupAdapter implements preview.PreviewBuildAppsLookup by
// copying Apps from the most recent SUCCEEDED build of the (tenant, project).
// J-23 LIVE catch 2026-06-07: without this, preview builds fail with
// "WorkflowSpec.Plan must be non-nil" because PR webhooks don't ship
// parsed loopzero.yaml (only push webhooks do via WebhookBuildRequest).
type PreviewAppsLookupAdapter struct {
	Builds interface {
		projectBuilds
		buildApps
	}
}

func (a *PreviewAppsLookupAdapter) LatestSucceededApps(ctx context.Context, tenantID, projectID string) ([]deploycontracts.AppSpec, error) {
	// ListByProject newest first; iterate to find latest SUCCEEDED.
	const lookbackLimit = 20
	builds, err := a.Builds.ListByProject(ctx, tenantID, projectID, lookbackLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("preview apps lookup: list builds: %w", err)
	}
	var sourceBuildID string
	for _, b := range builds {
		if b.Status == "SUCCEEDED" {
			sourceBuildID = b.ID
			break
		}
	}
	if sourceBuildID == "" {
		return nil, nil // honest empty — preview service logs the hint
	}
	dbApps, err := a.Builds.GetBuildApps(ctx, sourceBuildID)
	if err != nil {
		return nil, fmt.Errorf("preview apps lookup: get apps for %s: %w", sourceBuildID, err)
	}
	out := make([]deploycontracts.AppSpec, 0, len(dbApps))
	for _, da := range dbApps {
		out = append(out, deploycontracts.AppSpecFromStored(da))
	}
	return out, nil
}

// PreviewCRCreatorAdapter bridges preview.PreviewCRCreator (services
// package's spec type) to workload.PreviewCRManager (k8s package's spec
// type). Two spec types exist to avoid the k8s↔services import cycle;
// fields are identical so the conversion is a flat copy.
type PreviewCRCreatorAdapter struct {
	Mgr *workload.PreviewCRManager
}

func (a *PreviewCRCreatorAdapter) EnsurePreviewCR(ctx context.Context, spec preview.PreviewCRSpec) error {
	return a.Mgr.EnsurePreviewCR(ctx, workload.PreviewCRSpec{
		Name:      spec.Name,
		TenantID:  spec.TenantID,
		ProjectID: spec.ProjectID,
		PRNumber:  spec.PRNumber,
		Branch:    spec.Branch,
		ReleaseID: spec.ReleaseID,
		ExpiresAt: spec.ExpiresAt,
		PublicURL: spec.PublicURL,
	})
}

func (a *PreviewCRCreatorAdapter) DeletePreviewCR(ctx context.Context, tenantID, projectID string, prNumber int) error {
	return a.Mgr.DeletePreviewCR(ctx, tenantID, projectID, prNumber)
}

// The ports these adapters satisfy, asserted at compile time.
var (
	_ preview.PreviewBuildAppsLookup = (*PreviewAppsLookupAdapter)(nil)
	_ preview.PreviewCRCreator       = (*PreviewCRCreatorAdapter)(nil)
)
