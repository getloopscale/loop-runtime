// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"testing"

	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"
)

// mockPATValidator simulates the existing GitHub PAT validator.
type mockPATValidator struct {
	result *TokenValidationResult
	err    error
}

func (v *mockPATValidator) ValidateToken(_ context.Context, _ string) (*TokenValidationResult, error) {
	return v.result, v.err
}

func TestDualValidator_PAT_RoutesToPATValidator(t *testing.T) {
	patResult := &TokenValidationResult{
		UserID:   "github-octocat",
		Email:    "octocat@github.com",
		GitHubID: 123456,
	}
	pat := &mockPATValidator{result: patResult}
	dual := NewDualValidator(nil, nil, pat) // No JWT validator

	result, err := dual.ValidateToken(context.Background(), "ghp_abc123def456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserID != "github-octocat" {
		t.Errorf("UserID = %q, want github-octocat", result.UserID)
	}
}

func TestDualValidator_PATPrefix_RoutesCorrectly(t *testing.T) {
	pat := &mockPATValidator{
		result: &TokenValidationResult{UserID: "pat-user", Email: "pat@test.com"},
	}
	// JWT validator exists but PAT token should route to PAT validator
	jwtVal := NewJWTValidator("http://invalid:4444/.well-known/jwks.json", "http://invalid:4444/")
	dual := NewDualValidator(jwtVal, nil, pat)

	for _, prefix := range []string{"ghp_", "gho_", "github_pat_"} {
		token := prefix + "testtoken123"
		result, err := dual.ValidateToken(context.Background(), token)
		if err != nil {
			t.Errorf("PAT with prefix %q should succeed: %v", prefix, err)
			continue
		}
		if result.UserID != "pat-user" {
			t.Errorf("PAT with prefix %q routed to wrong validator", prefix)
		}
	}
}

func TestDualValidator_NilJWT_FallsToPAT(t *testing.T) {
	pat := &mockPATValidator{
		result: &TokenValidationResult{UserID: "fallback-user"},
	}
	dual := NewDualValidator(nil, nil, pat)

	// Any token format should fall to PAT when JWT validator is nil
	result, err := dual.ValidateToken(context.Background(), "any-token-format")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserID != "fallback-user" {
		t.Errorf("should fall back to PAT validator")
	}
}

func TestTokenFormatDetection(t *testing.T) {
	// GitHub PATs
	if !loopzerojwt.IsGitHubPAT("ghp_abc123") {
		t.Error("ghp_ should be detected as GitHub PAT")
	}
	if !loopzerojwt.IsGitHubPAT("gho_abc123") {
		t.Error("gho_ should be detected as GitHub PAT")
	}

	// JWTs
	if !loopzerojwt.IsJWTToken("eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1c2VyMSJ9.signature") {
		t.Error("3-segment token should be detected as JWT")
	}

	// Neither
	if loopzerojwt.IsGitHubPAT("random-token") {
		t.Error("random token should not be detected as GitHub PAT")
	}
	if loopzerojwt.IsJWTToken("random-token") {
		t.Error("random token should not be detected as JWT")
	}
}
