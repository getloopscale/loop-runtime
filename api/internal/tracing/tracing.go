// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package tracing provides OpenTelemetry distributed tracing bootstrap.
// MEET-2026-03-30-020: APPROVED_WITH_CONDITIONS
//
// Conditions applied:
//   - C5: export timeout 1s, retry disabled, bounded queue 2048
//   - C7: feature flag LOOPZERO_TRACING_ENABLED — default TRUE everywhere
//     (dev + prod). Local Tilt YAML + bootstrap chart explicitly set
//     false; production default-tracing-enabled is gated downstream by
//     the OTel collector / Tempo (find_obs_007 alignment).
//   - C2: trace-id is separate from correlation-id (no format change)
package tracing

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.32.0"
)

const (
	// DefaultEndpoint is the in-cluster OTel Collector address.
	DefaultEndpoint = "otel-collector.loopzero-system.svc.cluster.local:4317"

	// exportTimeout is the maximum time for an export attempt (C5).
	// Default OTel SDK is 10s — too long, risks latency under failure.
	exportTimeout = 1 * time.Second

	// maxQueueSize bounds the span export queue (C5).
	// Prevents unbounded memory growth when Collector is unreachable.
	maxQueueSize = 2048
)

// Init configures the OTel TracerProvider with OTLP gRPC exporter.
// Returns a shutdown function for graceful shutdown.
//
// If LOOPZERO_TRACING_ENABLED is "false" (C7), returns a no-op shutdown
// with zero overhead — no TracerProvider is registered.
//
// If the Collector is unreachable, the API continues normally.
// Spans are dropped after queue exhaustion (graceful degradation).
func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
	// C7: Feature flag — default true in dev, false in prod.
	if !isEnabled() {
		slog.Info("tracing disabled via LOOPZERO_TRACING_ENABLED=false")
		return func(context.Context) error { return nil }, nil
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}

	// C5: Export timeout 1s, retry disabled.
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // In-cluster, no TLS
		otlptracegrpc.WithTimeout(exportTimeout),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
			Enabled: false, // C5: No retry on boot — fail fast
		}),
	)
	if err != nil {
		return func(context.Context) error { return nil }, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.DeploymentEnvironmentName(getEnv()),
		),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
	)
	if err != nil {
		return func(context.Context) error { return nil }, err
	}

	// C5: Bounded queue prevents memory exhaustion when Collector is down.
	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithMaxQueueSize(maxQueueSize),
		sdktrace.WithExportTimeout(exportTimeout),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// Register globally so otel.Tracer("loopzero-api") works everywhere.
	otel.SetTracerProvider(tp)

	// W3C TraceContext propagation for cross-service trace correlation.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info("tracing enabled",
		"endpoint", endpoint,
		"max_queue_size", maxQueueSize,
		"export_timeout", exportTimeout.String(),
	)

	return tp.Shutdown, nil
}

// isEnabled checks the LOOPZERO_TRACING_ENABLED feature flag (C7).
//
// Default: "true" unless explicitly set to "false" or "0".
//
// T-OBS-007 of deep-review-remediation-plan: previously the package header
// comment said "default true dev, false prod" but production gets the
// SAME default-true behavior as dev — production has to explicitly set
// LOOPZERO_TRACING_ENABLED=false to disable. Per dogfood-2026-05-08-fixes
// T0.1, the local Tilt YAML + bootstrap chart explicitly set false; the
// PRODUCTION default remains tracing-enabled (the OTel collector
// downstream gates whether spans actually flow).
func isEnabled() bool {
	v := os.Getenv("LOOPZERO_TRACING_ENABLED")
	if v == "" {
		return true // Default enabled in BOTH dev and prod — local Tilt + bootstrap chart explicitly disable.
	}
	return !strings.EqualFold(v, "false") && v != "0"
}

// getEnv returns the deployment environment name for resource attributes.
func getEnv() string {
	env := os.Getenv("LOOPZERO_ENV")
	if env == "" {
		return "development"
	}
	return env
}
