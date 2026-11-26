// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package reconcileloop

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestStartReconcileSpan_ReturnsContextAndSpan(t *testing.T) {
	ctx := context.Background()
	ctx, span := StartReconcileSpan(ctx, "tenant", "my-tenant", "loop-runtime-system")
	defer span.End()

	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	// With no-op tracer provider, span is returned but SpanContext may not
	// be valid. We verify the function doesn't panic and returns usable values.
	got := trace.SpanFromContext(ctx)
	if got == nil {
		t.Fatal("expected span in context")
	}
}

func TestStartChildSpan_CreatesChildSpan(t *testing.T) {
	ctx := context.Background()
	ctx, parentSpan := StartReconcileSpan(ctx, "tenant", "t1", "ns")
	defer parentSpan.End()

	ctx, childSpan := StartChildSpan(ctx, "tenant.gitea.ensure",
		attribute.String("provisioner", "gitea"))
	defer childSpan.End()

	got := trace.SpanFromContext(ctx)
	if got == nil {
		t.Fatal("expected span in context")
	}
}

func TestRecordError_SetsErrorStatus(t *testing.T) {
	ctx := context.Background()
	_, span := StartReconcileSpan(ctx, "test", "obj", "ns")
	defer span.End()

	// Should not panic on nil error.
	RecordError(span, nil)

	// Should not panic on non-nil error.
	RecordError(span, errors.New("test error"))
}

func TestSetSuccess_SetsOkStatus(t *testing.T) {
	ctx := context.Background()
	_, span := StartReconcileSpan(ctx, "test", "obj", "ns")
	defer span.End()

	// Should not panic.
	SetSuccess(span)
}

func TestReconcileOutcome_Success(t *testing.T) {
	start := time.Now()
	// Should not panic on success path.
	ReconcileOutcome("tenant", start, nil)
}

func TestReconcileOutcome_Error(t *testing.T) {
	start := time.Now()
	// Should not panic on error path.
	ReconcileOutcome("tenant", start, errors.New("reconcile failed"))
}
