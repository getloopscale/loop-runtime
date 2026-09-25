# loopzero-api — Config manifest (install-config-contract)

Non-secret config the `loopzero-api` chart injects into a **ConfigMap** (`envFrom configMapRef`).
Set per environment via `-f values-<env>.yaml`. Secrets are NOT here — see **[SECRETS.md](SECRETS.md)**.

This table is **set-equal** to the rendered ConfigMap keys (hermetic Go guard `TestLoopZeroApiConfigManifest`).

| Key | Default | Required (prod) | Scope | Description |
|---|---|---|---|---|
| `PORT` | `3001` | N | - | HTTP listen port |
| `METRICS_PORT` | `9090` | N | - | Prometheus metrics port |
| `LOOPZERO_ENV` | `development` | N | - | development/staging/production (allowlist) |
| `LOG_LEVEL` | `info` | N | - | log level |
| `BASE_DOMAIN` | `loopzero.local` | N | - | base domain for app hostnames |
| `SERVER_READ_HEADER_TIMEOUT` | `10s` | N | - | HTTP read-header timeout |
| `SERVER_READ_TIMEOUT` | `30s` | N | - | HTTP read timeout |
| `SERVER_IDLE_TIMEOUT` | `120s` | N | - | HTTP idle timeout |
| `SERVER_GRACEFUL_SHUTDOWN` | `15s` | N | - | graceful shutdown window |
| `DB_MAX_CONNS` | `25` | N | - | pgx pool max conns |
| `DB_MIN_CONNS` | `5` | N | - | pgx pool min conns |
| `DB_MAX_CONN_IDLE_TIME` | `30s` | N | - | pool idle time |
| `DB_MAX_CONN_LIFETIME` | `1h` | N | - | pool conn lifetime |
| `DB_HEALTH_CHECK_PERIOD` | `30s` | N | - | pool health check period |
| `DB_CONNECT_TIMEOUT` | `5s` | N | - | DB connect timeout |
| `DB_STATEMENT_TIMEOUT` | `30000` | N | - | PG statement_timeout (ms) |
| `HTTP_CLIENT_TIMEOUT` | `10s` | N | - | outbound HTTP client timeout |
| `ARGOCD_URL` | `http://argocd-server.argocd.svc.cluster.local:80` | N | internal-url | ArgoCD API URL |
| `ARGOCD_CLIENT_TIMEOUT` | `90s` | N | - | ArgoCD client timeout |
| `ARGOCD_INSECURE` | `true` | N | - | skip ArgoCD TLS verify (dev only; prod must be false) |
| `ARGOCD_POLL_INTERVAL` | `10s` | N | - | ArgoCD poll interval |
| `ARGOCD_SYNC_TIMEOUT` | `300s` | N | - | ArgoCD sync timeout |
| `ARGO_NAMESPACE` | `argo` | N | - | Argo Workflows namespace |
| `ARGOCD_NAMESPACE` | `argocd` | N | - | ArgoCD namespace |
| `API_CROSS_CLUSTER_URL` | `http://loopzero-api.loopzero-system.svc.cluster.local:3001` | N | internal-url | cross-cluster API URL |
| `GITEA_URL` | `http://gitea-http.gitea.svc.cluster.local:3000` | N | internal-url | Gitea URL |
| `GITEA_CROSS_CLUSTER_URL` | (empty) | N | internal-url | Gitea URL reachable from build cluster |
| `GITEA_ORG` | `loopzero` | N | - | Gitea org for tenant gitops repos |
| `CORE_KUBE_CONTEXT` | `k3d-loopzero-core` | N | - | mgmt cluster context name |
| `BUILD_KUBE_CONTEXT` | `k3d-loopzero-build` | N | - | build cluster context name |
| `CELL_0_KUBE_CONTEXT` | `k3d-loopzero-cell-0` | N | - | runtime cluster context name |
| `MINIO_ENDPOINT` | `minio.loopzero-system.svc.cluster.local:9000` | N | internal-url | MinIO endpoint |
| `MINIO_CROSS_CLUSTER_ENDPOINT` | (empty) | N | internal-url | MinIO endpoint reachable from build cluster |
| `SOURCE_BUCKET` | `loopzero-builds` | N | - | S3 bucket for source bundles |
| `REGISTRY_URL` | (empty) | N | - | container registry URL |
| `REGISTRY_INSECURE` | `false` | N | - | skip registry TLS verify (prod must be false) |
| `HARBOR_REGISTRY_URL` | (empty) | N | - | secondary Harbor registry (empty=disabled) |
| `WORKLOAD_INGRESS_URL` | (empty) | N | - | workload ingress URL |
| `LOOPZERO_PREVIEW_PUBLIC_DOMAIN` | (empty) | N | public-url | preview wildcard public domain |
| `LOOPZERO_PUBLIC_DOMAIN` | (empty) | N | public-url | app wildcard public domain |
| `LOOPZERO_CLUSTER_ISSUER` | `letsencrypt-prod` | Y | - | cert-manager ClusterIssuer (required prod) |
| `LOOPZERO_PROMETHEUS_ADDRESS` | (empty) | N | internal-url | Prometheus URL for canary analysis |
| `LOKI_URL` | `http://loki.monitoring:3100` | N | internal-url | Loki push URL |
| `LOOPZERO_GRAFANA_BASE_URL` | (empty) | N | public-url | Grafana deep-link root |
| `GITHUB_APP_ID` | (empty) | N | - | GitHub App ID |
| `GITHUB_API_BASE_URL` | (empty) | N | public-url | GitHub API base URL |
| `BILLING_RETURN_URL` | (empty) | N | public-url | billing return URL |
| `BILLING_UPGRADE_URL` | (empty) | N | public-url | upgrade CTA URL |
| `CF_ACCOUNT_ID` | (empty) | N | - | Cloudflare account ID |
| `CF_KV_NAMESPACE_ID` | (empty) | N | - | Cloudflare KV namespace ID |
| `CF_ZONE_MANAGEMENT` | `false` | N | - | enable CF Zone Management |
| `LOOPZERO_COSIGN_KEYLESS` | `false` | N | - | cosign keyless mode |
| `LOOPZERO_COSIGN_ATTEST_ENABLED` | `false` | N | - | SLSA attestation |
| `LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY` | `false` | N | - | skip SCT verify (local only) |
| `LOOPZERO_COSIGN_TUF_ROOT_PINNED` | `false` | N | - | TUF root pinned |
| `LOOPZERO_FULCIO_URL` | (empty) | N | internal-url | Fulcio URL override |
| `LOOPZERO_REKOR_URL` | (empty) | N | internal-url | Rekor URL override |
| `LOOPZERO_COSIGN_IMAGE` | (empty) | N | - | cosign image |
| `LOOPZERO_SYFT_IMAGE` | (empty) | N | - | syft image |
| `OPENBAO_TRANSIT_ENABLED` | `false` | N | - | OpenBao transit encryption opt-in |
| `OPENBAO_VAULT_ADDR` | (empty) | N | internal-url | OpenBao address |
| `OPENBAO_SPKI_PIN_SHA256` | (empty) | N | - | OpenBao leaf SPKI pin |
| `LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_CURRENT` | (empty) | N | - | service-auth verify public key (current) |
| `LOOPZERO_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS` | (empty) | N | - | service-auth verify public key (previous) |
| `LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR` | (empty) | N | internal-url | service-auth nonce Redis addr |
| `LOOPZERO_SERVICE_AUTH_REQUIRE_SIGNATURE` | `false` | N | - | enforce service-auth signature |
| `LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT` | (empty) | N | - | service-auth secret creation ts (rotation metric) |
| `CB_KEY` | `argocd` | N | - | circuit breaker key |
| `CB_OPEN_THRESHOLD` | `3` | N | - | CB open threshold |
| `CB_CLOSE_THRESHOLD` | `2` | N | - | CB close threshold |
| `CB_RETRY_INTERVAL` | `30s` | N | - | CB retry interval |
| `BUILD_QUEUE_TIMEOUT` | `5m` | N | - | build queue timeout |
| `BUILD_RUNNING_TIMEOUT` | `30m` | N | - | build running timeout |
| `BUILD_RELEASING_TIMEOUT` | `10m` | N | - | build releasing timeout |
| `BUILD_CANCEL_TIMEOUT` | `60s` | N | - | build cancel timeout |
| `BUILD_REAPER_INTERVAL` | `30s` | N | - | build reaper interval |
| `RELEASE_READY_TIMEOUT` | `5m` | N | - | release ready timeout |
| `RELEASE_PENDING_TIMEOUT` | `6m` | N | - | release pending timeout |
| `RELEASE_REAPER_INTERVAL` | `30s` | N | - | release reaper interval |
| `BUILD_RECONCILE_INTERVAL` | `5s` | N | - | build reconcile interval |
| `SSE_HEARTBEAT_INTERVAL` | `15s` | N | - | SSE heartbeat interval |
| `SSE_POLL_INTERVAL` | `1s` | N | - | SSE poll interval |
| `SSE_SAFETY_TIMEOUT` | `5m` | N | - | SSE safety timeout |
| `SSE_MAX_CONCURRENT_STREAMS` | `200` | N | - | SSE max concurrent streams |
| `AUTH_CACHE_TTL` | `5m` | N | - | auth cache TTL |
| `SOURCE_MAX_BUNDLE_SIZE` | `524288000` | N | - | max source bundle size (bytes) |
| `SOURCE_MULTIPART_MAX_MEMORY` | `33554432` | N | - | multipart max memory (bytes) |
| `LOOPZERO_DISABLE_LEGACY_LOOPS` | `false` | N | - | disable in-API background loops |
| `LOOPZERO_ADMISSION_CONTROL` | `false` | N | - | backpressure admission control |
| `MAX_CONCURRENT_BUILDS_PER_CELL` | `20` | N | - | cell concurrent build limit |
| `MAX_TENANTS_PER_CELL` | `5000` | N | - | max tenants per cell |
| `MAX_CONCURRENT_BUILDS_PER_TENANT` | `3` | N | - | the per-tenant concurrent build limit: reconcile backpressure, admission cap and the Tenant CR limit |
| `PG_CIRCUIT_BREAKER_THRESHOLD_PCT` | `90` | N | - | PG pool CB threshold % |
| `LOOPZERO_USE_PG_CLAIMS` | `false` | N | - | PG-backed processing claims |
| `LOOPZERO_KEDA_ENABLED` | `false` | N | - | KEDA scale-to-zero gate |
| `CORS_ALLOWED_ORIGIN` | (empty) | Y | public-url | CORS allowed origin (required prod) |
| `JWKS_URL` | `http://ory-hydra-public.ory.svc.cluster.local:4444/.well-known/jwks.json` | N | internal-url | JWKS endpoint |
| `JWT_ISSUER` | `http://ory-hydra-public.ory.svc.cluster.local:4444/` | N | internal-url | expected JWT issuer |
| `LOOPZERO_ADMIN_API` | `false` | N | - | enable admin API surface |
| `LOOPZERO_MAINTENANCE_MODE` | `false` | N | - | maintenance mode flag |
| `LOOPZERO_PROGRESSIVE_DELIVERY` | `true` | N | - | progressive delivery (canary) flag |
| `LOOPZERO_LEAN_RESOURCES` | `false` | N | - | lean resources mode |
| `LOOPZERO_ENV_BLOCKLIST_MODE` | (empty) | N | - | env blocklist mode |
| `LOOPZERO_ENV_READINESS_CHECKER` | (empty) | N | - | env readiness checker mode |
| `LOOPZERO_TRUSTED_PROXY_CIDRS` | (empty) | N | - | trusted proxy CIDRs |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | N | internal-url | OTel collector endpoint |
| `LOOPZERO_TRACING_ENABLED` | `false` | N | - | OTel tracing opt-in |
| `LOOPZERO_PPROF_ADDR` | (empty) | N | - | pprof listen addr |
| `LOOPZERO_PPROF_ENABLED` | `false` | N | - | pprof gate |

> **public-url** = browser-facing `https://`; **internal-url** = cluster DNS `http://`. Never mix (redirect-loop bug class).

## Deliberately NOT chart knobs (operator/topology-set)

These env vars the engine reads are **intentionally excluded** from this ConfigMap (review F1 / AR-S3-01) — listed as prose, not table rows, so they are not counted as config keys:

- **TENANT_NAMESPACE** — inert/deprecated per ADR-0018; a source-guard test forbids any code from reading it (const `loopzero-system`). Injecting it would re-publish a closed foot-gun.
- **LOOPZERO_CELL_ID** — per-controller-instance value (operator/topology-set at pod creation); a single chart-wide ConfigMap cannot express per-cell identity in multi-cell.
