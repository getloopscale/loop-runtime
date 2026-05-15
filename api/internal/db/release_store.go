// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/pkg/id"
	"github.com/loopzero/loopzero/pkg/statemachine"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// releaseMachine is the release state machine singleton.
// Used to validate all status transitions.
var releaseMachine = statemachine.NewReleaseMachine()

// Release represents a release record in the database.
type Release struct {
	ID              string          `json:"id"`
	BuildID         string          `json:"build_id"`
	TenantID        string          `json:"tenant_id"`
	ProjectID       string          `json:"project_id"`
	Environment     string          `json:"environment"`
	Status          string          `json:"status"`
	CorrelationID   string          `json:"correlation_id"`
	CreatedBy       string          `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	GitCommit       string          `json:"git_commit,omitempty"`
	GitMirrorStatus string          `json:"git_mirror_status"`
	Apps            json.RawMessage `json:"apps,omitempty"`
	Artifacts       json.RawMessage `json:"artifacts,omitempty"`
	SideEffects     json.RawMessage `json:"side_effects,omitempty"`

	// CHG-2026-04-25-ATTESTATION-WIRE: supply-chain attestation fields.
	// All three are populated by phaseACreateRelease when the build
	// workflow surfaced cosign+digest outputs (Phase 1+2 of the change).
	// NULL on legacy releases (workflows pre-this-change). Never set
	// SignedAt without Signature — the timestamp without the signature
	// reference is meaningless. ImageDigest is the cryptographic sha256,
	// distinct from the legacy `image_digest` aliasing the tag in
	// AppRelease.ImageDigest.
	ImageDigest    *string    `json:"image_digest,omitempty"`
	ImageSignature *string    `json:"image_signature,omitempty"`
	ImageSignedAt  *time.Time `json:"image_signed_at,omitempty"`
}

// ReleaseStore defines the interface for release persistence operations.
//
// NOTE: a `releaseSelectColumns` const was introduced in CHG-2026-04-25-
// ATTESTATION-WIRE (commit 1f093272) intending to centralize the 9+
// scattered SELECT column lists. The refactor was never completed (zero
// call sites), so the orphan const was removed 2026-05-15 iter 14. The
// centralization remains valuable future work — when re-introducing,
// ensure every SELECT query in this file consumes it.
type ReleaseStore interface {
	Create(ctx context.Context, tx DBTX, release *Release) error
	GetByID(ctx context.Context, id string) (*Release, error)
	UpdateStatus(ctx context.Context, tx DBTX, id string, event string) (*Release, error)
	// AppendSideEffect merges a JSON object into the release's side_effects column.
	// Used by the release reaper (T4.1) to capture transition reasons in the
	// audit trail. Idempotent: re-running with the same key overwrites that key
	// only; other keys are preserved (jsonb || jsonb merge).
	// fix-all-platform-issues-plan T4.1.
	AppendSideEffect(ctx context.Context, tx DBTX, id string, patch map[string]any) error
	SetGitCommit(ctx context.Context, tx DBTX, id string, commitSHA string) error
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*Release, error)
	GetActiveByEnvTx(ctx context.Context, tx DBTX, tenantID, projectID, env string) (*Release, error)
	GetPreviousRelease(ctx context.Context, tenantID, projectID, env string) (*Release, error)
	GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*Release, error)
	ListByEnv(ctx context.Context, tenantID, projectID, env string, limit int) ([]Release, error)
	// GetLatestByBuildID returns the most recent release for a build, or nil if none exists.
	GetLatestByBuildID(ctx context.Context, buildID string) (*Release, error)
	// FindPendingReleases returns releases in PENDING state (awaiting ArgoCD sync
	// verification) for the given cell. Used by the Release Convergence Loop
	// (MEET-2026-03-27-012 Phase 1.1). Roadmap 03 adds the cell_id filter so
	// multi-cell controllers stay isolated. Pass CellDefault for single-cell
	// backward compatibility.
	FindPendingReleases(ctx context.Context, cellID string) ([]Release, error)
	// FindActiveReleases returns releases in ACTIVE state for the given cell,
	// used by the health check ticker (MEET-2026-03-31-023 T1-4) to detect
	// post-activation ArgoCD divergence.
	FindActiveReleases(ctx context.Context, cellID string) ([]Release, error)
	// GetByIDTx retrieves a release by ID within a transaction.
	// MEET-2026-03-31-023 T0-1: Used by ActivateReleaseKernel idempotency guard.
	GetByIDTx(ctx context.Context, tx DBTX, id string) (*Release, error)
	// CancelAllByEnv transitions every non-terminal release for the given
	// (tenant, project, env) to CANCELLED via the state-machine
	// ProjectDeleted event. Used by ProjectDeleter so future GetActiveByEnv
	// lookups don't return zombie rows after the workload is gone.
	// Returns the number of releases cancelled. Already-CANCELLED rows are
	// skipped silently. Failures on individual rows are logged but don't
	// abort the cascade — best-effort cleanup.
	CancelAllByEnv(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, error)
}

// PostgresReleaseStore implements ReleaseStore using PostgreSQL.
type PostgresReleaseStore struct {
	db DBTX
}

// NewPostgresReleaseStore creates a new PostgresReleaseStore.
func NewPostgresReleaseStore(db DBTX) *PostgresReleaseStore {
	return &PostgresReleaseStore{db: db}
}

// Create inserts a new release with status READY.
// If release.ID is empty, a new ID is generated via pkg/id.
func (s *PostgresReleaseStore) Create(ctx context.Context, tx DBTX, release *Release) error {
	if release.ID == "" {
		release.ID = id.GenerateReleaseID()
	}
	release.Status = string(statemachine.ReleaseReady)

	// CHG-2026-04-25-ATTESTATION-WIRE: persist signature columns when
	// the caller populated them (Phase 5 in container_delivery). nil
	// pointers are passed through pgx as NULL; legacy callers that
	// don't set them still get a working insert.
	_, err := tx.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, apps, artifacts, image_digest, image_signature, image_signed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		release.ID, release.BuildID, release.TenantID, release.ProjectID,
		release.Environment, release.Status, release.CorrelationID, release.CreatedBy, release.Apps, release.Artifacts,
		release.ImageDigest, release.ImageSignature, release.ImageSignedAt,
	)
	if err != nil {
		return fmt.Errorf("release store: insert failed: %w", err)
	}

	return nil
}

// GetByID retrieves a release by its ID.
// Returns ErrReleaseNotFound if the release does not exist.
func (s *PostgresReleaseStore) GetByID(ctx context.Context, releaseID string) (*Release, error) {
	return s.scanRelease(s.db.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases WHERE id = $1`,
		releaseID,
	))
}

