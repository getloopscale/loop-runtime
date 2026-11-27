// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package observability

import (
	"net/http"

	"github.com/getloopscale/loop-runtime/pkg/logging"
)

// RequestLogger is HTTP middleware that constructs a request-scoped
// *slog.Logger enriched with correlation_id + trace context, and
// registers it in the request ctx via `logging.WithLogger`.
//
// Downstream handlers retrieve the logger via
// `logging.LoggerFromContext(r.Context())` and every log line they
// emit automatically carries `correlation_id` (+ trace_id/span_id
// when an OTel extractor is configured).
//
// Beta Logs Gate (audit M08 / 7-criteria Beta readiness): this is
// the load-bearing wire that connects:
//   - CorrelationID middleware (injects id into ctx)
//   - pkg/logging.ContextLogger.FromContext (enriches a base logger)
//   - pkg/logging.WithLogger (registers in ctx)
//   - logging.LoggerFromContext (handler retrieval)
//
// MUST be chained AFTER CorrelationID middleware so the id is
// already present in ctx when the enrichment runs.
//
// Pattern in router setup:
//
//	r.Use(middleware.CorrelationID)
//	r.Use(middleware.RequestLogger(contextLogger))
//	r.Use(middleware.AuditLog(logger, writer))
func RequestLogger(cl *logging.ContextLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// cl.FromContext enriches with correlation_id (read from
			// ctx by pkg/logging) + trace context (if extractor is
			// configured). When cl is nil — e.g. a test wired only
			// CorrelationID and didn't supply a logger — we skip
			// enrichment and let handlers fall back to slog.Default.
			if cl == nil {
				next.ServeHTTP(w, r)
				return
			}
			enriched := cl.FromContext(r.Context())
			if enriched == nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := logging.WithLogger(r.Context(), enriched)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
