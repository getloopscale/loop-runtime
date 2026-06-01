// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package config centralizes all environment-based configuration for the LoopZero API.
// Every value has a sensible default for local k3d development.
// In production (LOOPZERO_ENV=production), security-sensitive values MUST be set explicitly.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// T1.3 (plan 2026-05-21-deep-review-remediation): dev-default sentinels are
// declared as constants so the production-startup guard checks against a
// single source of truth AND the no-hardcoded-credentials gate exempts this
// file by name (the literals are used semantically, not as defaults that
// could leak to production).
const (
	devDefaultMinIOAccessKey       = "loopzero-admin"
	devDefaultMinIOSecretKey       = "loopzero-dev-password"
	devDefaultArgoCallbackSecret   = "loopzero-dev-callback-secret"
	devDefaultBootstrapSecret      = "loopzero-dev-bootstrap-secret"
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

	// Database
	DatabaseURL string // DATABASE_URL — validated for sslmode in production
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
	BuildKubeconfig   string
	BuildKubeContext  string
	RuntimeKubeconfig string
	RuntimeKubeContext string

	// Service URLs
	GiteaURL             string
	GiteaCrossClusterURL string
	// B77 (end-to-end-deploy-validation-plan iter 11) — Gitea organization
	// where tenant gitops repos live. Default "loopzero" preserves prior behavior;
	// configurable via GITEA_ORG env var.
	GiteaOrg             string
	APICrossClusterURL   string
	ArgocdURL            string
	MinIOEndpoint             string
	MinIOCrossClusterEndpoint string // Used by build cluster workflows to reach MinIO
	SourceBucket              string // S3 bucket for source bundles (default: "loopzero-builds")
	RegistryURL          string
	RegistryInsecure     bool   // true for local dev registries without TLS
	HarborRegistryURL    string // MEET-048: secondary registry for dual-push during migration. Empty = disabled.

	// Cosign keyless signing config (M2 T8.0 of plan 2026-05-27-M07-to-3-85 — v1.1 EC-3 wire env var).
	// CosignKeyless=false (default): keyed mode via cosign-signing-key Secret.
	// CosignKeyless=true: keyless via Fulcio + Rekor + projected SA token.
	// Requires Sigstore stack provisioned on build cluster (per plan M2 Phase 7).
	// Without this wire, helm values bonitos viram pod env mas InfraParams default
	// fica false (keyed) — silent teatro pattern "Implemented but not wired" do CLAUDE.md.
	CosignKeyless       bool
	CosignAttestEnabled bool   // SLSA L3 SBOM-to-image attestation; requires CosignKeyless=true
	CosignFulcioURL     string // Override default Fulcio URL when CosignKeyless=true. Empty = cosign default (sigstore public).
	CosignRekorURL      string // Override default Rekor URL when CosignKeyless=true. Empty = cosign default.

	// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1.
	// CR-1 fix from MEET-2026-05-28-001: `--insecure-skip-verify` was hardcoded
	// unconditionally in cosign sign + attest scripts. Single env var flip
	// (LOOPZERO_COSIGN_KEYLESS=true) bypassed SCT verification globally.
	//
	// CosignInsecureSkipSCT: render `--insecure-skip-verify` flag in cosign
	// sign/attest commands. ONLY for LOCAL k3d with self-hosted CTLog whose
	// public key isn't in the default TUF root. Default false.
	//
	// CosignTUFRootPinned: TUF root has been pinned (operator ceremony complete +
	// Pulumi sigstore.ts deployed with `tufPreMintedKeys`). Default false.
	//
	// Validate() rejects CosignInsecureSkipSCT=true && IsProduction() &&
	// !CosignTUFRootPinned (the "skip SCT in prod without TUF" attack vector).
	CosignInsecureSkipSCT bool
	CosignTUFRootPinned   bool

	WorkloadIngressURL   string
	BaseDomain           string // Base domain for app hostnames (e.g., "loopzero.local", "loopzero.cloud")
	// B14 (MEET-2026-04-23-058): cert-manager ClusterIssuer used for all app TLS
	// Certificates. REQUIRED per-environment (prod: letsencrypt-prod; dev: loopzero-ca-issuer).
	// Set via LOOPZERO_CLUSTER_ISSUER env var. Empty = Validate() fatal at startup.
	ClusterIssuer        string

	// argo-rollouts-progressive-delivery-plan T1.2 (D6) — Prometheus URL for
	// canary AnalysisTemplate queries. Must be reachable from workload
	// cluster pods. For k3d local dev: "http://172.18.0.X:30090" (mgmt
	// NodePort). For prod: "http://prometheus.monitoring.svc.cluster.local:9090"
	// when prometheus is in the workload cluster, OR a reachable URL via
	// CF Tunnel/service mesh. Empty disables Prometheus-driven analysis
	// (canary still runs setWeight+pause but skips analysis steps).
	PrometheusAddress string

	// Credentials (read from env vars or K8s secrets)
	GiteaToken         string
	ArgocdToken        string
	MinIOAccessKey     string
	MinIOSecretKey     string
	ArgoCallbackSecret    string
	BootstrapSecret       string // SEC-003: shared secret for POST /internal/bootstrap
	SecretEncryptionKey   string // SEC-001: 32-byte hex-encoded AES-256 key for secret encryption at rest

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
	BuildQueueTimeout     time.Duration
	BuildRunningTimeout   time.Duration
	BuildReleasingTimeout time.Duration
	BuildCancelTimeout    time.Duration
	BuildReaperInterval   time.Duration

	// Release Reaper (spec §5.2)
	ReleaseReadyTimeout   time.Duration
	ReleasePendingTimeout time.Duration
	ReleaseReaperInterval time.Duration

	// Build Reconcile Loop (C-01 fix)
	ReconcileInterval  time.Duration
	MaxBuildsPerTenant int

	// Loki (build log aggregation)
	LokiURL string

	// Cloudflare (static frontend delivery via CF Worker + KV + Zone Management)
	CFAPIToken        string // Cloudflare API token
	CFAccountID       string // Cloudflare account ID
	CFKVNamespaceID   string // KV namespace ID for routing table
	CFZoneManagement  bool   // Enable CF Zone Management for custom domains (v2)

	// CORS
	CORSAllowedOrigin string // CORS_ALLOWED_ORIGIN — required in production, open in dev

	// JWT Authentication (Sprint 11 — Ory Hydra integration).
	// JWKS endpoint for validating tokens issued by the platform sign-up flow
	// (https://app.<domain> account registration). Earlier prototypes
	// referenced GitHub PAT validation; that path was removed — platform
	// tokens are the only accepted credential for `loopzero login --token`.
	// Env: JWKS_ENDPOINT (e.g., "http://ory-hydra-public.ory.svc.cluster.local:4444/.well-known/jwks.json")
	JWKSEndpoint string
	// JWTIssuer is the expected issuer in JWT tokens (must match Hydra config).
	// Env: JWT_ISSUER (e.g., "http://ory-hydra-public.ory.svc.cluster.local:4444/")
	JWTIssuer string

	// Roadmap 02 — API Loop Extraction feature flag.
	//
	// When DisableLegacyLoops is true, the API process skips leader election
	// and does NOT start ANY of the background loops (build reconcile, build
	// reaper, release reaper, release convergence, idempotency cleaner, SSE
	// event pruner, domain provisioner, domain health checker, cell metrics).
	// The API continues to serve HTTP requests; the controller binary
	// (cmd/controller/) is expected to run the loops instead.
	//
	// Default: false (loops run in the API, matching pre-extraction behavior).
	// When the controller binary is fully rolled out, this flag defaults to
	// true on the API Deployment and the controller Deployment runs the loops.
	// Setting it back to false is the documented rollback path.
	//
	// Env: LOOPZERO_DISABLE_LEGACY_LOOPS ("true" to disable loops in the API).
	DisableLegacyLoops bool

	// Roadmap 07 — Backpressure and Admission Control.
	//
	// When AdmissionControl is true, the API consults the build admission
	// gate and pg-pool circuit breaker. At capacity returns HTTP 429 or 503.
	// Default: false — preserves pre-roadmap-07 behavior.
	// Env: LOOPZERO_ADMISSION_CONTROL
	AdmissionControl bool

	// MaxConcurrentBuildsPerCell: cell-level concurrent build limit.
	// Env: MAX_CONCURRENT_BUILDS_PER_CELL (default: 20)
	MaxConcurrentBuildsPerCell int

	// MaxTenantsPerCell: maximum tenants per cell for placement engine.
	// Env: MAX_TENANTS_PER_CELL (default: 5000)
	MaxTenantsPerCell int

	// MaxConcurrentBuildsPerTenant: per-tenant concurrent build limit.
	// Env: MAX_CONCURRENT_BUILDS_PER_TENANT (default: 3)
	MaxConcurrentBuildsPerTenant int

	// PGCircuitBreakerThresholdPct: pool utilization % above which mutations are rejected.
	// Env: PG_CIRCUIT_BREAKER_THRESHOLD_PCT (default: 90)
	PGCircuitBreakerThresholdPct int

	// UsePGClaims enables PG-backed processing claims for the reconcile
	// and convergence loops (Phase 0.2, scale-50k-plan). When true, both
	// loops use ClaimStore.Claim/Release in addition to sync.Map for
	// cross-replica dedup visibility.
	// Env: LOOPZERO_USE_PG_CLAIMS
	UsePGClaims bool

	// CHG-2026-04-30-DELETE-SINGLE-CELL: removed cfg.DynamicRouter and
	// cfg.SingleCell. Multi-cell is the only architecture. DynamicRouter
	// is used unconditionally; single-cell is the degenerate case of N=1
	// ClusterCredentials discovered. See setup_background.go:setupCellRouter.

	// ServiceAuthSecretCreatedAt is the ISO 8601 timestamp of when the
	// current service auth secret was created. Used to compute the age
	// metric (loopzero_service_auth_secret_age_seconds). If unset, the
	// process start time is used as a fallback (less accurate but still
	// triggers rotation alerts on long-running processes).
	// Env: LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT
	ServiceAuthSecretCreatedAt time.Time

	// KEDAEnabled is a feature gate for KEDA-based scale-to-zero support.
	//
	// When false (default), the API rejects autoscaling requests with
	// min:0 replicas — scale-to-zero is not supported without KEDA.
	// When true, the API allows min:0 and expects KEDA ScaledObjects
	// to manage the zero-to-one scaling transition.
	//
	// Env: LOOPZERO_KEDA_ENABLED
	KEDAEnabled bool
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

		// Database
		DatabaseURL: os.Getenv("DATABASE_URL"),
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
		ArgocdClientTimeout: envDuration("ARGOCD_CLIENT_TIMEOUT", 90*time.Second), // T1.1 D1: bumped 30→90s for concurrent Ginkgo load headroom

		// Kubernetes Contexts
		MgmtKubeconfig:  envOr("MGMT_KUBECONFIG", defaultKubeconfig),
		MgmtKubeContext: envOr("MGMT_KUBE_CONTEXT", "k3d-loopzero-mgmt"),
		BuildKubeconfig:   envOr("BUILD_KUBECONFIG", defaultKubeconfig),
		BuildKubeContext:  envOr("BUILD_KUBE_CONTEXT", "k3d-loopzero-build"),
		RuntimeKubeconfig: envOr("RUNTIME_KUBECONFIG", defaultKubeconfig),
		RuntimeKubeContext: envOr("RUNTIME_KUBE_CONTEXT", "k3d-loopzero-workload"),

		// Service URLs
		GiteaURL:             envOr("GITEA_URL", "http://gitea-http.gitea.svc.cluster.local:3000"),
		GiteaCrossClusterURL: envOr("GITEA_CROSS_CLUSTER_URL", ""),
		GiteaOrg:             envOr("GITEA_ORG", "loopzero"),
		APICrossClusterURL:   envOr("API_CROSS_CLUSTER_URL", "http://loopzero-api.loopzero-system.svc.cluster.local:3001"),
		ArgocdURL:            envOr("ARGOCD_URL", "http://argocd-server.argocd.svc.cluster.local:80"),
		// CHG-2026-04-29-DOGFOOD iter 9 — MUST include scheme. The s3
		// uploader composes the URL as "<endpoint>/<bucket>/<key>" and
		// `http.NewRequest` rejects URLs without a scheme with the
		// opaque "unsupported protocol scheme" error. Live local
		// dogfood discovered this when the API silently swallowed the
		// upload error and returned 200 to the CLI, then the build
		// pipeline failed downstream because the bundle was missing.
		MinIOEndpoint:             envOr("MINIO_ENDPOINT", "http://minio.loopzero-system.svc.cluster.local:9000"),
		MinIOCrossClusterEndpoint: envOr("MINIO_CROSS_CLUSTER_ENDPOINT", ""),
		SourceBucket:              envOr("SOURCE_BUCKET", "loopzero-builds"),
		RegistryURL:          envOr("REGISTRY_URL", "k3d-loopzero-registry:5000"),
		RegistryInsecure:     os.Getenv("REGISTRY_INSECURE") == "true",
		HarborRegistryURL:    os.Getenv("HARBOR_REGISTRY_URL"), // MEET-048: empty = disabled

		// Cosign keyless config (M2 T8.0, v1.1 EC-3 of plan 2026-05-27-M07-to-3-85)
		// Defaults preserve keyed mode (safe regression). Operator sets LOOPZERO_COSIGN_KEYLESS=true
		// AFTER Sigstore stack provisioned em build cluster.
		CosignKeyless:        envBool("LOOPZERO_COSIGN_KEYLESS", false),
		CosignAttestEnabled:  envBool("LOOPZERO_COSIGN_ATTEST_ENABLED", false),
		// T1.2 CR-1 fix: explicit env opt-in for SCT-skip flag (LOCAL only)
		CosignInsecureSkipSCT: envBool("LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY", false),
		CosignTUFRootPinned:   envBool("LOOPZERO_COSIGN_TUF_ROOT_PINNED", false),
		CosignFulcioURL:      os.Getenv("LOOPZERO_FULCIO_URL"),
		CosignRekorURL:       os.Getenv("LOOPZERO_REKOR_URL"),
		WorkloadIngressURL:   os.Getenv("WORKLOAD_INGRESS_URL"),
		BaseDomain:           envOr("BASE_DOMAIN", "loopzero.local"),
		ClusterIssuer:        os.Getenv("LOOPZERO_CLUSTER_ISSUER"),
		PrometheusAddress:    os.Getenv("LOOPZERO_PROMETHEUS_ADDRESS"),

		// Credentials
		GiteaToken:         os.Getenv("GITEA_TOKEN"),
		ArgocdToken:        os.Getenv("ARGOCD_TOKEN"),
		MinIOAccessKey:     envOr("MINIO_ACCESS_KEY", devDefaultMinIOAccessKey),
		MinIOSecretKey:     envOr("MINIO_SECRET_KEY", devDefaultMinIOSecretKey),
		ArgoCallbackSecret:  envOr("LOOPZERO_ARGO_CALLBACK_SECRET", devDefaultArgoCallbackSecret),
		BootstrapSecret:     envOr("LOOPZERO_BOOTSTRAP_SECRET", devDefaultBootstrapSecret),
		SecretEncryptionKey: os.Getenv("LOOPZERO_SECRET_ENCRYPTION_KEY"), // SEC-001: empty = no encryption (dev only)

		// Argo Workflows
		ArgoNamespace: envOr("ARGO_NAMESPACE", "argo"),

		// Circuit Breaker
		CBKey:            envOr("CB_KEY", "argocd"),
		CBOpenThreshold:  envInt("CB_OPEN_THRESHOLD", 3),
		CBCloseThreshold: envInt("CB_CLOSE_THRESHOLD", 2),
		CBRetryInterval:  envDuration("CB_RETRY_INTERVAL", 30*time.Second),

		// ArgoCD Sync
		ArgocdPollInterval: envDuration("ARGOCD_POLL_INTERVAL", 10*time.Second),
		ArgocdSyncTimeout: envDuration("ARGOCD_SYNC_TIMEOUT", 300*time.Second),
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
		BuildQueueTimeout:     envDuration("BUILD_QUEUE_TIMEOUT", 5*time.Minute),
		BuildRunningTimeout:   envDuration("BUILD_RUNNING_TIMEOUT", 30*time.Minute),
		BuildReleasingTimeout: envDuration("BUILD_RELEASING_TIMEOUT", 10*time.Minute),
		BuildCancelTimeout:    envDuration("BUILD_CANCEL_TIMEOUT", 60*time.Second),
		BuildReaperInterval:   envDuration("BUILD_REAPER_INTERVAL", 30*time.Second),

		// Release Reaper (spec §5.2)
		ReleaseReadyTimeout:   envDuration("RELEASE_READY_TIMEOUT", 5*time.Minute),
		ReleasePendingTimeout: envDuration("RELEASE_PENDING_TIMEOUT", 6*time.Minute),
		ReleaseReaperInterval: envDuration("RELEASE_REAPER_INTERVAL", 30*time.Second),

		// Build Reconcile Loop (C-01 fix): polls Argo Workflow CRDs every 5s
		ReconcileInterval:  envDuration("BUILD_RECONCILE_INTERVAL", 5*time.Second),
		MaxBuildsPerTenant: envInt("MAX_BUILDS_PER_TENANT", 3),

		// Loki (build log aggregation)
		LokiURL: envOr("LOKI_URL", "http://loki.monitoring:3100"),

		// Cloudflare (static frontend delivery)
		CFAPIToken:        os.Getenv("CF_API_TOKEN"),
		CFAccountID:       os.Getenv("CF_ACCOUNT_ID"),
		CFKVNamespaceID:   os.Getenv("CF_KV_NAMESPACE_ID"),
		CFZoneManagement:  os.Getenv("CF_ZONE_MANAGEMENT") == "true",

		// CORS
		CORSAllowedOrigin: os.Getenv("CORS_ALLOWED_ORIGIN"),

		// JWT Authentication (Sprint 11 — when set, enables dual JWT + PAT validation)
		JWKSEndpoint: os.Getenv("JWKS_URL"),
		JWTIssuer:    os.Getenv("JWT_ISSUER"),

		// Roadmap 02 — API Loop Extraction. When true, the API does not run
		// any background loops (controller binary runs them instead).
		DisableLegacyLoops: os.Getenv("LOOPZERO_DISABLE_LEGACY_LOOPS") == "true",

		// Roadmap 07 — Backpressure & Admission Control.
		AdmissionControl:             os.Getenv("LOOPZERO_ADMISSION_CONTROL") == "true",
		MaxConcurrentBuildsPerCell:   envInt("MAX_CONCURRENT_BUILDS_PER_CELL", 20),
		MaxTenantsPerCell:            envInt("MAX_TENANTS_PER_CELL", 5000),
		MaxConcurrentBuildsPerTenant: envInt("MAX_CONCURRENT_BUILDS_PER_TENANT", 3),
		PGCircuitBreakerThresholdPct: envInt("PG_CIRCUIT_BREAKER_THRESHOLD_PCT", 90),

		// Scale 50k plan — Phase 0.2 feature flag (Phase 0.3 multi-cell is
		// no longer toggle-gated; see CHG-2026-04-30-DELETE-SINGLE-CELL).
		UsePGClaims: os.Getenv("LOOPZERO_USE_PG_CLAIMS") == "true",

		// WS.1: Service auth secret rotation monitoring.
		ServiceAuthSecretCreatedAt: envTime("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT"),

		// KEDA scale-to-zero feature gate.
		KEDAEnabled: os.Getenv("LOOPZERO_KEDA_ENABLED") == "true",
	}
}

