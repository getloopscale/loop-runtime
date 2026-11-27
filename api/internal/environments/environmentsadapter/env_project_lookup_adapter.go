// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package environmentsadapter

import (
	"context"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/environments/environmentshttp"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// EnvProjectLookupAdapter wires the canonical routes.ProjectStore
// (ports.ProjectStore — backed by PostgresProjectStore) into the
// EnvironmentHandler's M2 v1.2 cross-tenant gate (EC-1 SEPA-CRITICAL).
//
// Why a thin adapter:
//   - The handler depends on the smaller `EnvProjectLookup` interface
//     (single method `ProjectExistsForTenant`) per ISP — the bulk of
//     ProjectStore (Create/List/Delete) is irrelevant to the gate.
//   - GetByName already scopes by tenant_id in the SQL
//     (queries/projects.sql line 41 `WHERE tenant_id = $1 AND name = $2`),
//     so the adapter is a pure pass-through with nil-check semantics
//     for "exists".
//
// Plan: 2026-06-07-runtime-envs-per-project-m2 v1.2 T4.1 wiring.
// Sponsor 2026-06-08: "let's run the LIVE test, shall we?" —
// the LIVE bash test caught that the gate was unwired in setup_server.go
// (handler shipped with the EnvProjectLookup interface, no production
// implementation injected). This adapter closes that gap.
type EnvProjectLookupAdapter struct {
	Store ports.ProjectStore
}

// ProjectExistsForTenant returns true when (tenant_id, projectName) tuple
// exists in the projects table. False (no error) when absent — handler
// maps to 404 PROJECT_NOT_FOUND. Errors propagate as transient I/O
// failures (handler maps to 500).
func (a *EnvProjectLookupAdapter) ProjectExistsForTenant(ctx context.Context, tenantID, projectName string) (bool, error) {
	if a == nil || a.Store == nil {
		// This lookup IS the cross-tenant gate. Unwired, it cannot say whether
		// the project belongs to the tenant, so it fails closed: a wiring slip
		// must surface as an error, never as access to every project.
		return false, fmt.Errorf("environment project lookup: no project store wired, cannot verify %q for tenant %s",
			projectName, tenantID)
	}
	project, err := a.Store.GetByName(ctx, tenantID, projectName)
	if err != nil {
		return false, err
	}
	// GetByName returns (nil, nil) when the tuple is absent (per
	// projects.sql NoRows convention). Non-nil = exists.
	return project != nil, nil
}

// The port this adapter satisfies, asserted at compile time.
var _ environmentshttp.EnvProjectLookup = (*EnvProjectLookupAdapter)(nil)
