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
}

// ReleaseStore defines the interface for release persistence operations.
type ReleaseStore interface {
	Create(ctx context.Context, tx DBTX, release *Release) error
	GetByID(ctx context.Context, id string) (*Release, error)
	UpdateStatus(ctx context.Context, tx DBTX, id string, event string) (*Release, error)
	SetGitCommit(ctx context.Context, tx DBTX, id string, commitSHA string) error
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*Release, error)
	GetActiveByEnvTx(ctx context.Context, tx DBTX, tenantID, projectID, env string) (*Release, error)
	GetPreviousRelease(ctx context.Context, tenantID, projectID, env string) (*Release, error)
	GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*Release, error)
	ListByEnv(ctx context.Context, tenantID, projectID, env string, limit int) ([]Release, error)
	// GetLatestByBuildID returns the most recent release for a build, or nil if none exists.
	GetLatestByBuildID(ctx context.Context, buildID string) (*Release, error)
	// FindPendingReleases returns releases in PENDING state (awaiting ArgoCD sync verification).
	// Used by the Release Convergence Loop (MEET-2026-03-27-012 Phase 1.1).
	FindPendingReleases(ctx context.Context) ([]Release, error)
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

	_, err := tx.Exec(ctx,
		`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by, apps, artifacts)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		release.ID, release.BuildID, release.TenantID, release.ProjectID,
		release.Environment, release.Status, release.CorrelationID, release.CreatedBy, release.Apps, release.Artifacts,
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
		        COALESCE(side_effects, 'null'::jsonb)
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
		        COALESCE(side_effects, 'null'::jsonb)
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
		        COALESCE(side_effects, 'null'::jsonb)
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
		        COALESCE(side_effects, 'null'::jsonb)
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
		        COALESCE(side_effects, 'null'::jsonb)
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
		        COALESCE(side_effects, 'null'::jsonb)
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
		        COALESCE(side_effects, 'null'::jsonb)
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
		if err := rows.Scan(
			&r.ID, &r.BuildID, &r.TenantID, &r.ProjectID, &r.Environment,
			&r.Status, &r.CorrelationID, &r.CreatedBy,
			&r.CreatedAt, &r.GitCommit, &r.GitMirrorStatus,
			&r.Apps, &r.Artifacts, &r.SideEffects,
		); err != nil {
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
		        COALESCE(side_effects, 'null'::jsonb)
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

// FindPendingReleases returns all releases in PENDING state.
// These are releases where GitOps push succeeded but ArgoCD sync verification
// has not yet completed. The Release Convergence Loop processes these.
func (s *PostgresReleaseStore) FindPendingReleases(ctx context.Context) ([]Release, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, build_id, tenant_id, project_id, environment, status, correlation_id,
		        created_by, created_at, COALESCE(git_commit, ''), git_mirror_status,
		        COALESCE(apps, 'null'::jsonb), COALESCE(artifacts, 'null'::jsonb),
		        COALESCE(side_effects, 'null'::jsonb)
		 FROM releases
		 WHERE status = 'PENDING'
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("release store: find pending releases: %w", err)
	}
	defer rows.Close()

	var releases []Release
	for rows.Next() {
		var r Release
		if err := rows.Scan(
			&r.ID, &r.BuildID, &r.TenantID, &r.ProjectID, &r.Environment,
			&r.Status, &r.CorrelationID, &r.CreatedBy,
			&r.CreatedAt, &r.GitCommit, &r.GitMirrorStatus,
			&r.Apps, &r.Artifacts, &r.SideEffects,
		); err != nil {
			return nil, fmt.Errorf("release store: scan pending release: %w", err)
		}
		releases = append(releases, r)
	}
	if releases == nil {
		releases = []Release{}
	}
	return releases, nil
}

// scanRelease scans a single release row.
// Returns ErrReleaseNotFound if the row contains no data.
func (s *PostgresReleaseStore) scanRelease(row Row) (*Release, error) {
	var r Release
	err := row.Scan(
		&r.ID, &r.BuildID, &r.TenantID, &r.ProjectID, &r.Environment,
		&r.Status, &r.CorrelationID, &r.CreatedBy,
		&r.CreatedAt, &r.GitCommit, &r.GitMirrorStatus,
		&r.Apps, &r.Artifacts, &r.SideEffects,
	)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return nil, loopzeroerrors.ErrReleaseNotFound
		}
		return nil, fmt.Errorf("release store: scan failed: %w", err)
	}
	return &r, nil
}