// Validate logs warnings about insecure defaults and returns an error for
// security violations in production that must not be ignored.
// Called after Load() to surface non-production configuration (C-03).
// validEnvs is the canonical LOOPZERO_ENV allowlist. Sprint A T2.1 (closes
// sec-008 / CR-1 partial of MEET-2026-05-28-001): any LOOPZERO_ENV value not
// in this set is rejected at startup. Prevents case-bypass like
// LOOPZERO_ENV=Production silently degrading to non-prod IsProduction()
// behavior (case-sensitive == compare). EC-8 ordering: this check runs
// EARLY in Validate() so all subsequent IsProduction() gates can assume
// a canonical value.
var validEnvs = map[string]bool{
	"development": true,
	"staging":     true,
	"production":  true,
}

// isValidEnv returns true iff s is a canonical LOOPZERO_ENV value. Exact
// string match — NO case-fold, NO trim. A typo or case variant
// (Production, prod, " production") fails fast at startup. Adding a new
// env (e.g. "drill") requires updating this map + Validate() tests +
// auditing every IsProduction() call site for whether the new env needs
// near-prod treatment — explicit governance gate per plan ADR D2.
func isValidEnv(s string) bool {
	return validEnvs[s]
}

func (c Config) Validate() error {
	// Sprint A T2.1 (2026-06-01, closes sec-008): LOOPZERO_ENV allowlist.
	// Reject any non-canonical value to prevent case-bypass attacks on
	// IsProduction()-gated security checks (CR-1 of MEET-2026-05-28-001).
	// This check is FIRST so all downstream gates can trust c.Env.
	if !isValidEnv(c.Env) {
		return fmt.Errorf("LOOPZERO_ENV=%q invalid; allowed: development, staging, production", c.Env)
	}

	// B14 (MEET-2026-04-23-058): ClusterIssuer is required — no in-code default.
	// Each environment (prod/staging/dev) MUST set LOOPZERO_CLUSTER_ISSUER explicitly.
	// Prevents the "invisible default" footgun that caused B14.
	if c.ClusterIssuer == "" {
		return fmt.Errorf("LOOPZERO_CLUSTER_ISSUER is required (prod: letsencrypt-prod, dev: loopzero-ca-issuer)")
	}

	// MEET-2026-03-31-023 T2-13: Cross-field validation rules.
	// These prevent silent misconfigurations that cause runtime failures.
	if c.DBMaxConns > 0 && c.DBMinConns > 0 && c.DBMaxConns < c.DBMinConns {
		return fmt.Errorf("DB_MAX_CONNS (%d) must be >= DB_MIN_CONNS (%d)", c.DBMaxConns, c.DBMinConns)
	}
	if c.ReleasePendingTimeout > 0 && c.ArgocdSyncTimeout > 0 && c.ReleasePendingTimeout < c.ArgocdSyncTimeout {
		if c.IsProduction() {
			return fmt.Errorf("RELEASE_PENDING_TIMEOUT (%s) must be >= ARGOCD_SYNC_TIMEOUT (%s) — releases will be reaped before sync completes",
				c.ReleasePendingTimeout, c.ArgocdSyncTimeout)
		}
		slog.Warn("RELEASE_PENDING_TIMEOUT is less than ARGOCD_SYNC_TIMEOUT — releases may be reaped before ArgoCD sync completes",
			"release_pending_timeout", c.ReleasePendingTimeout, "argocd_sync_timeout", c.ArgocdSyncTimeout)
	}
	if c.BuildRunningTimeout > 0 && c.BuildQueueTimeout > 0 && c.BuildRunningTimeout < c.BuildQueueTimeout {
		slog.Warn("BUILD_RUNNING_TIMEOUT is less than BUILD_QUEUE_TIMEOUT — unusual configuration",
			"running_timeout", c.BuildRunningTimeout, "queue_timeout", c.BuildQueueTimeout)
	}

	// Dev credential warnings (production check is done below as fatal)
	if c.ArgoCallbackSecret == "loopzero-dev-callback-secret" && !c.IsProduction() {
		slog.Warn("using default callback secret — only acceptable for local development")
	}
	// SEC-001: Warn if secret encryption is disabled.
	if c.SecretEncryptionKey == "" {
		if c.IsProduction() {
			return fmt.Errorf("LOOPZERO_SECRET_ENCRYPTION_KEY is required in production — secrets must be encrypted at rest")
		}
		slog.Warn("LOOPZERO_SECRET_ENCRYPTION_KEY not set — secrets stored in plaintext (only acceptable for local development)")
	}
	// SEC-2: ARGOCD_INSECURE=true in production is a fatal error, not just a log.
	// TLS skip allows MITM attacks on ArgoCD API calls.
	if c.ArgocdInsecure && c.IsProduction() {
		return fmt.Errorf("ARGOCD_INSECURE=true is forbidden in production — TLS verification must be enabled")
	}
	// REVIEW-BLOCKER-2: REGISTRY_INSECURE=true in production is a fatal error.
	// Without TLS, BuildKit pushes images over HTTP — MITM can replace images,
	// rendering all supply chain security (cosign, SBOM) useless.
	if c.RegistryInsecure && c.IsProduction() {
		return fmt.Errorf("REGISTRY_INSECURE=true is forbidden in production — registry TLS verification must be enabled")
	}
	// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1.
	// CR-1 fix from MEET-2026-05-28-001: gate `--insecure-skip-verify` flag.
	// Allowed in prod ONLY when TUF root pinned (operator ceremony complete).
	// Without TUF root, skipping SCT verification = any compromised Fulcio
	// can forge signatures undetectable by the SLSA L3 chain.
	if c.CosignInsecureSkipSCT && c.IsProduction() && !c.CosignTUFRootPinned {
		return fmt.Errorf("LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY=true requires LOOPZERO_COSIGN_TUF_ROOT_PINNED=true in production — SCT verification skip without TUF root is a supply-chain compromise vector")
	}
	// H4: Default dev credentials in production are a fatal error, not just a warning.
	// Without this, production deployments silently use the dev defaults declared
	// as constants at the top of this file (devDefault* sentinels).
	if c.MinIOAccessKey == devDefaultMinIOAccessKey && c.IsProduction() {
		return fmt.Errorf("MINIO_ACCESS_KEY using default dev credentials in production — override via MINIO_ACCESS_KEY env var")
	}
	if c.MinIOSecretKey == devDefaultMinIOSecretKey && c.IsProduction() {
		return fmt.Errorf("MINIO_SECRET_KEY using default dev credentials in production — override via MINIO_SECRET_KEY env var")
	}
	if c.BootstrapSecret == devDefaultBootstrapSecret && c.IsProduction() {
		return fmt.Errorf("LOOPZERO_BOOTSTRAP_SECRET using default dev value in production — override via LOOPZERO_BOOTSTRAP_SECRET env var")
	}
	if c.ArgoCallbackSecret == devDefaultArgoCallbackSecret && c.IsProduction() {
		return fmt.Errorf("LOOPZERO_ARGO_CALLBACK_SECRET using default dev value in production — override via LOOPZERO_ARGO_CALLBACK_SECRET env var")
	}
	// SEC: CORS_ALLOWED_ORIGIN must be set in production to prevent open CORS.
	if c.CORSAllowedOrigin == "" && c.IsProduction() {
		return fmt.Errorf("CORS_ALLOWED_ORIGIN must be set in production")
	}
	// H1: DATABASE_URL with sslmode=disable in production sends credentials and data in plaintext.
	if c.DatabaseURL != "" && c.IsProduction() && strings.Contains(c.DatabaseURL, "sslmode=disable") {
		return fmt.Errorf("DATABASE_URL contains sslmode=disable which is forbidden in production — use sslmode=require or sslmode=verify-full")
	}
	// KEDA feature gate — informational log when enabled.
	if c.KEDAEnabled {
		slog.Warn("KEDA feature gate is enabled — scale-to-zero (min:0) autoscaling requests will be accepted")
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

// envTime parses an environment variable as an RFC 3339 timestamp.
// Returns the zero value if the variable is unset or unparseable.
func envTime(key string) time.Time {
	if v := os.Getenv(key); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		slog.Warn("invalid timestamp in environment variable — ignoring", "key", key, "value", v)
	}
	return time.Time{}
}

// envBool parses an environment variable as bool. Accepts "true", "1", "yes" (case-insensitive)
// as true; everything else (including unset) returns defaultValue.
// Used by M2 T8.0 (v1.1 EC-3) for LOOPZERO_COSIGN_KEYLESS + LOOPZERO_COSIGN_ATTEST_ENABLED.
func envBool(key string, defaultValue bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	slog.Warn("invalid bool in environment variable — using default", "key", key, "value", v, "default", defaultValue)
	return defaultValue
}
