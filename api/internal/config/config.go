// Package config centralizes all environment-based configuration for the LoopZero API.
// Every value has a sensible default for local k3d development.
// In production (LOOPZERO_ENV=production), security-sensitive values MUST be set explicitly.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config holds all API server configuration, read from environment variables at startup.
type Config struct {
	// Server
	Port        string
	MetricsPort string
	Env         string // "development" or "production"

	// HTTP Server Timeouts
	ReadHeaderTimeout    time.Duration
	ReadTimeout          time.Duration
	IdleTimeout          time.Duration
	GracefulShutdownTime time.Duration

	// Database Pool
	DBMaxConns         int32
	DBMinConns         int32
	DBMaxConnIdleTime  time.Duration
	DBMaxConnLifetime  time.Duration
	DBHealthCheckTime  time.Duration
	DBConnectTimeout   time.Duration
	DBStatementTimeout string // milliseconds as string for PostgreSQL runtime param

	// HTTP Clients
	HTTPClientTimeout  time.Duration
	ArgocdClientTimeout time.Duration

	// Kubernetes Contexts
	MgmtKubeconfig  string
	MgmtKubeContext string
	BuildKubeconfig string
	BuildKubeContext string

	// Service URLs
	GiteaURL             string
	GiteaCrossClusterURL string
	APICrossClusterURL   string
	ArgocdURL            string
	MinIOEndpoint             string
	MinIOCrossClusterEndpoint string // Used by build cluster workflows to reach MinIO
	RegistryURL          string
	RegistryInsecure     bool   // true for local dev registries without TLS
	WorkloadIngressURL   string
	BaseDomain           string // Base domain for app hostnames (e.g., "loopzero.local", "loopzero.cloud")

	// Credentials (read from env vars or K8s secrets)
	GiteaToken         string
	ArgocdToken        string
	MinIOAccessKey     string
	MinIOSecretKey     string
	ArgoCallbackSecret string

	// Argo Workflows
	ArgoNamespace string

	// Circuit Breaker
	CBKey            string
	CBOpenThreshold  int
	CBCloseThreshold int
	CBRetryInterval  time.Duration

	// ArgoCD Sync
	ArgocdPollInterval time.Duration
	ArgocdSyncTimeout  time.Duration
	ArgocdInsecure     bool

	// SSE
	SSEHeartbeatInterval    time.Duration
	SSEPollInterval         time.Duration
	SSESafetyTimeout        time.Duration
	SSEMaxConcurrentStreams int

	// Auth
	AuthCacheTTL time.Duration

	// Source Upload
	MaxBundleSize        int64
	MultipartMaxMemory   int64

	// Build Reaper
	BuildQueueTimeout   time.Duration
	BuildRunningTimeout time.Duration
	BuildCancelTimeout  time.Duration
	BuildReaperInterval time.Duration

	// Release Reaper (spec §5.2)
	ReleaseReadyTimeout   time.Duration
	ReleasePendingTimeout time.Duration
	ReleaseReaperInterval time.Duration

	// Build Reconcile Loop (C-01 fix)
	ReconcileInterval time.Duration
}

