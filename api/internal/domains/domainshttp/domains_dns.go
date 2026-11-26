// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainshttp

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// Delete removes a custom domain (DELETE /api/v1/domains/{domain}).
func (h *DomainHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	domainName := chi.URLParam(r, "domain")

	d, err := h.store.GetByDomainAndTenant(ctx, domainName, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain lookup failed", "error", err)
		// CHG-2026-04-23-AUDIT-13: typed error shape.
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if d == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewDomainNotFound(domainName))
		return
	}

	// CRIT-02: validate transition via formal state machine
	next, err := removalPhase(d.Status)
	if err != nil {
		httpio.WriteJSON(w, http.StatusConflict, map[string]string{
			"error": "Domain cannot be removed in its current state (" + d.Status + "). Try again later.",
			"code":  "INVALID_STATE",
		})
		return
	}

	// CRIT-01: CAS transition prevents TOCTOU race with concurrent deploy.
	if err := h.store.TransitionStatus(ctx, d.Domain, d.Status, string(next)); err != nil {
		slog.ErrorContext(ctx, "domain transition to removing failed", "error", err)
		httpio.WriteJSON(w, http.StatusConflict, map[string]string{
			"code":    "CONCURRENT_MODIFICATION",
			"message": "Domain state changed concurrently. Please retry.",
		})
		return
	}

	httpio.WriteJSON(w, http.StatusOK, domainDeleteResponse{
		OK:     true,
		Domain: d.Domain,
		Status: string(next),
	})
}

// --- v2: DNS records ---

// ListDNSRecords returns DNS records for a CF-managed domain (GET /api/v1/domains/{domain}/dns-records).
func (h *DomainHandler) ListDNSRecords(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	domainName := chi.URLParam(r, "domain")
	d, err := h.store.GetByDomainAndTenant(ctx, domainName, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain lookup failed", "domain", domainName, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if d == nil {
		// CHG-2026-04-23-AUDIT-13: typed error shape.
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewDomainNotFound(domainName))
		return
	}

	if !d.IsCFManaged() || h.cfZone == nil {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "NOT_CF_MANAGED",
			"message": "DNS records management is only available for Cloudflare-managed domains.",
		})
		return
	}

	records, err := h.cfZone.ListDNSRecords(ctx, *d.CFZoneID)
	if err != nil {
		slog.ErrorContext(ctx, "cf list dns records failed", "domain", domainName, "error", err)
		httpio.WriteJSON(w, http.StatusBadGateway, map[string]string{"code": "INTERNAL_ERROR", "message": "Failed to fetch DNS records"})
		return
	}

	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"domain":  domainName,
		"records": records,
		"total":   len(records),
	})
}
