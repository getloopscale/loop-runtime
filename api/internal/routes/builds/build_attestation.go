// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// T1.3 of deep-review-remediation-plan (closes find_arch_001 partial):
// canonical types live in domain/. Aliases preserve back-compat.
type (
	AttestationInfo  = ports.AttestationInfo
	AttestationStore = ports.AttestationStore
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
//
// Tenant isolation: the tenant comes from the authenticated request, and a
// build owned by another tenant answers 404 exactly like a build that does not
// exist (ADR-0014). A store failure is a 500, never a 404 — reporting an outage
// as "not found" tells the caller their build vanished.
func (h *BuildAttestationHandler) Get(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, `{"error":"build_id is required"}`, http.StatusBadRequest)
		return
	}

	uc := auth.GetUserContext(r.Context())
	if uc == nil || uc.TenantID == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	info, err := h.store.GetBuildAttestationForTenant(r.Context(), uc.TenantID, buildID)
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
			return
		}
		h.logger.Error("attestation lookup failed",
			"build_id", buildID,
			"tenant_id", uc.TenantID,
			"error", err,
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, info)
}
