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

// buildMachine is the build state machine singleton.
// Used to validate all status transitions.
var buildMachine = statemachine.NewBuildMachine()

// Build represents a build record in the database.
type Build struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	ProjectID       string          `json:"project_id"`
	Environment     string          `json:"environment"`
	Status          string          `json:"status"`
	CommitSHA       string          `json:"commit_sha,omitempty"`
	CorrelationID   string          `json:"correlation_id"`
	CreatedBy       string          `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	ErrorMessage    string          `json:"error_message,omitempty"`
	DurationSeconds *int            `json:"duration_seconds,omitempty"`
	WorkflowID      string          `json:"workflow_id,omitempty"`
	AppsSpec        json.RawMessage `json:"apps_spec,omitempty"` // App specs from submission (name, type, port, secrets)
}

// BuildAppSpec is a single app spec stored at build submission time.
// Used by the reconcile loop to generate manifests and compute image refs.
type BuildAppSpec struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Rendering string   `json:"rendering,omitempty"` // "static" or "server" (Sprint 10)
	Port      int      `json:"port"`
	Secrets   []string `json:"secrets,omitempty"`
}

// BuildStore defines the interface for build persistence operations.
type BuildStore interface {
	Create(ctx context.Context, tx DBTX, build *Build) error
	GetByID(ctx context.Context, id string) (*Build, error)
	HasActiveBuild(ctx context.Context, tx DBTX, tenantID, projectID, env string) (bool, error)
	UpdateStatus(ctx context.Context, tx DBTX, id string, event string) (*Build, error)
	SetErrorMessage(ctx context.Context, tx DBTX, id string, errorMessage string) error
	SetWorkflowID(ctx context.Context, tx DBTX, id string, workflowID string) error
	SetAppsSpec(ctx context.Context, tx DBTX, id string, apps []BuildAppSpec) error
	GetBuildApps(ctx context.Context, buildID string) ([]BuildAppSpec, error)
	ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]Build, error)
	ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]Build, int, error)
	FindActiveBuilds(ctx context.Context) ([]Build, error)
	CancelActiveBuilds(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, []string, error)
}

// PostgresBuildStore implements BuildStore using PostgreSQL.
type PostgresBuildStore struct {
	db DBTX
}

// NewPostgresBuildStore creates a new PostgresBuildStore.
func NewPostgresBuildStore(db DBTX) *PostgresBuildStore {
	return &PostgresBuildStore{db: db}
}

// Create inserts a new build with status QUEUED.
// If build.ID is empty, a new ID is generated via pkg/id.
func (s *PostgresBuildStore) Create(ctx context.Context, tx DBTX, build *Build) error {
	if build.ID == "" {
		build.ID = id.GenerateBuildID()
	}
	build.Status = string(statemachine.BuildQueued)

	_, err := tx.Exec(ctx,
		`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by, commit_sha)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		build.ID, build.TenantID, build.ProjectID, build.Environment,
		build.Status, build.CorrelationID, build.CreatedBy, build.CommitSHA,
	)
	if err != nil {
		var uv *UniqueViolationError
		if errors.As(err, &uv) {
			return loopzeroerrors.ErrDeployInProgress
		}
		return fmt.Errorf("build store: insert failed: %w", err)
	}

	return nil
}

// GetByID retrieves a build by its ID.
// Returns ErrBuildNotFound if the build does not exist.
func (s *PostgresBuildStore) GetByID(ctx context.Context, buildID string) (*Build, error) {
	return s.scanBuild(s.db.QueryRow(ctx,
		`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
		        correlation_id, created_by, created_at, started_at, finished_at,
		        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
		 FROM builds WHERE id = $1`,
		buildID,
	))
}

