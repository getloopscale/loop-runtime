// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/github"
	"github.com/getloopscale/loopzero/api/internal/services/build"
)

// BuildLookupAdapter bridges db.BuildStore → github.BuildLookup.
type BuildLookupAdapter struct {
	Store db.BuildStore
}

func (a *BuildLookupAdapter) GetCommitSHA(ctx context.Context, buildID string) (string, error) {
	build, err := a.Store.GetByID(ctx, buildID)
	if err != nil || build == nil {
		return "", err
	}
	return build.CommitSHA, nil
}

// RepoLookupAdapter bridges store.PostgresRepositoryStore → github.RepositoryLookup.
type RepoLookupAdapter struct {
	Store *store.PostgresRepositoryStore
}

func (a *RepoLookupAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (string, int64, error) {
	r, err := a.Store.GetByProject(ctx, tenantID, projectName)
	if err != nil || r == nil {
		return "", 0, err
	}
	return r.RepoFullName, r.InstallationID, nil
}

// correlationSHAPrefixLen is how much of the commit SHA the push correlation id
// carries.
const correlationSHAPrefixLen = 8

// BuildSubmitterAdapter bridges github.BuildSubmitter → build.BuildOrchestrator.
type BuildSubmitterAdapter struct {
	Orchestrator *build.BuildOrchestrator
	Logger       *slog.Logger
}

func (a *BuildSubmitterAdapter) SubmitBuildFromWebhook(ctx context.Context, req github.WebhookBuildRequest) error {
	if req.LoopZeroYaml == nil {
		return fmt.Errorf("no loopzero.yaml parsed")
	}
	// The SHA comes from the webhook payload; it is validated here, before it
	// is sliced into the correlation id, so a malformed delivery is refused
	// instead of panicking the webhook goroutine.
	if len(req.CommitSHA) < correlationSHAPrefixLen {
		return fmt.Errorf("auto-deploy: commit SHA %q for %s is shorter than %d characters",
			req.CommitSHA, req.RepoFullName, correlationSHAPrefixLen)
	}

	// Sponsor LIVE-caught 2026-06-18: this conversion previously lived
	// inline here AND in services/preview_service.go appsFromLoopZeroYaml,
	// with a comment "mirror of autodeploy_adapters.go" admitting the
	// duplication. Canonical 2026-06-16 added `start` field extraction
	// only to this path — preview path silently kept losing it, so PR
	// previews failed at generate-dockerfile time while push deploys
	// succeeded against the same loopzero.yaml. Single source of truth now
	// lives in deploycontracts.ConvertLoopZeroYamlApps (with hermetic regression
	// locks for every extracted field).
	apps := deploycontracts.ConvertLoopZeroYamlApps(req.LoopZeroYaml.Apps)

	a.Logger.Info("auto-deploy: submitting build",
		"project", req.ProjectName,
		"environment", req.Environment,
		"sha", req.CommitSHA,
		"apps", len(apps),
	)

	_, err := a.Orchestrator.SubmitBuild(ctx, deploycontracts.SubmitBuildRequest{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectName,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		Apps:          apps,
		CreatedBy:     fmt.Sprintf("github-push:%s", req.SenderLogin),
		CorrelationID: fmt.Sprintf("gh-push-%s-%s", req.RepoFullName, req.CommitSHA[:correlationSHAPrefixLen]),
	})
	if err != nil {
		return fmt.Errorf("build submit failed: %w", err)
	}

	a.Logger.Info("auto-deploy: build submitted successfully",
		"project", req.ProjectName,
		"sha", req.CommitSHA,
	)
	return nil
}

// The ports these adapters satisfy, asserted at compile time.
var (
	_ github.BuildLookup      = (*BuildLookupAdapter)(nil)
	_ github.RepositoryLookup = (*RepoLookupAdapter)(nil)
	_ github.BuildSubmitter   = (*BuildSubmitterAdapter)(nil)
)
