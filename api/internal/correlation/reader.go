// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package correlation reads the Correlation-ID header from incoming
// requests, with backward-compatible dual-read of the legacy
// `X-Correlation-ID` form.
//
// T6.1 of plan 2026-06-01-p3-infra-observability-test-debt-plan.md v1.1
// (closes finding `val-002`). ADR D5: canonical header form is
// `Correlation-ID` (no `X-` prefix), matching RFC 6648 deprecation of
// `X-` prefixes and the existing precedent in `api/internal/middleware/
// correlation.go` + `routes/builds.go:412`. Pre-T6.1, `promote.go:130`
// and `rollback.go:121` read `X-Correlation-ID` directly, breaking
// distributed tracing for any caller relying on the middleware-injected
// canonical header.
//
// Why a helper instead of inlining: the gate
// `cmd/loopruntime-ops/internal/quality/correlationheader` SCANS every HTTP package
// (`api/internal/<feature>/<name>http`) for
// any `r.Header.Get("X-?Correlation-ID")` literal. The reader lives
// here (`api/internal/correlation/`), OUTSIDE the gate's scan scope,
// so the backward-compat fallback is allowed exactly once at the
// authoritative source.
//
// Deprecation timeline (ADR D5):
//   - v1: helper accepts BOTH headers; WARN logs each `X-Correlation-ID`
//     hit so operators can identify lagging callers.
//   - v2 (next major): helper drops the fallback. Callers that still
//     send `X-Correlation-ID` will get correlationID="" and downstream
//     tracing degrades — surface explicitly in the CHANGELOG.
package correlation

import (
	"log/slog"
	"net/http"
)

// canonicalHeader is the modern form. New code MUST use this name.
const canonicalHeader = "Correlation-ID"

// legacyHeader is the deprecated form (ADR D5). Continues to be
// accepted via Read() for one release cycle; emits a deprecation
// warning on every hit.
const legacyHeader = "X-Correlation-ID"

// Read returns the Correlation-ID for the incoming request. First
// tries the canonical `Correlation-ID` header; if empty, falls back
// to the deprecated `X-Correlation-ID` and logs a WARN naming the
// caller's User-Agent (so operators can identify lagging clients).
//
// Returns the empty string when NEITHER header is present — callers
// should treat empty as "no propagated ID" (the surrounding context
// MAY already have one injected by middleware/correlation.go).
//
// Per gate `cmd/loopruntime-ops/internal/quality/correlationheader`: routes/*.go MUST NOT call
// `r.Header.Get("Correlation-ID"|"X-Correlation-ID")` directly —
// they MUST call this function. The gate's AST scan exempts this
// package (`api/internal/correlation/`) so the fallback lives in
// exactly ONE place.
func Read(r *http.Request) string {
	if id := r.Header.Get(canonicalHeader); id != "" {
		return id
	}
	if id := r.Header.Get(legacyHeader); id != "" {
		slog.Warn("deprecated X-Correlation-ID header — caller should switch to Correlation-ID",
			"user_agent", r.Header.Get("User-Agent"),
			"path", r.URL.Path,
			"correlation_id", id,
		)
		return id
	}
	return ""
}
