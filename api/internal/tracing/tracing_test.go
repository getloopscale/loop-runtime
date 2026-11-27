// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestInit_ReturnsShutdownFunction(t *testing.T) {
	// Init with unreachable collector — should not error (graceful degradation)
	shutdown, err := Init(context.Background(), "test-service", Options{Enabled: true, Endpoint: "localhost:4317"})
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
	// C7: tracing disabled returns a no-op
	shutdown, err := Init(context.Background(), "test-service", Options{Enabled: false})
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