// Load reads all configuration from environment variables with sensible defaults.
func Load() Config {
	home, _ := os.UserHomeDir()
	defaultKubeconfig := home + "/.kube/config"

	return Config{
		// Server
		Port:        envOr("PORT", "3001"),
		MetricsPort: envOr("METRICS_PORT", "9090"),
		Env:         envOr("LOOPZERO_ENV", "development"),

		// HTTP Server Timeouts
		ReadHeaderTimeout:    envDuration("SERVER_READ_HEADER_TIMEOUT", 10*time.Second),
		ReadTimeout:          envDuration("SERVER_READ_TIMEOUT", 30*time.Second),
		IdleTimeout:          envDuration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		GracefulShutdownTime: envDuration("SERVER_GRACEFUL_SHUTDOWN", 15*time.Second),

		// Database Pool
		DBMaxConns:         int32(envInt("DB_MAX_CONNS", 25)),
		DBMinConns:         int32(envInt("DB_MIN_CONNS", 5)),
		DBMaxConnIdleTime:  envDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Second),
		DBMaxConnLifetime:  envDuration("DB_MAX_CONN_LIFETIME", 1*time.Hour),
		DBHealthCheckTime:  envDuration("DB_HEALTH_CHECK_PERIOD", 30*time.Second),
		DBConnectTimeout:   envDuration("DB_CONNECT_TIMEOUT", 5*time.Second),
		DBStatementTimeout: envOr("DB_STATEMENT_TIMEOUT", "30000"),

		// HTTP Clients
		HTTPClientTimeout:   envDuration("HTTP_CLIENT_TIMEOUT", 10*time.Second),
		ArgocdClientTimeout: envDuration("ARGOCD_CLIENT_TIMEOUT", 30*time.Second),

		// Kubernetes Contexts
		MgmtKubeconfig:  envOr("MGMT_KUBECONFIG", defaultKubeconfig),
		MgmtKubeContext: envOr("MGMT_KUBE_CONTEXT", "k3d-loopzero-mgmt"),
		BuildKubeconfig: envOr("BUILD_KUBECONFIG", defaultKubeconfig),
		BuildKubeContext: envOr("BUILD_KUBE_CONTEXT", "k3d-loopzero-build"),

		// Service URLs
		GiteaURL:             envOr("GITEA_URL", "http://gitea-http.loopzero-system.svc.cluster.local:3000"),
		GiteaCrossClusterURL: envOr("GITEA_CROSS_CLUSTER_URL", ""),
		APICrossClusterURL:   envOr("API_CROSS_CLUSTER_URL", "http://loopzero-api.loopzero-system.svc.cluster.local:3001"),
		ArgocdURL:            envOr("ARGOCD_URL", "http://argocd-server.argocd.svc.cluster.local:80"),
		MinIOEndpoint:             envOr("MINIO_ENDPOINT", "minio.loopzero-system.svc.cluster.local:9000"),
		MinIOCrossClusterEndpoint: envOr("MINIO_CROSS_CLUSTER_ENDPOINT", ""),
		RegistryURL:          envOr("REGISTRY_URL", "k3d-loopzero-registry:5000"),
		RegistryInsecure:     os.Getenv("REGISTRY_INSECURE") == "true",
		WorkloadIngressURL:   os.Getenv("WORKLOAD_INGRESS_URL"),
		BaseDomain:           envOr("BASE_DOMAIN", "loopzero.local"),

		// Credentials
		GiteaToken:         os.Getenv("GITEA_TOKEN"),
		ArgocdToken:        os.Getenv("ARGOCD_TOKEN"),
		MinIOAccessKey:     envOr("MINIO_ACCESS_KEY", "loopzero-admin"),
		MinIOSecretKey:     envOr("MINIO_SECRET_KEY", "loopzero-dev-password"),
		ArgoCallbackSecret: envOr("LOOPZERO_ARGO_CALLBACK_SECRET", "loopzero-dev-callback-secret"),

		// Argo Workflows
		ArgoNamespace: envOr("ARGO_NAMESPACE", "argo"),

		// Circuit Breaker
		CBKey:            envOr("CB_KEY", "argocd"),
		CBOpenThreshold:  envInt("CB_OPEN_THRESHOLD", 3),
		CBCloseThreshold: envInt("CB_CLOSE_THRESHOLD", 2),
		CBRetryInterval:  envDuration("CB_RETRY_INTERVAL", 30*time.Second),

		// ArgoCD Sync
		ArgocdPollInterval: envDuration("ARGOCD_POLL_INTERVAL", 10*time.Second),
		ArgocdSyncTimeout:  envDuration("ARGOCD_SYNC_TIMEOUT", 120*time.Second),
		ArgocdInsecure:     os.Getenv("ARGOCD_INSECURE") == "true",

		// SSE
		SSEHeartbeatInterval:    envDuration("SSE_HEARTBEAT_INTERVAL", 15*time.Second),
		SSEPollInterval:         envDuration("SSE_POLL_INTERVAL", 1*time.Second),
		SSESafetyTimeout:        envDuration("SSE_SAFETY_TIMEOUT", 5*time.Minute),
		SSEMaxConcurrentStreams: envInt("SSE_MAX_CONCURRENT_STREAMS", 200),

		// Auth
		AuthCacheTTL: envDuration("AUTH_CACHE_TTL", 5*time.Minute),

		// Source Upload
		MaxBundleSize:      int64(envInt("SOURCE_MAX_BUNDLE_SIZE", 500*1024*1024)),
		MultipartMaxMemory: int64(envInt("SOURCE_MULTIPART_MAX_MEMORY", 32<<20)),

		// Build Reaper (spec §5.1)
		BuildQueueTimeout:   envDuration("BUILD_QUEUE_TIMEOUT", 5*time.Minute),
		BuildRunningTimeout: envDuration("BUILD_RUNNING_TIMEOUT", 30*time.Minute),
		BuildCancelTimeout:  envDuration("BUILD_CANCEL_TIMEOUT", 60*time.Second),
		BuildReaperInterval: envDuration("BUILD_REAPER_INTERVAL", 30*time.Second),

		// Release Reaper (spec §5.2)
		ReleaseReadyTimeout:   envDuration("RELEASE_READY_TIMEOUT", 5*time.Minute),
		ReleasePendingTimeout: envDuration("RELEASE_PENDING_TIMEOUT", 2*time.Minute),
		ReleaseReaperInterval: envDuration("RELEASE_REAPER_INTERVAL", 30*time.Second),

		// Build Reconcile Loop (C-01 fix): polls Argo Workflow CRDs every 5s
		ReconcileInterval: envDuration("BUILD_RECONCILE_INTERVAL", 5*time.Second),
	}
}

