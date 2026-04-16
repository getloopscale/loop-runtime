// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package github provides clients for the GitHub API.
// AppClient authenticates as a GitHub App using JWT + installation tokens.
// Used for: source download, commit status updates, PR comments.
package github

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
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
	githubAPIBase = "https://api.github.com"
)

// AppClient authenticates as a GitHub App and manages installation tokens.
// Thread-safe: tokens are cached and refreshed automatically.
type AppClient struct {
	appID      int64
	privateKey *rsa.PrivateKey
	httpClient *http.Client

	mu         sync.RWMutex
	tokenCache map[int64]*cachedToken
}

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// NewAppClient creates a GitHub App client from App ID and PEM-encoded private key.
// Returns nil if credentials are missing (acceptable in local dev).
func NewAppClient(appID int64, privateKeyPEM string) (*AppClient, error) {
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
		tokenCache: make(map[int64]*cachedToken),
	}, nil
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
		fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBase, installationID), nil)
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
	url := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", githubAPIBase, owner, repo, sha)
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
	TargetURL   string // URL to LoopZero deploy/logs
	Description string // Short description (max 140 chars)
	Context     string // "loopzero/deploy" or "loopzero/preview"
}

// SetCommitStatus updates the commit status on GitHub.
func (c *AppClient) SetCommitStatus(ctx context.Context, installationID int64, owner, repo, sha string, status CommitStatus) error {
	url := fmt.Sprintf("%s/repos/%s/%s/statuses/%s", githubAPIBase, owner, repo, sha)

	payload := fmt.Sprintf(`{"state":%q,"target_url":%q,"description":%q,"context":%q}`,
		status.State, status.TargetURL, status.Description, status.Context)

	resp, err := c.doAuthenticated(ctx, installationID, "POST", url, stringReader(payload))
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

// --- PR Comments ---

// CommentOnPR posts a comment on a pull request.
func (c *AppClient) CommentOnPR(ctx context.Context, installationID int64, owner, repo string, prNumber int, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", githubAPIBase, owner, repo, prNumber)

	payload := fmt.Sprintf(`{"body":%q}`, body)

	resp, err := c.doAuthenticated(ctx, installationID, "POST", url, stringReader(payload))
	if err != nil {
		return fmt.Errorf("github: comment on PR: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github: comment on PR (status %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// --- Helpers ---

func stringReader(s string) io.Reader {
	return strings.NewReader(s)
}
