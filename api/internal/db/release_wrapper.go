// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// release_wrapper.go — thin Go wrapper around sqlc-generated Queries for
// the `releases` table, with state-machine integration preserved at the
// store layer per D4 of plan docs/plans/2026-05-29-sqlc-migration-plan.md
// v1.2.
//
// T1.1b of plan v1.2 (Phase 1 pilot — M07 Rekor URL persistence 3-layer
// bug site). 5 of 13 method wrappers landed here; remaining 8 land in
// T1.1c as the sqlc query set grows.
//
// Pattern (per ADR-1 D3 + D4 + sqlctxprop gate):
//   - With `emit_methods_with_db_argument: true` (D3), each generated
//     function takes `(ctx, db DBTX, params)` — the caller passes the
//     pool OR a tx EVERY call. This is the "tx-required form" — pool-
//     bound shortcuts are not auto-generated, forcing intentional
//     tx-threading at every callsite.
//   - The wrapper holds a default DBTX (the connection pool) for
//     non-tx paths (background reapers, internal admin), and accepts
//     `pgx.Tx` parameters on mutating methods. When tx != nil, the
//     wrapper threads it; when nil, it falls back to the pool.
//   - State-machine integration (UpdateStatus) lives HERE — the
//     wrapper calls releaseMachine.SafeTransition BEFORE invoking
//     the underlying UPDATE.
//
// Co-exists with PostgresReleaseStore in release_store.go during T1.1b;
// caller migration + release_store.go deletion happens in T1.1c.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/loopzero/loopzero/api/internal/db/generated"
	"github.com/loopzero/loopzero/pkg/statemachine"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// ReleaseStoreSqlc is the sqlc-backed store wrapper for releases.
type ReleaseStoreSqlc struct {
	q    *generated.Queries
	pool generated.DBTX // default fallback for non-tx paths
}

// NewReleaseStoreSqlc constructs a sqlc-backed release store. `pool` is
// the connection pool used when callers do NOT provide a tx. Mutations
// SHOULD always come with a tx per Rule 6 + Rule 7; the pool fallback
// is for read-only paths outside withEnvironmentLock.
func NewReleaseStoreSqlc(pool generated.DBTX) *ReleaseStoreSqlc {
	return &ReleaseStoreSqlc{
		q:    generated.New(),
		pool: pool,
	}
}

// db returns the DBTX to use for this call — tx if provided, else pool.
func (s *ReleaseStoreSqlc) db(tx pgx.Tx) generated.DBTX {
	if tx == nil {
		return s.pool
	}
	return tx
}

// Create inserts a Release row. Caller MUST provide a pgx.Tx when this is
// invoked inside withEnvironmentLock (sqlctxprop gate enforces).
func (s *ReleaseStoreSqlc) Create(ctx context.Context, tx pgx.Tx, r *Release) error {
	params := generated.InsertReleaseParams{
		ID:                       r.ID,
		BuildID:                  r.BuildID,
		TenantID:                 r.TenantID,
		ProjectID:                r.ProjectID,
		Environment:              r.Environment,
		Status:                   r.Status,
		CorrelationID:            r.CorrelationID,
		CreatedBy:                r.CreatedBy,
		CreatedAt:                pgtype.Timestamptz{Time: r.CreatedAt, Valid: !r.CreatedAt.IsZero()},
		GitCommit:                stringEmptyToNilPtr(r.GitCommit),
		GitMirrorStatus:          r.GitMirrorStatus,
		Apps:                     r.Apps,
		Artifacts:                r.Artifacts,
		SideEffects:              r.SideEffects,
		ImageDigest:              r.ImageDigest,
		ImageSignature:           r.ImageSignature,
		ImageSignedAt:            timePtrToTimestamptz(r.ImageSignedAt),
		ImageAttestationRekorUrl: r.ImageAttestationRekorURL,
	}

	row, err := s.q.InsertRelease(ctx, s.db(tx), params)
	if err != nil {
		return fmt.Errorf("sqlc: insert release: %w", err)
	}
	// Mirror generated row back into the caller-owned Release struct.
	r.ID = row.ID
	r.Status = row.Status
	r.CreatedAt = row.CreatedAt.Time
	return nil
}

