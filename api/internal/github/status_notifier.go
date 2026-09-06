// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// RepositoryLookup finds the linked repo for a project (DIP).
type RepositoryLookup interface {
	GetByProject(ctx context.Context, tenantID, projectName string) (repoFullName string, installationID int64, err error)
}

// BuildLookup finds a build's source commit SHA (DIP).
type BuildLookup interface {
	GetCommitSHA(ctx context.Context, buildID string) (string, error)
}

// StatusNotifier updates GitHub commit status when builds complete.
type StatusNotifier struct {
	appClient   *AppClient
	repoLookup  RepositoryLookup
	buildLookup BuildLookup
	logger      *slog.Logger
	baseDomain  string
}

// NewStatusNotifier creates a new StatusNotifier. Returns nil if AppClient is nil.
func NewStatusNotifier(appClient *AppClient, repoLookup RepositoryLookup, buildLookup BuildLookup, baseDomain string, logger *slog.Logger) *StatusNotifier {
	if appClient == nil {
		return nil
	}
	return &StatusNotifier{
		appClient:   appClient,
		repoLookup:  repoLookup,
		buildLookup: buildLookup,
		logger:      logger,
		baseDomain:  baseDomain,
	}
}

// OnBuildSucceeded sets commit status to "success" on GitHub.
// Receives buildID (not commit SHA) — looks up the source commit SHA from the build.
func (n *StatusNotifier) OnBuildSucceeded(ctx context.Context, tenantID, projectName, buildID, environment string) {
	if n == nil {
		return
	}

	// Look up source commit SHA from build (not gitops SHA)
	commitSHA, err := n.buildLookup.GetCommitSHA(ctx, buildID)
	if err != nil || commitSHA == "" {
		n.logger.Warn("github status: could not find commit SHA for build", "build_id", buildID, "error", err)
		return
	}

	repoFullName, installationID, err := n.repoLookup.GetByProject(ctx, tenantID, projectName)
	if err != nil || repoFullName == "" {
		return // not a GitHub-connected project
	}

	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return
	}

	deployURL := fmt.Sprintf("https://%s-%s-%s-%s.%s",
		tenantID, projectName, "api", environment, n.baseDomain)

	if err := n.appClient.SetCommitStatus(ctx, installationID, parts[0], parts[1], commitSHA, CommitStatus{
		State:       "success",
		TargetURL:   deployURL,
		Description: fmt.Sprintf("Deployed to %s", environment),
		Context:     "loopzero/deploy",
	}); err != nil {
		n.logger.Warn("github status: failed to set success", "error", err, "sha", commitSHA)
	} else {
		n.logger.Info("github status: commit marked as success", "sha", commitSHA, "repo", repoFullName)
	}
}

// OnBuildFailed sets commit status to "failure" on GitHub.
func (n *StatusNotifier) OnBuildFailed(ctx context.Context, tenantID, projectName, commitSHA, buildID string) {
	if n == nil {
		return
	}

	repoFullName, installationID, err := n.repoLookup.GetByProject(ctx, tenantID, projectName)
	if err != nil || repoFullName == "" {
		return
	}

	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return
	}

	if err := n.appClient.SetCommitStatus(ctx, installationID, parts[0], parts[1], commitSHA, CommitStatus{
		State:       "failure",
		Description: "Build failed",
		Context:     "loopzero/deploy",
	}); err != nil {
		n.logger.Warn("github status: failed to set failure", "error", err, "sha", commitSHA)
	} else {
		n.logger.Info("github status: commit marked as failure", "sha", commitSHA, "repo", repoFullName)
	}
}
