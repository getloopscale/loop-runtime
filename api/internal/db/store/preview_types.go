// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// preview_types.go — public structs for the preview environments
// lifecycle store that survives the T2.2 (Batch B #1) deletion of the
// legacy preview_store.go. The 2 prod call sites use the constructor
// `&db.PostgresPreviewStore{DB: dbAdapter}` (via NewPostgresPreviewStore)
// — preserved here.
//
// PreviewRecord is the DB row representation that keeps time.Time +
// *string nullable pointers separated from the wire-level
// ports.Preview (string timestamps, value strings). The conversion
// happens in ToDomain.

// previewStatus values mirror the state machine in pkg/statemachine/preview.go,
// stored lowercase in PostgreSQL.
const (
	PreviewStatusCreating      = "creating"
	PreviewStatusActive        = "active"
	PreviewStatusExpiring      = "expiring"
	PreviewStatusDeleting      = "deleting"
	PreviewStatusDeleted       = "deleted"
	PreviewStatusFailed        = "failed"
	PreviewStatusCleanupFailed = "cleanup_failed"
)

// PreviewRecord is the database row representation of a preview environment.
// Distinct from ports.Preview to keep DB types (time.Time) and JSON wire
// types (string) separate.
type PreviewRecord struct {
	ID             string
	TenantID       string
	ProjectID      string
	PRNumber       int
	Branch         string
	CommitSHA      string
	Status         string
	URL            *string
	BuildID        *string
	ReleaseID      *string
	RepoFullName   *string
	InstallationID *int64
	ExpiresAt      time.Time
	CellID         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// ToDomain converts the DB record to a ports.Preview suitable for API
// responses. Times are serialized as RFC3339; nil pointers become empty.
func (p *PreviewRecord) ToDomain() *ports.Preview {
	preview := &ports.Preview{
		ID:           p.ID,
		TenantID:     p.TenantID,
		ProjectID:    p.ProjectID,
		PRNumber:     p.PRNumber,
		Branch:       p.Branch,
		CommitSHA:    p.CommitSHA,
		Status:       p.Status,
		CreatedAt:    p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    p.UpdatedAt.UTC().Format(time.RFC3339),
		TTLExpiresAt: p.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if p.URL != nil {
		preview.URL = *p.URL
	}
	if p.BuildID != nil {
		preview.BuildID = *p.BuildID
	}
	if p.ReleaseID != nil {
		preview.ReleaseID = *p.ReleaseID
	}
	if p.RepoFullName != nil {
		preview.RepoFullName = *p.RepoFullName
	}
	return preview
}

// PreviewStore defines persistence operations for preview environments.
//
// API read paths are satisfied by ListByTenant/ListByProject/GetByID.
// Write paths (Create, UpdateStatus, etc.) are used by PreviewService and
// run inside transactions provided by withEnvironmentLock when serializing
// against concurrent webhook events for the same PR.
// Co-located with the adapter in db/store/ per ADR 2026-07-03-m9-db-store-
// subpackage-topology D2 (negligible external port footprint; not a smell).
type PreviewStore interface {
	// API queries (reads).
	ListByTenant(ctx context.Context, tenantID string) ([]ports.Preview, error)
	ListByProject(ctx context.Context, tenantID, projectID string) ([]ports.Preview, error)
	GetByID(ctx context.Context, tenantID, previewID string) (*ports.Preview, error)
	GetByPR(ctx context.Context, tenantID, projectID string, prNumber int) (*ports.Preview, error)

	// PreviewService writes.
	Create(ctx context.Context, tx db.DBTX, rec *PreviewRecord) error
	UpdateStatus(ctx context.Context, tx db.DBTX, previewID, status string) error
	UpdateBuild(ctx context.Context, tx db.DBTX, previewID, buildID, commitSHA string) error
	UpdateURL(ctx context.Context, tx db.DBTX, previewID, url string) error
	RequestDeletion(ctx context.Context, tenantID, previewID string) error
}

// PostgresPreviewStore implements PreviewStore via sqlc-backed dispatch
// through SqlcBridge.
//
// T2.2 Batch B #1 (plan 2026-05-29-sqlc-migration v1.3): legacy
// 2-executor field signature (Exec ExecQueryExecutor + Query
// QueryExecutor) replaced by single DBTX field; per-method tx
// override preserved via the same `tx DBTX` argument shape. Constructor
// signature widened to accept any combination of executors (kept for
// call-site compat) but both are stored as a single DBTX.
type PostgresPreviewStore struct {
	DB db.DBTX
}

// NewPostgresPreviewStore preserves the 2-arg call-site signature
// used by api/cmd/setup_github.go:184 + api/cmd/setup_server.go:267.
// Both `exec` and `query` come from the same dbAdapter in practice,
// so we accept the second and ignore it for the DBTX field.
func NewPostgresPreviewStore(exec db.ExecQueryExecutor, query db.QueryExecutor) *PostgresPreviewStore {
	if dbtx, ok := exec.(db.DBTX); ok {
		return &PostgresPreviewStore{DB: dbtx}
	}
	// Fall back to query if it's a DBTX (some call sites swap the
	// args). If neither implements DBTX the wrapper bridge() will
	// surface a typed error at runtime per fail-fast.
	if dbtx, ok := query.(db.DBTX); ok {
		return &PostgresPreviewStore{DB: dbtx}
	}
	return &PostgresPreviewStore{}
}
