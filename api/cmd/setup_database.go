// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for sql.Open("pgx", ...)

	"github.com/getloopscale/loopzero/pkg/enforcement"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/routes/health"
)

// DatabaseDeps holds everything produced by setupDatabase.
type DatabaseDeps struct {
	DBAdapter    *db.PgxAdapter
	MemberStore  auth.MembershipStore
	ProjectStore ports.ProjectStore
	// PostgresProjects is the same instance as ProjectStore, typed concretely
	// so the per-Account project cap can be wired onto it without a type
	// assertion in main.
	PostgresProjects *store.PostgresProjectStore
	DBHealth         health.DBHealthChecker
	PlanLimiter      *enforcement.PlanLimiter

	// StdDB is the stdlib sql.DB used for plan enforcement (caller must defer Close).
	StdDB *sql.DB
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
//     (`infra/helm/loopzero-api/templates/deployment.yaml`) — the secret stored the
//     key as `uri`, the mapping was missing, and loopzero-api "runs without DB",
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

	poolCfg, err := db.NewPool(context.Background(), dbURL, db.PoolConfig{
		MaxConns:          cfg.DBMaxConns,
		MinConns:          cfg.DBMinConns,
		MaxConnIdleTime:   cfg.DBMaxConnIdleTime,
		MaxConnLifetime:   cfg.DBMaxConnLifetime,
		HealthCheckPeriod: cfg.DBHealthCheckTime,
		ConnectTimeout:    cfg.DBConnectTimeout,
		StatementTimeout:  cfg.DBStatementTimeout,
	})
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
	// loopzero_api. Serving cross-tenant data is neither.
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
		MemberStore:      &store.PostgresMembershipStore{DB: poolCfg},
		ProjectStore:     projectStore,
		PostgresProjects: projectStore,
		DBHealth:         poolCfg,
		PoolCollector:    poolCollector,
	}

	// Plan enforcement limiter (stdlib sql.DB for simple queries). Quotas are
	// not optional: if their connection cannot be opened, the boot fails like
	// it does for the request pool, instead of serving with every limit off.
	stdDB, err := openPlanEnforcementDB(dbURL)
	if err != nil {
		poolCollector.Stop()
		poolCfg.Close()
		return nil, err
	}
	deps.StdDB = stdDB
	deps.PlanLimiter = enforcement.NewPlanLimiter(enforcement.NewSQLLimitsStore(stdDB), logger)
	// The per-Account project cap is wired onto this store in main, once the
	// cross-tenant (loopzero_worker) connection it counts through exists — see
	// projectCapEnforcement.
	logger.Info("plan enforcement enabled")

	return deps, nil
}

// openPlanEnforcementDB opens and pings the stdlib connection plan enforcement
// runs on. A failure is returned with nothing left open.
func openPlanEnforcementDB(dbURL string) (*sql.DB, error) {
	stdDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, fmt.Errorf("plan enforcement: open database: %w", err)
	}
	stdDB.SetMaxOpenConns(2)
	if err := stdDB.Ping(); err != nil {
		if cerr := stdDB.Close(); cerr != nil {
			return nil, fmt.Errorf("plan enforcement: ping database: %w (close: %v)", err, cerr)
		}
		return nil, fmt.Errorf("plan enforcement: ping database: %w", err)
	}
	return stdDB, nil
}