// HasActiveBuild checks if there is a build in QUEUED, RUNNING, or RELEASING status
// for the given tenant, project, and environment.
// RELEASING is included because a build in this state has completed its image build
// and is in the process of creating a release — allowing a new deploy would create
// two concurrent releases for the same environment (ARCH-002 fix).
// This runs inside the advisory lock transaction.
func (s *PostgresBuildStore) HasActiveBuild(ctx context.Context, tx DBTX, tenantID, projectID, env string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM builds
			WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
			AND status IN ('QUEUED', 'RUNNING', 'RELEASING')
		)`,
		tenantID, projectID, env,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("build store: has active build query failed: %w", err)
	}
	return exists, nil
}

// UpdateStatus validates a state transition via the build state machine,
// then updates the build status in the database.
// Returns ErrBuildNotFound if the build does not exist.
// Returns ErrInvalidTransition if the event is not valid from the current state.
func (s *PostgresBuildStore) UpdateStatus(ctx context.Context, tx DBTX, buildID string, event string) (*Build, error) {
	// Use pool if no transaction provided (e.g., outside lock)
	executor := tx
	if executor == nil {
		executor = s.db
	}
	// Fetch current build
	build, err := s.scanBuild(executor.QueryRow(ctx,
		`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
		        correlation_id, created_by, created_at, started_at, finished_at,
		        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
		 FROM builds WHERE id = $1`,
		buildID,
	))
	if err != nil {
		return nil, err
	}

	// Validate transition via state machine — catch panics and convert to typed error
	newState, err := safeTransition(buildMachine, statemachine.State(build.Status), statemachine.Event(event))
	if err != nil {
		return nil, err
	}

	// Update in DB — set timing fields based on state transition.
	// RUNNING: record started_at (build execution began)
	// Terminal states (SUCCEEDED/FAILED/CANCELLED): record finished_at + compute duration
	var query string
	var args []interface{}

	switch newState {
	case statemachine.BuildRunning:
		query = `UPDATE builds SET status = $1, started_at = NOW() WHERE id = $2`
		args = []interface{}{string(newState), buildID}
	case statemachine.BuildSucceeded, statemachine.BuildFailed, statemachine.BuildCancelled:
		query = `UPDATE builds SET status = $1, finished_at = NOW(),
			duration_seconds = COALESCE(EXTRACT(EPOCH FROM (NOW() - started_at))::int, 0)
			WHERE id = $2`
		args = []interface{}{string(newState), buildID}
	default:
		query = `UPDATE builds SET status = $1 WHERE id = $2`
		args = []interface{}{string(newState), buildID}
	}

	_, err = executor.Exec(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("build store: update status failed: %w", err)
	}

	build.Status = string(newState)
	return build, nil
}

// SetErrorMessage stores the error message and finished_at timestamp for a build.
// Called after UpdateStatus when transitioning to a failed terminal state.
func (s *PostgresBuildStore) SetErrorMessage(ctx context.Context, tx DBTX, buildID string, errorMessage string) error {
	executor := tx
	if executor == nil {
		executor = s.db
	}
	result, err := executor.Exec(ctx,
		`UPDATE builds SET error_message = $1, finished_at = NOW() WHERE id = $2`,
		errorMessage, buildID,
	)
	if err != nil {
		return fmt.Errorf("build store: set error message failed: %w", err)
	}
	if result.RowsAffected == 0 {
		return loopzeroerrors.ErrBuildNotFound
	}
	return nil
}

// SetWorkflowID stores the Argo Workflow name for a build.
// Called immediately after workflow submission so the startup reconciler
// and cancel flow can correlate builds with their external workflows.
func (s *PostgresBuildStore) SetWorkflowID(ctx context.Context, tx DBTX, buildID string, workflowID string) error {
	executor := tx
	if executor == nil {
		executor = s.db
	}
	result, err := executor.Exec(ctx,
		`UPDATE builds SET workflow_id = $1 WHERE id = $2`,
		workflowID, buildID,
	)
	if err != nil {
		return fmt.Errorf("build store: set workflow id failed: %w", err)
	}
	if result.RowsAffected == 0 {
		return loopzeroerrors.ErrBuildNotFound
	}
	return nil
}

// SetAppsSpec stores the app specifications for a build (name, type, port, secrets).
// Called during build submission so the reconcile loop can generate manifests.
func (s *PostgresBuildStore) SetAppsSpec(ctx context.Context, tx DBTX, buildID string, apps []BuildAppSpec) error {
	executor := tx
	if executor == nil {
		executor = s.db
	}
	appsJSON, err := json.Marshal(apps)
	if err != nil {
		return fmt.Errorf("build store: marshal apps spec: %w", err)
	}
	_, err = executor.Exec(ctx,
		`UPDATE builds SET apps_spec = $1 WHERE id = $2`,
		appsJSON, buildID,
	)
	if err != nil {
		return fmt.Errorf("build store: set apps spec failed: %w", err)
	}
	return nil
}

// GetBuildApps returns the app specifications for a build.
// These are stored at submission time and used by the reconcile loop
// to generate manifests and compute image references.
func (s *PostgresBuildStore) GetBuildApps(ctx context.Context, buildID string) ([]BuildAppSpec, error) {
	var appsSpec json.RawMessage
	err := s.db.QueryRow(ctx,
		`SELECT COALESCE(apps_spec, '[]'::jsonb) FROM builds WHERE id = $1`,
		buildID,
	).Scan(&appsSpec)
	if err != nil {
		return nil, fmt.Errorf("build store: get build apps: %w", err)
	}

	var apps []BuildAppSpec
	if err := json.Unmarshal(appsSpec, &apps); err != nil {
		return nil, fmt.Errorf("build store: unmarshal apps spec: %w", err)
	}
	return apps, nil
}

// ListByProject returns builds for a project, ordered by created_at DESC.
func (s *PostgresBuildStore) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]Build, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
		        correlation_id, created_by, created_at, started_at, finished_at,
		        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
		 FROM builds
		 WHERE tenant_id = $1 AND project_id = $2
		 ORDER BY created_at DESC
		 LIMIT $3 OFFSET $4`,
		tenantID, projectID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("build store: list query failed: %w", err)
	}
	defer rows.Close()

	var builds []Build
	for rows.Next() {
		var b Build
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.ProjectID, &b.Environment, &b.Status,
			&b.CommitSHA, &b.CorrelationID, &b.CreatedBy,
			&b.CreatedAt, &b.StartedAt, &b.FinishedAt, &b.ErrorMessage,
			&b.DurationSeconds, &b.WorkflowID,
		); err != nil {
			return nil, fmt.Errorf("build store: scan failed: %w", err)
		}
		builds = append(builds, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build store: rows error: %w", err)
	}

	if builds == nil {
		builds = []Build{}
	}
	return builds, nil
}

