// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// project_wrapper.go — sqlc-backed implementation of
// PostgresProjectStore. T2.1 Batch A #4 of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 5 prod call sites (api/cmd/setup_orchestrator.go × 3,
// api/cmd/setup_database.go × 1, api/cmd/controller/setup_services.go
// × 1) compile UNCHANGED — struct-literal `&db.PostgresProjectStore{
// DB: dbAdapter}` preserved (struct + GenerateProjectID() in
// project_types.go).
//
// Invariants preserved:
//   - 23505 unique_violation on (tenant_id, name) →
//     ports.ErrProjectExists (legacy contract; isPGUniqueViolation
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

	"github.com/getloopscale/loopzero/api/internal/db"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/projectstatus"
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
// ports.ErrProjectExists.
//
// M4 (account-level tenancy, Phase 2): when a ProjectCapChecker is wired, the
// per-Account project cap is enforced atomically. Unlimited plans (cap < 0) skip
// the lock+count (EC-6); bounded plans take a per-account advisory lock so the
// count+insert cannot race (two concurrent creations at the boundary can never
// both succeed). A nil checker leaves creation unenforced (dev/tests).
func (s *PostgresProjectStore) CreateProject(ctx context.Context, input ports.CreateProjectInput) (ports.ProjectResult, error) {
	if s.capChecker == nil {
		return s.insertProject(ctx, input)
	}
	// Resolve the cap up-front (no lock). cap < 0 = unlimited → no enforcement.
	_, plan, cap, err := s.capChecker.CheckProjectsValue(ctx, input.TenantID, 0)
	if err != nil {
		return ports.ProjectResult{}, err
	}
	if cap < 0 {
		return s.insertProject(ctx, input) // EC-6: unlimited never locks/counts
	}
	return s.createProjectCapped(ctx, input, plan, cap)
}

// insertProject is the non-enforced insert (nil checker / unlimited plan), via
// the sqlc bridge. 23505 → ports.ErrProjectExists.
func (s *PostgresProjectStore) insertProject(ctx context.Context, input ports.CreateProjectInput) (ports.ProjectResult, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	id := GenerateProjectID()
	if err := q.InsertProject(ctx, gdb, generated.InsertProjectParams{
		ID:        id,
		TenantID:  input.TenantID,
		Name:      input.Name,
		CreatedBy: input.CreatedBy,
	}); err != nil {
		if db.IsPGUniqueViolation(err) {
			return ports.ProjectResult{}, ports.ErrProjectExists
		}
		return ports.ProjectResult{}, fmt.Errorf("project store: insert failed: %w", err)
	}

	return ports.ProjectResult{ID: id, Name: input.Name, TeamID: input.TenantID}, nil
}

