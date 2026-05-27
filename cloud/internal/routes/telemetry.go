// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// TelemetryHandler accepts beacon-style POSTs from the dashboard and emits
// them as structured slog records. The minimal-viable implementation logs
// to STDOUT only — future versions may ingest into Loki or a dedicated
// pipeline (D6 of plan 2026-05-27-backend-frontend-integration).
//
// Four endpoints are served via a single handler factory keyed by category:
//   POST /v1/telemetry/errors        — uncaught window errors
//   POST /v1/telemetry/web-vitals    — CLS / LCP / INP web-vitals
//   POST /v1/telemetry/events        — business events (deploy_clicked, …)
//   POST /v1/telemetry/interactions  — UI interaction counters
//
// All four are PUBLIC (no JWT) — sendBeacon cannot set custom headers.
// DOS protection: body ≤ 64 KiB (EC-03). Rate-limiting accepted as
// out-of-scope v1 (EC-11 documented) — sendBeacon volume per user is
// low and the body cap already blocks the OOM vector.
type TelemetryHandler struct {
	logger *slog.Logger
}

// NewTelemetryHandler wires the handler with a structured logger.
// `logger` may be nil — defaults to slog.Default() so the package never
// panics when wiring is incomplete (e.g., early in main()).
func NewTelemetryHandler(logger *slog.Logger) *TelemetryHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TelemetryHandler{logger: logger}
}

// telemetryMaxBodyBytes caps every telemetry POST body. 64 KiB is the
// effective ceiling of the browser sendBeacon API on most engines —
// payloads larger than this are rejected by the browser before they
// reach the network, so accepting more would be wasted.
//
// EC-03 of plan v1.1 — guards against a hostile / buggy client posting
// multi-MB blobs and exhausting the cloud-service heap.
const telemetryMaxBodyBytes = 64 * 1024

// makeHandler returns an http.HandlerFunc that decodes a JSON payload
// (capped at 64 KiB) and emits a single structured log record with
// `category=<eventCategory>`. Always responds 202 Accepted — telemetry
// is fire-and-forget on the wire; an error from the logger is a server
// problem the client cannot help with.
func (h *TelemetryHandler) makeHandler(eventCategory string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, telemetryMaxBodyBytes)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE",
					"Telemetry body must be ≤64 KiB.")
				return
			}
			writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD",
				"Telemetry body must be valid JSON.")
			return
		}
		h.logger.Info("telemetry",
			"category", eventCategory,
			"payload", payload,
		)
		w.WriteHeader(http.StatusAccepted)
	}
}

// Errors handles POST /v1/telemetry/errors — uncaught window/runtime errors.
func (h *TelemetryHandler) Errors(w http.ResponseWriter, r *http.Request) {
	h.makeHandler("errors")(w, r)
}

// WebVitals handles POST /v1/telemetry/web-vitals — CLS / LCP / INP samples.
func (h *TelemetryHandler) WebVitals(w http.ResponseWriter, r *http.Request) {
	h.makeHandler("web-vitals")(w, r)
}

// Events handles POST /v1/telemetry/events — business / product events.
func (h *TelemetryHandler) Events(w http.ResponseWriter, r *http.Request) {
	h.makeHandler("events")(w, r)
}

// Interactions handles POST /v1/telemetry/interactions — UI interaction counters.
func (h *TelemetryHandler) Interactions(w http.ResponseWriter, r *http.Request) {
	h.makeHandler("interactions")(w, r)
}
