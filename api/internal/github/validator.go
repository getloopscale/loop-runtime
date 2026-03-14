// Package github provides a concrete TokenValidator that validates tokens
// against the GitHub API. It implements middleware.TokenValidator (DIP).
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/loopzero/loopzero/api/internal/middleware"
)

const defaultBaseURL = "https://api.github.com"

// TokenValidator validates GitHub personal access tokens by calling the
// GitHub /user endpoint. It implements middleware.TokenValidator.
type TokenValidator struct {
	HTTPClient *http.Client
	BaseURL    string // defaults to "https://api.github.com" when empty
}

// EffectiveBaseURL returns the base URL used for API calls,
// defaulting to https://api.github.com when BaseURL is empty.
func (v *TokenValidator) EffectiveBaseURL() string {
	if v.BaseURL == "" {
		return defaultBaseURL
	}
	return v.BaseURL
}

// githubUserResponse is the subset of the GitHub /user response we need.
type githubUserResponse struct {
	Login string  `json:"login"`
	Email *string `json:"email"` // pointer because GitHub can return null
	ID    int64   `json:"id"`
}

// ValidateToken calls GET /user on the GitHub API with the provided token.
// On success it returns user info with UserID prefixed as "github-{login}".
// On 401 it returns middleware.ErrInvalidToken.
// On network/other errors it returns a wrapped error (NOT ErrInvalidToken).
func (v *TokenValidator) ValidateToken(ctx context.Context, token string) (*middleware.TokenValidationResult, error) {
	baseURL := v.EffectiveBaseURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/user", nil)
	if err != nil {
		return nil, fmt.Errorf("github: failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := v.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, middleware.ErrInvalidToken
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: unexpected status %d", resp.StatusCode)
	}

	var user githubUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("github: failed to decode response: %w", err)
	}

	email := ""
	if user.Email != nil {
		email = *user.Email
	}

	return &middleware.TokenValidationResult{
		UserID:   "github-" + user.Login,
		Email:    email,
		GitHubID: user.ID,
	}, nil
}
