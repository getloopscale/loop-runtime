// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// project_types.go — public struct for the projects domain that
// survives the T2.1 (Batch A #4) deletion of the legacy
// `project_store.go`. The 5 prod call sites use the struct-literal
// `&db.PostgresProjectStore{DB: dbAdapter}` shape — preserved here.

// ProjectCapChecker resolves the per-Account project cap. M4 (account-level
// tenancy, Phase 2): mirrors the #101 ScaleReplicasChecker shape. Returns the
// resolved plan and the cap (`cap < 0` = unlimited; `cap == 0` = fail-closed /
// no limit configured; `cap > 0` = bounded). `allowed` reflects whether `current`
// is under the cap. The create path calls it with `current=0` to resolve the cap
// up-front, then enforces `count < cap` atomically under an advisory lock.
// Co-located with the adapter in db/store/ per ADR 2026-07-03-m9-db-store-
// subpackage-topology D2 (negligible external port footprint; not a smell).
type ProjectCapChecker interface {
	CheckProjectsValue(ctx context.Context, tenantID string, current int) (allowed bool, plan string, cap int, err error)
}

// PostgresProjectStore creates projects in PostgreSQL. Implements
// domain.ProjectStore (DIP).
//
// T2.1 Batch A #4 (plan 2026-05-29-sqlc-migration v1.3): DB field
// type preserved (db.DBTX); methods (project_wrapper.go) dispatch
// through SqlcBridge to sqlc-generated queries.
//
// M4: capChecker (optional, set via SetProjectCapChecker) enables per-Account
// project-cap enforcement at creation. Nil checker = no enforcement (dev/tests).
type PostgresProjectStore struct {
	DB         db.DBTX
	capChecker ProjectCapChecker
	// crossTenant counts the Account's projects across ITS tenants. It is a
	// connection whose cross-tenant reach is a NAMED policy (loopzero_worker), not
	// the request pool — which since migration 000062 sees one tenant and would
	// silently turn a per-account cap into a per-tenant one. Nil leaves the
	// count on the caller's transaction (tests, un-provisioned databases).
	crossTenant db.DBTX
}

// SetCrossTenantCounter wires the connection used to count an Account's
// projects across its tenants. Called at composition, after the background pool
// is built and its reach verified (see cmd/setup_background_database.go).
//
// Deliberately a separate setter rather than a constructor argument: the store
// is built before the background pool exists, and the same post-wiring shape is
// already used by SetProjectCapChecker.
func (s *PostgresProjectStore) SetCrossTenantCounter(c db.DBTX) {
	s.crossTenant = c
}

// SetProjectCapChecker wires the per-Account project-cap checker (M4). Mirrors
// the #101 ScaleHandler.SetReplicasChecker wiring. Called in setup AFTER the
// production deps are wired; a nil checker leaves creation unenforced.
func (s *PostgresProjectStore) SetProjectCapChecker(c ProjectCapChecker) {
	s.capChecker = c
}
