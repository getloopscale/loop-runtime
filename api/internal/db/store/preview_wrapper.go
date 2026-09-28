// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// preview_wrapper.go — sqlc-backed implementation of
// PostgresPreviewStore. T2.2 Batch B #1 of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// All 11 PreviewStore methods (Create UPSERT, UpdateStatus,
// UpdateBuild, UpdateURL, RequestDeletion soft-delete + 4 read paths
// + GetByPR) dispatch through SqlcBridge to sqlc-generated queries in
// generated/previews.sql.go.
//
// Invariants preserved:
//   - Create UPSERT on partial unique index (tenant_id, project_id,
//     pr_number) WHERE deleted_at IS NULL → status reset to 'creating',
//     build_id/release_id cleared, branch/commit_sha/url/expires_at
//     refreshed
//   - UpdateStatus/UpdateBuild/UpdateURL/RequestDeletion 0 rows-
//     affected → ErrPreviewNotFound
//   - GetByID/GetByPR pgx.ErrNoRows → ErrPreviewNotFound
//   - RequestDeletion soft-delete contract (status='deleting',
//     deleted_at=now) — tenantID scopes the UPDATE to prevent
//     cross-tenant deletes when callers bypass RLS
//   - Default TTL=72h applied in Create when ExpiresAt is zero
//   - Default ID generation via id.GeneratePreviewID() when ID is
//     empty

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/id"
)

// =========================================================================
// Writes (PreviewService transactional path)
// =========================================================================

