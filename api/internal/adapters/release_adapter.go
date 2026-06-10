// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// StatusReleaseStoreAdapter implements routes.StatusReleaseStore using db.ReleaseStore.
type StatusReleaseStoreAdapter struct {
	Store db.ReleaseStore
}

func (a *StatusReleaseStoreAdapter) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*routes.StatusRelease, error) {
	release, err := a.Store.GetActiveByEnv(ctx, tenantID, projectID, env)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, nil
	}
	return &routes.StatusRelease{
		ID:        release.ID,
		BuildID:   release.BuildID,
		Status:    release.Status,
		GitCommit: release.GitCommit,
		CreatedAt: release.CreatedAt,
	}, nil
}
