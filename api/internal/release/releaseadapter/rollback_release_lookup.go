// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package releaseadapter

import (
	"context"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/release/rollbackhttp"
)

// rollbackLookupWindow is how many of the environment's newest releases the
// lookup reads. A build older than that is rolled back to by release id.
const rollbackLookupWindow = 500

// RollbackReleaseLookupAdapter resolves the build id the dashboard sends into
// the release a rollback targets: the newest release of that build in the
// environment being rolled back.
//
// A build has one release per environment it reached, so the lookup is scoped
// to the environment. Taking the newest release of the build anywhere picked
// the release a promote had created in another environment.
type RollbackReleaseLookupAdapter struct {
	Store releasesOfEnvironment
}

// GetReleaseForBuild returns the id of the newest release of the build in the
// environment, or "" when the environment has none. The read is scoped to the
// tenant, so a build of another tenant is also "".
func (a *RollbackReleaseLookupAdapter) GetReleaseForBuild(ctx context.Context, tenantID, projectID, environment, buildID string) (string, error) {
	releases, err := a.Store.ListByEnv(ctx, tenantID, projectID, environment, rollbackLookupWindow)
	if err != nil {
		return "", fmt.Errorf("rollback lookup: %w", err)
	}
	newest := -1
	for i := range releases {
		if releases[i].BuildID != buildID {
			continue
		}
		if newest < 0 || releases[i].CreatedAt.After(releases[newest].CreatedAt) {
			newest = i
		}
	}
	if newest < 0 {
		return "", nil
	}
	return releases[newest].ID, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: this file did NOT declare, not even in prose, what it adapted — and the package's
// admission criterion was "it is an adapter", which refuses nothing. The port was discovered
// from the consumer, and is now written where the compiler checks it.
var _ rollbackhttp.RollbackReleaseLookup = (*RollbackReleaseLookupAdapter)(nil)
