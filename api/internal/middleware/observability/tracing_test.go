// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package observability

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/getloopscale/loop-runtime/pkg/logging"
)

// The tracing doc promises that every request log carries trace_id. The
// request-scoped logger handlers actually use is the one RequestLogger puts in
// the context, so the promise holds only if that logger is enriched from the
// active span.
func TestRequestLogs_CarryTheTraceIDOfTheRequestSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = tp.Shutdown(t.Context()) })

	var out bytes.Buffer
	cl := logging.NewContextLogger(slog.New(slog.NewJSONHandler(&out, nil)), TraceIDs)
	handler := OtelHTTP(RequestLogger(cl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logging.LoggerFromContext(r.Context()).Info("handled")
	})))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if !strings.Contains(out.String(), `"trace_id":"`) {
		t.Errorf("request log line has no trace_id: %s", out.String())
	}
}

func TestTraceIDs_EmptyWithoutASpan(t *testing.T) {
	traceID, spanID := TraceIDs(t.Context())
	if traceID != "" || spanID != "" {
		t.Errorf("TraceIDs() = (%q, %q), want empty outside a span", traceID, spanID)
	}
}