// ListByProjectAndEnv returns builds for a project, optionally filtered by environment.
// Returns (builds, totalCount, error). If env is empty, returns builds for all environments.
func (s *PostgresBuildStore) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]Build, int, error) {
	// Clamp limit to 1-50
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var rows Rows
	var err error
	var total int

	if env != "" {
		rows, err = s.db.Query(ctx,
			`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
			        correlation_id, created_by, created_at, started_at, finished_at,
			        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
			 FROM builds
			 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
			 ORDER BY created_at DESC
			 LIMIT $4 OFFSET $5`,
			tenantID, projectID, env, limit, offset,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("build store: list by project and env query failed: %w", err)
		}

		err = s.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM builds WHERE tenant_id = $1 AND project_id = $2 AND environment = $3`,
			tenantID, projectID, env,
		).Scan(&total)
	} else {
		rows, err = s.db.Query(ctx,
			`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
			        correlation_id, created_by, created_at, started_at, finished_at,
			        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
			 FROM builds
			 WHERE tenant_id = $1 AND project_id = $2
			 ORDER BY created_at DESC
			 LIMIT $3 OFFSET $4`,
			tenantID, projectID, limit, offset,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("build store: list by project query failed: %w", err)
		}

		err = s.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM builds WHERE tenant_id = $1 AND project_id = $2`,
			tenantID, projectID,
		).Scan(&total)
	}

	if err != nil {
		return nil, 0, fmt.Errorf("build store: count query failed: %w", err)
	}

	defer rows.Close()

	var builds []Build
	for rows.Next() {
		var b Build
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.ProjectID, &b.Environment, &b.Status,
			&b.CommitSHA, &b.CorrelationID, &b.CreatedBy,
			&b.CreatedAt, &b.StartedAt, &b.FinishedAt, &b.ErrorMessage,
			&b.DurationSeconds, &b.WorkflowID,
		); err != nil {
			return nil, 0, fmt.Errorf("build store: scan failed: %w", err)
		}
		builds = append(builds, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("build store: rows error: %w", err)
	}

	if builds == nil {
		builds = []Build{}
	}
	return builds, total, nil
}

