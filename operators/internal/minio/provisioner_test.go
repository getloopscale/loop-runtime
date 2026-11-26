// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package minio

import (
	"testing"
)

func TestGenerateSecretKey_UniqueAndCorrectLength(t *testing.T) {
	key1, err := generateSecretKey()
	if err != nil {
		t.Fatalf("generateSecretKey() error: %v", err)
	}
	if len(key1) != 40 { // 20 bytes = 40 hex chars
		t.Errorf("generateSecretKey() len = %d, want 40", len(key1))
	}

	key2, err := generateSecretKey()
	if err != nil {
		t.Fatalf("generateSecretKey() error: %v", err)
	}
	if key1 == key2 {
		t.Error("generateSecretKey() produced duplicate keys")
	}
}
