// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for sql.Open("pgx", ...)

	"github.com/getloopscale/loopzero/pkg/enforcement"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/middleware"
	"github.com/getloopscale/loopzero/api/internal/routes"
)

// DatabaseDeps holds everything produced by setupDatabase.
type DatabaseDeps struct {
	DBAdapter    *db.PgxAdapter
	MemberStore  middleware.MembershipStore
	ProjectStore routes.ProjectStore
	DBHealth     routes.DBHealthChecker
	PlanLimiter  *enforcement.PlanLimiter

	// StdDB is the stdlib sql.DB used for plan enforcement (caller must defer Close).
	StdDB *sql.DB
	// PoolCollector exports PG pool metrics (caller must defer Stop).
	PoolCollector *metrics.PoolStatsCollector
}

// setupDatabase creates the pgx connection pool, pool metrics collector,
// stdlib DB for plan enforcement, and Sprint 1 stores.
// Returns nil DatabaseDeps if DATABASE_URL is empty (graceful degradation).
func setupDatabase(cfg config.Config, logger *slog.Logger) (*DatabaseDeps, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Warn("DATABASE_URL not set — running without database")
		return nil, nil
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

	// PG pool stats collector — exports connection metrics every 15s.
	poolCollector := metrics.NewPoolStatsCollector(poolCfg.Pool, logger)
	poolCollector.Start(context.Background(), 15*time.Second)

	// Concrete project store kept in a typed var so M4 can wire the per-Account
	// project-cap checker onto it once the PlanLimiter is built below.
	projectStore := &db.PostgresProjectStore{DB: poolCfg}
	deps := &DatabaseDeps{
		DBAdapter:     poolCfg,
		MemberStore:   &db.PostgresMembershipStore{DB: poolCfg},
		ProjectStore:  projectStore,
		DBHealth:      poolCfg,
		PoolCollector: poolCollector,
	}

	// Plan enforcement limiter (uses stdlib sql.DB for simple queries)
	stdDB, err := sql.Open("pgx", dbURL)
	if err != nil {
		logger.Warn("failed to open stdlib DB for plan enforcement — limits disabled", "error", err)
	} else {
		stdDB.SetMaxOpenConns(2)
		if pingErr := stdDB.Ping(); pingErr != nil {
			logger.Warn("plan enforcement DB ping failed — limits disabled", "error", pingErr)
			_ = stdDB.Close()
		} else {
			deps.StdDB = stdDB
			deps.PlanLimiter = enforcement.NewPlanLimiter(stdDB, logger)
			// M4 (account-level tenancy, Phase 2): enforce the per-Account project
			// cap at the single project-creation path. The store resolves the cap
			// account-scoped + counts under a per-account advisory lock atomically.
			// Replaces the per-tenant 402 middleware with the canonical 422 cap.
			projectStore.SetProjectCapChecker(&planLimiterCapAdapter{limiter: deps.PlanLimiter})
			logger.Info("plan enforcement enabled")
		}
	}

	return deps, nil
}
