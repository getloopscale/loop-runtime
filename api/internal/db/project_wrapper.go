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
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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
//
// M4 (account-level tenancy, Phase 2): when a ProjectCapChecker is wired, the
// per-Account project cap is enforced atomically. Unlimited plans (cap < 0) skip
// the lock+count (EC-6); bounded plans take a per-account advisory lock so the
// count+insert cannot race (two concurrent creations at the boundary can never
// both succeed). A nil checker leaves creation unenforced (dev/tests).
func (s *PostgresProjectStore) CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.ProjectResult, error) {
	if s.capChecker == nil {
		return s.insertProject(ctx, input)
	}
	// Resolve the cap up-front (no lock). cap < 0 = unlimited → no enforcement.
	_, plan, cap := s.capChecker.CheckProjectsValue(input.TenantID, 0)
	if cap < 0 {
		return s.insertProject(ctx, input) // EC-6: unlimited never locks/counts
	}
	return s.createProjectCapped(ctx, input, plan, cap)
}

// insertProject is the non-enforced insert (nil checker / unlimited plan), via
// the sqlc bridge. 23505 → domain.ErrProjectExists.
func (s *PostgresProjectStore) insertProject(ctx context.Context, input domain.CreateProjectInput) (domain.ProjectResult, error) {
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

	return domain.ProjectResult{ID: id, Name: input.Name, TeamID: input.TenantID}, nil
}

// createProjectCapped enforces the per-Account project cap ATOMICALLY: one
// transaction holds a per-account advisory lock across the count + insert, so two
// concurrent creations at the cap boundary cannot both succeed (the count is
// re-read under the lock). cap == 0 (no limit configured) fails closed.
func (s *PostgresProjectStore) createProjectCapped(ctx context.Context, input domain.CreateProjectInput, plan string, cap int) (domain.ProjectResult, error) {
	adapter, ok := s.DB.(*PgxAdapter)
	if !ok || adapter.Pool == nil {
		// A cap checker is wired (enforcement intended) but the store has no pgx
		// pool to open a transaction — atomic enforcement is impossible. FAIL CLOSED
		// on the quota boundary rather than silently bypass the cap: a non-pgx handle
		// here is a wiring bug, not a legitimate dev path (the nil-checker branch in
		// CreateProject already handles the unenforced dev case).
		return domain.ProjectResult{}, fmt.Errorf("project store: cap enforcement requires a pgx pool, got DBTX %T", s.DB)
	}

	tx, err := adapter.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ProjectResult{}, fmt.Errorf("project store: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	// Resolve the Account (tenants.account_id is NOT NULL since M3 000044).
	var accountID string
	if err := tx.QueryRow(ctx, "SELECT account_id FROM tenants WHERE id = $1", input.TenantID).Scan(&accountID); err != nil {
		return domain.ProjectResult{}, fmt.Errorf("project store: resolve account: %w", err)
	}

	// Per-account advisory lock (xact-scoped — auto-released on commit/rollback),
	// the canonical engine pattern (internal/lock.WithEnvironmentLock). Serializes
	// concurrent creations for the SAME Account so the count+insert is atomic.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", accountID); err != nil {
		return domain.ProjectResult{}, fmt.Errorf("project store: advisory lock: %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx,
		"SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id = $1)",
		accountID,
	).Scan(&count); err != nil {
		return domain.ProjectResult{}, fmt.Errorf("project store: count projects: %w", err)
	}

	// Block-new-only (ADR-M4-4): creating one more would make count+1 > cap.
	// cap == 0 (misconfigured) → count >= 0 always true → fail closed.
	if count >= cap {
		metrics.PlanLimitViolations.WithLabelValues("max_projects_per_account", plan).Inc()
		return domain.ProjectResult{}, loopzeroerrors.NewProjectsExceedPlan(plan, cap, count, accountID)
	}

	id := GenerateProjectID()
	if _, err := tx.Exec(ctx,
		"INSERT INTO projects (id, tenant_id, name, created_by) VALUES ($1, $2, $3, $4)",
		id, input.TenantID, input.Name, input.CreatedBy,
	); err != nil {
		if isPGUniqueViolation(err) {
			return domain.ProjectResult{}, domain.ErrProjectExists
		}
		return domain.ProjectResult{}, fmt.Errorf("project store: insert failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ProjectResult{}, fmt.Errorf("project store: commit: %w", err)
	}
	return domain.ProjectResult{ID: id, Name: input.Name, TeamID: input.TenantID}, nil
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
			ID:                 r.ID,
			Name:               r.Name,
			TeamID:             r.TenantID,
			CreatedBy:          r.CreatedBy,
			EnvironmentCount:   r.EnvironmentCount,
			ProductionEnvCount: r.ProductionEnvCount,
			// Sponsor LIVE-caught 2026-06-18: the aggregate status MUST
			// consider release state, not just latest build. A cancelled
			// build with a still-ACTIVE release is Live, not Failed.
			// MapProjectStatus reads release first, falls back to build
			// when no release is consultable. See aggregate_project_
			// status_test.go for the canonical decision tree.
			LastDeployStatus: string(enforcement.MapProjectStatus(
				r.ActiveReleaseStatusRaw,
				statemachine.State(r.LastDeployStatusRaw),
				r.LatestBuildEnvironment,
			)),
			// T5.3 LIVE-bug fix (2026-06-08): additive completion of T2.1's
			// summary contract. AppsCount + LatestFramework feed the
			// /projects table Apps + Framework columns (was always 0 / —).
			AppsCount:              r.AppsCount,
			LatestFramework:        r.LatestFrameworkRaw,
			LatestBuildEnvironment: r.LatestBuildEnvironment,
		}
		if r.CreatedAt.Valid {
			out.CreatedAt = r.CreatedAt.Time
		}
		// LastDeployAt is NULL when the project has zero builds. Pointer
		// + omitempty means the field is OMITTED from the wire when
		// absent — the dashboard's `p.last_deploy ?? '—'` guard then
		// fires canonically. Previously a value-type `time.Time` zero
		// serialized as `"0001-01-01T00:00:00Z"` (truthy in JS), which
		// rendered "2,027 years ago" on Overview / project-detail hero
		// (#118, qa-dogfood-cleanup-2026-06-21 T3.1).
		if r.LastDeployAt.Valid {
			t := r.LastDeployAt.Time
			out.LastDeployAt = &t
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
