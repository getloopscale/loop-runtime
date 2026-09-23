// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	gh "github.com/getloopscale/loopzero/api/internal/github"
	"github.com/getloopscale/loopzero/api/internal/routes/integrations"
	"github.com/getloopscale/loopzero/api/internal/routes/repositories"
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

func (a *RepoStoreAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (*repositories.ProjectRepository, error) {
	r, err := a.Store.GetByProject(ctx, tenantID, projectName)
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

func (a *RepoStoreAdapter) GetByRepo(ctx context.Context, repoFullName string) (*repositories.ProjectRepository, error) {
	r, err := a.Store.GetByRepo(ctx, repoFullName)
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

func (a *RepoStoreAdapter) SaveInstallation(ctx context.Context, id int64, accountLogin, accountType string, tenantID *string) error {
	return a.Store.SaveInstallation(ctx, id, accountLogin, accountType, tenantID)
}

// InstallationTenant reports the tenant an installation is bound to; "" when
// it is unclaimed (created by the installation webhook, tenant NULL) or absent.
func (a *RepoStoreAdapter) InstallationTenant(ctx context.Context, installationID int64) (string, error) {
	inst, err := a.Store.GetInstallation(ctx, installationID)
	if err != nil {
		return "", err
	}
	if inst == nil || inst.TenantID == nil {
		return "", nil
	}
	return *inst.TenantID, nil
}

// InstallationLookupAdapter bridges store.PostgresRepositoryStore →
// routes.InstallationLookup. Returns the minimal routes.InstallationRecord
// shape the integrations handler needs for tenant-boundary checks,
// without dragging the db or github types into the routes package.
// Plan J-09 T2.1.
type InstallationLookupAdapter struct {
	Store *store.PostgresRepositoryStore
}

func (a *InstallationLookupAdapter) GetInstallation(ctx context.Context, installationID int64) (*integrations.InstallationRecord, error) {
	inst, err := a.Store.GetInstallation(ctx, installationID)
	if err != nil || inst == nil {
		return nil, err
	}
	return &integrations.InstallationRecord{
		ID:       inst.ID,
		TenantID: inst.TenantID,
	}, nil
}

// InstallationRepoListerAdapter bridges github.AppClient →
// routes.InstallationRepoLister. Translates the github package response
// shape to the sanitised routes type, and wraps the github.InstallationRevokedError
// into routes.ErrInstallationRevoked so the handler's errors.Is() check
// works without importing the github package.
// Plan J-09 T2.1.
type InstallationRepoListerAdapter struct {
	Client *gh.AppClient
}

func (a *InstallationRepoListerAdapter) ListInstallationRepositories(ctx context.Context, installationID int64, page int) ([]integrations.InstallationRepository, error) {
	repos, err := a.Client.ListInstallationRepositories(ctx, installationID, page)
	if err != nil {
		var revoked *gh.InstallationRevokedError
		if errors.As(err, &revoked) {
			return nil, fmt.Errorf("github installation %d revoked: %w", installationID, integrations.ErrInstallationRevoked)
		}
		return nil, err
	}
	out := make([]integrations.InstallationRepository, 0, len(repos))
	for _, r := range repos {
		out = append(out, integrations.InstallationRepository{
			FullName:      r.FullName,
			DefaultBranch: r.DefaultBranch,
			Private:       r.Private,
			Language:      r.Language,
		})
	}
	return out, nil
}

// The ports these adapters satisfy, asserted at compile time.
var (
	_ repositories.RepositoryStore        = (*RepoStoreAdapter)(nil)
	_ integrations.InstallationLookup     = (*InstallationLookupAdapter)(nil)
	_ integrations.InstallationRepoLister = (*InstallationRepoListerAdapter)(nil)
)
