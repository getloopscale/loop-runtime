// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logging

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loop-runtime/pkg/correlation"
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

// FromContext returns a child logger enriched with trace + correlation
// context. Compose this on each request/reconcile boundary so structured
// logs include the active trace_id/span_id and correlation_id without
// callers passing them explicitly.
//
// Beta Logs Gate (audit M08 / 7-criteria Beta readiness): the
// correlation_id enrichment is the load-bearing piece for cross-system
// grep — once promtail extracts it as a Loki label, an operator can run
// `logcli query '{correlation_id="X"}'` and see every log line from CLI
// boundary down through the API request, through any operator reconcile,
// down to pod-side audit_log writes.
//
// Returns nil when the base logger is nil. Returns the base logger
// unchanged when neither a trace NOR a correlation ID is present.
func (c *ContextLogger) FromContext(ctx context.Context) *slog.Logger {
	if c == nil || c.base == nil {
		return nil
	}

	var attrs []any

	if c.extractor != nil {
		traceID, spanID := c.extractor(ctx)
		if traceID != "" && traceID != "00000000000000000000000000000000" {
			attrs = append(attrs, "trace_id", traceID)
			if spanID != "" && spanID != "0000000000000000" {
				attrs = append(attrs, "span_id", spanID)
			}
		}
	}

	if cid := correlation.IDFromContext(ctx); cid != "" {
		attrs = append(attrs, "correlation_id", cid)
	}

	if len(attrs) == 0 {
		return c.base
	}
	return c.base.With(attrs...)
}

// Beta Logs Gate — context-keyed enriched logger.
// ---------------------------------------------------------------------------
// `LoggerFromContext` + `WithLogger` provide the request-scoped pattern
// that the API middleware uses to make `correlation_id` (+ trace, when
// configured) automatically present on every log line emitted from
// downstream handlers, without each handler having to call
// `cl.FromContext(ctx)` manually.
//
// Pattern (in api/cmd/main.go):
//
//	cl := logging.NewContextLogger(baseLogger, traceExtractor)
//	r.Use(middleware.RequestLogger(cl))
//
// Pattern (in a handler):
//
//	logger := logging.LoggerFromContext(r.Context())
//	logger.Info("project deployed", "project", p.Name)
//
// The emitted JSON automatically carries `correlation_id` because the
// middleware ran `cl.FromContext(ctx)` for the request and registered
// the resulting *slog.Logger via WithLogger.

type loggerCtxKey struct{}

// WithLogger returns a child context carrying `logger` for downstream
// retrieval via LoggerFromContext.
//
// Per ADR-0034 explicit DI: the request middleware constructs the
// enriched logger once and threads it through the ctx; handlers do not
// need to re-extract trace/correlation themselves.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerCtxKey{}, logger)
}

// LoggerFromContext extracts the request-scoped enriched logger.
//
// Returns slog.Default() as the safe fallback when:
//   - No logger was registered via WithLogger (background goroutine,
//     test that did not wire the middleware, recovery code path).
//
// This is the canonical way for handlers + services to get a logger
// that automatically has correlation_id bound — equivalent to calling
// `cl.FromContext(ctx)` everywhere, but the middleware did the
// enrichment once for the whole request scope.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if v, ok := ctx.Value(loggerCtxKey{}).(*slog.Logger); ok && v != nil {
		return v
	}
	return slog.Default()
}

// Audit gap M04 #23 closure (active-execution-plan §6.F.1b): the legacy
// `RegisterTraceExtractor` + `globalExtractor` + `extractorMu` were removed.
// They became dead code when the package-level `FromContext` helper
// (their only consumer) was deleted in §6.F.1 (commit d98c20e9). The
// 4 operator main.go binaries that used to call `RegisterTraceExtractor`
// were write-only into a registry no one read — registration produced
// no observable behavior. Operators that want trace_id enrichment in
// their slog logs should construct a `ContextLogger` via
// `NewContextLogger(baseLogger, extractorFn)` and call its FromContext
// per-reconcile boundary (ADR-0034 explicit DI).
