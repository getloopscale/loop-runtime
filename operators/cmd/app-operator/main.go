// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Group B: Application + Exposure controllers.
// Lightest operator — zero external service dependencies, mgmt cluster only.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/app"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/operatorboot"
	"github.com/getloopscale/loop-runtime/operators/internal/tracing"
	"github.com/getloopscale/loop-runtime/pkg/healthcheck"

	_ "github.com/getloopscale/loop-runtime/operators/internal/metrics"
)

var scheme = runtime.NewScheme()

func init() {
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to register client-go scheme: %v", err))
	}
	if err := loopruntimev1.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to register loop-runtime/v1 scheme: %v", err))
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

	tracingShutdown, tracingErr := tracing.Init(context.Background(), "loop-runtime-app-operator")
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

	cacheOpts, err := managerCacheOptions()
	if err != nil {
		setupLog.Error(err, "invalid manager cache scoping")
		os.Exit(1)
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: healthProbeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "loop-runtime-app-operator.runtime.getloopscale.com",
		Cache:                  cacheOpts,
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
	// The Application and Exposure controllers read workload Pods and TLS
	// Secrets in the runtime cluster of each tenant's cell.
	//
	// T1.4 of deep-review-remediation-plan (find_arch_007): CELL_0_KUBECONFIG
	// should be mandatory in production, but flipping the guard before the
	// Helm chart mounts the Secret would CrashLoopBackOff every pod (EC-4).
	// LOOP_RUNTIME_REQUIRE_CELL_0_KUBECONFIG=true opts into fail-fast; without it a
	// missing or unusable kubeconfig starts the operator with a factory that
	// reports ErrRuntimeClusterUnavailable on every lookup, which the
	// controllers surface as "not ready" rather than guessing a cluster. The
	// setup log below is the signal of that degraded mode.
	requireKubeconfig := os.Getenv("LOOP_RUNTIME_REQUIRE_CELL_0_KUBECONFIG") == "true"
	appFactory, factoryErr := runtimeFactory(mgr, os.Getenv("CELL_0_KUBECONFIG"), setupLog)
	if factoryErr != nil {
		if requireKubeconfig {
			setupLog.Error(factoryErr, "runtime cluster access required (LOOP_RUNTIME_REQUIRE_CELL_0_KUBECONFIG=true) — refusing to start")
			os.Exit(1)
		}
		setupLog.Error(factoryErr, "runtime cluster access unavailable — Application fast-path Healthy and TLS certificate checks disabled (degraded)")
		appFactory = cellrouting.NewUnavailableRuntimeClientFactory(factoryErr.Error())
	} else {
		setupLog.Info("ApplicationController: cell-aware fast-path Healthy enabled")
	}

	if err := app.SetupApplicationControllerWithFactory(mgr, baseDomain, appFactory); err != nil {
		setupLog.Error(err, "unable to setup ApplicationController")
		os.Exit(1)
	}

	// #277: the ExposureController needs the same factory as the ApplicationController —
	// the TLS Secret it looks for lives in the workload's cell, not in this cluster.
	// ROUTE_PROBE_PUBLIC_DOMAIN: probe the public URL instead of an internal route
	// hostname that resolves nowhere (#447). Unset where route hostnames are public.
	if err := app.SetupExposureController(mgr, app.ExposureControllerConfig{
		RuntimeFactory:    appFactory,
		ProbePublicDomain: os.Getenv("ROUTE_PROBE_PUBLIC_DOMAIN"),
	}); err != nil {
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

// managerCacheOptions scopes the manager's informers.
//
// The only Secret this operator reads on this cluster is a cell's kubeconfig,
// in loop-runtime-system (CellAwareRuntimeClientFactory). The chart grants exactly
// that through a namespaced Role, so the Secret informer watches that
// namespace and nothing else. The workload CRDs stay cluster-wide (ADR-0018
// C9): they live in per-tenant namespaces.
func managerCacheOptions() (cache.Options, error) {
	secretScope, err := operatorboot.SecretCache(constants.NamespacePlatformSystem)
	if err != nil {
		return cache.Options{}, err
	}
	return cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Secret{}: secretScope,
		},
	}, nil
}

// runtimeFactory builds the cell-aware factory from the CELL_0_KUBECONFIG
// path, or reports why runtime cluster access is unavailable.
func runtimeFactory(mgr ctrl.Manager, cell0Kubeconfig string, log logr.Logger) (cellrouting.RuntimeClientFactory, error) {
	if cell0Kubeconfig == "" {
		return nil, fmt.Errorf("CELL_0_KUBECONFIG is not set")
	}
	runtimeClient, err := cellrouting.CreateClusterClient(log, cell0Kubeconfig, scheme, "runtime")
	if err != nil {
		return nil, fmt.Errorf("create runtime cluster client: %w", err)
	}
	return cellrouting.NewCellAwareRuntimeClientFactory(mgr.GetClient(), runtimeClient, constants.NamespacePlatformSystem), nil
}
