// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for sql.Open("pgx", ...)

	"github.com/getloopscale/loop-runtime/pkg/enforcement"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/health/healthhttp"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
)

// DatabaseDeps holds everything produced by setupDatabase.
type DatabaseDeps struct {
	DBAdapter *db.PgxAdapter
	// MemberStore is set by wireCrossTenantLookups once the cross-tenant pool
	// exists: resolving a user's teams spans tenants.
	MemberStore  auth.MembershipStore
	ProjectStore ports.ProjectStore
	// PostgresProjects is the same instance as ProjectStore, typed concretely
	// so the per-Account project cap can be wired onto it without a type
	// assertion in main.
	PostgresProjects *store.PostgresProjectStore
	DBHealth         healthhttp.DBHealthChecker
	// PlanLimits and PlanLimiter are set by wirePlanEnforcement once the
	// cross-tenant background pool exists — not by setupDatabase: the plan
	// lookups cannot run on the request pool (see store.PlanLimitsStore).
	PlanLimits  *store.PlanLimitsStore
	PlanLimiter *enforcement.PlanLimiter
	// PoolCollector exports PG pool metrics (caller must defer Stop).
	PoolCollector *metrics.PoolStatsCollector
}

// ErrDatabaseURLNotSet reports a missing DATABASE_URL at boot.
//
// # Why this is an error and not a degradation
//
// It used to return `(nil, nil)` under the name "graceful degradation". Three
// measurements retired that name
// (MEET-ASYNC-AMENDMENT-2026-08-17-no-database-is-a-config-error):
//
//   - The degradation never once executed. `buildServerConfig` read `dbDeps` and
//     `stores` fields ABOVE its own `if != nil` guards, so the process logged the
//     warning and died three functions later on a nil dereference (B-104,
//     reproduced by `setup_server_nil_deps_test.go` before any change).
//   - Its blast radius is not one surface: 27 of the router's 36 register "sem
//     banco" (no database) as their disable reason. A missing secret does not degrade the
//     product, it erases three quarters of it — while `/api/v1/health` keeps
//     answering 200, because that probe is the shallow one.
//   - It has happened, and this repository records the damage: B35, 2026-04-28
//     (`infra/helm/loop-runtime-api/templates/deployment.yaml`) — the secret stored the
//     key as `uri`, the mapping was missing, and loop-runtime-api "runs without DB",
//     found by someone investigating something else.
//
// The distinction the old code collapsed is boot PRECONDITION vs runtime
// DEPENDENCY. An unreachable database is transient and already fails correctly
// (`db.NewPool` returns an error, `main` exits). An absent variable is a
// configuration error: no amount of waiting produces it.
//
// Returning `nil, nil` for "it did not work, but carry on" is the magic value
// `rules/error-handling.md` exists to forbid.
var ErrDatabaseURLNotSet = errors.New("DATABASE_URL is not set — the API cannot serve without a database (27 of the 36 surfaces depend on it)")

// setupDatabase creates the pgx connection pool, pool metrics collector,
// stdlib DB for plan enforcement, and Sprint 1 stores.
// Fails with ErrDatabaseURLNotSet when DATABASE_URL is absent.
func setupDatabase(cfg config.Config, logger *slog.Logger) (*DatabaseDeps, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, ErrDatabaseURLNotSet
	}

	poolCfg, err := db.NewPool(context.Background(), dbURL, requestPoolConfig(cfg))
	if err != nil {
		return nil, err
	}

	// Tenant isolation is a boot precondition, not a runtime hope.
	//
	// This pool serves HTTP requests. If its role can bypass RLS — by holding
	// BYPASSRLS, or by owning the tables while FORCE is off — then every
	// isolation policy is inert and a missing WHERE clause silently returns
	// another tenant's rows. That was the measured state on 2026-08-14, and it
	// survived review precisely because nothing observable changed.
	//
	// Failing to start is loud and fixed in seconds by pointing DATABASE_URL at
	// loop_runtime_api. Serving cross-tenant data is neither.
	if err := db.AssertRequestPoolIsolated(context.Background(), poolCfg.Pool); err != nil {
		return nil, err
	}

	// PG pool stats collector — exports connection metrics every 15s.
	poolCollector := metrics.NewPoolStatsCollector(poolCfg.Pool, logger)
	poolCollector.Start(context.Background(), 15*time.Second)

	// Concrete project store kept in a typed var so M4 can wire the per-Account
	// project-cap checker onto it once the PlanLimiter is built below.
	projectStore := &store.PostgresProjectStore{DB: poolCfg}
	deps := &DatabaseDeps{
		DBAdapter:        poolCfg,
		ProjectStore:     projectStore,
		PostgresProjects: projectStore,
		DBHealth:         poolCfg,
		PoolCollector:    poolCollector,
	}

	// Plan enforcement and the per-Account project cap are wired in main, once
	// the cross-tenant (loop_runtime_worker) connection they read through exists — see
	// wirePlanEnforcement and projectCapEnforcement.
	return deps, nil
}

// requestPoolConfig is the pool that serves HTTP requests. It scopes every
// connection to the tenant the request was authenticated for
// (dbtx.WithTenantScope): the request role is RLS-bound, and a connection with
// no scope reads no tenant's rows.
func requestPoolConfig(cfg config.Config) db.PoolConfig {
	pc := backgroundPoolConfig(cfg)
	pc.TenantScoped = true
	return pc
}

// backgroundPoolConfig is the sizing both pools share. The background pool
// uses it as is: its role crosses tenants through named policies.
func backgroundPoolConfig(cfg config.Config) db.PoolConfig {
	return db.PoolConfig{
		MaxConns:          cfg.DBMaxConns,
		MinConns:          cfg.DBMinConns,
		MaxConnIdleTime:   cfg.DBMaxConnIdleTime,
		MaxConnLifetime:   cfg.DBMaxConnLifetime,
		HealthCheckPeriod: cfg.DBHealthCheckTime,
		ConnectTimeout:    cfg.DBConnectTimeout,
		StatementTimeout:  cfg.DBStatementTimeout,
	}
}

// wireCrossTenantLookups builds the lookups authentication makes before a team
// is selected. Resolving which teams a user belongs to spans tenants — a user
// may belong to several — so it reads through the cross-tenant pool; over the
// request pool tenant_members is hidden by fail-closed RLS and every
// authenticated call would be refused with NO_TENANT.
func wireCrossTenantLookups(deps *DatabaseDeps, crossTenant db.DBTX) {
	deps.MemberStore = &store.PostgresMembershipStore{DB: crossTenant}
}

// wirePlanEnforcement builds the plan-limits store and the limiter every plan
// gate uses on the cross-tenant background pool. Over the request pool the
// plan lookups see no subscription under fail-closed RLS and every tenant
// would be capped as Starter (store.PlanLimitsStore records the measurement).
// Quotas are not optional: main reaches this only with the background pool up,
// and exits when that pool cannot be opened.
func wirePlanEnforcement(deps *DatabaseDeps, crossTenant db.DBTX, logger *slog.Logger) {
	deps.PlanLimits = store.NewPlanLimitsStore(crossTenant)
	deps.PlanLimiter = enforcement.NewPlanLimiter(deps.PlanLimits, logger)
	logger.Info("plan enforcement enabled")
}
