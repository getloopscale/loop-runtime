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

var (
	globalExtractor TraceExtractor
	extractorMu     sync.RWMutex
)

// RegisterTraceExtractor sets the global trace extractor.
// Call once at startup from the component that initializes OTel.
//
// Example (in api/cmd/main.go):
//
//	logging.RegisterTraceExtractor(func(ctx context.Context) (string, string) {
//	    sc := trace.SpanContextFromContext(ctx)
//	    return sc.TraceID().String(), sc.SpanID().String()
//	})
func RegisterTraceExtractor(fn TraceExtractor) {
	extractorMu.Lock()
	defer extractorMu.Unlock()
	globalExtractor = fn
}

// FromContext returns a child logger enriched with trace context from ctx.
// If a TraceExtractor is registered and the context carries a valid trace,
// trace_id and span_id fields are added to the logger.
// If no extractor is registered or the context has no trace, returns the
// logger unchanged.
func FromContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return nil
	}

	extractorMu.RLock()
	fn := globalExtractor
	extractorMu.RUnlock()

	if fn == nil {
		return logger
	}

	traceID, spanID := fn(ctx)
	if traceID == "" || traceID == "00000000000000000000000000000000" {
		return logger
	}

	attrs := []any{"trace_id", traceID}
	if spanID != "" && spanID != "0000000000000000" {
		attrs = append(attrs, "span_id", spanID)
	}
	return logger.With(attrs...)
}
