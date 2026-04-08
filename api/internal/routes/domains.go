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
	TLSStatus               *string    `json:"tls_status,omitempty"`
	TLSExpiresAt            *time.Time `json:"tls_expires_at,omitempty"`
	FailureReason           *string    `json:"failure_reason,omitempty"`
	LastCNAMECheck          *time.Time `json:"-"`
	CNAMEConsecutiveFailures int       `json:"-"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`

	// v2: CF Zone Management fields
	CFZoneID           *string  `json:"cf_zone_id,omitempty"`
	CFZoneStatus       string   `json:"cf_zone_status,omitempty"`
	CFNameservers      []string `json:"nameservers,omitempty"`
	DNSRecordsImported bool     `json:"-"`
}

// IsCFManaged returns true if this domain uses CF Zone Management (v2).
func (d *CustomDomain) IsCFManaged() bool {
	return d.CFZoneID != nil && *d.CFZoneID != ""
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

	// v2: CF Zone Management
	CreateWithCFZone(ctx context.Context, d *CustomDomain) error
	UpdateCFZoneStatus(ctx context.Context, domain, cfZoneStatus string) error
	FindPendingNS(ctx context.Context) ([]CustomDomain, error)
	FindNSActive(ctx context.Context) ([]CustomDomain, error)
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

// CFZoneManager abstracts CF Zone API operations (DIP — testable without real CF).
type CFZoneManager interface {
	CreateZone(ctx context.Context, domain string) (*CFZoneResult, error)
	ScanDNSRecords(ctx context.Context, zoneID string) error
	ListDNSRecords(ctx context.Context, zoneID string) ([]CFDNSRecord, error)
	CreateDNSRecord(ctx context.Context, zoneID string, record CFDNSRecord) (*CFDNSRecord, error)
	DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error
	DeleteZone(ctx context.Context, zoneID string) error
}

// CFZoneResult contains the result of creating a CF zone.
type CFZoneResult struct {
	ZoneID      string
	Nameservers []string
}

// CFDNSRecord mirrors cfzone.DNSRecord for DIP.
type CFDNSRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Priority *int   `json:"priority,omitempty"`
	Proxied  *bool  `json:"proxied,omitempty"`
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
				"error": "domain already registered",
				"code":  "DOMAIN_ALREADY_EXISTS",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register domain"})
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

// createWithCFZone implements the v2 domain creation flow:
// 1. Create CF zone for the domain
// 2. Scan existing DNS records (email preservation)
// 3. Add CNAME @ → cname.dev.loopzero.example (proxied, CF flattening)
// 4. Return nameservers for user to configure at registrar
func (h *DomainHandler) createWithCFZone(w http.ResponseWriter, r *http.Request, ctx context.Context, uc *middleware.UserContext, req domainCreateRequest) {
	// Step 1: Create CF zone
	zone, err := h.cfZone.CreateZone(ctx, req.Domain)
	if err != nil {
		slog.ErrorContext(ctx, "cf zone create failed", "domain", req.Domain, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Failed to configure domain. Please try again.",
			"code":  "CF_ZONE_ERROR",
		})
		return
	}

	// Step 2: Scan existing DNS records (email, DKIM, etc.)
	if scanErr := h.cfZone.ScanDNSRecords(ctx, zone.ZoneID); scanErr != nil {
		slog.WarnContext(ctx, "cf dns scan failed (non-fatal)", "domain", req.Domain, "error", scanErr)
	}

	// Step 3: Remove conflicting apex records (A/AAAA/CNAME at root)
	// The DNS scan may import existing A records that conflict with our CNAME.
	if scannedRecords, listErr := h.cfZone.ListDNSRecords(ctx, zone.ZoneID); listErr == nil {
		for _, rec := range scannedRecords {
			// Only remove apex records that conflict with our CNAME
			if (rec.Type == "A" || rec.Type == "AAAA" || rec.Type == "CNAME") &&
				(rec.Name == req.Domain || rec.Name == "@") {
				slog.InfoContext(ctx, "cf removing conflicting apex record", "domain", req.Domain, "type", rec.Type, "content", rec.Content)
				_ = h.cfZone.DeleteDNSRecord(ctx, zone.ZoneID, rec.ID)
			}
		}
	}

	// Step 4: Add routing CNAME (proxied → CF edge → tunnel → Traefik)
	proxied := true
	_, cnameErr := h.cfZone.CreateDNSRecord(ctx, zone.ZoneID, CFDNSRecord{
		Type:    "CNAME",
		Name:    req.Domain,
		Content: h.cnameTarget,
		TTL:     1, // auto (required when proxied)
		Proxied: &proxied,
	})
	if cnameErr != nil {
		slog.ErrorContext(ctx, "cf cname create failed", "domain", req.Domain, "error", cnameErr)
		// Rollback: delete the zone
		_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Failed to configure domain routing. Please try again.",
			"code":  "CF_ZONE_ERROR",
		})
		return
	}

	// Step 5: List imported records, deduplicate (CF scan sometimes imports duplicates with different quoting)
	var dnsRecords []CFDNSRecord
	var recordCount int
	if records, listErr := h.cfZone.ListDNSRecords(ctx, zone.ZoneID); listErr == nil {
		seen := make(map[string]bool)
		for _, rec := range records {
			// Normalize: strip surrounding quotes for dedup key
			content := strings.Trim(rec.Content, "\"")
			key := rec.Type + ":" + rec.Name + ":" + content
			if seen[key] {
				// Duplicate — delete from CF zone
				slog.InfoContext(ctx, "cf removing duplicate record", "domain", req.Domain, "type", rec.Type, "name", rec.Name)
				_ = h.cfZone.DeleteDNSRecord(ctx, zone.ZoneID, rec.ID)
				continue
			}
			seen[key] = true
			dnsRecords = append(dnsRecords, rec)
		}
		recordCount = len(dnsRecords)
	}

	// Step 6: Save to DB
	zoneID := zone.ZoneID
	domain := &CustomDomain{
		ID:                 GenerateDomainID(),
		TenantID:           uc.TenantID,
		ProjectName:        req.Project,
		AppName:            req.App,
		Environment:        req.Environment,
		Domain:             req.Domain,
		Status:             "pending_ns",
		VerificationToken:  "", // not needed for v2
		CFZoneID:           &zoneID,
		CFZoneStatus:       "pending",
		CFNameservers:      zone.Nameservers,
		DNSRecordsImported: recordCount > 1, // >1 because CNAME always exists
	}

	if err := h.store.CreateWithCFZone(ctx, domain); err != nil {
		if errors.Is(err, ErrDomainExists) {
			// Rollback zone
			_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "domain already registered",
				"code":  "DOMAIN_ALREADY_EXISTS",
			})
			return
		}
		slog.ErrorContext(ctx, "domain create failed", "error", err)
		_ = h.cfZone.DeleteZone(ctx, zone.ZoneID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register domain"})
		return
	}

	metrics.CustomDomainsTotal.WithLabelValues(uc.TenantID, "pending_ns").Inc()

	writeJSON(w, http.StatusCreated, domainCreateV2Response{
		OK:                true,
		ID:                domain.ID,
		Domain:            domain.Domain,
		Project:           domain.ProjectName,
		App:               domain.AppName,
		Environment:       domain.Environment,
		Status:            domain.Status,
		Nameservers:       zone.Nameservers,
		DNSRecordsImported: recordCount,
		DNSRecords:        dnsRecords,
		ActionRequired:    "Change your nameservers at your DNS provider to activate this domain.",
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

// --- v2: DNS Record Management ---

// ListDNSRecords returns DNS records for a CF-managed domain (GET /api/v1/domains/{domain}/dns-records).
func (h *DomainHandler) ListDNSRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
	ctx := r.Context()
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

func strPtr(s string) *string { return &s }