// resolveAccountAndLock resolves the tenant's Account (tenants.account_id, NOT
// NULL since M3 000044) and acquires the per-account xact-scoped advisory lock on
// tx — the canonical engine serialization (internal/lock.WithEnvironmentLock),
// auto-released when tx commits/rolls back. Both the API create path
// (createProjectCapped) and the deploy path (EnsureProject) call this so they
// serialize on the SAME hashtext(accountID) keyspace — the project cap is atomic
// ACROSS both paths, not just within one. Shared so neither path duplicates the
// resolve+lock SQL.
func resolveAccountAndLock(ctx context.Context, tx db.DBTX, tenantID string) (string, error) {
	var accountID string
	if err := tx.QueryRow(ctx, "SELECT account_id FROM tenants WHERE id = $1", tenantID).Scan(&accountID); err != nil {
		return "", fmt.Errorf("project store: resolve account: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", accountID); err != nil {
		return "", fmt.Errorf("project store: advisory lock: %w", err)
	}
	return accountID, nil
}

// enforceProjectCapTx counts the Account's projects under the already-held
// advisory lock and, if creating one more would exceed cap, increments the
// violation metric and returns a *LoopZeroError(PROJECTS_EXCEED_PLAN). cap == 0
// (misconfigured) fails closed (count >= 0 always). The caller MUST have called
// resolveAccountAndLock on the same tx first. Shared by createProjectCapped (API)
// and ensureProjectCapped (deploy) so the cap rule + the count SQL live once.
//
// RLS INVARIANT (load-bearing), and how it is satisfied NOW.
//
// The count is a CROSS-TENANT aggregate over all the Account's tenants, while
// the transaction that calls it carries ONE tenant's context (internal/lock
// sets `app.tenant_id = <deploying tenant>` on the submit path). Until
// 2026-08-14 the request pool held BYPASSRLS, so the policy never applied and
// the count saw every tenant. Migration 000062 plus the boot guard
// `AssertRequestPoolIsolated` moved that pool to `loopzero_api` — non-owner, no
// BYPASSRLS — and the count silently became per-TENANT.
//
// Measured on a real database: one account, two tenants, one project each.
//
//	loopzero_api  (context = tenant 1)  -> 1   <= cap, creation allowed
//	loopzero_worker                     -> 2
//
// A plan capped at N projects became N per tenant, unbounded across tenants.
// Nothing errored and nothing logged; the cap simply stopped being the cap.
//
// `crossTenant` is the fix: a handle on the connection that legitimately spans
// tenants (loopzero_worker, whose reach is the named *_worker_cross_tenant policies
// of migration 000062). It is used for the COUNT only — the lock, the checks and
// the write all stay on the caller's transaction.
//
// WHY COUNTING OFF-TRANSACTION IS SAFE HERE. Every creator takes
// `pg_advisory_xact_lock(hashtext(accountID))` before reaching this point, so at
// most one creator per account is in flight; a separate connection therefore
// sees the complete committed state for that account. Without the lock this
// would be a race; with it, it is the same number the in-transaction count used
// to produce.
//
// When `crossTenant` is nil the count falls back to the caller's transaction —
// the pre-2026-08-14 behaviour — and is filtered by RLS. That path is for tests
// and for a database whose worker role was never provisioned; production wires
// it, and `TestProjectCap_CrossTenantCount` pins the difference.
func enforceProjectCapTx(ctx context.Context, tx db.DBTX, crossTenant db.DBTX, accountID, plan string, cap int) error {
	counter := tx
	if crossTenant != nil {
		counter = crossTenant
	}
	var count int
	if err := counter.QueryRow(ctx,
		"SELECT count(*) FROM projects WHERE tenant_id IN (SELECT id FROM tenants WHERE account_id = $1)",
		accountID,
	).Scan(&count); err != nil {
		return fmt.Errorf("project store: count projects: %w", err)
	}
	// Block-new-only (ADR-M4-4): creating one more would make count+1 > cap.
	if count >= cap {
		metrics.PlanLimitViolations.WithLabelValues("max_projects_per_account", plan).Inc()
		return loopzeroerrors.NewProjectsExceedPlan(plan, cap, count, accountID)
	}
	return nil
}

// createProjectCapped enforces the per-Account project cap ATOMICALLY: one
// transaction holds a per-account advisory lock across the count + insert, so two
// concurrent creations at the cap boundary cannot both succeed (the count is
// re-read under the lock). cap == 0 (no limit configured) fails closed.
func (s *PostgresProjectStore) createProjectCapped(ctx context.Context, input ports.CreateProjectInput, plan string, cap int) (ports.ProjectResult, error) {
	adapter, ok := s.DB.(*db.PgxAdapter)
	if !ok || adapter.Pool == nil {
		// A cap checker is wired (enforcement intended) but the store has no pgx
		// pool to open a transaction — atomic enforcement is impossible. FAIL CLOSED
		// on the quota boundary rather than silently bypass the cap: a non-pgx handle
		// here is a wiring bug, not a legitimate dev path (the nil-checker branch in
		// CreateProject already handles the unenforced dev case).
		return ports.ProjectResult{}, fmt.Errorf("project store: cap enforcement requires a pgx pool, got DBTX %T", s.DB)
	}

	pgtx, err := adapter.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.ProjectResult{}, fmt.Errorf("project store: begin tx: %w", err)
	}
	defer func() { _ = pgtx.Rollback(ctx) }() // no-op after a successful Commit
	tx := &db.PgxTxAdapter{Tx: pgtx}

	accountID, err := resolveAccountAndLock(ctx, tx, input.TenantID)
	if err != nil {
		return ports.ProjectResult{}, err
	}
	if err := enforceProjectCapTx(ctx, tx, s.crossTenant, accountID, plan, cap); err != nil {
		return ports.ProjectResult{}, err
	}

	id := GenerateProjectID()
	if _, err := tx.Exec(ctx,
		"INSERT INTO projects (id, tenant_id, name, created_by) VALUES ($1, $2, $3, $4)",
		id, input.TenantID, input.Name, input.CreatedBy,
	); err != nil {
		if db.IsPGUniqueViolation(err) {
			return ports.ProjectResult{}, ports.ErrProjectExists
		}
		return ports.ProjectResult{}, fmt.Errorf("project store: insert failed: %w", err)
	}

	if err := pgtx.Commit(ctx); err != nil {
		return ports.ProjectResult{}, fmt.Errorf("project store: commit: %w", err)
	}
	return ports.ProjectResult{ID: id, Name: input.Name, TeamID: input.TenantID}, nil
}

