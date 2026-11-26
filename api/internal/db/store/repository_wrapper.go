// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// repository_wrapper.go — sqlc-backed implementation of
// PostgresRepositoryStore. T2.1 Batch A (3rd of 10) of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 1 prod call site (api/cmd/setup_github.go:56) uses the
// struct-literal `&db.PostgresRepositoryStore{DB: dbAdapter}` —
// preserved verbatim (struct + types in repository_types.go).
//
// Invariants preserved:
//   - Idempotent UPSERT on (id) for installations + (repo_full_name)
//     for project_repositories
//   - UnlinkRepository returns ErrRepositoryNotLinked when no row
//     affected (CHG-2026-04-23-AUDIT-21 sentinel pattern)
//   - GetByRepo cross-tenant (webhook path; tenantpredicate:exempt)
//   - GetByProject tenant-scoped (API path; tenant predicate
//     enforced)

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// generateRepoID produces a "repo-XXXX" identifier for new
// project_repositories rows.
func generateRepoID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "repo-" + hex.EncodeToString(b)
}

// SaveInstallation upserts a GitHub App installation.
func (s *PostgresRepositoryStore) SaveInstallation(ctx context.Context, id int64, accountLogin, accountType string, tenantID *string) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	if err := q.UpsertGitHubInstallation(ctx, gdb, generated.UpsertGitHubInstallationParams{
		ID:           id,
		AccountLogin: accountLogin,
		AccountType:  accountType,
		TenantID:     tenantID,
	}); err != nil {
		return fmt.Errorf("repository store: save installation: %w", err)
	}
	return nil
}

// GetInstallation retrieves a GitHub installation by ID. Returns
// (nil, nil) when not found (legacy contract).
func (s *PostgresRepositoryStore) GetInstallation(ctx context.Context, installationID int64) (*GitHubInstallation, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	row, err := q.GetGitHubInstallation(ctx, gdb, installationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("repository store: get installation: %w", err)
	}
	return installationFromRow(row), nil
}

// installationFromRow converts a github_installations row.
func installationFromRow(row generated.GithubInstallation) *GitHubInstallation {
	inst := &GitHubInstallation{
		ID:           row.ID,
		AccountLogin: row.AccountLogin,
		AccountType:  row.AccountType,
		TenantID:     row.TenantID,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
		VerifiedBy:   row.VerifiedBy,
	}
	if row.VerifiedAt.Valid {
		verifiedAt := row.VerifiedAt.Time
		inst.VerifiedAt = &verifiedAt
	}
	return inst
}

// ClaimInstallation binds an installation to tenantID, recording that GitHub
// confirmed userID can access it. It reports false, and writes nothing, when
// the installation is bound to another tenant.
func (s *PostgresRepositoryStore) ClaimInstallation(ctx context.Context, id int64, accountLogin, accountType, tenantID, userID string) (bool, error) {
	if tenantID == "" || userID == "" {
		return false, fmt.Errorf("repository store: claim installation %d: tenant and user are required", id)
	}
	gdb := db.Generated(s.DB)
	q := generated.New()

	_, err := q.ClaimGitHubInstallation(ctx, gdb, generated.ClaimGitHubInstallationParams{
		ID:           id,
		AccountLogin: accountLogin,
		AccountType:  accountType,
		TenantID:     &tenantID,
		VerifiedBy:   &userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("repository store: claim installation: %w", err)
	}
	return true, nil
}

// ListVerifiedInstallations returns the installations bound to tenantID with
// the binding confirmed on GitHub, ordered by id.
func (s *PostgresRepositoryStore) ListVerifiedInstallations(ctx context.Context, tenantID string) ([]GitHubInstallation, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.ListVerifiedGitHubInstallationsByTenant(ctx, gdb, &tenantID)
	if err != nil {
		return nil, fmt.Errorf("repository store: list verified installations: %w", err)
	}
	installations := make([]GitHubInstallation, 0, len(rows))
	for _, row := range rows {
		installations = append(installations, *installationFromRow(row))
	}
	return installations, nil
}

// DeleteInstallation removes an installation; FK cascade purges
// project_repositories rows owned by it.
func (s *PostgresRepositoryStore) DeleteInstallation(ctx context.Context, installationID int64) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	if err := q.DeleteGitHubInstallation(ctx, gdb, installationID); err != nil {
		return fmt.Errorf("repository store: delete installation: %w", err)
	}
	return nil
}

