// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
)

// setupBackgroundDatabase builds the pool the background loops run on, and
// refuses to return one that cannot legitimately cross tenants.
//
// WHY A SECOND POOL EXISTS AT ALL. The request pool is deliberately unable to
// see another tenant's rows — `AssertRequestPoolIsolated` refuses to boot
// otherwise. Background work is the opposite job: a retention pruner, a build
// reconciler and an idempotency cleaner exist precisely to look at every
// tenant. Handing them the request pool does not fail; since migration 000062
// made the tables fail-closed it makes every sweep read and delete NOTHING,
// with no error and no log line. Measured 2026-08-14 against a real database
// holding one audit_log row: owner-without-context 0, loopzero_api 0, loopzero_worker 1.
//
// The reach is a NAMED policy (`*_worker_cross_tenant`, migration 000062) held
// by a role that owns nothing and cannot run DDL. That is the difference the
// guard enforces: an auditor can enumerate this traversal in `pg_policies` and
// revoke it one table at a time — which was never true of the owner privilege
// migrations 000063/000064 removed.
//
// `EffectiveWorkerDatabaseURL()` still falls back to `DATABASE_URL` when
// LOOPZERO_WORKER_DATABASE_URL is unset. That fallback is no longer a
// "defense-in-depth gap" as its doc-comment called it — under fail-closed RLS
// it is a silent no-op — so the guard turns it into a boot failure naming the
// variable to set.
func setupBackgroundDatabase(cfg config.Config, logger *slog.Logger) (*db.PgxAdapter, error) {
	adapter, err := db.NewPool(context.Background(), cfg.EffectiveWorkerDatabaseURL(), backgroundPoolConfig(cfg))
	if err != nil {
		return nil, err
	}

	if err := db.AssertBackgroundPoolCrossTenant(context.Background(), adapter.Pool); err != nil {
		adapter.Close()
		return nil, err
	}

	// The posture is logged, not merely asserted: an operator reading the boot
	// output should be able to see WHICH policy grants the traversal, without
	// opening a psql session. A reach nobody can name is the thing this whole
	// line of work removed.
	posture, perr := db.MeasureBackgroundReach(context.Background(), adapter.Pool, "audit_log")
	if perr == nil {
		logger.Info("background pool cross-tenant reach verified",
			"role", posture.Role,
			"policy", posture.Policy,
			"worker_role_configured", cfg.WorkerRoleEnabled())
	}

	return adapter, nil
}