// GetByIDTx retrieves a release by its ID within a transaction.
// MEET-2026-03-31-023 T0-1: Used by ActivateReleaseKernel to re-fetch
// release state inside the advisory lock, preventing duplicate activation
// when SyncCallbackActivator and ReleaseConvergenceLoop race.
func (s *PostgresReleaseStore) GetByIDTx(ctx context.Context, tx DBTX, releaseID string) (*Release, error) {
	executor := tx
	if executor == nil {
		executor = s.db
	}
	return s.scanRelease(executor.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases WHERE id = $1`,
		releaseID,
	))
}

// UpdateStatus validates a state transition via the release state machine,
// then updates the release status in the database.
// Returns ErrReleaseNotFound if the release does not exist.
// Returns ErrInvalidTransition if the event is not valid from the current state.
func (s *PostgresReleaseStore) UpdateStatus(ctx context.Context, tx DBTX, releaseID string, event string) (*Release, error) {
	// Use pool if no transaction provided (e.g., outside lock)
	executor := tx
	if executor == nil {
		executor = s.db
	}
	// Fetch current release
	release, err := s.scanRelease(executor.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases WHERE id = $1`,
		releaseID,
	))
	if err != nil {
		return nil, err
	}

	// Validate transition via state machine
	newState, err := safeTransition(releaseMachine, statemachine.State(release.Status), statemachine.Event(event))
	if err != nil {
		return nil, err
	}

	// Update in DB
	_, err = executor.Exec(ctx,
		`UPDATE releases SET status = $1 WHERE id = $2`,
		string(newState), releaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("release store: update status failed: %w", err)
	}

	release.Status = string(newState)
	return release, nil
}

