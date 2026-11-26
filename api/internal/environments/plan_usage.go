// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environments

import (
	"context"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// TenantEnvironments lists the environments a tenant has, deployed or not.
type TenantEnvironments interface {
	ListEnvironments(ctx context.Context, tenantID string) ([]ports.EnvironmentEntry, error)
}

// PlanUsage counts a tenant's permanent environments for the plan gate.
type PlanUsage struct {
	environments TenantEnvironments
}

// PlanUsageFromEnvironments counts from the list the Environments API serves.
// The deploy pointers (environment_targets) get a row at an environment's
// first release, so counting those let a tenant create any number of
// environments it never deployed to.
func PlanUsageFromEnvironments(environments TenantEnvironments) PlanUsage {
	return PlanUsage{environments: environments}
}

// PermanentEnvironments counts the (project, environment) tuples that are not
// previews. The legacy entry with no project is not one a tenant created, so
// it is left out.
func (u PlanUsage) PermanentEnvironments(ctx context.Context, tenantID string) (int, error) {
	entries, err := u.environments.ListEnvironments(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("count permanent environments of tenant %q: %w", tenantID, err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Project == "" || naming.IsPreviewEnv(entry.Name) {
			continue
		}
		count++
	}
	return count, nil
}