// EnsureProject creates a project if it doesn't exist (idempotent).
// Accepts an explicit DBTX so callers can compose with their existing
// transaction (release pipeline: project ensure within env lock).
//
// M5 (hermetic project-cap, #106): when a ProjectCapChecker is wired and the
// plan is bounded (cap >= 0), the deploy-time auto-create enforces the SAME
// per-Account cap as CreateProject — closing the bypass where `loopzero deploy
// <new-name>` created a project past the cap. Unlimited (cap < 0) and the
// nil-checker dev/test path keep the original unenforced idempotent insert.
//
// COST (bounded plans only): a bounded-plan deploy now does, before the
// idempotent insert, one plan resolution + a per-account advisory lock + an
// existence SELECT + a cross-tenant count — and serializes concurrent deploys of
// the SAME account on hashtext(accountID) for the (short) remainder of the
// build-submit tx. Deploys are low-QPS and the locked window is DB-only (the
// workflow submit is outside it), so this is acceptable; enterprise/unlimited
// and the nil-checker (dev) path stay zero-overhead (the early returns below).
func (s *PostgresProjectStore) EnsureProject(ctx context.Context, tx db.DBTX, tenantID, name, createdBy string) error {
	if s.capChecker != nil {
		_, plan, cap, err := s.capChecker.CheckProjectsValue(ctx, tenantID, 0)
		if err != nil {
			return err
		}
		if cap >= 0 { // bounded plan → enforce atomically (cap < 0 = unlimited, EC-6)
			return s.ensureProjectCapped(ctx, tx, tenantID, name, createdBy, plan, cap)
		}
	}
	return s.ensureProjectInsert(ctx, tx, tenantID, name, createdBy)
}

// ensureProjectInsert is the original idempotent insert (ON CONFLICT
// (tenant_id, name) DO NOTHING) via the sqlc bridge. Used for the unenforced
// path (nil checker / unlimited) and as the final insert under the cap lock.
func (s *PostgresProjectStore) ensureProjectInsert(ctx context.Context, tx db.DBTX, tenantID, name, createdBy string) error {
	target := tx
	if target == nil {
		target = s.DB
	}
	gdb := db.Generated(target)
	q := generated.New()

	if err := q.EnsureProject(ctx, gdb, generated.EnsureProjectParams{
		ID:        GenerateProjectID(),
		TenantID:  tenantID,
		Name:      name,
		CreatedBy: createdBy,
	}); err != nil {
		return fmt.Errorf("project store: ensure project failed: %w", err)
	}
	return nil
}

// ensureProjectCapped enforces the per-Account project cap ATOMICALLY for a
// genuinely-NEW project on the deploy path. Existence is checked UNDER the
// advisory lock so a re-deploy to an EXISTING project is never blocked, even when
// the account is at/over cap (ADR-M5-2, block-new-only).
//
// Deadlock-safety (ADR-M5-1): the advisory lock is taken on the CALLER's tx (the
// build-submit env-lock'd tx) — never a nested BeginTx/connection. The build path
// holds the env-lock (hashtext("{tenant}:{project}:{env}")) then this account-lock
// (hashtext(accountID)); the API path takes the account-lock alone; no path takes
// account→env. The single global order env→account is therefore acyclic. When tx
// is nil (no caller tx) we open our own tx from the pgx pool (EC-1); without a pgx
// pool we fail closed rather than bypass the cap.
func (s *PostgresProjectStore) ensureProjectCapped(ctx context.Context, tx db.DBTX, tenantID, name, createdBy, plan string, cap int) error {
	capTx := tx
	var ownTx pgx.Tx
	if capTx == nil {
		adapter, ok := s.DB.(*db.PgxAdapter)
		if !ok || adapter.Pool == nil {
			return fmt.Errorf("project store: cap enforcement requires a pgx pool, got DBTX %T", s.DB)
		}
		t, err := adapter.Pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return fmt.Errorf("project store: begin tx: %w", err)
		}
		ownTx = t
		defer func() { _ = t.Rollback(ctx) }() // no-op after a successful Commit
		capTx = &db.PgxTxAdapter{Tx: t}
	}

	accountID, err := resolveAccountAndLock(ctx, capTx, tenantID)
	if err != nil {
		return err
	}

	// Existence UNDER the lock: an already-existing (tenant_id, name) is a
	// re-deploy → never blocked (ADR-M5-2). Checked after the lock so it cannot
	// race a concurrent first-time create of the same project.
	var one int
	existErr := capTx.QueryRow(ctx, "SELECT 1 FROM projects WHERE tenant_id = $1 AND name = $2", tenantID, name).Scan(&one)
	switch {
	case existErr == nil:
		// Project exists → no new row, no cap check. Commit our own tx if we own it.
		if ownTx != nil {
			if err := ownTx.Commit(ctx); err != nil {
				return fmt.Errorf("project store: commit: %w", err)
			}
		}
		return nil
	case errors.Is(existErr, pgx.ErrNoRows) || errors.Is(existErr, db.ErrNoRows):
		// Not found → genuinely new → enforce the cap below.
	default:
		return fmt.Errorf("project store: existence check: %w", existErr)
	}

	if err := enforceProjectCapTx(ctx, capTx, s.crossTenant, accountID, plan, cap); err != nil {
		return err
	}

	if err := s.ensureProjectInsert(ctx, capTx, tenantID, name, createdBy); err != nil {
		return err
	}

	if ownTx != nil {
		if err := ownTx.Commit(ctx); err != nil {
			return fmt.Errorf("project store: commit: %w", err)
		}
	}
	return nil
}

