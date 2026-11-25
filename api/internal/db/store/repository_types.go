// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// repository_types.go — public types for the repositories domain
// (project ↔ GitHub repo linkage + GitHub App installation
// lifecycle) that survive the T2.1 (Batch A #3) deletion of the
// legacy `repository_store.go`. The 1 prod call site
// (api/cmd/setup_github.go:56) uses the struct-literal
// `&db.PostgresRepositoryStore{DB: dbAdapter}` shape — preserved
// here.

import (
	"github.com/getloopscale/loopzero/api/internal/ports"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// ErrRepositoryNotLinked is returned by UnlinkRepository when no
// link row exists for the (tenant, project) pair.
// CHG-2026-04-23-AUDIT-21 — mirrors the sentinel pattern in
// tenancy.ErrEnvironmentNotFound (AUDIT-12), replacing the silent-success
// behaviour where DELETE returned nil regardless of rows affected.
var ErrRepositoryNotLinked = ports.ErrRepositoryNotLinked

// ProjectRepository links a LoopZero project to a GitHub repository.
// Distinct from generated.ProjectRepository (sqlc-emitted row type
// with pgtype-based timestamp fields). Wrapper converts between
// them.
type ProjectRepository struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ProjectName    string    `json:"project_name"`
	Provider       string    `json:"provider"`
	RepoFullName   string    `json:"repo_full_name"`
	RepoID         *int64    `json:"repo_id,omitempty"`
	InstallationID int64     `json:"installation_id"`
	DefaultBranch  string    `json:"default_branch"`
	AutoDeploy     bool      `json:"auto_deploy"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// GitHubInstallation represents a GitHub App installation. The
// generated row type is named GithubInstallation (sqlc casing) — we
// keep the legacy CamelCase here so the public symbol stays
// readable. Wrapper converts between the two.
type GitHubInstallation struct {
	ID           int64     `json:"id"`
	AccountLogin string    `json:"account_login"`
	AccountType  string    `json:"account_type"`
	TenantID     *string   `json:"tenant_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// VerifiedAt is when GitHub confirmed that a user of the tenant can access
	// the installation, and VerifiedBy that user. Nil on a row the installation
	// webhook created, and on a binding older than the confirmation.
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	VerifiedBy *string    `json:"verified_by,omitempty"`
}

// VerifiedFor reports whether the installation is bound to tenantID with the
// binding confirmed on GitHub. It is the one rule that lets a tenant use an
// installation.
func (i *GitHubInstallation) VerifiedFor(tenantID string) bool {
	return i != nil && tenantID != "" && i.TenantID != nil && *i.TenantID == tenantID && i.VerifiedAt != nil
}

// PostgresRepositoryStore implements repository persistence (project
// ↔ GitHub repo linkage + installation lifecycle).
//
// T2.1 Batch A #3 (plan 2026-05-29-sqlc-migration v1.3): DB field
// type preserved (db.DBTX — same as legacy); methods
// (repository_wrapper.go) dispatch through SqlcBridge to sqlc-
// generated queries.
type PostgresRepositoryStore struct {
	DB db.DBTX
}
