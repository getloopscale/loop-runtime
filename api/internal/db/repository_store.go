// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ProjectRepository links a LoopZero project to a GitHub repository.
type ProjectRepository struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ProjectName    string    `json:"project_name"`
	Provider       string    `json:"provider"`
	RepoFullName   string    `json:"repo_full_name"`
	RepoID         *int64    `json:"repo_id,omitempty"`
	InstallationID int64     `json:"installation_id"`
	DefaultBranch  string    `json:"default_branch"`
	AutoDeploy     bool      `json:"auto_deploy"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// GitHubInstallation represents a GitHub App installation.
type GitHubInstallation struct {
	ID           int64     `json:"id"`
	AccountLogin string    `json:"account_login"`
	AccountType  string    `json:"account_type"`
	TenantID     *string   `json:"tenant_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PostgresRepositoryStore implements repository persistence.
type PostgresRepositoryStore struct {
	DB DBTX
}

func generateRepoID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "repo-" + hex.EncodeToString(b)
}

// SaveInstallation upserts a GitHub App installation.
func (s *PostgresRepositoryStore) SaveInstallation(ctx context.Context, inst *GitHubInstallation) error {
	_, err := s.DB.Exec(ctx,
		`INSERT INTO github_installations (id, account_login, account_type, tenant_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, now(), now())
		 ON CONFLICT (id) DO UPDATE SET
		   account_login = EXCLUDED.account_login,
		   tenant_id = COALESCE(EXCLUDED.tenant_id, github_installations.tenant_id),
		   updated_at = now()`,
		inst.ID, inst.AccountLogin, inst.AccountType, inst.TenantID,
	)
	if err != nil {
		return fmt.Errorf("repository store: save installation: %w", err)
	}
	return nil
}

// GetInstallation retrieves a GitHub installation by ID.
func (s *PostgresRepositoryStore) GetInstallation(ctx context.Context, installationID int64) (*GitHubInstallation, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id, account_login, account_type, tenant_id, created_at, updated_at
		 FROM github_installations WHERE id = $1`, installationID)
	if err != nil {
		return nil, fmt.Errorf("repository store: get installation: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	var inst GitHubInstallation
	if err := rows.Scan(&inst.ID, &inst.AccountLogin, &inst.AccountType, &inst.TenantID, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
		return nil, fmt.Errorf("repository store: scan installation: %w", err)
	}
	return &inst, nil
}

// DeleteInstallation removes an installation and all linked repos.
func (s *PostgresRepositoryStore) DeleteInstallation(ctx context.Context, installationID int64) error {
	// FK cascade deletes project_repositories
	_, err := s.DB.Exec(ctx, `DELETE FROM github_installations WHERE id = $1`, installationID)
	if err != nil {
		return fmt.Errorf("repository store: delete installation: %w", err)
	}
	return nil
}

// LinkRepository connects a LoopZero project to a GitHub repository.
func (s *PostgresRepositoryStore) LinkRepository(ctx context.Context, tenantID, projectName, repoFullName string, installationID int64, defaultBranch string) error {
	_, err := s.DB.Exec(ctx,
		`INSERT INTO project_repositories (id, tenant_id, project_name, provider, repo_full_name, installation_id, default_branch, auto_deploy, created_at, updated_at)
		 VALUES ($1, $2, $3, 'github', $4, $5, $6, true, now(), now())`,
		generateRepoID(), tenantID, projectName, repoFullName, installationID, defaultBranch,
	)
	if err != nil {
		return fmt.Errorf("repository store: link repository: %w", err)
	}
	return nil
}

// UnlinkRepository disconnects a project from its GitHub repository.
func (s *PostgresRepositoryStore) UnlinkRepository(ctx context.Context, tenantID, projectName string) error {
	_, err := s.DB.Exec(ctx,
		`DELETE FROM project_repositories WHERE tenant_id = $1 AND project_name = $2`,
		tenantID, projectName,
	)
	if err != nil {
		return fmt.Errorf("repository store: unlink repository: %w", err)
	}
	return nil
}

// GetByRepo finds the project linked to a GitHub repository.
func (s *PostgresRepositoryStore) GetByRepo(ctx context.Context, repoFullName string) (*ProjectRepository, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, provider, repo_full_name, repo_id,
		        installation_id, default_branch, auto_deploy, created_at, updated_at
		 FROM project_repositories WHERE repo_full_name = $1`, repoFullName)
	if err != nil {
		return nil, fmt.Errorf("repository store: get by repo: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil // not linked
	}

	var r ProjectRepository
	if err := rows.Scan(&r.ID, &r.TenantID, &r.ProjectName, &r.Provider, &r.RepoFullName,
		&r.RepoID, &r.InstallationID, &r.DefaultBranch, &r.AutoDeploy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, fmt.Errorf("repository store: scan: %w", err)
	}
	return &r, nil
}

// GetByProject finds the repository linked to a LoopZero project.
func (s *PostgresRepositoryStore) GetByProject(ctx context.Context, tenantID, projectName string) (*ProjectRepository, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, provider, repo_full_name, repo_id,
		        installation_id, default_branch, auto_deploy, created_at, updated_at
		 FROM project_repositories WHERE tenant_id = $1 AND project_name = $2`, tenantID, projectName)
	if err != nil {
		return nil, fmt.Errorf("repository store: get by project: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	var r ProjectRepository
	if err := rows.Scan(&r.ID, &r.TenantID, &r.ProjectName, &r.Provider, &r.RepoFullName,
		&r.RepoID, &r.InstallationID, &r.DefaultBranch, &r.AutoDeploy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, fmt.Errorf("repository store: scan: %w", err)
	}
	return &r, nil
}
