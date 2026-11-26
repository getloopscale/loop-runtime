// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package gitops implements the GitOps pusher that commits manifests to Gitea
// tenant repos. This is the Step 6 architectural fix: the API now owns the
// entire release flow — build produces artifacts only, API commits to GitOps.
package gitops

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/naming"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/getloopscale/loop-runtime/api/internal/gitclient"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/pkg/gitearepo"
)

// GiteaGitOpsPusherConfig holds configuration for connecting to Gitea GitOps repos.
type GiteaGitOpsPusherConfig struct {
	BaseURL string // e.g., "http://gitea-http.loopzero-system.svc:3000"
	Token   string // Gitea access token for push authentication
	OrgName string // e.g., "loopzero"
}

// GiteaGitOpsPusher implements deploycontracts.GitOpsPusher by committing manifests
// to the tenant GitOps repo in Gitea.
//
// Repo naming: {tenantID}-gitops (matches TenantController convention)
// File path: {environment}/{projectID}/manifests.yaml
// Git trailers: Release-ID, Correlation-ID, Build-ID (spec §17.5)
type GiteaGitOpsPusher struct {
	git     gitRunner
	repo    gitRepoManager
	token   string // kept for GetManifestsAtCommit (HTTP API, not git)
	baseURL string // kept for GetManifestsAtCommit
	orgName string // kept for GetManifestsAtCommit
	client  *http.Client
}

// gitRunner is the subset of gitclient.GitRunner the pusher drives.
type gitRunner interface {
	Run(ctx context.Context, dir string, args ...string) error
	Output(ctx context.Context, dir string, args ...string) (string, error)
	RunWithAuth(ctx context.Context, dir string, args ...string) error
}

// gitRepoManager is the subset of gitearepo.Client the pusher needs.
type gitRepoManager interface {
	EnsureRepo(ctx context.Context, repoName string, opts gitearepo.EnsureOptions) error
	RepoURL(repoName string) string
}

// NewGiteaGitOpsPusher creates a new GiteaGitOpsPusher.
func NewGiteaGitOpsPusher(cfg GiteaGitOpsPusherConfig) *GiteaGitOpsPusher {
	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	return &GiteaGitOpsPusher{
		git:     gitclient.NewGitRunner(cfg.Token),
		repo:    gitearepo.New(cfg.BaseURL, cfg.Token, cfg.OrgName, httpClient),
		token:   cfg.Token,
		baseURL: cfg.BaseURL,
		orgName: cfg.OrgName,
		client:  httpClient,
	}
}

// Push commits manifests to the tenant GitOps repo and returns the commit SHA.
// The operation is idempotent: pushing the same manifests twice is safe (no
// change is staged, and the current HEAD SHA is returned).
func (p *GiteaGitOpsPusher) Push(ctx context.Context, opts deploycontracts.GitOpsPushOpts) (string, error) {
	// MEET-2026-04-09-042 P1: Manifest validation gate.
	// Reject known-problematic resource types that can block ArgoCD sync.
	// Certificate CRs caused a production incident (50+ min blocked sync).
	if err := validateManifests(opts.Manifests); err != nil {
		return "", fmt.Errorf("gitops push: manifest validation failed: %w", err)
	}

	// MEET-2026-03-31-023 T1-8: Measure GitOps push duration.
	pushStart := time.Now()
	defer func() {
		metrics.GitOpsOperationDuration.WithLabelValues("push").Observe(time.Since(pushStart).Seconds())
	}()

	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.gitops.push_manifests",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", opts.TenantID),
			attribute.String("loopzero.project", opts.ProjectID),
			attribute.String("loopzero.environment", opts.Environment),
		))
	defer span.End()

	if opts.Manifests == "" {
		return "", fmt.Errorf("gitops push: manifests cannot be empty")
	}

	ws, cleanup, err := p.openWorkspace(ctx, opts.TenantID, "push")
	if err != nil {
		return "", err
	}
	defer cleanup()

	// BLOCKER-3: environment-first hierarchy so ArgoCD Application path scopes to one environment
	relPath := filepath.Join(opts.Environment, opts.ProjectID)
	if err := ws.writeFile(filepath.Join(relPath, "manifests.yaml"), opts.Manifests); err != nil {
		return "", err
	}
	if err := ws.git.Run(ctx, ws.dir, "add", relPath+"/"); err != nil {
		return "", fmt.Errorf("gitops push: git add: %w", err)
	}

	// Git trailers (spec §17.5)
	commitMsg := fmt.Sprintf("Deploy %s to %s\n\nRelease-ID: %s\nCorrelation-ID: %s\nBuild-ID: %s",
		opts.ProjectID, opts.Environment,
		opts.ReleaseID, opts.CorrelationID, opts.BuildID,
	)
	sha, err := ws.commitAndPush(ctx, commitMsg)
	if err != nil {
		return "", err
	}
	span.SetAttributes(attribute.String("loopzero.gitops.commit_sha", sha))
	return sha, nil
}

