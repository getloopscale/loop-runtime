// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// release_wrapper.go — thin Go wrapper around sqlc-generated Queries for
// the `releases` table, with state-machine integration preserved at the
// store layer per D4 of plan 2026-05-29-sqlc-migration-plan
// v1.3.
//
// T1.1c2b of plan v1.3 (Phase 1 pilot — M07 Rekor URL persistence 3-layer
// bug site). Satisfies db.ReleaseStore interface 1:1 so T1.1c2c can swap
// `NewPostgresReleaseStore` → `NewReleaseStoreSqlc` across 3 callers without
// service-side signature churn.
//
// Pattern (per ADR-1 D3 + D4 + sqlctxprop gate):
//   - With `emit_methods_with_db_argument: true` (D3), each generated
//     function takes `(ctx, db generated.DBTX, params)` — the caller
//     passes the pool OR a tx EVERY call (tx-required form).
//   - Internally, the wrapper bridges db.DBTX → generated.DBTX via the
//     SqlcBridge interface (PgxAdapter / PgxTxAdapter both implement it
//     via GeneratedDB()). Test fakes that satisfy db.DBTX without holding
//     a real pgx connection are intentionally unsupported — sqlc-backed
//     stores require pgxmock or a real PG instance for tests (per T0.6
//     reshape matrix).
//   - State-machine integration (UpdateStatus) lives HERE — the wrapper
//     fetches the current release tenant-isolated, runs
//     releaseMachine.SafeTransition, then invokes the UPDATE through tx.
//     This is structurally stronger than legacy PostgresReleaseStore,
//     which UPDATEs by id alone — wrapper enforces tenant_id predicate.
//
// Co-exists with PostgresReleaseStore in release_store.go during T1.1c2b;
// caller migration + release_store.go deletion happens in T1.1c2c.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/pkg/domain"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/id"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// activeUniqueIndexName is the constraint name from migration 000022.
// SQLSTATE 23505 violations matching this name indicate the partial
// unique index `(tenant_id, project_id, environment) WHERE
// status='ACTIVE'` rejected a second ACTIVE row — the
// at-most-one-ACTIVE-release-per-env invariant. T5.1 CV-M-1 of plan
// 2026-06-01-p3-infra-observability-test-debt v1.1.
const activeUniqueIndexName = "idx_releases_active_unique"

// isReleasesActiveUniqueViolation reports whether err is a Postgres
// unique-violation specifically against `idx_releases_active_unique`.
// Distinguished from generic 23505 (e.g., builds primary key collision)
// by inspecting the ConstraintName field — false-positives on other
// unique-index violations are NOT mapped to CONCURRENT_ACTIVATION.
//
// Used by UpdateStatus to surface the typed `CONCURRENT_ACTIVATION`
// error (pkg/errors.NewConcurrentActivation) when the partial unique
// index fires during a state transition to ACTIVE. The
// convergence-loop retry on next reconcile tick converges the race
// safely (loop is idempotent).
func isReleasesActiveUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code != "23505" {
		return false
	}
	return pgErr.ConstraintName == activeUniqueIndexName
}

// Compile-time assertion: ReleaseStoreSqlc satisfies the db.ReleaseStore
// interface 1:1 so T1.1c2c can swap NewPostgresReleaseStore →
// NewReleaseStoreSqlc without service-side signature churn.
var _ db.ReleaseStore = (*ReleaseStoreSqlc)(nil)

// ReleaseStoreSqlc is the sqlc-backed store wrapper for releases.
type ReleaseStoreSqlc struct {
	q          *generated.Queries
	pool       db.DBTX // default fallback for non-tx paths
	eventStore sseEventStore
}

// sseEventStore is the minimal interface AppendSideEffect needs from
// `sse.EventStore`. Declared locally (not imported) to avoid an import
// cycle between `db` and `sse` (sse already depends on db). T1.1 of plan
// 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1.
type sseEventStore interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
}

// NoopEventStore is a no-op implementation of sseEventStore for read-only
// store call-sites (e.g., ReleaseInspector) that never invoke
// AppendSideEffect. T1.3.b v1.1 EC-1 absorption: the non-nil contract on
// NewReleaseStoreSqlc surfaces mis-wired startups loud + fast, but
// genuinely read-only consumers need a canonical no-op to satisfy it
// without dragging the full PostgresEventStore dependency chain.
type NoopEventStore struct{}

// Append on NoopEventStore is a no-op returning (0, nil) — read-only
// callers that wire NoopEventStore by design never reach
// AppendSideEffect; if they do, the event is silently dropped (correct
// behavior for the no-op contract).
func (NoopEventStore) Append(_ context.Context, _ string, _ string, _ json.RawMessage) (int64, error) {
	return 0, nil
}

