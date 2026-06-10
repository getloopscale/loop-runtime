// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// build_wrapper.go — thin Go wrapper around sqlc-generated Queries for
// the `builds` table, with state-machine integration preserved at the
// store layer per D4 of plan docs/plans/2026-05-29-sqlc-migration-plan.md
// v1.3.
//
// T1.2 of plan v1.3 (Phase 1 pilot — concurrent-build invariant Rule 6 +
// IDOR closure find_sec_idor_buildstore + multi-cell cell_id propagation).
// Satisfies db.BuildStore interface 1:1 so the swap at the 3 prod wiring
// sites (setup_stores.go, setup_server.go, controller/setup_services.go)
// is interface-safe — service-side field types unchanged.
//
// Bridge pattern (identical to ReleaseStoreSqlc T1.1):
//   - Constructor accepts db.DBTX (matches BuildStore interface)
//   - gen(tx) DBTX → generated.DBTX via SqlcBridge (PgxAdapter +
//     PgxTxAdapter implement; test fakes deliberately don't — they
//     panic loudly to surface mis-wired tests early)
//   - State-machine SafeTransition runs at store layer (D4) — UpdateStatus
//     fetches via GetByIDTx (tenant-isolated), invokes buildMachine,
//     dispatches to 3 sqlc UPDATE queries based on newPhase (timing fields)
//
// Note: Build sqlc UPDATE queries enforce `tenant_id = $N` predicate —
// structurally stronger than legacy PostgresBuildStore which UPDATE-ed
// by id alone. Tenant derived internally via GetBuildByID lookup.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/loopzero/loopzero/api/internal/db/generated"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/id"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// Compile-time assertion: BuildStoreSqlc satisfies db.BuildStore 1:1.
var _ BuildStore = (*BuildStoreSqlc)(nil)

// BuildStoreSqlc is the sqlc-backed store wrapper for builds.
type BuildStoreSqlc struct {
	q    *generated.Queries
	pool DBTX
}

// NewBuildStoreSqlc constructs a sqlc-backed build store. `pool` is the
// connection pool used for read-only paths outside withEnvironmentLock.
// Mutations always carry a tx per Rule 6 + Rule 7; the pool fallback is
// for reapers + admin paths. Pool MUST implement SqlcBridge (PgxAdapter
// does); test fakes that don't will panic at query time — deliberate
// fail-fast to surface mis-wired tests early.
func NewBuildStoreSqlc(pool DBTX) *BuildStoreSqlc {
	return &BuildStoreSqlc{
		q:    generated.New(),
		pool: pool,
	}
}

// gen bridges db.DBTX → generated.DBTX. tx wins over pool. Panics if
// concrete type does not implement SqlcBridge.
func (s *BuildStoreSqlc) gen(tx DBTX) generated.DBTX {
	target := tx
	if target == nil {
		target = s.pool
	}
	bridge, ok := target.(SqlcBridge)
	if !ok {
		panic(fmt.Sprintf(
			"build_wrapper: DBTX %T does not implement SqlcBridge — sqlc-backed stores require PgxAdapter / PgxTxAdapter or pgxmock per T0.6 reshape matrix",
			target,
		))
	}
	return bridge.GeneratedDB()
}

// Create inserts a new build with status QUEUED. ID generated via pkg/id
// when empty. Unique-violation (23505) on (tenant, project, env, status)
// surfaces as ErrDeployInProgress to preserve the concurrent-deploy
// rejection invariant (Rule 6 + ARCH-002).
func (s *BuildStoreSqlc) Create(ctx context.Context, tx DBTX, build *Build) error {
	if build.ID == "" {
		build.ID = id.GenerateBuildID()
	}
	build.Status = string(statemachine.BPhQueued)

	_, err := s.q.InsertBuild(ctx, s.gen(tx), generated.InsertBuildParams{
		ID:            build.ID,
		TenantID:      build.TenantID,
		ProjectID:     build.ProjectID,
		Environment:   build.Environment,
		Status:        build.Status,
		CorrelationID: build.CorrelationID,
		CreatedBy:     build.CreatedBy,
		CommitSha:     stringEmptyToNilPtr(build.CommitSHA),
	})
	if err != nil {
		if isPGUniqueViolation(err) {
			return loopzeroerrors.ErrDeployInProgress
		}
		return fmt.Errorf("sqlc: insert build: %w", err)
	}
	return nil
}

