// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package config centralizes all environment-based configuration for the LoopZero API.
// Every value has a sensible default for local k3d development.
// In production (LOOPZERO_ENV=production), security-sensitive values MUST be set explicitly.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// T1.3 (plan 2026-05-21-deep-review-remediation): dev-default sentinels are
// declared as constants so both users of each literal — the fallback in Load()
// and the production guard in validateProductionCredentials — read the same
// value. A literal repeated in the two places is a literal that can diverge, and
// the half that diverges is the guard: it would stop recognising the default it
// exists to refuse.
//
// These ARE credential values, not names, so gosec G101 is not a false positive.
// What makes them safe is not their content: it is that
// validateProductionCredentials REFUSES TO START when LOOPZERO_ENV is production and
// any of them survived. Dev is the only environment where they are reachable.
//
// (A previous version of this comment also claimed a "no-hardcoded-credentials
// gate exempts this file by name". No such gate exists — grep over
// cmd/loopzero-ops/internal/quality/ finds neither this path nor `devDefault`. The
// claim was removed rather than left to be trusted.)
const (
	devDefaultMinIOAccessKey = "loopzero-admin"
	//nolint:gosec // G101: dev default, refused in production by validateProductionCredentials
	devDefaultMinIOSecretKey = "loopzero-dev-password"
	//nolint:gosec // G101: dev default, refused in production by validateProductionCredentials
	devDefaultArgoCallbackSecret = "loopzero-dev-callback-secret"
	//nolint:gosec // G101: dev default, refused in production by validateProductionCredentials
	devDefaultBootstrapSecret = "loopzero-dev-bootstrap-secret"
)

