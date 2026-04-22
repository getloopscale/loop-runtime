// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// Delete removes a custom domain (DELETE /api/v1/domains/{domain}).
func (h *DomainHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	// SEC-002 fix: only owners and admins can manage custom domains
	if uc.Role != "owner" && uc.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Only team owners and admins can manage custom domains.",
			"code":  "FORBIDDEN",
		})
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

	// CRIT-02: validate transition via formal state machine
	if !canRemove(d.Status) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "Domain cannot be removed in its current state (" + d.Status + "). Try again later.",
			"code":  "INVALID_STATE",
		})
		return
	}

	// CRIT-01: CAS transition prevents TOCTOU race with concurrent deploy.
	if err := h.store.TransitionStatus(ctx, d.Domain, d.Status, "removing"); err != nil {
		slog.ErrorContext(ctx, "domain transition to removing failed", "error", err)
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "Domain state changed concurrently. Please retry.",
			"code":  "CONCURRENT_MODIFICATION",
		})
		return
	}

	writeJSON(w, http.StatusOK, domainDeleteResponse{
		OK:     true,
		Domain: d.Domain,
		Status: "removing",
	})
}

// --- v2: DNS Record Management ---

// ListDNSRecords returns DNS records for a CF-managed domain (GET /api/v1/domains/{domain}/dns-records).
func (h *DomainHandler) ListDNSRecords(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	domainName := chi.URLParam(r, "domain")
	d, err := h.store.GetByDomainAndTenant(ctx, domainName, uc.TenantID)
	if err != nil || d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "domain not found"})
		return
	}

	if !d.IsCFManaged() || h.cfZone == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "DNS records management is only available for Cloudflare-managed domains.",
			"code":  "NOT_CF_MANAGED",
		})
		return
	}

	records, err := h.cfZone.ListDNSRecords(ctx, *d.CFZoneID)
	if err != nil {
		slog.ErrorContext(ctx, "cf list dns records failed", "domain", domainName, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Failed to fetch DNS records"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"domain":  domainName,
		"records": records,
		"total":   len(records),
	})
}

// AddDNSRecords adds DNS records to a CF-managed domain (POST /api/v1/domains/{domain}/dns-records).
func (h *DomainHandler) AddDNSRecords(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if uc.Role != "owner" && uc.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Only team owners and admins can manage DNS records.", "code": "FORBIDDEN"})
		return
	}

	domainName := chi.URLParam(r, "domain")
	d, err := h.store.GetByDomainAndTenant(ctx, domainName, uc.TenantID)
	if err != nil || d == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "domain not found"})
		return
	}

	if !d.IsCFManaged() || h.cfZone == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "DNS management only available for Cloudflare-managed domains.", "code": "NOT_CF_MANAGED"})
		return
	}

	var req struct {
		Records []CFDNSRecord `json:"records"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	const maxDNSRecordsPerRequest = 50
	if len(req.Records) > maxDNSRecordsPerRequest {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("Too many records (max %d per request).", maxDNSRecordsPerRequest),
			"code":  "TOO_MANY_RECORDS",
		})
		return
	}

	// Validate: reject protected record types
	for _, rec := range req.Records {
		upperType := strings.ToUpper(rec.Type)
		if upperType == "NS" || upperType == "SOA" {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": fmt.Sprintf("Cannot add %s records — these are managed by the platform.", upperType),
				"code":  "PROTECTED_RECORD",
			})
			return
		}
	}

	var created []CFDNSRecord
	for _, rec := range req.Records {
		result, err := h.cfZone.CreateDNSRecord(ctx, *d.CFZoneID, rec)
		if err != nil {
			slog.ErrorContext(ctx, "cf create dns record failed", "domain", domainName, "record", rec.Name, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": fmt.Sprintf("Failed to add record %s %s: %v", rec.Type, rec.Name, err),
			})
			return
		}
		created = append(created, *result)
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"ok":      true,
		"created": len(created),
		"records": created,
	})
}