// Validate logs warnings about insecure defaults and returns an error for
// security violations in production that must not be ignored.
// Called after Load() to surface non-production configuration (C-03).
func (c Config) Validate() error {
	if c.ArgoCallbackSecret == "loopzero-dev-callback-secret" {
		slog.Warn("using default callback secret — only acceptable for local development")
	}
	// SEC-2: ARGOCD_INSECURE=true in production is a fatal error, not just a log.
	// TLS skip allows MITM attacks on ArgoCD API calls.
	if c.ArgocdInsecure && c.IsProduction() {
		return fmt.Errorf("ARGOCD_INSECURE=true is forbidden in production — TLS verification must be enabled")
	}
	// REVIEW-BLOCKER-2: REGISTRY_INSECURE=true in production is a fatal error.
	// Without TLS, Kaniko pushes images over HTTP — MITM can replace images,
	// rendering all supply chain security (cosign, SBOM) useless.
	if c.RegistryInsecure && c.IsProduction() {
		return fmt.Errorf("REGISTRY_INSECURE=true is forbidden in production — registry TLS verification must be enabled")
	}
	if c.MinIOAccessKey == "loopzero-admin" && c.IsProduction() {
		slog.Error("MINIO_ACCESS_KEY using default dev credentials in production — override via MINIO_ACCESS_KEY env var")
	}
	if c.MinIOSecretKey == "loopzero-dev-password" && c.IsProduction() {
		slog.Error("MINIO_SECRET_KEY using default dev credentials in production — override via MINIO_SECRET_KEY env var")
	}
	return nil
}

// GiteaCrossCluster returns the Gitea URL accessible from the build cluster.
// Falls back to the internal GiteaURL if not explicitly set.
func (c Config) GiteaCrossCluster() string {
	if c.GiteaCrossClusterURL != "" {
		return c.GiteaCrossClusterURL
	}
	return c.GiteaURL
}

// MinIOCrossCluster returns the MinIO endpoint accessible from the build cluster.
// Falls back to the internal MinIOEndpoint if not explicitly set.
func (c Config) MinIOCrossCluster() string {
	if c.MinIOCrossClusterEndpoint != "" {
		return c.MinIOCrossClusterEndpoint
	}
	return c.MinIOEndpoint
}

// IsProduction returns true when running in production mode.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

// envOr returns the environment variable value or the default.
func envOr(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// envInt returns the environment variable as int or the default.
func envInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}

// envDuration returns the environment variable as duration or the default.
// Accepts Go duration strings: "30s", "5m", "1h".
func envDuration(key string, defaultValue time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultValue
}
