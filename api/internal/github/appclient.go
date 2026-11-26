// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package github provides clients for the GitHub API.
// AppClient authenticates as a GitHub App using JWT + installation tokens.
// Used for: source download, commit status updates, PR comments.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// DefaultGitHubAPIBase is the canonical github.com API root. Used as
	// the fallback when GITHUB_API_BASE_URL is unset. Operators on
	// GitHub Enterprise Server (GHES) override via env to point at
	// their own appliance (e.g. https://github.acme.corp/api/v3).
	DefaultGitHubAPIBase = "https://api.github.com"
)

// AppClient authenticates as a GitHub App and manages installation tokens.
// Thread-safe: tokens are cached and refreshed automatically.
type AppClient struct {
	appID      int64
	privateKey *rsa.PrivateKey
	httpClient *http.Client
	baseURL    string // GitHub REST root; empty means DefaultGitHubAPIBase.

	mu         sync.RWMutex
	tokenCache map[int64]*cachedToken
}

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// AppID is the id of the GitHub App this client authenticates as.
func (c *AppClient) AppID() int64 { return c.appID }

// NewAppClient creates a GitHub App client from App ID and PEM-encoded private key.
// Returns nil if credentials are missing (acceptable in local dev).
// apiBaseURL is the GitHub REST root (config.GitHubAPIBaseURL); a GitHub
// Enterprise Server install points it at its appliance. Empty means
// DefaultGitHubAPIBase.
func NewAppClient(appID int64, privateKeyPEM, apiBaseURL string) (*AppClient, error) {
	if appID == 0 || privateKeyPEM == "" {
		return nil, nil
	}

	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("github app: failed to decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("github app: failed to parse private key: %w", err)
	}

	return &AppClient{
		appID:      appID,
		privateKey: key,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    apiBaseURL,
		tokenCache: make(map[int64]*cachedToken),
	}, nil
}

// apiBase returns the per-client GitHub API base URL. Falls back to
// DefaultGitHubAPIBase when the field is zero — defense-in-depth so
// struct-literal construction in tests (without NewAppClient) doesn't
// produce empty URLs.
func (c *AppClient) apiBase() string {
	if c.baseURL == "" {
		return DefaultGitHubAPIBase
	}
	return c.baseURL
}

