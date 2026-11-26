// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// =============================================================================
// metricsWriter internal tests (package-level access)
// =============================================================================

func TestWriter_DefaultStatus200(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec, status: http.StatusOK}
	if mw.status != http.StatusOK {
		t.Errorf("expected default 200, got %d", mw.status)
	}
}

func TestWriter_WriteHeader_CapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec, status: http.StatusOK}
	mw.WriteHeader(http.StatusConflict)
	if mw.status != http.StatusConflict {
		t.Errorf("expected 409, got %d", mw.status)
	}
}

func TestWriter_Flush_NonFlusher(t *testing.T) {
	mw := &metricsWriter{ResponseWriter: &minimalWriter{}, status: http.StatusOK}
	// Should not panic when underlying writer doesn't implement http.Flusher
	mw.Flush()
}

func TestWriter_Flush_Flusher(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec, status: http.StatusOK}
	mw.Flush()
}

func TestWriter_Unwrap_ReturnsUnderlying(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec, status: http.StatusOK}
	if mw.Unwrap() != rec {
		t.Error("Unwrap should return the underlying ResponseWriter")
	}
}

// minimalWriter implements http.ResponseWriter but NOT http.Flusher.
type minimalWriter struct{}

func (w *minimalWriter) Header() http.Header         { return http.Header{} }
func (w *minimalWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *minimalWriter) WriteHeader(_ int)           {}
