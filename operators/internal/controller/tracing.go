// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/loopzero/loopzero/operators/internal/shared"
)

// Thin wrappers delegating to shared package. These keep existing controller
// code unchanged during the multi-binary extraction (ADR D2). When controllers
// move to their own packages (Phase 3), they will import shared directly.

func startReconcileSpan(ctx context.Context, controller, name, namespace string) (context.Context, trace.Span) {
	return shared.StartReconcileSpan(ctx, controller, name, namespace)
}

func startChildSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return shared.StartChildSpan(ctx, operation, attrs...)
}

func recordError(span trace.Span, err error) {
	shared.RecordError(span, err)
}

func setSuccess(span trace.Span) {
	shared.SetSuccess(span)
}

func reconcileOutcome(controller string, start time.Time, err error) {
	shared.ReconcileOutcome(controller, start, err)
}
