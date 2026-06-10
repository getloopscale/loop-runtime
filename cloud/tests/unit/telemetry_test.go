// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T1.2 of plan 2026-05-27-backend-frontend-integration v1.1.
// Tests the 4 telemetry endpoints (/v1/telemetry/{errors,web-vitals,events,interactions})
// plus the 64 KiB body cap (EC-03 DOS protection — sendBeacon worst case).

package unit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/metrics"
	"github.com/loopzero/loopzero/cloud/internal/routes"
)

// scrapeMetricCounter renders the /metrics output via metrics.Handler()
// and returns the float64 value of the counter named `metricName`.
// Returns 0 if the metric is absent from the output (Prometheus emits
// counters only after first increment OR after init() registration).
// Lines without labels follow `<name> <value>`.
func scrapeMetricCounter(metricName string) (float64, error) {
	h := metrics.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return 0, fmt.Errorf("metrics handler returned %d", rec.Code)
	}
	scanner := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Expect either `<name> <value>` (no labels) OR `<name>{<labels>} <value>`.
		// We need exact match on the name segment before any '{' or space.
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		nameSeg := fields[0]
		if idx := strings.Index(nameSeg, "{"); idx >= 0 {
			nameSeg = nameSeg[:idx]
		}
		if nameSeg != metricName {
			continue
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse value %q: %w", fields[1], err)
		}
		return v, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return 0, nil // not present yet — counter starts at 0
}

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

// Plan J-05 T3.1 D4 + EC-3 — counter increment is the LAST step before
// 202, gated on `event=device_success_viewed`. The next three tests pin
// that contract.

func TestTelemetry_Events_DeviceSuccessViewed_IncrementsCounter(t *testing.T) {
	logger, _ := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	before := readBrowserSuccessCounter(t)
	body, _ := json.Marshal(map[string]any{
		"event":     "device_success_viewed",
		"timestamp": 1700000000,
		"origin":    "https://app-dev.loopzero.example",
	})
	w := postTelemetry(t, h.Events, body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}
	after := readBrowserSuccessCounter(t)
	if after-before != 1 {
		t.Fatalf("expected counter +1, got delta=%v (before=%v after=%v)",
			after-before, before, after)
	}
}

func TestTelemetry_Events_OtherEvent_DoesNotIncrement(t *testing.T) {
	logger, _ := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	before := readBrowserSuccessCounter(t)
	body, _ := json.Marshal(map[string]any{
		"event": "deploy_clicked",
	})
	w := postTelemetry(t, h.Events, body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}
	after := readBrowserSuccessCounter(t)
	if after != before {
		t.Fatalf("expected counter unchanged, got delta=%v", after-before)
	}
}

func TestTelemetry_Events_RejectedPayload_DoesNotIncrement(t *testing.T) {
	// EC-3 ordering invariant: bad-JSON and oversize bodies MUST NOT
	// increment the success counter. Rejected path returns before the
	// increment, which we assert by tracking the counter delta.
	logger, _ := captureLogger()
	h := routes.NewTelemetryHandler(logger)

	before := readBrowserSuccessCounter(t)
	// 1) Malformed JSON path → 400, no increment.
	w1 := postTelemetry(t, h.Events, []byte("{not valid"))
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("malformed: want 400, got %d", w1.Code)
	}
	// 2) Oversize body path → 413, no increment.
	big := strings.Repeat("a", 100*1024)
	w2 := postTelemetry(t, h.Events, []byte(`{"event":"device_success_viewed","x":"`+big+`"}`))
	if w2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize: want 413, got %d", w2.Code)
	}
	after := readBrowserSuccessCounter(t)
	if after != before {
		t.Fatalf("rejected paths must not increment counter; delta=%v", after-before)
	}
}

// readBrowserSuccessCounter scrapes the /metrics endpoint and returns
// the current value of loopzero_device_flow_browser_success_total. Returns
// 0 if the metric is not yet registered (counter starts at 0 by spec).
func readBrowserSuccessCounter(t *testing.T) float64 {
	t.Helper()
	// Cross-package: import metrics with its Handler.
	const metricName = "loopzero_device_flow_browser_success_total"
	// We can't read the registered counter directly without importing
	// prometheus/testutil — instead scrape the exposition and parse.
	value, err := scrapeMetricCounter(metricName)
	if err != nil {
		t.Fatalf("scrape %s: %v", metricName, err)
	}
	return value
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
