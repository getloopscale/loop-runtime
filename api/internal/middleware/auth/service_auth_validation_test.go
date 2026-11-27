// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Split out of the flat `middleware` package's middleware_test.go (B-059): a
// test file lives in the package of its subject, and that file tested six
// middlewares that now live in five packages.

package auth

import (
	"strings"
	"testing"
)

// --- Service auth validation tests ---

func TestValidateServiceAuth_DualKeyRotation(t *testing.T) {
	authn := NewAuthenticator(Config{ServiceSecret: "new-secret", ServiceSecretPrevious: "old-secret"})
	if !authn.validateServiceAuth("new-secret") {
		t.Error("current secret should be accepted")
	}
	if !authn.validateServiceAuth("old-secret") {
		t.Error("previous secret should be accepted during rotation")
	}
	if authn.validateServiceAuth("wrong-secret") {
		t.Error("invalid secret should be rejected")
	}
}

func TestValidateServiceAuth_EmptyConfig(t *testing.T) {
	authn := NewAuthenticator(Config{ServiceSecret: ""})
	if authn.validateServiceAuth("any-secret") {
		t.Error("should reject when service auth is not configured")
	}
}

// --- Token cache key security ---

func TestTokenCacheKey_DeterministicHash(t *testing.T) {
	key1 := tokenCacheKey("my-secret-token")
	key2 := tokenCacheKey("my-secret-token")
	if key1 != key2 {
		t.Error("same token should produce same cache key")
	}

	key3 := tokenCacheKey("different-token")
	if key1 == key3 {
		t.Error("different tokens should produce different cache keys")
	}

	// Should not contain the original token (SEC-001)
	if strings.Contains(key1, "my-secret-token") {
		t.Error("cache key should be a hash, not contain the raw token")
	}
}
