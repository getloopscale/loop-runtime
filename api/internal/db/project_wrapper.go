// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// project_wrapper.go — sqlc-backed implementation of
// PostgresProjectStore. T2.1 Batch A #4 of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
//
// The 5 prod call sites (api/cmd/setup_orchestrator.go × 3,
// api/cmd/setup_database.go × 1, api/cmd/controller/setup_services.go
// × 1) compile UNCHANGED — struct-literal `&db.PostgresProjectStore{
// DB: dbAdapter}` preserved (struct + GenerateProjectID() in
// project_types.go).
//
// Invariants preserved:
//   - 23505 unique_violation on (tenant_id, name) →
//     domain.ErrProjectExists (legacy contract; isPGUniqueViolation
//     covers both raw pgconn.PgError + UniqueViolationError paths)
//   - EnsureProject idempotent (ON CONFLICT DO NOTHING)
//   - GetByName NoRows → (nil, nil)
//   - DeleteProject 0-rows-affected → "project not found" error

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/domain"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// GenerateProjectID generates a project ID in the format
// "proj-{8 hex chars}". Kept package-level + exported so call sites
// outside the wrapper (e.g., services that pre-allocate IDs before
// invoking CreateProject) can reuse the same identifier shape.
func GenerateProjectID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "proj-" + hex.EncodeToString(b)
}

