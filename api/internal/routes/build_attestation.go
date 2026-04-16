// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AttestationInfo describes the supply chain attestation for a build's releases.
type AttestationInfo struct {
	BuildID        string `json:"build_id"`
	ImageDigest    string `json:"image_digest,omitempty"`
	ImageSignature string `json:"image_signature,omitempty"`
	SignedAt       string `json:"signed_at,omitempty"`
	Signed         bool   `json:"signed"`
}

// AttestationStore is the minimal contract for reading attestation data.
type AttestationStore interface {
	GetBuildAttestation(ctx context.Context, buildID string) (*AttestationInfo, error)
}

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
