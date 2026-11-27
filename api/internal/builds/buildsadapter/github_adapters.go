// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildsadapter

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/github"
)

// BuildLookupAdapter bridges db.BuildStore → github.BuildLookup.
type BuildLookupAdapter struct {
	Store interface {
		buildByID
		buildApps
	}
}

func (a *BuildLookupAdapter) GetCommitSHA(ctx context.Context, buildID string) (string, error) {
	build, err := a.Store.GetByID(ctx, buildID)
	if err != nil || build == nil {
		return "", err
	}
	return build.CommitSHA, nil
}

// GetServedAppNames returns the build's apps that serve HTTP (a port), whose
// public URL the commit status can link.
func (a *BuildLookupAdapter) GetServedAppNames(ctx context.Context, buildID string) ([]string, error) {
	apps, err := a.Store.GetBuildApps(ctx, buildID)
	if err != nil {
		return nil, err
	}
	var served []string
	for _, app := range apps {
		if app.Port > 0 {
			served = append(served, app.Name)
		}
	}
	return served, nil
}

// buildSubmission is the orchestrator operation a push submits through.
// builds.BuildOrchestrator satisfies it.
type buildSubmission interface {
	SubmitBuild(ctx context.Context, req deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error)
}

// BuildSubmitterAdapter bridges github.BuildSubmitter → build.BuildOrchestrator.
type BuildSubmitterAdapter struct {
	Orchestrator buildSubmission
	Logger       *slog.Logger
}

func (a *BuildSubmitterAdapter) SubmitBuildFromWebhook(ctx context.Context, req github.WebhookBuildRequest) error {
	if req.LoopRuntimeYaml == nil {
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
	// inline here AND in services/preview_service.go appsFromLoopRuntimeYaml,
	// with a comment "mirror of autodeploy_adapters.go" admitting the
	// duplication. Canonical 2026-06-16 added `start` field extraction
	// only to this path — preview path silently kept losing it, so PR
	// previews failed at generate-dockerfile time while push deploys
	// succeeded against the same loopzero.yaml. Single source of truth now
	// lives in deploycontracts.ConvertLoopRuntimeYamlApps (with hermetic regression
	// locks for every extracted field).
	apps := deploycontracts.ConvertLoopRuntimeYamlApps(req.LoopRuntimeYaml.Apps)

	a.Logger.Info("auto-deploy: submitting build",
		"project", req.ProjectName,
		"environment", req.Environment,
		"sha", req.CommitSHA,
		"apps", len(apps),
	)

	// GitHub delivers the push with no user and no tenant scope; the tenant
	// is the one the repository is linked to, resolved server-side. The
	// orchestrator's stores run on the request pool, whose tenant tables are
	// fail-closed under Row Level Security, so the submission acts in it.
	ctx = dbtx.WithTenantScope(ctx, req.TenantID)
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
	_ github.BuildLookup    = (*BuildLookupAdapter)(nil)
	_ github.BuildSubmitter = (*BuildSubmitterAdapter)(nil)
)

// correlationSHAPrefixLen is how much of the commit SHA the push correlation id
// carries.
const correlationSHAPrefixLen = 8
