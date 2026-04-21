// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"go.opentelemetry.io/otel/trace"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/controller"
	"github.com/loopzero/loopzero/operators/internal/gitea"
	"github.com/loopzero/loopzero/operators/internal/harbor"
	"github.com/loopzero/loopzero/operators/internal/httpclient"
	miniop "github.com/loopzero/loopzero/operators/internal/minio"
	"github.com/loopzero/loopzero/operators/internal/tracing"
	cb "github.com/loopzero/loopzero/pkg/circuitbreaker"
	"github.com/loopzero/loopzero/pkg/logging"

	// Register metrics package so prometheus.MustRegister runs at init.
	opmetrics "github.com/loopzero/loopzero/operators/internal/metrics"
)

var scheme = runtime.NewScheme()

// newClientWithRetry wraps client.New with exponential backoff retry.
// This handles transient connectivity issues during cluster startup ordering
// (ARCH_OP_003). Config parsing is not retried — only the client connection.
func newClientWithRetry(log logr.Logger, cfg *rest.Config, opts client.Options, clusterName string) (client.Client, error) {
	const maxAttempts = 3
	backoff := 2 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		c, err := client.New(cfg, opts)
		if err == nil {
			return c, nil
		}
		lastErr = err
		if attempt < maxAttempts {
			log.V(0).Info("retrying cluster client creation",
				"cluster", clusterName,
				"attempt", attempt,
				"nextRetryIn", backoff.String(),
				"error", err,
			)
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("failed after %d attempts: %w", maxAttempts, lastErr)
}

func init() {
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to register client-go scheme: %v", err))
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		panic(fmt.Sprintf("failed to register loopzero/v1 scheme: %v", err))
	}
}

