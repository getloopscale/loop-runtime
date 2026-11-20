// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Group D: Platform infrastructure controllers.
// ClusterCredential + GitRepositoryBinding + PlatformBootstrap.
// Low frequency, no external provisioner clients.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/httpclient"
	"github.com/getloopscale/loopzero/operators/internal/operatorboot"
	"github.com/getloopscale/loopzero/operators/internal/platform"
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

	tracingShutdown, tracingErr := tracing.Init(context.Background(), "loopzero-platform-operator")
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

	argoCDNamespace := os.Getenv("ARGOCD_NAMESPACE")
	if argoCDNamespace == "" {
		argoCDNamespace = constants.DefaultArgoCDNamespace
	}

	cacheOpts, err := managerCacheOptions(argoCDNamespace)
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
		LeaderElectionID:       "loopzero-platform-operator.loopzero.io",
		Cache:                  cacheOpts,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	httpClient := httpclient.New()

	if err := platform.SetupClusterCredentialController(mgr, argoCDNamespace); err != nil {
		setupLog.Error(err, "unable to setup ClusterCredentialController")
		os.Exit(1)
	}

	if err := platform.SetupGitRepositoryBindingController(mgr, httpClient); err != nil {
		setupLog.Error(err, "unable to setup GitRepositoryBindingController")
		os.Exit(1)
	}

	if err := platform.SetupPlatformBootstrapController(mgr, httpClient); err != nil {
		setupLog.Error(err, "unable to setup PlatformBootstrapController")
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

	setupLog.Info("starting platform-operator", "leaderElection", enableLeaderElection)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// managerCacheOptions scopes the manager's informers.
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C16. Cache
// scoping per ADR-0018 v2 C9 + operator-split-invariants.md I-1.
// PlatformBootstrap, GitRepositoryBinding, ClusterCredential all
// live in `loopzero-system` only.
//
// Secrets are read and written in two namespaces, the ones the chart grants
// through namespaced Roles: loopzero-system (cell kubeconfigs, the Gitea admin
// Secret, platform-tokens, Git credentials) and the ArgoCD namespace (the
// cluster, repository and repo-creds Secrets the controllers write for
// ArgoCD, and its admin Secret). The Secret informer — which also feeds the
// ClusterCredential controller's Secret watch — watches those two only. A
// resource naming a Secret anywhere else fails the read loudly instead of
// reaching it.
func managerCacheOptions(argoCDNamespace string) (cache.Options, error) {
	platformCRDNamespaces := map[string]cache.Config{
		constants.NamespacePlatformSystem: {},
	}
	secretScope, err := operatorboot.SecretCache(constants.NamespacePlatformSystem, argoCDNamespace)
	if err != nil {
		return cache.Options{}, err
	}
	return cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&loopzerov1.PlatformBootstrap{}:    {Namespaces: platformCRDNamespaces},
			&loopzerov1.GitRepositoryBinding{}: {Namespaces: platformCRDNamespaces},
			&loopzerov1.ClusterCredential{}:    {Namespaces: platformCRDNamespaces},
			&corev1.Secret{}:                   secretScope,
		},
	}, nil
}
