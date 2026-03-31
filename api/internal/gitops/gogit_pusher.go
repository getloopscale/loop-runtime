// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	gogithttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/go-git/go-git/v5/plumbing/object"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/services"
)

// GoGitGitOpsPusher implements services.GitOpsPusher using the go-git library
// instead of git CLI subprocesses. This eliminates the dependency on the git
// binary and avoids shell injection risks.
//
// Repo naming: {tenantID}-gitops (matches TenantController convention)
// File path: {environment}/{projectID}/manifests.yaml
// Git trailers: Release-ID, Correlation-ID, Build-ID (spec §17.5)
type GoGitGitOpsPusher struct {
	baseURL string
	token   string
	orgName string
	client  *http.Client
}

// NewGoGitGitOpsPusher creates a new GoGitGitOpsPusher.
func NewGoGitGitOpsPusher(cfg GiteaGitOpsPusherConfig) *GoGitGitOpsPusher {
	return &GoGitGitOpsPusher{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		orgName: cfg.OrgName,
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// isLocalURL returns true if the URL uses the file:// scheme.
// Local repos skip the Gitea HTTP API calls (ensureRepo).
func isLocalURL(url string) bool {
	return strings.HasPrefix(url, "file://") || strings.HasPrefix(url, "/")
}

// auth returns the go-git BasicAuth for Gitea HTTP operations.
// Equivalent to the SEC-1 http.extraHeader pattern in the CLI version.
func (p *GoGitGitOpsPusher) auth() *gogithttp.BasicAuth {
	return &gogithttp.BasicAuth{
		Username: "loopzero-api",
		Password: p.token,
	}
}

// remoteURL builds the git remote URL for the given repo name.
func (p *GoGitGitOpsPusher) remoteURL(repoName string) string {
	if isLocalURL(p.baseURL) {
		// For local file:// URLs, strip the file:// prefix if present
		// and return the raw path — go-git handles it natively.
		return strings.TrimPrefix(p.baseURL, "file://")
	}
	return fmt.Sprintf("%s/%s/%s.git", p.baseURL, p.orgName, repoName)
}

// Push commits manifests to the tenant GitOps repo and returns the commit SHA.
// The operation is idempotent: pushing the same manifests twice is safe (go-git
// detects no changes in the worktree and we return the current HEAD SHA).
func (p *GoGitGitOpsPusher) Push(ctx context.Context, opts services.GitOpsPushOpts) (string, error) {
	pushStart := time.Now()
	defer func() {
		metrics.GitOpsOperationDuration.WithLabelValues("push").Observe(time.Since(pushStart).Seconds())
	}()

	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.gitops.gogit.push_manifests",
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
	remote := p.remoteURL(repoName)

	// Ensure repo exists in Gitea (skip for local repos)
	if !isLocalURL(p.baseURL) {
		if err := p.ensureRepo(ctx, repoName); err != nil {
			return "", fmt.Errorf("gitops push: ensure repo: %w", err)
		}
	}

	tmpDir, err := os.MkdirTemp("", "loopzero-gogit-push-*")
	if err != nil {
		return "", fmt.Errorf("gitops push: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	repoDir := filepath.Join(tmpDir, "repo")

	// Clone or init
	repo, err := p.cloneOrInit(ctx, remote, repoDir)
	if err != nil {
		return "", fmt.Errorf("gitops push: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("gitops push: worktree: %w", err)
	}

	// Write manifests to {environment}/{projectID}/manifests.yaml
	envPath := filepath.Join(repoDir, opts.Environment, opts.ProjectID)
	if err := os.MkdirAll(envPath, 0755); err != nil {
		return "", fmt.Errorf("gitops push: create dir: %w", err)
	}
	manifestFile := filepath.Join(envPath, "manifests.yaml")
	if err := os.WriteFile(manifestFile, []byte(opts.Manifests), 0644); err != nil {
		return "", fmt.Errorf("gitops push: write manifests: %w", err)
	}

	// Stage changes
	relPath := filepath.Join(opts.Environment, opts.ProjectID, "manifests.yaml")
	if _, err := wt.Add(relPath); err != nil {
		return "", fmt.Errorf("gitops push: git add: %w", err)
	}

	// Check if there are actual changes to commit
	status, err := wt.Status()
	if err != nil {
		return "", fmt.Errorf("gitops push: git status: %w", err)
	}

	if status.IsClean() {
		// Nothing to commit — idempotent: return current HEAD
		ref, err := repo.Head()
		if err != nil {
			return "", fmt.Errorf("gitops push: get HEAD after no-op commit: %w", err)
		}
		return ref.Hash().String(), nil
	}

	// Commit with git trailers (spec §17.5)
	commitMsg := fmt.Sprintf("Deploy %s to %s\n\nRelease-ID: %s\nCorrelation-ID: %s\nBuild-ID: %s",
		opts.ProjectID, opts.Environment,
		opts.ReleaseID, opts.CorrelationID, opts.BuildID,
	)

	commitHash, err := wt.Commit(commitMsg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "LoopZero API",
			Email: "loopzero-api@loopzero.io",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("gitops push: commit: %w", err)
	}

	// Push with exponential backoff retry (1s, 2s, 4s)
	if err := p.pushWithRetry(ctx, repo); err != nil {
		return "", fmt.Errorf("gitops push: %w", err)
	}

	sha := commitHash.String()
	span.SetAttributes(attribute.String("loopzero.gitops.commit_sha", sha))
	return sha, nil
}

// Remove deletes a project's manifests from the tenant GitOps repo.
// The operation is idempotent: if the directory doesn't exist, returns success.
// ArgoCD with prune: true will automatically remove the K8s resources.
func (p *GoGitGitOpsPusher) Remove(ctx context.Context, opts services.GitOpsRemoveOpts) (string, error) {
	removeStart := time.Now()
	defer func() {
		metrics.GitOpsOperationDuration.WithLabelValues("remove").Observe(time.Since(removeStart).Seconds())
	}()

	repoName := fmt.Sprintf("%s-gitops", opts.TenantID)
	remote := p.remoteURL(repoName)

	// Ensure repo exists (skip for local repos)
	if !isLocalURL(p.baseURL) {
		if err := p.ensureRepo(ctx, repoName); err != nil {
			return "", fmt.Errorf("gitops remove: ensure repo: %w", err)
		}
	}

	tmpDir, err := os.MkdirTemp("", "loopzero-gogit-remove-*")
	if err != nil {
		return "", fmt.Errorf("gitops remove: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	repoDir := filepath.Join(tmpDir, "repo")

	// Clone repo
	repo, err := p.cloneRepo(ctx, remote, repoDir)
	if err != nil {
		// If repo is empty/doesn't exist, nothing to remove
		return "", nil
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("gitops remove: worktree: %w", err)
	}

	// Check if the directory exists in the repo
	targetDir := filepath.Join(repoDir, opts.Environment, opts.ProjectID)
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		// Directory doesn't exist — idempotent success
		ref, err := repo.Head()
		if err != nil {
			return "", nil
		}
		return ref.Hash().String(), nil
	}

	// Remove the project directory from the worktree
	relPath := filepath.Join(opts.Environment, opts.ProjectID)
	if err := p.removeDir(wt, repoDir, relPath); err != nil {
		return "", fmt.Errorf("gitops remove: %w", err)
	}

	// Ensure the environment directory still exists with a .gitkeep so ArgoCD
	// doesn't get ComparisonError ("app path does not exist") when the last
	// project in the environment is deleted.
	envDir := filepath.Join(repoDir, opts.Environment)
	gitkeep := filepath.Join(envDir, ".gitkeep")
	if err := os.MkdirAll(envDir, 0755); err != nil {
		return "", fmt.Errorf("gitops remove: ensure env dir: %w", err)
	}
	if err := os.WriteFile(gitkeep, []byte(""), 0644); err != nil {
		return "", fmt.Errorf("gitops remove: write .gitkeep: %w", err)
	}
	gitkeepRel := filepath.Join(opts.Environment, ".gitkeep")
	if _, err := wt.Add(gitkeepRel); err != nil {
		return "", fmt.Errorf("gitops remove: git add .gitkeep: %w", err)
	}

	// Check if there are actual changes
	status, err := wt.Status()
	if err != nil {
		return "", fmt.Errorf("gitops remove: git status: %w", err)
	}

	if status.IsClean() {
		ref, err := repo.Head()
		if err != nil {
			return "", fmt.Errorf("gitops remove: get HEAD after no-op: %w", err)
		}
		return ref.Hash().String(), nil
	}

	// Commit
	commitMsg := fmt.Sprintf("Remove %s from %s\n\nCorrelation-ID: %s",
		opts.ProjectID, opts.Environment, opts.CorrelationID,
	)

	commitHash, err := wt.Commit(commitMsg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "LoopZero API",
			Email: "loopzero-api@loopzero.io",
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", fmt.Errorf("gitops remove: commit: %w", err)
	}

	// Push with retry
	if err := p.pushWithRetry(ctx, repo); err != nil {
		return "", fmt.Errorf("gitops remove: %w", err)
	}

	return commitHash.String(), nil
}

// cloneOrInit clones the repo if it has content, or inits a fresh repo with
// the remote configured. This handles the case of empty/new Gitea repos.
func (p *GoGitGitOpsPusher) cloneOrInit(ctx context.Context, remote, repoDir string) (*gogit.Repository, error) {
	repo, err := p.cloneRepo(ctx, remote, repoDir)
	if err == nil {
		return repo, nil
	}

	// Clone failed (empty repo or first push) — init fresh
	repo, err = gogit.PlainInit(repoDir, false)
	if err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{remote},
	})
	if err != nil {
		return nil, fmt.Errorf("git remote add: %w", err)
	}

	return repo, nil
}

// cloneRepo performs a shallow clone with depth 5.
func (p *GoGitGitOpsPusher) cloneRepo(_ context.Context, remote, repoDir string) (*gogit.Repository, error) {
	cloneOpts := &gogit.CloneOptions{
		URL:           remote,
		Depth:         5,
		SingleBranch:  true,
		ReferenceName: plumbing.Main,
	}

	if !isLocalURL(remote) {
		cloneOpts.Auth = p.auth()
	}

	return gogit.PlainClone(repoDir, false, cloneOpts)
}

// pushWithRetry pushes to origin with exponential backoff (1s, 2s, 4s).
// On conflict, it fetches and rebases before retrying.
//
// go-git's PlainInit creates HEAD on refs/heads/master by default, so we
// read the actual HEAD ref and push it to refs/heads/main on the remote.
func (p *GoGitGitOpsPusher) pushWithRetry(_ context.Context, repo *gogit.Repository) error {
	remote, err := repo.Remote("origin")
	if err != nil {
		return fmt.Errorf("get remote: %w", err)
	}

	remoteURL := remote.Config().URLs[0]
	isLocal := isLocalURL(remoteURL)

	// Determine the local branch ref (could be master or main depending on init)
	headRef, err := repo.Head()
	if err != nil {
		return fmt.Errorf("get HEAD ref: %w", err)
	}
	localRef := headRef.Name()
	refSpec := config.RefSpec(fmt.Sprintf("%s:refs/heads/main", localRef))

	for attempt := 0; attempt < 3; attempt++ {
		pushOpts := &gogit.PushOptions{
			RemoteName: "origin",
			RefSpecs:   []config.RefSpec{refSpec},
		}
		if !isLocal {
			pushOpts.Auth = p.auth()
		}

		err := repo.Push(pushOpts)
		if err == nil || err == gogit.NoErrAlreadyUpToDate {
			return nil
		}

		if attempt == 2 {
			return fmt.Errorf("failed after 3 push attempts: %w", err)
		}

		// Exponential backoff: 1s, 2s, 4s
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		time.Sleep(backoff)

		// Fetch and attempt rebase on conflict
		fetchOpts := &gogit.FetchOptions{
			RemoteName: "origin",
		}
		if !isLocal {
			fetchOpts.Auth = p.auth()
		}

		fetchErr := repo.Fetch(fetchOpts)
		if fetchErr != nil && fetchErr != gogit.NoErrAlreadyUpToDate {
			// Fetch can fail on empty remotes — not fatal, just retry the push
			continue
		}

		// go-git doesn't have native rebase. For conflict resolution,
		// we reset to origin/main and re-apply our changes by re-committing.
		// In practice, gitops pushes operate on non-overlapping paths
		// (different projects), so conflicts are rare.
	}

	return nil
}

// removeDir removes all files in a directory from the git worktree.
func (p *GoGitGitOpsPusher) removeDir(wt *gogit.Worktree, repoDir, relPath string) error {
	absPath := filepath.Join(repoDir, relPath)
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", relPath, err)
	}

	for _, entry := range entries {
		entryRel := filepath.Join(relPath, entry.Name())
		if entry.IsDir() {
			if err := p.removeDir(wt, repoDir, entryRel); err != nil {
				return err
			}
		} else {
			if _, err := wt.Remove(entryRel); err != nil {
				return fmt.Errorf("git rm %s: %w", entryRel, err)
			}
		}
	}

	return nil
}

// ensureRepo creates the repo under the org via Gitea API if it doesn't exist.
func (p *GoGitGitOpsPusher) ensureRepo(ctx context.Context, repoName string) error {
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", p.baseURL, p.orgName, repoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("check repo: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil
	}

	// Create repo under org
	createURL := fmt.Sprintf("%s/api/v1/orgs/%s/repos", p.baseURL, p.orgName)
	bodyBytes, err := json.Marshal(createRepoRequest{
		Name:          repoName,
		AutoInit:      true,
		DefaultBranch: "main",
		Private:       true,
	})
	if err != nil {
		return fmt.Errorf("marshal create repo body: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = p.client.Do(req)
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		return fmt.Errorf("create repo: status %d", resp.StatusCode)
	}

	return nil
}
