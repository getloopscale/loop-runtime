// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
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

// cbStateLoggerFunc returns the shared circuit breaker state change callback
// used by all provisioner clients (MEET-052).
func cbStateLoggerFunc() func(string, cb.State, cb.State) {
	return func(name string, from, to cb.State) {
		slog.Info("circuit breaker state change",
			"breaker", name, "from", from.String(), "to", to.String())
		opmetrics.TenantCircuitBreakerState.WithLabelValues(name).Set(float64(to))
	}
}

// provisionerDeps holds all provisioners and multi-cluster clients created during setup.
type provisionerDeps struct {
	gitProvisioner    gitea.GitRepoProvisioner
	argoCDProvisioner argocd.Provisioner
	giteaURL          string
	argoCDNamespace   string
	workloadClusterURL string
	buildClient       client.Client
	runtimeClient     client.Client
	minIOProvisioner  miniop.CredentialProvisioner
	minIOEndpoint     string
	harborClient      harbor.Provisioner
	harborRegistryHost string
	registryCIDRs     []string
	doToken           string
	registryDockerConfig string
}

// setupProvisioners creates all provisioner clients and multi-cluster K8s clients.
func setupProvisioners(setupLog logr.Logger, mgr ctrl.Manager) provisionerDeps {
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

	buildClient := createClusterClient(setupLog, "BUILD_KUBECONFIG", "build")
	runtimeClient := createClusterClient(setupLog, "RUNTIME_KUBECONFIG", "runtime")

	minIOProvisioner, minIOEndpoint := setupMinIO(setupLog)
	harborClient, harborRegistryHost := setupHarbor(setupLog, cbStateLogger)

	doToken := os.Getenv("DIGITALOCEAN_TOKEN")
	registryDockerConfig := os.Getenv("REGISTRY_DOCKER_CONFIG")
	if doToken != "" && registryDockerConfig != "" {
		setupLog.Error(nil, "DIGITALOCEAN_TOKEN and REGISTRY_DOCKER_CONFIG are mutually exclusive — set only one")
		os.Exit(1)
	}

	return provisionerDeps{
		gitProvisioner:       gitProvisioner,
		argoCDProvisioner:    argoCDProvisioner,
		giteaURL:             giteaURL,
		argoCDNamespace:      argoCDNamespace,
		workloadClusterURL:   workloadClusterURL,
		buildClient:          buildClient,
		runtimeClient:        runtimeClient,
		minIOProvisioner:     minIOProvisioner,
		minIOEndpoint:        minIOEndpoint,
		harborClient:         harborClient,
		harborRegistryHost:   harborRegistryHost,
		registryCIDRs:        parseRegistryCIDRs(setupLog),
		doToken:              doToken,
		registryDockerConfig: registryDockerConfig,
	}
}

// createClusterClient reads a kubeconfig env var and creates a K8s client with retry.
func createClusterClient(setupLog logr.Logger, envVar, clusterName string) client.Client {
	kubeconfig := os.Getenv(envVar)
	if kubeconfig == "" {
		setupLog.Error(nil, envVar+" is required")
		os.Exit(1)
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		setupLog.Error(err, "unable to create "+clusterName+" cluster config", "kubeconfig", kubeconfig)
		os.Exit(1)
	}
	c, err := newClientWithRetry(setupLog, cfg, client.Options{Scheme: scheme}, clusterName)
	if err != nil {
		setupLog.Error(err, "unable to create "+clusterName+" cluster client after retries")
		os.Exit(1)
	}
	setupLog.Info(clusterName+" cluster client created", "kubeconfig", kubeconfig)
	return c
}

// setupMinIO creates the MinIO admin provisioner if all credentials are provided.
func setupMinIO(setupLog logr.Logger) (miniop.CredentialProvisioner, string) {
	minIOEndpoint := os.Getenv("MINIO_ENDPOINT")
	minIOAccessKey := os.Getenv("MINIO_ACCESS_KEY")
	minIOSecretKey := os.Getenv("MINIO_SECRET_KEY")

	if minIOEndpoint == "" || minIOAccessKey == "" || minIOSecretKey == "" {
		setupLog.Info("minio per-tenant provisioner disabled — using global credentials from SPACES_ACCESS_KEY_ID",
			"MINIO_ENDPOINT_set", minIOEndpoint != "",
			"SPACES_ACCESS_KEY_ID_set", os.Getenv("SPACES_ACCESS_KEY_ID") != "")
		return nil, minIOEndpoint
	}

	provisioner, err := miniop.NewAdminProvisioner(miniop.AdminConfig{
		Endpoint:  minIOEndpoint,
		AccessKey: minIOAccessKey,
		SecretKey: minIOSecretKey,
	})
	if err != nil {
		setupLog.Error(err, "failed to create MinIO admin provisioner")
		os.Exit(1)
	}
	setupLog.Info("minio per-tenant provisioner enabled", "endpoint", minIOEndpoint)
	return provisioner, minIOEndpoint
}

