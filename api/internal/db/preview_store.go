// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/id"
)

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
// Distinct from domain.Preview to keep DB types (time.Time) and JSON wire
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

// ToDomain converts the DB record to a domain.Preview suitable for API
// responses. Times are serialized as RFC3339; nil pointers become empty.
func (p *PreviewRecord) ToDomain() *domain.Preview {
	preview := &domain.Preview{
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
type PreviewStore interface {
	// API queries (reads).
	ListByTenant(ctx context.Context, tenantID string) ([]domain.Preview, error)
	ListByProject(ctx context.Context, tenantID, projectID string) ([]domain.Preview, error)
	GetByID(ctx context.Context, tenantID, previewID string) (*domain.Preview, error)
	GetByPR(ctx context.Context, tenantID, projectID string, prNumber int) (*domain.Preview, error)

	// PreviewService writes.
	Create(ctx context.Context, tx DBTX, rec *PreviewRecord) error
	UpdateStatus(ctx context.Context, tx DBTX, previewID, status string) error
	UpdateBuild(ctx context.Context, tx DBTX, previewID, buildID, commitSHA string) error
	UpdateURL(ctx context.Context, tx DBTX, previewID, url string) error
	RequestDeletion(ctx context.Context, tenantID, previewID string) error
}

// PostgresPreviewStore implements PreviewStore using PostgreSQL.
//
// Like other stores in this package, it accepts an ExecQueryExecutor for
// writes (typically a transaction) and a QueryExecutor for reads (typically
// the pool). Methods that take an explicit `tx DBTX` argument run inside
// that transaction; otherwise they fall back to s.Exec / s.Query.
type PostgresPreviewStore struct {
	Exec  ExecQueryExecutor
	Query QueryExecutor
}

// NewPostgresPreviewStore creates a PostgresPreviewStore.
func NewPostgresPreviewStore(exec ExecQueryExecutor, query QueryExecutor) *PostgresPreviewStore {
	return &PostgresPreviewStore{Exec: exec, Query: query}
}

// previewSelectColumns is the canonical projection used by all reads.
const previewSelectColumns = `
id, tenant_id, project_id, pr_number, branch, commit_sha, status,
url, build_id, release_id, repo_full_name, installation_id,
expires_at, cell_id, created_at, updated_at, deleted_at`

// scanPreviewRow scans a single row from a SELECT using previewSelectColumns.
func scanPreviewRow(scan func(dest ...any) error) (*PreviewRecord, error) {
	var rec PreviewRecord
	err := scan(
		&rec.ID,
		&rec.TenantID,
		&rec.ProjectID,
		&rec.PRNumber,
		&rec.Branch,
		&rec.CommitSHA,
		&rec.Status,
		&rec.URL,
		&rec.BuildID,
		&rec.ReleaseID,
		&rec.RepoFullName,
		&rec.InstallationID,
		&rec.ExpiresAt,
		&rec.CellID,
		&rec.CreatedAt,
		&rec.UpdatedAt,
		&rec.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// Create inserts a preview row using UPSERT semantics so a `synchronize`
// webhook on a PR with an existing live preview reuses the same row.
//
// Conflict target: (tenant_id, project_id, pr_number) WHERE deleted_at IS NULL
// (see migration 000003). On conflict, status is reset to "creating",
// build_id cleared, and commit_sha replaced with the new commit.
func (s *PostgresPreviewStore) Create(ctx context.Context, tx DBTX, rec *PreviewRecord) error {
	if rec.ID == "" {
		rec.ID = id.GeneratePreviewID()
	}
	if rec.Status == "" {
		rec.Status = PreviewStatusCreating
	}
	if rec.ExpiresAt.IsZero() {
		// Default TTL aligns with plan (72h).
		rec.ExpiresAt = time.Now().UTC().Add(72 * time.Hour)
	}

	executor := s.exec(tx)

	const stmt = `
INSERT INTO preview_environments (
	id, tenant_id, project_id, pr_number, branch, commit_sha, status,
	url, build_id, release_id, repo_full_name, installation_id, expires_at
) VALUES (
	$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (tenant_id, project_id, pr_number) WHERE deleted_at IS NULL DO UPDATE SET
	branch     = EXCLUDED.branch,
	commit_sha = EXCLUDED.commit_sha,
	status     = 'creating',
	build_id   = NULL,
	release_id = NULL,
	url        = EXCLUDED.url,
	repo_full_name  = EXCLUDED.repo_full_name,
	installation_id = EXCLUDED.installation_id,
	expires_at = EXCLUDED.expires_at,
	updated_at = now()
RETURNING id, status, created_at, updated_at, expires_at`

	row := executor.(DBTX).QueryRow(ctx, stmt,
		rec.ID, rec.TenantID, rec.ProjectID, rec.PRNumber, rec.Branch,
		rec.CommitSHA, rec.Status, rec.URL, rec.BuildID, rec.ReleaseID,
		rec.RepoFullName, rec.InstallationID, rec.ExpiresAt,
	)

	if err := row.Scan(&rec.ID, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt, &rec.ExpiresAt); err != nil {
		return fmt.Errorf("preview store: create failed: %w", err)
	}
	return nil
}

// UpdateStatus sets status. Caller is responsible for state-machine validity
// (this is a thin DB-level setter).
func (s *PostgresPreviewStore) UpdateStatus(ctx context.Context, tx DBTX, previewID, status string) error {
	executor := s.exec(tx)
	res, err := executor.Exec(ctx,
		`UPDATE preview_environments SET status = $1 WHERE id = $2 AND deleted_at IS NULL`,
		status, previewID,
	)
	if err != nil {
		return fmt.Errorf("preview store: update status failed: %w", err)
	}
	if res.RowsAffected == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// UpdateBuild associates a build (and optionally a refreshed commit_sha) with
// a preview. Used after the API submits a build for the preview.
func (s *PostgresPreviewStore) UpdateBuild(ctx context.Context, tx DBTX, previewID, buildID, commitSHA string) error {
	executor := s.exec(tx)
	const stmt = `
UPDATE preview_environments
   SET build_id = $1,
       commit_sha = COALESCE(NULLIF($2, ''), commit_sha)
 WHERE id = $3 AND deleted_at IS NULL`
	res, err := executor.Exec(ctx, stmt, buildID, commitSHA, previewID)
	if err != nil {
		return fmt.Errorf("preview store: update build failed: %w", err)
	}
	if res.RowsAffected == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// UpdateURL sets the public URL exposed to the user.
func (s *PostgresPreviewStore) UpdateURL(ctx context.Context, tx DBTX, previewID, url string) error {
	executor := s.exec(tx)
	res, err := executor.Exec(ctx,
		`UPDATE preview_environments SET url = $1 WHERE id = $2 AND deleted_at IS NULL`,
		url, previewID,
	)
	if err != nil {
		return fmt.Errorf("preview store: update url failed: %w", err)
	}
	if res.RowsAffected == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// RequestDeletion soft-deletes a preview by setting status='deleting' and
// deleted_at. The row remains visible for audit/history but is excluded
// from active uniqueness checks (see migration 000003 partial unique index).
//
// tenantID scopes the operation to prevent cross-tenant deletions even
// when callers bypass RLS.
func (s *PostgresPreviewStore) RequestDeletion(ctx context.Context, tenantID, previewID string) error {
	res, err := s.Exec.Exec(ctx,
		`UPDATE preview_environments
		    SET status = 'deleting',
		        deleted_at = now()
		  WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`,
		previewID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("preview store: request deletion failed: %w", err)
	}
	if res.RowsAffected == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// ListByTenant returns all live previews for a tenant.
func (s *PostgresPreviewStore) ListByTenant(ctx context.Context, tenantID string) ([]domain.Preview, error) {
	stmt := `SELECT ` + previewSelectColumns + `
FROM preview_environments
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC`
	return s.queryList(ctx, stmt, tenantID)
}

// ListByProject returns all live previews for a (tenant, project) pair.
func (s *PostgresPreviewStore) ListByProject(ctx context.Context, tenantID, projectID string) ([]domain.Preview, error) {
	stmt := `SELECT ` + previewSelectColumns + `
FROM preview_environments
WHERE tenant_id = $1 AND project_id = $2 AND deleted_at IS NULL
ORDER BY created_at DESC`
	return s.queryList(ctx, stmt, tenantID, projectID)
}

// GetByID returns a preview by its ID, scoped to a tenant.
// Returns ErrPreviewNotFound when no live row matches.
func (s *PostgresPreviewStore) GetByID(ctx context.Context, tenantID, previewID string) (*domain.Preview, error) {
	stmt := `SELECT ` + previewSelectColumns + `
FROM preview_environments
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`
	rows, err := s.Query.Query(ctx, stmt, previewID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("preview store: get by id failed: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if iterErr := rows.Err(); iterErr != nil {
			return nil, fmt.Errorf("preview store: rows iteration failed: %w", iterErr)
		}
		return nil, loopzeroerrors.ErrPreviewNotFound
	}
	rec, err := scanPreviewRow(rows.Scan)
	if err != nil {
		return nil, fmt.Errorf("preview store: scan failed: %w", err)
	}
	return rec.ToDomain(), nil
}

// GetByPR returns the live preview for a (tenant, project, pr_number) tuple.
// Returns ErrPreviewNotFound when no live row matches.
func (s *PostgresPreviewStore) GetByPR(ctx context.Context, tenantID, projectID string, prNumber int) (*domain.Preview, error) {
	stmt := `SELECT ` + previewSelectColumns + `
FROM preview_environments
WHERE tenant_id = $1 AND project_id = $2 AND pr_number = $3 AND deleted_at IS NULL`
	rows, err := s.Query.Query(ctx, stmt, tenantID, projectID, prNumber)
	if err != nil {
		return nil, fmt.Errorf("preview store: get by PR failed: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if iterErr := rows.Err(); iterErr != nil {
			return nil, fmt.Errorf("preview store: rows iteration failed: %w", iterErr)
		}
		return nil, loopzeroerrors.ErrPreviewNotFound
	}
	rec, err := scanPreviewRow(rows.Scan)
	if err != nil {
		return nil, fmt.Errorf("preview store: scan failed: %w", err)
	}
	return rec.ToDomain(), nil
}

// queryList executes a SELECT and returns the resulting domain.Preview slice.
func (s *PostgresPreviewStore) queryList(ctx context.Context, stmt string, args ...any) ([]domain.Preview, error) {
	rows, err := s.Query.Query(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("preview store: list query failed: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Preview, 0)
	for rows.Next() {
		rec, scanErr := scanPreviewRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("preview store: scan failed: %w", scanErr)
		}
		out = append(out, *rec.ToDomain())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("preview store: rows iteration failed: %w", err)
	}
	return out, nil
}

// exec returns the executor to use for a write — the explicit transaction
// when provided, otherwise the store's default executor.
func (s *PostgresPreviewStore) exec(tx DBTX) ExecQueryExecutor {
	if tx != nil {
		return tx
	}
	return s.Exec
}

// Compile-time interface assertion.
var _ PreviewStore = (*PostgresPreviewStore)(nil)

// Sentinel sanity check — keeps the linter happy if we later introduce
// helpers that need to recognize ErrNoRows from the underlying driver.
var _ = errors.Is
