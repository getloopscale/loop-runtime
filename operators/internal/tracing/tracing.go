// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package tracing provides OpenTelemetry distributed tracing bootstrap for operators.
// Mirrors the API tracing package (api/internal/tracing/tracing.go) with identical
// configuration: OTLP gRPC exporter, feature flag, graceful degradation.
package tracing

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	DefaultEndpoint = "otel-collector.loopruntime-system.svc.cluster.local:4317"

	// exportTimeout is the maximum time for an export attempt.
	// Default OTel SDK is 10s — too long, risks latency under failure.
	exportTimeout = 1 * time.Second

	// maxQueueSize bounds the span export queue.
	// Prevents unbounded memory growth when Collector is unreachable.
	maxQueueSize = 2048
)

// Init configures the OTel TracerProvider with OTLP gRPC exporter.
// Returns a shutdown function for graceful shutdown.
//
// If LOOPRUNTIME_TRACING_ENABLED is "false", returns a no-op shutdown
// with zero overhead — no TracerProvider is registered.
//
// If the Collector is unreachable, the operator continues normally.
// Spans are dropped after queue exhaustion (graceful degradation).
func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
	if !isEnabled() {
		slog.Info("tracing disabled via LOOPRUNTIME_TRACING_ENABLED=false")
		return func(context.Context) error { return nil }, nil
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	// Validated at the boundary — a typo here should fail the operator's startup,
	// not degrade tracing silently after the value was already logged.
	endpointHost, endpointPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT is not a valid host:port: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(), // In-cluster, no TLS
		otlptracegrpc.WithTimeout(exportTimeout),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
			Enabled: false, // No retry — fail fast
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

	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithMaxQueueSize(maxQueueSize),
		sdktrace.WithExportTimeout(exportTimeout),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// Register globally so otel.Tracer("loopruntime-operator") works everywhere.
	otel.SetTracerProvider(tp)

	// W3C TraceContext propagation for cross-service trace correlation.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	//nolint:gosec // G706 false positive — same reasoning as the pprof server:
	// these are the components net.SplitHostPort accepted, and gosec's taint
	// analysis has no notion of validation.
	slog.Info("tracing enabled",
		"endpoint_host", endpointHost,
		"endpoint_port", endpointPort,
		"max_queue_size", maxQueueSize,
		"export_timeout", exportTimeout.String(),
	)

	return tp.Shutdown, nil
}

// isEnabled checks the LOOPRUNTIME_TRACING_ENABLED feature flag.
// Default: "true" unless explicitly set to "false".
func isEnabled() bool {
	v := os.Getenv("LOOPRUNTIME_TRACING_ENABLED")
	if v == "" {
		return true
	}
	return !strings.EqualFold(v, "false") && v != "0"
}

// getEnv returns the deployment environment name for resource attributes.
func getEnv() string {
	env := os.Getenv("LOOPRUNTIME_ENV")
	if env == "" {
		return "development"
	}
	return env
}
