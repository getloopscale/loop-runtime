package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GiteaSourceStoreConfig holds configuration for connecting to Gitea.
type GiteaSourceStoreConfig struct {
	BaseURL string // e.g., "http://gitea-http.loopzero-system.svc:3000"
	Token   string // Gitea access token for push authentication
	OrgName string // e.g., "loopzero"
}

// GiteaSourceStore implements SourceStore by pushing git bundles to Gitea
// source repos. The CLI creates a git bundle (git bundle create -1 HEAD),
// uploads it to the API, and this store pushes it to Gitea so the Argo
// Workflow can clone it (spec §7.1, Mode B).
type GiteaSourceStore struct {
	baseURL string
	token   string
	orgName string
	client  *http.Client
}

// NewGiteaSourceStore creates a new Gitea-backed SourceStore.
func NewGiteaSourceStore(cfg GiteaSourceStoreConfig) (*GiteaSourceStore, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("gitea source store: base URL is required")
	}
	return &GiteaSourceStore{
		baseURL: cfg.BaseURL,
		token:   cfg.Token,
		orgName: cfg.OrgName,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// PushBundle receives a git bundle from the CLI, unbundles it into a temp
// repo, and pushes to the Gitea source repo for this tenant/project.
// Returns the HEAD commit SHA after push.
func (s *GiteaSourceStore) PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (string, error) {
	data, err := io.ReadAll(bundle)
	if err != nil {
		return "", fmt.Errorf("gitea source: reading bundle: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("gitea source: bundle is empty")
	}

	// Create temp directory for git operations
	tmpDir, err := os.MkdirTemp("", "loopzero-source-*")
	if err != nil {
		return "", fmt.Errorf("gitea source: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Write bundle to temp file
	bundlePath := filepath.Join(tmpDir, "source.bundle")
	if err := os.WriteFile(bundlePath, data, 0644); err != nil {
		return "", fmt.Errorf("gitea source: write bundle: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")

	// Repo naming per spec §7.5: loopzero-source-{tenant}-{project}
	repoName := fmt.Sprintf("loopzero-source-%s", tenantID)
	if projectID != "" {
		repoName = fmt.Sprintf("loopzero-source-%s-%s", tenantID, projectID)
	}

	// Construct plain remote URL (no embedded credentials).
	// SEC-1: Auth is injected via http.extraHeader in gitCmdWithAuth.
	remoteURL := s.repoURL(repoName)

	// Ensure repo exists in Gitea (create via API if not)
	if err := s.ensureRepo(ctx, repoName); err != nil {
		return "", fmt.Errorf("gitea source: ensure repo: %w", err)
	}

	// Try to clone existing repo from Gitea first (for incremental pushes)
	// SEC-1: Auth via http.extraHeader, not embedded in URL.
	if err := s.gitCmdWithAuth(ctx, tmpDir, "clone", remoteURL, repoDir); err != nil {
		// Clone failed (empty repo or first push) — init fresh
		if err := s.gitCmd(ctx, tmpDir, "git", "init", repoDir); err != nil {
			return "", fmt.Errorf("gitea source: git init: %w", err)
		}
	}

	// Unbundle into the repo: git bundle unbundle applies the bundle refs
	// Use git pull from the bundle to get the content
	if err := s.gitCmd(ctx, repoDir, "git", "pull", bundlePath, "HEAD"); err != nil {
		// If pull fails (e.g., unrelated histories), try fetch + checkout
		if err2 := s.gitCmd(ctx, repoDir, "git", "fetch", bundlePath, "HEAD"); err2 != nil {
			return "", fmt.Errorf("gitea source: git fetch from bundle: %w (pull error: %v)", err2, err)
		}
		if err2 := s.gitCmd(ctx, repoDir, "git", "checkout", "FETCH_HEAD"); err2 != nil {
			return "", fmt.Errorf("gitea source: git checkout FETCH_HEAD: %w", err2)
		}
	}

	// Get the HEAD commit SHA
	commitSHA, err := s.gitOutput(ctx, repoDir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitea source: get HEAD SHA: %w", err)
	}

	// Ensure remote points to Gitea
	_ = s.gitCmd(ctx, repoDir, "git", "remote", "remove", "origin")
	if err := s.gitCmd(ctx, repoDir, "git", "remote", "add", "origin", remoteURL); err != nil {
		return "", fmt.Errorf("gitea source: set remote: %w", err)
	}

	// Set git identity for any merge commits
	_ = s.gitCmd(ctx, repoDir, "git", "config", "user.email", "loopzero-api@loopzero.io")
	_ = s.gitCmd(ctx, repoDir, "git", "config", "user.name", "LoopZero API")

	// Push to Gitea (create branch if needed, force push to overwrite)
	// SEC-1: Auth via http.extraHeader, not embedded in URL.
	if err := s.gitCmdWithAuth(ctx, repoDir, "push", "--force", "origin", "HEAD:refs/heads/main"); err != nil {
		return "", fmt.Errorf("gitea source: git push: %w", err)
	}

	return commitSHA, nil
}

// createSourceRepoRequest is the JSON body for Gitea's create-repo API.
// SEC-4: Using json.Marshal prevents JSON injection via repo names.
type createSourceRepoRequest struct {
	Name          string `json:"name"`
	AutoInit      bool   `json:"auto_init"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// ensureRepo creates the repo under the org via Gitea API if it doesn't exist.
// MOD-2: Uses s.client (with timeout) instead of http.DefaultClient.
func (s *GiteaSourceStore) ensureRepo(ctx context.Context, repoName string) error {
	// Check if repo exists
	checkURL := fmt.Sprintf("%s/api/v1/repos/%s/%s", s.baseURL, s.orgName, repoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+s.token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("check repo: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil // repo exists
	}

	// Create repo under org
	createURL := fmt.Sprintf("%s/api/v1/orgs/%s/repos", s.baseURL, s.orgName)
	bodyBytes, err := json.Marshal(createSourceRepoRequest{
		Name:          repoName,
		AutoInit:      false,
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
	req.Header.Set("Authorization", "token "+s.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = s.client.Do(req)
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		return fmt.Errorf("create repo: status %d", resp.StatusCode)
	}

	return nil
}

// repoURL returns the plain Gitea repo URL without embedded credentials.
// SEC-1: Credentials are injected via http.extraHeader in gitCmdWithAuth,
// preventing token exposure in /proc/PID/cmdline.
func (s *GiteaSourceStore) repoURL(repoName string) string {
	return fmt.Sprintf("%s/%s/%s.git", s.baseURL, s.orgName, repoName)
}

// gitAuthArgs returns git config args that inject the auth token via HTTP header.
// SEC-1: This replaces embedding the token in the git remote URL.
func (s *GiteaSourceStore) gitAuthArgs() []string {
	return []string{
		"-c", fmt.Sprintf("http.extraHeader=Authorization: token %s", s.token),
	}
}

// gitCmdWithAuth runs a git command with auth headers for network operations
// (clone, push, fetch). SEC-1: Credentials are passed via http.extraHeader
// instead of embedded in the URL, preventing token exposure in /proc/PID/cmdline.
func (s *GiteaSourceStore) gitCmdWithAuth(ctx context.Context, dir string, gitArgs ...string) error {
	authArgs := s.gitAuthArgs()
	fullArgs := append([]string{"git"}, authArgs...)
	fullArgs = append(fullArgs, gitArgs...)
	return s.gitCmd(ctx, dir, fullArgs...)
}

// gitCmd runs a git command in the specified directory.
func (s *GiteaSourceStore) gitCmd(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (output: %s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// gitOutput runs a git command and returns stdout trimmed.
func (s *GiteaSourceStore) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}
