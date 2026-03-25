// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"net/http"
)

// DBHealthChecker is an interface for checking database connectivity.
// This follows DIP — the handler depends on an abstraction, not a concrete DB connection.
type DBHealthChecker interface {
	Ping() error
}

// HealthHandler provides HTTP handlers for health checks.
type HealthHandler struct {
	db DBHealthChecker
}

// NewHealthHandler creates a new HealthHandler.
// The db parameter can be nil for basic health checks (GET /health).
func NewHealthHandler(db DBHealthChecker) *HealthHandler {
	return &HealthHandler{db: db}
}

// Health responds with a simple 200 OK.
// GET /api/v1/health — lightweight liveness probe.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// DeepHealth checks all dependencies (database) and responds accordingly.
// GET /api/v1/health/deep — readiness probe.
func (h *HealthHandler) DeepHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.db == nil || h.db.Ping() != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "degraded",
			"database": "disconnected",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"database": "connected",
	})
}
