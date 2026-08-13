// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	gh "github.com/getloopscale/loopzero/api/internal/github"
	"github.com/getloopscale/loopzero/api/internal/routes"
	"github.com/getloopscale/loopzero/api/internal/routes/repositories"
)

// repoStoreAdapter bridges store.PostgresRepositoryStore → repositories.RepositoryStore.
type repoStoreAdapter struct {
	store *store.PostgresRepositoryStore
}

func (a *repoStoreAdapter) LinkRepository(ctx context.Context, tenantID, projectName, repoFullName string, installationID int64, defaultBranch string, autoDeploy bool) error {
	return a.store.LinkRepository(ctx, tenantID, projectName, repoFullName, installationID, defaultBranch, autoDeploy)
}

func (a *repoStoreAdapter) UnlinkRepository(ctx context.Context, tenantID, projectName string) error {
	return a.store.UnlinkRepository(ctx, tenantID, projectName)
}

func (a *repoStoreAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (*repositories.ProjectRepository, error) {
	r, err := a.store.GetByProject(ctx, tenantID, projectName)
	if err != nil || r == nil {
		return nil, err
	}
	return &repositories.ProjectRepository{
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

func (a *repoStoreAdapter) GetByRepo(ctx context.Context, repoFullName string) (*repositories.ProjectRepository, error) {
	r, err := a.store.GetByRepo(ctx, repoFullName)
	if err != nil || r == nil {
		return nil, err
	}
	return &repositories.ProjectRepository{
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

// installationLookupAdapter bridges store.PostgresRepositoryStore →
// routes.InstallationLookup. Returns the minimal routes.InstallationRecord
// shape the integrations handler needs for tenant-boundary checks,
// without dragging the db or github types into the routes package.
// Plan J-09 T2.1.
type installationLookupAdapter struct {
	store *store.PostgresRepositoryStore
}

func (a *installationLookupAdapter) GetInstallation(ctx context.Context, installationID int64) (*routes.InstallationRecord, error) {
	inst, err := a.store.GetInstallation(ctx, installationID)
	if err != nil || inst == nil {
		return nil, err
	}
	return &routes.InstallationRecord{
		ID:       inst.ID,
		TenantID: inst.TenantID,
	}, nil
}

// installationRepoListerAdapter bridges github.AppClient →
// routes.InstallationRepoLister. Translates the github package response
// shape to the sanitised routes type, and wraps the github.InstallationRevokedError
// into routes.ErrInstallationRevoked so the handler's errors.Is() check
// works without importing the github package.
// Plan J-09 T2.1.
type installationRepoListerAdapter struct {
	client *gh.AppClient
}

func (a *installationRepoListerAdapter) ListInstallationRepositories(ctx context.Context, installationID int64, page int) ([]routes.InstallationRepository, error) {
	repos, err := a.client.ListInstallationRepositories(ctx, installationID, page)
	if err != nil {
		var revoked *gh.InstallationRevokedError
		if errors.As(err, &revoked) {
			return nil, fmt.Errorf("github installation %d revoked: %w", installationID, routes.ErrInstallationRevoked)
		}
		return nil, err
	}
	out := make([]routes.InstallationRepository, 0, len(repos))
	for _, r := range repos {
		out = append(out, routes.InstallationRepository{
			FullName:      r.FullName,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
			Language:      r.Language,
		})
	}
	return out, nil
}
