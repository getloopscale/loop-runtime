// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package ports provides shared domain types used across layers (routes, db, services).
// These types live here to avoid DIP violations where infrastructure (db/) would import
// presentation (routes/).
package ports

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// --- Domain (Custom Domains) ---

// ErrDomainExists is returned when a domain is already registered (unique constraint violation).
var ErrDomainExists = errors.New("domain already exists")

// ErrEnvironmentExists is returned by EnvironmentManager.AddEnvironment when
// the (project, environment) tuple is already present. Callers that only want
// the environment to exist (the first-deploy ensurer) treat it as success; the
// create route answers 409.
var ErrEnvironmentExists = errors.New("environment already exists")

// ErrEnvironmentNotFound is returned by EnvironmentManager.RemoveEnvironment
// when the (project, environment) tuple is not present (AUDIT-12).
var ErrEnvironmentNotFound = errors.New("environment not found")

// ErrRepositoryNotLinked is returned by RepositoryStore.UnlinkRepository when
// no repository is linked to the (tenant, project) pair (AUDIT-21).
var ErrRepositoryNotLinked = errors.New("no repository linked to project")

// GenerateDomainID generates a domain ID in the format "dom-{8 hex chars}".
func GenerateDomainID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "dom-" + hex.EncodeToString(b)
}

