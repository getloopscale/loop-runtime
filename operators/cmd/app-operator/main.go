// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Group B: Application + Exposure controllers.
// Lightest operator — zero external service dependencies, mgmt cluster only.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"


	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/app"
	"github.com/getloopscale/loopzero/operators/internal/shared"
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

	if shared.PprofEnabled() {
		shared.StartPprofServer()
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
	// per ADR D3 of the plan): RUNTIME_KUBECONFIG should be MANDATORY in
	// production, but EC-4 of the edge-case review warned that flipping the
	// guard mid-rollout (when the Helm chart hasn't yet mounted the Secret)
	// CrashLoopBackOffs every operator pod. Compromise:
	//
	//   - Set LOOPZERO_REQUIRE_RUNTIME_KUBECONFIG=true to opt into fail-fast mode
	//     (post-T1.4a Helm chart deploy). Operator exits with non-zero if
	//     RUNTIME_KUBECONFIG is missing OR factory creation fails.
	//   - Leave LOOPZERO_REQUIRE_RUNTIME_KUBECONFIG unset (default) for graceful
	//     fallback (legacy path; warning emitted; no fast-path Healthy).
	//
	// Production cells flip the env var to true ONCE T1.4a Helm mount is
	// observed via task doctor. Pre-flip, the warning + loopzero_app_operator_
	// cell_factory_disabled_total counter (T1.5/find_arch_007 wired) provides
	// observability of the degraded mode.
	var appFactory shared.RuntimeClientFactory
	requireKubeconfig := os.Getenv("LOOPZERO_REQUIRE_RUNTIME_KUBECONFIG") == "true"
	runtimeKubeconfig := os.Getenv("RUNTIME_KUBECONFIG")
	if runtimeKubeconfig != "" {
		runtimeClient, err := shared.CreateClusterClient(setupLog, runtimeKubeconfig, scheme, "runtime")
		if err != nil {
			if requireKubeconfig {
				setupLog.Error(err, "failed to create runtime cluster client AND LOOPZERO_REQUIRE_RUNTIME_KUBECONFIG=true — refusing to start")
				os.Exit(1)
			}
			setupLog.Error(err, "failed to create runtime cluster client — fast-path Healthy disabled (degraded)")
		} else {
			appFactory = shared.NewCellAwareRuntimeClientFactory(
				mgr.GetClient(), runtimeClient, "loopzero-system")
			setupLog.Info("ApplicationController: cell-aware fast-path Healthy enabled")
		}
	} else {
		if requireKubeconfig {
			setupLog.Error(nil, "RUNTIME_KUBECONFIG required (LOOPZERO_REQUIRE_RUNTIME_KUBECONFIG=true) but not set — refusing to start")
			os.Exit(1)
		}
		setupLog.Info("RUNTIME_KUBECONFIG not set — Application fast-path Healthy disabled (legacy mode)")
	}

	if err := app.SetupApplicationControllerWithFactory(mgr, baseDomain, appFactory); err != nil {
		setupLog.Error(err, "unable to setup ApplicationController")
		os.Exit(1)
	}

	if err := app.SetupExposureController(mgr); err != nil {
		setupLog.Error(err, "unable to setup ExposureController")
		os.Exit(1)
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
