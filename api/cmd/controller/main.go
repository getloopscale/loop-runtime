// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main is the entrypoint for the LoopZero Controller binary.
//
// # What this binary does
//
// The controller runs the platform background loops that used to live in the
// API process (build reconciliation, reapers, release convergence, event
// pruning, idempotency cleanup). See docs/roadmap/02-api-loop-extraction/
// for the full design rationale. Short version:
//
//   - The API binary must be horizontally scalable. Before this extraction,
//     the leader API replica ran HTTP traffic AND all background
//     reconciliation, which is a structural bottleneck above ~50 concurrent
//     requests.
//   - This binary owns the background loops exclusively. The API sets
//     LOOPZERO_DISABLE_LEGACY_LOOPS=true and stays stateless.
//   - Leader election uses the cell-scoped generation counter from
//     api/internal/leader so a stale leader's state transitions silently
//     no-op (see ErrFenced semantics in that package).
//   - This binary is the first consumer of api/internal/leader.Acquirer and
//     api/internal/processing.ClaimStore, the two fencing primitives added
//     in roadmap iteration 3.
//
// # What this binary is NOT
//
// It does NOT serve the HTTP API (routes.Register, auth, idempotency
// middleware, etc.). It does NOT handle GitHub webhooks. It does NOT
// manage custom domains (DomainProvisioner/DomainHealthChecker). Those
// stay in the API binary. If a loop is added here that needs a
// feature the API has, wire the dep explicitly — avoid importing
// cmd/main.go helpers.
//
// # Shutdown
//
// SIGTERM / SIGINT cancels the background context. The main goroutine
// waits up to 30 s for in-flight reconciliations to finish, releases the
// leader lease, and exits 0. If the wait exceeds 30 s a warning is
// logged and the binary exits anyway; Kubernetes will kill us at
// terminationGracePeriodSeconds (default 60 s) regardless.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for sql.Open

	"github.com/getloopscale/loop-runtime/pkg/logging"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/leader"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

func main() {
	silenceKlog()

	_ = flag.CommandLine.Parse(os.Args[1:])

	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "controller: invalid config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.NewLogger("loopzero-controller").With(
		"component", "controller",
		"cell_id", cellID(cfg),
	)
	logger.Info("starting loopzero-controller",
		"registry_url", cfg.RegistryURL,
		"base_domain", cfg.BaseDomain,
		"reconcile_interval", cfg.ReconcileInterval,
	)

	// A root context that cancels on SIGTERM / SIGINT. All loops derive
	// from this so a single ^C unwinds the whole process.
	shutdownCtx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ---- Database ----
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("DATABASE_URL is required — controller cannot run without PG")
		os.Exit(1)
	}
	dbAdapter, err := db.NewPool(context.Background(), dbURL, db.PoolConfig{
		MaxConns:          cfg.DBMaxConns,
		MinConns:          cfg.DBMinConns,
		MaxConnIdleTime:   cfg.DBMaxConnIdleTime,
		MaxConnLifetime:   cfg.DBMaxConnLifetime,
		HealthCheckPeriod: cfg.DBHealthCheckTime,
		ConnectTimeout:    cfg.DBConnectTimeout,
		StatementTimeout:  cfg.DBStatementTimeout,
	})
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbAdapter.Close()

	// Export pool saturation metrics — same collector the API uses.
	poolCollector := metrics.NewPoolStatsCollector(dbAdapter.Pool, logger)
	poolCollector.Start(context.Background(), 15*time.Second)
	defer poolCollector.Stop()

	// WS.1: Track service auth secret age for rotation alerting.
	secretAgeCollector := metrics.NewSecretAgeCollector(cfg.ServiceAuthSecretCreatedAt, logger)
	secretAgeCollector.Start(context.Background())
	defer secretAgeCollector.Stop()

	// ---- Services + orchestrator ----
	loops, err := setupServices(cfg, dbAdapter, logger)
	if err != nil {
		logger.Error("controller setup failed", "error", err)
		os.Exit(1)
	}

	// ---- Leader election via generation counter (roadmap 02) ----
	acquirer := leader.NewAcquirer(dbAdapter, leader.DefaultConfig())
	holder := podName()
	lease, err := acquirer.Acquire(shutdownCtx, leaderLoopName, cellID(cfg), holder)
	if err != nil {
		if errors.Is(err, leader.ErrNotLeader) {
			logger.Info("another controller instance holds the lease — exiting so orchestrator can retry")
			os.Exit(0)
		}
		logger.Error("leader acquire failed", "error", err)
		os.Exit(1)
	}
	logger.Info("leader lease acquired",
		"name", lease.Name,
		"cell_id", lease.CellID,
		"generation", lease.GenerationID,
	)
	metrics.LeaderStatus.Set(1)

	// ---- Start background loops ----
	var bgWg sync.WaitGroup
	bgWg.Add(1)
	go runHeartbeat(shutdownCtx, &bgWg, acquirer, lease, logger, cancel)
	startBackgroundLoops(shutdownCtx, &bgWg, loops, logger)
	// Compute-metering producer (plan metering-compute-loop T3.2 S5b). Leader-gated
	// via the lease above; observe-only + non-fatal (a wiring failure logs + skips
	// metering, never crashes the controller).
	startMeteringLoop(shutdownCtx, &bgWg, cfg, *lease, logger)

	// ---- Minimal HTTP surface for liveness + metrics ----
	healthSrv := newHealthServer(logger, acquirer, lease)
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("health server error", "error", err)
		}
	}()

	logger.Info("loopzero-controller ready — background loops running", "loops", len(loops))

	// Block until SIGTERM / SIGINT or a fatal heartbeat failure.
	<-shutdownCtx.Done()
	logger.Info("shutdown signal received — stopping loops")

	// Stop the health server with its own deadline.
	shutdownTimer, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = healthSrv.Shutdown(shutdownTimer)

	// Wait up to 30 s for loops to finish.
	done := make(chan struct{})
	go func() { bgWg.Wait(); close(done) }()
	select {
	case <-done:
		logger.Info("all background goroutines stopped")
	case <-time.After(30 * time.Second):
		logger.Warn("background goroutines did not stop within 30s — exiting anyway")
	}

	// Best-effort lease release — marks the slot as stale so the next
	// controller starts cleanly instead of waiting for the stale window.
	releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer releaseCancel()
	if err := acquirer.Release(releaseCtx, lease); err != nil && !errors.Is(err, leader.ErrFenced) {
		logger.Warn("leader release failed", "error", err)
	}
	metrics.LeaderStatus.Set(0)
	logger.Info("loopzero-controller stopped cleanly")
}
