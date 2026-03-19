// Package gitops implements the GitOps pusher that commits manifests to Gitea
// tenant repos. This is the Step 6 architectural fix: the API now owns the
// entire release flow — build produces artifacts only, API commits to GitOps.
package gitops

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/loopzero/loopzero/api/internal/services"
)

// GiteaGitOpsPusherConfig holds configuration for connecting to Gitea GitOps repos.
type GiteaGitOpsPusherConfig struct {
	BaseURL string // e.g., "http://gitea-http.loopzero-system.svc:3000"
	Token   string // Gitea access token for push authentication
	OrgName string // e.g., "loopzero"
}

// GiteaGitOpsPusher implements services.GitOpsPusher by committing manifests
// to the tenant GitOps repo in Gitea.
//
// Repo naming: loopzero-tenant-{tenantID}
// File path: {projectID}/{environment}/manifests.yaml
// Git trailers: Release-ID, Correlation-ID, Build-ID (spec §17.5)
type GiteaGitOpsPusher struct {
	baseURL string
	token   string
	orgName string
}

// NewGiteaGitOpsPusher creates a new GiteaGitOpsPusher.
func NewGiteaGitOpsPusher(cfg GiteaGitOpsPusherConfig) *GiteaGitOpsPusher {
	return &GiteaGitOpsPusher{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		orgName: cfg.OrgName,
	}
}

// Push commits manifests to the tenant GitOps repo and returns the commit SHA.
// The operation is idempotent: pushing the same manifests twice is safe (git
// will detect "nothing to commit" and we return the current HEAD SHA).
func (p *GiteaGitOpsPusher) Push(ctx context.Context, opts services.GitOpsPushOpts) (string, error) {
	if opts.Manifests == "" {
		return "", fmt.Errorf("gitops push: manifests cannot be empty")
	}

	repoName := fmt.Sprintf("loopzero-tenant-%s", opts.TenantID)
	remoteURL := p.authenticatedURL(repoName)

	// Ensure repo exists in Gitea
	if err := p.ensureRepo(ctx, repoName); err != nil {
		return "", fmt.Errorf("gitops push: ensure repo: %w", err)
	}

	// Create temp directory for git operations
	tmpDir, err := os.MkdirTemp("", "loopzero-gitops-push-*")
	if err != nil {
		return "", fmt.Errorf("gitops push: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")

	// Clone existing repo (or init fresh if empty)
	if err := p.gitCmd(ctx, tmpDir, "git", "clone", "--depth", "5", remoteURL, repoDir); err != nil {
		// Clone failed (empty repo or first push) — init fresh
		if err := p.gitCmd(ctx, tmpDir, "git", "init", repoDir); err != nil {
			return "", fmt.Errorf("gitops push: git init: %w", err)
		}
		_ = p.gitCmd(ctx, repoDir, "git", "remote", "add", "origin", remoteURL)
	}

	// Set git identity
	_ = p.gitCmd(ctx, repoDir, "git", "config", "user.email", "loopzero-api@loopzero.io")
	_ = p.gitCmd(ctx, repoDir, "git", "config", "user.name", "LoopZero API")

	// Write manifests to {projectID}/{environment}/manifests.yaml
	envPath := filepath.Join(repoDir, opts.ProjectID, opts.Environment)
	if err := os.MkdirAll(envPath, 0755); err != nil {
		return "", fmt.Errorf("gitops push: create dir: %w", err)
	}
	manifestFile := filepath.Join(envPath, "manifests.yaml")
	if err := os.WriteFile(manifestFile, []byte(opts.Manifests), 0644); err != nil {
		return "", fmt.Errorf("gitops push: write manifests: %w", err)
	}

	// Stage changes
	relPath := filepath.Join(opts.ProjectID, opts.Environment)
	if err := p.gitCmd(ctx, repoDir, "git", "add", relPath+"/"); err != nil {
		return "", fmt.Errorf("gitops push: git add: %w", err)
	}

	// Commit with git trailers (spec §17.5)
	commitMsg := fmt.Sprintf("Deploy %s to %s\n\nRelease-ID: %s\nCorrelation-ID: %s\nBuild-ID: %s",
		opts.ProjectID, opts.Environment,
		opts.ReleaseID, opts.CorrelationID, opts.BuildID,
	)

	// git commit — if nothing changed, this is idempotent (no error)
	commitErr := p.gitCmd(ctx, repoDir, "git", "commit", "-m", commitMsg)
	if commitErr != nil {
		// "nothing to commit" is not an error for idempotency
		if strings.Contains(commitErr.Error(), "nothing to commit") {
			sha, err := p.gitOutput(ctx, repoDir, "git", "rev-parse", "HEAD")
			if err != nil {
				return "", fmt.Errorf("gitops push: get HEAD after no-op commit: %w", err)
			}
			return sha, nil
		}
		return "", fmt.Errorf("gitops push: git commit: %w", commitErr)
	}

	// Push with fetch-rebase-push on conflict (up to 3 attempts)
	for attempt := 0; attempt < 3; attempt++ {
		if err := p.gitCmd(ctx, repoDir, "git", "push", "origin", "HEAD:refs/heads/main"); err == nil {
			break
		}
		if attempt == 2 {
			return "", fmt.Errorf("gitops push: failed after 3 push attempts")
		}
		// Fetch and rebase on conflict
		_ = p.gitCmd(ctx, repoDir, "git", "fetch", "origin")
		if err := p.gitCmd(ctx, repoDir, "git", "rebase", "origin/main"); err != nil {
			return "", fmt.Errorf("gitops push: rebase failed: %w", err)
		}
	}

	// Return the commit SHA
	sha, err := p.gitOutput(ctx, repoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitops push: get commit SHA: %w", err)
	}

	return sha, nil
}

// ensureRepo creates the repo under the org via Gitea API if it doesn't exist.
func (p *GiteaGitOpsPusher) ensureRepo(ctx context.Context, repoName string) error {
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", p.baseURL, p.orgName, repoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("check repo: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil
	}

	// Create repo under org
	createURL := fmt.Sprintf("%s/api/v1/orgs/%s/repos", p.baseURL, p.orgName)
	body := fmt.Sprintf(`{"name":"%s","auto_init":true,"default_branch":"main","private":true}`, repoName)

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, createURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		return fmt.Errorf("create repo: status %d", resp.StatusCode)
	}

	return nil
}

