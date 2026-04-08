// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// repoStoreAdapter bridges db.PostgresRepositoryStore → routes.RepositoryStore.
type repoStoreAdapter struct {
	store *db.PostgresRepositoryStore
}

func (a *repoStoreAdapter) LinkRepository(ctx context.Context, tenantID, projectName, repoFullName string, installationID int64, defaultBranch string) error {
	return a.store.LinkRepository(ctx, tenantID, projectName, repoFullName, installationID, defaultBranch)
}

func (a *repoStoreAdapter) UnlinkRepository(ctx context.Context, tenantID, projectName string) error {
	return a.store.UnlinkRepository(ctx, tenantID, projectName)
}

func (a *repoStoreAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (*routes.ProjectRepository, error) {
	r, err := a.store.GetByProject(ctx, tenantID, projectName)
	if err != nil || r == nil {
		return nil, err
	}
	return &routes.ProjectRepository{
		ID:             r.ID,
		TenantID:       r.TenantID,
		ProjectName:    r.ProjectName,
		Provider:       r.Provider,
		RepoFullName:   r.RepoFullName,
		InstallationID: r.InstallationID,
		DefaultBranch:  r.DefaultBranch,
		AutoDeploy:     r.AutoDeploy,
		CreatedAt:      r.CreatedAt,
	}, nil
}

func (a *repoStoreAdapter) GetByRepo(ctx context.Context, repoFullName string) (*routes.ProjectRepository, error) {
	r, err := a.store.GetByRepo(ctx, repoFullName)
	if err != nil || r == nil {
		return nil, err
	}
	return &routes.ProjectRepository{
		ID:             r.ID,
		TenantID:       r.TenantID,
		ProjectName:    r.ProjectName,
		Provider:       r.Provider,
		RepoFullName:   r.RepoFullName,
		InstallationID: r.InstallationID,
		DefaultBranch:  r.DefaultBranch,
		AutoDeploy:     r.AutoDeploy,
		CreatedAt:      r.CreatedAt,
	}, nil
}

func (a *repoStoreAdapter) SaveInstallation(ctx context.Context, id int64, accountLogin, accountType string, tenantID *string) error {
	return a.store.SaveInstallation(ctx, id, accountLogin, accountType, tenantID)
}