// AppendSideEffect merges a JSON object into the release's side_effects.
// fix-all-platform-issues-plan T4.1: capture reaper transition reasons in
// the audit trail so future RCAs have actionable data instead of just a
// status code. Implementation uses PostgreSQL `||` jsonb operator —
// repeated keys are overwritten, others preserved.
func (s *PostgresReleaseStore) AppendSideEffect(ctx context.Context, tx DBTX, id string, patch map[string]any) error {
	if len(patch) == 0 {
		return nil
	}
	executor := tx
	if executor == nil {
		executor = s.db
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("release store: marshal side_effects patch: %w", err)
	}
	_, err = executor.Exec(ctx,
		`UPDATE releases SET side_effects = COALESCE(side_effects, '{}'::jsonb) || $1::jsonb WHERE id = $2`,
		string(patchJSON), id,
	)
	if err != nil {
		return fmt.Errorf("release store: append side_effects: %w", err)
	}
	return nil
}

// CancelAllByEnv cascades the ProjectDeleted event to every non-terminal
// release row for the (tenant, project, env) tuple. The release state machine
// allows ProjectDeleted from READY/PENDING/ACTIVE/SUPERSEDED/ROLLED_BACK and
// every *_FAILED state, all converging on CANCELLED. CANCELLED rows are
// skipped via the WHERE filter. Returns the count of cancelled rows.
//
// Atomicity: the caller is responsible for passing the same transaction used
// for cancel-builds + delete-environment-target. If any individual transition
// fails (e.g. unexpected state), it is logged via the wrapped error but the
// cascade continues — we prefer best-effort cleanup over leaving a single
// failure to block the whole project delete.
func (s *PostgresReleaseStore) CancelAllByEnv(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, error) {
	executor := tx
	if executor == nil {
		executor = s.db
	}

	rows, err := executor.Query(ctx,
		`SELECT id, status FROM releases
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
		   AND status <> 'CANCELLED'`,
		tenantID, projectID, env,
	)
	if err != nil {
		return 0, fmt.Errorf("release store: list non-terminal for cancel cascade: %w", err)
	}
	type row struct {
		id     string
		status string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.status); err != nil {
			rows.Close()
			return 0, fmt.Errorf("release store: scan cancel cascade row: %w", err)
		}
		pending = append(pending, r)
	}
	rows.Close()

	cancelled := 0
	for _, r := range pending {
		newState, err := safeTransition(releaseMachine, statemachine.State(r.status), statemachine.ReleaseProjectDeleted)
		if err != nil {
			// Skip silently — state machine forbids this transition (e.g. an
			// unknown future state). Better to keep cleaning the rest than abort.
			continue
		}
		_, err = executor.Exec(ctx,
			`UPDATE releases SET status = $1 WHERE id = $2`,
			string(newState), r.id,
		)
		if err != nil {
			return cancelled, fmt.Errorf("release store: cancel cascade update %s: %w", r.id, err)
		}
		cancelled++
	}
	return cancelled, nil
}

// SetGitCommit persists the git commit SHA from the GitOps push on a release.
// LEVE-2: Enables end-to-end audit trail from build → release → git commit.
func (s *PostgresReleaseStore) SetGitCommit(ctx context.Context, tx DBTX, releaseID string, commitSHA string) error {
	executor := tx
	if executor == nil {
		executor = s.db
	}
	_, err := executor.Exec(ctx,
		`UPDATE releases SET git_commit = $1 WHERE id = $2`,
		commitSHA, releaseID,
	)
	if err != nil {
		return fmt.Errorf("release store: set git_commit failed: %w", err)
	}
	return nil
}

