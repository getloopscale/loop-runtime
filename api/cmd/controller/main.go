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
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver for sql.Open

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/loopzero/loopzero/pkg/logging"

	"github.com/loopzero/loopzero/api/internal/adapters"
	"github.com/loopzero/loopzero/api/internal/argo"
	"github.com/loopzero/loopzero/api/internal/argocd"
	"github.com/loopzero/loopzero/api/internal/config"
	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/gitops"
	"github.com/loopzero/loopzero/api/internal/k8s"
	"github.com/loopzero/loopzero/api/internal/leader"
	"github.com/loopzero/loopzero/api/internal/metrics"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/api/internal/sse"

	"k8s.io/klog/v2"
)

// leaderLoopName is the name the controller binary uses in the
// leader_generations table. Paired with cell_id from config.
const leaderLoopName = "loopzero-controller"

// healthPort is the HTTP port for /healthz and /metrics. Kept off 8080
// so it never collides with the API binary when both happen to run on
// the same node during a rolling update.
const healthPort = ":9091"

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

	// ---- Stores ----
	buildStore := db.NewPostgresBuildStore(dbAdapter)
	releaseStore := db.NewPostgresReleaseStore(dbAdapter)
	envTargetStore := db.NewPostgresEnvironmentTargetStore(dbAdapter, dbAdapter)
	pgEventStore := sse.NewPostgresEventStore(dbAdapter, dbAdapter)
	domainStore := &db.PostgresDomainStore{DB: dbAdapter}
	_ = envTargetStore // referenced by orchestrator + convergence loop

	// Environment locker (pg_advisory_xact_lock, roadmap Rule 6).
	locker := &adapters.PoolEnvironmentLocker{Pool: dbAdapter.Pool}

	// ---- Build cluster (Argo) ----
	rawWorkflowSubmitter, err := argo.NewWorkflowSubmitter(argo.WorkflowSubmitterConfig{
		Kubeconfig:       cfg.BuildKubeconfig,
		Context:          cfg.BuildKubeContext,
		RegistryURL:      cfg.RegistryURL,
		RegistryInsecure: cfg.RegistryInsecure,
		BaseDomain:       cfg.BaseDomain,
	})
	if err != nil {
		logger.Error("failed to create workflow submitter", "error", err)
		os.Exit(1)
	}
	workflowSubmitter := services.NewCircuitBreakerWorkflowSubmitter(
		rawWorkflowSubmitter,
		services.CircuitBreakerConfig{
			OpenThreshold:  cfg.CBOpenThreshold,
			CloseThreshold: cfg.CBCloseThreshold,
			RetryInterval:  cfg.CBRetryInterval,
		},
		logger,
	)

	// ---- ArgoCD sync client ----
	argocdHTTPClient := &http.Client{Timeout: cfg.ArgocdClientTimeout}
	if cfg.ArgocdInsecure {
		// #nosec G402 — Insecure is opt-in, used for self-signed in dev clusters.
		argocdHTTPClient.Transport = &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		}
	}
	rawSyncClient := argocd.NewSyncClient(argocd.SyncClientConfig{
		BaseURL:    cfg.ArgocdURL,
		Token:      cfg.ArgocdToken,
		Insecure:   cfg.ArgocdInsecure,
		HTTPClient: argocdHTTPClient,
	})
	syncClient := services.NewCircuitBreakerSyncClient(rawSyncClient, services.CircuitBreakerConfig{
		OpenThreshold:  cfg.CBOpenThreshold,
		CloseThreshold: cfg.CBCloseThreshold,
		RetryInterval:  cfg.CBRetryInterval,
	}, logger)

	// ---- Gitea gitops pusher ----
	gitopsPusher := gitops.NewGiteaGitOpsPusher(gitops.GiteaGitOpsPusherConfig{
		BaseURL: cfg.GiteaURL,
		Token:   cfg.GiteaToken,
		OrgName: "loopzero",
	})

	// ---- k8s managers (mgmt cluster) ----
	tenantCRMgr, err := k8s.NewTenantCRManager(cfg.MgmtKubeconfig, cfg.MgmtKubeContext)
	if err != nil {
		logger.Error("failed to create tenant CR manager", "error", err)
		os.Exit(1)
	}
	appCRMgr := &appCRAdapter{mgr: k8s.NewApplicationCRManager(tenantCRMgr.Client, "")}

	// ---- Build orchestrator (required by reconcile loop) ----
	orch := services.NewBuildOrchestrator(services.BuildOrchestratorDeps{
		BuildStore:        buildStore,
		ReleaseStore:      releaseStore,
		EnvTargetStore:    envTargetStore,
		ProjectStore:      &db.PostgresProjectStore{DB: dbAdapter},
		EventStore:        pgEventStore,
		Workflow:          workflowSubmitter,
		Sync:              syncClient,
		Locker:            locker,
		GitOps:            gitopsPusher,
		RegistryURL:       cfg.RegistryURL,
		ArgocdSyncTimeout: cfg.ArgocdSyncTimeout,
		BaseDomain:        cfg.BaseDomain,
	})

	// ---- Leader election via generation counter (roadmap 02) ----
	// The controller binary is the FIRST consumer of internal/leader.
	// Acquire the lease before starting any loop. Heartbeat every 5 s.
	// On ErrFenced (another controller took over), exit immediately and
	// let the orchestrator restart us.
	acquirer := leader.NewAcquirer(dbAdapter, leader.DefaultConfig())
	holder := podName()
	lease, err := acquirer.Acquire(shutdownCtx, leaderLoopName, cellID(cfg), holder)
	if err != nil {
		if errors.Is(err, leader.ErrNotLeader) {
			logger.Info("another controller instance holds the lease — exiting so orchestrator can retry")
			// Exit 0 so Kubernetes treats this as a non-error; the peer is
			// healthy and doing the work. If we exited 1 we would loop in
			// CrashLoopBackOff against a live peer.
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

	// Heartbeat goroutine — if we lose the lease, exit for re-election.
	var bgWg sync.WaitGroup
	bgWg.Add(1)
	go runHeartbeat(shutdownCtx, &bgWg, acquirer, lease, logger, cancel)

	// ---- Build reconcile loop ----
	reconcileLoop := services.NewBuildReconcileLoop(
		buildStore,
		workflowSubmitter,
		orch,
		locker,
		domainStore,
		appCRMgr,
		pgEventStore,
		logger,
		services.BuildReconcileLoopConfig{
			Interval:           cfg.ReconcileInterval,
			RegistryURL:        cfg.RegistryURL,
			BaseDomain:         cfg.BaseDomain,
			MaxBuildsPerTenant: cfg.MaxBuildsPerTenant,
			CellID:             cellID(cfg),
		},
	)

	// ---- Build reaper ----
	buildReaper := services.NewBuildReaper(
		buildStore,
		&services.PostgresStaleBuildChecker{DB: dbAdapter},
		locker,
		workflowSubmitter,
		pgEventStore,
		logger,
		services.BuildReaperConfig{
			QueueTimeout:     cfg.BuildQueueTimeout,
			RunningTimeout:   cfg.BuildRunningTimeout,
			ReleasingTimeout: cfg.BuildReleasingTimeout,
			CancelTimeout:    cfg.BuildCancelTimeout,
			Interval:         cfg.BuildReaperInterval,
			CellID:           cellID(cfg),
		},
		reconcileLoop,
	)

	// ---- Release reaper ----
	releaseReaper := services.NewReleaseReaper(
		releaseStore,
		&services.PostgresStaleReleaseChecker{DB: dbAdapter},
		locker,
		pgEventStore,
		logger,
		services.ReleaseReaperConfig{
			ReadyTimeout:   cfg.ReleaseReadyTimeout,
			PendingTimeout: cfg.ReleasePendingTimeout,
			Interval:       cfg.ReleaseReaperInterval,
			CellID:         cellID(cfg),
		},
	)

	// ---- Release convergence loop ----
	releaseConvergenceLoop := services.NewReleaseConvergenceLoop(
		releaseStore,
		envTargetStore,
		syncClient,
		locker,
		pgEventStore,
		cfg.BaseDomain,
		logger,
		services.ReleaseConvergenceLoopConfig{
			Interval:      10 * time.Second,
			SyncTimeout:   300 * time.Second,
			MaxConcurrent: 10,
			CellID:        cellID(cfg),
		},
	)

	// ---- Idempotency cleaner ----
	idempotencyCleaner := services.NewIdempotencyCleaner(
		&db.PostgresIdempotencyStore{DB: dbAdapter},
		logger,
		1*time.Hour,
	)

	// ---- SSE event pruner ----
	eventPruner := sse.NewEventPruner(
		pgEventStore,
		logger,
		24*time.Hour,
		1*time.Hour,
	)

	// ---- Start all loops ----
	bgWg.Add(6)
	go func() { defer bgWg.Done(); reconcileLoop.Start(shutdownCtx) }()
	go func() { defer bgWg.Done(); buildReaper.Start(shutdownCtx) }()
	go func() { defer bgWg.Done(); releaseReaper.Start(shutdownCtx) }()
	go func() { defer bgWg.Done(); releaseConvergenceLoop.Start(shutdownCtx) }()
	go func() { defer bgWg.Done(); idempotencyCleaner.Start(shutdownCtx) }()
	go func() { defer bgWg.Done(); eventPruner.Start(shutdownCtx) }()

	// ---- Minimal HTTP surface for liveness + metrics ----
	healthSrv := newHealthServer(logger, acquirer, lease)
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("health server error", "error", err)
		}
	}()

	logger.Info("loopzero-controller ready — 6 loops running")

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

