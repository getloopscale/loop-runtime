// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"testing"
)

func TestValidateAdminAPIKey_CurrentKey(t *testing.T) {
	AdminAPIKey = "test-admin-key-current"
	AdminAPIKeyPrevious = ""
	defer func() { AdminAPIKey = ""; AdminAPIKeyPrevious = "" }()

	if !ValidateAdminAPIKey("test-admin-key-current") {
		t.Error("should accept current key")
	}
}

func TestValidateAdminAPIKey_PreviousKey(t *testing.T) {
	AdminAPIKey = "test-admin-key-current"
	AdminAPIKeyPrevious = "test-admin-key-previous"
	defer func() { AdminAPIKey = ""; AdminAPIKeyPrevious = "" }()

	if !ValidateAdminAPIKey("test-admin-key-previous") {
		t.Error("should accept previous key during rotation")
	}
}

func TestValidateAdminAPIKey_InvalidKey(t *testing.T) {
	AdminAPIKey = "test-admin-key-current"
	AdminAPIKeyPrevious = "test-admin-key-previous"
	defer func() { AdminAPIKey = ""; AdminAPIKeyPrevious = "" }()

	if ValidateAdminAPIKey("wrong-key") {
		t.Error("should reject invalid key")
	}
}

func TestValidateAdminAPIKey_EmptyConfig(t *testing.T) {
	AdminAPIKey = ""
	AdminAPIKeyPrevious = ""

	if ValidateAdminAPIKey("any-key") {
		t.Error("should reject when admin key is not configured")
	}
}
