// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package reconcileloop guarda o que acontece DENTRO de um laço de reconcile: o sumário
// de prontidão, os predicados de evento, os finalizadores e a observabilidade das
// próprias reconciliações.
//
// # O nome, e o que ele substituiu (B-061)
//
// Chamava-se `reconcilehelpers` e pontuou coesão 2/5 — o único pacote da família
// "helpers" em toda a árvore Go, num limiar respeitado por 148 módulos e 730k LOC.
// O doc dele ainda o chamava de `shared`, um nome que ele já não tinha: `go doc`
// renderizava um pacote inexistente.
//
// Saíram daqui a política de tenant (`tenantpolicy`), o roteamento de cell
// (`cellrouting`) e o arranque de binário (`operatorboot`). O que ficou tem uma razão
// só de mudar, e o nome diz qual.
package reconcileloop

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/getloopscale/loopzero/operators/internal/metrics"
)

var tracer = otel.Tracer("loopzero-operator")

// StartReconcileSpan creates the root reconcile span with standard attributes.
func StartReconcileSpan(ctx context.Context, controller, name, namespace string) (context.Context, trace.Span) {
	ctx, span := tracer.Start(ctx, "loopzero.operator."+controller+".reconcile",
		trace.WithAttributes(
			attribute.String("k8s.object.name", name),
			attribute.String("k8s.namespace", namespace),
			attribute.String("controller", controller),
		),
	)
	return ctx, span
}

// StartChildSpan creates a child span for a sub-operation (provisioner call, etc).
func StartChildSpan(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, operation, trace.WithAttributes(attrs...))
}

// RecordError records an error on the span and sets status to Error.
func RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// SetSuccess sets the span status to OK.
func SetSuccess(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}

// ReconcileOutcome tracks metrics for a reconcile invocation.
// Call with defer at the start of each Reconcile method:
//
//	start := time.Now()
//	defer func() { reconcileloop.ReconcileOutcome(controllerName, start, retErr) }()
func ReconcileOutcome(controller string, start time.Time, err error) {
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
func errorType(err error) string {
	if err == nil {
		return ""
	}
	return "reconcile_error"
}
