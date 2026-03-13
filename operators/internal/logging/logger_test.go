package logging_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/loopzero/loopzero/operators/internal/logging"
)

func TestNewLogger_OutputsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger("test-component", &buf)

	logger.Info("test message")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("logger output is not valid JSON: %v\nraw output: %s", err, buf.String())
	}
}

func TestNewLogger_RequiredFieldsPresent(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger("my-controller", &buf)

	logger.Info("hello structured logging")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	requiredFields := []string{"time", "level", "msg", "component"}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("missing required field %q in log output: %v", field, parsed)
		}
	}

	if parsed["component"] != "my-controller" {
		t.Errorf("expected component=%q, got %q", "my-controller", parsed["component"])
	}
	if parsed["msg"] != "hello structured logging" {
		t.Errorf("expected msg=%q, got %q", "hello structured logging", parsed["msg"])
	}
}

func TestWithCorrelationID_AddsField(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger("test-component", &buf)
	logger = logging.WithCorrelationID(logger, "corr-456")

	logger.Info("correlated log")

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed["correlation_id"] != "corr-456" {
		t.Errorf("expected correlation_id=%q, got %q", "corr-456", parsed["correlation_id"])
	}
	if parsed["component"] != "test-component" {
		t.Errorf("expected component=%q, got %q", "test-component", parsed["component"])
	}
}

func TestNewLogger_RespectsLogLevelEnv(t *testing.T) {
	original := os.Getenv("LOG_LEVEL")
	defer func() {
		if original == "" {
			os.Unsetenv("LOG_LEVEL")
		} else {
			os.Setenv("LOG_LEVEL", original)
		}
	}()

	os.Setenv("LOG_LEVEL", "WARN")

	var buf bytes.Buffer
	logger := logging.NewLogger("test-component", &buf)

	logger.Info("this should be suppressed")
	if buf.Len() != 0 {
		t.Errorf("expected info log to be suppressed at WARN level, but got: %s", buf.String())
	}

	logger.Warn("this should appear")
	if buf.Len() == 0 {
		t.Error("expected warn log to appear at WARN level, but got nothing")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if parsed["msg"] != "this should appear" {
		t.Errorf("expected msg=%q, got %q", "this should appear", parsed["msg"])
	}
}
