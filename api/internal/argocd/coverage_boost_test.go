// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"testing"
)

// =============================================================================
// safePrefix tests (previously 66.7% coverage)
// =============================================================================

func TestSafePrefix_ShortToken(t *testing.T) {
	result := safePrefix("abc")
	if result != "***" {
		t.Errorf("expected '***' for short token, got %q", result)
	}
}

func TestSafePrefix_ExactlyTenChars(t *testing.T) {
	result := safePrefix("1234567890")
	if result != "***" {
		t.Errorf("expected '***' for 10-char token, got %q", result)
	}
}

func TestSafePrefix_LongToken(t *testing.T) {
	result := safePrefix("1234567890ABCDEF")
	if result != "1234567890..." {
		t.Errorf("expected '1234567890...', got %q", result)
	}
}

func TestSafePrefix_Empty(t *testing.T) {
	result := safePrefix("")
	if result != "***" {
		t.Errorf("expected '***' for empty token, got %q", result)
	}
}

// =============================================================================
// NewSyncClient tests (previously 0% coverage)
// =============================================================================

func TestNewSyncClient_Basic(t *testing.T) {
	c := NewSyncClient(SyncClientConfig{
		BaseURL: "http://argocd:80",
		Token:   "test-token",
	})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.baseURL != "http://argocd:80" {
		t.Errorf("baseURL = %q, want http://argocd:80", c.baseURL)
	}
	if c.client == nil {
		t.Fatal("expected non-nil HTTP client")
	}
}

func TestNewSyncClient_WithTokenProvider(t *testing.T) {
	tp := NewStaticTokenProvider("my-token")
	c := NewSyncClient(SyncClientConfig{
		BaseURL:       "https://argocd:443",
		TokenProvider: tp,
	})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.tokenProvider != tp {
		t.Error("expected token provider to be set")
	}
}

func TestNewSyncClient_WithCACertPath_Missing(t *testing.T) {
	// Non-existent CA cert should fall back to insecure
	c := NewSyncClient(SyncClientConfig{
		BaseURL:    "https://argocd:443",
		Token:      "test",
		CACertPath: "/nonexistent/ca.crt",
	})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
