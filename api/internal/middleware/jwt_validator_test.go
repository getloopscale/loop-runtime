// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"testing"
	"time"

	loopzerojwt "github.com/loopzero/loopzero/pkg/jwt"
)

func TestJWTOnlyValidator_NilValidators_ReturnsError(t *testing.T) {
	v := NewJWTOnlyValidator(nil, nil)
	_, err := v.ValidateToken(context.Background(), "some-token")
	if err == nil {
		t.Error("expected error with nil validators")
	}
}

func TestJWTOnlyValidator_HMACValidToken(t *testing.T) {
	secret := "test-secret-32-chars-minimum!!!!"
	issuer := loopzerojwt.NewSessionTokenIssuer(secret, "loopzero-cloud", 1*time.Hour)
	v := NewJWTOnlyValidator(nil, issuer)

	// Issue a valid token
	token, _, err := issuer.Issue("user-123", "test@test.com", "Test User", "tn-test")
	if err != nil {
		t.Fatalf("failed to issue token: %v", err)
	}

	result, err := v.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("valid HMAC token rejected: %v", err)
	}
	if result.UserID != "user-123" {
		t.Errorf("UserID = %q, want user-123", result.UserID)
	}
	if result.Email != "test@test.com" {
		t.Errorf("Email = %q, want test@test.com", result.Email)
	}
}

func TestJWTOnlyValidator_InvalidToken(t *testing.T) {
	issuer := loopzerojwt.NewSessionTokenIssuer("test-secret-32-chars-minimum!!!!", "loopzero-cloud", 1*time.Hour)
	v := NewJWTOnlyValidator(nil, issuer)

	_, err := v.ValidateToken(context.Background(), "not-a-jwt")
	if err == nil {
		t.Error("invalid token should return error")
	}
}

func TestJWTOnlyValidator_WrongSecret(t *testing.T) {
	// Issue with one secret, validate with another
	issuerA := loopzerojwt.NewSessionTokenIssuer("secret-aaaaaaaaaaaaaaaaaaaaaaaaa", "loopzero-cloud", 1*time.Hour)
	issuerB := loopzerojwt.NewSessionTokenIssuer("secret-bbbbbbbbbbbbbbbbbbbbbbbbb", "loopzero-cloud", 1*time.Hour)

	token, _, _ := issuerA.Issue("user-1", "a@test.com", "A", "tn")
	v := NewJWTOnlyValidator(nil, issuerB)

	_, err := v.ValidateToken(context.Background(), token)
	if err == nil {
		t.Error("token signed with wrong secret should be rejected")
	}
}
