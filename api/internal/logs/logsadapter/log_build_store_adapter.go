// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logsadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/logs/logshttp"
)

// LogBuildStoreAdapter implements routes.LogBuildStore using db.BuildStore.
// The logs handler needs only build ID, tenant, and status for authorization
// and response metadata.
type LogBuildStoreAdapter struct {
	Store interface {
		buildByID
		tenantBuildByID
	}
}

func (a *LogBuildStoreAdapter) GetByID(ctx context.Context, id string) (*logshttp.LogBuild, error) {
	build, err := a.Store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if build == nil {
		return nil, nil
	}
	return &logshttp.LogBuild{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

// GetByIDForTenant routes to the tenant-scoped DB query — T0.4 of
// deep-review-2026-05-10-remediation-plan (find_sec_idor_buildstore).
func (a *LogBuildStoreAdapter) GetByIDForTenant(ctx context.Context, tenantID, id string) (*logshttp.LogBuild, error) {
	build, err := a.Store.GetByIDForTenant(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if build == nil {
		return nil, nil
	}
	return &logshttp.LogBuild{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

// The port this adapter satisfies, asserted at compile time.
var _ logshttp.LogBuildStore = (*LogBuildStoreAdapter)(nil)
