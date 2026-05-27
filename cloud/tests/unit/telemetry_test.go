// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.2 of plan 2026-05-27-backend-frontend-integration v1.1.
// Tests the 4 telemetry endpoints (/v1/telemetry/{errors,web-vitals,events,interactions})
// plus the 64 KiB body cap (EC-03 DOS protection — sendBeacon worst case).

package unit

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/routes"
)

// captureLogger returns a slog.Logger that writes JSON records into a
// bytes.Buffer the test can inspect. Each test gets its own buffer so
// parallel-safe.
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), buf
}

func postTelemetry(t *testing.T, handler http.HandlerFunc, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/telemetry/x", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestTelemetry_Errors_AcceptsValidPayload(t *testing.T) {
	logger, buf := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	body, _ := json.Marshal(map[string]any{
		"message": "TypeError: undefined is not a function",
		"stack":   "at App.tsx:42",
	})
	w := postTelemetry(t, h.Errors, body)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(buf.String(), `"category":"errors"`) {
		t.Errorf("structured log missing category=errors: %s", buf.String())
	}
}

func TestTelemetry_WebVitals_LogsCategory(t *testing.T) {
	logger, buf := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	body, _ := json.Marshal(map[string]any{"metric": "LCP", "value": 1850})
	w := postTelemetry(t, h.WebVitals, body)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}
	if !strings.Contains(buf.String(), `"category":"web-vitals"`) {
		t.Errorf("structured log missing category=web-vitals: %s", buf.String())
	}
}

func TestTelemetry_Events_RejectsMalformedJSON(t *testing.T) {
	logger, _ := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	w := postTelemetry(t, h.Events, []byte("this is not json {{"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "INVALID_PAYLOAD") {
		t.Errorf("want INVALID_PAYLOAD code, got: %s", w.Body.String())
	}
}

func TestTelemetry_Interactions_NoAuthRequired(t *testing.T) {
	// Sanity: the handler itself does NOT check Authorization. (Routing
	// also leaves it un-guarded — verified separately by main_test if
	// added — but this test pins the handler-level contract: sendBeacon
	// has no way to set a Bearer header, so we must accept anonymous.)
	logger, _ := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	body, _ := json.Marshal(map[string]any{"action": "command_palette_open"})
	// Deliberately omit Authorization header
	req := httptest.NewRequest(http.MethodPost, "/v1/telemetry/interactions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Interactions(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202 (no auth), got %d", w.Code)
	}
}

func TestTelemetry_RejectsPayloadOver64KB(t *testing.T) {
	// EC-03: hostile / buggy client posts 100 KiB → 413.
	logger, _ := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	// Build a JSON payload comfortably larger than 64 KiB:
	//   { "noise": "<100 KiB of 'a'>" }
	big := strings.Repeat("a", 100*1024)
	body := []byte(`{"noise":"` + big + `"}`)
	if len(body) <= 64*1024 {
		t.Fatalf("test fixture not actually >64KiB (len=%d)", len(body))
	}
	w := postTelemetry(t, h.Errors, body)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PAYLOAD_TOO_LARGE") {
		t.Errorf("want PAYLOAD_TOO_LARGE code, got: %s", w.Body.String())
	}
}

func TestTelemetry_Limit64KB_BoundaryExact(t *testing.T) {
	// Payload of EXACTLY 64 KiB must be accepted; +1 byte must be rejected.
	// Catches off-by-one in MaxBytesReader configuration.
	logger, _ := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	// Build a JSON object whose total serialized length is exactly 64 KiB.
	// Frame: {"x":"..."} → 8 chars overhead.
	overhead := len(`{"x":""}`) // 8
	pad := 64*1024 - overhead
	exactBody := []byte(`{"x":"` + strings.Repeat("a", pad) + `"}`)
	if len(exactBody) != 64*1024 {
		t.Fatalf("fixture len=%d not 64KiB", len(exactBody))
	}
	w := postTelemetry(t, h.Errors, exactBody)
	if w.Code != http.StatusAccepted {
		t.Fatalf("exact-64KiB payload rejected; want 202 got %d body=%s",
			w.Code, w.Body.String())
	}

	overBody := append(exactBody[:len(exactBody)-2], []byte(`bb"}`)...)
	if len(overBody) <= 64*1024 {
		t.Fatalf("over-fixture len=%d not >64KiB", len(overBody))
	}
	w2 := postTelemetry(t, h.Errors, overBody)
	if w2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("+1 byte payload accepted; want 413 got %d", w2.Code)
	}
}

func TestTelemetry_NilLoggerFallsBackToDefault(t *testing.T) {
	// Nil-safety: NewTelemetryHandler(nil) must not panic. This matters
	// because main() may construct the handler before any logger wiring.
	h := routes.NewTelemetryHandler(nil)
	body, _ := json.Marshal(map[string]any{"k": "v"})
	w := postTelemetry(t, h.Events, body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("nil-logger fallback broken: %d %s", w.Code, w.Body.String())
	}
}

func TestTelemetry_LogPayloadPreserved(t *testing.T) {
	// The full inbound payload must appear inside the log record so a
	// future ingest pipeline (Loki/Tempo) can correlate by any field.
	logger, buf := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	body, _ := json.Marshal(map[string]any{
		"deploy_id":   "dpl_42",
		"step":        "build",
		"duration_ms": 2310,
	})
	w := postTelemetry(t, h.Events, body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}

	// Decode the captured JSON line and confirm the payload is round-tripped.
	logLine, err := io.ReadAll(buf)
	if err != nil {
		t.Fatalf("read log buffer: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(logLine, &rec); err != nil {
		t.Fatalf("log line not valid JSON: %v line=%s", err, logLine)
	}
	payload, ok := rec["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload key not a map: %v", rec["payload"])
	}
	if payload["deploy_id"] != "dpl_42" {
		t.Errorf("deploy_id missing from logged payload: %v", payload)
	}
}
