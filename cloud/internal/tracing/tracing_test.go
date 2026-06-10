// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tracing

import (
	"context"
	"os"
	"testing"
)

func TestInit_DisabledViaEnv(t *testing.T) {
	t.Setenv("LOOPZERO_TRACING_ENABLED", "false")

	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Init returned error when disabled: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown function should not be nil even when disabled")
	}
	// Shutdown should be a no-op
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}

func TestInit_DisabledViaZero(t *testing.T) {
	t.Setenv("LOOPZERO_TRACING_ENABLED", "0")

	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Init returned error when disabled: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown returned error: %v", err)
	}
}

func TestInit_EnabledByDefault(t *testing.T) {
	// Clear the env var to test default behavior
	_ = os.Unsetenv("LOOPZERO_TRACING_ENABLED")
	// Point to an unreachable endpoint so Init succeeds but export silently fails
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:0")

	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown function should not be nil")
	}
	// Clean up
	if err := shutdown(context.Background()); err != nil {
		t.Logf("shutdown error (expected with unreachable endpoint): %v", err)
	}
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     bool
	}{
		{"empty (default)", "", true},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"1", "1", true},
		{"false", "false", false},
		{"FALSE", "FALSE", false},
		{"False", "False", false},
		{"0", "0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == "" {
				_ = os.Unsetenv("LOOPZERO_TRACING_ENABLED")
			} else {
				t.Setenv("LOOPZERO_TRACING_ENABLED", tt.envValue)
			}
			if got := isEnabled(); got != tt.want {
				t.Errorf("isEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	_ = os.Unsetenv("LOOPZERO_ENV")
	if got := getEnv(); got != "development" {
		t.Errorf("getEnv() = %q, want %q", got, "development")
	}

	t.Setenv("LOOPZERO_ENV", "prod")
	if got := getEnv(); got != "prod" {
		t.Errorf("getEnv() = %q, want %q", got, "prod")
	}
}
