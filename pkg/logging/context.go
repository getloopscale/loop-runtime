// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package logging

import (
	"context"
	"log/slog"
	"sync"
)

// TraceExtractor extracts trace_id and span_id from a context.
// Callers (api/, operators/) register their OTel-specific extractor
// so pkg/ can inject trace context into logs without depending on OTel.
type TraceExtractor func(ctx context.Context) (traceID, spanID string)

// ContextLogger is an explicit, dependency-injected combination of a
// base *slog.Logger and a TraceExtractor. It replaces the package-level
// global registry (RegisterTraceExtractor) per ADR-0034 so multiple
// extractors can coexist (e.g., when api+operators run in the same
// binary, or when tests want isolation).
//
// Per ADR-0034 + Google CDK design principle (no global state).
//
// Usage in main():
//
//	cl := logging.NewContextLogger(baseLogger, func(ctx context.Context) (string, string) {
//	    sc := trace.SpanContextFromContext(ctx)
//	    return sc.TraceID().String(), sc.SpanID().String()
//	})
//	// later in handlers:
//	logger := cl.FromContext(ctx)
//	logger.Info("request received", "path", r.URL.Path)
type ContextLogger struct {
	base      *slog.Logger
	extractor TraceExtractor
}

// NewContextLogger constructs a ContextLogger with the given base logger
// and trace extractor. Either may be nil — a nil base means the
// returned (*ContextLogger).FromContext returns nil too; a nil extractor
// means trace_id is never enriched and FromContext returns the base
// logger unchanged.
func NewContextLogger(base *slog.Logger, extractor TraceExtractor) *ContextLogger {
	return &ContextLogger{base: base, extractor: extractor}
}

// FromContext returns a child logger enriched with trace context.
// Compose this on each request/reconcile boundary so structured logs
// include the active trace_id/span_id without callers passing them
// explicitly.
//
// Returns nil when the base logger is nil. Returns the base logger
// unchanged when no extractor is configured or the context has no
// active trace.
func (c *ContextLogger) FromContext(ctx context.Context) *slog.Logger {
	if c == nil || c.base == nil {
		return nil
	}
	if c.extractor == nil {
		return c.base
	}
	traceID, spanID := c.extractor(ctx)
	if traceID == "" || traceID == "00000000000000000000000000000000" {
		return c.base
	}
	attrs := []any{"trace_id", traceID}
	if spanID != "" && spanID != "0000000000000000" {
		attrs = append(attrs, "span_id", spanID)
	}
	return c.base.With(attrs...)
}

// --- Legacy global API (deprecated, retained for 1-release window per CONDITION-1) ---

var (
	globalExtractor TraceExtractor
	extractorMu     sync.RWMutex
)

// RegisterTraceExtractor sets the global trace extractor.
//
// Deprecated: prefer NewContextLogger for explicit injection (per
// ADR-0034). The global registry will be removed in the next major
// release. Existing callers should migrate by:
//
//  1. In main.go, replace the RegisterTraceExtractor call with
//     `cl := logging.NewContextLogger(baseLogger, extractorFn)`.
//  2. Pass `cl` to handlers via your usual DI mechanism.
//  3. Replace `logging.FromContext(ctx, logger)` calls with
//     `cl.FromContext(ctx)`.
func RegisterTraceExtractor(fn TraceExtractor) {
	extractorMu.Lock()
	defer extractorMu.Unlock()
	globalExtractor = fn
}

