// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopzero/loopzero/api/internal/github"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

func TestGitHubTokenValidator(t *testing.T) {
	t.Run("valid token returns user info with github- prefix on UserID", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/user" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.Header.Get("Authorization") != "Bearer ghp_valid_token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"login": "lucas",
				"email": "lucas@acme.dev",
				"id":    float64(12345),
			})
		}))
		defer server.Close()

		validator := &github.TokenValidator{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
		}

		// Act
		result, err := validator.ValidateToken(context.Background(), "ghp_valid_token")

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.UserID != "github-lucas" {
			t.Errorf("expected UserID=github-lucas, got %q", result.UserID)
		}
		if result.Email != "lucas@acme.dev" {
			t.Errorf("expected Email=lucas@acme.dev, got %q", result.Email)
		}
		if result.GitHubID != 12345 {
			t.Errorf("expected GitHubID=12345, got %d", result.GitHubID)
		}
	})

	t.Run("invalid token returns ErrInvalidToken", func(t *testing.T) {
		// Arrange
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "Bad credentials"})
		}))
		defer server.Close()

		validator := &github.TokenValidator{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
		}

		// Act
		_, err := validator.ValidateToken(context.Background(), "ghp_expired")

		// Assert
		if !errors.Is(err, middleware.ErrInvalidToken) {
			t.Fatalf("expected ErrInvalidToken, got %v", err)
		}
	})

	t.Run("network error returns error that is NOT ErrInvalidToken", func(t *testing.T) {
		// Arrange — point to a closed server to simulate network failure
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		serverURL := server.URL
		server.Close() // close immediately to cause connection refused

		validator := &github.TokenValidator{
			HTTPClient: http.DefaultClient,
			BaseURL:    serverURL,
		}

		// Act
		_, err := validator.ValidateToken(context.Background(), "ghp_any")

		// Assert
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if errors.Is(err, middleware.ErrInvalidToken) {
			t.Fatal("network error should NOT be ErrInvalidToken")
		}
	})

	t.Run("nil email in GitHub response returns empty email", func(t *testing.T) {
		// Arrange — GitHub returns null email for users with private email
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"login": "private-user",
				"email": nil,
				"id":    float64(99999),
			})
		}))
		defer server.Close()

		validator := &github.TokenValidator{
			HTTPClient: server.Client(),
			BaseURL:    server.URL,
		}

		// Act
		result, err := validator.ValidateToken(context.Background(), "ghp_valid")

		// Assert
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result.UserID != "github-private-user" {
			t.Errorf("expected UserID=github-private-user, got %q", result.UserID)
		}
		if result.Email != "" {
			t.Errorf("expected empty email, got %q", result.Email)
		}
	})

	t.Run("default BaseURL is https://api.github.com when empty", func(t *testing.T) {
		validator := &github.TokenValidator{
			HTTPClient: &http.Client{},
		}
		if validator.EffectiveBaseURL() != "https://api.github.com" {
			t.Errorf("expected default base URL, got %q", validator.EffectiveBaseURL())
		}
	})
}