// GetByID returns a build by ID with NO tenant filter. HTTP handlers
// MUST NOT call this — use GetByIDForTenant. Reserved for internal
// callers (convergence loops, reaper, domain_provisioner).
func (s *BuildStoreSqlc) GetByID(ctx context.Context, buildID string) (*Build, error) {
	row, err := s.q.GetBuildByID(ctx, s.gen(nil), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("sqlc: get build by id: %w", err)
	}
	return generatedBuildRowToBuild(row), nil
}

// GetByIDForTenant is the tenant-scoped lookup. Returns ErrBuildNotFound
// when the build belongs to a different tenant — closing the IDOR vector
// at the DB layer (find_sec_idor_buildstore).
func (s *BuildStoreSqlc) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*Build, error) {
	row, err := s.q.GetBuildByIDForTenant(ctx, s.gen(nil), generated.GetBuildByIDForTenantParams{
		ID:       buildID,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("sqlc: get build by id for tenant: %w", err)
	}
	return generatedBuildRowToBuild(row), nil
}

// HasActiveBuild checks if there is a build in QUEUED, RUNNING, or
// RELEASING status for the (tenant, project, env). Runs inside the
// advisory lock transaction (sqlctxprop gate validates tx form).
func (s *BuildStoreSqlc) HasActiveBuild(ctx context.Context, tx DBTX, tenantID, projectID, env string) (bool, error) {
	exists, err := s.q.HasActiveBuildForEnv(ctx, s.gen(tx), generated.HasActiveBuildForEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return false, fmt.Errorf("sqlc: has active build: %w", err)
	}
	return exists, nil
}

// UpdateStatus validates the transition via the typed state machine then
// dispatches to one of 3 sqlc UPDATE queries based on the resulting phase
// (timing fields are different across {Running, Terminal, Other}).
// Returns ErrBuildNotFound when the build doesn't exist.
// Returns loopzeroerrors.InvalidTransition on invalid transitions.
func (s *BuildStoreSqlc) UpdateStatus(ctx context.Context, tx DBTX, buildID, event string) (*Build, error) {
	current, err := s.getBuildByIDViaTx(ctx, tx, buildID)
	if err != nil {
		return nil, err
	}

	newPhase, err := buildMachine.SafeTransition(
		statemachine.BuildPhase(current.Status),
		statemachine.BuildPhaseEvent(event),
	)
	if err != nil {
		return nil, loopzeroerrors.NewInvalidTransition(current.Status, event)
	}

	var row generated.Build
	switch newPhase {
	case statemachine.BPhRunning:
		row, err = s.q.UpdateBuildStatusToRunning(ctx, s.gen(tx), generated.UpdateBuildStatusToRunningParams{
			Status:   string(newPhase),
			ID:       buildID,
			TenantID: current.TenantID,
		})
	case statemachine.BPhSucceeded, statemachine.BPhFailed, statemachine.BPhCancelled:
		row, err = s.q.UpdateBuildStatusToTerminal(ctx, s.gen(tx), generated.UpdateBuildStatusToTerminalParams{
			Status:   string(newPhase),
			ID:       buildID,
			TenantID: current.TenantID,
		})
	default:
		row, err = s.q.UpdateBuildStatusOther(ctx, s.gen(tx), generated.UpdateBuildStatusOtherParams{
			Status:   string(newPhase),
			ID:       buildID,
			TenantID: current.TenantID,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("sqlc: update build status: %w", err)
	}
	return generatedBuildRowToBuild(row), nil
}

// SetErrorMessage stores the error message and stamps finished_at.
// Returns ErrBuildNotFound when the row doesn't exist.
func (s *BuildStoreSqlc) SetErrorMessage(ctx context.Context, tx DBTX, buildID, errorMessage string) error {
	current, err := s.getBuildByIDViaTx(ctx, tx, buildID)
	if err != nil {
		return err
	}
	rows, err := s.q.SetBuildErrorMessage(ctx, s.gen(tx), generated.SetBuildErrorMessageParams{
		ErrorMessage: stringEmptyToNilPtr(errorMessage),
		ID:           buildID,
		TenantID:     current.TenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: set build error message: %w", err)
	}
	if rows == 0 {
		return loopzeroerrors.ErrBuildNotFound
	}
	return nil
}

// SetWorkflowID stores the Argo Workflow name. Returns ErrBuildNotFound
// when the row doesn't exist.
func (s *BuildStoreSqlc) SetWorkflowID(ctx context.Context, tx DBTX, buildID, workflowID string) error {
	current, err := s.getBuildByIDViaTx(ctx, tx, buildID)
	if err != nil {
		return err
	}
	rows, err := s.q.SetBuildWorkflowID(ctx, s.gen(tx), generated.SetBuildWorkflowIDParams{
		WorkflowID: stringEmptyToNilPtr(workflowID),
		ID:         buildID,
		TenantID:   current.TenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: set build workflow id: %w", err)
	}
	if rows == 0 {
		return loopzeroerrors.ErrBuildNotFound
	}
	return nil
}

// SetAppsSpec persists the canonicalized app spec JSON for the build.
func (s *BuildStoreSqlc) SetAppsSpec(ctx context.Context, tx DBTX, buildID string, apps []BuildAppSpec) error {
	current, err := s.getBuildByIDViaTx(ctx, tx, buildID)
	if err != nil {
		return err
	}
	appsJSON, err := json.Marshal(apps)
	if err != nil {
		return fmt.Errorf("sqlc: marshal apps spec: %w", err)
	}
	if err := s.q.SetBuildAppsSpec(ctx, s.gen(tx), generated.SetBuildAppsSpecParams{
		Column1:  appsJSON,
		ID:       buildID,
		TenantID: current.TenantID,
	}); err != nil {
		return fmt.Errorf("sqlc: set build apps spec: %w", err)
	}
	return nil
}

// GetBuildApps returns the persisted app specs. Cross-tenant by id —
// caller MUST validate ownership before invoking (reconcile loop path
// scopes by cell_id, HTTP handlers use GetByIDForTenant first).
func (s *BuildStoreSqlc) GetBuildApps(ctx context.Context, buildID string) ([]BuildAppSpec, error) {
	data, err := s.q.GetBuildApps(ctx, s.gen(nil), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("sqlc: get build apps: %w", err)
	}
	var apps []BuildAppSpec
	if err := json.Unmarshal(data, &apps); err != nil {
		return nil, fmt.Errorf("sqlc: unmarshal apps spec: %w", err)
	}
	return apps, nil
}

// ListByProject returns builds for a (tenant, project) newest first.
func (s *BuildStoreSqlc) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]Build, error) {
	rows, err := s.q.ListBuildsByProject(ctx, s.gen(nil), generated.ListBuildsByProjectParams{
		TenantID:  tenantID,
		ProjectID: projectID,
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: list builds by project: %w", err)
	}
	out := make([]Build, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedBuildRowToBuild(r))
	}
	return out, nil
}

// ListByProjectAndEnv returns (builds, totalCount, error). Empty env
// falls back to project-only list. Limit clamped to 1-50 (legacy parity).
func (s *BuildStoreSqlc) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]Build, int, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	if env == "" {
		// Project-only fallback path; total via CountBuildsByProject.
		builds, err := s.ListByProject(ctx, tenantID, projectID, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		total, err := s.q.CountBuildsByProject(ctx, s.gen(nil), generated.CountBuildsByProjectParams{
			TenantID:  tenantID,
			ProjectID: projectID,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("sqlc: count builds by project: %w", err)
		}
		return builds, int(total), nil
	}

	rows, err := s.q.ListBuildsByProjectAndEnv(ctx, s.gen(nil), generated.ListBuildsByProjectAndEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
		Limit:       int32(limit),
		Offset:      int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("sqlc: list builds by project+env: %w", err)
	}
	total, err := s.q.CountBuildsByProjectAndEnv(ctx, s.gen(nil), generated.CountBuildsByProjectAndEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("sqlc: count builds by project+env: %w", err)
	}
	out := make([]Build, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedBuildRowToBuild(r))
	}
	return out, int(total), nil
}

// FindActiveBuilds returns up to 100 QUEUED/RUNNING/RELEASING builds
// scoped to the given cell. Reaper path: intentionally cross-tenant
// within the cell. Empty cellID defaults to CellDefault per multi-cell
// backward compat.
func (s *BuildStoreSqlc) FindActiveBuilds(ctx context.Context, cellID string) ([]Build, error) {
	if cellID == "" {
		cellID = CellDefault
	}
	rows, err := s.q.FindActiveBuildsByCell(ctx, s.gen(nil), cellID)
	if err != nil {
		return nil, fmt.Errorf("sqlc: find active builds: %w", err)
	}
	out := make([]Build, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedBuildRowToBuild(r))
	}
	return out, nil
}

// CancelActiveBuilds cancels QUEUED/RUNNING builds for (tenant, project,
// env). Returns count + workflow_ids (for Argo cancellation). Skips the
// CANCELLING intermediate state — intentional bulk-op shortcut for
// project deletion.
func (s *BuildStoreSqlc) CancelActiveBuilds(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, []string, error) {
	wfIDs, err := s.q.CancelActiveBuildsByEnv(ctx, s.gen(tx), generated.CancelActiveBuildsByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return 0, nil, fmt.Errorf("sqlc: cancel active builds: %w", err)
	}
	count := len(wfIDs)
	// Filter empty workflow IDs (legacy behavior — empty strings dropped
	// from the returned slice since they're not actionable Argo names).
	workflowIDs := make([]string, 0, len(wfIDs))
	for _, id := range wfIDs {
		if id != "" {
			workflowIDs = append(workflowIDs, id)
		}
	}
	return count, workflowIDs, nil
}

// getBuildByIDViaTx fetches a build inside a tx. Cross-tenant: caller
// uses the returned tenant_id to scope subsequent UPDATEs.
func (s *BuildStoreSqlc) getBuildByIDViaTx(ctx context.Context, tx DBTX, buildID string) (*Build, error) {
	row, err := s.q.GetBuildByID(ctx, s.gen(tx), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("sqlc: get build by id (tx): %w", err)
	}
	return generatedBuildRowToBuild(row), nil
}

// -----------------------------------------------------------------------
// Conversion helpers between db.Build (legacy public type) and the sqlc-
// generated row. Legacy Build has 15 visible fields; generated.Build has
// 19 (adds CellID, ProcessingBy, ProcessingSince, BundleDigest, AppsSpec).
// We map only the legacy 15 — additional columns are accessible via
// future-facing typed accessors when consumers are ready.
// -----------------------------------------------------------------------

func generatedBuildRowToBuild(row generated.Build) *Build {
	b := &Build{
		ID:            row.ID,
		TenantID:      row.TenantID,
		ProjectID:     row.ProjectID,
		Environment:   row.Environment,
		Status:        row.Status,
		CorrelationID: row.CorrelationID,
		CreatedBy:     row.CreatedBy,
		CreatedAt:     row.CreatedAt.Time,
	}
	if row.CommitSha != nil {
		b.CommitSHA = *row.CommitSha
	}
	if row.StartedAt.Valid {
		v := row.StartedAt.Time
		b.StartedAt = &v
	}
	if row.FinishedAt.Valid {
		v := row.FinishedAt.Time
		b.FinishedAt = &v
	}
	if row.ErrorMessage != nil {
		b.ErrorMessage = *row.ErrorMessage
	}
	if row.DurationSeconds != nil {
		d := int(*row.DurationSeconds)
		b.DurationSeconds = &d
	}
	if row.WorkflowID != nil {
		b.WorkflowID = *row.WorkflowID
	}
	if row.AppsSpec != nil {
		b.AppsSpec = row.AppsSpec
	}
	if row.BundleDigest != nil {
		b.BundleDigest = *row.BundleDigest
	}
	return b
}

// isPGUniqueViolation checks if the err is a Postgres unique-violation
// error (SQLSTATE 23505). Unlike the legacy `db.UniqueViolationError`
// pathway, sqlc-backed queries flow through pgx-native types directly
// (NOT through PgxAdapter.translatePgError), so we must inspect the raw
// pgconn.PgError. Used by Create to surface ErrDeployInProgress on
// concurrent-deploy collision (Rule 6 + ARCH-002).
func isPGUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	// Translated form (legacy callers that wrap raw pg errors through
	// PgxAdapter still surface UniqueViolationError; preserved here for
	// defense-in-depth even though sqlc-direct path shouldn't hit it).
	var uv *UniqueViolationError
	return errors.As(err, &uv)
}
