// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package domainshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getloopscale/loopzero/pkg/statemachine"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	auditctx "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Create registers a new custom domain (POST /api/v1/domains).
func (h *DomainHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req domainCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{"code": "INTERNAL_ERROR", "message": "invalid request body"})
		return
	}

	// HIGH-01 fix: normalize domain before validation and storage
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))

	// Validate required fields
	if req.Domain == "" || req.Project == "" || req.App == "" || req.Environment == "" {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "domain, project, app, and environment are required",
		})
		return
	}

	// B-059: declare the resource id for the audit row (was body-snooped
	// from the `project` key).
	auditctx.SetResourceID(ctx, req.Project)

	// Validate domain format.
	// Red Team loopzero-cloud#103 closure: was `code: INTERNAL_ERROR` which
	// connotes a server failure; this is INPUT validation, so emit the
	// client-classified `INVALID_DOMAIN` code (mapped to 400 in
	// pkg/errors/http/mapping.go).
	if err := h.validator.Validate(req.Domain); err != nil {
		httpio.WriteJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_DOMAIN", "message": err.Error()})
		return
	}

	// SEC-007 fix: enforce per-tenant domain quota (hard cap prevents squatting DoS).
	const maxDomainsPerTenant = 25
	count, err := h.store.CountByTenant(ctx, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain count failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if count >= maxDomainsPerTenant {
		httpio.WriteJSON(w, http.StatusForbidden, map[string]string{
			"error": "Domain limit reached. You can register up to " + fmt.Sprintf("%d", maxDomainsPerTenant) + " custom domains.",
			"code":  "DOMAIN_LIMIT_REACHED",
		})
		return
	}

	if !h.allowedByPlan(ctx, w, uc.TenantID, req) {
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
		ID:                ports.GenerateDomainID(),
		TenantID:          uc.TenantID,
		ProjectName:       req.Project,
		AppName:           req.App,
		Environment:       req.Environment,
		Domain:            req.Domain,
		Status:            string(statemachine.DPhPendingVerification),
		VerificationToken: token,
		TLSStatus:         strPtr("pending"),
	}

	if err := h.store.Create(ctx, domain); err != nil {
		if errors.Is(err, ports.ErrDomainExists) {
			httpio.WriteJSON(w, http.StatusConflict, map[string]string{
				"code":    "DOMAIN_ALREADY_EXISTS",
				"message": "domain already registered",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "failed to register domain"})
		return
	}

	metrics.CustomDomainsTotal.WithLabelValues(uc.TenantID, string(statemachine.DPhPendingVerification)).Inc()

	httpio.WriteJSON(w, http.StatusCreated, domainCreateResponse{
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