// NewReleaseStoreSqlc constructs a sqlc-backed release store. `pool` is the
// connection pool used when callers do NOT provide a tx. Mutations SHOULD
// always come with a tx per Rule 6 + Rule 7; the pool fallback is for
// read-only paths outside withEnvironmentLock. The concrete pool MUST
// implement SqlcBridge (PgxAdapter does); test fakes that don't will
// produce nil generated.DBTX and panic at query time — a deliberate
// failure mode to surface mis-wired tests early.
//
// `eventStore` is REQUIRED (non-nil) per T1.1 of plan
// 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1 (EC-1 absorption):
// AppendSideEffect emits `signal.changed` SSE events via this writer.
// Panics on nil to surface mis-wired startups loud + fast — prevents
// silent dead-feature where SSE realtime path appears installed but
// emits zero events forever.
func NewReleaseStoreSqlc(pool db.DBTX, eventStore sseEventStore) *ReleaseStoreSqlc {
	if eventStore == nil {
		panic("ReleaseStoreSqlc requires non-nil EventStore (canonical SSE infrastructure)")
	}
	return &ReleaseStoreSqlc{
		q:          generated.New(),
		pool:       pool,
		eventStore: eventStore,
	}
}

// gen resolves the sqlc handle (db.Generated): tx wins over pool. A handle
// that is not a SqlcBridge makes the query fail with db.ErrNotSqlcBridge.
func (s *ReleaseStoreSqlc) gen(tx db.DBTX) generated.DBTX {
	target := tx
	if target == nil {
		target = s.pool
	}
	return db.Generated(target)
}

// Create inserts a Release row. Caller MUST provide a tx when this is
// invoked inside withEnvironmentLock (sqlctxprop gate enforces).
func (s *ReleaseStoreSqlc) Create(ctx context.Context, tx db.DBTX, r *db.Release) error {
	// V9 dogfood 2026-05-29 LIVE-caught: sqlc-emitted InsertRelease
	// forces explicit values for columns that PG schema declares as
	// DEFAULT-bound. The legacy handwritten SQL relied on PG defaults
	// to populate (created_at default now(), status default 'READY').
	// sqlc-generated INSERT VALUES list passes every column → zero
	// Go values become PG NULL/empty → constraint violations.
	//
	// Defaults applied wrapper-side (mirror PG defaults + legacy
	// PostgresReleaseStore.Create behavior):
	//   - id "" → id.GenerateReleaseID() (legacy generated; primary
	//     key MUST be unique — empty string would collide on the 2nd
	//     retry of the BuildReconcileLoop, violating releases_pkey)
	//   - created_at zero → time.Now().UTC()
	//     (schema: NOT NULL DEFAULT now())
	//   - status "" → "READY"
	//     (schema: CHECK status IN ('READY', 'PENDING', ...);
	//      statemachine.ReleaseReady is the initial state)
	if r.ID == "" {
		r.ID = id.GenerateReleaseID()
	}
	createdAt := r.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	status := r.Status
	if status == "" {
		// Mirror pkg/statemachine/release.go: ReleaseReady = "READY"
		// (initial state of the release lifecycle, before
		// GitMirrorCompleted → PENDING → ACTIVE).
		status = "READY"
	}
	params := generated.InsertReleaseParams{
		ID:            r.ID,
		BuildID:       r.BuildID,
		TenantID:      r.TenantID,
		ProjectID:     r.ProjectID,
		Environment:   r.Environment,
		Status:        status,
		CorrelationID: r.CorrelationID,
		CreatedBy:     r.CreatedBy,
		CreatedAt:     pgtype.Timestamptz{Time: createdAt, Valid: true},
		// Status defaulted above.

		GitCommit:                dbconv.StringEmptyToNilPtr(r.GitCommit),
		GitMirrorStatus:          r.GitMirrorStatus,
		Apps:                     r.Apps,
		Artifacts:                r.Artifacts,
		SideEffects:              r.SideEffects,
		ImageDigest:              r.ImageDigest,
		ImageSignature:           r.ImageSignature,
		ImageSignedAt:            dbconv.TimePtrToTimestamptz(r.ImageSignedAt),
		ImageAttestationRekorUrl: r.ImageAttestationRekorURL,
	}

	row, err := s.q.InsertRelease(ctx, s.gen(tx), params)
	if err != nil {
		return fmt.Errorf("sqlc: insert release: %w", err)
	}
	// Mirror generated row back into the caller-owned Release struct.
	r.ID = row.ID
	r.Status = row.Status
	r.CreatedAt = row.CreatedAt.Time
	return nil
}

