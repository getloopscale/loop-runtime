// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import "github.com/getloopscale/loopzero/api/internal/db"

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
type ProjectCapChecker interface {
	CheckProjectsValue(tenantID string, current int) (allowed bool, plan string, cap int)
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
}

// SetProjectCapChecker wires the per-Account project-cap checker (M4). Mirrors
// the #101 ScaleHandler.SetReplicasChecker wiring. Called in setup AFTER the
// production deps are wired; a nil checker leaves creation unenforced.
func (s *PostgresProjectStore) SetProjectCapChecker(c ProjectCapChecker) {
	s.capChecker = c
}
