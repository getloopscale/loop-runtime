// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/loopzero/loopzero/operators/internal/metrics"
)

var tracer = otel.Tracer("loopzero-operator")

// startReconcileSpan creates the root reconcile span with standard attributes.
func startReconcileSpan(ctx context.Context, controller, name, namespace string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "loopzero.operator."+controller+".reconcile",
		trace.WithAttributes(
			attribute.String("k8s.object.name", name),
			attribute.String("k8s.namespace", namespace),
			attribute.String("controller", controller),
		),
	)
	return ctx, span
}

// startChildSpan creates a child span for a sub-operation (provisioner call, etc).
func startChildSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, operation, trace.WithAttributes(attrs...))
}

// recordError records an error on the span and sets status to Error.
func recordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// setSuccess sets the span status to OK.
func setSuccess(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}

// reconcileOutcome tracks metrics for a reconcile invocation.
// Call with defer at the start of each Reconcile method:
//
//	start := time.Now()
//	defer func() { reconcileOutcome(controllerName, start, retErr) }()
func reconcileOutcome(controller string, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	result := "success"
	if err != nil {
		result = "error"
		metrics.ReconcileErrors.WithLabelValues(controller, errorType(err)).Inc()
	}
	metrics.ReconcileDuration.WithLabelValues(controller, result).Observe(duration)
	metrics.ReconcileTotal.WithLabelValues(controller, result).Inc()
}

// errorType extracts a short error type string for metrics labels.
// Avoids high-cardinality labels by using the error's type rather than the full message.
func errorType(err error) string {
	if err == nil {
		return ""
	}
	// Use a generic label. In the future, typed errors from pkg/errors could be inspected.
	return "reconcile_error"
}
