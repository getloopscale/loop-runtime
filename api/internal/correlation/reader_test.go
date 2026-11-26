// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package correlation

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRead_CanonicalHeader: caller sends `Correlation-ID` → returned
// as-is, no WARN logged.
func TestRead_CanonicalHeader(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := httptest.NewRequest(http.MethodGet, "/api/v1/promote", nil)
	r.Header.Set("Correlation-ID", "corr-abc-123")

	got := Read(r)
	if got != "corr-abc-123" {
		t.Errorf("want %q, got %q", "corr-abc-123", got)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("canonical header MUST NOT emit deprecation WARN; log: %s", buf.String())
	}
}

// TestRead_LegacyHeader: caller sends only `X-Correlation-ID` →
// returned as fallback, deprecation WARN emitted.
func TestRead_LegacyHeader(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := httptest.NewRequest(http.MethodGet, "/api/v1/promote", nil)
	r.Header.Set("X-Correlation-ID", "corr-legacy-456")
	r.Header.Set("User-Agent", "loop-runtime-cli/0.4.0")

	got := Read(r)
	if got != "corr-legacy-456" {
		t.Errorf("want %q, got %q", "corr-legacy-456", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, "deprecated") {
		t.Errorf("legacy header MUST emit deprecation WARN; log: %s", logged)
	}
	if !strings.Contains(logged, "loop-runtime-cli/0.4.0") {
		t.Errorf("WARN MUST include User-Agent; log: %s", logged)
	}
}

// TestRead_BothHeaders: canonical wins; legacy is ignored and NO WARN
// fired (the caller already migrated; backward-compat fallback never
// kicked in).
func TestRead_BothHeaders(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := httptest.NewRequest(http.MethodGet, "/api/v1/promote", nil)
	r.Header.Set("Correlation-ID", "corr-NEW")
	r.Header.Set("X-Correlation-ID", "corr-OLD")

	got := Read(r)
	if got != "corr-NEW" {
		t.Errorf("canonical wins; want %q, got %q", "corr-NEW", got)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("canonical-present MUST NOT emit deprecation WARN; log: %s", buf.String())
	}
}

// TestRead_NeitherHeader: returns "" and emits NO WARN.
func TestRead_NeitherHeader(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := httptest.NewRequest(http.MethodGet, "/api/v1/promote", nil)
	got := Read(r)
	if got != "" {
		t.Errorf("want empty, got %q", got)
	}
	if strings.Contains(buf.String(), "deprecated") {
		t.Errorf("no-headers MUST NOT emit deprecation WARN; log: %s", buf.String())
	}
}
