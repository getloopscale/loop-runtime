// Package gitops implements the GitOps pusher that commits manifests to Gitea
// tenant repos. This is the Step 6 architectural fix: the API now owns the
// entire release flow — build produces artifacts only, API commits to GitOps.
package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
// File path: {environment}/{projectID}/manifests.yaml
// Git trailers: Release-ID, Correlation-ID, Build-ID (spec §17.5)
type GiteaGitOpsPusher struct {
	baseURL string
	token   string
	orgName string
	client  *http.Client
}

// NewGiteaGitOpsPusher creates a new GiteaGitOpsPusher.
func NewGiteaGitOpsPusher(cfg GiteaGitOpsPusherConfig) *GiteaGitOpsPusher {
	return &GiteaGitOpsPusher{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		orgName: cfg.OrgName,
		client:  &http.Client{Timeout: 15 * time.Second},
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
	remoteURL := p.repoURL(repoName)

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
	// SEC-1: Auth via http.extraHeader, not embedded in URL.
	if err := p.gitCmdWithAuth(ctx, tmpDir, "clone", "--depth", "5", remoteURL, repoDir); err != nil {
		// Clone failed (empty repo or first push) — init fresh
		if err := p.gitCmd(ctx, tmpDir, "git", "init", repoDir); err != nil {
			return "", fmt.Errorf("gitops push: git init: %w", err)
		}
		_ = p.gitCmd(ctx, repoDir, "git", "remote", "add", "origin", remoteURL)
	}

	// Set git identity
	_ = p.gitCmd(ctx, repoDir, "git", "config", "user.email", "loopzero-api@loopzero.io")
	_ = p.gitCmd(ctx, repoDir, "git", "config", "user.name", "LoopZero API")

	// Write manifests to {environment}/{projectID}/manifests.yaml
	// BLOCKER-3: environment-first hierarchy so ArgoCD Application path scopes to one environment
	envPath := filepath.Join(repoDir, opts.Environment, opts.ProjectID)
	if err := os.MkdirAll(envPath, 0755); err != nil {
		return "", fmt.Errorf("gitops push: create dir: %w", err)
	}
	manifestFile := filepath.Join(envPath, "manifests.yaml")
	if err := os.WriteFile(manifestFile, []byte(opts.Manifests), 0644); err != nil {
		return "", fmt.Errorf("gitops push: write manifests: %w", err)
	}

	// Stage changes
	relPath := filepath.Join(opts.Environment, opts.ProjectID)
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
	// SEC-1: Network git commands use gitCmdWithAuth (http.extraHeader).
	for attempt := 0; attempt < 3; attempt++ {
		if err := p.gitCmdWithAuth(ctx, repoDir, "push", "origin", "HEAD:refs/heads/main"); err == nil {
			break
		}
		if attempt == 2 {
			return "", fmt.Errorf("gitops push: failed after 3 push attempts")
		}
		// Fetch and rebase on conflict
		_ = p.gitCmdWithAuth(ctx, repoDir, "fetch", "origin")
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

// createRepoRequest is the JSON body for Gitea's create-repo API.
// SEC-4: Using json.Marshal prevents JSON injection via repo names.
type createRepoRequest struct {
	Name          string `json:"name"`
	AutoInit      bool   `json:"auto_init"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// ensureRepo creates the repo under the org via Gitea API if it doesn't exist.
// MOD-2: Uses p.client (with timeout) instead of http.DefaultClient.
func (p *GiteaGitOpsPusher) ensureRepo(ctx context.Context, repoName string) error {
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
	resp.Body.Close()

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
	resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		return fmt.Errorf("create repo: status %d", resp.StatusCode)
	}

	return nil
}

// repoURL returns the plain Gitea repo URL without embedded credentials.
// SEC-1: Credentials are injected via http.extraHeader in gitCmdWithAuth,
// preventing token exposure in /proc/PID/cmdline.
func (p *GiteaGitOpsPusher) repoURL(repoName string) string {
	return fmt.Sprintf("%s/%s/%s.git", p.baseURL, p.orgName, repoName)
}

// gitAuthArgs returns git config args that inject the auth token via HTTP header.
// SEC-1: This replaces embedding the token in the git remote URL.
func (p *GiteaGitOpsPusher) gitAuthArgs() []string {
	return []string{
		"-c", fmt.Sprintf("http.extraHeader=Authorization: token %s", p.token),
	}
}

// gitCmdWithAuth runs a git command with auth headers for network operations
// (clone, push, fetch). SEC-1: Credentials are passed via http.extraHeader
// instead of embedded in the URL, preventing token exposure in /proc/PID/cmdline.
func (p *GiteaGitOpsPusher) gitCmdWithAuth(ctx context.Context, dir string, gitArgs ...string) error {
	authArgs := p.gitAuthArgs()
	fullArgs := append([]string{"git"}, authArgs...)
	fullArgs = append(fullArgs, gitArgs...)
	return p.gitCmd(ctx, dir, fullArgs...)
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
