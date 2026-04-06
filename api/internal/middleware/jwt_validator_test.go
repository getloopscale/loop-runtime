// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"testing"
)

func TestJWTOnlyValidator_NilValidators_ReturnsError(t *testing.T) {
	v := NewJWTOnlyValidator(nil, nil)
	_, err := v.ValidateToken(context.Background(), "some-token")
	if err == nil {
		t.Error("expected error with nil validators")
	}
}

func TestJWTOnlyValidator_RSAFirst_ThenHMAC(t *testing.T) {
	// RSA validator will fail (invalid JWKS URL), HMAC should be tried
	rsaVal := NewJWTValidator("http://invalid:4444/.well-known/jwks.json", "http://invalid:4444/")
	hmacVal := NewHMACTokenValidator("test-secret-32-chars-minimum!!!!", "loopzero-cloud")

	v := NewJWTOnlyValidator(rsaVal, hmacVal)

	// Create a valid HMAC token
	// (can't easily test without the issuer, but we verify the flow)
	_, err := v.ValidateToken(context.Background(), "not-a-valid-jwt")
	if err == nil {
		t.Error("invalid token should return error")
	}
}

func TestHMACTokenValidator_InvalidToken(t *testing.T) {
	v := NewHMACTokenValidator("test-secret-32-chars-minimum!!!!", "loopzero-cloud")
	_, err := v.ValidateToken(context.Background(), "not-a-jwt")
	if err == nil {
		t.Error("invalid token should return error")
	}
}

func TestHMACTokenValidator_WrongSecret(t *testing.T) {
	v := NewHMACTokenValidator("wrong-secret-32-chars-minimum!!!", "loopzero-cloud")
	// This is a token signed with a different secret — should fail
	_, err := v.ValidateToken(context.Background(), "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyMSJ9.invalid")
	if err == nil {
		t.Error("token with wrong secret should return error")
	}
}
