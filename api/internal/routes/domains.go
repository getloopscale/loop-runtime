// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// ErrDomainExists is an alias for domain.ErrDomainExists (backward compatibility).
var ErrDomainExists = domain.ErrDomainExists

// GenerateDomainID is an alias for domain.GenerateDomainID (backward compatibility).
var GenerateDomainID = domain.GenerateDomainID

// generateVerificationToken generates a TXT verification value.
func generateVerificationToken() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return "loopzero-verify=" + hex.EncodeToString(b)
}

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	CustomDomain    = domain.CustomDomain
	DomainListFilter = domain.DomainListFilter
	CFZoneResult    = domain.CFZoneResult
	CFDNSRecord     = domain.CFDNSRecord
	DomainStore     = domain.DomainStore
	DomainVerifier  = domain.DomainVerifier
	DomainValidator = domain.DomainValidator
	CFZoneManager   = domain.CFZoneManager
)

// domainMachine is the formal domain state machine (CRIT-02 fix).
// Replaces ad-hoc map with pkg/statemachine convention.
var domainMachine = statemachine.NewDomainTypedMachine()

// canRemove checks if a domain can transition to "removing" from its current state.
// Uses the formal state machine — does not panic on invalid transitions.
func canRemove(currentStatus string) bool {
	ok := true
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	result := domainMachine.Transition(statemachine.DomainPhase(currentStatus), statemachine.DPEventRemove)
	return ok && result == statemachine.DPhRemoving
}

// DomainHandler provides HTTP handlers for custom domain operations.
type DomainHandler struct {
	store       DomainStore
	verifier    DomainVerifier
	validator   DomainValidator
	cnameTarget string
	cfZone      CFZoneManager // nil = v1 mode (TXT verification), non-nil = v2 (CF Zone)
}

// NewDomainHandler creates a new DomainHandler.
func NewDomainHandler(store DomainStore, verifier DomainVerifier, validator DomainValidator, cnameTarget string) *DomainHandler {
	return &DomainHandler{
		store:       store,
		verifier:    verifier,
		validator:   validator,
		cnameTarget: cnameTarget,
	}
}

// WithCFZone enables CF Zone Management (v2) for this handler.
func (h *DomainHandler) WithCFZone(cfZone CFZoneManager) *DomainHandler {
	h.cfZone = cfZone
	return h
}

// --- Request/Response types ---

type domainCreateRequest struct {
	Domain      string `json:"domain"`
	Project     string `json:"project"`
	App         string `json:"app"`
	Environment string `json:"environment"`
}

type domainCreateResponse struct {
	OK           bool                      `json:"ok"`
	ID           string                    `json:"id"`
	Domain       string                    `json:"domain"`
	Project      string                    `json:"project"`
	App          string                    `json:"app"`
	Environment  string                    `json:"environment"`
	Status       string                    `json:"status"`
	Verification *domainVerificationInfo   `json:"verification,omitempty"`
	CNAMETarget  string                    `json:"cname_target,omitempty"`
}

// domainCreateV2Response is returned when CF Zone Management (v2) is active.
type domainCreateV2Response struct {
	OK                 bool         `json:"ok"`
	ID                 string       `json:"id"`
	Domain             string       `json:"domain"`
	Project            string       `json:"project"`
	App                string       `json:"app"`
	Environment        string       `json:"environment"`
	Status             string       `json:"status"`
	Nameservers        []string     `json:"nameservers"`
	DNSRecordsImported int          `json:"dns_records_imported"`
	DNSRecords         []CFDNSRecord `json:"dns_records,omitempty"`
	ActionRequired     string       `json:"action_required"`
}

type domainVerificationInfo struct {
	TXTName  string `json:"txt_name"`
	TXTValue string `json:"txt_value"`
}

type domainVerifyResponse struct {
	Domain            string     `json:"domain"`
	Status            string     `json:"status"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
	VerificationError string     `json:"verification_error,omitempty"`
	Hint              string     `json:"hint,omitempty"`
	Message           string     `json:"message,omitempty"`
}

type domainStatusResponse struct {
	ID           string                  `json:"id"`
	Domain       string                  `json:"domain"`
	Project      string                  `json:"project"`
	App          string                  `json:"app"`
	Environment  string                  `json:"environment"`
	Status       string                  `json:"status"`
	TLS          *domainTLSInfo          `json:"tls,omitempty"`
	Verification *domainVerificationInfo `json:"verification,omitempty"`
	CNAMETarget  string                  `json:"cname_target,omitempty"`
	Nameservers  []string                `json:"nameservers,omitempty"`  // v2: CF nameservers
	CFManaged    bool                    `json:"cf_managed,omitempty"`   // v2: true if CF zone
	CreatedAt    time.Time               `json:"created_at"`
	VerifiedAt   *time.Time              `json:"verified_at,omitempty"`
}

type domainTLSInfo struct {
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type domainListResponse struct {
	Domains []CustomDomain `json:"domains"`
	Total   int            `json:"total"`
	Limit   int            `json:"limit"`
	Offset  int            `json:"offset"`
}

type domainDeleteResponse struct {
	OK     bool   `json:"ok"`
	Domain string `json:"domain"`
	Status string `json:"status"`
}

// --- Handlers ---

// Get returns details for a specific domain (GET /api/v1/domains/{domain}).
func (h *DomainHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	domainName := chi.URLParam(r, "domain")

	d, err := h.store.GetByDomainAndTenant(ctx, domainName, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain lookup failed", "error", err)
		// CHG-2026-04-23-AUDIT-13: typed error shape for contract uniformity.
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if d == nil {
		writeLoopZeroError(w, loopzeroerrors.NewDomainNotFound(domainName))
		return
	}

	resp := domainStatusResponse{
		ID:          d.ID,
		Domain:      d.Domain,
		Project:     d.ProjectName,
		App:         d.AppName,
		Environment: d.Environment,
		Status:      d.Status,
		CNAMETarget: h.cnameTarget,
		CreatedAt:   d.CreatedAt,
		VerifiedAt:  d.VerifiedAt,
	}

	if d.TLSStatus != nil && *d.TLSStatus != "" && *d.TLSStatus != "pending" {
		resp.TLS = &domainTLSInfo{
			Status:    *d.TLSStatus,
			ExpiresAt: d.TLSExpiresAt,
		}
	}

	// v2: include CF zone info
	if d.IsCFManaged() {
		resp.CFManaged = true
		resp.Nameservers = d.CFNameservers
	}

	// v1: expose verification token to owner/admin for pending domains
	if d.Status == "pending_verification" && (uc.Role == "owner" || uc.Role == "admin") {
		resp.Verification = &domainVerificationInfo{
			TXTName:  "_loopzero-verify." + d.Domain,
			TXTValue: d.VerificationToken,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// List returns all domains for the current tenant (GET /api/v1/domains).
func (h *DomainHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	filter := DomainListFilter{
		Project:     r.URL.Query().Get("project"),
		App:         r.URL.Query().Get("app"),
		Environment: r.URL.Query().Get("environment"),
		Limit:       limit,
		Offset:      offset,
	}

	domains, total, err := h.store.ListByTenant(ctx, uc.TenantID, filter)
	if err != nil {
		slog.ErrorContext(ctx, "domain list failed", "error", err)
		// CHG-2026-04-23-AUDIT-13: typed error shape.
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, domainListResponse{
		Domains: domains,
		Total:   total,
		Limit:   filter.Limit,
		Offset:  filter.Offset,
	})
}

func strPtr(s string) *string { return &s }