// GetActiveByEnv retrieves the active release for a given environment.
// Returns nil (not error) when no active release exists.
func (s *PostgresReleaseStore) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*Release, error) {
	release, err := s.scanRelease(s.db.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND status = 'ACTIVE'
		 ORDER BY created_at DESC
		 LIMIT 1`,
		tenantID, projectID, env,
	))
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
			return nil, nil // Not found is not an error for GetActiveByEnv
		}
		return nil, err
	}
	return release, nil
}

// GetActiveByEnvTx retrieves the active release using the provided transaction.
// MOD-4 fix: ensures consistent read within the advisory lock transaction.
func (s *PostgresReleaseStore) GetActiveByEnvTx(ctx context.Context, tx DBTX, tenantID, projectID, env string) (*Release, error) {
	release, err := s.scanRelease(tx.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3 AND status = 'ACTIVE'
		 ORDER BY created_at ASC
		 LIMIT 1`,
		tenantID, projectID, env,
	))
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return release, nil
}

// GetPreviousRelease retrieves the most recent SUPERSEDED or ROLLED_BACK release
// for a given environment. Used by rollback to find the target release.
// Returns nil (not error) when no previous release exists.
func (s *PostgresReleaseStore) GetPreviousRelease(ctx context.Context, tenantID, projectID, env string) (*Release, error) {
	release, err := s.scanRelease(s.db.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
		   AND status IN ('SUPERSEDED', 'ROLLED_BACK')
		 ORDER BY created_at DESC
		 LIMIT 1`,
		tenantID, projectID, env,
	))
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return release, nil
}

// GetByIDForTenant retrieves a release by ID with tenant isolation.
// Returns nil (not error) when not found or tenant doesn't match.
func (s *PostgresReleaseStore) GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*Release, error) {
	release, err := s.scanRelease(s.db.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases WHERE id = $1 AND tenant_id = $2`,
		releaseID, tenantID,
	))
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return release, nil
}

// ListByEnv retrieves releases for an environment, ordered by creation date DESC.
// Includes ACTIVE, SUPERSEDED, and ROLLED_BACK releases (available for rollback).
func (s *PostgresReleaseStore) ListByEnv(ctx context.Context, tenantID, projectID, env string, limit int) ([]Release, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
		   AND status IN ('ACTIVE', 'SUPERSEDED', 'ROLLED_BACK')
		 ORDER BY created_at DESC
		 LIMIT $4`,
		tenantID, projectID, env, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("release store: list by env failed: %w", err)
	}
	defer rows.Close()

	var releases []Release
	for rows.Next() {
		var r Release
		if err := scanReleaseFields(rows, &r); err != nil {
			return nil, fmt.Errorf("release store: scan release in list: %w", err)
		}
		releases = append(releases, r)
	}
	return releases, nil
}

// GetLatestByBuildID returns the most recent release for a given build ID.
// Returns nil (not error) if no release exists for this build.
func (s *PostgresReleaseStore) GetLatestByBuildID(ctx context.Context, buildID string) (*Release, error) {
	r, err := s.scanRelease(s.db.QueryRow(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases WHERE build_id = $1
		 ORDER BY created_at DESC LIMIT 1`,
		buildID,
	))
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
			return nil, nil // No release for this build — not an error
		}
		return nil, err
	}
	return r, nil
}

