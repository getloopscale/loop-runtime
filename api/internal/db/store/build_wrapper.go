// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// build_wrapper.go — thin Go wrapper around sqlc-generated Queries for
// the `builds` table, with state-machine integration preserved at the
// store layer per D4 of plan 2026-05-29-sqlc-migration-plan
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
//   - gen(tx) DBTX → generated.DBTX via db.Generated (PgxAdapter +
//     PgxTxAdapter implement SqlcBridge; any other handle fails the
//     query with db.ErrNotSqlcBridge)
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
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/pkg/domain"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/id"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// Compile-time assertion: BuildStoreSqlc satisfies db.BuildStore 1:1.
var _ db.BuildStore = (*BuildStoreSqlc)(nil)

// BuildStoreSqlc is the sqlc-backed store wrapper for builds.
type BuildStoreSqlc struct {
	q    *generated.Queries
	pool db.DBTX
}

// NewBuildStoreSqlc constructs a sqlc-backed build store. `pool` is the
// connection pool used for read-only paths outside withEnvironmentLock.
// Mutations always carry a tx per Rule 6 + Rule 7; the pool fallback is
// for reapers + admin paths. Pool MUST implement SqlcBridge (PgxAdapter
// does); any other handle fails each query with db.ErrNotSqlcBridge.
func NewBuildStoreSqlc(pool db.DBTX) *BuildStoreSqlc {
	return &BuildStoreSqlc{
		q:    generated.New(),
		pool: pool,
	}
}

// gen resolves the sqlc handle (db.Generated): tx wins over pool. A handle
// that is not a SqlcBridge makes the query fail with db.ErrNotSqlcBridge.
func (s *BuildStoreSqlc) gen(tx db.DBTX) generated.DBTX {
	target := tx
	if target == nil {
		target = s.pool
	}
	return db.Generated(target)
}

// Create inserts a new build with status QUEUED. ID generated via pkg/id
// when empty. Unique-violation (23505) on (tenant, project, env, status)
// surfaces as ErrDeployInProgress to preserve the concurrent-deploy
// rejection invariant (Rule 6 + ARCH-002).
func (s *BuildStoreSqlc) Create(ctx context.Context, tx db.DBTX, build *db.Build) error {
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
		CommitSha:     dbconv.StringEmptyToNilPtr(build.CommitSHA),
	})
	if err != nil {
		if db.IsPGUniqueViolation(err) {
			return loopruntimeerrors.ErrDeployInProgress
		}
		return fmt.Errorf("sqlc: insert build: %w", err)
	}
	return nil
}

// GetByID returns a build by ID with NO tenant filter. HTTP handlers
// MUST NOT call this — use GetByIDForTenant. Reserved for internal
// callers (convergence loops, reaper, domain_provisioner).
func (s *BuildStoreSqlc) GetByID(ctx context.Context, buildID string) (*db.Build, error) {
	row, err := s.q.GetBuildByID(ctx, s.gen(nil), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopruntimeerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("sqlc: get build by id: %w", err)
	}
	return generatedBuildRowToBuild(row), nil
}

