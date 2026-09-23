// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Group C: Preview controller.
// Needs the CELL_0_KUBECONFIG cluster (the default for unplaced tenants), the
// ClusterCredentials of every other cell, and BaseDomain.
// Different scaling profile: TTL checks every 5 minutes.
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
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/operatorboot"
	"github.com/getloopscale/loopzero/operators/internal/preview"
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

	tracingShutdown, tracingErr := tracing.Init(context.Background(), "loopzero-preview-operator")
	if tracingErr != nil {
		setupLog.Error(tracingErr, "tracing init failed — continuing without tracing")
	}
	defer func() { _ = tracingShutdown(context.Background()) }()

	// Audit gap M04 #23 (§6.F.1b): RegisterTraceExtractor + the package-level
	// FromContext consumer were removed; this call was write-only into a
	// registry no one read. ContextLogger via NewContextLogger is the
	// replacement per ADR-0034 if trace_id-in-slog is needed.

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
		LeaderElectionID:       "loopzero-preview-operator.loopzero.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	cell0Kubeconfig := os.Getenv("CELL_0_KUBECONFIG")
	if cell0Kubeconfig == "" {
		setupLog.Error(nil, "CELL_0_KUBECONFIG is required")
		os.Exit(1)
	}
	runtimeClient, err := cellrouting.CreateClusterClient(setupLog, cell0Kubeconfig, scheme, "runtime")
	if err != nil {
		setupLog.Error(err, "failed to create runtime cluster client")
		os.Exit(1)
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		setupLog.Error(nil, "BASE_DOMAIN is required")
		os.Exit(1)
	}

	// Previews are created in the runtime cluster of their tenant's cell, the
	// same cell the tenant-operator points the preview's ArgoCD Application at.
	runtimeFactory := cellrouting.NewCellAwareRuntimeClientFactory(
		mgr.GetClient(), runtimeClient, constants.NamespacePlatformSystem)
	if err := preview.SetupPreviewController(mgr, runtimeFactory, baseDomain); err != nil {
		setupLog.Error(err, "unable to setup PreviewController")
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

	setupLog.Info("starting preview-operator", "leaderElection", enableLeaderElection)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
