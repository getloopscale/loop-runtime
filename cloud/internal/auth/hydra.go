// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package auth implements OAuth2 flows via Ory Hydra.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HydraClient communicates with Ory Hydra's public and admin APIs.
type HydraClient struct {
	publicURL  string
	adminURL   string
	clientID   string
	httpClient *http.Client
}

// NewHydraClient creates a client for Ory Hydra.
func NewHydraClient(publicURL, adminURL, clientID string) *HydraClient {
	return &HydraClient{
		publicURL: strings.TrimRight(publicURL, "/"),
		adminURL:  strings.TrimRight(adminURL, "/"),
		clientID:  clientID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// DeviceAuthorizeResponse is returned when starting a Device Code Flow.
type DeviceAuthorizeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// TokenResponse is returned on successful token exchange.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	IDToken      string `json:"id_token,omitempty"`
}

// TokenErrorResponse represents an OAuth2 error (e.g., authorization_pending).
type TokenErrorResponse struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

// DeviceAuthorize initiates the Device Code Flow (RFC 8628).
func (c *HydraClient) DeviceAuthorize(scope string) (*DeviceAuthorizeResponse, error) {
	data := url.Values{
		"client_id": {c.clientID},
		"scope":     {scope},
	}

	resp, err := c.httpClient.PostForm(c.publicURL+"/oauth2/device/auth", data)
	if err != nil {
		return nil, fmt.Errorf("device authorize request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorize failed (status %d): %s", resp.StatusCode, body)
	}

	var result DeviceAuthorizeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("invalid device authorize response: %w", err)
	}
	return &result, nil
}

// DeviceToken polls the token endpoint during Device Code Flow.
// Returns (token, nil) on success, (nil, nil) if authorization is pending,
// or (nil, error) on failure.
func (c *HydraClient) DeviceToken(deviceCode string) (*TokenResponse, error) {
	data := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
		"client_id":   {c.clientID},
	}

	resp, err := c.httpClient.PostForm(c.publicURL+"/oauth2/token", data)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		var token TokenResponse
		if err := json.Unmarshal(body, &token); err != nil {
			return nil, fmt.Errorf("invalid token response: %w", err)
		}
		return &token, nil
	}

	// Check for authorization_pending or slow_down (expected during polling)
	var tokenErr TokenErrorResponse
	if err := json.Unmarshal(body, &tokenErr); err == nil {
		if tokenErr.Error == "authorization_pending" || tokenErr.Error == "slow_down" {
			return nil, nil // Still waiting — not an error
		}
		return nil, fmt.Errorf("token error: %s — %s", tokenErr.Error, tokenErr.Description)
	}

	return nil, fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, body)
}

// RefreshToken exchanges a refresh token for a new access token.
func (c *HydraClient) RefreshToken(refreshToken string) (*TokenResponse, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.clientID},
	}

	resp, err := c.httpClient.PostForm(c.publicURL+"/oauth2/token", data)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed (status %d): %s", resp.StatusCode, body)
	}

	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("invalid refresh response: %w", err)
	}
	return &token, nil
}

// LoginRequest represents Hydra's login challenge.
type LoginRequest struct {
	Challenge string `json:"challenge"`
	ClientID  string `json:"client_id"`
	Subject   string `json:"subject"`
	Skip      bool   `json:"skip"`
}

// ConsentRequest represents Hydra's consent challenge.
type ConsentRequest struct {
	Challenge               string   `json:"challenge"`
	ClientID                string   `json:"client_id"`
	Subject                 string   `json:"subject"`
	RequestedScope          []string `json:"requested_scope"`
	RequestedAccessTokenAud []string `json:"requested_access_token_audience"`
}

// GetLoginRequest fetches the login challenge from Hydra admin.
func (c *HydraClient) GetLoginRequest(challenge string) (*LoginRequest, error) {
	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/login?login_challenge=%s", c.adminURL, challenge)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get login request failed: %w", err)
	}
	defer resp.Body.Close()

	var req LoginRequest
	if err := json.NewDecoder(resp.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.Challenge = challenge
	return &req, nil
}

// AcceptLogin accepts the login challenge and returns the redirect URL.
func (c *HydraClient) AcceptLogin(challenge, subject string) (string, error) {
	payload := map[string]interface{}{
		"subject":     subject,
		"remember":    true,
		"remember_for": 3600,
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/login/accept?login_challenge=%s", c.adminURL, challenge)
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("accept login failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.RedirectTo, nil
}

// GetConsentRequest fetches the consent challenge from Hydra admin.
func (c *HydraClient) GetConsentRequest(challenge string) (*ConsentRequest, error) {
	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent?consent_challenge=%s", c.adminURL, challenge)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get consent request failed: %w", err)
	}
	defer resp.Body.Close()

	var req ConsentRequest
	if err := json.NewDecoder(resp.Body).Decode(&req); err != nil {
		return nil, err
	}
	req.Challenge = challenge
	return &req, nil
}

// AcceptConsent accepts the consent challenge, granting requested scopes.
func (c *HydraClient) AcceptConsent(challenge string, grantScope []string) (string, error) {
	payload := map[string]interface{}{
		"grant_scope":                grantScope,
		"remember":                   true,
		"remember_for":               3600,
		"grant_access_token_audience": []string{},
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent/accept?consent_challenge=%s", c.adminURL, challenge)
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("accept consent failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.RedirectTo, nil
}

// RegisterClient registers the CLI OAuth2 client in Hydra (idempotent).
func (c *HydraClient) RegisterClient() error {
	// Check if client already exists
	getURL := fmt.Sprintf("%s/admin/clients/%s", c.adminURL, c.clientID)
	resp, err := c.httpClient.Get(getURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil // Already exists
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Create the client
	payload := map[string]interface{}{
		"client_id":                  c.clientID,
		"client_name":               "LoopZero CLI",
		"grant_types":               []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token", "authorization_code"},
		"response_types":            []string{"code"},
		"scope":                     "openid offline_access",
		"token_endpoint_auth_method": "none", // Public client
		"redirect_uris":             []string{"http://localhost:9876/auth"},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", c.adminURL+"/admin/clients", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register client failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register client failed (status %d): %s", resp.StatusCode, respBody)
	}
	return nil
}
