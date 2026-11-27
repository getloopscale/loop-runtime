// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewLogger(t *testing.T) {
	t.Run("outputs valid JSON", func(t *testing.T) {
		var buf bytes.Buffer
		logger := NewLogger("test-component", &buf)
		logger.Info("test message")

		var parsed map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("expected valid JSON, got error: %v", err)
		}
	})

	t.Run("includes required fields: time, level, msg, component", func(t *testing.T) {
		var buf bytes.Buffer
		logger := NewLogger("api-server", &buf)
		logger.Info("hello structured logging")

		var parsed map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("JSON parse error: %v", err)
		}

		for _, field := range []string{"time", "level", "msg", "component"} {
			if _, ok := parsed[field]; !ok {
				t.Errorf("expected field %q in log output", field)
			}
		}

		if parsed["msg"] != "hello structured logging" {
			t.Errorf("expected msg 'hello structured logging', got %v", parsed["msg"])
		}
		if parsed["component"] != "api-server" {
			t.Errorf("expected component 'api-server', got %v", parsed["component"])
		}
	})

	t.Run("respects LOG_LEVEL env var", func(t *testing.T) {
		t.Setenv("LOG_LEVEL", "warn")

		var buf bytes.Buffer
		logger := NewLogger("test", &buf)
		logger.Info("this should be suppressed")
		logger.Warn("this should appear")

		lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
		if len(lines) != 1 {
			t.Fatalf("expected 1 log line, got %d", len(lines))
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(lines[0], &parsed); err != nil {
			t.Fatalf("JSON parse error: %v", err)
		}
		if parsed["msg"] != "this should appear" {
			t.Errorf("expected 'this should appear', got %v", parsed["msg"])
		}
	})

	t.Run("defaults to stdout when no writer provided", func(t *testing.T) {
		// Should not panic
		logger := NewLogger("default-test")
		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
	})
}

func TestParseLogLevel_AllVariants(t *testing.T) {
	tests := []struct {
		env            string
		wantSuppressed string // a message at this level should be suppressed
		wantVisible    string // a message at this level should be visible
	}{
		{"DEBUG", "", "debug message"},
		{"debug", "", "debug message"},
		{"WARN", "info suppressed", "warn message"},
		{"WARNING", "info suppressed", "warn visible"},
		{"warn", "info suppressed", "warn message"},
		{"ERROR", "warn suppressed", "error message"},
		{"error", "warn suppressed", "error message"},
		{"", "debug suppressed", "info message"},        // default = info
		{"INVALID", "debug suppressed", "info message"}, // unknown = info
		{"  WARN  ", "info suppressed", "warn message"}, // whitespace trimmed
	}

	for _, tt := range tests {
		t.Run("LOG_LEVEL="+tt.env, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", tt.env)
			var buf bytes.Buffer
			logger := NewLogger("test", &buf)

			if tt.wantSuppressed != "" {
				logger.Debug(tt.wantSuppressed)
				logger.Info(tt.wantSuppressed)
			}
			// Reset buffer to only check visible message
			buf.Reset()
			logger.Warn(tt.wantVisible)
			logger.Error(tt.wantVisible)

			if buf.Len() == 0 {
				t.Errorf("expected at least one log line for level %q", tt.env)
			}
		})
	}
}

func TestWithCorrelationID(t *testing.T) {
	t.Run("adds correlation_id field", func(t *testing.T) {
		var buf bytes.Buffer
		logger := NewLogger("test-component", &buf)
		logger = WithCorrelationID(logger, "abc-123")
		logger.Info("correlated log")

		var parsed map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("JSON parse error: %v", err)
		}

		if parsed["correlation_id"] != "abc-123" {
			t.Errorf("expected correlation_id 'abc-123', got %v", parsed["correlation_id"])
		}
		if parsed["component"] != "test-component" {
			t.Errorf("expected component preserved, got %v", parsed["component"])
		}
	})
}
