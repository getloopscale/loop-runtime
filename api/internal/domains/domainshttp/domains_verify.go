// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package domainshttp

import (
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// Verify checks DNS TXT records for domain ownership (POST /api/v1/domains/{domain}/verify).
// Idempotent: returns 200 for already-verified domains (MEET-033).
// Phase 8.1: Deprecated when CF Zone Management (v2) is active — domains use nameserver delegation.
func (h *DomainHandler) Verify(w http.ResponseWriter, r *http.Request) {
	// v2: TXT verification is deprecated — domains use nameserver delegation
	if h.cfZone != nil {
		httpio.WriteJSON(w, http.StatusGone, map[string]string{
			"code":    "DEPRECATED",
			"message": "TXT verification is deprecated. Domains now use nameserver delegation. Use 'loopscale domain add' to register a new domain.",
		})
		return
	}

	ctx := r.Context()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	domainName := chi.URLParam(r, "domain")

	d, err := h.store.GetByDomainAndTenant(ctx, domainName, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain lookup failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if d == nil {
		httpio.WriteJSON(w, http.StatusNotFound, map[string]string{"code": "INTERNAL_ERROR", "message": "domain not found"})
		return
	}

	// Already verified or past it — return success (idempotent, MEET-033)
	switch statemachine.DomainPhase(d.Status) {
	case statemachine.DPhVerified, statemachine.DPhProvisioning, statemachine.DPhActive:
		httpio.WriteJSON(w, http.StatusOK, domainVerifyResponse{
			Domain:     d.Domain,
			Status:     d.Status,
			VerifiedAt: d.VerifiedAt,
		})
		return
	}

	// Only a phase with a verification transition can be verified; the
	// machine answers which (pending_verification verifies, failed retries).
	next, err := verificationPhase(d.Status)
	if err != nil {
		httpio.WriteJSON(w, http.StatusOK, domainVerifyResponse{
			Domain: d.Domain,
			Status: d.Status,
		})
		return
	}

	// DNS check only — provisioning is async (MEET-033 de-orchestration)
	verified, err := h.verifier.VerifyTXT(ctx, d.Domain, d.VerificationToken)
	if err != nil {
		slog.ErrorContext(ctx, "DNS verification error", "error", err, "domain", d.Domain)
		httpio.WriteJSON(w, http.StatusOK, domainVerifyResponse{
			Domain:            d.Domain,
			Status:            d.Status,
			VerificationError: "DNS lookup failed. Please try again.",
			Hint:              "DNS changes can take up to 48 hours to propagate. Most providers update within 5 minutes.",
		})
		return
	}

	if !verified {
		httpio.WriteJSON(w, http.StatusOK, domainVerifyResponse{
			Domain:            d.Domain,
			Status:            d.Status,
			VerificationError: "TXT record not found for _loopruntime-verify." + d.Domain,
			Hint:              "DNS changes can take up to 48 hours to propagate. Most providers update within 5 minutes.",
		})
		return
	}

	// TXT matches — update status to verified
	now := time.Now().UTC()
	if err := h.store.UpdateVerified(ctx, d.Domain, now); err != nil {
		slog.ErrorContext(ctx, "domain verify update failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "failed to update domain status"})
		return
	}

	httpio.WriteJSON(w, http.StatusOK, domainVerifyResponse{
		Domain:     d.Domain,
		Status:     string(next),
		VerifiedAt: &now,
		Message:    "Provisioning in progress. Check status with: loopscale domain status " + d.Domain,
	})
}
