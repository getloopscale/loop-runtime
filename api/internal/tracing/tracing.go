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
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.32.0"
)

// preCheckOTelEndpoint resolves the hostname portion of the OTel collector
// endpoint via net.LookupHost. Returns nil on success; non-nil error means
// DNS resolution failed and the caller SHOULD register a no-op exporter
// instead of letting the gRPC client retry forever (F-S3-01).
//
// Plan T3.1 D9 — endpoint format examples accepted:
//   - "otel-collector.loopzero-system.svc.cluster.local:4317"
//   - "otel-collector:4317"
//   - "http://otel-collector:4317" (scheme stripped)
//   - "otel-collector" (no port)
func preCheckOTelEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("endpoint is empty")
	}
	host := endpoint
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if idx := strings.IndexByte(host, '/'); idx >= 0 {
		host = host[:idx]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return fmt.Errorf("could not parse hostname from %q", endpoint)
	}
	// Use a short-timeout resolver to avoid blocking startup.
	resolver := &net.Resolver{}
	resolveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := resolver.LookupHost(resolveCtx, host)
	if err != nil {
		return fmt.Errorf("LookupHost %q: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no addresses for %q", host)
	}
	return nil
}

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

// Options is what Init needs from configuration (config.Config carries it).
type Options struct {
	// Enabled turns export on; when false Init registers nothing.
	Enabled bool
	// Endpoint is the OTLP collector; empty means DefaultEndpoint.
	Endpoint string
	// Environment is the deployment.environment resource attribute; empty
	// means "development".
	Environment string
}

// Init configures the OTel TracerProvider with OTLP gRPC exporter.
// Returns a shutdown function for graceful shutdown.
//
// If LOOPZERO_TRACING_ENABLED is "false" (C7), returns a no-op shutdown
// with zero overhead — no TracerProvider is registered.
//
// If the Collector is unreachable, the API continues normally.
// Spans are dropped after queue exhaustion (graceful degradation).
func Init(ctx context.Context, serviceName string, opts Options) (shutdown func(context.Context) error, err error) {
	if !opts.Enabled {
		slog.Info("tracing disabled via LOOPZERO_TRACING_ENABLED=false")
		return func(context.Context) error { return nil }, nil
	}

	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	environment := opts.Environment
	if environment == "" {
		environment = "development"
	}

	// Plan T3.1 D9 — DNS pre-check. If the endpoint hostname is unresolvable,
	// register no-op exporter and log SINGLE warn instead of letting the
	// upstream OTel client retry forever (which spams "name resolver error:
	// produced zero addresses" every 5s — F-S3-01).
	if err := preCheckOTelEndpoint(endpoint); err != nil {
		slog.Warn("tracing disabled: OTel endpoint unresolvable",
			"endpoint", endpoint, "error", err,
			"hint", "set OTEL_EXPORTER_OTLP_ENDPOINT to a reachable collector or unset LOOPZERO_TRACING_ENABLED")
		return func(context.Context) error { return nil }, nil
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
			semconv.DeploymentEnvironmentName(environment),
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
