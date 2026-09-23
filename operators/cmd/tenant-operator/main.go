// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Group A: Tenant + Environment controllers.
// Heaviest operator — owns all external provisioners (Gitea, ArgoCD, Harbor, MinIO)
// and both cross-cluster clients (build + runtime).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/argocd"
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/environment"
	"github.com/getloopscale/loopzero/operators/internal/gitea"
	"github.com/getloopscale/loopzero/operators/internal/harbor"
	miniop "github.com/getloopscale/loopzero/operators/internal/minio"
	"github.com/getloopscale/loopzero/operators/internal/operatorboot"
	"github.com/getloopscale/loopzero/operators/internal/tenant"
	"github.com/getloopscale/loopzero/operators/internal/tracing"
	cb "github.com/getloopscale/loopzero/pkg/circuitbreaker"
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

func cbStateLoggerFunc() func(string, cb.State, cb.State) {
	return func(name string, from, to cb.State) {
		slog.Info("circuit breaker state change",
			"breaker", name, "from", from.String(), "to", to.String())
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

	// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 condition C16 +
	// operator-split-invariants.md I-1. Cache scoping per ADR-0018 v2 C9.
	// Tenant + Environment cache restricted to `loopzero-system`; reading
	// these CRs from any other namespace returns empty list (so a CR
	// applied to `default` is invisible to this controller, fail-safe
	// for the dogfood iter 7-8 misplacement vector).
	platformCRDNamespaces := map[string]cache.Config{
		constants.NamespacePlatformSystem: {},
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: healthProbeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "loopzero-tenant-operator.loopzero.io",
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&loopzerov1.Tenant{}:      {Namespaces: platformCRDNamespaces},
				&loopzerov1.Environment{}: {Namespaces: platformCRDNamespaces},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// --- Provisioners ---
	cbStateLogger := cbStateLoggerFunc()

	giteaURL := os.Getenv("GITEA_URL")
	if giteaURL == "" {
		giteaURL = constants.DefaultGiteaURL
	}
	giteaToken := os.Getenv("GITEA_TOKEN")
	if giteaToken == "" {
		setupLog.Error(nil, "GITEA_TOKEN is required")
		os.Exit(1)
	}
	// B-069: the organisation is configuration, not a literal. It was a
	// hardcoded "loopzero" here, while the API side reads GITEA_ORG -- so any
	// deployment that set the org got repos created in one organisation and
	// pushed to another. Same envOr shape as GITEA_URL above.
	giteaOrg := os.Getenv("GITEA_ORG")
	if giteaOrg == "" {
		giteaOrg = constants.DefaultGiteaOrg
	}
	gitProvisionerRaw := gitea.NewRealProvisioner(giteaURL, giteaToken, giteaOrg)
	giteaCB := cb.New(cb.Config{
		Name: "gitea", OpenThreshold: 5, CloseThreshold: 1,
		RetryInterval: 120 * time.Second,
	}, cbStateLogger)
	gitProvisioner := gitea.NewCircuitBreakerProvisioner(gitProvisionerRaw, giteaCB, slog.Default())

	argoCDNamespace := os.Getenv("ARGOCD_NAMESPACE")
	if argoCDNamespace == "" {
		argoCDNamespace = constants.DefaultArgoCDNamespace
	}
	dynClient, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable to create dynamic client for ArgoCD provisioner")
		os.Exit(1)
	}
	argoCDProvisionerRaw := argocd.NewRealProvisioner(argoCDNamespace, dynClient)
	argoCDCB := cb.New(cb.Config{
		Name: "argocd", OpenThreshold: 5, CloseThreshold: 1,
		RetryInterval: 120 * time.Second,
	}, cbStateLogger)
	argoCDProvisioner := argocd.NewCircuitBreakerProvisioner(argoCDProvisionerRaw, argoCDCB, slog.Default())

	// Multi-cluster clients
	buildKubeconfig := os.Getenv("BUILD_KUBECONFIG")
	if buildKubeconfig == "" {
		setupLog.Error(nil, "BUILD_KUBECONFIG is required")
		os.Exit(1)
	}
	buildClient, err := cellrouting.CreateClusterClient(setupLog, buildKubeconfig, scheme, "build")
	if err != nil {
		setupLog.Error(err, "failed to create build cluster client")
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
	// The runtime cluster of each tenant's cell; cellID "" (never placed)
	// resolves to the CELL_0_KUBECONFIG cluster.
	runtimeFactory := cellrouting.NewCellAwareRuntimeClientFactory(
		mgr.GetClient(), runtimeClient, constants.NamespacePlatformSystem)

	// MinIO (optional)
	var minIOProvisioner miniop.CredentialProvisioner
	minIOEndpoint := os.Getenv("MINIO_ENDPOINT")
	minIOAccessKey := os.Getenv("MINIO_ACCESS_KEY")
	minIOSecretKey := os.Getenv("MINIO_SECRET_KEY")
	if minIOEndpoint != "" && minIOAccessKey != "" && minIOSecretKey != "" {
		rawProvisioner, err := miniop.NewAdminProvisioner(miniop.AdminConfig{
			Endpoint:  minIOEndpoint,
			AccessKey: minIOAccessKey,
			SecretKey: minIOSecretKey,
		})
		if err != nil {
			setupLog.Error(err, "failed to create MinIO admin provisioner")
			os.Exit(1)
		}
		minIOCB := cb.New(cb.Config{
			Name: "minio", OpenThreshold: 5, CloseThreshold: 1,
			RetryInterval: 120 * time.Second,
		}, cbStateLogger)
		minIOProvisioner = miniop.NewCircuitBreakerProvisioner(rawProvisioner, minIOCB, slog.Default())
		setupLog.Info("minio per-tenant provisioner enabled", "endpoint", minIOEndpoint)
	}

	// Harbor (optional)
	var harborClient harbor.Provisioner
	var harborRegistryHost string
	harborURL := os.Getenv("HARBOR_URL")
	if harborURL != "" {
		harborUser := os.Getenv("HARBOR_ADMIN_USERNAME")
		harborPass := os.Getenv("HARBOR_ADMIN_PASSWORD")
		if harborUser == "" || harborPass == "" {
			setupLog.Error(nil, "HARBOR_ADMIN_USERNAME and HARBOR_ADMIN_PASSWORD required when HARBOR_URL is set")
			os.Exit(1)
		}
		harborReal := harbor.NewClient(harborURL, harborUser, harborPass)
		harborCB := cb.New(cb.Config{
			Name: "harbor", OpenThreshold: 5, CloseThreshold: 1,
			RetryInterval: 120 * time.Second,
		}, cbStateLogger)
		harborClient = harbor.NewCircuitBreakerClient(harborReal, harborCB, slog.Default())
		harborRegistryHost = os.Getenv("HARBOR_REGISTRY_HOST")
		if harborRegistryHost == "" {
			harborRegistryHost = strings.TrimPrefix(strings.TrimPrefix(harborURL, "https://"), "http://")
		}
		setupLog.Info("harbor integration enabled", "url", harborURL)
	}

	// Registry CIDRs
	var registryCIDRs []string
	if cidrs := os.Getenv("REGISTRY_CIDRS"); cidrs != "" {
		for _, c := range strings.Split(cidrs, ",") {
			if trimmed := strings.TrimSpace(c); trimmed != "" {
				registryCIDRs = append(registryCIDRs, trimmed)
			}
		}
	}

	doToken := os.Getenv("DIGITALOCEAN_TOKEN")
	registryDockerConfig := os.Getenv("REGISTRY_DOCKER_CONFIG")
	if doToken != "" && registryDockerConfig != "" {
		setupLog.Error(nil, "DIGITALOCEAN_TOKEN and REGISTRY_DOCKER_CONFIG are mutually exclusive")
		os.Exit(1)
	}

	// --- Controllers ---
	tenantMaxConcurrent := 0
	if v := os.Getenv("LOOPZERO_TENANT_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tenantMaxConcurrent = n
		}
	}
	if err := tenant.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner, buildClient, runtimeFactory, tenant.TenantControllerConfig{
		GiteaURL:                giteaURL,
		WorkloadClusterURL:      os.Getenv("WORKLOAD_CLUSTER_URL"),
		MinIOProvisioner:        minIOProvisioner,
		HarborClient:            harborClient,
		HarborRegistryHost:      harborRegistryHost,
		RegistryCIDRs:           registryCIDRs,
		MaxConcurrentReconciles: tenantMaxConcurrent,
		Credentials: tenant.BuildCredentials{
			MinIOEndpoint:         minIOEndpoint,
			SpacesAccessKeyID:     os.Getenv("SPACES_ACCESS_KEY_ID"),
			SpacesSecretAccessKey: os.Getenv("SPACES_SECRET_ACCESS_KEY"),
			DOToken:               doToken,
			RegistryDockerConfig:  registryDockerConfig,
		},
	}); err != nil {
		setupLog.Error(err, "unable to setup TenantController")
		os.Exit(1)
	}

	// Both controllers write to the runtime cluster of the tenant's cell, so
	// they share one factory (and its per-cell client cache).
	if err := environment.SetupEnvironmentControllerWithFactory(mgr, runtimeFactory); err != nil {
		setupLog.Error(err, "unable to setup EnvironmentController")
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
