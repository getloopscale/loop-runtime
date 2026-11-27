// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/correlation"
)

// Audit gap M04 #23 (active-execution-plan §6.F.1): the deprecated
// package-level FromContext helper was removed along with its 5 dedicated
// tests. The canonical replacement is (*ContextLogger).FromContext via
// dependency injection (ADR-0034), exercised by the tests below.
// RegisterTraceExtractor is still used by 4 main.go binaries and is
// retained until a cross-binary DI refactor lands.

// ---------------------------------------------------------------------------
// T2.5 — ContextLogger explicit DI per ADR-0034.
// ---------------------------------------------------------------------------

func TestContextLogger_FromContext_EnrichesWithTrace(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl := NewContextLogger(base, func(ctx context.Context) (string, string) {
		return "trace-abc", "span-def"
	})
	got := cl.FromContext(context.Background())
	if got == nil {
		t.Fatal("FromContext should return enriched logger")
	}
	if got == base {
		t.Error("FromContext should NOT return the base logger when extractor produces a trace")
	}
}

func TestContextLogger_FromContext_NilExtractorReturnsBase(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl := NewContextLogger(base, nil)
	got := cl.FromContext(context.Background())
	if got != base {
		t.Error("nil extractor should return the base logger unchanged")
	}
}

func TestContextLogger_FromContext_EmptyTraceReturnsBase(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl := NewContextLogger(base, func(ctx context.Context) (string, string) {
		return "", ""
	})
	got := cl.FromContext(context.Background())
	if got != base {
		t.Error("empty trace_id should return the base logger unchanged")
	}
}

func TestContextLogger_NilSafetyReturnsNil(t *testing.T) {
	var cl *ContextLogger
	if got := cl.FromContext(context.Background()); got != nil {
		t.Errorf("nil ContextLogger.FromContext must return nil; got %v", got)
	}
	cl2 := NewContextLogger(nil, nil)
	if got := cl2.FromContext(context.Background()); got != nil {
		t.Errorf("nil base logger must produce nil; got %v", got)
	}
}

func TestContextLogger_TwoInstancesIsolated(t *testing.T) {
	// Two ContextLoggers with different extractors must not interfere.
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl1 := NewContextLogger(base, func(ctx context.Context) (string, string) {
		return "trace-1", "span-1"
	})
	cl2 := NewContextLogger(base, func(ctx context.Context) (string, string) {
		return "trace-2", "span-2"
	})
	if got1, got2 := cl1.FromContext(context.Background()), cl2.FromContext(context.Background()); got1 == got2 {
		t.Error("two ContextLoggers with different extractors must yield distinct loggers (no shared state)")
	}
}

// TestContextLogger_LegacyGlobalAPIStillWorks was deleted along with the
// FromContext deprecated package-level helper (audit gap M04 #23 closure).
// The new canonical API is (*ContextLogger).FromContext, covered by the
// TestContextLogger_FromContext_* family above.

// ---------------------------------------------------------------------------
// Beta Logs Gate — correlation_id enrichment.
// ---------------------------------------------------------------------------
// Per the Beta-readiness audit, log lines must carry correlation_id so an
// operator can grep cross-system from CLI → API → workflow → pod via Loki
// LogQL `{correlation_id="<id>"}` once promtail extracts the JSON label.
// FromContext is the single composition point — adding correlation here
// means every call site that already does `logger := cl.FromContext(ctx)`
// gets the enrichment for free without per-handler changes.

func TestContextLogger_FromContext_EnrichesWithCorrelationID(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl := NewContextLogger(base, nil) // no trace extractor; correlation only
	ctx := contextWithCorrelation(t, "corr-abc-123")
	got := cl.FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext should return non-nil logger when correlation is present")
	}
	if got == base {
		t.Error("FromContext should enrich (return new logger) when correlation_id is set")
	}
}

func TestContextLogger_FromContext_EnrichesWithBothTraceAndCorrelation(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl := NewContextLogger(base, func(ctx context.Context) (string, string) {
		return "trace-1", "span-1"
	})
	ctx := contextWithCorrelation(t, "corr-1")
	got := cl.FromContext(ctx)
	if got == base {
		t.Error("expected enriched logger when BOTH trace and correlation present")
	}
}

func TestContextLogger_FromContext_NoCorrelationReturnsBaseWhenNoTrace(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	cl := NewContextLogger(base, nil)
	// No correlation set; no trace extractor → base logger unchanged.
	got := cl.FromContext(context.Background())
	if got != base {
		t.Error("when neither trace nor correlation are present, must return base unchanged")
	}
}

// TestContextLogger_OutputContainsCorrelationID — end-to-end output
// invariant. Capture actual JSON the enriched logger emits and confirm
// it carries the `correlation_id` key with the expected value. Without
// this, the enrichment could silently break if a future refactor changes
// the slog attribute key name (promtail relabel depends on the literal
// "correlation_id" key matching).
func TestContextLogger_OutputContainsCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))
	cl := NewContextLogger(base, nil)
	ctx := correlation.WithID(context.Background(), "corr-output-probe")
	cl.FromContext(ctx).Info("probe")
	if !strings.Contains(buf.String(), `"correlation_id":"corr-output-probe"`) {
		t.Errorf("expected JSON to contain correlation_id=corr-output-probe, got: %s", buf.String())
	}
}

// TestLoggerFromContext_FallsBackToDefault — when no logger is wired
// via WithLogger (background goroutine, test, recovery path), the
// fallback to slog.Default() prevents nil-pointer panics.
func TestLoggerFromContext_FallsBackToDefault(t *testing.T) {
	got := LoggerFromContext(context.Background())
	if got == nil {
		t.Fatal("LoggerFromContext must NEVER return nil")
	}
}

// TestWithLogger_RoundTrip — the registered logger comes back out.
func TestWithLogger_RoundTrip(t *testing.T) {
	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	enriched := base.With("k", "v")
	ctx := WithLogger(context.Background(), enriched)
	got := LoggerFromContext(ctx)
	if got != enriched {
		t.Error("LoggerFromContext should return the logger registered via WithLogger")
	}
}

// TestWithLogger_NilIsNoOp — protect against accidentally registering
// a nil logger (would mean every downstream LoggerFromContext caller
// panics on first method call). Nil is a no-op; fallback to default.
func TestWithLogger_NilIsNoOp(t *testing.T) {
	ctx := WithLogger(context.Background(), nil)
	got := LoggerFromContext(ctx)
	if got == nil {
		t.Fatal("nil logger registration must NOT corrupt LoggerFromContext")
	}
}

// contextWithCorrelation builds a context carrying a correlation ID via
// the canonical pkg/correlation API. pkg/correlation is in the same Go
// module as pkg/logging so direct import is safe (no cycle).
func contextWithCorrelation(t *testing.T, id string) context.Context {
	t.Helper()
	return correlation.WithID(context.Background(), id)
}
