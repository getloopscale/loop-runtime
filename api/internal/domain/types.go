// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package domain provides shared domain types used across layers (routes, db, services).
// These types live here to avoid DIP violations where infrastructure (db/) would import
// presentation (routes/).
package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

// --- Domain (Custom Domains) ---

// ErrDomainExists is returned when a domain is already registered (unique constraint violation).
var ErrDomainExists = errors.New("domain already exists")

// GenerateDomainID generates a domain ID in the format "dom-{8 hex chars}".
func GenerateDomainID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "dom-" + hex.EncodeToString(b)
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

// DomainListFilter holds optional filters for listing domains.
type DomainListFilter struct {
	Project     string
	App         string
	Environment string
	Limit       int
	Offset      int
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

// DomainVerifier checks DNS TXT records for domain ownership.
type DomainVerifier interface {
	VerifyTXT(ctx context.Context, domain, expectedToken string) (bool, error)
}

// DomainValidator validates domain names against platform rules.
type DomainValidator interface {
	Validate(domain string) error
}

// CFZoneManager abstracts CF Zone API operations (DIP -- testable without real CF).
type CFZoneManager interface {
	CreateZone(ctx context.Context, domain string) (*CFZoneResult, error)
	ScanDNSRecords(ctx context.Context, zoneID string) error
	ListDNSRecords(ctx context.Context, zoneID string) ([]CFDNSRecord, error)
	CreateDNSRecord(ctx context.Context, zoneID string, record CFDNSRecord) (*CFDNSRecord, error)
	DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error
	DeleteZone(ctx context.Context, zoneID string) error
}

// --- Teams ---

// ErrMemberNotFound is returned when a team member does not exist.
var ErrMemberNotFound = errors.New("member not found")

// TeamMemberInfo represents a team member in API responses.
type TeamMemberInfo struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

// TeamUserInfo represents a platform user (for add-member lookup).
type TeamUserInfo struct {
	ID          string
	Email       string
	DisplayName string
}

// TeamStore abstracts team member persistence (DIP).
type TeamStore interface {
	ListMembers(ctx context.Context, tenantID string) ([]TeamMemberInfo, error)
	AddMember(ctx context.Context, tenantID, userID, role string) error
	RemoveMember(ctx context.Context, tenantID, userID string) error
	UpdateMemberRole(ctx context.Context, tenantID, userID, newRole string) error
	GetMember(ctx context.Context, tenantID, userID string) (*TeamMemberInfo, error)
	GetUserByUsername(ctx context.Context, username string) (*TeamUserInfo, error)
}

// --- Audit ---

// AuditFilter defines query parameters for audit log listing.
type AuditFilter struct {
	Action       string
	ResourceType string
	Since        time.Time
	Limit        int
	Offset       int
}

// AuditEntryResult represents a single audit log entry in API responses.
type AuditEntryResult struct {
	Action        string         `json:"action"`
	ResourceType  string         `json:"resource_type"`
	ResourceID    string         `json:"resource_id"`
	Actor         string         `json:"actor"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"timestamp"`
}

// AuditStore abstracts audit log persistence.
type AuditStore interface {
	ListAuditEntries(ctx context.Context, tenantID string, filter AuditFilter) ([]AuditEntryResult, int, error)
}

// AuditLogger abstracts audit log writing (DIP).
// MEET-2026-03-31-024: Application-level audit for sensitive operations.
type AuditLogger interface {
	LogAudit(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error
}

// --- Secrets ---

// SecretMetadata holds the name and context of a secret -- never its value.
type SecretMetadata struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	App         string `json:"app"`
}

// SecretStore abstracts secret persistence (DIP).
// Implementations MUST NOT expose secret values in list operations.
type SecretStore interface {
	ListNames(ctx context.Context, tenantID, projectID, env string) ([]SecretMetadata, error)
	Set(ctx context.Context, tenantID, projectID, env, app, name string, value []byte) (created bool, err error)
	Delete(ctx context.Context, tenantID, projectID, env, app, name string) (deleted bool, err error)
}

// SecretK8sSync abstracts K8s Secret + ExternalSecret CR creation (DIP).
type SecretK8sSync interface {
	CreateSecret(ctx context.Context, tenantID, projectID, env, app, name, value string) error
	DeleteSecret(ctx context.Context, tenantID, env, app, name string) error
}

// --- Projects ---

// ErrProjectExists is returned when a project with the same name already exists in the tenant.
var ErrProjectExists = errors.New("project already exists")

// CreateProjectInput is the input for creating a project.
type CreateProjectInput struct {
	TenantID  string
	Name      string
	CreatedBy string
}

// ProjectResult is the result of creating or retrieving a project.
type ProjectResult struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TeamID    string    `json:"team"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// ProjectStore abstracts project persistence (DIP).
type ProjectStore interface {
	CreateProject(ctx context.Context, input CreateProjectInput) (ProjectResult, error)
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
	ListByTenant(ctx context.Context, tenantID string) ([]ProjectResult, error)
	DeleteProject(ctx context.Context, tenantID, projectID string) error
}

// TenantCRSpec holds the spec for creating/updating a Tenant CR.
type TenantCRSpec struct {
	Plan string
}

// TenantCRManager abstracts Kubernetes Tenant CR operations (DIP).
type TenantCRManager interface {
	EnsureTenant(ctx context.Context, tenantID string, spec TenantCRSpec) error
}

// ProjectDeleteInput is the input for deleting a project.
type ProjectDeleteInput struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string // empty = all environments
	DeletedBy     string
	CorrelationID string
}

// ProjectDeleteOutput is the result of project deletion.
type ProjectDeleteOutput struct {
	ProjectName         string   `json:"project"`
	DeletedEnvironments []string `json:"deleted_environments"`
	CancelledBuilds     int      `json:"cancelled_builds"`
}

// ProjectDeleter abstracts project deletion orchestration (DIP).
type ProjectDeleter interface {
	Delete(ctx context.Context, req ProjectDeleteInput) (*ProjectDeleteOutput, error)
}

// --- Bootstrap ---

// BootstrapRequest is the JSON body for POST /internal/bootstrap.
type BootstrapRequest struct {
	AdminGitHubID     string `json:"admin_github_id"`
	AdminLogin        string `json:"admin_login"`
	TenantSlug        string `json:"tenant_slug"`
	TenantDisplayName string `json:"tenant_display_name"`
	Plan              string `json:"plan"`
	// CellID is set by the PlacementEngine before persisting.
	// Not part of the JSON body -- injected by the handler.
	// ADR-0008: Phase 0 = "cell-default", Phase 1+ = PlacementEngine assigns.
	CellID string `json:"-"`
}

// BootstrapStore abstracts the database operations for bootstrap.
// Implemented by PostgresBootstrapStore (production) and InMemoryBootstrapStore (tests).
type BootstrapStore interface {
	// Bootstrap creates the admin user, tenant, and membership records.
	// Returns (userID, tenantID, created, error).
	// created is true if any record was actually inserted (not already existing).
	// Must be idempotent -- duplicate calls with the same data must not fail.
	Bootstrap(ctx context.Context, req BootstrapRequest) (userID, tenantID string, created bool, err error)
}

// CellPlacementEngine assigns tenants to Cells.
// ADR-0008: Phase 0 = DefaultEngine (always "cell-default").
type CellPlacementEngine interface {
	AssignCell(ctx context.Context) (cellID string, err error)
	// AssignAndExecute picks a cell AND runs the callback in the same serialized
	// transaction. MEET-053 D-01: fixes TOCTOU between AssignCell and INSERT.
	AssignAndExecute(ctx context.Context, fn func(ctx context.Context, cellID string) error) error
}

// --- Tenant Deletion ---

// DeletionRequest is the input for requesting tenant deletion.
type DeletionRequest struct {
	TenantID    string
	RequestedBy string
	Reason      string // "user_request" | "trial_expired" | "admin_evict"
	GraceUntil  *time.Time
	Force       bool
}

// DeletionRecord represents a pending deletion in the database.
type DeletionRecord struct {
	TenantID     string     `json:"id"`
	RequestedAt  time.Time  `json:"requested_at"`
	RequestedBy  string     `json:"requested_by"`
	Reason       string     `json:"reason"`
	GraceUntil   *time.Time `json:"grace_until,omitempty"`
	Force        bool       `json:"force"`
	AttemptCount int        `json:"attempt_count"`
}

// TombstoneRecord is the permanent audit record for a deleted tenant.
type TombstoneRecord struct {
	TenantID         string
	TenantSlug       string
	RequestedBy      string
	Reason           string
	ResourcesCleaned map[string]interface{}
	ResourcesFailed  map[string]interface{}
}

// TenantDeletionStore provides access to tenant deletion data in PostgreSQL.
type TenantDeletionStore interface {
	// RequestDeletion inserts a pending deletion record. Idempotent: returns existing
	// record if tenant is already pending deletion.
	RequestDeletion(ctx context.Context, input DeletionRequest) (*DeletionRecord, error)

	// CancelDeletion removes the pending deletion record (restore within grace).
	// Returns nil if no pending deletion exists (idempotent).
	CancelDeletion(ctx context.Context, tenantID string) error

	// GetPendingDeletion returns the pending deletion record for a tenant, or nil.
	GetPendingDeletion(ctx context.Context, tenantID string) (*DeletionRecord, error)

	// WriteTombstone writes a permanent audit record after tenant deletion completes.
	WriteTombstone(ctx context.Context, tombstone TombstoneRecord) error
}

// Preview represents a preview environment created for a pull request.
type Preview struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
	PRNumber  int    `json:"pr_number"`
	Branch    string `json:"branch"`
	Status    string `json:"status"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}