// Config holds all API server configuration, read from environment variables at startup.
type Config struct {
	// loadErrors are the variables Load could not parse; Validate reports them.
	loadErrors []error

	// Server
	Port        string
	MetricsPort string
	Env         string // "development" or "production"

	// TracingEnabled turns OpenTelemetry export on (LOOPZERO_TRACING_ENABLED,
	// default true); OTelEndpoint is the collector (OTEL_EXPORTER_OTLP_ENDPOINT,
	// empty = the tracing package default).
	TracingEnabled bool
	OTelEndpoint   string

	// GitHubAPIBaseURL is the GitHub REST root (GITHUB_API_BASE_URL); a GitHub
	// Enterprise Server install points it at its appliance.
	GitHubAPIBaseURL string

	// EnvBlocklistMode is how services.json env keys on the reserved blocklist
	// are handled (LOOPZERO_ENV_BLOCKLIST_MODE): "enforce" (default) or "warn".
	// Warn is refused in production.
	EnvBlocklistMode string

	// HTTP Server Timeouts
	ReadHeaderTimeout    time.Duration
	ReadTimeout          time.Duration
	IdleTimeout          time.Duration
	GracefulShutdownTime time.Duration

	// Database
	DatabaseURL string // DATABASE_URL — validated for sslmode in production

	// WorkerDatabaseURL — T3.1 of plan 2026-06-01-p3-infra-observability-
	// test-debt v1.1 ADR D3. Connection string for background loops
	// (reconcile, convergence, reapers, idempotency cleaner). When set,
	// background loops use this URL with the `loopzero_worker` non-owner
	// role so RLS policies apply (the `loopzero` owner bypasses RLS by
	// design — see migration 000023 header). When EMPTY, background
	// loops fall back to DatabaseURL (legacy behavior; RLS bypass via
	// owner — works but loses defense-in-depth). Validation requires
	// the URL to be non-empty in IsProduction() once migration 000023
	// has shipped (operator-controlled rollout via env var).
	WorkerDatabaseURL string // LOOPZERO_WORKER_DATABASE_URL
	// Database Pool
	DBMaxConns         int32
	DBMinConns         int32
	DBMaxConnIdleTime  time.Duration
	DBMaxConnLifetime  time.Duration
	DBHealthCheckTime  time.Duration
	DBConnectTimeout   time.Duration
	DBStatementTimeout string // milliseconds as string for PostgreSQL runtime param

	// HTTP Clients
	HTTPClientTimeout   time.Duration
	ArgocdClientTimeout time.Duration

	// Kubernetes Contexts
	MgmtKubeconfig     string
	MgmtKubeContext    string
	BuildKubeconfig    string
	BuildKubeContext   string
	RuntimeKubeconfig  string
	RuntimeKubeContext string

	// Service URLs
	GiteaURL             string
	GiteaCrossClusterURL string
	// B77 (end-to-end-deploy-validation-plan iter 11) — Gitea organization
	// where tenant gitops repos live. Default "loopzero" preserves prior behavior;
	// configurable via GITEA_ORG env var.
	GiteaOrg                  string
	APICrossClusterURL        string
	ArgocdURL                 string
	MinIOEndpoint             string
	MinIOCrossClusterEndpoint string // Used by build cluster workflows to reach MinIO
	SourceBucket              string // S3 bucket for source bundles (default: "loopzero-builds")
	RegistryURL               string
	RegistryInsecure          bool   // true for local dev registries without TLS
	HarborRegistryURL         string // MEET-048: secondary registry for dual-push during migration. Empty = disabled.

	// Cosign keyless signing config (M2 T8.0 of plan 2026-05-27-M07-to-3-85 — v1.1 EC-3 wire env var).
	// CosignKeyless=false (default): keyed mode via cosign-signing-key Secret.
	// CosignKeyless=true: keyless via Fulcio + Rekor + projected SA token.
	// Requires Sigstore stack provisioned on build cluster (per plan M2 Phase 7).
	// Without this wire, the helm values become pod env but InfraParams stays at its
	// default false (keyed) — the silent "Implemented but not wired" pattern from CLAUDE.md.
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

	WorkloadIngressURL string
	BaseDomain         string // Base domain for app hostnames (e.g., "loopzero.local", "loopzero.cloud")
	// PreviewPublicDomain is the wildcard domain used to compose public
	// preview URLs surfaced to users. When empty, falls back to BaseDomain
	// (legacy behavior). Set to the Caddy/CF wildcard root in environments
	// where previews are exposed via an edge proxy distinct from the
	// internal IngressRoute SNI domain (e.g., dev-public:
	// `preview-dev.loopzero.example` while BaseDomain stays `loopzero.local`).
	//
	// URL convention: <tenant>-<project>-<entryApp>-preview-pr-N.<PreviewPublicDomain>
	// where <entryApp> is the first server/frontend app in loopzero.yaml — this
	// matches the IngressRoute Host emitted by appHostname() so the Caddy
	// wildcard block reverse-proxies cleanly to Traefik.
	PreviewPublicDomain string
	// PublicDomain is the wildcard domain used to compose public app URLs
	// surfaced to users for PERMANENT environments (production, staging,
	// etc. — non-preview). When empty, status.url emit is suppressed and
	// the dashboard renders "URL pending" badge (per ADR D3 of plan
	// 2026-06-12-dashboard-journey-j25-live-url-discoverability).
	// Set to the Caddy/CF wildcard root in environments where apps are
	// exposed via an edge proxy distinct from the internal IngressRoute
	// SNI domain (e.g., dev-public: `prod-dev.loopzero.example` while
	// BaseDomain stays `loopzero.local`).
	//
	// URL convention: <tenant>-<project>-<entryApp>-<env>.<PublicDomain>
	// where <entryApp> is the first server/frontend app in loopzero.yaml — this
	// matches the IngressRoute Host emitted by appHostname() (single source
	// of truth: pkg/naming.AppHostname) so the Caddy wildcard block
	// reverse-proxies cleanly to Traefik. Mirror of PreviewPublicDomain
	// for permanent envs (FAANG canonical J-25 sponsor walkthrough catch
	// 2026-06-12 — closes catches #21+#22).
	PublicDomain string
	// GrafanaBaseURL is the deep-link root for Grafana dashboards surfaced from
	// the J-18 Status / Runtime Health page. Templated by StatusHandler into
	// statusResponse.MetricsRangeURL as
	// `<GrafanaBaseURL>/d/loopzero-project?var-project=<name>&var-env=<env>`.
	// Empty disables the affordance — dashboard renders "Open in Grafana" as
	// disabled (honest, per ADR D2 of plan 2026-06-06-dashboard-journey-j18-
	// status-runtime-health v1.1).
	// Set via LOOPZERO_GRAFANA_BASE_URL env var.
	GrafanaBaseURL string
	// B14 (MEET-2026-04-23-058): cert-manager ClusterIssuer used for all app TLS
	// Certificates. REQUIRED per-environment (prod: letsencrypt-prod; dev: loopzero-ca-issuer).
	// Set via LOOPZERO_CLUSTER_ISSUER env var. Empty = Validate() fatal at startup.
	ClusterIssuer string

	// argo-rollouts-progressive-delivery-plan T1.2 (D6) — Prometheus URL for
	// canary AnalysisTemplate queries. Must be reachable from workload
	// cluster pods. For k3d local dev: "http://172.18.0.X:30090" (mgmt
	// NodePort). For prod: "http://prometheus.monitoring.svc.cluster.local:9090"
	// when prometheus is in the workload cluster, OR a reachable URL via
	// CF Tunnel/service mesh. Empty disables Prometheus-driven analysis
	// (canary still runs setWeight+pause but skips analysis steps).
	PrometheusAddress string

	// Credentials (read from env vars or K8s secrets)
	GiteaToken          string
	ArgocdToken         string
	MinIOAccessKey      string
	MinIOSecretKey      string
	ArgoCallbackSecret  string
	BootstrapSecret     string // SEC-003: shared secret for POST /internal/bootstrap
	SecretEncryptionKey string // SEC-001: 32-byte hex-encoded AES-256 key for secret encryption at rest

	// T2.5 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 —
	// OpenBao transit encryption credentials. When OpenBaoTransitEnabled is
	// true, ALL secret writes are encrypted via OpenBao transit BEFORE
	// reaching PG (the SecretEncryptionKey AES path is ignored; an EC-05
	// boot log records `crypto_mode=transit aes_key_ignored=true`).
	OpenBaoTransitEnabled bool   // OPENBAO_TRANSIT_ENABLED — opt-in
	OpenBaoVaultAddr      string // OPENBAO_VAULT_ADDR (e.g. https://openbao.secrets-system.svc:8200)
	OpenBaoVaultRoleID    string // OPENBAO_VAULT_ROLE_ID — AppRole role_id
	OpenBaoVaultSecretID  string // OPENBAO_VAULT_SECRET_ID — AppRole secret_id
	OpenBaoSPKIPinSHA256  string // OPENBAO_SPKI_PIN_SHA256 — hex-encoded SHA-256 of leaf cert SPKI (optional)

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
	MaxBundleSize      int64
	MultipartMaxMemory int64

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
	CFAPIToken       string // Cloudflare API token
	CFAccountID      string // Cloudflare account ID
	CFKVNamespaceID  string // KV namespace ID for routing table
	CFZoneManagement bool   // Enable CF Zone Management for custom domains (v2)

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

	// API process switches, read here once rather than ad hoc in main.
	//
	// PprofEnabled starts the pprof server (LOOPZERO_PPROF_ENABLED "true" or "1";
	// default off, dev/debug only) on PprofAddr (LOOPZERO_PPROF_ADDR, default
	// ":6060"), reachable by port-forward and never by the public ingress.
	PprofEnabled bool
	PprofAddr    string
	// EnvReadinessChecker makes EnsureEnvironment poll the Environment CR
	// until status.phase is Active (LOOPZERO_ENV_READINESS_CHECKER "true").
	EnvReadinessChecker bool
	// BrokerToken is the bearer the ESO secret-fetch broker requires
	// (LOOPZERO_BROKER_TOKEN); empty disables the broker endpoint.
	BrokerToken string
	// TrustedProxyCIDRs lists the proxies whose X-Forwarded-For the pre-auth
	// IP rate limit honours (LOOPZERO_TRUSTED_PROXY_CIDRS, comma-separated).
	TrustedProxyCIDRs string
	// BillingUpgradeURL is the upgrade link a quota refusal points to
	// (BILLING_UPGRADE_URL).
	BillingUpgradeURL string

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

	l := &envLoader{}
	cfg := Config{
		// Server
		Port:        envOr("PORT", "3001"),
		MetricsPort: envOr("METRICS_PORT", "9090"),
		Env:         envOr("LOOPZERO_ENV", "development"),

		TracingEnabled:   envBool("LOOPZERO_TRACING_ENABLED", true),
		OTelEndpoint:     os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		GitHubAPIBaseURL: envOr("GITHUB_API_BASE_URL", "https://api.github.com"),
		EnvBlocklistMode: strings.ToLower(strings.TrimSpace(envOr("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce"))),

		// HTTP Server Timeouts
		ReadHeaderTimeout:    l.duration("SERVER_READ_HEADER_TIMEOUT", 10*time.Second),
		ReadTimeout:          l.duration("SERVER_READ_TIMEOUT", 30*time.Second),
		IdleTimeout:          l.duration("SERVER_IDLE_TIMEOUT", 120*time.Second),
		GracefulShutdownTime: l.duration("SERVER_GRACEFUL_SHUTDOWN", 15*time.Second),

		// Database
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		WorkerDatabaseURL: os.Getenv("LOOPZERO_WORKER_DATABASE_URL"),
		// Database Pool
		DBMaxConns:         int32(l.int("DB_MAX_CONNS", 25)),
		DBMinConns:         int32(l.int("DB_MIN_CONNS", 5)),
		DBMaxConnIdleTime:  l.duration("DB_MAX_CONN_IDLE_TIME", 30*time.Second),
		DBMaxConnLifetime:  l.duration("DB_MAX_CONN_LIFETIME", 1*time.Hour),
		DBHealthCheckTime:  l.duration("DB_HEALTH_CHECK_PERIOD", 30*time.Second),
		DBConnectTimeout:   l.duration("DB_CONNECT_TIMEOUT", 5*time.Second),
		DBStatementTimeout: envOr("DB_STATEMENT_TIMEOUT", "30000"),

		// HTTP Clients
		HTTPClientTimeout:   l.duration("HTTP_CLIENT_TIMEOUT", 10*time.Second),
		ArgocdClientTimeout: l.duration("ARGOCD_CLIENT_TIMEOUT", 90*time.Second), // T1.1 D1: bumped 30→90s for concurrent Ginkgo load headroom

		// Kubernetes Contexts
		MgmtKubeconfig:     envOr("CORE_KUBECONFIG", defaultKubeconfig),
		MgmtKubeContext:    envOr("CORE_KUBE_CONTEXT", "k3d-loopzero-core"),
		BuildKubeconfig:    envOr("BUILD_KUBECONFIG", defaultKubeconfig),
		BuildKubeContext:   envOr("BUILD_KUBE_CONTEXT", "k3d-loopzero-build"),
		RuntimeKubeconfig:  envOr("CELL_0_KUBECONFIG", defaultKubeconfig),
		RuntimeKubeContext: envOr("CELL_0_KUBE_CONTEXT", "k3d-loopzero-cell-0"),

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
		RegistryURL:               envOr("REGISTRY_URL", "k3d-loopzero-registry:5000"),
		RegistryInsecure:          os.Getenv("REGISTRY_INSECURE") == "true",
		HarborRegistryURL:         os.Getenv("HARBOR_REGISTRY_URL"), // MEET-048: empty = disabled

		// Cosign keyless config (M2 T8.0, v1.1 EC-3 of plan 2026-05-27-M07-to-3-85)
		// Defaults preserve keyed mode (safe regression). Operator sets LOOPZERO_COSIGN_KEYLESS=true
		// AFTER Sigstore stack provisioned em build cluster.
		CosignKeyless:       envBool("LOOPZERO_COSIGN_KEYLESS", false),
		CosignAttestEnabled: envBool("LOOPZERO_COSIGN_ATTEST_ENABLED", false),
		// T1.2 CR-1 fix: explicit env opt-in for SCT-skip flag (LOCAL only)
		CosignInsecureSkipSCT: envBool("LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY", false),
		CosignTUFRootPinned:   envBool("LOOPZERO_COSIGN_TUF_ROOT_PINNED", false),
		CosignFulcioURL:       os.Getenv("LOOPZERO_FULCIO_URL"),
		CosignRekorURL:        os.Getenv("LOOPZERO_REKOR_URL"),
		WorkloadIngressURL:    os.Getenv("WORKLOAD_INGRESS_URL"),
		BaseDomain:            envOr("BASE_DOMAIN", "loopzero.local"),
		PreviewPublicDomain:   os.Getenv("LOOPZERO_PREVIEW_PUBLIC_DOMAIN"),
		PublicDomain:          os.Getenv("LOOPZERO_PUBLIC_DOMAIN"),
		GrafanaBaseURL:        os.Getenv("LOOPZERO_GRAFANA_BASE_URL"),
		ClusterIssuer:         os.Getenv("LOOPZERO_CLUSTER_ISSUER"),
		PrometheusAddress:     os.Getenv("LOOPZERO_PROMETHEUS_ADDRESS"),

		// Credentials
		GiteaToken:          os.Getenv("GITEA_TOKEN"),
		ArgocdToken:         os.Getenv("ARGOCD_TOKEN"),
		MinIOAccessKey:      envOr("MINIO_ACCESS_KEY", devDefaultMinIOAccessKey),
		MinIOSecretKey:      envOr("MINIO_SECRET_KEY", devDefaultMinIOSecretKey),
		ArgoCallbackSecret:  envOr("LOOPZERO_ARGO_CALLBACK_SECRET", devDefaultArgoCallbackSecret),
		BootstrapSecret:     envOr("LOOPZERO_BOOTSTRAP_SECRET", devDefaultBootstrapSecret),
		SecretEncryptionKey: os.Getenv("LOOPZERO_SECRET_ENCRYPTION_KEY"), // SEC-001: empty = no encryption (dev only)

		// T2.5 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 —
		// OpenBao transit credentials (per D6 ADR fail-loud refusal).
		OpenBaoTransitEnabled: envOr("OPENBAO_TRANSIT_ENABLED", "false") == "true",
		OpenBaoVaultAddr:      os.Getenv("OPENBAO_VAULT_ADDR"),
		OpenBaoVaultRoleID:    os.Getenv("OPENBAO_VAULT_ROLE_ID"),
		OpenBaoVaultSecretID:  os.Getenv("OPENBAO_VAULT_SECRET_ID"),
		OpenBaoSPKIPinSHA256:  os.Getenv("OPENBAO_SPKI_PIN_SHA256"),

		// Argo Workflows
		ArgoNamespace: envOr("ARGO_NAMESPACE", "argo"),

		// Circuit Breaker
		CBKey:            envOr("CB_KEY", "argocd"),
		CBOpenThreshold:  l.int("CB_OPEN_THRESHOLD", 3),
		CBCloseThreshold: l.int("CB_CLOSE_THRESHOLD", 2),
		CBRetryInterval:  l.duration("CB_RETRY_INTERVAL", 30*time.Second),

		// ArgoCD Sync
		ArgocdPollInterval: l.duration("ARGOCD_POLL_INTERVAL", 10*time.Second),
		ArgocdSyncTimeout:  l.duration("ARGOCD_SYNC_TIMEOUT", 300*time.Second),
		ArgocdInsecure:     os.Getenv("ARGOCD_INSECURE") == "true",

		// SSE
		SSEHeartbeatInterval:    l.duration("SSE_HEARTBEAT_INTERVAL", 15*time.Second),
		SSEPollInterval:         l.duration("SSE_POLL_INTERVAL", 1*time.Second),
		SSESafetyTimeout:        l.duration("SSE_SAFETY_TIMEOUT", 5*time.Minute),
		SSEMaxConcurrentStreams: l.int("SSE_MAX_CONCURRENT_STREAMS", 200),

		// Auth
		AuthCacheTTL: l.duration("AUTH_CACHE_TTL", 5*time.Minute),

		// Source Upload
		MaxBundleSize:      int64(l.int("SOURCE_MAX_BUNDLE_SIZE", 500*1024*1024)),
		MultipartMaxMemory: int64(l.int("SOURCE_MULTIPART_MAX_MEMORY", 32<<20)),

		// Build Reaper (spec §5.1)
		BuildQueueTimeout:     l.duration("BUILD_QUEUE_TIMEOUT", 5*time.Minute),
		BuildRunningTimeout:   l.duration("BUILD_RUNNING_TIMEOUT", 30*time.Minute),
		BuildReleasingTimeout: l.duration("BUILD_RELEASING_TIMEOUT", 10*time.Minute),
		BuildCancelTimeout:    l.duration("BUILD_CANCEL_TIMEOUT", 60*time.Second),
		BuildReaperInterval:   l.duration("BUILD_REAPER_INTERVAL", 30*time.Second),

		// Release Reaper (spec §5.2)
		ReleaseReadyTimeout:   l.duration("RELEASE_READY_TIMEOUT", 5*time.Minute),
		ReleasePendingTimeout: l.duration("RELEASE_PENDING_TIMEOUT", 6*time.Minute),
		ReleaseReaperInterval: l.duration("RELEASE_REAPER_INTERVAL", 30*time.Second),

		// Build Reconcile Loop (C-01 fix): polls Argo Workflow CRDs every 5s
		ReconcileInterval:  l.duration("BUILD_RECONCILE_INTERVAL", 5*time.Second),
		MaxBuildsPerTenant: l.int("MAX_BUILDS_PER_TENANT", 3),

		// Loki (build log aggregation)
		LokiURL: envOr("LOKI_URL", "http://loki.monitoring:3100"),

		// Cloudflare (static frontend delivery)
		CFAPIToken:       os.Getenv("CF_API_TOKEN"),
		CFAccountID:      os.Getenv("CF_ACCOUNT_ID"),
		CFKVNamespaceID:  os.Getenv("CF_KV_NAMESPACE_ID"),
		CFZoneManagement: os.Getenv("CF_ZONE_MANAGEMENT") == "true",

		// CORS
		CORSAllowedOrigin: os.Getenv("CORS_ALLOWED_ORIGIN"),

		// JWT Authentication (Sprint 11 — when set, enables dual JWT + PAT validation)
		JWKSEndpoint: os.Getenv("JWKS_URL"),
		JWTIssuer:    os.Getenv("JWT_ISSUER"),

		// Roadmap 02 — API Loop Extraction. When true, the API does not run
		// any background loops (controller binary runs them instead).
		DisableLegacyLoops: os.Getenv("LOOPZERO_DISABLE_LEGACY_LOOPS") == "true",

		PprofEnabled:        pprofEnabled(os.Getenv("LOOPZERO_PPROF_ENABLED")),
		PprofAddr:           envOr("LOOPZERO_PPROF_ADDR", ":6060"),
		EnvReadinessChecker: os.Getenv("LOOPZERO_ENV_READINESS_CHECKER") == "true",
		BrokerToken:         os.Getenv("LOOPZERO_BROKER_TOKEN"),
		TrustedProxyCIDRs:   os.Getenv("LOOPZERO_TRUSTED_PROXY_CIDRS"),
		BillingUpgradeURL:   os.Getenv("BILLING_UPGRADE_URL"),

		// Roadmap 07 — Backpressure & Admission Control.
		AdmissionControl:             os.Getenv("LOOPZERO_ADMISSION_CONTROL") == "true",
		MaxConcurrentBuildsPerCell:   l.int("MAX_CONCURRENT_BUILDS_PER_CELL", 20),
		MaxTenantsPerCell:            l.int("MAX_TENANTS_PER_CELL", 5000),
		MaxConcurrentBuildsPerTenant: l.int("MAX_CONCURRENT_BUILDS_PER_TENANT", 3),
		PGCircuitBreakerThresholdPct: l.int("PG_CIRCUIT_BREAKER_THRESHOLD_PCT", 90),

		// Scale 50k plan — Phase 0.2 feature flag (Phase 0.3 multi-cell is
		// no longer toggle-gated; see CHG-2026-04-30-DELETE-SINGLE-CELL).
		UsePGClaims: os.Getenv("LOOPZERO_USE_PG_CLAIMS") == "true",

		// WS.1: Service auth secret rotation monitoring.
		ServiceAuthSecretCreatedAt: envTime("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT"),

		// KEDA scale-to-zero feature gate.
		KEDAEnabled: os.Getenv("LOOPZERO_KEDA_ENABLED") == "true",
	}
	cfg.loadErrors = l.errs
	return cfg
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

// Validate runs the full configuration validation chain. T4.1 of plan
// 2026-06-11-code-review-remediation v1.2 + ADR D6 — decomposed from the
// pre-refactor CC=46 monolith into named sub-validators per concern.
// Each helper is independently testable and stays well under CC=10.
//
// Order matters: earlier checks establish invariants (e.g. c.Env is
// valid) that later checks assume. Mirrors the pre-refactor order
// verbatim so behavior is byte-identical for the existing test suite.
func (c Config) Validate() error {
	if len(c.loadErrors) > 0 {
		return fmt.Errorf("config: unparseable environment variables: %w", errors.Join(c.loadErrors...))
	}
	if err := c.validateEnv(); err != nil {
		return err
	}
	if err := c.validateEnvBlocklistMode(); err != nil {
		return err
	}
	if err := c.validateClusterIssuer(); err != nil {
		return err
	}
	if err := c.validateRegistryURLs(); err != nil {
		return err
	}
	if err := c.validateTimeouts(); err != nil {
		return err
	}
	if err := c.validateSecretEncryptionKey(); err != nil {
		return err
	}
	if err := c.validateTLSPosture(); err != nil {
		return err
	}
	if err := c.validateProductionCredentials(); err != nil {
		return err
	}
	if err := c.validateCORS(); err != nil {
		return err
	}
	if err := c.validateDatabaseSSL(); err != nil {
		return err
	}
	c.logFeatureGates()
	if err := c.validateOpenBaoTransit(); err != nil {
		return err
	}
	c.logCryptoMode()
	return nil
}

// validateEnv enforces the LOOPZERO_ENV allowlist (Sprint A T2.1, closes
// sec-008). MUST run first — all downstream IsProduction() checks
// trust c.Env after this gate.
func (c Config) validateEnv() error {
	if !isValidEnv(c.Env) {
		return fmt.Errorf("LOOPZERO_ENV=%q invalid; allowed: development, staging, production", c.Env)
	}
	return nil
}

// validateRegistryURLs rejects registry hosts that would break the
// buildkitd.toml the build step writes them into (a quote, backslash or line
// break ends or injects a TOML table header).
// validateEnvBlocklistMode refuses an unknown mode, and warn in production:
// the blocklist is what stops a tenant setting LD_PRELOAD or PATH on its
// own process.
func (c Config) validateEnvBlocklistMode() error {
	switch c.EnvBlocklistMode {
	case "", "enforce": // the zero value is the safe default
		return nil
	case "warn":
		if c.IsProduction() {
			return fmt.Errorf("config: LOOPZERO_ENV_BLOCKLIST_MODE=warn is not allowed in production")
		}
		return nil
	default:
		return fmt.Errorf("config: LOOPZERO_ENV_BLOCKLIST_MODE=%q must be enforce or warn", c.EnvBlocklistMode)
	}
}

func (c Config) validateRegistryURLs() error {
	for _, kv := range [][2]string{{"REGISTRY_URL", c.RegistryURL}, {"HARBOR_REGISTRY_URL", c.HarborRegistryURL}} {
		if strings.ContainsAny(kv[1], "\"\\\r\n") {
			return fmt.Errorf("%s=%q contains a quote, backslash or line break; it must be host[:port]", kv[0], kv[1])
		}
	}
	return nil
}

// validateClusterIssuer requires an explicit LOOPZERO_CLUSTER_ISSUER per
// MEET-2026-04-23-058 B14 — no in-code default to avoid the invisible-
// default footgun.
func (c Config) validateClusterIssuer() error {
	if c.ClusterIssuer == "" {
		return fmt.Errorf("LOOPZERO_CLUSTER_ISSUER is required (prod: letsencrypt-prod, dev: loopzero-ca-issuer)")
	}
	return nil
}

// validateTimeouts runs MEET-2026-03-31-023 T2-13 cross-field
// timeout-ordering rules. Hard errors in production for release/sync
// inversion (would reap before sync completes); soft warn for build
// queue/run inversion (unusual but functional).
func (c Config) validateTimeouts() error {
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
	return nil
}

// validateSecretEncryptionKey enforces SEC-001 — encryption key is
// mandatory in production. Also warns when dev callback secret is in
// use outside production.
func (c Config) validateSecretEncryptionKey() error {
	if c.ArgoCallbackSecret == "loopzero-dev-callback-secret" && !c.IsProduction() {
		slog.Warn("using default callback secret — only acceptable for local development")
	}
	if c.SecretEncryptionKey == "" {
		if c.IsProduction() {
			return fmt.Errorf("LOOPZERO_SECRET_ENCRYPTION_KEY is required in production — secrets must be encrypted at rest")
		}
		slog.Warn("LOOPZERO_SECRET_ENCRYPTION_KEY not set — secrets stored in plaintext (only acceptable for local development)")
	}
	return nil
}

// validateTLSPosture rejects insecure TLS knobs in production. Three
// branches that share the same prod-gating shape (SEC-2 ArgoCD,
// REVIEW-BLOCKER-2 registry, MEET-2026-05-28-001 CR-1 cosign).
func (c Config) validateTLSPosture() error {
	if c.ArgocdInsecure && c.IsProduction() {
		return fmt.Errorf("ARGOCD_INSECURE=true is forbidden in production — TLS verification must be enabled")
	}
	if c.RegistryInsecure && c.IsProduction() {
		return fmt.Errorf("REGISTRY_INSECURE=true is forbidden in production — registry TLS verification must be enabled")
	}
	if c.CosignInsecureSkipSCT && c.IsProduction() && !c.CosignTUFRootPinned {
		return fmt.Errorf("LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY=true requires LOOPZERO_COSIGN_TUF_ROOT_PINNED=true in production — SCT verification skip without TUF root is a supply-chain compromise vector")
	}
	return nil
}

// validateProductionCredentials rejects the dev-default sentinels in
// production (H4). Without these gates a misconfigured deploy would
// silently use the constants at the top of this file.
func (c Config) validateProductionCredentials() error {
	if !c.IsProduction() {
		return nil
	}
	if c.MinIOAccessKey == devDefaultMinIOAccessKey {
		return fmt.Errorf("MINIO_ACCESS_KEY using default dev credentials in production — override via MINIO_ACCESS_KEY env var")
	}
	if c.MinIOSecretKey == devDefaultMinIOSecretKey {
		return fmt.Errorf("MINIO_SECRET_KEY using default dev credentials in production — override via MINIO_SECRET_KEY env var")
	}
	if c.BootstrapSecret == devDefaultBootstrapSecret {
		return fmt.Errorf("LOOPZERO_BOOTSTRAP_SECRET using default dev value in production — override via LOOPZERO_BOOTSTRAP_SECRET env var")
	}
	if c.ArgoCallbackSecret == devDefaultArgoCallbackSecret {
		return fmt.Errorf("LOOPZERO_ARGO_CALLBACK_SECRET using default dev value in production — override via LOOPZERO_ARGO_CALLBACK_SECRET env var")
	}
	return nil
}

// validateCORS requires an explicit CORS_ALLOWED_ORIGIN in production
// to prevent open-CORS deployment.
func (c Config) validateCORS() error {
	if c.CORSAllowedOrigin == "" && c.IsProduction() {
		return fmt.Errorf("CORS_ALLOWED_ORIGIN must be set in production")
	}
	return nil
}

// validateDatabaseSSL rejects sslmode=disable in DATABASE_URL +
// LOOPZERO_WORKER_DATABASE_URL in production (H1 + ADR D3 of
// 2026-06-01-p3-infra-observability-test-debt v1.1).
func (c Config) validateDatabaseSSL() error {
	if !c.IsProduction() {
		return nil
	}
	if c.DatabaseURL != "" && strings.Contains(c.DatabaseURL, "sslmode=disable") {
		return fmt.Errorf("DATABASE_URL contains sslmode=disable which is forbidden in production — use sslmode=require or sslmode=verify-full")
	}
	if c.WorkerDatabaseURL != "" && strings.Contains(c.WorkerDatabaseURL, "sslmode=disable") {
		return fmt.Errorf("LOOPZERO_WORKER_DATABASE_URL contains sslmode=disable which is forbidden in production — use sslmode=require or sslmode=verify-full")
	}
	return nil
}

// logFeatureGates emits informational logs for runtime feature gates
// the operator may want to confirm at boot (currently: KEDA scale-to-
// zero). NOT an error path — pure observability.
func (c Config) logFeatureGates() {
	if c.KEDAEnabled {
		slog.Warn("KEDA feature gate is enabled — scale-to-zero (min:0) autoscaling requests will be accepted")
	}
}

// validateOpenBaoTransit enforces T2.5 + EC-05 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2 — fail-loud refusal
// when OpenBao transit is enabled but credentials are incomplete.
func (c Config) validateOpenBaoTransit() error {
	if !c.OpenBaoTransitEnabled {
		return nil
	}
	if c.OpenBaoVaultAddr == "" {
		return fmt.Errorf("OPENBAO_TRANSIT_ENABLED=true requires OPENBAO_VAULT_ADDR (e.g. https://openbao.secrets-system.svc:8200)")
	}
	if c.OpenBaoVaultRoleID == "" {
		return fmt.Errorf("OPENBAO_TRANSIT_ENABLED=true requires OPENBAO_VAULT_ROLE_ID (AppRole role_id)")
	}
	if c.OpenBaoVaultSecretID == "" {
		return fmt.Errorf("OPENBAO_TRANSIT_ENABLED=true requires OPENBAO_VAULT_SECRET_ID (AppRole secret_id)")
	}
	return nil
}

// logCryptoMode emits the active crypto mode at boot per the precedence
// `transit > aes-local > plaintext`. Defense-in-depth observability so
// operators detect when they silently land on the wrong mode.
func (c Config) logCryptoMode() {
	switch {
	case c.OpenBaoTransitEnabled:
		aesIgnored := c.SecretEncryptionKey != ""
		slog.Info("crypto_mode=transit",
			"openbao_addr", c.OpenBaoVaultAddr,
			"aes_key_ignored", aesIgnored,
			"spki_pinned", c.OpenBaoSPKIPinSHA256 != "")
	case c.SecretEncryptionKey != "":
		slog.Info("crypto_mode=aes-local",
			"openbao_transit_enabled", false)
	default:
		// plaintext mode is allowed only in dev (validateSecretEncryptionKey
		// errored in prod on SecretEncryptionKey=""); emit an explicit log
		// so operators do not silently land here.
		slog.Warn("crypto_mode=plaintext",
			"openbao_transit_enabled", false,
			"aes_key_set", false,
			"reason", "neither OPENBAO_TRANSIT_ENABLED nor LOOPZERO_SECRET_ENCRYPTION_KEY set — dev-only")
	}
}

// CryptoMode returns the active secrets encryption strategy: "transit"
// (OpenBao transit), "aes-local" (LOOPZERO_SECRET_ENCRYPTION_KEY AES path),
// or "plaintext" (neither configured — dev-only).
//
// Precedence: transit > aes-local > plaintext. EC-05 absorption: caller
// uses this to decide which encryption client to wire and to gate the
// fail-loud refusal at boot.
func (c *Config) CryptoMode() string {
	if c.OpenBaoTransitEnabled {
		return "transit"
	}
	if c.SecretEncryptionKey != "" {
		return "aes-local"
	}
	return "plaintext"
}

// EffectiveWorkerDatabaseURL returns the connection string background
// loops MUST use. When LOOPZERO_WORKER_DATABASE_URL is set (the
// loopzero_worker non-owner role per migration 000023), RLS policies apply
// to background queries. When empty (legacy), falls back to
// DatabaseURL (loopzero owner role; RLS bypass — defense-in-depth gap).
//
// T3.1 of plan 2026-06-01-p3-infra-observability-test-debt v1.1 ADR D3.
// Operator-controlled rollout: ship migration first, then flip the env
// var on loopzero-api Deployment. Falling back to DatabaseURL keeps the
// system functional in either state.
func (c Config) EffectiveWorkerDatabaseURL() string {
	if c.WorkerDatabaseURL != "" {
		return c.WorkerDatabaseURL
	}
	return c.DatabaseURL
}

// WorkerRoleEnabled reports whether background loops are configured to
// run as the `loopzero_worker` non-owner role (true) vs falling back to
// the `loopzero` owner role (false). Used by observability to log the
// active RLS-bypass posture at startup.
func (c Config) WorkerRoleEnabled() bool {
	return c.WorkerDatabaseURL != "" && c.WorkerDatabaseURL != c.DatabaseURL
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

// envLoader reads typed environment variables for Load and keeps every value
// it could not parse, so a typo stops startup (via Validate) instead of
// silently running with the default.
type envLoader struct {
	errs []error
}

// int returns the variable as an int, or defaultValue when it is unset.
func (l *envLoader) int(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s=%q is not an integer", key, v))
		return defaultValue
	}
	return i
}

// duration returns the variable as a Go duration ("30s", "5m", "1h"), or
// defaultValue when it is unset. A bare number has no unit and is rejected.
func (l *envLoader) duration(key string, defaultValue time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s=%q is not a duration such as 30s or 5m", key, v))
		return defaultValue
	}
	return d
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

// pprofEnabled accepts exactly "true" or "1": profiling is opt-in, so any
// other spelling (including "TRUE" and "yes") leaves it off.
func pprofEnabled(v string) bool {
	return v == "true" || v == "1"
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