func main() {
	var metricsAddr string
	var healthProbeAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&healthProbeAddr, "health-probe-bind-address", ":8081", "The address the health probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	opts := zap.Options{Development: false}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	tracingShutdown, tracingErr := tracing.Init(context.Background(), "loopzero-operator")
	if tracingErr != nil {
		setupLog.Error(tracingErr, "tracing init failed — continuing without tracing")
	}
	defer func() { _ = tracingShutdown(context.Background()) }()

	// Register OTel trace extractor so structured logs include trace_id/span_id.
	logging.RegisterTraceExtractor(func(ctx context.Context) (string, string) {
		sc := trace.SpanContextFromContext(ctx)
		if !sc.IsValid() {
			return "", ""
		}
		return sc.TraceID().String(), sc.SpanID().String()
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: healthProbeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "loopzero-operators.loopzero.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// Real provisioners — NO fakes.
	giteaURL := os.Getenv("GITEA_URL")
	if giteaURL == "" {
		giteaURL = constants.DefaultGiteaURL
	}
	giteaToken := os.Getenv("GITEA_TOKEN")
	if giteaToken == "" {
		setupLog.Error(nil, "GITEA_TOKEN is required")
		os.Exit(1)
	}
	// MEET-052: circuit breaker state change logger + metrics for provisioner clients.
	cbStateLogger := func(name string, from, to cb.State) {
		slog.Info("circuit breaker state change",
			"breaker", name, "from", from.String(), "to", to.String())
		opmetrics.TenantCircuitBreakerState.WithLabelValues(name).Set(float64(to))
	}

	gitProvisionerRaw := gitea.NewRealProvisioner(giteaURL, giteaToken, "loopzero")
	giteaCB := cb.New(cb.Config{
		Name: "gitea", OpenThreshold: 5, CloseThreshold: 1,
		RetryInterval: 120 * time.Second,
	}, cbStateLogger)
	gitProvisioner := gitea.NewCircuitBreakerProvisioner(gitProvisionerRaw, giteaCB, slog.Default())

	argoCDNamespace := os.Getenv("ARGOCD_NAMESPACE")
	if argoCDNamespace == "" {
		argoCDNamespace = constants.DefaultArgoCDNamespace
	}
	// ARCH-003: Create dynamic client from manager config and inject into ArgoCD provisioner
	// (rather than letting the provisioner create its own independent client).
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

	workloadClusterURL := os.Getenv("WORKLOAD_CLUSTER_URL")
	if workloadClusterURL == "" {
		workloadClusterURL = constants.DefaultWorkloadClusterURL
	}

	// Multi-cluster clients: the controller runs in mgmt but creates
	// resources on the build and runtime clusters.
	buildKubeconfig := os.Getenv("BUILD_KUBECONFIG")
	if buildKubeconfig == "" {
		setupLog.Error(nil, "BUILD_KUBECONFIG is required")
		os.Exit(1)
	}
	buildConfig, err := clientcmd.BuildConfigFromFlags("", buildKubeconfig)
	if err != nil {
		setupLog.Error(err, "unable to create build cluster config", "kubeconfig", buildKubeconfig)
		os.Exit(1)
	}
	buildClient, err := newClientWithRetry(setupLog, buildConfig, client.Options{Scheme: scheme}, "build")
	if err != nil {
		setupLog.Error(err, "unable to create build cluster client after retries")
		os.Exit(1)
	}
	setupLog.Info("build cluster client created", "kubeconfig", buildKubeconfig)

	runtimeKubeconfig := os.Getenv("RUNTIME_KUBECONFIG")
	if runtimeKubeconfig == "" {
		setupLog.Error(nil, "RUNTIME_KUBECONFIG is required")
		os.Exit(1)
	}
	runtimeConfig, err := clientcmd.BuildConfigFromFlags("", runtimeKubeconfig)
	if err != nil {
		setupLog.Error(err, "unable to create runtime cluster config", "kubeconfig", runtimeKubeconfig)
		os.Exit(1)
	}
	runtimeClient, err := newClientWithRetry(setupLog, runtimeConfig, client.Options{Scheme: scheme}, "runtime")
	if err != nil {
		setupLog.Error(err, "unable to create runtime cluster client after retries")
		os.Exit(1)
	}
	setupLog.Info("runtime cluster client created", "kubeconfig", runtimeKubeconfig)

	// MinIO per-tenant credential provisioner (optional — nil disables per-tenant isolation).
	// When enabled, each tenant gets its own MinIO user with a policy scoped to
	// loopzero-builds/{tenantID}/* only, preventing cross-tenant source/cache access (spec §17.2).
	var minIOProvisioner miniop.CredentialProvisioner
	minIOEndpoint := os.Getenv("MINIO_ENDPOINT")
	minIOAccessKey := os.Getenv("MINIO_ACCESS_KEY")
	minIOSecretKey := os.Getenv("MINIO_SECRET_KEY")

	// MinIO admin provisioner: creates per-tenant users and policies.
	// Only enabled when all 3 MinIO admin credentials are provided.
	// Disabled for DigitalOcean Spaces — Spaces uses SPACES_ACCESS_KEY_ID/SECRET_ACCESS_KEY
	// (no admin API). The controller falls back to global credentials from BuildCredentials.
	if minIOEndpoint != "" && minIOAccessKey != "" && minIOSecretKey != "" {
		var provErr error
		minIOProvisioner, provErr = miniop.NewAdminProvisioner(miniop.AdminConfig{
			Endpoint:  minIOEndpoint,
			AccessKey: minIOAccessKey,
			SecretKey: minIOSecretKey,
		})
		if provErr != nil {
			setupLog.Error(provErr, "failed to create MinIO admin provisioner")
			os.Exit(1)
		}
		setupLog.Info("minio per-tenant provisioner enabled", "endpoint", minIOEndpoint)
	} else {
		setupLog.Info("minio per-tenant provisioner disabled — using global credentials from SPACES_ACCESS_KEY_ID",
			"MINIO_ENDPOINT_set", minIOEndpoint != "",
			"SPACES_ACCESS_KEY_ID_set", os.Getenv("SPACES_ACCESS_KEY_ID") != "")
	}

	// RISK-007: Restrict build namespace port-443 egress to known registry CIDRs.
	// Set REGISTRY_CIDRS to comma-separated CIDRs (e.g., "104.16.0.0/12,185.199.108.0/22").
	// When empty, port-443 is unrestricted (local dev fallback).
	var registryCIDRs []string
	if cidrs := os.Getenv("REGISTRY_CIDRS"); cidrs != "" {
		for _, c := range strings.Split(cidrs, ",") {
			if trimmed := strings.TrimSpace(c); trimmed != "" {
				registryCIDRs = append(registryCIDRs, trimmed)
			}
		}
		setupLog.Info("registry CIDR restriction enabled", "cidrs", registryCIDRs)
	}

	// ARCH-OP-005: Validate DOToken and RegistryDockerConfig are mutually exclusive at startup.
	// Both configure registry credentials — DOToken calls the DO API, RegistryDockerConfig is static.
	// If both are set, the behavior is ambiguous (DOToken silently wins). Fail fast instead.
	doToken := os.Getenv("DIGITALOCEAN_TOKEN")
	registryDockerConfig := os.Getenv("REGISTRY_DOCKER_CONFIG")
	if doToken != "" && registryDockerConfig != "" {
		setupLog.Error(nil, "DIGITALOCEAN_TOKEN and REGISTRY_DOCKER_CONFIG are mutually exclusive — set only one")
		os.Exit(1)
	}

	// Harbor registry integration (optional — nil disables, backward compatible with DOCR).
	// Only enabled when HARBOR_URL is set. All 3 env vars are required when enabled.
	var harborClient harbor.Provisioner
	var harborRegistryHost string
	if harborURL := os.Getenv("HARBOR_URL"); harborURL != "" {
		harborUser := os.Getenv("HARBOR_ADMIN_USERNAME")
		harborPass := os.Getenv("HARBOR_ADMIN_PASSWORD")
		if harborUser == "" || harborPass == "" {
			setupLog.Error(nil, "HARBOR_ADMIN_USERNAME and HARBOR_ADMIN_PASSWORD are required when HARBOR_URL is set")
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
			// Derive from HARBOR_URL by stripping the scheme.
			harborRegistryHost = strings.TrimPrefix(strings.TrimPrefix(harborURL, "https://"), "http://")
		}
		setupLog.Info("harbor integration enabled", "url", harborURL, "registryHost", harborRegistryHost)
	} else {
		setupLog.Info("harbor integration disabled — HARBOR_URL not set")
	}

	tenantMaxConcurrent := 0 // defaults to 5 in SetupTenantController (MEET-052)
	if v := os.Getenv("LOOPZERO_TENANT_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tenantMaxConcurrent = n
		}
	}
	if err := controller.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner, buildClient, runtimeClient, controller.TenantControllerConfig{
		GiteaURL:                giteaURL,
		WorkloadClusterURL:      workloadClusterURL,
		MinIOProvisioner:        minIOProvisioner,
		HarborClient:            harborClient,
		HarborRegistryHost:      harborRegistryHost,
		RegistryCIDRs:           registryCIDRs,
		MaxConcurrentReconciles: tenantMaxConcurrent,
		Credentials: controller.BuildCredentials{
			MinIOEndpoint:        minIOEndpoint,
			SpacesAccessKeyID:    os.Getenv("SPACES_ACCESS_KEY_ID"),
			SpacesSecretAccessKey: os.Getenv("SPACES_SECRET_ACCESS_KEY"),
			DOToken:              doToken,
			RegistryDockerConfig: registryDockerConfig,
		},
	}); err != nil {
		setupLog.Error(err, "unable to setup TenantController")
		os.Exit(1)
	}

	if err := controller.SetupClusterCredentialController(mgr, argoCDNamespace); err != nil {
		setupLog.Error(err, "unable to setup ClusterCredentialController")
		os.Exit(1)
	}

	// --- Sprint 1: Wire remaining controllers (order matters) ---
	// Dependency chain: Tenant → Environment → Application → ExposedRoute (Exposure)
	// GitRepositoryBinding and PlatformBootstrap are independent but need httpClient.

	// 1. EnvironmentController: reconciles Environment CRs created by TenantController.
	// Creates namespaces, RBAC, NetworkPolicies, and ResourceQuotas on the runtime cluster.
	if err := controller.SetupEnvironmentController(mgr, runtimeClient); err != nil {
		setupLog.Error(err, "unable to setup EnvironmentController")
		os.Exit(1)
	}

	// 2. ApplicationController: reconciles LoopZeroApplication CRs.
	// Assigns hostnames, creates ExposedRoute CRs, tracks pod health.
	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		setupLog.Error(nil, "BASE_DOMAIN is required")
		os.Exit(1)
	}
	if err := controller.SetupApplicationController(mgr, baseDomain); err != nil {
		setupLog.Error(err, "unable to setup ApplicationController")
		os.Exit(1)
	}

	// 3. ExposureController: reconciles ExposedRoute CRs created by ApplicationController.
	// Checks TLS certificates and performs reachability probes.
	if err := controller.SetupExposureController(mgr); err != nil {
		setupLog.Error(err, "unable to setup ExposureController")
		os.Exit(1)
	}

	// ARCH-005: Shared HTTP client for controllers that make internal HTTP calls.
	// Centralized in internal/httpclient — single place to harden TLS when
	// internal services get proper cert-manager issued certificates.
	httpClient := httpclient.New()

	// 4. GitRepositoryBindingController: syncs Git credentials to ArgoCD Secrets.
	// Detects credential rotation via SHA256 hash comparison.
	if err := controller.SetupGitRepositoryBindingController(mgr, httpClient); err != nil {
		setupLog.Error(err, "unable to setup GitRepositoryBindingController")
		os.Exit(1)
	}

	// 5. PlatformBootstrapController: declarative bootstrap (Gitea → ArgoCD → API seed).
	// Replaces imperative shell scripts in CI. Last because most external dependencies.
	if err := controller.SetupPlatformBootstrapController(mgr, httpClient); err != nil {
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

	setupLog.Info("starting manager", "leaderElection", enableLeaderElection)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
