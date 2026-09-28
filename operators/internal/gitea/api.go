// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// APIClient is a typed HTTP client for the Gitea REST API.
// All URL construction is centralized here — no fmt.Sprintf of API paths
// should appear in controller or provisioner code.
type APIClient struct {
	baseURL string
	client  *http.Client
}

// NewAPIClient creates a Gitea API client.
func NewAPIClient(baseURL string, httpClient *http.Client) *APIClient {
	return &APIClient{baseURL: strings.TrimRight(baseURL, "/"), client: httpClient}
}

// --- URL builders (private, testable via the public methods) ---

func (c *APIClient) url(pathSegments ...string) string {
	return c.baseURL + "/" + strings.Join(pathSegments, "/")
}

func (c *APIClient) apiV1(pathSegments ...string) string {
	return c.url(append([]string{"api", "v1"}, pathSegments...)...)
}

// --- Version / Health ---

// CheckVersion calls GET /api/v1/version to verify Gitea is reachable.
func (c *APIClient) CheckVersion(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiV1("version"), nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Gitea: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain body for connection reuse.
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from Gitea version endpoint", resp.StatusCode)
	}
	return nil
}

// --- Token Management ---

// TokenInfo represents a Gitea API token list entry.
type TokenInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ValidateToken checks if a token is valid by calling GET /api/v1/user.
func (c *APIClient) ValidateToken(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiV1("user"), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain body for connection reuse.
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token validation failed with status %d", resp.StatusCode)
	}
	return nil
}

// ListTokens returns all API tokens for a user (requires basic auth).
func (c *APIClient) ListTokens(ctx context.Context, username, password string) ([]TokenInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiV1("users", username, "tokens"), nil)
	if err != nil {
		return nil, fmt.Errorf("creating list tokens request: %w", err)
	}
	req.SetBasicAuth(username, password)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing tokens: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list tokens returned status %d", resp.StatusCode)
	}

	var tokens []TokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("decoding tokens: %w", err)
	}
	return tokens, nil
}

// DeleteToken deletes a token by ID (requires basic auth).
func (c *APIClient) DeleteToken(ctx context.Context, username, password string, tokenID int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.apiV1("users", username, "tokens", fmt.Sprintf("%d", tokenID)), nil)
	if err != nil {
		return fmt.Errorf("creating delete token request: %w", err)
	}
	req.SetBasicAuth(username, password)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain body for connection reuse.
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete token %d failed with status %d", tokenID, resp.StatusCode)
	}
	return nil
}

// CreateTokenResponse contains the response from creating a token.
type CreateTokenResponse struct {
	SHA1 string `json:"sha1"`
}

// CreateToken creates a new API token (requires basic auth).
// Returns the token SHA1 value.
func (c *APIClient) CreateToken(ctx context.Context, username, password, tokenName string) (string, error) {
	bodyBytes, marshalErr := json.Marshal(map[string]string{"name": tokenName})
	if marshalErr != nil {
		return "", fmt.Errorf("gitea: marshal token request: %w", marshalErr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiV1("users", username, "tokens"), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("creating token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token creation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result CreateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}
	return result.SHA1, nil
}

// --- Organization Management ---

// EnsureOrg creates a Gitea organization if it doesn't exist (idempotent).
func (c *APIClient) EnsureOrg(ctx context.Context, token, orgName string) error {
	// Check if org exists
	checkReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiV1("orgs", orgName), nil)
	if err != nil {
		return fmt.Errorf("creating org check request: %w", err)
	}
	checkReq.Header.Set("Authorization", "token "+token)
	checkResp, err := c.client.Do(checkReq)
	if err != nil {
		return fmt.Errorf("checking org %s: %w", orgName, err)
	}
	defer func() { _ = checkResp.Body.Close() }()
	// Drain body for connection reuse.
	_, _ = io.ReadAll(checkResp.Body)

	if checkResp.StatusCode == http.StatusOK {
		return nil // Org exists
	}

	// Create org
	bodyBytes, marshalErr := json.Marshal(map[string]string{"username": orgName, "visibility": "private"})
	if marshalErr != nil {
		return fmt.Errorf("gitea: marshal org request: %w", marshalErr)
	}
	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiV1("orgs"), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("creating org request: %w", err)
	}
	createReq.Header.Set("Authorization", "token "+token)
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := c.client.Do(createReq)
	if err != nil {
		return fmt.Errorf("creating org %s: %w", orgName, err)
	}
	defer func() { _ = createResp.Body.Close() }()
	// Drain body for connection reuse.
	_, _ = io.ReadAll(createResp.Body)

	if createResp.StatusCode == http.StatusCreated {
		return nil
	}
	// COMP-005: On 422, verify the org actually exists rather than assuming it does.
	// A 422 could indicate other validation errors besides "already exists".
	if createResp.StatusCode == http.StatusUnprocessableEntity {
		verifyReq, verifyErr := http.NewRequestWithContext(ctx, http.MethodGet, c.apiV1("orgs", orgName), nil)
		if verifyErr != nil {
			return fmt.Errorf("gitea: org %s creation returned 422, verification request failed: %w", orgName, verifyErr)
		}
		verifyReq.Header.Set("Authorization", "token "+token)
		verifyResp, verifyErr := c.client.Do(verifyReq)
		if verifyErr != nil {
			return fmt.Errorf("gitea: org %s creation returned 422, verification call failed: %w", orgName, verifyErr)
		}
		defer func() { _ = verifyResp.Body.Close() }()
		_, _ = io.ReadAll(verifyResp.Body)
		if verifyResp.StatusCode == http.StatusOK {
			return nil // org confirmed to exist
		}
		return fmt.Errorf("org creation returned 422 but org %s does not exist (verify returned %d)", orgName, verifyResp.StatusCode)
	}
	return fmt.Errorf("unexpected status %d when creating org %s", createResp.StatusCode, orgName)
}

// RepoAPIURL converts a Git repo URL like http://host/owner/repo.git to
// the Gitea API URL: http://host/api/v1/repos/owner/repo.
// This is used for reachability checks on GitRepositoryBinding resources.
func RepoAPIURL(repoURL string) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("repo URL %q: %w", repoURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("repo URL %q: missing scheme or host", repoURL)
	}

	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("repo URL %q: expected path format owner/repo", repoURL)
	}

	u.Path = fmt.Sprintf("/api/v1/repos/%s/%s", parts[0], parts[1])
	return u.String(), nil
}