// setupHarbor creates the Harbor provisioner if HARBOR_URL is set.
func setupHarbor(setupLog logr.Logger, cbStateLogger func(string, cb.State, cb.State)) (harbor.Provisioner, string) {
	harborURL := os.Getenv("HARBOR_URL")
	if harborURL == "" {
		setupLog.Info("harbor integration disabled — HARBOR_URL not set")
		return nil, ""
	}

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
	harborClient := harbor.NewCircuitBreakerClient(harborReal, harborCB, slog.Default())

	harborRegistryHost := os.Getenv("HARBOR_REGISTRY_HOST")
	if harborRegistryHost == "" {
		harborRegistryHost = strings.TrimPrefix(strings.TrimPrefix(harborURL, "https://"), "http://")
	}
	setupLog.Info("harbor integration enabled", "url", harborURL, "registryHost", harborRegistryHost)
	return harborClient, harborRegistryHost
}

// parseRegistryCIDRs reads REGISTRY_CIDRS env var and returns parsed CIDRs.
func parseRegistryCIDRs(setupLog logr.Logger) []string {
	cidrs := os.Getenv("REGISTRY_CIDRS")
	if cidrs == "" {
		return nil
	}
	var result []string
	for _, c := range strings.Split(cidrs, ",") {
		if trimmed := strings.TrimSpace(c); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	setupLog.Info("registry CIDR restriction enabled", "cidrs", result)
	return result
}

// setupControllers wires all controllers to the manager.
func setupControllers(setupLog logr.Logger, mgr ctrl.Manager, deps provisionerDeps) {
	tenantMaxConcurrent := 0
	if v := os.Getenv("LOOPZERO_TENANT_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tenantMaxConcurrent = n
		}
	}
	if err := controller.SetupTenantController(mgr, deps.gitProvisioner, deps.argoCDProvisioner, deps.buildClient, deps.runtimeClient, controller.TenantControllerConfig{
		GiteaURL:                deps.giteaURL,
		WorkloadClusterURL:      deps.workloadClusterURL,
		MinIOProvisioner:        deps.minIOProvisioner,
		HarborClient:            deps.harborClient,
		HarborRegistryHost:      deps.harborRegistryHost,
		RegistryCIDRs:           deps.registryCIDRs,
		MaxConcurrentReconciles: tenantMaxConcurrent,
		Credentials: controller.BuildCredentials{
			MinIOEndpoint:         deps.minIOEndpoint,
			SpacesAccessKeyID:     os.Getenv("SPACES_ACCESS_KEY_ID"),
			SpacesSecretAccessKey: os.Getenv("SPACES_SECRET_ACCESS_KEY"),
			DOToken:               deps.doToken,
			RegistryDockerConfig:  deps.registryDockerConfig,
		},
	}); err != nil {
		setupLog.Error(err, "unable to setup TenantController")
		os.Exit(1)
	}

	if err := controller.SetupClusterCredentialController(mgr, deps.argoCDNamespace); err != nil {
		setupLog.Error(err, "unable to setup ClusterCredentialController")
		os.Exit(1)
	}

	if err := controller.SetupEnvironmentController(mgr, deps.runtimeClient); err != nil {
		setupLog.Error(err, "unable to setup EnvironmentController")
		os.Exit(1)
	}

	baseDomain := os.Getenv("BASE_DOMAIN")
	if baseDomain == "" {
		setupLog.Error(nil, "BASE_DOMAIN is required")
		os.Exit(1)
	}
	if err := controller.SetupApplicationController(mgr, baseDomain); err != nil {
		setupLog.Error(err, "unable to setup ApplicationController")
		os.Exit(1)
	}

	if err := controller.SetupExposureController(mgr); err != nil {
		setupLog.Error(err, "unable to setup ExposureController")
		os.Exit(1)
	}

	httpClient := httpclient.New()

	if err := controller.SetupGitRepositoryBindingController(mgr, httpClient); err != nil {
		setupLog.Error(err, "unable to setup GitRepositoryBindingController")
		os.Exit(1)
	}

	if err := controller.SetupPlatformBootstrapController(mgr, httpClient); err != nil {
		setupLog.Error(err, "unable to setup PlatformBootstrapController")
		os.Exit(1)
	}
}

// pprofEnabled checks LOOPZERO_PPROF_ENABLED. Default: false (safe for production).
// Set LOOPZERO_PPROF_ENABLED=true explicitly to enable (dev/debug only).
func pprofEnabled() bool {
	v := os.Getenv("LOOPZERO_PPROF_ENABLED")
	return v == "true" || v == "1"
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

	// pprof profiling — accessible via kubectl port-forward, never via public ingress.
	// Controlled by LOOPZERO_PPROF_ENABLED (default: false). Uses dedicated ServeMux.
	if pprofEnabled() {
		go func() {
			pprofAddr := os.Getenv("LOOPZERO_PPROF_ADDR")
			if pprofAddr == "" {
				pprofAddr = ":6061"
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/debug/pprof/", pprof.Index)
			mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
			mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
			mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
			mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
			slog.Info("pprof server starting", "addr", pprofAddr)
			if err := http.ListenAndServe(pprofAddr, mux); err != nil {
				slog.Error("pprof server failed", "error", err)
			}
		}()
	}

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

	// Create provisioners and multi-cluster clients
	deps := setupProvisioners(setupLog, mgr)

	// Wire all controllers
	setupControllers(setupLog, mgr, deps)

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
