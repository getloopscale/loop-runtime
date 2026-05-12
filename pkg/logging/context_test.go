// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package logging

import (
	"context"
	"io"
	"log/slog"
	"testing"
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