// CustomDomain represents a custom domain record.
type CustomDomain struct {
	ID                       string     `json:"id"`
	TenantID                 string     `json:"-"`
	ProjectName              string     `json:"project"`
	AppName                  string     `json:"app"`
	Environment              string     `json:"environment"`
	Domain                   string     `json:"domain"`
	Status                   string     `json:"status"`
	VerificationToken        string     `json:"-"`
	VerifiedAt               *time.Time `json:"verified_at,omitempty"`
	TLSStatus                *string    `json:"tls_status,omitempty"`
	TLSExpiresAt             *time.Time `json:"tls_expires_at,omitempty"`
	FailureReason            *string    `json:"failure_reason,omitempty"`
	LastCNAMECheck           *time.Time `json:"-"`
	CNAMEConsecutiveFailures int        `json:"-"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`

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

// DomainStore is the custom-domain persistence the HTTP request path needs:
// create, read, list, count, and the two writes a user action makes (a
// compare-and-set status move and marking a domain verified). The
// background provisioner and health checker declare their own ports in
// services/domain for the lifecycle writes (TLS, failure, CF zone status,
// deletion, the NS sweeps); carrying those here forced every request-path
// consumer and fake to implement methods it never calls.
type DomainStore interface {
	Create(ctx context.Context, d *CustomDomain) error
	CreateWithCFZone(ctx context.Context, d *CustomDomain) error
	GetByDomainAndTenant(ctx context.Context, domain, tenantID string) (*CustomDomain, error)
	ListByTenant(ctx context.Context, tenantID string, filter DomainListFilter) ([]CustomDomain, int, error)
	CountByTenant(ctx context.Context, tenantID string) (int, error)
	TransitionStatus(ctx context.Context, domain, expectedStatus, newStatus string) error
	UpdateVerified(ctx context.Context, domain string, verifiedAt time.Time) error
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
	// GetUsernameByID returns the user's display-friendly username for the given
	// opaque user_id. AUDIT-10: lets `loopzero whoami` show a meaningful identity
	// even when the token has no email claim (OAuth2 client_credentials).
	// Returns empty string if user not found.
	GetUsernameByID(ctx context.Context, userID string) (string, error)
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
// projectID is the canonical M2 namespace segment (per pkg/naming.Namespace);
// the runtime ExternalSecret CR lives at loopzero-{tenantID}-{projectID}-{env}.
type SecretK8sSync interface {
	CreateSecret(ctx context.Context, tenantID, projectID, env, app, name, value string) error
	DeleteSecret(ctx context.Context, tenantID, projectID, env, app, name string) error
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

// ProjectSummary is the dashboard-facing per-project summary record
// returned by GET /api/v1/projects post plan
// 2026-06-08-dashboard-screens-review-fixes T2.1 (ADR D2). Wire-format
// additive over ProjectResult — legacy consumers ignore the 2 new
// trailing fields. LastDeployStatus is the 5-value enum from
// pkg/plan.ProjectStatus (kept as string here to avoid a domain
// → projectstatus import edge; the wrapper layer is the SoT mapper).
type ProjectSummary struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	TeamID           string    `json:"team"`
	CreatedBy        string    `json:"created_by,omitempty"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
	EnvironmentCount int64     `json:"environment_count"`
	// Red Team loopzero-cloud#41 absorption (2026-06-27): subset of
	// EnvironmentCount that includes ONLY env_targets with kind='permanent'
	// (production-like envs). Distinguishes preview-only projects
	// (production_env_count == 0) from those wired to production.
	// Dashboard renders this as the canonical "Live envs" count;
	// EnvironmentCount stays for backwards compat as the all-kinds total.
	ProductionEnvCount int64 `json:"production_env_count"`
	// ActiveEnvCount is the count of env_targets that have a currently
	// serving ACTIVE release (preview OR permanent kind). This is what
	// the dashboard "LIVE ENVS" counter SHOULD reflect — a project with
	// a preview env in the ACTIVE state + Live URL serving 200 must
	// count as 1, not 0. Red Team loopzero-cloud#134 absorption (2026-07-02).
	//
	// Semantic distinction:
	//   - EnvironmentCount   — all env_targets (any kind, any release state)
	//   - ProductionEnvCount — kind='permanent' subset (any release state)
	//   - ActiveEnvCount     — any kind WITH corresponding release.status='ACTIVE'
	//
	// Legacy `production_env_count` stays for the `isPreviewOnly`
	// heuristic (dashboard `project-detail.tsx`) which asks a different
	// question: "was any PERMANENT env provisioned?" — not "how many
	// envs are currently live?". The two counters answer different
	// questions on the same page; conflating them was the #134 bug.
	ActiveEnvCount int64 `json:"active_env_count"`
	// LastDeployStatus is one of: "live", "building", "failed", "idle",
	// "queued" — derived from the latest build's status via
	// api/internal/projectstatus.MapBuildStatusToProjectStatus.
	LastDeployStatus string `json:"last_deploy_status"`
	// LastDeployAt + AppsCount + LatestFramework — T5.3 LIVE-bug fix 2026-06-08.
	// Plan 2026-06-08-dashboard-screens-review-fixes T2.1 originally dropped
	// legacy `last_deploy` timestamp + `apps[]` array + `framework` fields
	// from the listing response, leaving the dashboard /projects table
	// rendering "—" / "0" in those columns despite the data being available
	// via the latest_build LATERAL join. Sponsor walkthrough caught the
	// regression; these fields are the additive completion of T2.1.
	//
	// LastDeployAt is the latest build's created_at timestamp.
	//
	// 2026-06-21 follow-up (#118, qa-dogfood-cleanup-2026-06-21 T3.1):
	// switched from value-type `time.Time` to pointer `*time.Time` because
	// `json:"…,omitempty"` does NOT consider the zero-value of `time.Time`
	// as empty — the wire emitted `"0001-01-01T00:00:00Z"` which JS sees
	// as truthy, so `??` and `?:` guards on the dashboard never fired
	// (rendered "2,027 years ago" on Overview / project-detail hero).
	// Pointer + omitempty makes the field absent on the wire when the
	// project has no builds yet, so JS `??` works canonically.
	LastDeployAt *time.Time `json:"last_deploy,omitempty"`
	// AppsCount = jsonb_array_length(latest_build.apps_spec); 0 when no
	// builds OR empty apps_spec.
	AppsCount int64 `json:"apps_count"`
	// LatestFramework = latest_build.apps_spec[0].type — closest semantic
	// to "framework" (the dashboard renders this as a Badge). Empty string
	// when no builds OR apps_spec missing.
	LatestFramework string `json:"latest_framework,omitempty"`
	// LatestBuildEnvironment = latest_build.environment — feeds the
	// dashboard's Red Team loopzero-cloud#41 disambiguation: when this is
	// "preview" AND there is no active production release, the project
	// is preview-only and the badge MUST show "Preview" (not "Live" with
	// a "URL PENDING" hint). Red Team 2026-06-27 caught the divergence
	// where a project with 12 successful preview builds AND zero
	// production releases rendered Live across 4 surfaces. Empty string
	// when no builds exist.
	LatestBuildEnvironment string `json:"latest_build_environment,omitempty"`
	// LastReplicasReadyValue + LastReplicasReadyAt — T3.2 of plan
	// dashboard-badge-data-plane-poller closing Red Team loopzero-cloud#128
	// (badge lies "Live" when control-plane ACTIVE + data-plane 502).
	// The DataPlanePoller Runnable in loopzero-app-operator scrapes
	// kube_deployment_status_replicas_ready per (tenant,project,env)
	// tuple and UPDATEs environment_targets on each tick. Surfaced here
	// via the ListProjectsWithSummaryByTenant LATERAL join for the
	// permanent (production-flavor) env. Nil means no snapshot yet —
	// the dashboard maps NULL to the honest "Unknown" state per EC-7
	// no-flicker headroom, NOT to Live-or-Down. Pointer + `omitempty`
	// forces key absence on the wire (value-type time.Time with omitempty
	// silently serializes `"0001-01-01T00:00:00Z"` — the same trap #118
	// fixed for LastDeployAt).
	LastReplicasReadyValue *int       `json:"last_replicas_ready_value,omitempty"`
	LastReplicasReadyAt    *time.Time `json:"last_replicas_ready_at,omitempty"`
}

// ProjectStore abstracts project persistence (DIP).
type ProjectStore interface {
	CreateProject(ctx context.Context, input CreateProjectInput) (ProjectResult, error)
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
	ListByTenant(ctx context.Context, tenantID string) ([]ProjectResult, error)
	// ListByTenantWithSummary returns the same set as ListByTenant but
	// carries 2 additional fields (environment_count + last_deploy_status)
	// derived via a single SQL round-trip (LEFT JOIN LATERAL). Post
	// plan 2026-06-08-dashboard-screens-review-fixes T2.1.
	ListByTenantWithSummary(ctx context.Context, tenantID string) ([]ProjectSummary, error)
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
	// ADR-0008: Phase 0 = "cell-0", Phase 1+ = PlacementEngine assigns.
	CellID string `json:"-"`
}

// BootstrapStore abstracts the database operations for bootstrap.
// Implemented by PostgresBootstrapStore (production) and bootstraptest.InMemoryStore (tests).
type BootstrapStore interface {
	// Bootstrap creates the admin user, tenant, and membership records.
	// Returns (userID, tenantID, created, error).
	// created is true if any record was actually inserted (not already existing).
	// Must be idempotent -- duplicate calls with the same data must not fail.
	Bootstrap(ctx context.Context, req BootstrapRequest) (userID, tenantID string, created bool, err error)
}

// CellPlacementEngine assigns tenants to Cells.
// ADR-0008: Phase 0 = DefaultEngine (always "cell-0").
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
//
// JSON field naming aligns with the CLI's PreviewInspectApiResponse and
// PreviewListItem schemas. The `tenant_id` field is omitted from the JSON
// output (Rule 3 — internal terms) by being unexported via `json:"-"` for
// the wire format; the Go struct keeps it for store routing.
type Preview struct {
	ID           string `json:"id"`
	TenantID     string `json:"-"`
	ProjectID    string `json:"project_id"`
	PRNumber     int    `json:"pr_number"`
	Branch       string `json:"branch"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	Status       string `json:"status"`
	URL          string `json:"url,omitempty"`
	BuildID      string `json:"build_id,omitempty"`
	ReleaseID    string `json:"release_id,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	TTLExpiresAt string `json:"ttl_expires_at,omitempty"`
}

// SSEEvent represents a single event in the SSE event journal.
type SSEEvent struct {
	ID        int64           `json:"id"`
	BuildID   string          `json:"build_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// EventStore defines the contract for SSE event persistence.
// Supports append, replay (via Last-Event-ID), and cleanup.
type EventStore interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
	GetAfter(ctx context.Context, buildID string, afterID int64) ([]SSEEvent, error)
	// GetAfterForProject returns sse_events for ALL builds owned by
	// (tenantID, projectName) whose id > afterID. Plan J-17 T2.3 — backs
	// the project-level stream that powers the realtime deployments list.
	// Implementations MUST enforce tenant ownership in the SQL itself
	// (defense-in-depth — handler also checks, but the store closes the
	// vulnerability window between auth and query).
	GetAfterForProject(ctx context.Context, tenantID string, projectName string, afterID int64) ([]SSEEvent, error)
	Prune(ctx context.Context, olderThan time.Duration) (int64, error)
}
