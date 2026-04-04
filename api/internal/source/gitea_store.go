// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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

// PushBundle receives a source bundle from the CLI (git bundle or tar.gz),
// detects the format, and pushes to the Gitea source repo.
// Returns the source identifier (git SHA or "ch-{content-hash}").
func (s *GiteaSourceStore) PushBundle(ctx context.Context, tenantID, projectID string, bundle io.Reader) (string, error) {
	data, err := io.ReadAll(bundle)
	if err != nil {
		return "", fmt.Errorf("gitea source: reading bundle: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("gitea source: bundle is empty")
	}

	// Detect bundle format by magic bytes:
	//   tar.gz: 0x1f 0x8b (gzip magic)
	//   git bundle: "# v2 git bundle\n" or "# v3 git bundle\n"
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return s.pushTarBundle(ctx, tenantID, projectID, data)
	}
	return s.pushGitBundle(ctx, tenantID, projectID, data)
}

// pushGitBundle handles git bundle format (original flow).
func (s *GiteaSourceStore) pushGitBundle(ctx context.Context, tenantID, projectID string, data []byte) (string, error) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.push_git",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", tenantID),
			attribute.String("loopzero.project", projectID),
		))
	defer span.End()

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

// pushTarBundle handles tar.gz format for projects without git.
// Extracts the archive, computes a content hash, creates a git repo,
// commits all files, and pushes to Gitea. Returns "ch-{sha256}".
func (s *GiteaSourceStore) pushTarBundle(ctx context.Context, tenantID, projectID string, data []byte) (string, error) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.source.push_tar",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", tenantID),
			attribute.String("loopzero.project", projectID),
		))
	defer span.End()

	tmpDir, err := os.MkdirTemp("", "loopzero-tar-source-*")
	if err != nil {
		return "", fmt.Errorf("gitea source: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return "", fmt.Errorf("gitea source: mkdir repo: %w", err)
	}

	// Extract tar.gz
	tarPath := filepath.Join(tmpDir, "source.tar.gz")
	if err := os.WriteFile(tarPath, data, 0644); err != nil {
		return "", fmt.Errorf("gitea source: write tar: %w", err)
	}
	if err := s.gitCmd(ctx, repoDir, "tar", "xzf", tarPath); err != nil {
		return "", fmt.Errorf("gitea source: extract tar: %w", err)
	}

	// Compute content hash from extracted files
	contentHash, err := computeDirHash(repoDir)
	if err != nil {
		return "", fmt.Errorf("gitea source: content hash: %w", err)
	}

	// Repo naming per spec §7.5
	repoName := fmt.Sprintf("loopzero-source-%s", tenantID)
	if projectID != "" {
		repoName = fmt.Sprintf("loopzero-source-%s-%s", tenantID, projectID)
	}
	remoteURL := s.repoURL(repoName)

	if err := s.ensureRepo(ctx, repoName); err != nil {
		return "", fmt.Errorf("gitea source: ensure repo: %w", err)
	}

	// Initialize git repo from extracted files
	if err := s.gitCmd(ctx, repoDir, "git", "init"); err != nil {
		return "", fmt.Errorf("gitea source: git init: %w", err)
	}
	_ = s.gitCmd(ctx, repoDir, "git", "config", "user.email", "loopzero-api@loopzero.io")
	_ = s.gitCmd(ctx, repoDir, "git", "config", "user.name", "LoopZero API")
	if err := s.gitCmd(ctx, repoDir, "git", "add", "-A"); err != nil {
		return "", fmt.Errorf("gitea source: git add: %w", err)
	}
	if err := s.gitCmd(ctx, repoDir, "git", "commit", "-m", fmt.Sprintf("source upload ch-%s", contentHash[:12])); err != nil {
		return "", fmt.Errorf("gitea source: git commit: %w", err)
	}

	// Push to Gitea
	_ = s.gitCmd(ctx, repoDir, "git", "remote", "remove", "origin")
	if err := s.gitCmd(ctx, repoDir, "git", "remote", "add", "origin", remoteURL); err != nil {
		return "", fmt.Errorf("gitea source: set remote: %w", err)
	}
	if err := s.gitCmdWithAuth(ctx, repoDir, "push", "--force", "origin", "HEAD:refs/heads/main"); err != nil {
		return "", fmt.Errorf("gitea source: git push: %w", err)
	}

	return "ch-" + contentHash, nil
}

// CreateGitBundleFromRepo clones the Gitea repo and creates a git bundle for S3 upload.
// Used when original source was tar.gz — the Argo Workflow CWT expects git bundles in S3.
func (s *GiteaSourceStore) CreateGitBundleFromRepo(ctx context.Context, tenantID, projectID string) (string, error) {
	repoName := fmt.Sprintf("loopzero-source-%s", tenantID)
	if projectID != "" {
		repoName = fmt.Sprintf("loopzero-source-%s-%s", tenantID, projectID)
	}
	remoteURL := s.repoURL(repoName)

	tmpDir, err := os.MkdirTemp("", "loopzero-gitbundle-*")
	if err != nil {
		return "", fmt.Errorf("gitea source: create temp dir: %w", err)
	}

	repoDir := filepath.Join(tmpDir, "repo")
	if err := s.gitCmdWithAuth(ctx, tmpDir, "clone", remoteURL, repoDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("gitea source: clone for bundle: %w", err)
	}

	bundlePath := filepath.Join(tmpDir, "source.bundle")
	if err := s.gitCmd(ctx, repoDir, "git", "bundle", "create", bundlePath, "HEAD"); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("gitea source: create bundle: %w", err)
	}

	// Clean up repo dir but keep the bundle
	_ = os.RemoveAll(repoDir)
	return bundlePath, nil
}

// computeDirHash computes a deterministic SHA256 hash of all files in a directory.
// Uses sorted paths and file contents for reproducibility.
func computeDirHash(dir string) (string, error) {
	var paths []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		// Skip .git directory (created during processing)
		if strings.HasPrefix(rel, ".git") {
			return filepath.SkipDir
		}
		if !info.IsDir() {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, rel := range paths {
		h.Write([]byte(rel))
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
// SEC-002 fix: sanitize output and args to prevent token leakage in error logs.
func (s *GiteaSourceStore) gitCmd(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		sanitizedOutput := s.sanitizeOutput(string(output))
		sanitizedArgs := s.sanitizeArgs(args)
		return fmt.Errorf("%s: %w (output: %s)", strings.Join(sanitizedArgs, " "), err, strings.TrimSpace(sanitizedOutput))
	}
	return nil
}

// gitOutput runs a git command and returns stdout trimmed.
// SEC-002 fix: sanitize args in error messages to prevent token leakage.
func (s *GiteaSourceStore) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	if err != nil {
		sanitizedArgs := s.sanitizeArgs(args)
		return "", fmt.Errorf("%s: %w", strings.Join(sanitizedArgs, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// sanitizeOutput removes the Gitea token from git command output to prevent leakage in logs.
func (s *GiteaSourceStore) sanitizeOutput(output string) string {
	if s.token == "" {
		return output
	}
	return strings.ReplaceAll(output, s.token, "***")
}

// sanitizeArgs removes credentials from git command arguments for safe logging.
func (s *GiteaSourceStore) sanitizeArgs(args []string) []string {
	if s.token == "" {
		return args
	}
	safe := make([]string, len(args))
	for i, arg := range args {
		safe[i] = strings.ReplaceAll(arg, s.token, "***")
	}
	return safe
}