// ListByTenant returns all projects for a tenant, ordered by name.
// Empty slice (not nil) when the tenant has no projects.
func (s *PostgresProjectStore) ListByTenant(ctx context.Context, tenantID string) ([]ports.ProjectResult, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.ListProjectsByTenant(ctx, gdb, tenantID)
	if err != nil {
		return nil, fmt.Errorf("project store: list by tenant: %w", err)
	}
	results := make([]ports.ProjectResult, 0, len(rows))
	for _, r := range rows {
		out := ports.ProjectResult{
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
// api/internal/projectstatus.MapBuildStatusToProjectStatus (exhaustive 7-state
// switch per EC-1 absorption).
func (s *PostgresProjectStore) ListByTenantWithSummary(ctx context.Context, tenantID string) ([]ports.ProjectSummary, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.ListProjectsWithSummaryByTenant(ctx, gdb, tenantID)
	if err != nil {
		return nil, fmt.Errorf("project store: list by tenant with summary: %w", err)
	}
	results := make([]ports.ProjectSummary, 0, len(rows))
	for _, r := range rows {
		out := ports.ProjectSummary{
			ID:                 r.ID,
			Name:               r.Name,
			TeamID:             r.TenantID,
			CreatedBy:          r.CreatedBy,
			EnvironmentCount:   r.EnvironmentCount,
			ProductionEnvCount: r.ProductionEnvCount,
			// Red Team loopzero-cloud#134 (2026-07-02): "LIVE ENVS" counter must
			// reflect envs with a currently-serving ACTIVE release (preview
			// or permanent), not the legacy production_env_count subset.
			ActiveEnvCount: r.ActiveEnvCount,
			// Sponsor LIVE-caught 2026-06-18: the aggregate status MUST
			// consider release state, not just latest build. A cancelled
			// build with a still-ACTIVE release is Live, not Failed.
			// MapProjectStatus reads release first, falls back to build
			// when no release is consultable. See aggregate_project_
			// status_test.go for the canonical decision tree.
			LastDeployStatus: string(projectstatus.MapProjectStatus(
				statemachine.State(r.ActiveReleaseStatusRaw),
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
		// T3.2 of plan dashboard-badge-data-plane-poller (Red Team
		// loopzero-cloud#128). Both pgtype fields carry .Valid=false when
		// the environment_targets row has no data-plane snapshot yet
		// (Cleanroom warm-up window before the first DataPlanePoller
		// tick OR the poller is disabled via the env-gate). Nil pointers
		// + `json:"...,omitempty"` produce absent JSON keys — the
		// dashboard maps absence to the honest "Unknown" state without
		// collapsing it to Down.
		if r.LastReplicasReadyValue != nil {
			v := int(*r.LastReplicasReadyValue)
			out.LastReplicasReadyValue = &v
		}
		if r.LastReplicasReadyAt.Valid {
			t := r.LastReplicasReadyAt.Time
			out.LastReplicasReadyAt = &t
		}
		results = append(results, out)
	}
	return results, nil
}

// GetByName retrieves a project by tenant and name. Returns (nil,
// nil) when not found (legacy contract).
func (s *PostgresProjectStore) GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	row, err := q.GetProjectByName(ctx, gdb, generated.GetProjectByNameParams{
		TenantID: tenantID,
		Name:     name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("project store: get by name: %w", err)
	}
	result := &ports.ProjectResult{
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
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.DeleteProjectByID(ctx, gdb, generated.DeleteProjectByIDParams{
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
