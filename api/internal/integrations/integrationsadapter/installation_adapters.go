// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integrationsadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	gh "github.com/getloopscale/loopzero/api/internal/github"
	"github.com/getloopscale/loopzero/api/internal/integrations/integrationshttp"
)

// InstallationLookupAdapter bridges store.PostgresRepositoryStore →
// routes.InstallationLookup. Returns the minimal routes.InstallationRecord
// shape the integrations handler needs for tenant-boundary checks,
// without dragging the db or github types into the routes package.
// Plan J-09 T2.1.
type InstallationLookupAdapter struct {
	Store *store.PostgresRepositoryStore
}

func (a *InstallationLookupAdapter) GetInstallation(ctx context.Context, installationID int64) (*integrationshttp.InstallationRecord, error) {
	inst, err := a.Store.GetInstallation(ctx, installationID)
	if err != nil || inst == nil {
		return nil, err
	}
	return &integrationshttp.InstallationRecord{
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

func (a *InstallationRepoListerAdapter) ListInstallationRepositories(ctx context.Context, installationID int64, page int) ([]integrationshttp.InstallationRepository, error) {
	repos, err := a.Client.ListInstallationRepositories(ctx, installationID, page)
	if err != nil {
		var revoked *gh.InstallationRevokedError
		if errors.As(err, &revoked) {
			return nil, fmt.Errorf("github installation %d revoked: %w", installationID, integrationshttp.ErrInstallationRevoked)
		}
		return nil, err
	}
	out := make([]integrationshttp.InstallationRepository, 0, len(repos))
	for _, r := range repos {
		out = append(out, integrationshttp.InstallationRepository{
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
	_ integrationshttp.InstallationLookup     = (*InstallationLookupAdapter)(nil)
	_ integrationshttp.InstallationRepoLister = (*InstallationRepoListerAdapter)(nil)
)
