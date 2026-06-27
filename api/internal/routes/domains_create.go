// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Create registers a new custom domain (POST /api/v1/domains).
func (h *DomainHandler) Create(w http.ResponseWriter, r *http.Request) {
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
			"code":    "FORBIDDEN",
			"message": "Only team owners and admins can manage custom domains.",
		})
		return
	}

	var req domainCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INTERNAL_ERROR", "message": "invalid request body"})
		return
	}

	// HIGH-01 fix: normalize domain before validation and storage
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))

	// Validate required fields
	if req.Domain == "" || req.Project == "" || req.App == "" || req.Environment == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "domain, project, app, and environment are required",
		})
		return
	}

	// Validate domain format.
	// Red Team loopzero-cloud#103 closure: was `code: INTERNAL_ERROR` which
	// connotes a server failure; this is INPUT validation, so emit the
	// client-classified `INVALID_DOMAIN` code (mapped to 400 in
	// pkg/errors/http/mapping.go).
	if err := h.validator.Validate(req.Domain); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_DOMAIN", "message": err.Error()})
		return
	}

	// SEC-007 fix: enforce per-tenant domain quota (hard cap prevents squatting DoS).
	const maxDomainsPerTenant = 25
	count, err := h.store.CountByTenant(ctx, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain count failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if count >= maxDomainsPerTenant {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Domain limit reached. You can register up to " + fmt.Sprintf("%d", maxDomainsPerTenant) + " custom domains.",
			"code":  "DOMAIN_LIMIT_REACHED",
		})
		return
	}

	// v2 (CF Zone Management): create zone + scan DNS + set CNAME
	if h.cfZone != nil {
		h.createWithCFZone(w, r, ctx, uc, req)
		return
	}

	// v1 (TXT verification): generate token for manual DNS verification
	token := generateVerificationToken()

	domain := &CustomDomain{
		ID:                GenerateDomainID(),
		TenantID:          uc.TenantID,
		ProjectName:       req.Project,
		AppName:           req.App,
		Environment:       req.Environment,
		Domain:            req.Domain,
		Status:            "pending_verification",
		VerificationToken: token,
		TLSStatus:         strPtr("pending"),
	}

	if err := h.store.Create(ctx, domain); err != nil {
		if errors.Is(err, ErrDomainExists) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":    "DOMAIN_ALREADY_EXISTS",
				"message": "domain already registered",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "failed to register domain"})
		return
	}

	metrics.CustomDomainsTotal.WithLabelValues(uc.TenantID, "pending_verification").Inc()

	writeJSON(w, http.StatusCreated, domainCreateResponse{
		OK:          true,
		ID:          domain.ID,
		Domain:      domain.Domain,
		Project:     domain.ProjectName,
		App:         domain.AppName,
		Environment: domain.Environment,
		Status:      domain.Status,
		Verification: &domainVerificationInfo{
			TXTName:  "_loopzero-verify." + domain.Domain,
			TXTValue: token,
		},
		CNAMETarget: h.cnameTarget,
	})
}