// GetInstallationToken returns a valid installation access token.
// Cached until 5 minutes before expiry, then refreshed.
func (c *AppClient) GetInstallationToken(ctx context.Context, installationID int64) (string, error) {
	// Check cache
	c.mu.RLock()
	if cached, ok := c.tokenCache[installationID]; ok {
		if time.Now().Before(cached.ExpiresAt.Add(-5 * time.Minute)) {
			c.mu.RUnlock()
			return cached.Token, nil
		}
	}
	c.mu.RUnlock()

	// Refresh token
	appJWT, err := c.generateJWT()
	if err != nil {
		return "", fmt.Errorf("github app: generate JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase(), installationID), nil)
	if err != nil {
		return "", fmt.Errorf("github app: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github app: token exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github app: token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("github app: decode token response: %w", err)
	}

	// Cache
	c.mu.Lock()
	c.tokenCache[installationID] = &cachedToken{
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt,
	}
	c.mu.Unlock()

	return result.Token, nil
}

// generateJWT creates a short-lived JWT signed with the App's private key.
// GitHub accepts JWTs with max 10 minute expiry.
func (c *AppClient) generateJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", c.appID),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // clock skew tolerance
		ExpiresAt: jwt.NewNumericDate(now.Add(8 * time.Minute)),   // 8min (max 10)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(c.privateKey)
}

// doAuthenticated makes an authenticated HTTP request using the installation token.
func (c *AppClient) doAuthenticated(ctx context.Context, installationID int64, method, url string, body io.Reader) (*http.Response, error) {
	token, err := c.GetInstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("github api: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// --- Source Download ---

// DownloadSource downloads the source tarball for a specific commit.
// Returns a ReadCloser that streams the tarball (caller must close).
func (c *AppClient) DownloadSource(ctx context.Context, installationID int64, owner, repo, sha string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", c.apiBase(), owner, repo, sha)
	resp, err := c.doAuthenticated(ctx, installationID, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: download source: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("github: download source (status %d): %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
}

// --- Commit Status ---

// CommitStatus represents a GitHub commit status.
type CommitStatus struct {
	State       string // "pending", "success", "failure", "error"
	TargetURL   string // URL to LoopRuntime deploy/logs
	Description string // Short description (max 140 chars)
	Context     string // "loopruntime/deploy" or "loopruntime/preview"
}

// SetCommitStatus updates the commit status on GitHub.
func (c *AppClient) SetCommitStatus(ctx context.Context, installationID int64, owner, repo, sha string, status CommitStatus) error {
	url := fmt.Sprintf("%s/repos/%s/%s/statuses/%s", c.apiBase(), owner, repo, sha)

	payload, err := json.Marshal(struct {
		State       string `json:"state"`
		TargetURL   string `json:"target_url,omitempty"`
		Description string `json:"description"`
		Context     string `json:"context"`
	}{status.State, status.TargetURL, status.Description, status.Context})
	if err != nil {
		return fmt.Errorf("github: encode commit status: %w", err)
	}

	resp, err := c.doAuthenticated(ctx, installationID, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("github: set commit status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github: set commit status (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// Plan J-09 T5.1 + T2.1 (2026-06-05) — repo introspection helpers used by
// the Connect handler (loopzero.yaml validation) and the dashboard repo picker.

// BranchNotFoundError is the typed error BranchExists returns when the ref is
// absent, so a caller can tell "branch not found" from "file not found" (EC-10).
type BranchNotFoundError struct {
	Owner, Repo, Ref string
}

func (e *BranchNotFoundError) Error() string {
	return fmt.Sprintf("branch %q not found in %s/%s", e.Ref, e.Owner, e.Repo)
}

// BranchExists returns nil if the given ref exists on the repository, OR a
// *BranchNotFoundError so the caller can distinguish it from "file not found"
// (EC-10). Other HTTP errors propagate as-is.
//
// Calls GET /repos/{owner}/{repo}/branches/{ref} per the GitHub REST API.
func (c *AppClient) BranchExists(ctx context.Context, installationID int64, owner, repo, ref string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/branches/%s", c.apiBase(), owner, repo, ref)
	resp, err := c.doAuthenticated(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("github branch check: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return &BranchNotFoundError{Owner: owner, Repo: repo, Ref: ref}
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github branch check failed (status %d): %s", resp.StatusCode, body)
	}
}

// FileNotFoundError is returned by GetFileContent when the path is absent
// on the requested ref. Lets the Connect handler distinguish MISSING_DEPLOY_DESCRIPTOR
// from any other HTTP error class.
type FileNotFoundError struct {
	Owner, Repo, Ref, Path string
}

func (e *FileNotFoundError) Error() string {
	return fmt.Sprintf("file %q not found at %s in %s/%s", e.Path, e.Ref, e.Owner, e.Repo)
}

// GetFileContent fetches the decoded raw bytes of a repository file at a
// specific ref. Calls GET /repos/{owner}/{repo}/contents/{path}?ref={ref}.
// GitHub returns base64-encoded content under .content; we decode it here
// so callers receive a plain []byte ready for yaml.Unmarshal.
//
// Plan J-09 T5.1 — used by Connect handler to validate loopzero.yaml exists and
// parses as YAML before writing the project↔repo link.
func (c *AppClient) GetFileContent(ctx context.Context, installationID int64, owner, repo, ref, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.apiBase(), owner, repo, path, ref)
	resp, err := c.doAuthenticated(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github file fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, &FileNotFoundError{Owner: owner, Repo: repo, Ref: ref, Path: path}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github file fetch failed (status %d): %s", resp.StatusCode, body)
	}
	var payload struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read github file body: %w", err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode github file json: %w", err)
	}
	if payload.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected file encoding %q", payload.Encoding)
	}
	// GitHub wraps the base64 payload at 60-char boundaries; strip newlines.
	clean := strings.ReplaceAll(payload.Content, "\n", "")
	decoded, err := base64DecodeStrict(clean)
	if err != nil {
		return nil, fmt.Errorf("decode base64 file content: %w", err)
	}
	return decoded, nil
}

// HeadCommitInfo carries the minimal metadata the auto-deploy path needs to
// synthesize a git bundle from a tarball: SHA + commit message + author login.
// Per autodeploy.tarballToGitBundle signature (commitSHA + commitMessage +
// author), these three fields are sufficient to reproduce a deterministic
// bundle without an extra commits API round-trip.
type HeadCommitInfo struct {
	SHA           string
	CommitMessage string
	AuthorLogin   string
}

// GetHeadCommit returns the HEAD commit metadata of the given branch.
// Used by the Connect handler (connect-then-deploy 2026-06-09) to trigger
// an auto-deploy of the current HEAD immediately after a repository link,
// matching Vercel-canonical UX (connecting a Git repo deploys it; user is
// not forced to push an extra commit to see the first build).
//
// Calls GET /repos/{owner}/{repo}/branches/{branch} — the same endpoint
// BranchExists uses, but parses out commit.sha + commit.commit.message +
// commit.author.login from the response. Returns a typed BranchNotFoundError
// when the branch is absent so the caller can distinguish "deploy skipped
// because no branch" from a transient HTTP error.
func (c *AppClient) GetHeadCommit(ctx context.Context, installationID int64, owner, repo, branch string) (*HeadCommitInfo, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/branches/%s", c.apiBase(), owner, repo, branch)
	resp, err := c.doAuthenticated(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github head commit fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, &BranchNotFoundError{Owner: owner, Repo: repo, Ref: branch}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github head commit fetch failed (status %d): %s", resp.StatusCode, body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read github head commit body: %w", err)
	}
	var payload struct {
		Commit struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
			} `json:"commit"`
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode github head commit json: %w", err)
	}
	return &HeadCommitInfo{
		SHA:           payload.Commit.SHA,
		CommitMessage: payload.Commit.Commit.Message,
		AuthorLogin:   payload.Commit.Author.Login,
	}, nil
}

// base64DecodeStrict is an indirection so tests can stub the decoder.
var base64DecodeStrict = func(s string) ([]byte, error) {
	return base64Decode(s)
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// --- Installation repo listing (Plan J-09 T2.1) ---

// InstallationRecord is the minimal persistence-side shape of a GitHub
// App installation that the integrations handler needs for tenant
// boundary checks. Kept here (not in the db package) so handlers wire
// DIP without dragging the db import surface.
type InstallationRecord struct {
	ID       int64
	TenantID *string
}

// InstallationRevokedError signals the GitHub App was uninstalled or the
// installation token no longer grants access. The handler maps this to
// HTTP 410 GONE so the dashboard can prompt a reinstall.
type InstallationRevokedError struct {
	InstallationID int64
}

func (e *InstallationRevokedError) Error() string {
	return fmt.Sprintf("github installation %d revoked", e.InstallationID)
}

// InstallationRepository is the sanitised slice of /installation/repositories
// the dashboard renders in the picker. We deliberately drop URLs, hooks, and
// owner.email/owner.sites_admin so a compromised browser cannot exfiltrate
// anything besides the org/repo names the user already typed.
type InstallationRepository struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Language      string `json:"language,omitempty"`
}

// ListInstallationRepositories returns every repo the given installation has
// access to, paginated up to 100 per page. The installation token is
// minted/reused via GetInstallationToken (5min safety margin on the cache).
// 404 from GitHub is normalised to InstallationRevokedError so callers map it
// to HTTP 410 GONE.
func (c *AppClient) ListInstallationRepositories(ctx context.Context, installationID int64, page int) ([]InstallationRepository, error) {
	if page <= 0 {
		page = 1
	}
	url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", c.apiBase(), page)
	resp, err := c.doAuthenticated(ctx, installationID, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github list installation repos: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusNotFound, http.StatusForbidden, http.StatusUnauthorized:
		return nil, &InstallationRevokedError{InstallationID: installationID}
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github list installation repos failed (status %d): %s", resp.StatusCode, body)
	}

	var payload struct {
		Repositories []struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
			Private       bool   `json:"private"`
			Language      string `json:"language"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode github installation repos: %w", err)
	}
	out := make([]InstallationRepository, 0, len(payload.Repositories))
	for _, r := range payload.Repositories {
		out = append(out, InstallationRepository{
			FullName:      r.FullName,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
			Language:      r.Language,
		})
	}
	return out, nil
}