// LinkRepository connects a LoopRuntime project to a GitHub repository.
// Idempotent re-link per `loopruntime connect` CLI contract.
//
// Plan J-09 T3.2 (2026-06-05) — autoDeploy parameter (was hardcoded true
// in the SQL). Caller passes the user's choice; default true at the route
// handler boundary, not here.
func (s *PostgresRepositoryStore) LinkRepository(ctx context.Context, tenantID, projectName, repoFullName string, installationID int64, defaultBranch string, autoDeploy bool) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	if err := q.LinkProjectRepository(ctx, gdb, generated.LinkProjectRepositoryParams{
		ID:             generateRepoID(),
		TenantID:       tenantID,
		ProjectName:    projectName,
		RepoFullName:   repoFullName,
		InstallationID: installationID,
		DefaultBranch:  defaultBranch,
		AutoDeploy:     autoDeploy,
	}); err != nil {
		return fmt.Errorf("repository store: link repository: %w", err)
	}
	return nil
}

// UnlinkRepository disconnects a project from its GitHub repository.
// Returns ErrRepositoryNotLinked if no row was affected
// (CHG-2026-04-23-AUDIT-21).
func (s *PostgresRepositoryStore) UnlinkRepository(ctx context.Context, tenantID, projectName string) error {
	gdb := db.Generated(s.DB)
	q := generated.New()

	rows, err := q.UnlinkProjectRepository(ctx, gdb, generated.UnlinkProjectRepositoryParams{
		TenantID:    tenantID,
		ProjectName: projectName,
	})
	if err != nil {
		return fmt.Errorf("repository store: unlink repository: %w", err)
	}
	if rows == 0 {
		return ErrRepositoryNotLinked
	}
	return nil
}

// GetByRepo finds the project linked to a GitHub repository
// (webhook path — cross-tenant). Returns (nil, nil) when not linked.
func (s *PostgresRepositoryStore) GetByRepo(ctx context.Context, repoFullName string) (*ProjectRepository, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	row, err := q.GetProjectRepositoryByRepo(ctx, gdb, repoFullName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("repository store: get by repo: %w", err)
	}
	return generatedProjectRepoToProjectRepo(row), nil
}

// GetByProject finds the repository linked to a LoopRuntime project
// (tenant-scoped API path). Returns (nil, nil) when not linked.
func (s *PostgresRepositoryStore) GetByProject(ctx context.Context, tenantID, projectName string) (*ProjectRepository, error) {
	gdb := db.Generated(s.DB)
	q := generated.New()

	row, err := q.GetProjectRepositoryByProject(ctx, gdb, generated.GetProjectRepositoryByProjectParams{
		TenantID:    tenantID,
		ProjectName: projectName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("repository store: get by project: %w", err)
	}
	return generatedProjectRepoToProjectRepo(row), nil
}

// generatedProjectRepoToProjectRepo maps the sqlc generated row to
// the legacy public type. Wraps the pgtype.Timestamptz fields into
// time.Time and the *int64 RepoID through unchanged.
func generatedProjectRepoToProjectRepo(row generated.ProjectRepository) *ProjectRepository {
	r := &ProjectRepository{
		ID:             row.ID,
		TenantID:       row.TenantID,
		ProjectName:    row.ProjectName,
		Provider:       row.Provider,
		RepoFullName:   row.RepoFullName,
		RepoID:         row.RepoID,
		InstallationID: row.InstallationID,
		DefaultBranch:  row.DefaultBranch,
		AutoDeploy:     row.AutoDeploy,
	}
	if row.CreatedAt.Valid {
		r.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		r.UpdatedAt = row.UpdatedAt.Time
	}
	return r
}