// GetByID is a cross-tenant lookup (used by reapers / admin paths).
// Marked `-- tenantpredicate:exempt` in queries/releases.sql.
func (s *ReleaseStoreSqlc) GetByID(ctx context.Context, releaseID string) (*Release, error) {
	row, err := s.q.GetReleaseByID(ctx, s.pool, releaseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlc: get release by id: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetByIDForTenant is the canonical tenant-isolated lookup.
func (s *ReleaseStoreSqlc) GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*Release, error) {
	row, err := s.q.GetReleaseByIDForTenant(ctx, s.pool, generated.GetReleaseByIDForTenantParams{
		ID:       releaseID,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlc: get release by id for tenant: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// UpdateStatus integrates the typed state-machine SafeTransition. Caller
// MUST provide a pgx.Tx when invoked inside withEnvironmentLock.
//
// D4 invariant: SafeTransition runs at the store layer (not in handlers).
// Per existing untyped→typed migration (release_store.go:24-34 comment in
// the legacy path), the typed wrapper catches the Machine.Transition
// panic and returns statemachine.ErrInvalidTransition — re-wrapped here
// as loopzeroerrors.InvalidTransition for backward compat.
func (s *ReleaseStoreSqlc) UpdateStatus(ctx context.Context, tx pgx.Tx, releaseID, tenantID, event string) (*Release, error) {
	// 1. Fetch current release (tenant-isolated for the canonical path;
	// reaper paths use GetByID directly).
	current, err := s.GetByIDForTenant(ctx, tenantID, releaseID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, loopzeroerrors.ErrReleaseNotFound
	}

	// 2. Validate transition via typed state machine — panic-trapped to
	// ErrInvalidTransition, then re-wrapped to loopzeroerrors.InvalidTransition.
	newPhase, err := releaseMachine.SafeTransition(
		statemachine.ReleasePhase(current.Status),
		statemachine.ReleasePhaseEvent(event),
	)
	if err != nil {
		return nil, loopzeroerrors.NewInvalidTransition(current.Status, event)
	}

	// 3. UPDATE through tx (D3 tx-required form when inside lock).
	row, err := s.q.UpdateReleaseStatus(ctx, s.db(tx), generated.UpdateReleaseStatusParams{
		Status:   string(newPhase),
		ID:       releaseID,
		TenantID: tenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: update release status: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetActiveByEnv returns the currently-ACTIVE release for an env tuple.
// At most one ACTIVE release per (tenant, project, env) at any time.
func (s *ReleaseStoreSqlc) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*Release, error) {
	row, err := s.q.GetActiveByEnv(ctx, s.pool, generated.GetActiveByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlc: get active by env: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetPreviousRelease returns the most-recent SUPERSEDED-or-ROLLED-BACK
// release — the canonical rollback target for an env.
func (s *ReleaseStoreSqlc) GetPreviousRelease(ctx context.Context, tenantID, projectID, env string) (*Release, error) {
	row, err := s.q.GetPreviousRelease(ctx, s.pool, generated.GetPreviousReleaseParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlc: get previous release: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetLatestByBuildID returns the most-recent release for a build_id (cross-
// tenant; caller validates ownership). Used by release_inspector + reconcile.
func (s *ReleaseStoreSqlc) GetLatestByBuildID(ctx context.Context, buildID string) (*Release, error) {
	row, err := s.q.GetLatestByBuildID(ctx, s.pool, buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlc: get latest by build_id: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// ListByEnv returns up to `limit` releases for a (tenant, project, env) tuple.
func (s *ReleaseStoreSqlc) ListByEnv(ctx context.Context, tenantID, projectID, env string, limit int32) ([]Release, error) {
	rows, err := s.q.ListReleasesByEnv(ctx, s.pool, generated.ListReleasesByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
		Limit:       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: list releases by env: %w", err)
	}
	out := make([]Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// SetGitCommit records the git commit SHA on a release after the gitops push.
func (s *ReleaseStoreSqlc) SetGitCommit(ctx context.Context, tx pgx.Tx, releaseID, tenantID, commitSHA string) error {
	_, err := s.q.SetReleaseGitCommit(ctx, s.db(tx), generated.SetReleaseGitCommitParams{
		GitCommit: stringEmptyToNilPtr(commitSHA),
		ID:        releaseID,
		TenantID:  tenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: set git commit: %w", err)
	}
	return nil
}

// AppendSideEffect merges a JSON object into the release's side_effects
// column via PostgreSQL jsonb concat (`||`). Repeated keys overwrite.
func (s *ReleaseStoreSqlc) AppendSideEffect(ctx context.Context, tx pgx.Tx, releaseID, tenantID string, patch []byte) error {
	if len(patch) == 0 {
		return nil
	}
	_, err := s.q.AppendReleaseSideEffect(ctx, s.db(tx), generated.AppendReleaseSideEffectParams{
		Column1:  patch,
		ID:       releaseID,
		TenantID: tenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: append side effect: %w", err)
	}
	return nil
}

// CancelAllByEnv cancels every non-CANCELLED release for an env. Returns
// the slice of cancelled release IDs.
func (s *ReleaseStoreSqlc) CancelAllByEnv(ctx context.Context, tx pgx.Tx, tenantID, projectID, env string) ([]string, error) {
	ids, err := s.q.CancelReleasesByEnv(ctx, s.db(tx), generated.CancelReleasesByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: cancel releases by env: %w", err)
	}
	return ids, nil
}

// FindPendingReleases returns up to 100 PENDING releases scoped to the
// given cell. Reaper path: intentionally cross-tenant within the cell.
// Empty cellID defaults to CellDefault per multi-cell backward compat.
func (s *ReleaseStoreSqlc) FindPendingReleases(ctx context.Context, cellID string) ([]Release, error) {
	if cellID == "" {
		cellID = CellDefault
	}
	rows, err := s.q.FindPendingReleasesByCell(ctx, s.pool, cellID)
	if err != nil {
		return nil, fmt.Errorf("sqlc: find pending releases: %w", err)
	}
	out := make([]Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// FindActiveReleases returns up to 100 ACTIVE releases scoped to the given
// cell, DISTINCT ON (tenant, project, env) → most-recent per tuple. Used by
// the convergence-loop health ticker (MEET-2026-03-31-023 T1-4).
func (s *ReleaseStoreSqlc) FindActiveReleases(ctx context.Context, cellID string) ([]Release, error) {
	if cellID == "" {
		cellID = CellDefault
	}
	rows, err := s.q.FindActiveReleasesByCell(ctx, s.pool, cellID)
	if err != nil {
		return nil, fmt.Errorf("sqlc: find active releases: %w", err)
	}
	out := make([]Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// FindPendingReleasesByTenantEnv returns PENDING releases for a specific
// (tenant, env). Used by the callback-driven activation path (CODE-SCAN-001).
func (s *ReleaseStoreSqlc) FindPendingReleasesByTenantEnv(ctx context.Context, tenantID, env string) ([]Release, error) {
	rows, err := s.q.FindPendingReleasesByTenantEnv(ctx, s.pool, generated.FindPendingReleasesByTenantEnvParams{
		TenantID:    tenantID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: find pending releases by tenant+env: %w", err)
	}
	out := make([]Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// -----------------------------------------------------------------------
// Conversion helpers between db.Release (legacy public type) and the
// sqlc-generated row type. We mirror nullable columns back and forth so
// existing callers continue to consume the unchanged db.Release shape
// during T1.1b. T1.1c will migrate callers off db.Release as part of
// the wrapper-replacement step.
// -----------------------------------------------------------------------

func generatedRowToRelease(row generated.Release) *Release {
	r := &Release{
		ID:              row.ID,
		BuildID:         row.BuildID,
		TenantID:        row.TenantID,
		ProjectID:       row.ProjectID,
		Environment:     row.Environment,
		Status:          row.Status,
		CorrelationID:   row.CorrelationID,
		CreatedBy:       row.CreatedBy,
		CreatedAt:       row.CreatedAt.Time,
		GitMirrorStatus: row.GitMirrorStatus,
		Apps:            row.Apps,
		Artifacts:       row.Artifacts,
		SideEffects:     row.SideEffects,
	}
	if row.GitCommit != nil {
		r.GitCommit = *row.GitCommit
	}
	if row.ImageDigest != nil {
		r.ImageDigest = row.ImageDigest
	}
	if row.ImageSignature != nil {
		r.ImageSignature = row.ImageSignature
	}
	if row.ImageSignedAt.Valid {
		v := row.ImageSignedAt.Time
		r.ImageSignedAt = &v
	}
	if row.ImageAttestationRekorUrl != nil {
		r.ImageAttestationRekorURL = row.ImageAttestationRekorUrl
	}
	return r
}

// stringEmptyToNilPtr converts an empty string to a nil *string for sqlc
// nullable columns. Non-empty strings get a heap-allocated pointer.
func stringEmptyToNilPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// timePtrToTimestamptz converts a *time.Time to a pgtype.Timestamptz.
// nil → Valid=false.
func timePtrToTimestamptz(p *time.Time) pgtype.Timestamptz {
	if p == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *p, Valid: true}
}
