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
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/deploycontracts"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/getloopscale/loopzero/api/internal/gitclient"
	"github.com/getloopscale/loopzero/api/internal/metrics"
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
	git     *gitclient.GitRunner
	repo    *gitclient.GiteaRepoManager
	token   string // kept for GetManifestsAtCommit (HTTP API, not git)
	baseURL string // kept for GetManifestsAtCommit
	orgName string // kept for GetManifestsAtCommit
	client  *http.Client
}

// NewGiteaGitOpsPusher creates a new GiteaGitOpsPusher.
func NewGiteaGitOpsPusher(cfg GiteaGitOpsPusherConfig) *GiteaGitOpsPusher {
	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	return &GiteaGitOpsPusher{
		git:     gitclient.NewGitRunner(cfg.Token),
		repo:    gitclient.NewGiteaRepoManager(cfg.BaseURL, cfg.Token, cfg.OrgName, httpClient),
		token:   cfg.Token,
		baseURL: cfg.BaseURL,
		orgName: cfg.OrgName,
		client:  httpClient,
	}
}

// Push commits manifests to the tenant GitOps repo and returns the commit SHA.
// The operation is idempotent: pushing the same manifests twice is safe (git
// will detect "nothing to commit" and we return the current HEAD SHA).
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

	repoName := fmt.Sprintf("%s-gitops", opts.TenantID)
	remoteURL := p.repo.RepoURL(repoName)

	// Ensure repo exists in Gitea
	if err := p.repo.EnsureRepo(ctx, repoName, true); err != nil {
		return "", fmt.Errorf("gitops push: ensure repo: %w", err)
	}

	// Create temp directory for git operations
	tmpDir, err := os.MkdirTemp("", "loopzero-gitops-push-*")
	if err != nil {
		return "", fmt.Errorf("gitops push: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	repoDir := filepath.Join(tmpDir, "repo")

	// Clone existing repo (or init fresh if empty)
	// SEC-1: Auth via http.extraHeader, not embedded in URL.
	if err := p.git.RunWithAuth(ctx, tmpDir, "clone", "--depth", "5", remoteURL, repoDir); err != nil {
		// Clone failed (empty repo or first push) — init fresh
		if err := p.git.Run(ctx, tmpDir, "init", repoDir); err != nil {
			return "", fmt.Errorf("gitops push: git init: %w", err)
		}
		if err := p.git.Run(ctx, repoDir, "remote", "add", "origin", remoteURL); err != nil {
			return "", fmt.Errorf("gitops push: git remote add: %w", err)
		}
	}

	// Set git identity
	if err := p.git.Run(ctx, repoDir, "config", "user.email", "loopzero-api@loopzero.io"); err != nil {
		return "", fmt.Errorf("gitops push: git config user.email: %w", err)
	}
	if err := p.git.Run(ctx, repoDir, "config", "user.name", "LoopZero API"); err != nil {
		return "", fmt.Errorf("gitops push: git config user.name: %w", err)
	}

	// Write manifests to {environment}/{projectID}/manifests.yaml
	// BLOCKER-3: environment-first hierarchy so ArgoCD Application path scopes to one environment
	envPath := filepath.Join(repoDir, opts.Environment, opts.ProjectID)
	if err := os.MkdirAll(envPath, 0o750); err != nil {
		return "", fmt.Errorf("gitops push: create dir: %w", err)
	}
	manifestFile := filepath.Join(envPath, "manifests.yaml")
	if err := os.WriteFile(manifestFile, []byte(opts.Manifests), 0o600); err != nil {
		return "", fmt.Errorf("gitops push: write manifests: %w", err)
	}

	// Stage changes
	relPath := filepath.Join(opts.Environment, opts.ProjectID)
	if err := p.git.Run(ctx, repoDir, "add", relPath+"/"); err != nil {
		return "", fmt.Errorf("gitops push: git add: %w", err)
	}

	// Commit with git trailers (spec §17.5)
	commitMsg := fmt.Sprintf("Deploy %s to %s\n\nRelease-ID: %s\nCorrelation-ID: %s\nBuild-ID: %s",
		opts.ProjectID, opts.Environment,
		opts.ReleaseID, opts.CorrelationID, opts.BuildID,
	)

	// git commit — if nothing changed, this is idempotent (no error)
	commitErr := p.git.Run(ctx, repoDir, "commit", "-m", commitMsg)
	if commitErr != nil {
		// "nothing to commit" is not an error for idempotency
		if strings.Contains(commitErr.Error(), "nothing to commit") {
			sha, err := p.git.Output(ctx, repoDir, "rev-parse", "HEAD")
			if err != nil {
				return "", fmt.Errorf("gitops push: get HEAD after no-op commit: %w", err)
			}
			return sha, nil
		}
		return "", fmt.Errorf("gitops push: git commit: %w", commitErr)
	}

	// Push with fetch-rebase-push on conflict (up to 3 attempts)
	// SEC-1: Network git commands use gitCmdWithAuth (http.extraHeader).
	for attempt := 0; attempt < 3; attempt++ {
		if err := p.git.RunWithAuth(ctx, repoDir, "push", "origin", "HEAD:refs/heads/main"); err == nil {
			break
		}
		if attempt == 2 {
			return "", fmt.Errorf("gitops push: failed after 3 push attempts")
		}
		// Fetch and rebase on conflict
		if err := p.git.RunWithAuth(ctx, repoDir, "fetch", "origin"); err != nil {
			return "", fmt.Errorf("gitops push: fetch failed: %w", err)
		}
		if err := p.git.Run(ctx, repoDir, "rebase", "origin/main"); err != nil {
			return "", fmt.Errorf("gitops push: rebase failed: %w", err)
		}
	}

	// Return the commit SHA
	sha, err := p.git.Output(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitops push: get commit SHA: %w", err)
	}

	span.SetAttributes(attribute.String("loopzero.gitops.commit_sha", sha))
	return sha, nil
}

// Remove deletes a project's manifests from the tenant GitOps repo.
// The operation is idempotent: if the directory doesn't exist, returns success.
// ArgoCD with prune: true will automatically remove the K8s resources.
func (p *GiteaGitOpsPusher) Remove(ctx context.Context, opts deploycontracts.GitOpsRemoveOpts) (string, error) {
	// MEET-2026-03-31-023 T1-8: Measure GitOps remove duration.
	removeStart := time.Now()
	defer func() {
		metrics.GitOpsOperationDuration.WithLabelValues("remove").Observe(time.Since(removeStart).Seconds())
	}()

	repoName := fmt.Sprintf("%s-gitops", opts.TenantID)
	remoteURL := p.repo.RepoURL(repoName)

	// Ensure repo exists (it should, but be safe)
	if err := p.repo.EnsureRepo(ctx, repoName, true); err != nil {
		return "", fmt.Errorf("gitops remove: ensure repo: %w", err)
	}

	// Create temp directory for git operations
	tmpDir, err := os.MkdirTemp("", "loopzero-gitops-remove-*")
	if err != nil {
		return "", fmt.Errorf("gitops remove: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	repoDir := filepath.Join(tmpDir, "repo")

	// Clone repo
	// SEC-1: Auth via http.extraHeader, not embedded in URL.
	if err := p.git.RunWithAuth(ctx, tmpDir, "clone", "--depth", "5", remoteURL, repoDir); err != nil {
		// If repo is empty/doesn't exist, nothing to remove
		return "", nil
	}

	// Set git identity
	if err := p.git.Run(ctx, repoDir, "config", "user.email", "loopzero-api@loopzero.io"); err != nil {
		return "", fmt.Errorf("gitops remove: git config user.email: %w", err)
	}
	if err := p.git.Run(ctx, repoDir, "config", "user.name", "LoopZero API"); err != nil {
		return "", fmt.Errorf("gitops remove: git config user.name: %w", err)
	}

	// Check if the directory exists in the repo
	targetDir := filepath.Join(repoDir, opts.Environment, opts.ProjectID)
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		// Directory doesn't exist — idempotent success
		sha, err := p.git.Output(ctx, repoDir, "rev-parse", "HEAD")
		if err != nil {
			return "", nil
		}
		return sha, nil
	}

	// git rm -r {env}/{projectID}/
	relPath := filepath.Join(opts.Environment, opts.ProjectID)
	if err := p.git.Run(ctx, repoDir, "rm", "-r", relPath); err != nil {
		return "", fmt.Errorf("gitops remove: git rm: %w", err)
	}

	// Ensure the environment directory still exists with a .gitkeep so ArgoCD
	// doesn't get ComparisonError ("app path does not exist") when the last
	// project in the environment is deleted. Without this, ArgoCD sync goes
	// Unknown and can't prune the remaining K8s resources (F-01/F-02/F-03).
	envDir := filepath.Join(repoDir, opts.Environment)
	gitkeep := filepath.Join(envDir, ".gitkeep")
	if err := os.MkdirAll(envDir, 0o750); err != nil {
		return "", fmt.Errorf("gitops remove: ensure env dir: %w", err)
	}
	if err := os.WriteFile(gitkeep, []byte(""), 0o600); err != nil {
		return "", fmt.Errorf("gitops remove: write .gitkeep: %w", err)
	}
	if err := p.git.Run(ctx, repoDir, "add", filepath.Join(opts.Environment, ".gitkeep")); err != nil {
		return "", fmt.Errorf("gitops remove: git add .gitkeep: %w", err)
	}

	// Commit
	commitMsg := fmt.Sprintf("Remove %s from %s\n\nCorrelation-ID: %s",
		opts.ProjectID, opts.Environment, opts.CorrelationID,
	)
	commitErr := p.git.Run(ctx, repoDir, "commit", "-m", commitMsg)
	if commitErr != nil {
		if strings.Contains(commitErr.Error(), "nothing to commit") {
			sha, err := p.git.Output(ctx, repoDir, "rev-parse", "HEAD")
			if err != nil {
				return "", fmt.Errorf("gitops remove: get HEAD after no-op: %w", err)
			}
			return sha, nil
		}
		return "", fmt.Errorf("gitops remove: git commit: %w", commitErr)
	}

	// Push with retry (3 attempts with fetch-rebase-push on conflict)
	// SEC-1: Network git commands use gitCmdWithAuth (http.extraHeader).
	for attempt := 0; attempt < 3; attempt++ {
		if err := p.git.RunWithAuth(ctx, repoDir, "push", "origin", "HEAD:refs/heads/main"); err == nil {
			break
		}
		if attempt == 2 {
			return "", fmt.Errorf("gitops remove: failed after 3 push attempts")
		}
		if err := p.git.RunWithAuth(ctx, repoDir, "fetch", "origin"); err != nil {
			return "", fmt.Errorf("gitops remove: fetch failed: %w", err)
		}
		if err := p.git.Run(ctx, repoDir, "rebase", "origin/main"); err != nil {
			return "", fmt.Errorf("gitops remove: rebase failed: %w", err)
		}
	}

	sha, err := p.git.Output(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitops remove: get commit SHA: %w", err)
	}

	return sha, nil
}

// GetManifestsAtCommit retrieves the manifest content from a specific git commit.
// Uses the Gitea raw content API: GET /api/v1/repos/{owner}/{repo}/raw/{filepath}?ref={commit}
// The filepath follows the convention: {env}/{projectID}/manifests.yaml
func (p *GiteaGitOpsPusher) GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error) {
	repoName := fmt.Sprintf("%s-gitops", tenantID)
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
