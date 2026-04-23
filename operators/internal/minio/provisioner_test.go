// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package minio

import (
	"errors"
	"testing"
)

func TestIsNotFoundError_MatchesKnownMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"generic error", errors.New("connection refused"), false},
		{"does not exist", errors.New("user does not exist"), true},
		{"not found", errors.New("resource not found"), true},
		{"specified user", errors.New("The specified user does not exist"), true},
		{"canned policy", errors.New("The canned policy does not exist"), true},
		{"wrapped does not exist", errors.New("minio: user does not exist: timeout"), true},
		{"partial match no", errors.New("everything is fine"), false},
		{"empty error", errors.New(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotFoundError(tt.err)
			if got != tt.want {
				t.Errorf("isNotFoundError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

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
