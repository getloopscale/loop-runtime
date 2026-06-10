// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// T1.3 of deep-review-remediation-plan (closes find_arch_001 partial):
// canonical types live in domain/. Aliases preserve back-compat.
type (
	AttestationInfo  = domain.AttestationInfo
	AttestationStore = domain.AttestationStore
)

// BuildAttestationHandler serves GET /api/v1/builds/:id/attestation.
type BuildAttestationHandler struct {
	store  AttestationStore
	logger *slog.Logger
}

// NewBuildAttestationHandler creates a handler for attestation queries.
func NewBuildAttestationHandler(store AttestationStore, logger *slog.Logger) *BuildAttestationHandler {
	return &BuildAttestationHandler{store: store, logger: logger}
}

// Get returns the supply chain attestation for a build.
func (h *BuildAttestationHandler) Get(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, `{"error":"build_id is required"}`, http.StatusBadRequest)
		return
	}

	info, err := h.store.GetBuildAttestation(r.Context(), buildID)
	if err != nil {
		h.logger.Error("attestation lookup failed",
			"build_id", buildID,
			"error", err,
		)
		http.Error(w, `{"error":"build not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}
