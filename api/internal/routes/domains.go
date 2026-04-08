// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// ErrDomainExists is returned when a domain is already registered (unique constraint violation).
var ErrDomainExists = errors.New("domain already exists")

// GenerateDomainID generates a domain ID in the format "dom-{8 hex chars}".
func GenerateDomainID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "dom-" + hex.EncodeToString(b)
}

// generateVerificationToken generates a TXT verification value.
func generateVerificationToken() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return "loopzero-verify=" + hex.EncodeToString(b)
}

// CustomDomain represents a custom domain record.
type CustomDomain struct {
	ID                      string     `json:"id"`
	TenantID                string     `json:"-"`
	ProjectName             string     `json:"project"`
	AppName                 string     `json:"app"`
	Environment             string     `json:"environment"`
	Domain                  string     `json:"domain"`
	Status                  string     `json:"status"`
	VerificationToken       string     `json:"-"`
	VerifiedAt              *time.Time `json:"verified_at,omitempty"`
	TLSStatus               string     `json:"tls_status,omitempty"`
	TLSExpiresAt            *time.Time `json:"tls_expires_at,omitempty"`
	FailureReason           string     `json:"failure_reason,omitempty"`
	LastCNAMECheck          *time.Time `json:"-"`
	CNAMEConsecutiveFailures int       `json:"-"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// DomainStore abstracts custom domain persistence (DIP).
type DomainStore interface {
	Create(ctx context.Context, d *CustomDomain) error
	GetByDomainAndTenant(ctx context.Context, domain, tenantID string) (*CustomDomain, error)
	ListByTenant(ctx context.Context, tenantID string, filter DomainListFilter) ([]CustomDomain, int, error)
	CountByTenant(ctx context.Context, tenantID string) (int, error)
	UpdateStatus(ctx context.Context, domain, status string) error
	TransitionStatus(ctx context.Context, domain, expectedStatus, newStatus string) error
	UpdateVerified(ctx context.Context, domain string, verifiedAt time.Time) error
	UpdateTLS(ctx context.Context, domain, tlsStatus string, expiresAt *time.Time) error
	UpdateFailure(ctx context.Context, domain, status, reason string) error
	Delete(ctx context.Context, domain string) error
}

// DomainListFilter holds optional filters for listing domains.
type DomainListFilter struct {
	Project     string
	App         string
	Environment string
	Limit       int
	Offset      int
}

// DomainVerifier checks DNS TXT records for domain ownership.
type DomainVerifier interface {
	VerifyTXT(ctx context.Context, domain, expectedToken string) (bool, error)
}

// DomainValidator validates domain names against platform rules.
type DomainValidator interface {
	Validate(domain string) error
}

// domainMachine is the formal domain state machine (CRIT-02 fix).
// Replaces ad-hoc map with pkg/statemachine convention.
var domainMachine = statemachine.NewDomainMachine()

// canRemove checks if a domain can transition to "removing" from its current state.
// Uses the formal state machine — does not panic on invalid transitions.
func canRemove(currentStatus string) bool {
	ok := true
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	result := domainMachine.Transition(statemachine.State(currentStatus), statemachine.DomainRemove)
	return ok && result == statemachine.DomainRemoving
}

// DomainHandler provides HTTP handlers for custom domain operations.
type DomainHandler struct {
	store     DomainStore
	verifier  DomainVerifier
	validator DomainValidator
	cnameTarget string
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
	Verification domainVerificationInfo    `json:"verification"`
	CNAMETarget  string                    `json:"cname_target"`
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
	CNAMETarget  string                  `json:"cname_target"`
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

// Create registers a new custom domain (POST /api/v1/domains).
func (h *DomainHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	var req domainCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
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

	// Validate domain format
	if err := h.validator.Validate(req.Domain); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// SEC-007 fix: enforce per-tenant domain quota (hard cap prevents squatting DoS).
	const maxDomainsPerTenant = 25
	count, err := h.store.CountByTenant(ctx, uc.TenantID)
	if err != nil {
		slog.ErrorContext(ctx, "domain count failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if count >= maxDomainsPerTenant {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Domain limit reached. You can register up to " + fmt.Sprintf("%d", maxDomainsPerTenant) + " custom domains.",
			"code":  "DOMAIN_LIMIT_REACHED",
		})
		return
	}

	// Generate verification token and attempt atomic insert.
	// CODE-001 fix: no check-then-insert TOCTOU. The unique index on domain
	// column handles conflict detection atomically. ErrDomainExists → 409.
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
		TLSStatus:         "pending",
	}

	if err := h.store.Create(ctx, domain); err != nil {
		if errors.Is(err, ErrDomainExists) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "domain already registered",
				"code":  "DOMAIN_ALREADY_EXISTS",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register domain"})
		return
	}

	// F-004 fix: emit metric on domain creation
	metrics.CustomDomainsTotal.WithLabelValues(uc.TenantID, "pending_verification").Inc()

	writeJSON(w, http.StatusCreated, domainCreateResponse{
		OK:          true,
		ID:          domain.ID,
		Domain:      domain.Domain,
		Project:     domain.ProjectName,
		App:         domain.AppName,
		Environment: domain.Environment,
		Status:      domain.Status,
		Verification: domainVerificationInfo{
			TXTName:  "_loopzero-verify." + domain.Domain,
			TXTValue: token,
		},
		CNAMETarget: h.cnameTarget,
	})
}

// Verify checks DNS TXT records for domain ownership (POST /api/v1/domains/{domain}/verify).
// Idempotent: returns 200 for already-verified domains (MEET-033).
func (h *DomainHandler) Verify(w http.ResponseWriter, r *http.Request) {
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

// Get returns details for a specific domain (GET /api/v1/domains/{domain}).
func (h *DomainHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	if d.TLSStatus != "" && d.TLSStatus != "pending" {
		resp.TLS = &domainTLSInfo{
			Status:    d.TLSStatus,
			ExpiresAt: d.TLSExpiresAt,
		}
	}

	// FIND-004/SEC-009 fix: only expose verification token to owner/admin.
	// Members can see domain status but not the raw TXT value.
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
	ctx := r.Context()
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, domainListResponse{
		Domains: domains,
		Total:   total,
		Limit:   filter.Limit,
		Offset:  filter.Offset,
	})
}

// Delete removes a custom domain (DELETE /api/v1/domains/{domain}).
func (h *DomainHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
