// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// RollbackReleaseLookupAdapter satisfies routes.RollbackReleaseLookup
// by composing db.ReleaseStore.GetLatestByBuildID with a tenant ownership
// re-check.
//
// LIVE-caught 2026-06-18 against the dashboard rollback flow: dashboard
// passes {build_id} (user-facing identifier), engine needs to_release_id
// (state-machine canonical). The adapter resolves the latest release
// for the build + asserts the release belongs to the requesting tenant
// (defense-in-depth — GetLatestByBuildID is cross-tenant by design).
type RollbackReleaseLookupAdapter struct {
	Store db.ReleaseStore
}

// GetLatestReleaseForBuild returns the release ID for the most-recent
// release of the named build, after verifying it belongs to the
// requested tenant. Empty string + nil error = no release found OR
// release belongs to a different tenant (handler maps both to 404 —
// existence-leak guard).
func (a *RollbackReleaseLookupAdapter) GetLatestReleaseForBuild(ctx context.Context, tenantID, buildID string) (string, error) {
	release, err := a.Store.GetLatestByBuildID(ctx, buildID)
	if err != nil {
		return "", fmt.Errorf("rollback lookup: %w", err)
	}
	if release == nil {
		return "", nil
	}
	if release.TenantID != tenantID {
		// Cross-tenant access attempt — never expose the release ID;
		// caller treats this as "not found" (404).
		return "", nil
	}
	return release.ID, nil
}
