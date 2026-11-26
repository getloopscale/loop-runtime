// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultGitHubWebBase is the github.com web root, where the App's user
// authorization lives. A GitHub Enterprise Server install points
// GITHUB_WEB_BASE_URL at its appliance.
const DefaultGitHubWebBase = "https://github.com"

// ErrOAuthCodeRejected is returned when GitHub refuses the authorization code:
// it is wrong, expired, or was exchanged already. The user authorizes again.
var ErrOAuthCodeRejected = errors.New("github rejected the authorization code")

// UserInstallation is an installation of this App that a GitHub user can
// access.
type UserInstallation struct {
	ID           int64
	AccountLogin string
	AccountType  string
}

// UserAccessConfig is what the App's user authorization needs.
type UserAccessConfig struct {
	// AppID is this App's id. Installations of any other App are dropped.
	AppID int64
	// ClientID and ClientSecret are the App's OAuth client credentials.
	ClientID     string
	ClientSecret string
	// WebBaseURL is the GitHub web root; empty means DefaultGitHubWebBase.
	WebBaseURL string
	// APIBaseURL is the GitHub REST root; empty means DefaultGitHubAPIBase.
	APIBaseURL string
	// HTTPClient is optional.
	HTTPClient *http.Client
}

// UserAccessClient asks GitHub which installations of this App a user can
// access, with the user-to-server token the App's user authorization issues.
// The token lives for one call: it is never stored, logged or returned.
type UserAccessClient struct {
	appID        int64
	clientID     string
	clientSecret string
	webBase      string
	apiBase      string
	httpClient   *http.Client
	pageSize     int
}

// userInstallationsPageSize is GitHub's largest page.
const userInstallationsPageSize = 100

// maxUserInstallationPages bounds the pagination of one authorization.
const maxUserInstallationPages = 50

// NewUserAccessClient builds the client. Every field of the App's identity is
// required: without the app id the filter by App would keep nothing.
func NewUserAccessClient(cfg UserAccessConfig) (*UserAccessClient, error) {
	if cfg.AppID == 0 || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("github user access: app id, client id and client secret are required")
	}
	webBase, apiBase := cfg.WebBaseURL, cfg.APIBaseURL
	if webBase == "" {
		webBase = DefaultGitHubWebBase
	}
	if apiBase == "" {
		apiBase = DefaultGitHubAPIBase
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &UserAccessClient{
		appID:        cfg.AppID,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		webBase:      strings.TrimRight(webBase, "/"),
		apiBase:      strings.TrimRight(apiBase, "/"),
		httpClient:   httpClient,
		pageSize:     userInstallationsPageSize,
	}, nil
}

// InstallationsForCode exchanges the one-time code of a user authorization and
// returns the installations of this App that the user can access. It returns
// ErrOAuthCodeRejected when GitHub refuses the code, and another error when
// GitHub could not be asked; in both cases it returns no installation.
func (c *UserAccessClient) InstallationsForCode(ctx context.Context, code string) ([]UserInstallation, error) {
	token, err := c.exchangeCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return c.listInstallations(ctx, token)
}

// exchangeCode trades the code for a user-to-server token. GitHub answers 200
// for a refused code and names the reason in the body.
func (c *UserAccessClient) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{"client_id": {c.clientID}, "client_secret": {c.clientSecret}, "code": {code}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("github user access: build the code exchange: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github user access: code exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github user access: code exchange answered status %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		// The body may hold the token: the decode error is not wrapped.
		return "", errors.New("github user access: code exchange answer is not JSON")
	}
	switch {
	case payload.Error == "bad_verification_code":
		return "", ErrOAuthCodeRejected
	case payload.Error != "":
		return "", fmt.Errorf("github user access: code exchange refused: %s", payload.Error)
	case payload.AccessToken == "":
		return "", errors.New("github user access: code exchange answered no token")
	}
	return payload.AccessToken, nil
}

// listInstallations reads every page of GET /user/installations and keeps the
// installations of this App.
func (c *UserAccessClient) listInstallations(ctx context.Context, token string) ([]UserInstallation, error) {
	var installations []UserInstallation
	seen := 0
	for page := 1; page <= maxUserInstallationPages; page++ {
		batch, total, err := c.installationsPage(ctx, token, page)
		if err != nil {
			return nil, err
		}
		for _, inst := range batch {
			if inst.AppID != c.appID || inst.ID <= 0 {
				continue
			}
			installations = append(installations, UserInstallation{
				ID: inst.ID, AccountLogin: inst.Account.Login, AccountType: inst.Account.Type,
			})
		}
		seen += len(batch)
		if len(batch) < c.pageSize || seen >= total {
			return installations, nil
		}
	}
	return nil, fmt.Errorf("github user access: more than %d pages of installations", maxUserInstallationPages)
}

type userInstallationPayload struct {
	ID      int64 `json:"id"`
	AppID   int64 `json:"app_id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
}

func (c *UserAccessClient) installationsPage(ctx context.Context, token string, page int) ([]userInstallationPayload, int, error) {
	endpoint := fmt.Sprintf("%s/user/installations?per_page=%d&page=%d", c.apiBase, c.pageSize, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("github user access: build the installations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("github user access: list installations: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("github user access: list installations answered status %d", resp.StatusCode)
	}
	var payload struct {
		TotalCount    int                       `json:"total_count"`
		Installations []userInstallationPayload `json:"installations"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, 0, fmt.Errorf("github user access: decode installations: %w", err)
	}
	return payload.Installations, payload.TotalCount, nil
}
