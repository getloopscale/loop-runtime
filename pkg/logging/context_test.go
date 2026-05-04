// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestFromContext_WithExtractor(t *testing.T) {
	// Register a mock extractor
	RegisterTraceExtractor(func(_ context.Context) (string, string) {
		return "abc123trace", "def456span"
	})
	defer RegisterTraceExtractor(nil) // cleanup

	var buf bytes.Buffer
	logger := NewLogger("test", &buf)
	logger = FromContext(context.Background(), logger)
	logger.Info("traced log")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	if parsed["trace_id"] != "abc123trace" {
		t.Errorf("expected trace_id 'abc123trace', got %v", parsed["trace_id"])
	}
	if parsed["span_id"] != "def456span" {
		t.Errorf("expected span_id 'def456span', got %v", parsed["span_id"])
	}
}

func TestFromContext_WithoutExtractor(t *testing.T) {
	RegisterTraceExtractor(nil) // ensure no extractor

	var buf bytes.Buffer
	logger := NewLogger("test", &buf)
	logger = FromContext(context.Background(), logger)
	logger.Info("no trace")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	if _, ok := parsed["trace_id"]; ok {
		t.Error("expected no trace_id without extractor")
	}
}

func TestFromContext_ZeroTraceID(t *testing.T) {
	RegisterTraceExtractor(func(_ context.Context) (string, string) {
		return "00000000000000000000000000000000", "0000000000000000"
	})
	defer RegisterTraceExtractor(nil)

	var buf bytes.Buffer
	logger := NewLogger("test", &buf)
	logger = FromContext(context.Background(), logger)
	logger.Info("zero trace")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	if _, ok := parsed["trace_id"]; ok {
		t.Error("expected no trace_id for zero trace ID")
	}
}

func TestFromContext_NilLogger(t *testing.T) {
	result := FromContext(context.Background(), nil)
	if result != nil {
		t.Error("expected nil for nil logger")
	}
}

func TestFromContext_WithCorrelationID(t *testing.T) {
	RegisterTraceExtractor(func(_ context.Context) (string, string) {
		return "trace123", "span456"
	})
	defer RegisterTraceExtractor(nil)

	var buf bytes.Buffer
	logger := NewLogger("test", &buf)
	logger = WithCorrelationID(logger, "corr-789")
	logger = FromContext(context.Background(), logger)
	logger.Info("full context")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}

	if parsed["correlation_id"] != "corr-789" {
		t.Errorf("expected correlation_id 'corr-789', got %v", parsed["correlation_id"])
	}
	if parsed["trace_id"] != "trace123" {
		t.Errorf("expected trace_id 'trace123', got %v", parsed["trace_id"])
	}
}

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

func TestContextLogger_LegacyGlobalAPIStillWorks(t *testing.T) {
	// Backward-compat regression guard: the deprecated global registry
	// must keep working until the next major release.
	RegisterTraceExtractor(func(ctx context.Context) (string, string) {
		return "legacy-trace", "legacy-span"
	})
	defer RegisterTraceExtractor(nil) // reset for other tests

	base := slog.New(slog.NewTextHandler(io.Discard, nil))
	got := FromContext(context.Background(), base)
	if got == nil {
		t.Fatal("legacy FromContext should return a logger")
	}
	if got == base {
		t.Error("legacy FromContext should enrich the logger when extractor is registered")
	}
}