// authenticatedURL builds the Gitea repo URL with token auth embedded.
func (p *GiteaGitOpsPusher) authenticatedURL(repoName string) string {
	url := fmt.Sprintf("%s/%s/%s.git", p.baseURL, p.orgName, repoName)
	url = strings.Replace(url, "://", fmt.Sprintf("://oauth2:%s@", p.token), 1)
	return url
}

// gitCmd runs a git command in the specified directory.
// SEC-3: Output is sanitized to prevent token leakage in logs.
func (p *GiteaGitOpsPusher) gitCmd(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		sanitized := p.sanitizeOutput(string(output))
		safeArgs := p.sanitizeArgs(args)
		return fmt.Errorf("%s: %w (output: %s)", strings.Join(safeArgs, " "), err, strings.TrimSpace(sanitized))
	}
	return nil
}

// sanitizeOutput removes the Gitea token from git command output to prevent leakage in logs.
func (p *GiteaGitOpsPusher) sanitizeOutput(output string) string {
	if p.token == "" {
		return output
	}
	return strings.ReplaceAll(output, p.token, "***")
}

// sanitizeArgs removes credentials from git command arguments for safe logging.
func (p *GiteaGitOpsPusher) sanitizeArgs(args []string) []string {
	if p.token == "" {
		return args
	}
	safe := make([]string, len(args))
	for i, arg := range args {
		safe[i] = strings.ReplaceAll(arg, p.token, "***")
	}
	return safe
}

// gitOutput runs a git command and returns stdout trimmed.
func (p *GiteaGitOpsPusher) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}