// FindPendingReleases returns releases in PENDING state belonging to the
// given cell, limited to 100 rows to prevent unbounded result sets under
// load (CODE-001 fix). These are releases where GitOps push succeeded but
// ArgoCD sync verification has not yet completed; the Release Convergence
// Loop processes them. The cell_id filter (roadmap 03, migration 000029)
// ensures each controller only processes its own cell's work.
func (s *PostgresReleaseStore) FindPendingReleases(ctx context.Context, cellID string) ([]Release, error) {
	if cellID == "" {
		cellID = CellDefault
	}
	rows, err := s.db.Query(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE cell_id = $1
		   AND status = 'PENDING'
		 ORDER BY created_at ASC
		 LIMIT 100`,
		cellID,
	)
	if err != nil {
		return nil, fmt.Errorf("release store: find pending releases: %w", err)
	}
	defer rows.Close()

	var releases []Release
	for rows.Next() {
		var r Release
		if err := scanReleaseFields(rows, &r); err != nil {
			return nil, fmt.Errorf("release store: scan pending release: %w", err)
		}
		releases = append(releases, r)
	}
	if releases == nil {
		releases = []Release{}
	}
	return releases, nil
}

// FindActiveReleases returns releases in ACTIVE state for the given cell,
// limited to 100 rows. MEET-2026-03-31-023 T1-4: used by the convergence
// loop health ticker to check post-activation ArgoCD health. Returns one
// ACTIVE release per (tenant, project, env) combination (the most recent one).
// Roadmap 03 adds the cell_id filter for multi-cell isolation.
func (s *PostgresReleaseStore) FindActiveReleases(ctx context.Context, cellID string) ([]Release, error) {
	if cellID == "" {
		cellID = CellDefault
	}
	rows, err := s.db.Query(ctx,
		`SELECT DISTINCT ON (tenant_id, project_id, environment)
		        id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE cell_id = $1
		   AND status = 'ACTIVE'
		 ORDER BY tenant_id, project_id, environment, created_at DESC
		 LIMIT 100`,
		cellID,
	)
	if err != nil {
		return nil, fmt.Errorf("release store: find active releases: %w", err)
	}
	defer rows.Close()

	var releases []Release
	for rows.Next() {
		var r Release
		if err := scanReleaseFields(rows, &r); err != nil {
			return nil, fmt.Errorf("release store: scan active release: %w", err)
		}
		releases = append(releases, r)
	}
	if releases == nil {
		releases = []Release{}
	}
	return releases, nil
}

// FindPendingReleasesByTenantEnv returns PENDING releases filtered by tenant and environment.
// More efficient than FindPendingReleases for callback-driven activation (CODE-SCAN-001 fix).
func (s *PostgresReleaseStore) FindPendingReleasesByTenantEnv(ctx context.Context, tenantID, env string) ([]Release, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb),
		        image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE status = 'PENDING' AND tenant_id = $1 AND environment = $2
		 ORDER BY created_at ASC`,
		tenantID, env,
	)
	if err != nil {
		return nil, fmt.Errorf("release store: find pending releases by tenant+env: %w", err)
	}
	defer rows.Close()

	var releases []Release
	for rows.Next() {
		var r Release
		if err := scanReleaseFields(rows, &r); err != nil {
			return nil, fmt.Errorf("release store: scan pending release by tenant+env: %w", err)
		}
		releases = append(releases, r)
	}
	if releases == nil {
		releases = []Release{}
	}
	return releases, nil
}

// scanReleaseFields is the shared scan target list for any code path
// that returns a *Release. Centralizing avoids drift across the 9+
// SELECTs in this file (CHG-2026-04-25-ATTESTATION-WIRE).
// The three signature columns are scanned at the end and stay nil for
// legacy rows where they are NULL.
func scanReleaseFields(row interface{ Scan(...any) error }, r *Release) error {
	return row.Scan(
		&r.ID, &r.BuildID, &r.TenantID, &r.ProjectID, &r.Environment,
		&r.Status, &r.CorrelationID, &r.CreatedBy,
		&r.CreatedAt, &r.GitCommit, &r.GitMirrorStatus,
		&r.Apps, &r.Artifacts, &r.SideEffects,
		&r.ImageDigest, &r.ImageSignature, &r.ImageSignedAt,
	)
}

// scanRelease scans a single release row.
// Returns ErrReleaseNotFound if the row contains no data.
func (s *PostgresReleaseStore) scanRelease(row Row) (*Release, error) {
	var r Release
	err := scanReleaseFields(row, &r)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return nil, loopzeroerrors.ErrReleaseNotFound
		}
		return nil, fmt.Errorf("release store: scan failed: %w", err)
	}
	return &r, nil
}
