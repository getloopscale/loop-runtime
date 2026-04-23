// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// Verify checks DNS TXT records for domain ownership (POST /api/v1/domains/{domain}/verify).
// Idempotent: returns 200 for already-verified domains (MEET-033).
// Phase 8.1: Deprecated when CF Zone Management (v2) is active — domains use nameserver delegation.
func (h *DomainHandler) Verify(w http.ResponseWriter, r *http.Request) {
	// v2: TXT verification is deprecated — domains use nameserver delegation
	if h.cfZone != nil {
		writeJSON(w, http.StatusGone, map[string]string{
			"code":    "DEPRECATED",
			"message": "TXT verification is deprecated. Domains now use nameserver delegation. Use 'loopzero domain add' to register a new domain.",
		})
		return
	}

	ctx := r.Context()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	domainName := chi.URLParam(r, "domain")

	d, err := h.store.GetByDomainAndTenant(ctx, domainName, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain lookup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "domain not found"})
		return
	}

	// Already verified or active — return success (idempotent, MEET-033)
	if d.Status == "verified" || d.Status == "provisioning" || d.Status == "active" {
		writeJSON(w, http.StatusOK, domainVerifyResponse{
			Domain:     d.Domain,
			Status:     d.Status,
			VerifiedAt: d.VerifiedAt,
		})
		return
	}

	// Only pending_verification and failed can be verified
	if d.Status != "pending_verification" && d.Status != "failed" {
		writeJSON(w, http.StatusOK, domainVerifyResponse{
			Domain: d.Domain,
			Status: d.Status,
		})
		return
	}

	// DNS check only — provisioning is async (MEET-033 de-orchestration)
	verified, err := h.verifier.VerifyTXT(ctx, d.Domain, d.VerificationToken)
	if err != nil {
		slog.ErrorContext(ctx, "DNS verification error", "error", err, "domain", d.Domain)
		writeJSON(w, http.StatusOK, domainVerifyResponse{
			Domain:            d.Domain,
			Status:            d.Status,
			VerificationError: "DNS lookup failed. Please try again.",
			Hint:              "DNS changes can take up to 48 hours to propagate. Most providers update within 5 minutes.",
		})
		return
	}

	if !verified {
		writeJSON(w, http.StatusOK, domainVerifyResponse{
			Domain:            d.Domain,
			Status:            "pending_verification",
			VerificationError: "TXT record not found for _loopzero-verify." + d.Domain,
			Hint:              "DNS changes can take up to 48 hours to propagate. Most providers update within 5 minutes.",
		})
		return
	}

	// TXT matches — update status to verified
	now := time.Now().UTC()
	if err := h.store.UpdateVerified(ctx, d.Domain, now); err != nil {
		slog.ErrorContext(ctx, "domain verify update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update domain status"})
		return
	}

	writeJSON(w, http.StatusOK, domainVerifyResponse{
		Domain:     d.Domain,
		Status:     "verified",
		VerifiedAt: &now,
		Message:    "Provisioning in progress. Check status with: loopzero domain status " + d.Domain,
	})
}