// FindActiveBuilds returns all builds in QUEUED or RUNNING state.
// Used by the startup reconciler to find orphaned builds on boot.
func (s *PostgresBuildStore) FindActiveBuilds(ctx context.Context) ([]Build, error) {
	rows, err := s.db.Query(ctx,
		// RELEASING included per MEET-2026-03-26-008: builds with completed images
		// that need release activation (Phase 2) to be retried after crash/failure.
		`SELECT id, tenant_id, project_id, environment, status, COALESCE(commit_sha, ''),
		        correlation_id, created_by, created_at, started_at, finished_at,
		        COALESCE(error_message, ''), duration_seconds, COALESCE(workflow_id, '')
		 FROM builds
		 WHERE status IN ('QUEUED', 'RUNNING', 'RELEASING')
		 ORDER BY created_at ASC
		 LIMIT 100`,
	)
	if err != nil {
		return nil, fmt.Errorf("build store: find active builds: %w", err)
	}
	defer rows.Close()

	var builds []Build
	for rows.Next() {
		var b Build
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.ProjectID, &b.Environment, &b.Status,
			&b.CommitSHA, &b.CorrelationID, &b.CreatedBy,
			&b.CreatedAt, &b.StartedAt, &b.FinishedAt, &b.ErrorMessage,
			&b.DurationSeconds, &b.WorkflowID,
		); err != nil {
			return nil, fmt.Errorf("build store: scan active build: %w", err)
		}
		builds = append(builds, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build store: rows error: %w", err)
	}

	if builds == nil {
		builds = []Build{}
	}
	return builds, nil
}

// CancelActiveBuilds cancels all QUEUED or RUNNING builds for a (tenant, project, env).
// Returns the number of builds cancelled and their workflow IDs (for Argo cancellation).
// This method runs inside an advisory lock transaction.
func (s *PostgresBuildStore) CancelActiveBuilds(ctx context.Context, tx DBTX, tenantID, projectID, env string) (int, []string, error) {
	executor := tx
	if executor == nil {
		executor = s.db
	}
	rows, err := executor.Query(ctx,
		`UPDATE builds SET status = 'CANCELLED', finished_at = NOW()
		 WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
		 AND status IN ('QUEUED', 'RUNNING')
		 RETURNING COALESCE(workflow_id, '')`,
		tenantID, projectID, env,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("build store: cancel active builds: %w", err)
	}
	defer rows.Close()

	var workflowIDs []string
	count := 0
	for rows.Next() {
		var wfID string
		if err := rows.Scan(&wfID); err != nil {
			return 0, nil, fmt.Errorf("build store: scan workflow_id: %w", err)
		}
		count++
		if wfID != "" {
			workflowIDs = append(workflowIDs, wfID)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("build store: cancel active builds rows: %w", err)
	}
	return count, workflowIDs, nil
}

// scanBuild scans a single build row.
// Returns ErrBuildNotFound if the row contains no data.
func (s *PostgresBuildStore) scanBuild(row Row) (*Build, error) {
	var b Build
	err := row.Scan(
		&b.ID, &b.TenantID, &b.ProjectID, &b.Environment, &b.Status,
		&b.CommitSHA, &b.CorrelationID, &b.CreatedBy,
		&b.CreatedAt, &b.StartedAt, &b.FinishedAt, &b.ErrorMessage,
		&b.DurationSeconds, &b.WorkflowID,
	)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("build store: scan failed: %w", err)
	}
	return &b, nil
}

// safeTransition wraps Machine.Transition, catching panics from invalid
// transitions and converting them to ErrInvalidTransition.
// The state machine panics by design (programming errors in operators),
// but in the API layer we return typed errors instead.
func safeTransition(m *statemachine.Machine, current statemachine.State, event statemachine.Event) (newState statemachine.State, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = loopzeroerrors.NewInvalidTransition(string(current), string(event))
		}
	}()
	newState = m.Transition(current, event)
	return newState, nil
}