// Remove deletes a project's manifests from the tenant GitOps repo.
// The operation is idempotent: if the directory doesn't exist, it returns the
// current HEAD. ArgoCD with prune: true will automatically remove the K8s
// resources.
//
// Every failure to reach or update the repo is returned: a delete that could
// not remove the manifests must fail, or ArgoCD keeps the workload running
// while the caller believes it is gone.
func (p *GiteaGitOpsPusher) Remove(ctx context.Context, opts deploycontracts.GitOpsRemoveOpts) (string, error) {
	// MEET-2026-03-31-023 T1-8: Measure GitOps remove duration.
	removeStart := time.Now()
	defer func() {
		metrics.GitOpsOperationDuration.WithLabelValues("remove").Observe(time.Since(removeStart).Seconds())
	}()

	ws, cleanup, err := p.openWorkspace(ctx, opts.TenantID, "remove")
	if err != nil {
		return "", err
	}
	defer cleanup()

	relPath := filepath.Join(opts.Environment, opts.ProjectID)
	if _, err := os.Stat(filepath.Join(ws.dir, relPath)); err != nil {
		if os.IsNotExist(err) {
			return ws.head(ctx) // nothing to remove — idempotent success
		}
		return "", fmt.Errorf("gitops remove: stat %s: %w", relPath, err)
	}
	if err := ws.git.Run(ctx, ws.dir, "rm", "-r", relPath); err != nil {
		return "", fmt.Errorf("gitops remove: git rm: %w", err)
	}

	// Keep the environment directory with a .gitkeep so ArgoCD doesn't get
	// ComparisonError ("app path does not exist") when the last project in the
	// environment is deleted. Without this, ArgoCD sync goes Unknown and can't
	// prune the remaining K8s resources (F-01/F-02/F-03).
	gitkeep := filepath.Join(opts.Environment, ".gitkeep")
	if err := ws.writeFile(gitkeep, ""); err != nil {
		return "", err
	}
	if err := ws.git.Run(ctx, ws.dir, "add", gitkeep); err != nil {
		return "", fmt.Errorf("gitops remove: git add .gitkeep: %w", err)
	}

	commitMsg := fmt.Sprintf("Remove %s from %s\n\nCorrelation-ID: %s",
		opts.ProjectID, opts.Environment, opts.CorrelationID,
	)
	return ws.commitAndPush(ctx, commitMsg)
}

// GetManifestsAtCommit retrieves the manifest content from a specific git commit.
// Uses the Gitea raw content API: GET /api/v1/repos/{owner}/{repo}/raw/{filepath}?ref={commit}
// The filepath follows the convention: {env}/{projectID}/manifests.yaml
func (p *GiteaGitOpsPusher) GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error) {
	repoName := naming.TenantGitOpsRepoName(tenantID)
	filePath := fmt.Sprintf("%s/%s/manifests.yaml", env, projectID)

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/raw/%s?ref=%s",
		p.baseURL, p.orgName, repoName, filePath, commitSHA)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("gitops reader: create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitops reader: fetch manifests: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("manifests not found at commit %s", commitSHA)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitops reader: unexpected status %d for commit %s", resp.StatusCode, commitSHA)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("gitops reader: read response: %w", err)
	}

	return buf.Bytes(), nil
}

// EnsureEnvironmentPath makes sure the directory the Argo CD Application of
// (project, environment) reads exists on the tenant repo. An Application whose
// path is missing reports ComparisonError until the first release writes it, so
// an environment is given the directory when it is created.
//
// A directory that already exists is left as it is, whatever it holds: a
// placeholder is never written next to manifests.
func (p *GiteaGitOpsPusher) EnsureEnvironmentPath(ctx context.Context, tenantID, projectID, environment string) error {
	ws, cleanup, err := p.openWorkspace(ctx, tenantID, "prepare")
	if err != nil {
		return err
	}
	defer cleanup()

	relPath := filepath.Join(environment, projectID)
	if _, err := os.Stat(filepath.Join(ws.dir, relPath)); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("gitops prepare: stat %s: %w", relPath, err)
	}
	gitkeep := filepath.Join(relPath, ".gitkeep")
	if err := ws.writeFile(gitkeep, ""); err != nil {
		return err
	}
	if err := ws.git.Run(ctx, ws.dir, "add", gitkeep); err != nil {
		return fmt.Errorf("gitops prepare: git add .gitkeep: %w", err)
	}
	_, err = ws.commitAndPush(ctx, fmt.Sprintf("Prepare %s in %s", projectID, environment))
	return err
}