// Create inserts a preview row using UPSERT semantics. On the partial
// unique index (tenant_id, project_id, pr_number) WHERE deleted_at IS
// NULL, the existing live row is reused with a refreshed branch,
// commit_sha, status='creating', cleared build_id + release_id.
//
// Caller-supplied tx (when non-nil) is used; otherwise s.DB.
//
// Mutates rec.ID + rec.Status + rec.CreatedAt + rec.UpdatedAt +
// rec.ExpiresAt to reflect the post-upsert values, matching the
// legacy contract callers rely on.
func (s *PostgresPreviewStore) Create(ctx context.Context, tx db.DBTX, rec *PreviewRecord) error {
	if rec.ID == "" {
		rec.ID = id.GeneratePreviewID()
	}
	if rec.Status == "" {
		rec.Status = PreviewStatusCreating
	}
	if rec.ExpiresAt.IsZero() {
		// Default TTL aligns with the preview plan (72h).
		rec.ExpiresAt = time.Now().UTC().Add(72 * time.Hour)
	}

	bridge := s.txBridge(tx)
	q := generated.New()
	out, err := q.InsertPreview(ctx, bridge, generated.InsertPreviewParams{
		ID:             rec.ID,
		TenantID:       rec.TenantID,
		ProjectID:      rec.ProjectID,
		PrNumber:       int32(rec.PRNumber),
		Branch:         rec.Branch,
		CommitSha:      rec.CommitSHA,
		Status:         rec.Status,
		Url:            rec.URL,
		BuildID:        rec.BuildID,
		ReleaseID:      rec.ReleaseID,
		RepoFullName:   rec.RepoFullName,
		InstallationID: rec.InstallationID,
		ExpiresAt:      pgtype.Timestamptz{Time: rec.ExpiresAt, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("preview store: create failed: %w", err)
	}
	rec.ID = out.ID
	rec.Status = out.Status
	if out.CreatedAt.Valid {
		rec.CreatedAt = out.CreatedAt.Time
	}
	if out.UpdatedAt.Valid {
		rec.UpdatedAt = out.UpdatedAt.Time
	}
	if out.ExpiresAt.Valid {
		rec.ExpiresAt = out.ExpiresAt.Time
	}
	return nil
}

// UpdateStatus sets status. Caller is responsible for state-machine
// validity (this is a thin DB-level setter). 0 rows-affected →
// ErrPreviewNotFound.
func (s *PostgresPreviewStore) UpdateStatus(ctx context.Context, tx db.DBTX, previewID, status string) error {
	bridge := s.txBridge(tx)
	q := generated.New()
	n, err := q.UpdatePreviewStatus(ctx, bridge, generated.UpdatePreviewStatusParams{
		Status: status,
		ID:     previewID,
	})
	if err != nil {
		return fmt.Errorf("preview store: update status failed: %w", err)
	}
	if n == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// UpdateBuild associates a build (and optionally a refreshed commit_sha).
// Empty commitSHA preserves the existing value via COALESCE(NULLIF($2,
// ”), commit_sha) in the SQL query.
func (s *PostgresPreviewStore) UpdateBuild(ctx context.Context, tx db.DBTX, previewID, buildID, commitSHA string) error {
	bridge := s.txBridge(tx)
	q := generated.New()
	// BuildID/CommitSha are *string in the generated params. Use
	// nullable wrappers — buildID is always provided (caller invariant);
	// commitSHA may be empty.
	bID := buildID
	// Column2 is COALESCE(NULLIF($2,''), commit_sha) — sqlc could not
	// infer a name from the expression, so the generated param key is
	// Column2. Pass empty string when caller wants to preserve the
	// existing commit_sha (NULLIF('','') is NULL → COALESCE keeps the
	// stored value).
	n, err := q.UpdatePreviewBuild(ctx, bridge, generated.UpdatePreviewBuildParams{
		BuildID: &bID,
		Column2: commitSHA,
		ID:      previewID,
	})
	if err != nil {
		return fmt.Errorf("preview store: update build failed: %w", err)
	}
	if n == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// UpdateURL sets the public URL exposed to the user.
func (s *PostgresPreviewStore) UpdateURL(ctx context.Context, tx db.DBTX, previewID, url string) error {
	bridge := s.txBridge(tx)
	q := generated.New()
	u := url
	n, err := q.UpdatePreviewURL(ctx, bridge, generated.UpdatePreviewURLParams{
		Url: &u,
		ID:  previewID,
	})
	if err != nil {
		return fmt.Errorf("preview store: update url failed: %w", err)
	}
	if n == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// RequestDeletion soft-deletes a preview by setting status='deleting'
// and deleted_at=now(). The row remains visible for audit/history
// but is excluded from the partial unique index, allowing the same
// PR to spawn a new live preview.
//
// tenantID scopes the operation to prevent cross-tenant deletions
// when callers bypass RLS.
func (s *PostgresPreviewStore) RequestDeletion(ctx context.Context, tenantID, previewID string) error {
	bridge := s.bridge()
	q := generated.New()
	n, err := q.RequestPreviewDeletion(ctx, bridge, generated.RequestPreviewDeletionParams{
		ID:       previewID,
		TenantID: tenantID,
	})
	if err != nil {
		return fmt.Errorf("preview store: request deletion failed: %w", err)
	}
	if n == 0 {
		return loopzeroerrors.ErrPreviewNotFound
	}
	return nil
}

// =========================================================================
// Reads (API handlers)
// =========================================================================

// ListByTenant returns all live previews for a tenant.
func (s *PostgresPreviewStore) ListByTenant(ctx context.Context, tenantID string) ([]ports.Preview, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.ListPreviewsByTenant(ctx, bridge, tenantID)
	if err != nil {
		return nil, fmt.Errorf("preview store: list query failed: %w", err)
	}
	out := make([]ports.Preview, 0, len(rows))
	for _, row := range rows {
		rec := previewRecordFromRow(row)
		out = append(out, *rec.ToDomain())
	}
	return out, nil
}

// ListByProject returns all live previews for a (tenant, project)
// pair.
func (s *PostgresPreviewStore) ListByProject(ctx context.Context, tenantID, projectID string) ([]ports.Preview, error) {
	bridge := s.bridge()
	q := generated.New()
	rows, err := q.ListPreviewsByProject(ctx, bridge, generated.ListPreviewsByProjectParams{
		TenantID:  tenantID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("preview store: list query failed: %w", err)
	}
	out := make([]ports.Preview, 0, len(rows))
	for _, row := range rows {
		rec := previewRecordFromRow(row)
		out = append(out, *rec.ToDomain())
	}
	return out, nil
}

// GetByID returns a live preview by its ID, scoped to a tenant.
// pgx.ErrNoRows → ErrPreviewNotFound.
func (s *PostgresPreviewStore) GetByID(ctx context.Context, tenantID, previewID string) (*ports.Preview, error) {
	bridge := s.bridge()
	q := generated.New()
	row, err := q.GetPreviewByIDForTenant(ctx, bridge, generated.GetPreviewByIDForTenantParams{
		ID:       previewID,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrPreviewNotFound
		}
		return nil, fmt.Errorf("preview store: get by id failed: %w", err)
	}
	rec := previewRecordFromRow(row)
	return rec.ToDomain(), nil
}

// GetByPR returns the live preview for a (tenant, project,
// pr_number) tuple. pgx.ErrNoRows → ErrPreviewNotFound.
func (s *PostgresPreviewStore) GetByPR(ctx context.Context, tenantID, projectID string, prNumber int) (*ports.Preview, error) {
	bridge := s.bridge()
	q := generated.New()
	row, err := q.GetPreviewByPR(ctx, bridge, generated.GetPreviewByPRParams{
		TenantID:  tenantID,
		ProjectID: projectID,
		PrNumber:  int32(prNumber),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrPreviewNotFound
		}
		return nil, fmt.Errorf("preview store: get by PR failed: %w", err)
	}
	rec := previewRecordFromRow(row)
	return rec.ToDomain(), nil
}

// =========================================================================
// Helpers
// =========================================================================

// bridge returns the sqlc-compatible DBTX from s.DB (pool path).
func (s *PostgresPreviewStore) bridge() generated.DBTX {
	return db.Generated(s.DB)
}

// txBridge prefers the explicit transaction when provided, otherwise
// falls back to s.DB.
func (s *PostgresPreviewStore) txBridge(tx db.DBTX) generated.DBTX {
	if tx != nil {
		return db.Generated(tx)
	}
	return s.bridge()
}

// previewRecordFromRow converts a sqlc-generated PreviewEnvironment
// row into the package-internal PreviewRecord struct used by
// ToDomain.
func previewRecordFromRow(row generated.PreviewEnvironment) *PreviewRecord {
	rec := &PreviewRecord{
		ID:             row.ID,
		TenantID:       row.TenantID,
		ProjectID:      row.ProjectID,
		PRNumber:       int(row.PrNumber),
		Branch:         row.Branch,
		CommitSHA:      row.CommitSha,
		Status:         row.Status,
		URL:            row.Url,
		BuildID:        row.BuildID,
		ReleaseID:      row.ReleaseID,
		RepoFullName:   row.RepoFullName,
		InstallationID: row.InstallationID,
		CellID:         row.CellID,
	}
	if row.ExpiresAt.Valid {
		rec.ExpiresAt = row.ExpiresAt.Time
	}
	if row.CreatedAt.Valid {
		rec.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		rec.UpdatedAt = row.UpdatedAt.Time
	}
	if row.DeletedAt.Valid {
		t := row.DeletedAt.Time
		rec.DeletedAt = &t
	}
	return rec
}

// Compile-time interface assertion.
var _ PreviewStore = (*PostgresPreviewStore)(nil)
