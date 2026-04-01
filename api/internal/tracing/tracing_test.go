// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInit_ReturnsShutdownFunction(t *testing.T) {
	// Init with unreachable collector — should not error (graceful degradation)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	t.Setenv("LOOPZERO_TRACING_ENABLED", "true")

	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init() returned nil shutdown function")
	}

	// Verify TracerProvider was registered
	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Errorf("expected *sdktrace.TracerProvider, got %T", tp)
	}

	// Shutdown should complete without error
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() returned error: %v", err)
	}
}

func TestInit_DisabledViaFeatureFlag(t *testing.T) {
	// C7: LOOPZERO_TRACING_ENABLED=false returns no-op
	t.Setenv("LOOPZERO_TRACING_ENABLED", "false")

	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init() returned nil shutdown function")
	}

	// No-op shutdown should work
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown() returned error: %v", err)
	}
}

func TestInit_DisabledWithZero(t *testing.T) {
	t.Setenv("LOOPZERO_TRACING_ENABLED", "0")

	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Init() returned error: %v", err)
	}
	// Should succeed as no-op
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() returned error: %v", err)
	}
}

func TestIsEnabled_Defaults(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"empty defaults to true", "", true},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"false", "false", false},
		{"FALSE", "FALSE", false},
		{"0", "0", false},
		{"1", "1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LOOPZERO_TRACING_ENABLED", tt.env)
			if got := isEnabled(); got != tt.want {
				t.Errorf("isEnabled() = %v, want %v (env=%q)", got, tt.want, tt.env)
			}
		})
	}
}