// GetByIDForTenant is the tenant-scoped lookup. Returns ErrBuildNotFound
// when the build belongs to a different tenant — closing the IDOR vector
// at the DB layer (find_sec_idor_buildstore).
func (s *BuildStoreSqlc) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*db.Build, error) {
	row, err := s.q.GetBuildByIDForTenant(ctx, s.gen(nil), generated.GetBuildByIDForTenantParams{
		ID:       buildID,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopruntimeerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("sqlc: get build by id for tenant: %w", err)
	}
	return generatedBuildRowToBuild(row), nil
}

// HasActiveBuild checks if there is a build in QUEUED, RUNNING, or
// RELEASING status for the (tenant, project, env). Runs inside the
// advisory lock transaction (sqlctxprop gate validates tx form).
func (s *BuildStoreSqlc) HasActiveBuild(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (bool, error) {
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
// Returns loopruntimeerrors.InvalidTransition on invalid transitions.
func (s *BuildStoreSqlc) UpdateStatus(ctx context.Context, tx db.DBTX, buildID, event string) (*db.Build, error) {
	// M9 5b (ADR 2026-07-03-m9-5b-nil-tx-status-atomicity): this read-modify-write
	// MUST run in the caller's transaction under withEnvironmentLock. A nil tx
	// would split GetByIDTx + UPDATE across two pooled connections. Lock-free
	// callers use UpdateStatusIfCurrent (single-statement CAS) instead.
	if tx == nil {
		return nil, db.ErrNilTxStatusMutation
	}
	current, err := s.getBuildByIDViaTx(ctx, tx, buildID)
	if err != nil {
		return nil, err
	}

	newPhase, err := buildMachine.SafeTransition(
		statemachine.BuildPhase(current.Status),
		statemachine.BuildPhaseEvent(event),
	)
	if err != nil {
		return nil, loopruntimeerrors.NewInvalidTransition(current.Status, event)
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

// UpdateStatusIfCurrent is the lock-free, single-statement CAS variant of
// UpdateStatus (M9 5b, ADR 2026-07-03-m9-5b-nil-tx-status-atomicity). It is for
// the two callers that legitimately run OUTSIDE withEnvironmentLock
// (build_orchestrator_submit.go: QUEUED→RUNNING after workflow submit, and
// failQueuedBuild QUEUED→FAILED on submit failure). Unlike UpdateStatus it does
// NO fetch: the caller supplies the expected `from` phase, SafeTransition
// computes the target in Go (MA-7 — the transition table stays out of SQL), and
// a single conditional UPDATE (WHERE id AND tenant_id AND status=from) applies
// the write atomically iff the row is still in `from`. 0 rows → (nil, nil): the
// build already left `from` (a concurrent lock-holder won) → a correct no-op.
func (s *BuildStoreSqlc) UpdateStatusIfCurrent(ctx context.Context, buildID, tenantID string, from statemachine.BuildPhase, event string) (*db.Build, error) {
	newPhase, err := buildMachine.SafeTransition(from, statemachine.BuildPhaseEvent(event))
	if err != nil {
		return nil, loopruntimeerrors.NewInvalidTransition(string(from), event)
	}

	var row generated.Build
	switch newPhase {
	case statemachine.BPhRunning:
		row, err = s.q.UpdateBuildStatusToRunningIfCurrent(ctx, s.gen(nil), generated.UpdateBuildStatusToRunningIfCurrentParams{
			Status:   string(newPhase),
			ID:       buildID,
			TenantID: tenantID,
			Status_2: string(from),
		})
	case statemachine.BPhSucceeded, statemachine.BPhFailed, statemachine.BPhCancelled:
		row, err = s.q.UpdateBuildStatusToTerminalIfCurrent(ctx, s.gen(nil), generated.UpdateBuildStatusToTerminalIfCurrentParams{
			Status:   string(newPhase),
			ID:       buildID,
			TenantID: tenantID,
			Status_2: string(from),
		})
	default:
		// RELEASING / CANCELLING are not lock-free CAS callers — guard misuse
		// loudly rather than silently falling through to a wrong query.
		return nil, fmt.Errorf("build_wrapper: UpdateStatusIfCurrent supports only RUNNING/terminal targets, got %q (from=%q event=%q)", newPhase, from, event)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // superseded: row already left `from` — correct no-op
		}
		return nil, fmt.Errorf("sqlc: update build status if current: %w", err)
	}
	return generatedBuildRowToBuild(row), nil
}

// SetErrorMessage stores the error message and stamps finished_at.
// Returns ErrBuildNotFound when the row doesn't exist.
func (s *BuildStoreSqlc) SetErrorMessage(ctx context.Context, tx db.DBTX, buildID, errorMessage string) error {
	current, err := s.getBuildByIDViaTx(ctx, tx, buildID)
	if err != nil {
		return err
	}
	rows, err := s.q.SetBuildErrorMessage(ctx, s.gen(tx), generated.SetBuildErrorMessageParams{
		ErrorMessage: dbconv.StringEmptyToNilPtr(errorMessage),
		ID:           buildID,
		TenantID:     current.TenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: set build error message: %w", err)
	}
	if rows == 0 {
		return loopruntimeerrors.ErrBuildNotFound
	}
	return nil
}

// SetWorkflowID stores the Argo Workflow name. Returns ErrBuildNotFound
// when the row doesn't exist.
func (s *BuildStoreSqlc) SetWorkflowID(ctx context.Context, tx db.DBTX, buildID, workflowID string) error {
	current, err := s.getBuildByIDViaTx(ctx, tx, buildID)
	if err != nil {
		return err
	}
	rows, err := s.q.SetBuildWorkflowID(ctx, s.gen(tx), generated.SetBuildWorkflowIDParams{
		WorkflowID: dbconv.StringEmptyToNilPtr(workflowID),
		ID:         buildID,
		TenantID:   current.TenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: set build workflow id: %w", err)
	}
	if rows == 0 {
		return loopruntimeerrors.ErrBuildNotFound
	}
	return nil
}

// SetAppsSpec persists the canonicalized app spec JSON for the build.
func (s *BuildStoreSqlc) SetAppsSpec(ctx context.Context, tx db.DBTX, buildID string, apps []db.BuildAppSpec) error {
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
func (s *BuildStoreSqlc) GetBuildApps(ctx context.Context, buildID string) ([]db.BuildAppSpec, error) {
	data, err := s.q.GetBuildApps(ctx, s.gen(nil), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopruntimeerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("sqlc: get build apps: %w", err)
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(data, &apps); err != nil {
		return nil, fmt.Errorf("sqlc: unmarshal apps spec: %w", err)
	}
	return apps, nil
}

// ListByProject returns builds for a (tenant, project) newest first.
func (s *BuildStoreSqlc) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]db.Build, error) {
	rows, err := s.q.ListBuildsByProject(ctx, s.gen(nil), generated.ListBuildsByProjectParams{
		TenantID:  tenantID,
		ProjectID: projectID,
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: list builds by project: %w", err)
	}
	out := make([]db.Build, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedBuildRowToBuild(r))
	}
	return out, nil
}

// ListByProjectAndEnv returns (builds, totalCount, error). Empty env
// falls back to project-only list. Limit clamped to 1-50 (legacy parity).
func (s *BuildStoreSqlc) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]db.Build, int, error) {
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
	out := make([]db.Build, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedBuildRowToBuild(r))
	}
	return out, int(total), nil
}

// ListEnvsForProject returns the distinct env names the project has at
// least one build for, ordered by most-recent build per env DESC. Empty
// slice (not nil) for cold projects. Post plan
// 2026-06-08-dashboard-screens-review-fixes T2.2 + ADR D3.
// Caller MUST gate via ProjectStore.GetByName BEFORE invoking this method
// (EC-2 SEPA-CRITICAL cross-tenant existence-leak prevention) — the
// query itself is tenant-scoped but cannot detect cross-tenant probes.
func (s *BuildStoreSqlc) ListEnvsForProject(ctx context.Context, tenantID, projectName string) ([]string, error) {
	rows, err := s.q.ListBuildEnvsForProject(ctx, s.gen(nil), generated.ListBuildEnvsForProjectParams{
		TenantID:  tenantID,
		ProjectID: projectName,
	})
	if err != nil {
		return nil, fmt.Errorf("build store: list envs for project: %w", err)
	}
	out := make([]string, 0, len(rows))
	out = append(out, rows...)
	return out, nil
}

// FindActiveBuilds returns up to 100 QUEUED/RUNNING/RELEASING builds
// scoped to the given cell. Reaper path: intentionally cross-tenant
// within the cell. Empty cellID defaults to CellDefault per multi-cell
// backward compat.
func (s *BuildStoreSqlc) FindActiveBuilds(ctx context.Context, cellID string) ([]db.Build, error) {
	if cellID == "" {
		cellID = domain.DefaultCellID
	}
	rows, err := s.q.FindActiveBuildsByCell(ctx, s.gen(nil), cellID)
	if err != nil {
		return nil, fmt.Errorf("sqlc: find active builds: %w", err)
	}
	out := make([]db.Build, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedBuildRowToBuild(r))
	}
	return out, nil
}

// CancelActiveBuilds cancels QUEUED/RUNNING builds for (tenant, project,
// env). Returns count + workflow_ids (for Argo cancellation). Skips the
// CANCELLING intermediate state — intentional bulk-op shortcut for
// project deletion.
func (s *BuildStoreSqlc) CancelActiveBuilds(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (int, []string, error) {
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
func (s *BuildStoreSqlc) getBuildByIDViaTx(ctx context.Context, tx db.DBTX, buildID string) (*db.Build, error) {
	row, err := s.q.GetBuildByID(ctx, s.gen(tx), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopruntimeerrors.ErrBuildNotFound
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

func generatedBuildRowToBuild(row generated.Build) *db.Build {
	b := &db.Build{
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

// BuildStatsByDay returns the tenant-scoped per-UTC-day build counts since the
// given lower bound (J-06 D2 / MEET-2026-07-03-003). Read-only path → pool via
// gen(nil). The sqlc query's WHERE tenant_id predicate is the only isolation
// barrier under the BYPASSRLS engine pool (#106/#111).
func (s *BuildStoreSqlc) BuildStatsByDay(ctx context.Context, tenantID string, since time.Time) ([]generated.BuildStatsByDayRow, error) {
	return s.q.BuildStatsByDay(ctx, s.gen(nil), generated.BuildStatsByDayParams{
		TenantID:  tenantID,
		CreatedAt: pgtype.Timestamptz{Time: since, Valid: true},
	})
}
