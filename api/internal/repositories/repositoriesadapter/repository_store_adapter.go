// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repositoriesadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/repositories/repositorieshttp"
)

// RepoStoreAdapter bridges store.PostgresRepositoryStore → repositories.RepositoryStore.
type RepoStoreAdapter struct {
	Store *store.PostgresRepositoryStore
}

func (a *RepoStoreAdapter) LinkRepository(ctx context.Context, tenantID, projectName, repoFullName string, installationID int64, defaultBranch string, autoDeploy bool) error {
	return a.Store.LinkRepository(ctx, tenantID, projectName, repoFullName, installationID, defaultBranch, autoDeploy)
}

func (a *RepoStoreAdapter) UnlinkRepository(ctx context.Context, tenantID, projectName string) error {
	return a.Store.UnlinkRepository(ctx, tenantID, projectName)
}

func (a *RepoStoreAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (*repositorieshttp.ProjectRepository, error) {
	r, err := a.Store.GetByProject(ctx, tenantID, projectName)
	if err != nil || r == nil {
		return nil, err
	}
	return &repositorieshttp.ProjectRepository{
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

func (a *RepoStoreAdapter) GetByRepo(ctx context.Context, repoFullName string) (*repositorieshttp.ProjectRepository, error) {
	r, err := a.Store.GetByRepo(ctx, repoFullName)
	if err != nil || r == nil {
		return nil, err
	}
	return &repositorieshttp.ProjectRepository{
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

// InstallationVerifiedFor reports whether the installation is bound to
// tenantID with the binding confirmed on GitHub.
func (a *RepoStoreAdapter) InstallationVerifiedFor(ctx context.Context, installationID int64, tenantID string) (bool, error) {
	inst, err := a.Store.GetInstallation(ctx, installationID)
	if err != nil {
		return false, err
	}
	return inst.VerifiedFor(tenantID), nil
}

// The port this adapter satisfies, asserted at compile time.
var _ repositorieshttp.RepositoryStore = (*RepoStoreAdapter)(nil)
