// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package jwt

import (
	"testing"
	"time"
)

func TestSessionTokenIssuer_IssueAndValidate(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)

	token, expiresIn, err := issuer.Issue("user-123", "test@test.com", "Test User", "tn-acme")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if expiresIn != 3600 {
		t.Errorf("expiresIn = %d, want 3600", expiresIn)
	}

	claims, err := issuer.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("Subject = %q, want user-123", claims.Subject)
	}
	if claims.Email != "test@test.com" {
		t.Errorf("Email = %q, want test@test.com", claims.Email)
	}
	if claims.Name != "Test User" {
		t.Errorf("Name = %q, want Test User", claims.Name)
	}
	if claims.TenantID != "tn-acme" {
		t.Errorf("TenantID = %q, want tn-acme", claims.TenantID)
	}
}

func TestSessionTokenIssuer_WrongSecret(t *testing.T) {
	issuerA := NewSessionTokenIssuer("secret-aaaaaaaaaaaaaaaaaaaaaaaaa", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	issuerB := NewSessionTokenIssuer("secret-bbbbbbbbbbbbbbbbbbbbbbbbb", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)

	token, _, _ := issuerA.Issue("user-1", "a@test.com", "A", "tn")

	_, err := issuerB.ValidateSession(token)
	if err == nil {
		t.Error("token signed with wrong secret should be rejected")
	}
}

func TestSessionTokenIssuer_ExpiredToken(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", -1*time.Second)

	token, _, err := issuer.Issue("user-1", "a@test.com", "A", "tn")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	_, err = issuer.ValidateSession(token)
	if err == nil {
		t.Error("expired token should be rejected")
	}
}

func TestSessionTokenIssuer_WrongIssuer(t *testing.T) {
	issuerA := NewSessionTokenIssuer("same-secret-32-chars-minimum!!!!", "issuer-a", "loopzero-cloud", 1*time.Hour)
	issuerB := NewSessionTokenIssuer("same-secret-32-chars-minimum!!!!", "issuer-b", "loopzero-cloud", 1*time.Hour)

	token, _, _ := issuerA.Issue("user-1", "a@test.com", "A", "tn")

	_, err := issuerB.ValidateSession(token)
	if err == nil {
		t.Error("token with wrong issuer should be rejected")
	}
}

func TestSessionTokenIssuer_ValidatesAudience(t *testing.T) {
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)

	token, _, _ := issuer.Issue("user-1", "a@test.com", "A", "tn")

	claims, err := issuer.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}

	if len(claims.Audience) == 0 || claims.Audience[0] != "loopzero-cloud" {
		t.Errorf("Audience = %v, want [loopzero-cloud]", claims.Audience)
	}
}

// B3 test: cross-audience token MUST be rejected.
// MEET-2026-04-07-039: token issued for "loopzero-cloud" must not be accepted by "loopzero-engine".
func TestSessionTokenIssuer_CrossAudienceRejected(t *testing.T) {
	cloudIssuer := NewSessionTokenIssuer("shared-secret-32-chars-minimum!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	engineValidator := NewSessionTokenIssuer("shared-secret-32-chars-minimum!!", "loopzero-cloud", "loopzero-engine", 1*time.Hour)

	token, _, _ := cloudIssuer.Issue("user-1", "a@test.com", "A", "tn-acme")

	_, err := engineValidator.ValidateSession(token)
	if err == nil {
		t.Error("token with audience 'loopzero-cloud' MUST be rejected by validator expecting 'loopzero-engine'")
	}
}

// B4 test: token with no audience must be rejected.
func TestSessionTokenIssuer_NoAudienceRejected(t *testing.T) {
	// Issue a token with empty audience
	issuer := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "", 1*time.Hour)
	token, _, _ := issuer.Issue("user-1", "a@test.com", "A", "tn")

	// Validate with expected audience — must reject
	validator := NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", "loopzero-cloud", 1*time.Hour)
	_, err := validator.ValidateSession(token)
	if err == nil {
		t.Error("token with empty audience MUST be rejected by validator expecting 'loopzero-cloud'")
	}
}
