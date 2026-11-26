// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Group A: Tenant + Environment controllers.
// Heaviest operator — owns all external provisioners (Gitea, ArgoCD, MinIO)
// and both cross-cluster clients (build + runtime).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/argocd"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/environment"
	"github.com/getloopscale/loop-runtime/operators/internal/gitea"
	"github.com/getloopscale/loop-runtime/operators/internal/metrics"
	miniop "github.com/getloopscale/loop-runtime/operators/internal/minio"
	"github.com/getloopscale/loop-runtime/operators/internal/operatorboot"
	"github.com/getloopscale/loop-runtime/operators/internal/tenant"
	"github.com/getloopscale/loop-runtime/operators/internal/tracing"
	cb "github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
	"github.com/getloopscale/loop-runtime/pkg/healthcheck"
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

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		setupLog.Error(err, "invalid tenant-operator configuration")
		os.Exit(1)
	}

	tracingShutdown, tracingErr := tracing.Init(context.Background(), "loopzero-tenant-operator")
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
		LeaderElectionID:       "loopzero-tenant-operator.loopzero.io",
		Cache:                  cacheOpts,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err := setupControllers(mgr, cfg); err != nil {
		setupLog.Error(err, "unable to set up the tenant-operator controllers")
		os.Exit(1)
	}

	// --- Health ---
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting tenant-operator", "leaderElection", enableLeaderElection)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// managerCacheOptions scopes the manager's informers.
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C16 +
// operator-split-invariants.md I-1. Cache scoping per ADR-0018 v2 C9.
// Tenant + Environment cache restricted to `loopzero-system`; reading
// these CRs from any other namespace returns empty list (so a CR
// applied to `default` is invisible to this controller, fail-safe
// for the dogfood iter 7-8 misplacement vector).
//
// Secrets are read in `loopzero-system` only — the kubeconfig Secret of each
// cell's ClusterCredential and the registry pull Secret the Environment
// controller copies into workload namespaces. The chart grants exactly that
// through a namespaced Role, so the Secret informer watches that namespace
// and nothing else.
func managerCacheOptions() (cache.Options, error) {
	platformCRDNamespaces := map[string]cache.Config{
		constants.NamespacePlatformSystem: {},
	}
	secretScope, err := operatorboot.SecretCache(constants.NamespacePlatformSystem)
	if err != nil {
		return cache.Options{}, err
	}
	return cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&loopzerov1.Tenant{}:      {Namespaces: platformCRDNamespaces},
			&loopzerov1.Environment{}: {Namespaces: platformCRDNamespaces},
			&corev1.Secret{}:          secretScope,
		},
	}, nil
}

// newBreaker is the circuit breaker every external provisioner gets: open
// after 5 consecutive failures, one probe every 2 minutes, state changes
// published as metrics.
func newBreaker(name string) *cb.Breaker {
	return cb.New(cb.Config{
		Name: name, OpenThreshold: 5, CloseThreshold: 1, RetryInterval: 120 * time.Second,
	}, metrics.ObserveCircuitBreakerTransition)
}

// setupControllers builds the provisioners and cluster clients cfg
// describes and registers the Tenant and Environment controllers and the
// misplaced-Tenant scan on mgr.
func setupControllers(mgr ctrl.Manager, cfg config) error {
	gitProvisioner := gitea.NewCircuitBreakerProvisioner(
		gitea.NewRealProvisioner(cfg.GiteaURL, cfg.GiteaToken, cfg.GiteaOrg), newBreaker("gitea"), slog.Default())

	dynClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return fmt.Errorf("dynamic client for the ArgoCD provisioner: %w", err)
	}
	argoCDProvisioner := argocd.NewCircuitBreakerProvisioner(
		argocd.NewRealProvisioner(cfg.ArgoCDNamespace, dynClient), newBreaker("argocd"), slog.Default())

	setupLog := ctrl.Log.WithName("setup")
	buildClient, err := cellrouting.CreateClusterClient(setupLog, cfg.BuildKubeconfig, scheme, "build")
	if err != nil {
		return fmt.Errorf("build cluster client: %w", err)
	}
	runtimeClient, err := cellrouting.CreateClusterClient(setupLog, cfg.Cell0Kubeconfig, scheme, "runtime")
	if err != nil {
		return fmt.Errorf("runtime cluster client: %w", err)
	}
	// The runtime cluster of each tenant's cell; cellID "" (never placed)
	// resolves to the CELL_0_KUBECONFIG cluster. Tenant and Environment
	// controllers share it, and its per-cell client cache.
	runtimeFactory := cellrouting.NewCellAwareRuntimeClientFactory(
		mgr.GetClient(), runtimeClient, constants.NamespacePlatformSystem)

	minIOProvisioner, err := newMinIOProvisioner(cfg.MinIO)
	if err != nil {
		return err
	}

	minIOEndpoint := ""
	if cfg.MinIO != nil {
		minIOEndpoint = cfg.MinIO.Endpoint
	}
	if err := tenant.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner, buildClient, runtimeFactory, tenant.TenantControllerConfig{
		GiteaURL:                cfg.GiteaURL,
		WorkloadClusterURL:      cfg.WorkloadClusterURL,
		MinIOProvisioner:        minIOProvisioner,
		RegistryCIDRs:           cfg.RegistryCIDRs,
		MaxConcurrentReconciles: cfg.TenantMaxConcurrent,
		Credentials: tenant.BuildCredentials{
			MinIOEndpoint:         minIOEndpoint,
			SpacesAccessKeyID:     cfg.SpacesAccessKeyID,
			SpacesSecretAccessKey: cfg.SpacesSecretKey,
			DOToken:               cfg.DOToken,
			RegistryDockerConfig:  cfg.RegistryDockerConfig,
		},
	}); err != nil {
		return fmt.Errorf("TenantController: %w", err)
	}
	if err := environment.SetupEnvironmentControllerWithFactory(mgr, runtimeFactory); err != nil {
		return fmt.Errorf("EnvironmentController: %w", err)
	}
	// ADR-0018 C2/C5: the cache cannot see a Tenant outside loopzero-system, so an
	// uncached cluster-wide scan publishes loopzero_misplaced_tenant_total.
	if err := mgr.Add(&tenant.MisplacedTenantScanner{Reader: mgr.GetAPIReader()}); err != nil {
		return fmt.Errorf("misplaced Tenant scan: %w", err)
	}
	return nil
}

// newMinIOProvisioner returns nil when MinIO is not configured.
func newMinIOProvisioner(cfg *minIOConfig) (miniop.CredentialProvisioner, error) {
	if cfg == nil {
		return nil, nil
	}
	raw, err := miniop.NewAdminProvisioner(miniop.AdminConfig{
		Endpoint: cfg.Endpoint, AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey,
	})
	if err != nil {
		return nil, fmt.Errorf("MinIO admin provisioner: %w", err)
	}
	ctrl.Log.WithName("setup").Info("minio per-tenant provisioner enabled", "endpoint", cfg.Endpoint)
	return miniop.NewCircuitBreakerProvisioner(raw, newBreaker("minio"), slog.Default()), nil
}
