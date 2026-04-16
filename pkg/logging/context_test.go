// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package logging

import (
	"bytes"
	"context"
	"encoding/json"
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
