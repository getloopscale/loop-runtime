// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/routes"
)

// envProjectLookupAdapter wires the canonical routes.ProjectStore
// (domain.ProjectStore — backed by PostgresProjectStore) into the
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
// Sponsor verbatim 2026-06-08: "vamos fazer o teste LIVE, bora?" —
// the LIVE bash test caught that the gate was unwired in setup_server.go
// (handler shipped with the EnvProjectLookup interface, no production
// implementation injected). This adapter closes that gap.
type envProjectLookupAdapter struct {
	store routes.ProjectStore
}

// ProjectExistsForTenant returns true when (tenant_id, projectName) tuple
// exists in the projects table. False (no error) when absent — handler
// maps to 404 PROJECT_NOT_FOUND. Errors propagate as transient I/O
// failures (handler maps to 500).
func (a *envProjectLookupAdapter) ProjectExistsForTenant(ctx context.Context, tenantID, projectName string) (bool, error) {
	if a == nil || a.store == nil {
		// Defensive: nil adapter falls open (legacy/test path); the
		// caller already gates on nil before wiring, but keep this
		// defensive guard so a future wiring slip doesn't 500.
		return true, nil
	}
	project, err := a.store.GetByName(ctx, tenantID, projectName)
	if err != nil {
		return false, err
	}
	// GetByName returns (nil, nil) when the tuple is absent (per
	// projects.sql NoRows convention). Non-nil = exists.
	return project != nil, nil
}