// GetByID is a cross-tenant lookup (used by reapers / admin paths). Marked
// `-- tenantpredicate:exempt` in queries/releases.sql. Returns
// ErrReleaseNotFound (via loopzeroerrors) when the row doesn't exist, matching
// the legacy PostgresReleaseStore contract.
func (s *ReleaseStoreSqlc) GetByID(ctx context.Context, releaseID string) (*db.Release, error) {
	row, err := s.q.GetReleaseByID(ctx, s.gen(nil), releaseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrReleaseNotFound
		}
		return nil, fmt.Errorf("sqlc: get release by id: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetByIDTx retrieves a release by ID within a transaction. Used by
// ActivationKernel.Activate idempotency guard (MEET-2026-03-31-023 T0-1).
// Cross-tenant by ID, like GetByID.
func (s *ReleaseStoreSqlc) GetByIDTx(ctx context.Context, tx db.DBTX, releaseID string) (*db.Release, error) {
	row, err := s.q.GetReleaseByID(ctx, s.gen(tx), releaseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrReleaseNotFound
		}
		return nil, fmt.Errorf("sqlc: get release by id (tx): %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetByIDForTenant is the canonical tenant-isolated lookup.
func (s *ReleaseStoreSqlc) GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*db.Release, error) {
	row, err := s.q.GetReleaseByIDForTenant(ctx, s.gen(nil), generated.GetReleaseByIDForTenantParams{
		ID:       releaseID,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrReleaseNotFound
		}
		return nil, fmt.Errorf("sqlc: get release by id for tenant: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// UpdateStatus validates a state transition via the typed state machine,
// then updates the release status in the database. Tenant-isolated UPDATE
// (UPDATE … WHERE id=$2 AND tenant_id=$3) — structurally stronger than
// legacy PostgresReleaseStore which matched by id alone.
//
// D4 invariant: SafeTransition runs at the store layer (not in handlers).
// Returns loopzeroerrors.ErrReleaseNotFound if the release doesn't exist.
// Returns loopzeroerrors.InvalidTransition if the event is not valid from
// the current state.
func (s *ReleaseStoreSqlc) UpdateStatus(ctx context.Context, tx db.DBTX, releaseID, event string) (*db.Release, error) {
	// M9 5b (ADR 2026-07-03-m9-5b-nil-tx-status-atomicity): this read-modify-write
	// (fetch → SafeTransition → UPDATE into the partial-unique-index) MUST run in
	// the caller's transaction under withEnvironmentLock. All 25 release callers +
	// the reaper already pass a real tx; a nil tx is a wiring bug, not a fallback.
	if tx == nil {
		return nil, db.ErrNilTxStatusMutation
	}
	// 1. Fetch current release (cross-tenant fetch; we need its tenant_id
	// to scope the UPDATE — matches legacy behavior that UPDATE-ed by id
	// without enforcing tenant predicate, but we ADD the predicate for
	// defense-in-depth).
	current, err := s.GetByIDTx(ctx, tx, releaseID)
	if err != nil {
		return nil, err
	}

	// 2. Validate transition via typed state machine — panic-trapped to
	// statemachine.ErrInvalidTransition, then re-wrapped to
	// loopzeroerrors.InvalidTransition for backward compat.
	newPhase, err := releaseMachine.SafeTransition(
		statemachine.ReleasePhase(current.Status),
		statemachine.ReleasePhaseEvent(event),
	)
	if err != nil {
		return nil, loopzeroerrors.NewInvalidTransition(current.Status, event)
	}

	// 3. UPDATE through tx with tenant predicate.
	row, err := s.q.UpdateReleaseStatus(ctx, s.gen(tx), generated.UpdateReleaseStatusParams{
		Status:   string(newPhase),
		ID:       releaseID,
		TenantID: current.TenantID,
	})
	if err != nil {
		// T5.1 CV-M-1: detect SQLSTATE 23505 against the partial
		// unique index `idx_releases_active_unique` (migration 000022)
		// and surface the typed CONCURRENT_ACTIVATION error so callers
		// can map to HTTP 409 (registered in pkg/errors/http/mapping)
		// and the CLI can show a specific message. Other 23505
		// violations (e.g., a future PK collision) fall through to the
		// generic wrapped error.
		if isReleasesActiveUniqueViolation(err) {
			return nil, loopzeroerrors.NewConcurrentActivation(current.ProjectID, current.Environment)
		}
		return nil, fmt.Errorf("sqlc: update release status: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetActiveByEnv returns the currently-ACTIVE release for an env tuple. At
// most one ACTIVE release per (tenant, project, env) at any time.
func (s *ReleaseStoreSqlc) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error) {
	row, err := s.q.GetActiveByEnv(ctx, s.gen(nil), generated.GetActiveByEnvParams{
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

// GetActiveByEnvTx returns the currently-ACTIVE release for an env tuple
// within a transaction. Used by ActivationKernel.Activate.
func (s *ReleaseStoreSqlc) GetActiveByEnvTx(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (*db.Release, error) {
	row, err := s.q.GetActiveByEnv(ctx, s.gen(tx), generated.GetActiveByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlc: get active by env (tx): %w", err)
	}
	return generatedRowToRelease(row), nil
}

// GetPreviousRelease returns the most-recent SUPERSEDED-or-ROLLED-BACK
// release — the canonical rollback target for an env.
func (s *ReleaseStoreSqlc) GetPreviousRelease(ctx context.Context, tenantID, projectID, env string) (*db.Release, error) {
	row, err := s.q.GetPreviousRelease(ctx, s.gen(nil), generated.GetPreviousReleaseParams{
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

// GetLatestByBuildID returns the most-recent release for a build_id
// (cross-tenant; caller validates ownership). Used by release_inspector +
// reconcile. Returns (nil, nil) when no release exists for the build.
func (s *ReleaseStoreSqlc) GetLatestByBuildID(ctx context.Context, buildID string) (*db.Release, error) {
	row, err := s.q.GetLatestByBuildID(ctx, s.gen(nil), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("sqlc: get latest by build_id: %w", err)
	}
	return generatedRowToRelease(row), nil
}

// ListByEnv returns up to `limit` releases for a (tenant, project, env)
// tuple. The `int` parameter is cast to int32 for sqlc compat.
func (s *ReleaseStoreSqlc) ListByEnv(ctx context.Context, tenantID, projectID, env string, limit int) ([]db.Release, error) {
	rows, err := s.q.ListReleasesByEnv(ctx, s.gen(nil), generated.ListReleasesByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
		Limit:       int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: list releases by env: %w", err)
	}
	out := make([]db.Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// SetGitCommit records the git commit SHA on a release after the gitops
// push. Tenant predicate enforced via internal GetByIDTx lookup.
func (s *ReleaseStoreSqlc) SetGitCommit(ctx context.Context, tx db.DBTX, releaseID, commitSHA string) error {
	current, err := s.GetByIDTx(ctx, tx, releaseID)
	if err != nil {
		return err
	}
	_, err = s.q.SetReleaseGitCommit(ctx, s.gen(tx), generated.SetReleaseGitCommitParams{
		GitCommit: dbconv.StringEmptyToNilPtr(commitSHA),
		ID:        releaseID,
		TenantID:  current.TenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: set git commit: %w", err)
	}
	return nil
}

// AppendSideEffect merges a JSON object into the release's side_effects
// column via PostgreSQL jsonb concat (`||`). Repeated keys overwrite.
// Idempotent: re-running with the same patch is a no-op semantically.
// fix-all-platform-issues-plan T4.1.
func (s *ReleaseStoreSqlc) AppendSideEffect(ctx context.Context, tx db.DBTX, id string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("sqlc: marshal side_effects patch: %w", err)
	}
	current, err := s.GetByIDTx(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = s.q.AppendReleaseSideEffect(ctx, s.gen(tx), generated.AppendReleaseSideEffectParams{
		Column1:  patchJSON,
		ID:       id,
		TenantID: current.TenantID,
	})
	if err != nil {
		return fmt.Errorf("sqlc: append side effect: %w", err)
	}

	// T1.1 of plan 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1:
	// emit `signal.changed` SSE event via canonical EventStore so J-18
	// useStatusStream consumer gets realtime updates. Best-effort emit
	// (does NOT fail tx — release row already updated; SSE replay is
	// at-least-once per EventStore contract). EC-3 absorption: structured
	// slog.Warn on emit failure — NOT silent `_, _` discard which violates
	// feedback_swallowed_silent_errors_pattern.
	if _, emitErr := s.eventStore.Append(ctx, id, "signal.changed", patchJSON); emitErr != nil {
		slog.Warn("AppendSideEffect: best-effort SSE emit failed",
			"release_id", id,
			"tenant_id", current.TenantID,
			"err", emitErr)
	}

	return nil
}

// CancelAllByEnv cancels the releases of a (tenant, project, env) tuple as the
// project-delete cascade: a release moves to CANCELLED only when the release
// machine has a ProjectDeleted edge from its status (so ARCHIVED and
// CANCELLED, both terminal, are left alone). It reads the statuses present,
// lets the machine choose, and updates only those — the transition table
// never leaks into SQL. Returns the COUNT of releases cancelled (the
// ProjectDeleter consumer checks cancelledReleases > 0). Runs on the caller's
// tx, under the environment lock.
func (s *ReleaseStoreSqlc) CancelAllByEnv(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (int, error) {
	present, err := s.q.ListReleaseStatusesByEnv(ctx, s.gen(tx), generated.ListReleaseStatusesByEnvParams{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Environment: env,
	})
	if err != nil {
		return 0, fmt.Errorf("sqlc: list release statuses by env: %w", err)
	}
	from := statusesCancelledOnProjectDelete(present)
	if len(from) == 0 {
		return 0, nil
	}
	ids, err := s.q.CancelReleasesByEnv(ctx, s.gen(tx), generated.CancelReleasesByEnvParams{
		TenantID:     tenantID,
		ProjectID:    projectID,
		Environment:  env,
		FromStatuses: from,
	})
	if err != nil {
		return 0, fmt.Errorf("sqlc: cancel releases by env: %w", err)
	}
	return len(ids), nil
}

// statusesCancelledOnProjectDelete keeps, in order, the statuses from which
// the release machine moves to CANCELLED on ProjectDeleted. A status the
// machine does not know is never cancelled.
func statusesCancelledOnProjectDelete(statuses []string) []string {
	out := make([]string, 0, len(statuses))
	for _, st := range statuses {
		next, err := releaseMachine.TryTransition(statemachine.ReleasePhase(st), statemachine.RPEventProjectDeleted)
		if err == nil && next == statemachine.RPhCancelled {
			out = append(out, st)
		}
	}
	return out
}

// FindPendingReleases returns up to 100 PENDING releases scoped to the
// given cell. Reaper path: intentionally cross-tenant within the cell.
// Empty cellID defaults to CellDefault per multi-cell backward compat.
func (s *ReleaseStoreSqlc) FindPendingReleases(ctx context.Context, cellID string) ([]db.Release, error) {
	if cellID == "" {
		cellID = domain.DefaultCellID
	}
	rows, err := s.q.FindPendingReleasesByCell(ctx, s.gen(nil), cellID)
	if err != nil {
		return nil, fmt.Errorf("sqlc: find pending releases: %w", err)
	}
	out := make([]db.Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// FindActiveReleases returns up to 100 ACTIVE releases scoped to the given
// cell, DISTINCT ON (tenant, project, env) → most-recent per tuple. Used
// by the convergence-loop health ticker (MEET-2026-03-31-023 T1-4).
func (s *ReleaseStoreSqlc) FindActiveReleases(ctx context.Context, cellID string) ([]db.Release, error) {
	if cellID == "" {
		cellID = domain.DefaultCellID
	}
	rows, err := s.q.FindActiveReleasesByCell(ctx, s.gen(nil), cellID)
	if err != nil {
		return nil, fmt.Errorf("sqlc: find active releases: %w", err)
	}
	out := make([]db.Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// FindPendingReleasesByTenantEnv returns PENDING releases for a specific
// (tenant, env). Used by the callback-driven activation path
// (CODE-SCAN-001). Satisfies the optional PendingReleaseByTenantEnvFinder
// type-asserted by sync_callback_activator.
func (s *ReleaseStoreSqlc) FindPendingReleasesByTenantEnv(ctx context.Context, tenantID, env string) ([]db.Release, error) {
	rows, err := s.q.FindPendingReleasesByTenantEnv(ctx, s.gen(nil), generated.FindPendingReleasesByTenantEnvParams{
		TenantID:    tenantID,
		Environment: env,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlc: find pending releases by tenant+env: %w", err)
	}
	out := make([]db.Release, 0, len(rows))
	for _, r := range rows {
		out = append(out, *generatedRowToRelease(r))
	}
	return out, nil
}

// -----------------------------------------------------------------------
// Conversion helpers between db.Release (legacy public type) and the
// sqlc-generated row type. We mirror nullable columns back and forth so
// existing callers continue to consume the unchanged db.Release shape
// during T1.1c2b. T1.1c2c migrates the 3 prod callers off
// PostgresReleaseStore to ReleaseStoreSqlc; db.Release stays.
// -----------------------------------------------------------------------

func generatedRowToRelease(row generated.Release) *db.Release {
	r := &db.Release{
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