// runHeartbeat refreshes the leader lease every HeartbeatInterval. On
// ErrFenced it sets LeaderStatus=0 and invokes cancelRoot() which cascades
// to all loop contexts. The process then exits via the normal shutdown
// path and Kubernetes restarts it; the next Acquire will either succeed
// (we become the new leader) or block until the peer dies.
func runHeartbeat(
	ctx context.Context,
	wg *sync.WaitGroup,
	acquirer *leader.Acquirer,
	lease *leader.Lease,
	logger *slog.Logger,
	cancelRoot context.CancelFunc,
) {
	defer wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hbCtx, hbCancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := acquirer.Heartbeat(hbCtx, lease)
			hbCancel()
			if err == nil {
				metrics.LeaderStatus.Set(1)
				continue
			}
			if errors.Is(err, leader.ErrFenced) {
				logger.Error("leader heartbeat fenced — another controller has taken over, exiting")
				metrics.LeaderStatus.Set(0)
				cancelRoot()
				return
			}
			// Transient error — log and retry on the next tick.
			logger.Warn("leader heartbeat transient error — will retry", "error", err)
		}
	}
}

// newHealthServer returns a minimal HTTP server exposing /healthz (liveness),
// /readyz (readiness — reports lease state), and /metrics (Prometheus).
// It listens on healthPort.
func newHealthServer(logger *slog.Logger, acquirer *leader.Acquirer, lease *leader.Lease) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// Readiness reflects lease health: if we lost the lease, we are
		// not ready to handle work until re-elected.
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := acquirer.Validate(ctx, lease); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "fenced",
				"error":  err.Error(),
			})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"generation": lease.GenerationID,
			"holder":     lease.Holder,
			"name":       lease.Name,
			"cell_id":    lease.CellID,
		})
	})

	mux.Handle("/metrics", promhttp.Handler())

	return &http.Server{
		Addr:              healthPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// cellID returns the cell to which this controller instance is scoped. It
// reads LOOPZERO_CELL_ID; if unset, falls back to the roadmap 03 default so a
// single-cell deployment works out of the box.
func cellID(_ config.Config) string {
	if v := os.Getenv("LOOPZERO_CELL_ID"); v != "" {
		return v
	}
	return db.CellDefault
}

// podName returns the holder identifier for the leader lease. Prefers
// HOSTNAME (which Kubernetes sets to the pod name) and falls back to
// "loopzero-controller" to remain usable outside Kubernetes.
func podName() string {
	if n := os.Getenv("HOSTNAME"); n != "" {
		return n
	}
	return "loopzero-controller"
}

// silenceKlog redirects the klog output from the embedded Kubernetes client
// libraries away from stderr so it does not break structured JSON logging.
func silenceKlog() {
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	_ = klogFlags.Set("logtostderr", "false")
	_ = klogFlags.Set("alsologtostderr", "false")
	_ = klogFlags.Set("stderrthreshold", "FATAL")
}

// appCRAdapter bridges k8s.ApplicationCRManager to services.ApplicationCREnsurer.
// Duplicated from api/cmd/main.go — the two binaries intentionally do not
// share a package here because cmd/main.go has HTTP-specific helpers the
// controller should never import.
type appCRAdapter struct {
	mgr *k8s.ApplicationCRManager
}

func (a *appCRAdapter) EnsureLoopZeroApplication(ctx context.Context, input services.ApplicationCRInput) error {
	return a.mgr.EnsureLoopZeroApplication(ctx, k8s.LoopZeroApplicationInput{
		TenantID:    input.TenantID,
		Project:     input.Project,
		Environment: input.Environment,
		AppName:     input.AppName,
		ServiceName: input.ServiceName,
		ServicePort: input.ServicePort,
	})
}
