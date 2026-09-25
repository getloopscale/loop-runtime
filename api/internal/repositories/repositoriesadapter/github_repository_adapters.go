// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repositoriesadapter

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/github"
)

// RepoLookupAdapter bridges store.PostgresRepositoryStore → github.RepositoryLookup.
type RepoLookupAdapter struct {
	Store *store.PostgresRepositoryStore
}

func (a *RepoLookupAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (string, int64, error) {
	r, err := a.Store.GetByProject(ctx, tenantID, projectName)
	if err != nil || r == nil {
		return "", 0, err
	}
	return r.RepoFullName, r.InstallationID, nil
}

// ConnectedRepoAdapter bridges store.PostgresRepositoryStore to
// github.RepoLookup, translating the persisted row into the GitHub
// package's own ConnectedRepo so that package does not import the store.
type ConnectedRepoAdapter struct {
	Store *store.PostgresRepositoryStore
}

func (a *ConnectedRepoAdapter) GetByRepo(ctx context.Context, repoFullName string) (*github.ConnectedRepo, error) {
	row, err := a.Store.GetByRepo(ctx, repoFullName)
	if err != nil || row == nil {
		return nil, err
	}
	return &github.ConnectedRepo{TenantID: row.TenantID, ProjectName: row.ProjectName, AutoDeploy: row.AutoDeploy}, nil
}

// The ports these adapters satisfy, asserted at compile time.
var (
	_ github.RepositoryLookup = (*RepoLookupAdapter)(nil)
	_ github.RepoLookup       = (*ConnectedRepoAdapter)(nil)
)
