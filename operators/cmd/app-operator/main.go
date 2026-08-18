// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Group B: Application + Exposure controllers.
// Lightest operator — zero external service dependencies, mgmt cluster only.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	// pgx stdlib driver — registered on import; used only when the data-plane
	// poller is enabled (default OFF). Zero runtime cost for legacy operators.
	_ "github.com/jackc/pgx/v5/stdlib"
	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/app"
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
	"github.com/getloopscale/loopzero/operators/internal/operatorboot"
	"github.com/getloopscale/loopzero/operators/internal/tracing"
	"github.com/getloopscale/loopzero/pkg/healthcheck"

	_ "github.com/getloopscale/loopzero/operators/internal/metrics"
)

var scheme = runtime.NewScheme()

func init() {
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to register client-go scheme: %v", err))
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to register loopzero/v1 scheme: %v", err))
	}
}

func main() {
	// T8.1 of deep-review-remediation-plan (closes find_infra_001 HIGH).
	// Distroless static has no shell — Dockerfile HEALTHCHECK delegates to
	// the binary itself with --healthcheck-probe. Must run BEFORE flag.Parse()
	// because flag parsing in probe mode would log noise.
	if healthcheck.MaybeRunAndExit("http://127.0.0.1:8081/healthz") {
		return
	}

	var metricsAddr string
	var healthProbeAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&healthProbeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager.")
	flag.Parse()

	opts := zap.Options{Development: false}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	tracingShutdown, tracingErr := tracing.Init(context.Background(), "loopzero-app-operator")
	if tracingErr != nil {
		setupLog.Error(tracingErr, "tracing init failed — continuing without tracing")
	}
	defer func() { _ = tracingShutdown(context.Background()) }()

	// Audit gap M04 #23 (§6.F.1b): RegisterTraceExtractor + the package-level
	// FromContext consumer were removed in commits d98c20e9 + this commit.
	// Trace IDs are still attached to OTel spans via `tracing.Init` above;
	// to also inject trace_id into slog logs, construct a logging.ContextLogger
	// via NewContextLogger(baseLogger, spanExtractorFn) and call FromContext
	// at each reconcile boundary.

	if operatorboot.PprofEnabled() {
		operatorboot.StartPprofServer()
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: healthProbeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "loopzero-app-operator.loopzero.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		setupLog.Error(nil, "BASE_DOMAIN is required")
		os.Exit(1)
	}

	// dogfood-2026-05-08-fixes T4.2 (F-6, ADR D6 + EC-5 multi-cell awareness).
	// Wire the cell-aware runtime client factory so ApplicationController can
	// fast-path EndpointsReady=true on workload Pod Ready, regardless of
	// ExposedRoute Reachable.
	//
	// T1.4 of deep-review-remediation-plan (closes find_arch_007 MEDIUM partial,
	// per ADR D3 of the plan): CELL_0_KUBECONFIG should be MANDATORY in
	// production, but EC-4 of the edge-case review warned that flipping the
	// guard mid-rollout (when the Helm chart hasn't yet mounted the Secret)
	// CrashLoopBackOffs every operator pod. Compromise:
	//
	//   - Set LOOPZERO_REQUIRE_CELL_0_KUBECONFIG=true to opt into fail-fast mode
	//     (post-T1.4a Helm chart deploy). Operator exits with non-zero if
	//     CELL_0_KUBECONFIG is missing OR factory creation fails.
	//   - Leave LOOPZERO_REQUIRE_CELL_0_KUBECONFIG unset (default) for graceful
	//     fallback (legacy path; warning emitted; no fast-path Healthy).
	//
	// Production cells flip the env var to true ONCE T1.4a Helm mount is
	// observed via task doctor. Pre-flip, the warning + loopzero_app_operator_
	// cell_factory_disabled_total counter (T1.5/find_arch_007 wired) provides
	// observability of the degraded mode.
	var appFactory cellrouting.RuntimeClientFactory
	requireKubeconfig := os.Getenv("LOOPZERO_REQUIRE_CELL_0_KUBECONFIG") == "true"
	cell0Kubeconfig := os.Getenv("CELL_0_KUBECONFIG")
	if cell0Kubeconfig != "" {
		runtimeClient, err := cellrouting.CreateClusterClient(setupLog, cell0Kubeconfig, scheme, "runtime")
		if err != nil {
			if requireKubeconfig {
				setupLog.Error(err, "failed to create runtime cluster client AND LOOPZERO_REQUIRE_CELL_0_KUBECONFIG=true — refusing to start")
				os.Exit(1)
			}
			setupLog.Error(err, "failed to create runtime cluster client — fast-path Healthy disabled (degraded)")
		} else {
			appFactory = cellrouting.NewCellAwareRuntimeClientFactory(
				mgr.GetClient(), runtimeClient, "loopzero-system")
			setupLog.Info("ApplicationController: cell-aware fast-path Healthy enabled")
		}
	} else {
		if requireKubeconfig {
			setupLog.Error(nil, "CELL_0_KUBECONFIG required (LOOPZERO_REQUIRE_CELL_0_KUBECONFIG=true) but not set — refusing to start")
			os.Exit(1)
		}
		setupLog.Info("CELL_0_KUBECONFIG not set — Application fast-path Healthy disabled (legacy mode)")
	}

	if err := app.SetupApplicationControllerWithFactory(mgr, baseDomain, appFactory); err != nil {
		setupLog.Error(err, "unable to setup ApplicationController")
		os.Exit(1)
	}

	// #277: o ExposureController precisa do mesmo factory que o ApplicationController —
	// o Secret TLS que ele procura vive na cell do workload, não neste cluster.
	if err := app.SetupExposureController(mgr, app.ExposureControllerConfig{
		RuntimeFactory: appFactory,
	}); err != nil {
		setupLog.Error(err, "unable to setup ExposureController")
		os.Exit(1)
	}

	// T2.2 of plan dashboard-badge-data-plane-poller — closes Red Team
	// loopzero-cloud#128. Opt-in periodic Prometheus scrape that persists
	// per-(tenant, project, environment) readiness snapshots into
	// env_targets. See operators/internal/app/data_plane_poller.go.
	//
	// Env gate (default OFF): registering the Runnable is an operator
	// decision because it introduces PG + Prometheus dependencies that
	// legacy app-operator deployments don't need.
	//
	// When LOOPZERO_DATA_PLANE_POLLER_ENABLED=true, LOOPZERO_DATA_PLANE_POLLER_PG_URL
	// and LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL become MANDATORY — refusing
	// to start on missing config is the honest signal (fail-fast, no
	// silent no-op that would hide misconfiguration).
	//
	// LOOPZERO_DATA_PLANE_POLLER_INTERVAL controls the scrape cadence; 30s is
	// the canonical default (blueprint Q6 + EC-7 no-flicker headroom).
	if os.Getenv("LOOPZERO_DATA_PLANE_POLLER_ENABLED") == "true" {
		pgURL := os.Getenv("LOOPZERO_DATA_PLANE_POLLER_PG_URL")
		promURL := os.Getenv("LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL")
		if pgURL == "" || promURL == "" {
			setupLog.Error(nil, "LOOPZERO_DATA_PLANE_POLLER_ENABLED=true but PG_URL / PROMETHEUS_URL missing — refusing to start")
			os.Exit(1)
		}
		interval := 30 * time.Second
		if raw := os.Getenv("LOOPZERO_DATA_PLANE_POLLER_INTERVAL"); raw != "" {
			if v, err := time.ParseDuration(raw); err == nil && v > 0 {
				interval = v
			}
		}
		db, err := sql.Open("pgx", pgURL)
		if err != nil {
			setupLog.Error(err, "data-plane-poller: sql.Open failed — refusing to start")
			os.Exit(1)
		}
		promClient, err := promapi.NewClient(promapi.Config{Address: promURL})
		if err != nil {
			setupLog.Error(err, "data-plane-poller: prom client init failed — refusing to start")
			os.Exit(1)
		}
		poller := app.NewDataPlanePoller(&app.PromV1Adapter{API: promv1.NewAPI(promClient)}, db, interval, nil)
		if err := mgr.Add(poller); err != nil {
			setupLog.Error(err, "data-plane-poller: mgr.Add failed — refusing to start")
			os.Exit(1)
		}
		setupLog.Info("data-plane-poller: enabled", "interval", interval)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting app-operator", "leaderElection", enableLeaderElection)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
