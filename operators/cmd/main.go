// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/controller"
	"github.com/loopzero/loopzero/operators/internal/gitea"
	"github.com/loopzero/loopzero/operators/internal/httpclient"
	miniop "github.com/loopzero/loopzero/operators/internal/minio"
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
	gitProvisioner := gitea.NewRealProvisioner(giteaURL, giteaToken, "loopzero")

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
	argoCDProvisioner := argocd.NewRealProvisioner(argoCDNamespace, dynClient)

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
	buildClient, err := client.New(buildConfig, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create build cluster client")
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
	runtimeClient, err := client.New(runtimeConfig, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "unable to create runtime cluster client")
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
		minIOProvisioner = miniop.NewAdminProvisioner(miniop.AdminConfig{
			Endpoint:  minIOEndpoint,
			AccessKey: minIOAccessKey,
			SecretKey: minIOSecretKey,
		})
		setupLog.Info("minio per-tenant provisioner enabled", "endpoint", minIOEndpoint)
	} else {
		setupLog.Info("minio per-tenant provisioner disabled — using global credentials from SPACES_ACCESS_KEY_ID",
			"MINIO_ENDPOINT_set", minIOEndpoint != "",
			"SPACES_ACCESS_KEY_ID_set", os.Getenv("SPACES_ACCESS_KEY_ID") != "")
	}

	if err := controller.SetupTenantController(mgr, gitProvisioner, argoCDProvisioner, buildClient, runtimeClient, controller.TenantControllerConfig{
		GiteaURL:           giteaURL,
		WorkloadClusterURL: workloadClusterURL,
		MinIOProvisioner:   minIOProvisioner,
		Credentials: controller.BuildCredentials{
			MinIOEndpoint:        minIOEndpoint,
			SpacesAccessKeyID:    os.Getenv("SPACES_ACCESS_KEY_ID"),
			SpacesSecretAccessKey: os.Getenv("SPACES_SECRET_ACCESS_KEY"),
			DOToken:              os.Getenv("DIGITALOCEAN_TOKEN"),
			RegistryDockerConfig: os.Getenv("REGISTRY_DOCKER_CONFIG"),
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
