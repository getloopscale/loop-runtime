// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/github"
	"github.com/loopzero/loopzero/api/internal/services"
)

// buildLookupAdapter bridges db.BuildStore → github.BuildLookup.
type buildLookupAdapter struct {
	store db.BuildStore
}

func (a *buildLookupAdapter) GetCommitSHA(ctx context.Context, buildID string) (string, error) {
	build, err := a.store.GetByID(ctx, buildID)
	if err != nil || build == nil {
		return "", err
	}
	return build.CommitSHA, nil
}

// repoLookupAdapter bridges db.PostgresRepositoryStore → github.RepositoryLookup.
type repoLookupAdapter struct {
	store *db.PostgresRepositoryStore
}

func (a *repoLookupAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (string, int64, error) {
	r, err := a.store.GetByProject(ctx, tenantID, projectName)
	if err != nil || r == nil {
		return "", 0, err
	}
	return r.RepoFullName, r.InstallationID, nil
}

// buildSubmitterAdapter bridges github.BuildSubmitter → services.BuildOrchestrator.
type buildSubmitterAdapter struct {
	orchestrator *services.BuildOrchestrator
	logger       *slog.Logger
}

func (a *buildSubmitterAdapter) SubmitBuildFromWebhook(ctx context.Context, req github.WebhookBuildRequest) error {
	if req.LoopZeroYaml == nil {
		return fmt.Errorf("no loopzero.yaml parsed")
	}

	// Convert loopzero.yaml apps to AppSpec
	var apps []services.AppSpec
	for name, appCfg := range req.LoopZeroYaml.Apps {
		app := services.AppSpec{
			Name: name,
		}

		// Extract fields from the map[string]interface{}
		if v, ok := appCfg["type"].(string); ok {
			app.Type = v
		} else {
			app.Type = "server" // default
		}
		if v, ok := appCfg["framework"].(string); ok {
			app.Framework = v
		}
		if v, ok := appCfg["path"].(string); ok {
			app.Path = v
		}
		if v, ok := appCfg["port"].(int); ok {
			app.Port = v
		} else if v, ok := appCfg["port"].(float64); ok {
			app.Port = int(v)
		}
		if v, ok := appCfg["build"].(string); ok {
			app.Build = v
		}
		if v, ok := appCfg["render"].(string); ok {
			app.Rendering = v
		}

		// Auto-infer: frontend → static
		if app.Type == "frontend" && app.Rendering == "" {
			app.Rendering = "static"
		}

		apps = append(apps, app)
	}

	a.logger.Info("auto-deploy: submitting build",
		"project", req.ProjectName,
		"environment", req.Environment,
		"sha", req.CommitSHA,
		"apps", len(apps),
	)

	_, err := a.orchestrator.SubmitBuild(ctx, services.SubmitBuildRequest{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectName,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		Apps:          apps,
		CreatedBy:     fmt.Sprintf("github-push:%s", req.SenderLogin),
		CorrelationID: fmt.Sprintf("gh-push-%s-%s", req.RepoFullName, req.CommitSHA[:8]),
	})
	if err != nil {
		return fmt.Errorf("build submit failed: %w", err)
	}

	a.logger.Info("auto-deploy: build submitted successfully",
		"project", req.ProjectName,
		"sha", req.CommitSHA,
	)
	return nil
}
