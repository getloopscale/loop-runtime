// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package integrationsadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	gh "github.com/getloopscale/loop-runtime/api/internal/github"
	"github.com/getloopscale/loop-runtime/api/internal/integrations/integrationshttp"
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
		Verified: inst.VerifiedAt != nil,
	}, nil
}

// InstallationClaimAdapter satisfies integrationshttp.InstallationClaimStore
// with the installation rows of the repository store.
type InstallationClaimAdapter struct {
	Store *store.PostgresRepositoryStore
}

// ClaimInstallation implements integrationshttp.InstallationClaimStore.
func (a *InstallationClaimAdapter) ClaimInstallation(ctx context.Context, inst integrationshttp.Installation, tenantID, userID string) (bool, error) {
	return a.Store.ClaimInstallation(ctx, inst.ID, inst.AccountLogin, inst.AccountType, tenantID, userID)
}

// ListVerifiedInstallations implements integrationshttp.InstallationClaimStore.
func (a *InstallationClaimAdapter) ListVerifiedInstallations(ctx context.Context, tenantID string) ([]integrationshttp.Installation, error) {
	rows, err := a.Store.ListVerifiedInstallations(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]integrationshttp.Installation, 0, len(rows))
	for _, row := range rows {
		out = append(out, integrationshttp.Installation{ID: row.ID, AccountLogin: row.AccountLogin, AccountType: row.AccountType})
	}
	return out, nil
}

// userInstallations is the GitHub call behind a claim (*gh.UserAccessClient).
type userInstallations interface {
	InstallationsForCode(ctx context.Context, code string) ([]gh.UserInstallation, error)
}

// UserInstallationSourceAdapter satisfies integrationshttp.UserInstallationSource
// with the App's user authorization on GitHub.
type UserInstallationSourceAdapter struct {
	Client userInstallations
}

// InstallationsForCode implements integrationshttp.UserInstallationSource. A
// code GitHub refuses becomes integrationshttp.ErrOAuthCodeRejected.
func (a *UserInstallationSourceAdapter) InstallationsForCode(ctx context.Context, code string) ([]integrationshttp.Installation, error) {
	found, err := a.Client.InstallationsForCode(ctx, code)
	if errors.Is(err, gh.ErrOAuthCodeRejected) {
		return nil, fmt.Errorf("%w", integrationshttp.ErrOAuthCodeRejected)
	}
	if err != nil {
		return nil, err
	}
	out := make([]integrationshttp.Installation, 0, len(found))
	for _, inst := range found {
		out = append(out, integrationshttp.Installation{ID: inst.ID, AccountLogin: inst.AccountLogin, AccountType: inst.AccountType})
	}
	return out, nil
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
	_ integrationshttp.InstallationClaimStore = (*InstallationClaimAdapter)(nil)
	_ integrationshttp.UserInstallationSource = (*UserInstallationSourceAdapter)(nil)
)