// CreateProject inserts a new project. 23505 unique_violation →
// domain.ErrProjectExists.
func (s *PostgresProjectStore) CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.ProjectResult, error) {
	bridge, ok := s.DB.(SqlcBridge)
	if !ok {
		return domain.ProjectResult{}, fmt.Errorf("project store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	q := generated.New()

	id := GenerateProjectID()
	if err := q.InsertProject(ctx, bridge.GeneratedDB(), generated.InsertProjectParams{
		ID:        id,
		TenantID:  input.TenantID,
		Name:      input.Name,
		CreatedBy: input.CreatedBy,
	}); err != nil {
		if isPGUniqueViolation(err) {
			return domain.ProjectResult{}, domain.ErrProjectExists
		}
		return domain.ProjectResult{}, fmt.Errorf("project store: insert failed: %w", err)
	}

	return domain.ProjectResult{
		ID:     id,
		Name:   input.Name,
		TeamID: input.TenantID,
	}, nil
}

// EnsureProject creates a project if it doesn't exist (idempotent).
// Accepts an explicit DBTX so callers can compose with their existing
// transaction (release pipeline: project ensure within env lock).
func (s *PostgresProjectStore) EnsureProject(ctx context.Context, tx DBTX, tenantID, name, createdBy string) error {
	target := tx
	if target == nil {
		target = s.DB
	}
	bridge, ok := target.(SqlcBridge)
	if !ok {
		return fmt.Errorf("project store: DBTX %T does not implement SqlcBridge", target)
	}
	q := generated.New()

	if err := q.EnsureProject(ctx, bridge.GeneratedDB(), generated.EnsureProjectParams{
		ID:        GenerateProjectID(),
		TenantID:  tenantID,
		Name:      name,
		CreatedBy: createdBy,
	}); err != nil {
		return fmt.Errorf("project store: ensure project failed: %w", err)
	}
	return nil
}

// ListByTenant returns all projects for a tenant, ordered by name.
// Empty slice (not nil) when the tenant has no projects.
func (s *PostgresProjectStore) ListByTenant(ctx context.Context, tenantID string) ([]domain.ProjectResult, error) {
	bridge, ok := s.DB.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("project store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	q := generated.New()

	rows, err := q.ListProjectsByTenant(ctx, bridge.GeneratedDB(), tenantID)
	if err != nil {
		return nil, fmt.Errorf("project store: list by tenant: %w", err)
	}
	results := make([]domain.ProjectResult, 0, len(rows))
	for _, r := range rows {
		out := domain.ProjectResult{
			ID:        r.ID,
			Name:      r.Name,
			TeamID:    r.TenantID,
			CreatedBy: r.CreatedBy,
		}
		if r.CreatedAt.Valid {
			out.CreatedAt = r.CreatedAt.Time
		}
		results = append(results, out)
	}
	return results, nil
}

// ListByTenantWithSummary returns all projects for a tenant with
// per-project summary fields (environment_count + last_deploy_status).
// Single SQL round-trip via LEFT JOIN LATERAL (no N+1). Post plan
// 2026-06-08-dashboard-screens-review-fixes T2.1 + ADR D2.
// Raw build status from the latest build row is mapped to the 5-value
// dashboard ProjectStatus enum via
// pkg/enforcement.MapBuildStatusToProjectStatus (exhaustive 7-state
// switch per EC-1 absorption).
func (s *PostgresProjectStore) ListByTenantWithSummary(ctx context.Context, tenantID string) ([]domain.ProjectSummary, error) {
	bridge, ok := s.DB.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("project store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	q := generated.New()

	rows, err := q.ListProjectsWithSummaryByTenant(ctx, bridge.GeneratedDB(), tenantID)
	if err != nil {
		return nil, fmt.Errorf("project store: list by tenant with summary: %w", err)
	}
	results := make([]domain.ProjectSummary, 0, len(rows))
	for _, r := range rows {
		out := domain.ProjectSummary{
			ID:               r.ID,
			Name:             r.Name,
			TeamID:           r.TenantID,
			CreatedBy:        r.CreatedBy,
			EnvironmentCount: r.EnvironmentCount,
			// Sponsor LIVE-caught 2026-06-18: the aggregate status MUST
			// consider release state, not just latest build. A cancelled
			// build with a still-ACTIVE release is Live, not Failed.
			// MapProjectStatus reads release first, falls back to build
			// when no release is consultable. See aggregate_project_
			// status_test.go for the canonical decision tree.
			LastDeployStatus: string(enforcement.MapProjectStatus(
				r.ActiveReleaseStatusRaw,
				statemachine.State(r.LastDeployStatusRaw),
			)),
			// T5.3 LIVE-bug fix (2026-06-08): additive completion of T2.1's
			// summary contract. AppsCount + LatestFramework feed the
			// /projects table Apps + Framework columns (was always 0 / —).
			AppsCount:       r.AppsCount,
			LatestFramework: r.LatestFrameworkRaw,
		}
		if r.CreatedAt.Valid {
			out.CreatedAt = r.CreatedAt.Time
		}
		// LastDeployAt is NULL when the project has zero builds — keep
		// out.LastDeployAt as the zero time so `omitempty` JSON tag omits
		// the field, and the dashboard's `p.last_deploy ? ... : '—'` guard
		// renders the em-dash naturally.
		if r.LastDeployAt.Valid {
			out.LastDeployAt = r.LastDeployAt.Time
		}
		results = append(results, out)
	}
	return results, nil
}

// GetByName retrieves a project by tenant and name. Returns (nil,
// nil) when not found (legacy contract).
func (s *PostgresProjectStore) GetByName(ctx context.Context, tenantID, name string) (*domain.ProjectResult, error) {
	bridge, ok := s.DB.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("project store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	q := generated.New()

	row, err := q.GetProjectByName(ctx, bridge.GeneratedDB(), generated.GetProjectByNameParams{
		TenantID: tenantID,
		Name:     name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("project store: get by name: %w", err)
	}
	result := &domain.ProjectResult{
		ID:        row.ID,
		Name:      row.Name,
		TeamID:    row.TenantID,
		CreatedBy: row.CreatedBy,
	}
	if row.CreatedAt.Valid {
		result.CreatedAt = row.CreatedAt.Time
	}
	return result, nil
}

// DeleteProject removes a project from the database. 0 rows affected
// → "project not found" error (legacy contract).
func (s *PostgresProjectStore) DeleteProject(ctx context.Context, tenantID, projectID string) error {
	bridge, ok := s.DB.(SqlcBridge)
	if !ok {
		return fmt.Errorf("project store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	q := generated.New()

	rows, err := q.DeleteProjectByID(ctx, bridge.GeneratedDB(), generated.DeleteProjectByIDParams{
		TenantID: tenantID,
		ID:       projectID,
	})
	if err != nil {
		return fmt.Errorf("project store: delete failed: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("project store: project not found")
	}
	return nil
}
