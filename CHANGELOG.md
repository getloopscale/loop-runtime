# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/)
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed
- **Scaling config (`replicas` + `autoscaling`) wired end-to-end and plan-capped** (CHG-2026-04-23-B15 / MEET-2026-04-23-059 / ADR-0011): previously the CLI recognised, persisted, and rendered scaling config but no plan-cap enforcement existed; users on any plan could request any replica count, and the CLI validator warned "Unknown field 'apps.api.replicas' -- ignored". This release closes the chain from `loopzero.yaml` → CLI validator → CLI payload → API route DTO → adapter → orchestrator → persisted spec. Mutual exclusion between `replicas` and `autoscaling` is now enforced at both CLI and API boundaries (`SCALING_AMBIGUOUS`, HTTP 422). Per-service replica cap enforced via new `max_replicas_per_service` plan metric: Starter=2, Pro=5, Team=10, Enterprise=unlimited (`REPLICAS_EXCEED_PLAN`, HTTP 422). The authoritative enforcement point is `BuildOrchestrator.SubmitBuild` so both the CLI deploy path and the GitHub App auto-deploy webhook are covered by the same gate.

### Fixed
- **False "Unknown field" warning on `replicas`/`autoscaling`** (CHG-2026-04-23-B15): CLI validator `knownFields` allow-list extended.
- **CLI parser silently dropped invalid scaling values** (CHG-2026-04-23-B15): `parseLoopZeroYaml` now preserves all numeric values so `validate-config.ts` can reject them explicitly with actionable messages naming the field and constraint.
- **Pre-existing duplicate `TestMiddleware_EmptyTenantPasses` symbol in `pkg/enforcement`** (CHG-2026-04-23-B15 unblocker): build failure inherited from `develop` that blocked enforcement tests. Duplicate in `limiter_test.go` removed; identical test remains in `middleware_test.go`.

### Added
- **Observability gaps resolution** — comprehensive observability improvements across all components
  - **Blackbox probes**: blackbox_exporter with HTTP, DNS, TCP modules. Probes API, Gitea, Traefik, ArgoCD. Alerts for probe failure, TLS cert expiry, slow probes
  - **Deploy E2E trace**: 12 new OTel spans covering auth, idempotency, ArgoCD sync/callback, release activation, rollback, domain provisioning, build reconcile, webhook, auto-deploy
  - **Sloth SLOs**: 7 formal SLO specs (deploy success 99.5%, deploy latency 95%<5min, rollback 99.9%, tenant availability 99.9%, API availability 99.9%, build pipeline 99%, domain provisioning 99%) with multi-window multi-burn-rate alerts
  - **Business metrics**: `loopzero_deploy_lead_time_seconds`, `loopzero_false_success_total`, `loopzero_retry_amplification_total`
  - **CLI tracing**: OpenTelemetry SDK in CLI with W3C TraceContext propagation to API. CLI spans become parent of API spans for E2E traces
  - **pprof endpoints**: on-demand profiling via `kubectl port-forward` on API (:6060) and operators (:6061)
  - **Telemetry pipeline observability**: ServiceMonitor for OTel Collector, alerts for span drop rate and export failures
  - **Dashboards**: Executive Summary (SLO error budgets, burn rates, deploy trends, lead time) and Blackbox Probes (probe status, latency, TLS, DNS)
- **`loopzero promote` command** — promote a release from one environment to another without re-building. API endpoint `POST /api/v1/projects/{name}/promote`, PromotionOrchestrator service, CLI command with `--from`/`--to` flags and `--json` output. Uses existing promotion state machine. Locks target environment via `withEnvironmentLock`. 28 new tests (6 service + 8 route handler + 15 CLI).

### Fixed
- **Tenant deletion stuck with no recovery** (operators-review): After DeletionMaxAttempts, controller now requeues every 1h instead of stopping entirely. Allows self-healing if dependencies recover. Metric `loopzero_tenant_deletion_stuck_total` still emitted for alerting.
- **MinIO error matching fragility** (operators-review): Replaced custom `contains()`/`searchString()` with `strings.Contains()` in minio provisioner. Added regression test `TestIsNotFoundError`.
- **Gitea EnsureOrg swallowed verification errors** (operators-review): On 422 + verification failure, now returns error instead of nil. Prevents silent org creation failures.
- **Inconsistent status update error handling** (operators-review): Standardized across GitRepositoryBinding and ClusterCredential controllers to log statusErr instead of returning it (preserves original reconcile error).
- **EnvironmentController pull secret failures invisible** (operators-review): Added `PullSecretReady` condition (informational, non-blocking) so operators can detect pull secret provisioning failures via `kubectl describe environment`.
- **Hardcoded "platform-system" string** (operators-review): Replaced with `constants.NamespacePlatformSystem` in `environment_controller.go`.
- **Grafana tenant-workloads dashboard showing no data**: Installed kube-state-metrics on runtime cluster with cross-cluster NodePort (30092) scrape from mgmt Prometheus. Dashboard queries use `job="runtime-kube-state-metrics"` which now resolves to real metrics. Also fixes tenant-ecosystem dashboard.

### Added
- **CircuitBreaker for MinIO provisioner** (operators-review): Wraps MinIO CredentialProvisioner with same CB pattern as Gitea/ArgoCD. Prevents retry storms on MinIO outage.
- **CircuitBreaker for Harbor client** (operators-review): Wraps Harbor Client with CB pattern. Preserves nil-guard behavior.
- **Type tests for 5 CRDs** (operators-review): Added type-level tests for LoopZeroApplication, Environment, ExposedRoute, GitRepositoryBinding, PlatformBootstrap (~40 new tests).
- **Ginkgo fake provisioner Reset()** (operators-review): Added `BeforeEach` in suite_test.go to reset fake provisioner state between test Contexts. Prevents state leakage between tests.
- **PlatformBootstrap ownership check** (operators-review): Validates `managed-by` label before deleting platform-tokens secret during cleanup.

### Security
- **TLS verification enforced in production** (operators-review, SEC-001): Added `OPERATOR_INSECURE_SKIP_TLS=false` to Pulumi production config and explicit `true` for dev. Previously defaulted to skip (insecure).

### Removed
- **AppSecret CRD unregistered** (operators-review): Removed from scheme registration. No controller ever implemented — secrets managed via ESO directly. Go types retained for future use.
- **ReconciliationReport CRD unregistered** (operators-review): Removed from scheme registration + CRD YAML deleted. Dead code with no controller or sprint assigned. Go types retained as experimental.

### Added
- **Tenant deletion lifecycle — Phases 0-5** (MEET-052): Full tenant deletion lifecycle implementation. Meeting record: MEET-2026-04-17-052.
  - Phase 0: DeleteApplications NotFound idempotency fix (prevents retry storms)
  - Phase 1: MaxConcurrentReconciles=5, exponential backoff (5s-5min, 10 max), PendingDeletion state machine
  - Phase 2: Generic circuit breaker (pkg/circuitbreaker/) + Gitea/ArgoCD provisioner wrappers
  - Phase 3: DB migration (tenants_pending_deletion + tenant_tombstones), LOOPZERO_ADMIN_API_KEY middleware with dual-key rotation, DELETE /api/v1/tenants/:id endpoint (soft-delete + 202), POST /api/v1/tenants/:id/restore (grace period cancel), IDOR protection
  - Phase 5: Per-step deletion metrics (loopzero_tenant_deletion_step_duration_seconds, loopzero_tenant_deletion_stuck_total, loopzero_tenant_cb_state)
- **Meeting record MEET-052** (MEET-052): Tenant Lifecycle & Deletion design review. TenantReaper rejected, controller-native approved. 3 security vetoes resolved, 21 mandatory actions.

### Fixed
- **DeleteApplications NotFound idempotency** (MEET-052): ArgoCD DeleteApplications now handles NotFound errors during concurrent deletion, preventing infinite requeue loops that caused retry storms on bulk tenant deletion.

### Added
- **Platform hardening v2 — E2E workflow** (hardening-v2): Weekly scheduled `infra-e2e.yml` validates full destroy→up→bootstrap→health cycle. `health-check.sh` validates 30+ deployments, services, tunnel, nodes, external endpoints.
- **OTel instrumentation — operators/** (hardening-v2): Custom Prometheus metrics (reconcile_duration, reconcile_errors, provisioner_duration). Child spans on all 7 controllers for provisioner calls (Gitea, ArgoCD, Harbor, MinIO). Error recording on spans. TraceExtractor for pkg/logging.
- **OTel instrumentation — cloud/** (hardening-v2): TracerProvider + OTLP exporter. otelhttp on all HTTP clients (Hydra, Kratos, Stripe). Prometheus metrics replacing atomic counters. trace_id in slog.
- **pkg/logging context propagation** (hardening-v2): `TraceExtractor` interface + `FromContext()` for trace_id/span_id injection without OTel dependency.
- **State integrity check** (hardening-v2): Post-deploy step in `infra-deploy.yml` detects duplicate URNs in Pulumi state.
- **Infra pre-merge checklist** (hardening-v2): `docs/checklists/infra-pre-merge.md` with 15 validation items.

### Changed
- **Pipeline simplification** (MEET-049, MEET-050): Rewrote infra pipeline from 1 monolithic workflow (1539 lines) to 5 focused workflows (~470 lines total). `infra-deploy.yml` (Pulumi up only), `infra-bootstrap.yml` (idempotent tokens/secrets/seed), `infra-destroy.yml` (stack-scoped with approval), `infra-restart.yml` (CI-gated pod restart), `infra-state-reset.yml` (break-glass emergency). Zero `pulumi cancel`/`state delete` in deploy path. Stack-scoped destroy prevents cross-environment deletion.
- **Pulumi idempotency** (hardening-v2): `resolve-build-node-ip` trigger changed from `Date.now()` to cluster endpoint. `gitea-bootstrap` Job: `ignoreChanges` on status/labels.

### Security
- **COOKIE_SECRET rotated to RandomPassword** (C1): Kratos UI cookie secret replaced from hardcoded string to Pulumi-managed `RandomPassword`. Eliminates session cookie forgery risk from source code access.
- **LOOPZERO_SERVICE_AUTH_SECRET auto-generated** (H12): Service-to-service auth secret now auto-generates via `RandomPassword` when not explicitly configured. Removes `dev-service-auth-secret-default` fallback.
- **Database and DOKS clusters protected** (C3): Added `protect: true` and `retainOnDelete: true` to PostgreSQL cluster and `protect: true` to all DOKS clusters. Prevents accidental `pulumi destroy` data loss.
- **RBAC escalate verb removed** (H15): Removed `escalate` verb from tenant controller ClusterRole on `clusterroles`/`clusterrolebindings`. Prevents privilege escalation from compromised controller pod.
- **RBAC loopzero.io wildcard replaced** (M42): Replaced `resources: ["*"]` with explicit CRD enumeration in tenant controller ClusterRole. New CRDs require explicit RBAC review.

### Fixed
- **Environment state machine: Failed state added** (H8): `EnvironmentFailed` state now reachable via `ProvisionFailed` event from `Pending`, with `Retry` path back to `Pending` and `DeleteRequested` exit.
- **Release state machine: cancel paths added** (M35): `GIT_MIRROR_FAILED`, `SYNC_FAILED`, `DRIFT_ALERT`, `ROLLBACK_FAILED` states now have `ReleaseCancelled` exit path.
- **Domain state machine: v2 degraded from provisioning** (M36): Added `DomainDegrade` transition from `DomainProvisioning` to `DomainDegraded` for CNAME loss during v2 setup.
- **Application computePhase uses all phases** (M28): `Serving`, `Degraded`, and `Down` phases now produced based on endpoint readiness and route conditions.
- **ExposedRoute probe response bodies drained** (M29): Added `io.Copy(io.Discard, resp.Body)` before close to allow HTTP connection reuse.
- **Rollback Phase 5 re-verifies release state** (M2): Re-fetches release inside lock to guard against concurrent deploy between Phase 1 and Phase 5.
- **Convergence loop semaphore fix** (M5): Semaphore acquired before goroutine spawn to prevent unbounded goroutine accumulation.
- **Harbor robot delete errors logged** (M34): `DeleteRobotAccount` errors now logged with context instead of silently discarded.
- **Enforcement limiter: hardLimit==0 denies** (M37): Missing plan limit rows now fail closed (deny) instead of silently allowing unlimited access.
- **Auto-deploy goroutine uses shutdown context** (H4): Auto-deploy goroutine now uses `shutdownCtx` with 5-minute timeout instead of `context.Background()`.
- **Callback goroutines have timeout** (H5): `onReleaseActivated` and `onBuildFailed` goroutines now use 30-second timeout context.
- **Tenant deletion sets condition on env delete failure** (H9): Failed Environment CR deletion now sets `EnvironmentCleanup` condition on Tenant status for visibility.
- **Domain handlers have request timeouts** (M7): All 6 domain endpoints now use `context.WithTimeout` (30s read, 60s write).
- **DNS records per-request limit** (M11): `AddDNSRecords` now rejects requests with more than 50 records.
- **Removed ungated GetByDomain** (M12): Removed `GetByDomain` (no tenant filter) — only `GetByDomainAndTenant` remains.
- **CancelActiveBuilds bypass documented** (M3): Added documentation explaining intentional state machine bypass for batch operations.
- **Service auth rejects invalid headers immediately** (M39): `X-Service-Auth` with wrong value now returns 401 instead of falling through to Bearer auth.
- **Auto-deploy commitSHA safe slice** (LOW): `commitSHA[:12]` replaced with bounds-checked slice to prevent panic on short SHAs.
- **Environment name max-length enforced in CLI** (LOW): `loopzero env create` now validates max 63 chars, matching deploy command validation.
- **Reserved names no longer leak Kubernetes** (LOW): `kube-system` removed from `RESERVED_NAMES` — replaced with generic `system`/`internal`.
- **PlatformBootstrap finalizer added** (M31): Cleanup of `platform-tokens` secret on PlatformBootstrap CR deletion.
- **Build namespace DNS egress scoped to kube-system** (LOW): DNS egress rule restricted to `kube-system` namespace, preventing DNS tunneling from build pods.
- **ArgoCD AppProject namespace glob removed** (M32): Glob fallback `loopzero-{tenant}-*` replaced with fail-closed empty destinations list. Deploys blocked until EnvironmentController provides explicit namespaces.
- **ArgoCD cluster secret scope reduced** (M33): Full kubeconfig no longer stored in ArgoCD cluster secret. Only bearer token + CA certificate extracted and stored in `config` field.

### Changed
- **Image build optimization** (Sprint 6+): Comprehensive build pipeline modernization:
  - All Dockerfiles use `--mount=type=cache` for Go modules and build cache (~30-50% faster rebuilds)
  - All Go builds use `-trimpath -ldflags="-s -w"` (smaller binaries, no path metadata)
  - All platform Dockerfiles have `HEALTHCHECK`, OCI labels, consolidated layers
  - Clone-runner `mc` binary verified with SHA256 checksum (supply chain hardening)
  - Cosign signing uses pre-built `loopzero-cosign-signer` image (no runtime binary downloads)
  - Dashboard runs as non-root via `nginxinc/nginx-unprivileged` on port 8080
  - `.dockerignore` expanded to exclude Pulumi node_modules, dist, build, coverage
  - Generated Dockerfiles (loopzeropacks) add `--mount=type=cache`, non-root USER, OCI labels for all 7 languages
  - PHP uses multi-stage `FROM composer:2.8` instead of `curl | php` installer
  - Rust binary name fixed (was evaluating in generator, not Docker build)
  - SLSA provenance attestation (`--provenance=max`) on user-built images via BuildKit
  - CI: keyless cosign signing (Sigstore/Fulcio OIDC), harbor-init signed + scanned
  - CI: `SOURCE_DATE_EPOCH` for reproducible builds, `--provenance=true --sbom=true` on all platform images
  - CI: `docker-bake.hcl` for declarative multi-image builds
  - CI: Trivy `--ignore-unfixed` + `.trivyignore` for false positive management
  - Semver release workflow (`.github/workflows/release.yml`)
  - ADR-0003 updated: Kaniko → BuildKit (reflects actual architecture)

### Added
- **Supply chain security — image signing** (Sprint 6): cosign sign + syft SBOM + cosign attest for all 4 platform images in CI pipeline. New `sign-and-attest` job with matrix strategy in `build-images.yml`. Trivy post-build scan on final images. SBOM artifacts retained 90 days.
- **Supply chain security — user image signing** (Sprint 6): Argo Workflow DAG now includes `sign-image-{app}` step after each `build-image-{app}`. Uses `gcr.io/projectsigstore/cosign:v2.4.1` with key mounted from `cosign-signing-key` K8s Secret.
- **Image signature admission policy** (Sprint 6): `vap-require-image-signature` ValidatingAdmissionPolicy requires digest references (`@sha256:`) for all container images in tenant namespaces. Starts in Audit mode.
- **Build attestation endpoint** (Sprint 6): `GET /api/v1/builds/:id/attestation` returns image digest, signature reference, and signing timestamp.
- **DB migration 000034**: Adds `image_digest`, `image_signature`, `image_signed_at` columns to `releases` table for supply chain tracking.
- **Distributed tracing — Grafana Tempo datasource** (Sprint 6): Tempo added as Grafana datasource with Loki derived fields linking `trace_id` to traces.
- **Distributed tracing — PG query tracing** (Sprint 6): `otelpgx` tracer hooks into pgx pool, creating spans for every database query with statement and duration.
- **Distributed tracing — operator instrumentation** (Sprint 6): OTel SDK added to operators with spans on all 7 controller reconcile loops (`loopzero.operator.{name}.reconcile`).
- **Cross-plane trace propagation** (Sprint 6): Argo Workflow annotations carry `loopzero.io/trace-id` and `loopzero.io/span-id` for end-to-end deploy trace correlation.

### Added
- **Harbor container registry** (MEET-048): Minimal deployment (core×2, registry×2, jobservice×1, Redis×1) with Helm chart, PDB, NetworkPolicy. Trivy/Notary/Portal disabled.
- **Harbor Pulumi provider** (MEET-048): `HarborRegistryProvider` implements `IProviderRegistry` ISP contract. Conditional `registryType` config (docr/harbor) with backward compatibility.
- **Container images bucket** (MEET-048): DO Spaces bucket `loopzero-*-container-images` provisioned via Pulumi for Harbor S3 storage backend.
- **Harbor API client** (MEET-048): `operators/internal/harbor/client.go` — project CRUD, robot account lifecycle, health check. 15 unit tests with httptest.
- **Per-tenant Harbor projects** (MEET-048): TenantController creates Harbor project per tenant with plan-based quotas (Starter=5GB, Growth=50GB, Enterprise=unlimited), immutable tags, robot accounts (build push+pull, workload pull-only, TTL ≤ 24h).
- **Registry health monitoring** (MEET-048): Canary probe metrics, 6 PrometheusRule alerts (LoopZeroRegistryHealthFailed critical, push/pull latency, storage, GC, PG connections), Grafana dashboard.
- **Harbor init script** (MEET-048): Idempotent Go script for post-deploy configuration — admin password, system config, `loopzero-platform` project, CI robot account.
- **Image mirror script** (MEET-048): Go script using `crane` for parallel DOCR→Harbor image migration with digest verification.
- **ADR-002** (MEET-048): Registry migration decision record documenting Harbor + DO Spaces choice, alternatives, trade-offs, dissent.
- **Harbor dual-push for tenant builds** (MEET-048): `HARBOR_REGISTRY_URL` env var enables crane-based mirror step in Argo Workflow DAG. Best-effort (continueOn.failed=true). DOCR remains primary.
- **Harbor init automation** (MEET-048): Dockerfile + Helm post-install Job (`harbor-init`) + CI build step. Zero manual intervention after `pulumi up`.
- **Harbor canary CronJob** (MEET-048): Runs every 5min, checks health + API access. Failure detected via kube-state-metrics alert.

### Changed
- **Harbor values.yaml aligned with Pulumi** (MEET-048): Replicas now 1 for dev (matches `harbor-bootstrap.ts`). PDB conditional on `replicas > 1`.
- **Harbor HarborProjectReady is now required** (MEET-048): Tenant cannot transition to Active without HarborProjectReady=True when HarborClient is configured. Backward compatible with DOCR mode.
- **Harbor robot secret self-healing** (MEET-048): If K8s Secret is manually deleted, next reconcile forces robot recreation to obtain new credentials. Prevents 22-hour blind window.
- **Harbor Docker config.json uses json.Marshal** (MEET-048): Replaced manual `quote()` with `json.Marshal` for correct escaping of all JSON special characters.

### Fixed
- **Harbor PDB blocking node drain** (MEET-048): PDB with `minAvailable: 1` on single replica blocked `kubectl drain`. Now conditional on `replicas > 1`.

### Changed
- **Git operations centralized in `internal/gitclient`**: Eliminates duplicated gitCmd/sanitize/ensureRepo across source, gitops, and github packages. Single GitRunner with exec.CommandContext + token sanitization.
- **Build scripts generated via type-safe `internal/shellgen`**: All 15 script functions in argo/scripts.go migrated to ScriptBuilder. User-supplied paths escaped via ShellQuote (POSIX single-quote). Zero fmt.Sprintf for shell in scripts.go.
- **BuildKit entrypoint uses ShellQuote**: socketPath escaped via ShellQuote, registryURL validated before TOML injection.
- **Source store go-git implementation**: `source/gogit_store.go` mirrors gitops/gogit_pusher pattern. GitRunner only for `git bundle create`.

### Security
- **Fixed unsafe tar extraction in GitHub auto-deploy**: Added symlink rejection, path traversal protection, and decompression size limits via SafeExtractTarGz. Previously extracted tarballs without any validation.
- **Shell script generation uses POSIX-safe quoting**: All interpolated values in build container scripts quoted via ShellQuote, preventing injection via crafted loopzero.yaml paths.
- **autodeploy.go uses exec.CommandContext**: Git commands in tarballToGitBundle now respect context cancellation (previously used exec.Command without context).

### Changed
- **CI dual-push** (MEET-048): `build-images.yml` and `infra-deploy.yml` push platform images to both DOCR and Harbor during transition period. Harbor steps conditional on `HARBOR_ROBOT_PASSWORD` secret.
- **Registry admission policy** (MEET-048): `vap-restrict-registries` updated to allow both DOCR and Harbor prefixes during dual-push transition.
- **Registry token expiry** reduced from 3600s to 600s in CI pipeline (SEC-002 compliance).

### Security
- **Per-tenant image isolation** (MEET-048): Harbor robot accounts scoped per-project prevent cross-tenant image access. Previously only naming convention.
- **Harbor admin API restricted** (MEET-048): NetworkPolicy limits Harbor core access to Traefik ingress and loopzero-system namespace.

### Added
- **DO PostgreSQL tuning** (`DatabasePostgresqlConfig`): pg_stat_statements=all, slow query logging >1s, idle transaction timeout 10min, shared_buffers 25%
- **DO Database role separation** (`DatabaseUser` + `DatabaseConnectionPool`): API connects as `loopzero_api` (DML only) through managed PGBouncer pool (transaction mode, 20 conns)
- **DO UptimeCheck**: External health monitoring of API from 3 regions (us_east, us_west, eu_west)
- **DO Project**: Groups all environment resources in DO dashboard
- **DO Registry credentials**: `ContainerRegistryDockerCredentials` generates docker config.json automatically (eliminates manual `doctl registry add`)
- **CF WAF**: Managed OWASP + Cloudflare rulesets at edge (`security.ts`)
- **CF Rate Limiting**: Auth 20/min, API 300/min, global 1000/min per IP
- **CF Zero Trust Access**: ArgoCD, Grafana, Gitea behind CF Access auth (email domain policy)
- **CF Logpush**: HTTP request + firewall event logs exported to DO Spaces
- **Build admission gate** (Roadmap 07): Cell-level and per-tenant concurrent build limits. Rejects with HTTP 429 when cell at capacity (`MAX_CONCURRENT_BUILDS_PER_CELL=20`) or tenant at capacity (`MAX_CONCURRENT_BUILDS_PER_TENANT=3`). 4 unit tests including `TestBuildAdmission_CellCapacityReject` and `TestBuildAdmission_TenantFairness`.
- **PG pool circuit breaker** (Roadmap 07): HTTP middleware that rejects mutation requests (POST/PUT/DELETE) with 503 + Retry-After when PG pool utilization exceeds threshold (`PG_CIRCUIT_BREAKER_THRESHOLD_PCT=90`). Read requests always pass through. 3 unit tests.
- **Cell capacity budget** (Roadmap 04): Published at `docs/capacity/cell-budget.md` with concrete numbers for tenants, builds, PG connections, ArgoCD apps per cell. Includes saturation thresholds (Yellow 70%, Red 90%) and graceful degradation behavior.
- **Cell provisioning — real implementation** (Roadmap 05): `cell.ts` now provisions complete DOKS clusters with ArgoCD, Traefik, cert-manager, network policies, and Prometheus agent. Zero placeholder values remain.
- **TOCTOU placement test** (Roadmap 06): `TestPlacement_ConcurrentCreation_NoTOCTOU` proves 10 concurrent tenant creations distribute evenly across 2 cells with no hot-spotting.
- **L4/L5 load tests** (Roadmap 08): `TestL4_500Tenants` (500 tenants, 2 cells, 20 concurrent) and `TestL5_1000Tenants_MultiCell` (1000 tenants, multi-cell, 50 concurrent) with pass criteria: >99% success, p99 < 10min.
- **Chaos test suite** (Roadmap 08): 7 implemented chaos tests replacing 6 PIt stubs. Covers: API pod restart, controller crash recovery, PG pool exhaustion, ArgoCD crash, PG failover, cell network partition.
- **Admission control metrics**: `loopzero_build_admission_admitted_total`, `loopzero_build_admission_rejected_total`, `loopzero_pg_circuit_breaker_tripped_total`.

### Changed
- **CI/CD pipeline optimization**: Build Images only triggers on app code changes (`api/`, `operators/`, `cloud/`, `pkg/`). Infra Deploy only triggers on infra changes (`infra/`). New images trigger lightweight pod restart (~2min) instead of full `pulumi up` (~15min). Eliminates 25min polling loop.

### Added
- **Declarative multi-cluster topology** (`pulumi-infra/src/topology/`): Config-driven N clusters per role. TopologyConfig schema, parser+validator, generic provisioner keyed by stable `clusterId`, ClusterRegistry output for StackReference. Cell factory (`createCell/createCells`) associates cellId→runtime cluster. 10 topology test cases. Legacy fallback from singleton config.
- **Distributed work queue** (`api/internal/workqueue/`): PG-backed queue with `SELECT ... FOR UPDATE SKIP LOCKED`. Enables non-leader pods to process background work. Queue/Consumer abstraction for all 6 reconcile loops.
- **Dynamic cell router** (`api/internal/placement/dynamic_router.go`): Periodically refreshes cell list from CellStore. K8sCellStore reads ClusterCredential CRDs for multi-cell Phase 2 routing.
- **Load test L3/L4**: L3 (100 tenants concurrent), L4 (50 concurrent lock stress on same project). CI workflow for scheduled/manual load test execution.
- **DB pool saturation metrics**: `loopzero_postgres_pool_acquire_duration_seconds`, `loopzero_postgres_pool_empty_acquire_total`, `loopzero_postgres_pool_max_conns` for contention visibility.
- **DB pool alerts**: `LoopZeroDBPoolSaturation` (>80% utilization, 5m), `LoopZeroDBPoolAcquireLatency` (p95 >100ms, 3m).
- **pg_stat_statements** enabled via migration 000029 for slow query visibility.
- **Leader-generation fencing primitive** (Roadmap 02). New package
  `api/internal/leader/` providing `Acquirer` with `Acquire`, `Heartbeat`,
  `Validate`, and `Release`. Every successfully acquired `Lease` carries a
  monotonic `GenerationID` that must be bound into every side-effecting SQL
  write; a stale leader's writes match zero rows and no-op. Heartbeat and
  Validate return `ErrFenced` when another holder has taken over, signaling
  the caller to exit. Backed by migration `000030_leader_generations.up.sql`
  (cell-scoped per-loop counter table). 16 unit tests including the
  explicit `TestLeaderGeneration_StaleWriteRejected` from the DoD.
- **PG-backed processing claims** (Roadmap 02). New package
  `api/internal/processing/` providing `ClaimStore.Claim`,
  `ClaimStore.Release`, and `ClaimStore.FindOrphanedClaims` over the
  `builds` and `releases` tables. Replaces the in-process `sync.Map`
  dedup used by `BuildReconcileLoop` and `ReleaseConvergenceLoop` so two
  controller replicas can never concurrently process the same build /
  release. Orphaned claims (worker crashed mid-processing) are
  automatically reclaimable after a 2-minute stale window via the CAS
  predicate. Backed by migration `000031_build_release_processing_claim.up.sql`
  adding `processing_by` / `processing_since` columns and partial indexes.
  12 unit tests including the DoD-required
  `TestProcessingClaim_OrphanDetection`.
- **Cell-scoped reconciliation queries** (Roadmap 03). Every `Find*` store method
  that backs a reconciliation loop now accepts an explicit `cellID` parameter and
  emits `WHERE cell_id = $N` at the SQL level. Migration 000029 denormalizes
  `cell_id` onto `builds` and `releases` (populated from `tenants.cell_id` via a
  `BEFORE INSERT` trigger), adds composite indexes
  `idx_builds_cell_status_created` and `idx_releases_cell_status_created`, and
  backfills existing rows. Custom domains stay JOIN-scoped because the cardinality
  is low. Signatures updated: `FindActiveBuilds`, `FindPendingReleases`,
  `FindActiveReleases`, `FindStaleBuilds`, `FindStaleReleases`,
  `FindPendingForAutoVerify`. Config structs `BuildReconcileLoopConfig`,
  `BuildReaperConfig`, `ReleaseReaperConfig`, `ReleaseConvergenceLoopConfig`,
  `DomainHealthCheckerConfig` gained a `CellID` field (defaults to `cell-default`
  for backward compatibility). 10 new unit tests (`cell_scoped_queries_test.go`,
  `stale_checker_cell_scope_test.go`) — all green with zero impact on existing
  tests. Prerequisite for ADR-0008 cell isolation and the future PG partition-
  by-cell_id migration tracked as escalation risk.
- IaC: split em 3 projetos Pulumi (`pulumi-infra`, `pulumi-platform`, `pulumi-control-plane`) com StackReference e contratos tipados
- IaC: `pulumi-shared/stack-outputs.ts` como single source of truth para contratos entre projetos
- IaC: StackReference parametrizado via config (`infraStackRef`, `platformStackRef`) — suporta per-developer stacks
- IaC: ADR-001 documentando boundaries, contratos e estratégia de migração
- IaC: baseline document com inventário de 160+ recursos, 19 outputs, 33 configs, 6 CRITICAL resources

### Changed
- IaC: admission policy `vap-require-resource-limits` de `Audit` para `Deny` — enforcement real em tenant namespaces
- IaC: Traefik com 2 réplicas, PDB (minAvailable: 1), topologySpreadConstraints e JSON logging
- IaC: PSA `enforce: baseline` + `warn: restricted` em todos os namespaces gerenciados (platform-system, argo, monitoring, external-secrets)
- IaC: Argo Workflows com `volumeClaimGC: OnWorkflowCompletion` — auto-delete PVCs de builds completados/falhos

### Fixed
- **Manifest gate is YAML-document-aware** (Roadmap 01, Correction G). Previously a
  no-op after MEET-2026-04-09-042 reverted the substring-matching gate that blocked
  all new tenant deploys (Certificate CRs with `issuerRef.kind: ClusterIssuer` matched
  the forbidden substring). The gate now parses each YAML document and inspects only
  the top-level `apiVersion`+`kind`, so Certificate resources pass while top-level
  `Namespace`, `ClusterRole`, `ClusterRoleBinding`, `ClusterIssuer`, and
  `CustomResourceDefinition` are rejected. Malformed YAML fails closed. 9 unit tests.
  Files: `api/internal/gitops/manifest_gate.go`, `manifest_gate_test.go`.
- IaC: `cfKvNamespaceId` agora vem do output real do `createStaticDelivery`, não de config estática — elimina dual source of truth que quebrava bootstrap em stack nova
- IaC: recursos Cloudflare (tunnel, KV, Worker) parametrizados com environment — evita colisão entre stacks na mesma conta CF
- IaC: bucket names (`builds`, `logs`, `static-assets`) parametrizados com `bucketName()` por env/region — evita corrupção cruzada entre stacks
- IaC: CDN movido para `CloudProviderFactory` — elimina `digitalocean.Cdn` direto em `infra.ts` e `spacesEndpoint` hardcoded
- IaC: `execution.declaredAt` removido — substituído por `infraVersion` determinístico, elimina diff artificial em todo `pulumi up`
- IaC: Promtail no runtime cluster reintegrado ao Pulumi — elimina step manual e garante reprodutibilidade em disaster recovery
- IaC: Loki S3 endpoint e bucket name agora consomem outputs tipados da fase de infra em vez de strings hardcoded
- IaC: Tempo storage endpoint agora vem do provider em vez de hardcoded `digitaloceanspaces.com`

## [0.13.0] — 2026-04-09

### Added
- **GitHub App auto-deploy** — `git push` triggers automatic deploy (model Vercel)
  - GitHub App "LoopZero Platform" (ID 3319955) with HMAC-SHA256 webhook validation
  - Source download via GitHub API installation tokens
  - Tarball→git bundle conversion for build pipeline compatibility
  - S3 upload + build submission from webhook handler
  - Commit status lifecycle: pending on push, success/failure on build complete
  - `loopzero connect` / `loopzero disconnect` CLI commands (14 tests)
  - Repository linking API: POST/DELETE/GET `/api/v1/projects/{name}/connect`
  - DB: `github_installations` + `project_repositories` tables (migration 027)
- **Custom Domains v2** — Cloudflare Zone Management (model Vercel)
  - `loopzero domain add` creates CF zone per domain, scans DNS, imports records
  - User only changes nameservers — CF handles TLS + WAF/DDoS
  - Apex domains via CF CNAME flattening (no A record workaround needed)
  - `loopzero domain dns` command for DNS record management
  - DB: `cf_zone_id`, `cf_zone_status`, `cf_nameservers` columns (migration 026)
  - State machine: `pending_ns` → `ns_active` → `provisioning` → `active`
  - E2E validated with aquim.ai (Python API + React frontend monorepo)
- **Monorepo Validator** — instant pre-upload validation
  - 30 framework recognition (Node, Go, Python, Ruby, Java, PHP, Rust)
  - Frontend auto-infers `render: static` (no explicit declaration needed)
  - Max 2 pods per project (frontends don't count)
  - Dockerfile detection for custom frameworks
  - Path traversal protection (SEC-10)
  - Validation runs in `loopzero deploy` before upload (<1s feedback)
  - 40 new tests covering all scenarios + bypass edge cases
- **Build/Release Garbage Collection** — prevents unbounded DB growth
  - Retain last 10 builds per project, delete older than 30 days
  - Hourly cleanup of builds table + events table
- **Manifest Validation Gate** — defense-in-depth for ArgoCD
  - Rejects Certificate/ClusterIssuer/Issuer CRs in gitops push path
  - Prevents ArgoCD sync blockage from stuck cert-manager resources
  - 4 unit tests

### Changed
- **Language support expanded** — 30 frameworks (was 18)
  - Added: `react`, `vue`, `svelte`, `astro`, `angular`, `fastify` (Node)
  - Added: `sinatra`, `rails`, `ruby` (Ruby)
  - Added: `spring`, `spring-boot`, `java` (Java)
  - Added: `laravel`, `slim`, `php` (PHP)
  - Added: `axum`, `actix`, `rocket`, `rust` (Rust)
  - loopzero-packs generates Dockerfiles for all new languages
  - Language strategies in argo/languages.go for build pipeline
- **Init container migration** — uses API image (not separate migrate image)
  - Migrations always in sync with running code (no ConfigMap desync)
  - `migrate` binary included in API Dockerfile (multi-stage)
- **Leader election** — retry loop (6 attempts, 30s) instead of single try
- **ClusterIssuer** — dual solver: DNS-01 for platform zone, HTTP-01 for custom domains
- **Dockerfile** — `go build ./cmd/` instead of `./cmd/main.go` (includes adapter files)

### Fixed
- **PG failover detection** — TCP keepalive on pgx pool (KeepAlive=15s)
  - Detects dead connections in ~90s (was 2h Linux default)
  - Critical for leader election and advisory lock release
- **Leader heartbeat timeout** — 10s context instead of `context.Background()`
  - Prevents indefinite hang on zombie PG connection
- **Non-leader re-election** — periodic retry (60s) for advisory lock
  - Pods no longer stay non-leader forever after PG recovery
- **Commit status SHA** — uses `build.CommitSHA` (source) not `release.GitCommit` (gitops)
  - Was returning 422 "No commit found" because gitops SHA doesn't exist on GitHub
- **ArgoCD revision mismatch** — retrigger sync instead of silent skip
  - Convergence loop now forces re-sync when revision doesn't match
- **Framework mapping gap** — CLI recognized 17 frameworks, server only 11
  - All CLI-recognized frameworks now mapped server-side
- **CI pipeline** — fixed 33 Go lint issues, 2 CLI lint errors, go mod tidy
- **loopzero-stacks templates** — all 19 templates fixed (removed `commands`, `render: server`, worker ports)

### Security
- **Manifest validation gate** — Certificate CRs blocked from gitops tree
- **Path traversal protection** — SEC-10 in CLI validator
- **GitHub webhook HMAC-SHA256** — validates all incoming events
- **Commit status permissions** — `statuses:write` on GitHub App installation

## [0.12.0] — 2026-04-08

### Added
- **Plan Enforcement** — Starter/Pro/Team limits enforced on projects/builds/environments/members
- **Audit Log** — mutation events written to DB, GET /api/v1/audit endpoint, `loopzero audit` CLI command

## [0.11.0] — 2026-04-06

### Added
- **Cloud Auth System** — full authentication deployed on DigitalOcean
  - Login via GitHub, Google (OIDC), and email/password
  - User registration with email verification (6-digit code via Resend)
  - Ory Kratos session-based auth (cookie, no tokens in browser)
  - Ory Hydra for CLI Device Code Flow (RFC 8628)
  - Dashboard at `cloud.dev.loopzero.example` with profile, logout, verification banner
  - Verification page auto-submits code from email link
- **Cloud Service** (`cloud/`) — Go service for OAuth2 consent, billing, metering
  - Device Code Flow endpoints (`/v1/auth/device/*`)
  - User info endpoint (`/v1/auth/me`) with JWT claims from Kratos identity
  - Stripe billing integration (webhook, subscriptions, plan limits)
  - Consent handler for Hydra login/consent flows
- **Dashboard** (`cloud/dashboard/`) — React SPA (Vite + Tailwind)
  - Login, register, verification, profile pages
  - Overview, projects, environments, team, billing, settings pages
  - Session-based auth guard (redirects to login when unauthenticated)
  - Verification banner for unverified email users
- **Email delivery via Resend HTTP API** — Kratos HTTP courier
  - DigitalOcean blocks SMTP ports; uses `delivery_strategy: http`
  - Jsonnet template maps `ctx.recipient/subject/html_body` to Resend API
  - Requires `--watch-courier` flag on Kratos
- **Security hardening** (MEET-033)
  - Password: min 10 chars + HaveIBeenPwned breach check
  - Cloudflare WAF rate-limiting: 10 login/min, 5 register/min per IP
  - Account enumeration mitigation (`security.account_enumeration.mitigate: true`)
- **Content hash deploy** — deploy without git (MEET-028)
  - Safe tar extraction (Go `archive/tar`, no path traversal/symlinks)
  - 30 new tests for content hash functions
- **ArgoCD SYNC_FAILED fix** — 7 root causes resolved (MEET-029)
  - Callback auth header, leader election hardening, timeout hierarchy
  - TriggerSync 404 recovery, convergence loop revision check
- **CI/CD pipeline chain** — push → CI → Build Images → Infra Deploy
  - Wait-for-CI polling in Build Images
  - Wait-for-images polling in Infra Deploy
  - cloud-api + dashboard added to build pipeline
- **API Gateway** — Traefik path-based routing under `cloud.dev.loopzero.example`
  - `/self-service/*` → Kratos, `/oauth2/*` → Hydra, `/auth/*` → cloud service
  - CORS middleware on loopzero-api for cross-origin dashboard calls

### Changed
- `COMMIT` → `VERSION` in CLI build list/inspect output
- Deploy error messages use product language (no internal terms)
- `ReleasePendingTimeout`: 5min → 10min (reaper/convergence race fix)

### Fixed
- Content hash: `.git` prefix bug excluded `.github/`, `.gitignore`
- `SourceStore` collapsed from 2 methods to 1 (`PushResult`)
- `getGitRoot()` stderr leak in non-git directories
- Dashboard auth: in-memory tokens → sessionStorage → Kratos sessions
- Kratos `login.after` hooks removed (invalid in v26, caused CrashLoopBackOff)

### Security
- Safe tar extraction: rejects symlinks, hardlinks, path traversal, decompression bombs
- Sensitive file detection for non-git projects
- ArgoCD callback Authorization header (was missing — 401 on every callback)
- Leader election: stale lock detection + 60s heartbeat + FATAL on loss
- `loopzero_api_leader_status` Prometheus gauge metric

### Infrastructure
- Cloudflare Pro plan for WAF rate-limiting
- DNS A records: `cloud.dev.loopzero.example`, `auth.dev.loopzero.example`
- Ory Kratos + Hydra deployed on mgmt cluster
- Resend SMTP secret in GitHub Actions (`LOOPZERO_RESEND_API_KEY`)
- OAuth secrets: `LOOPZERO_GITHUB_CLIENT_ID/SECRET`, `LOOPZERO_GOOGLE_CLIENT_ID/SECRET`

### Meetings
- MEET-028: Content hash security rework
- MEET-029: ArgoCD SYNC_FAILED definitive fix (7 root causes)
- MEET-030: Production readiness final push plan
- MEET-031: Email flows implementation
- MEET-032: Login redirect loop architecture fix
- MEET-033: Password hardening + Cloudflare WAF
- MEET-034: Kratos URL consistency fix
- MEET-035: SMTP NetworkPolicy (superseded by MEET-036)
- MEET-036: Resend HTTP API for email delivery

### Added
- CLI command `loopzero team list` to list team members with roles (#cmd-team)
  - New API routes `GET/POST/DELETE/PATCH /api/v1/teams/members`
  - TeamStore with ListMembers, AddMember, RemoveMember, UpdateMemberRole, GetUserByUsername
  - Authorization: owner/admin can add/remove, owner-only for role changes, owner cannot be removed
  - GitHub username resolution (strips `github-` prefix in all output)
- CLI command `loopzero team add <username> [--role]` to invite team members (#cmd-team)
- CLI command `loopzero team remove <username>` with owner protection (#cmd-team)
- CLI command `loopzero team role <username> <role>` for owner-only role changes (#cmd-team)
- CLI command `loopzero env list` to list environments for the current team (#cmd-env)
  - New API route `GET /api/v1/environments` with tenant isolation
  - New API route `POST /api/v1/environments` to create environments (patches Tenant CRD)
  - New API route `GET /api/v1/environments/{name}` for environment details
  - New API route `DELETE /api/v1/environments/{name}` with production protection (403)
- CLI command `loopzero env create <name>` with reserved name validation (#cmd-env)
- CLI command `loopzero env delete <name> --confirm <name>` with safety guards (#cmd-env)
- CLI command `loopzero env inspect <name>` showing active releases per project (#cmd-env)
- CLI command `loopzero project list` to list all projects in the current team (#cmd-project)
  - New API route `GET /api/v1/projects` with tenant isolation
  - New DB query `ListByTenant` returning projects ordered by name
- CLI command `loopzero project create <name>` to create a project manually (#cmd-project)
  - Handles 409 (already exists) as idempotent success
- CLI command `loopzero project inspect <name>` to show project details (#cmd-project)
  - New API route `GET /api/v1/projects/{name}` with 404 handling
- CLI command `loopzero build list [--env] [--limit]` to list recent builds for a project (#cmd-build)
  - New API route `GET /api/v1/projects/{name}/builds` with env filter and pagination
  - New DB query `ListByProjectAndEnv` with total count
  - Expanded `BuildStatusResponse` with timestamps, duration, error, apps
- CLI command `loopzero build inspect <build-id>` to show detailed build info (#cmd-build)
- CLI command `loopzero cancel <build-id>` to cancel running or queued builds (#cmd-cancel)
  - Exit codes: 0 success, 1 usage, 3 cancel failed, 4 auth
  - `--json` output: `{ ok, build_id, status }` on success, `{ code, error, exit_code }` on error
  - Rule 3 compliant: zero internal terms in any output
  - 13 unit tests covering all error cases + Rule 3 compliance
- Cell-Based Architecture Phase 0 implementation (#ADR-0008)
  - Migration 000011: `cell_id` column on tenants table (DEFAULT 'cell-default', indexed)
  - `CellID` field added to Tenant CRD spec (+kubebuilder:default="cell-default")
  - PlacementEngine interface + DefaultEngine (returns "cell-default" for all tenants)
  - CellRouter interface + PassthroughRouter (resolves any cell_id to current cluster config)
  - BootstrapHandler wired with PlacementEngine for cell assignment on tenant creation
  - CellMetricsCollector emits `loopzero_cell_tenant_count` gauge per cell_id (60s interval)
  - PostgresCellStore with `CellTenantCounts()` query
  - 9 unit tests for placement package (Engine + Router + interface compliance)
  - ADR-0008 approved with 5-phase roadmap (Phase 0-4, overview + per-phase plans)
  - Supersedes ADR-0007 (ArgoCD Application model)
- Cell Architecture Phase 1 — multi-cell foundation code (#ADR-0008)
  - ClientManager: lazy-init cached K8s clients per Cell (api/internal/cellmgr/)
  - SyncClientFactory: per-cell ArgoCD sync client resolution (api/internal/argocd/)
  - StaticRouter: resolves cell_id from configured CellConfig list
  - RoundRobinEngine: bin-packing placement with maxPerCell capacity (500 default)
  - TenantCellQuerier interface + CountByCell/GetTenantCellID in PostgresCellStore
  - ClusterCredential CRD CellID field (+kubebuilder:validation:Pattern)
  - ApplicationSpec.CellID passed from TenantController to ArgoCD provisioner
  - CF KV PutCellRouting/DeleteCellRouting for multi-cell DNS routing
  - Cell Provisioner Pulumi interfaces (CellConfig/CellOutput/createCell scaffold)
  - Cell Health Grafana dashboard (6 panels: tenant count, capacity, pods, sync latency, failures, convergence)
  - 28 new unit tests across placement, cellmgr, argocd packages
- Database Hardening Phase 1 — MEET-2026-03-31-024 (#MEET-024)
  - Migration 000009: Role separation (loopzero_api, loopzero_migrate, loopzero_readonly)
  - Migration 000009: Row-Level Security on all tenant-scoped tables (builds, releases,
    secrets, projects, environment_targets, tenant_members, audit_log, idempotency_keys)
  - Migration 000009: CHECK constraints on builds.status and releases.status columns
  - Migration 000009: Missing indexes (idx_releases_build_id, idx_releases_pending)
  - Migration 000009: Deferred audit triggers on secrets and tenant_members (COMMIT-time)
  - Migration 000009: Append-only enforcement trigger on audit_log table
  - Migration 000010: Pre-written emergency RLS disable (no-op, uncomment if needed)
  - SET LOCAL app.tenant_id via set_config() inside WithEnvironmentLock for RLS enforcement
  - Application-level audit logging (AuditLogger interface + AuditAdapter for secret/project ops)
  - Database health PrometheusRules (pool exhaustion, slow queries, long transactions, RLS denial)
  - Database Health Grafana dashboard
  - Runbooks: pool-exhaustion, slow-queries, rls-disable, credential-rotation

### Fixed
- SetGitCommit failure in container_delivery now fatal — prevents wrong-version activation
  when SyncCallbackActivator revision guard is bypassed by empty GitCommit (#MEET-024 BUG-03)
- syncRetriggers memory leak in ReleaseConvergenceLoop — cleanup on activate and fail (#MEET-024 BUG-04)
- Idempotency key lookup now scoped by tenant_id — prevents cross-tenant response replay (#MEET-024 BUG-01)
- Secret store Set() race condition: replaced SELECT+INSERT with atomic UPSERT using xmax (#MEET-024 BUG-02)
- DSN builder default sslmode changed from "disable" to "require" (#MEET-024 H3)

### Security
- Row-Level Security enforces tenant isolation at database level — defense-in-depth (#MEET-024 C2)
- Role separation: API connects as loopzero_api (DML only), no DDL access (#MEET-024 C1)
- Redundant idempotency_keys index dropped (key is already PK) (#MEET-024 DB-05)

- Cross-validation remediation plan and implementation (#MEET-023)
  - Meeting record with 6 specialist agents (Architect, DistSys, SRE, GitOps, API Domain, Verification)
  - Executable plan at docs/plans/cross-validation-remediation.md
  - ADR-0007: ArgoCD Application model evaluation (per-project vs notification scoping)
  - Leader election guard via pg_try_advisory_lock — prevents duplicate background loops on multi-replica
  - API scaling runbook at docs/runbooks/api-scaling.md
  - 7 new Prometheus metrics: drift_detected, release_state_divergence, releases_pending,
    reconcile_loop_last_success_timestamp, gitops_operation_duration, release_convergence_duration,
    releasing_builds (was registered, now has PrometheusRule)
  - 3 new PrometheusRules: LoopZeroOrphanBuild, LoopZeroReleasesPendingStuck, LoopZeroReconcileLoopStale
  - Post-activation health monitoring for ACTIVE releases (convergence loop extension)
  - Config cross-field validation: DBMaxConns >= DBMinConns, timeout coherence checks
  - GoGitGitOpsPusher: alternative GitOps pusher using go-git library (no git CLI subprocess),
    with exponential backoff, SEC-1 credential handling, and 7 unit tests (#MEET-023 T2-12)

### Fixed
- Idempotency guard in ActivateReleaseKernel prevents panic when SyncCallbackActivator and
  ReleaseConvergenceLoop race on the same release (#MEET-023 T0-1)
- Phantom alert LoopZeroDriftDetected: metric loopzero_drift_detected was referenced in PrometheusRule
  but never emitted — now registered and wired to health monitor (#MEET-023 T0-2)
- Convergence loop now retriggers TriggerSync for PENDING releases stuck > 30s with no active
  ArgoCD sync operation — prevents silent timeout with manual sync policy (#MEET-023 T1-5)
- Drain-on-shutdown: WaitGroup tracks all 6 background goroutines, waits up to 30s before
  closing DB pool on SIGTERM (#MEET-023 T1-7)

- Workload protection Phase 1 + Phase 2: defense against broken/unhealthy container images (#MEET-021)
  - Startup probe on all server/frontend apps (150s max startup, 5s period, 3s timeout)
  - Readiness probe with thresholds (10s period, 3 failures before removal from Service)
  - Liveness probe with thresholds (15s period, 5 failures — more tolerant than readiness)
  - progressDeadlineSeconds: 210s (fail-fast broken deploys, 40s margin over startup)
  - minReadySeconds: 10s (stabilization window for quick crashes)
  - Rolling update strategy: maxUnavailable=0, maxSurge=1 (zero-downtime deploys)
  - PodDisruptionBudget for ALL tenant workloads (server + worker + frontend)
  - unhealthyPodEvictionPolicy: AlwaysAllow (broken pods never block node drain)
  - terminationGracePeriodSeconds: 30s (explicit graceful shutdown contract)
  - 6 new PrometheusRules: CrashLoop (5m), ImagePullFailed, ReplicaMismatch (5m),
    HighRestartRate, DeployStuck (ProgressDeadlineExceeded), CrashLoopCritical (30m)
  - 4 ValidatingAdmissionPolicies (K8s native, no Kyverno): restrict registries,
    disallow :latest, require resource limits, protect platform NetworkPolicies
  - Build retry skips OOM (exit 137) via retryStrategy.expression (Argo Workflows native)
- Distributed tracing with Grafana Tempo + OpenTelemetry SDK for deploy flow troubleshooting (#MEET-020)
  - Grafana Tempo as trace backend (standalone, 7d retention, S3 in prod)
  - OTel Collector reconfigured: traces exported to Tempo (was debug/discard)
  - API instrumented with 6 sync span points: build.submit, source.upload, source.push_git, build.workflow_submit, lock.acquire, gitops.push_manifests
  - HTTP middleware auto-spans with otelhttp (trace context propagation)
  - Grafana Tempo datasource with trace-to-log correlation
  - PrometheusRules: LoopZeroTempoDown, LoopZeroTempoStorageHigh, LoopZeroOTelCollectorDown, LoopZeroCollectorExportErrors
  - ServiceMonitor for Tempo metrics scraping
  - Feature flag `LOOPZERO_TRACING_ENABLED` (default true dev, false prod)
  - Graceful degradation: API works without Collector (export timeout 1s, bounded queue)

### Security
- Replace full admin kubeconfigs with scoped ServiceAccount tokens — API and controller now get minimal RBAC per cluster instead of cluster-admin (#CRIT-02)
- Replace ESO admin token with scoped ServiceAccount — read-only access to Secrets in platform-system only (#HIGH-04)
- Fix CF Tunnel secret non-deterministic — was using `crypto.randomBytes()` at program time, causing tunnel recreation on every `pulumi up`. Now reads from Pulumi config (#CRIT-03)
- Fix hardcoded passwords fail-fast — production deploy now fails immediately if `giteaAdminPassword` or `minioRootPassword` not set (#CRIT-01)
- Fix tunnel token exposed in pod args — moved to K8s Secret with env var injection (#HIGH-03)
- Fix Gitea token scope from `all` to minimal `write:repository,write:organization,read:user` (#HIGH-05)
- Fix ArgoCD placeholder token was literal admin password — changed to invalid placeholder (#HIGH-09)
- Fix cloudflared Deployment missing securityContext, probes, and non-root enforcement (#MED-11)
- Fix PostgreSQL NetworkPolicy allowing all egress — restricted to metrics and replication ports (#MED-09)
- Disable Grafana anonymous Viewer access in production (#MED-08)
- Fix parseKubeconfig returning empty strings silently — now throws on missing fields (#MED-04)
- Fix ArgoCD token generation failure was WARN-only — bootstrap Job now fails on token errors (#MED-05)
- Fix shell injection via `app.Name` in `loopzeroPacksDockerfileStep` — added `ValidateAppName` with RFC-1123 allowlist + `%q` shell quoting as defense-in-depth (#CODE-001)
- Fix unvalidated build-arg keys flowing into `buildctl --opt` — validates env key format, skips invalid keys (#SEC-002)

### Fixed
- Fix `GetStatus` swallowing all K8s API errors — now distinguishes `IsNotFound` from transient errors; prevents mass build failures on K8s API blips (#BRC-006)
- Add panic recovery to reconcile loop goroutines — prevents single corrupted build from crashing entire API process (#BRC-004)
- Fix `NormalizeApp` failure leaving build QUEUED for 5 minutes — now calls `failQueuedBuild` immediately (#BRC-001)
- Fix Go/Python cache dirs pointing to `/root/.cache` while containers run as uid 1000 — caches now use `/workspace/.cache/` (#CODE-005)
- Fix static asset manifest truncated at 100 files — removed `head -100` limit (#CODE-003)
- Fix asset manifest JSON corruption on filenames with special characters — proper escaping (#CODE-006)
- Pin `imageLoopZeroPacks` from `:latest` to `v0.1.0` — deterministic builds (#COMP-004)

### Removed
- Remove dead compute provider (`IProviderCompute`, `DigitalOceanComputeProvider`, `contracts/compute.ts`) — zero call sites (#MED-02)

### Changed
- E2E failure scenario tests and chaos tests changed from Skip() to Pending (PIt) — shows yellow instead of false green (#HIGH-07)
- Container registry creation uses factory abstraction instead of direct DO import (#MED-01)
- API and controller image references use `infra.registryEndpoint` instead of hardcoded constant (#HIGH-08)
- Traefik Helm release name uses `${role}-traefik` instead of hardcoded `mgmt-traefik` (#MED-06)
- Bootstrap Job run-id uses deterministic `env-version` instead of `Date.toISOString()` (#MED-07)
- `provision-api-tokens.sh` Gitea username aligned to `gitea_admin` and namespace parameterized (#HIGH-06, #MED-12)
- Centralize DO Spaces endpoint in `InfraOutputs.spacesEndpoint` — replaces 5 inline constructions (#MED-10)
- Pipeline Step 2 reads REGISTRY from Pulumi output instead of hardcoded constant (#PIPELINE-01)
- CF Worker deploy step now fails the pipeline on error instead of silently continuing (#PIPELINE-02)

### Added
- PodDisruptionBudget for LoopZero API (`minAvailable: 1`) — prevents both pods being evicted during drain (#HIGH-02)
- NetworkPolicies for production (platform-system namespace) — default deny ingress + per-component rules (#HIGH-01)
- Auto-generate `tunnelSecret` in Pulumi config on first deploy — no manual pre-setup needed (#PIPELINE-03)
- Create `loopzero-logs` Spaces bucket via Pulumi — Loki log storage bucket was missing (#PIPELINE-04)
- Import `loopzero-logs` bucket in pipeline pre-step (survives destroy cycles) (#PIPELINE-04)
- Build `loopzero-packs-runner` image in infra-deploy Step 2 — was only in build-images.yml, missing from destroy+recreate (#PIPELINE-05)
- Update CWTs + BuildKit registry credentials in infra-deploy Step 2 — fully self-contained pipeline (#PIPELINE-06)
- Security scan is now a build gate — generate-dockerfile depends on security-scan completion (#CODE-008)
- Python security scan now runs after install-deps, not in parallel — pip-audit scans installed packages (#CODE-002)
- cwt-clone.yaml uses minio/mc base image instead of runtime wget — eliminates supply chain risk (#SEC-004)
- Extract `naming.ImageTag()` — single source of truth for image tag format, replaces 4 duplicate sites (#ARCH-001)
- Add `DispatchingDeliveryActivator` — selects container vs static delivery based on app type (#ARCH-009)
- Add activation retry limit (max 3 attempts) — fails build immediately instead of 10-minute reaper wait (#BRC-003)
- Fix duplicate `deploy.sync_failed` SSE events — `ErrActivationAlreadyHandled` sentinel prevents double emit (#BRC-002)
- ArgoCD sync client supports CA cert file for proper TLS verification — `CACertPath` config replaces `InsecureSkipVerify` (#SEC-T02b)

### Added
- CI build job for loopzero-packs-runner — was missing, absence breaks all deploys (#COMP-003)
- Build pod NetworkPolicy — default-deny egress with allowlist for DNS, MinIO, registry, packages (#SEC-009)
- Document ParseArgoAppName limitation for hyphenated env names, TODO for Sprint 5 labels (#ARCH-002)
- Unit tests for StaticDeliveryActivator — 3 tests covering nil KV client, release creation, SSE events (#COMP-007)

### Removed
- Remove 3 deprecated `scriptDockerfile*` functions — dead code with `//nolint:unused` (#COMP-005)
- Remove `cwt-summary.yaml` — ClusterWorkflowTemplate deployed but never called by WorkflowBuilder (#COMP-006)

### Previously Fixed
- ArgoCD notifications config moved from `configs:` to root level in Helm values — was being silently ignored by argo-cd chart (#MEET-015-IaC)
- Pulumi `installArgoCD()` now includes full config: RBAC, accounts, notifications, metrics, security contexts — was deploying with minimal inline values (#MEET-015-IaC)
- Gitea bootstrap Job uses `deleteBeforeReplace` + `replaceOnChanges` — fixes `pulumi up` failure when TTL garbage-collects completed Job (#MEET-015-IaC)
- Bootstrap Job image pinned from `bitnami/kubectl:latest` to `bitnami/kubectl:1.31` — deterministic builds (#MEET-015-IaC)
- Loki cross-cluster URL resolved by Pulumi via `@pulumi/command` — eliminates post-deploy `kubectl patch` on ConfigMap (#MEET-015-IaC)
- `project delete` now removes GitOps manifests even when no `environment_targets` rows exist — prevents orphaned pods after delete (#F-03)
- Cloudflare API token secret created on ALL clusters with cert-manager, not just mgmt — fixes TLS certificate issuance on runtime cluster (#F-04)

### Added
- Orphan resource protection Phase 1: ArgoCD `controller.operation.processing.timeout: 300`, `SyncClient.TerminateOperation()`, `ErrSyncOperationInProgress` typed error, circuit breaker 409 exclusion, `triggerSyncWithRetry` terminates stuck ops, convergence loop terminates on timeout (#MEET-019)
- Meeting record MEET-2026-03-27-012: ArgoCD deploy reliability architecture decision (#MEET-012)
- Dogfood fix roadmap: 12 tasks across 4 sprints to resolve all findings from 2026-03-27 audit (#DF-ROADMAP)
- `ProjectDeleter` now cancels running Argo Workflows during project deletion via `WorkflowCanceller` (#DF-2.1)
- `CancelActiveBuilds` returns workflow IDs via `RETURNING` clause for Argo cancellation (#DF-2.1)
- CLI deploy renderer handles `deploy.delivering`, `deploy.verifying`, `deploy.live` SSE events — eliminates 5-10 min feedback black hole (#MEET-012-1.5)
- Release Convergence Loop: async verification of PENDING releases via ArgoCD polling, decouples Phase D from build reconcile loop (#MEET-012-1.1)
- `FindPendingReleases` on `ReleaseStore` for convergence loop queries (#MEET-012-1.1)
- Deep health check: `/api/v1/health/deep` now verifies ArgoCD + Gitea connectivity (not just PostgreSQL) (#MEET-012-1.3)
- `HTTPServiceChecker` and `ServiceHealthChecker` interface for pluggable health probes (#MEET-012-1.3)
- `GetSyncRevision` on `SyncClient` returns the git revision ArgoCD synced — prevents silent wrong-version activation (#MEET-012-1.2)
- `CircuitBreakerSyncClient` now delegates `GetAppStatus` and `GetSyncRevision` to inner client — fixes silent skip of health/revision checks in production (#AUDIT-2026-03-28)
- `buildkit-registry-config` secret provisioned by CI pipeline (`build-images.yml`) — fixes build-image pods stuck in PodInitializing on DO (#AUDIT-2026-03-28)
- Wire `StatusHandler` into API server — `GET /api/v1/projects/{name}/status` now available (#DOGFOOD-F05)
- `StatusBuildStoreAdapter` and `StatusReleaseStoreAdapter` in adapters to bridge DB stores to status handler (#DOGFOOD-F05)
- Build cluster secrets patching in `update-cross-cluster-ips.sh` — `loopzero-gitea-credentials` and `loopzero-minio-credentials` now auto-updated (#DOGFOOD-F01)
- Wire `ProjectDeleter` into API server — `DELETE /api/v1/projects/{name}` now functional with full orchestration (#DOGFOOD-F03)

### Fixed
- ArgoCD sync policy contradiction: operator provisioner changed from `selfHeal: true` (auto-sync) to `automated: null` (manual sync), matching spec, Helm template, and tests (#MEET-012-0.1)
- Rollback ArgoCD app name: changed from `ProjectName` to `TenantID`, fixing silent rollback failure on every call (#MEET-012-0.2)
- Rollback sync failure no longer swallowed: returns error instead of logging warning, preventing false success reports (#MEET-012-1.4)
- Rollback legacy path uses `TenantID` instead of `ProjectName` for ArgoCD app name (#MEET-012-1.4)
- Commit SHA verification after ArgoCD sync: `phaseDVerify` checks `status.sync.revision` matches expected commit (#MEET-012-1.2)
- Container image path flattened from 3-level (`{tenant}/{project}/{app}`) to 2-level (`{tenant}/{project}-{app}`) — DO Container Registry rejects nested repos beyond 2 levels (#AUDIT-2026-03-28)
- Build cluster MinIO credentials changed from local endpoint to DO Spaces endpoint in Pulumi bootstrap (#AUDIT-2026-03-28)
- Stale Cloudflare DNS A record (24.144.69.42) for api.dev.loopzero.example removed — was causing intermittent connectivity (#AUDIT-2026-03-28)
- `failRollback` used State constant (`ROLLBACK_FAILED`) instead of Event constant (`RollbackFailed`) — would panic on real PostgreSQL store (#AUDIT-2026-03-28)
- BuildKit insecure registry: generate `buildkitd.toml` with `http = true` for local dev registry, fixing HTTPS→HTTP push error (#DF-1.1b)
- BuildKit entitlement: add `--allow-insecure-entitlement security.insecure` to buildkitd daemon when `RegistryInsecure=true` — unblocks all local builds (#DF-1.1)
- `loopzero logs --build <id>` no longer requires loopzero.yaml — build ID is globally unique and sufficient (#DF-3.1)
- `loopzero logs` error message now suggests where to find the build ID (#DF-3.3)
- Commander.js validation errors (missing args/options) now respect `--json` flag via `exitOverride()` (#DF-3.2)
- Operator crash on DO: graceful fallback when MinIO admin credentials absent — DO Spaces uses SPACES_ACCESS_KEY_ID (#DOGFOOD-DO-01)
- Missing `BASE_DOMAIN` env var in Pulumi tenant-controller deployment (#DOGFOOD-DO-02)
- Build logs endpoint returns 200 with empty entries when Loki is unavailable instead of 500 (#DOGFOOD-F04)
- CLI deploy build failure now includes `build_id` and `suggestion` in JSON error response (#DOGFOOD-F02)
- CLI deploy `--no-wait` human mode now shows `Track with: loopzero logs --build <id>` (#DOGFOOD-F10)
- CLI deploy avoids "Build failed: Build failed" message duplication when API error already contains prefix (#DOGFOOD-F02)
- CLI init `exitWithError` now includes typed `code` field in JSON responses: `AUTH_REQUIRED`, `NOT_GIT_REPO`, `CI_REQUIRES_YES`, `FILE_EXISTS` (#DOGFOOD-F09)

### Changed
- ArgoCD `RealProvisioner.EnsureAppProject` and `EnsureApplication` now distinguish NotFound from transient errors (#OPERATORS-REVIEW)
- `TenantReconciler.runSteps` re-fetches resource before status update to avoid stale resourceVersion conflicts (#OPERATORS-REVIEW)
- `EnvironmentReconciler` step runner applies same re-fetch pattern on error path (#OPERATORS-REVIEW)
- MinIO `EnsureTenantCredentials` checks if user exists before generating new secret key, preventing credential rotation on every reconcile (#OPERATORS-REVIEW)
- `ExposureController` uses 5-minute probe interval when route is Reachable (was 30s always), reducing reconcile load by ~10x (#OPERATORS-REVIEW)
- Internal HTTP clients (ArgoCD, Gitea, API) now include `InsecureSkipVerify` for in-cluster self-signed TLS (#OPERATORS-REVIEW)
- `ensureRepoCredentialSecret` uses `CreateOrUpdate` instead of Get+Create, enabling credential rotation (#OPERATORS-REVIEW)
- Extracted `BuildCredentials` struct from `TenantReconciler` — credential management separated from reconciliation logic (#OPERATORS-REVIEW)
- Docker config.json from DO API now validated for `auths` + `auth` field structure (#OPERATORS-REVIEW)
- Extract image version constants (`imageNode`, `imageGolang`, `imagePython`, `imageAlpine`, `imageBuildKit`) — single source of truth for build images (#FORGE-REVIEW)
- Replace manual `dockerfileDir`/`dockerfileName` with `filepath.Dir`/`filepath.Base` from stdlib (#FORGE-REVIEW)
- Eliminate 90% code duplication between `buildkitTemplateForApp` and `buildkitTemplateFromPlan` via shared `buildkitContainer` function (#FORGE-REVIEW)
- Sort map iteration in `paramsToForge` and BuildKit `buildArgs` for deterministic workflow YAML (#FORGE-REVIEW)
- Add `slog.Warn` when `ResolveStrategy` falls back to custom strategy for unrecognized frameworks (#FORGE-REVIEW)

### Removed
- Dead condition types: `TypeNamespacesReady`, `TypeRBACReady`, `TypeNetworkPolicyReady`, `TypePlatformSecretsReady` (#OPERATORS-REVIEW)
- Dead reason constants: `ReasonTimeout`, `ReasonInProgress`, `ReasonCreated`, `ReasonDeleting` (#OPERATORS-REVIEW)
- `internal/naming/` proxy package — controllers import `pkg/naming` directly (#OPERATORS-REVIEW)
- Condition re-exports from `api/v1/groupversion_info.go` (`ConditionX` constants and `SetCondition` var) (#OPERATORS-REVIEW)
- `giteaBootstrapClient` type alias — replaced with direct `gitea.BootstrapClient` usage (#OPERATORS-REVIEW)
- Package-level `doHTTPClient` global — replaced by `BuildCredentials.HTTPClient` field (#OPERATORS-REVIEW)

### Added
- Cloudflare Worker for static frontend delivery — serves assets from DO Spaces via CF edge, zero pods for static apps (#SPRINT-10)
- `cfkv` package — Cloudflare Workers KV client for routing table writes (#SPRINT-10)
- Automatic KV routing entry write on build completion for `render: static` apps (#SPRINT-10)
- DO Spaces CDN endpoint + public-read ACL for static assets (#SPRINT-10)
- Security headers (CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy) in CF Worker responses (#SPRINT-10)
- SPA fallback routing in CF Worker — deep links serve index.html (#SPRINT-10)
- `CF_API_TOKEN`, `CF_ACCOUNT_ID`, `CF_KV_NAMESPACE_ID` config vars for API (#SPRINT-10)
- BuildKit build engine as alternative to Kaniko — sidecar pattern with rootless `buildkitd` + `buildctl`, OCI registry cache (`--import-cache`/`--export-cache mode=max`), digest via `--metadata-file` JSON parsing (#BUILDKIT-MIGRATION)
- `UseKaniko` feature flag (`USE_KANIKO` env var, default `true`) — allows gradual migration and instant rollback to Kaniko during transition (#BUILDKIT-MIGRATION)
- `buildkit-registry-config` Secret in TenantController — registry credentials for BuildKit push, coexists with Kaniko's `docker-config` during migration (#BUILDKIT-MIGRATION)
- `ParseBuildKitDigest()` — deterministic digest extraction from BuildKit metadata JSON with strict `sha256:` prefix validation (#BUILDKIT-MIGRATION)
- `loopzero deploy --no-cache` flag — forces full rebuild without BuildKit layer cache, useful for debugging stale cache issues (#BUILDKIT-MIGRATION)
- 24 unit tests covering BuildKit template generation, sidecar pattern, security context, resource limits, cache flags, no-cache, multi-app isolation, digest parsing, DOKS compatibility (seccomp, apparmor, rootlesskit) (#BUILDKIT-MIGRATION)

### Removed
- Kaniko build engine — completely replaced by BuildKit with rootless sidecar pattern (#BUILDKIT-MIGRATION)
- `kanikoTemplateForApp()` function and all Kaniko container template generation code (#BUILDKIT-MIGRATION)
- `UseKaniko` feature flag from `InfraParams`, `Config`, `WorkflowSubmitterConfig`, and `USE_KANIKO` env var (#BUILDKIT-MIGRATION)
- `docker-config` Secret from TenantController — replaced by `buildkit-registry-config` (#BUILDKIT-MIGRATION)
- `docker-config` volume from Workflow CR — replaced by `buildkit-registry-config` + `buildkit-socket` volumes (#BUILDKIT-MIGRATION)
- `kaniko:` configuration section from Helm values.yaml (#BUILDKIT-MIGRATION)
- Apache License 2.0 — LICENSE file, SPDX headers on all 295 source files (.go + .ts), license field in package.json (#CNCF-PREP)
- SECURITY.md — vulnerability disclosure policy with severity-based response timelines (#CNCF-PREP)
- CONTRIBUTING.md — development setup, PR checklist, coding standards, DCO sign-off (#CNCF-PREP)
- CODE_OF_CONDUCT.md — adopted CNCF Code of Conduct v1.3 (#CNCF-PREP)
- GOVERNANCE.md — roles (contributor/reviewer/maintainer), decision process, dispute resolution (#CNCF-PREP)
- MAINTAINERS.md — maintainer list with organizational affiliation (#CNCF-PREP)
- RELEASES.md — release process, versioning strategy, artifact list, support policy (#CNCF-PREP)
- .github/CODEOWNERS — automated review assignment per component (#CNCF-PREP)
- Grafana Loki + Promtail infrastructure for build log aggregation — Loki (single binary, MinIO S3 backend) on mgmt cluster, Promtail (DaemonSet) on build cluster collecting Argo Workflows pod logs (#SPRINT-6A)
- `GET /api/v1/builds/{id}/logs` endpoint — queries Loki via LogQL with `?tail=N` and `?step=<name>` filtering, tenant isolation enforced (#SPRINT-6A)
- `loopzero logs --build <id>` CLI command — displays build output with step-prefixed lines, `--json` output, `--tail`, `--step`, `--follow` flags (#SPRINT-6A)
- Pod retention policy (4h) on Argo Workflows — gives Promtail time to ship logs before pod GC (#SPRINT-6A)
- Loki data source auto-configured in Grafana — build logs queryable via Grafana Explore (#SPRINT-6A)
- `POST /api/v1/projects/{name}/rollback` endpoint — rollback orchestrator with environment lock, release state machine reactivation, gitops manifest restore from git history (#SPRINT-6B)
- `loopzero rollback` CLI command — reverts to previous release with `--to <id>`, `--env`, `--json`, `--yes` flags, migration warning displayed (#SPRINT-6B)
- Release state machine: SUPERSEDED/ROLLED_BACK → ACTIVE reactivation via `Reactivated` event (#SPRINT-6B)
- ReleaseStore: GetPreviousRelease, GetByIDForTenant, ListByEnv methods for rollback target resolution (#SPRINT-6B)
- GitOps reader: GetManifestsAtCommit fetches manifest YAML from Gitea git history at specific commit (#SPRINT-6B)
- `POST /api/v1/secrets` endpoint — create/update secrets with name validation (uppercase), value never in response (#SPRINT-6C)
- `DELETE /api/v1/secrets/{name}` endpoint — remove secrets with tenant isolation (#SPRINT-6C)
- `loopzero secret set/list/delete` CLI commands — manage secrets via CLI, value never visible in output (#SPRINT-6C)

### Removed
- LoopZeroBuild CRD and BuildReconciler — build state owned exclusively by PostgreSQL (ADR-0006)
- LoopZeroRelease CRD and ReleaseReconciler — release state owned exclusively by PostgreSQL (ADR-0006)

### Fixed
- Tenant CRD creation now fails loudly with HTTP 503 instead of silent log — prevents deploys without infrastructure (ADR-0006)

### Added
- LoopZeroApplication CRD + ApplicationController — auto-assigns hostname via `naming.Hostname()`, creates ExposedRoute CR with ownerReference, tracks phase (Progressing/Serving/Reachable/Degraded/Down), sets conditions (ServiceResolved, IngressReady, CertificateIssued, RouteReachable, ApplicationHealthy) (#MEET-2026-03-24-004)
- ExposedRoute CRD + ExposureController — manages DNS/TLS exposure with cert-manager secret detection, HTTP reachability probes (30s cadence), consecutive failure tracking (Failed after 5), phase lifecycle (Pending/CertIssued/Reachable/Failed) (#MEET-2026-03-24-004)
- Internal `POST /internal/bootstrap` endpoint — seeds admin user, system tenant, and membership records for platform bootstrap. Idempotent via ON CONFLICT DO NOTHING (#MEET-2026-03-24-001)
- Per-tenant MinIO credential isolation — TenantController now provisions scoped MinIO users with bucket policies restricting access to `loopzero-builds/{tenantID}/*` only (#MULTI-CLUSTER-MINIO)
- Phase 3 (exposePlatform) — creates DNS records for management subdomains (api, argocd, grafana) and runtime wildcard (*.{domain}) (#PULUMI-PHASE3)
- DNS record type auto-detection (A for IPv4, AAAA for IPv6, CNAME for hostnames) in DigitalOcean provider (#PULUMI-DNS-FIX)
- TLS via Let's Encrypt — ClusterIssuer (ACME HTTP-01) + Certificate + IngressRoute for ArgoCD, Gitea, Grafana, API on mgmt cluster. Auto-provisioned in Phase 2 bootstrap (#PULUMI-TLS)
- Gitea moved from dedicated VM to mgmt cluster — StatefulSet + PVC (10Gi) + managed PostgreSQL backend. Removes Droplet, adds Helm chart in Phase 2. Accessible at git.{domain} (#PULUMI-GITEA-K8S)
- Unit test suite for infra/pulumi — 32 tests covering naming, tags, errors, node resolution, and DNS detection (#PULUMI-TESTS)
- GitHub Actions pipeline for Pulumi infrastructure: `infra-preview.yml` (PR preview with comment), `infra-deploy.yml` (develop→dev auto, main→prod with approval gate), workflow_dispatch for manual operations (#PULUMI-PIPELINE)
- `test-pulumi` job in CI gate — typecheck + vitest on every push (#PULUMI-PIPELINE)
- Prometheus + Grafana on mgmt cluster — Prometheus (server + kube-state-metrics + node-exporter, 15d retention), Grafana with pre-configured Prometheus datasource, K8s cluster/node/pod dashboards, anonymous viewer access, and execution state tracking (#PULUMI-OBSERVABILITY)
- DigitalOcean Container Registry provisioned via Pulumi (starter tier, 500MB) with execution state tracking and wiring output (#DEPLOY-REGISTRY)
- GitHub Actions workflow `build-images.yml` — builds and pushes loopzero-api and tenant-controller images to DO registry on develop/main push, with automatic DOKS-registry integration (#DEPLOY-IMAGES-CI)

### Changed
- Pulumi state backend changed from Pulumi Cloud to DigitalOcean Spaces (S3-compatible) — eliminates vendor lock-in, keeps state under our control, uses infrastructure we already pay for (#PULUMI-S3-BACKEND)

### Fixed
- TenantController multi-cluster test compilation — suite_test.go and operatortest/setup.go updated to pass buildClient and runtimeClient to SetupTenantController after signature expansion (#MULTI-CLUSTER-TEST-FIX)
- MinIOProvisioner and MinIOEndpoint not propagated in SetupTenantController — fields were declared on TenantReconciler but never wired from config (#MULTI-CLUSTER-MINIO)
- DNS record for `git` subdomain pointing to runtime cluster instead of management cluster — added `git` to `MGMT_SUBDOMAINS` so it resolves to mgmt ingress where Gitea runs (#PULUMI-GIT-DNS)
- Gitea CrashLoopBackOff caused by missing `gitea` database — managed PostgreSQL only creates `defaultdb`, now explicitly provisions `gitea` via `digitalocean.DatabaseDb` (#PULUMI-GITEA-DB)
- ArgoCD and Gitea services unreachable via IngressRoute (HTTP 404) — Pulumi Helm provider adds hash suffix to release names, causing Service name mismatch. Added `fullnameOverride` for deterministic names matching IngressRoute references (#PULUMI-HELM-NAMES)
- DigitalOcean DNS provider hardcoded CNAME type — fails when LB returns IP (common on DO). Now auto-detects A vs CNAME (#PULUMI-DNS-FIX)
- Traefik Service lookup may fail due to Helm chart naming template — added fullnameOverride for deterministic K8s Service names (#PULUMI-TRAEFIK-FIX)

### Changed
- **BREAKING**: `SubmitBuild` workflow submission moved OUTSIDE the advisory lock — lock now only covers build record creation (QUEUED). Workflow submit, status transition to RUNNING, and SSE emission happen after lock release. Reduces lock hold time from ~7s (Argo retry) to <100ms (#REVIEW-BLOCKER-1)
- Build state machine: added `QUEUED + BuildFailed → FAILED` transition — allows immediate failure on workflow submit error instead of waiting 5min queue timeout (#REVIEW-BLOCKER-1)
- `BuildReconcileLoop.reconcile()` changed from blocking `wg.Wait()` to fire-and-forget with non-blocking semaphore — next ticker tick can dispatch new builds even while previous ones wait on ArgoCD sync. Reduces P99 activation latency from 125s to ~5s (#REVIEW-MOD-2)
- **BREAKING**: CWT `loopzero-clone` image changed from `alpine/git:2.43.0` to `minio/mc:RELEASE.2024-01-16T16-06-34Z` — source bundle download now uses authenticated S3 protocol (`mc cp`) instead of unauthenticated HTTP (`wget`). MinIO bucket no longer needs to be public-read. Git installed via apk for bundle unpack (#REVIEW-SEC-MINIO, #REVIEW-MOD-3)
- Python `BuildStep` resources increased from `resourcesSmall` (256Mi limit) to `resourcesMedium` (2Gi limit) — prevents OOMKill when compiling C extensions (numpy, pandas, tensorflow) (#REVIEW-LEVE-1)
- Removed unused `manifests` emptyDir volume (256Mi) from workflow spec and `buildContainer()` mount list — vestige of C-02 migration where manifest generation moved to API (#REVIEW-LEVE-3)
- Argo Workflows controller resources increased from 500m/256Mi to 2CPU/1Gi — sized for 32 workflowWorkers with 50+ concurrent builds generating ~400 pods (#REVIEW-MOD-1)
- Argo Server service changed from NodePort (30081) to ClusterIP — UI gives full admin access, no longer externally reachable; use `kubectl port-forward` (#REVIEW-SEC-ARGO)
- Workflow TTL after success increased from 1h to 4h — enables post-deploy debugging without losing the Workflow CR. Failure TTL unchanged at 24h (#REVIEW-LEVE-2)
- **BREAKING**: Kaniko image destination changed from `{registry}/{tenant}/{project}:{sha}` to `{registry}/{tenant}/{project}/{app}:{sha}` — each app in a monorepo gets its own independent image build via per-app Kaniko DAG tasks. Multi-app Dockerfiles use `Dockerfile.{app}` naming (#REVIEW-MOD-4)
- Per-tenant MinIO credential provisioning via `AdminProvisioner` — TenantController creates MinIO users with S3 policies scoped to `loopzero-builds/{tenantID}/*` only. Eliminates shared credentials between tenants (spec §17.2, §17.4) (#REVIEW-BLOCKER-3)
- **BREAKING**: Mixed-language monorepos now supported — apps are grouped by framework, each language group gets independent validate/install/build/scan/dockerfile DAG steps with language suffix (e.g., `install-deps-node`, `install-deps-go`). Replaces single-strategy `resolveStrategyFromApps` (#REVIEW-AP-3)

### Added
- `operators/internal/minio/provisioner.go` — `CredentialProvisioner` interface + `AdminProvisioner` implementation using MinIO Admin API. Creates per-tenant users with scoped bucket policies. Credentials stored as K8s Secrets in `build-{tenant}` namespace (#REVIEW-BLOCKER-3)
- `CircuitBreakerWorkflowSubmitter` — wraps `WorkflowSubmitter` with circuit breaker for `Submit` operations. Opens after 3 consecutive Argo API failures, closes after 2 recovery probes. `Cancel` and `GetStatus` bypass the breaker (best-effort/read-only). Prevents 70s of wasted retries when build cluster is down (#REVIEW-MOD-5)
- `failQueuedBuild()` method on `BuildOrchestrator` — immediately transitions QUEUED builds to FAILED when workflow submission fails outside the lock, emits SSE `build.failed` event (#REVIEW-BLOCKER-1)
- `build-egress-restrict` NetworkPolicy on per-tenant build namespaces — restricts egress to DNS (53), MinIO (9000), container registry (5000/443), and K8s API (6443). Prevents source code exfiltration via malicious Dockerfiles (spec §17.1 threat model) (#REVIEW-BLOCKER-NETPOL)
- `ReleaseReaper` background goroutine — finds releases stuck in READY (>5min) or PENDING (>ArgoCD timeout) and transitions them to GIT_MIRROR_FAILED or SYNC_FAILED with SSE notification (#PIPELINE-RELEASE-REAPER)
- `ValidateAppPath` function in `api/internal/validation/` — rejects path traversal, absolute paths, and shell metacharacters in app.path from build requests (#PIPELINE-SEC-3)
- `minio.insecure` value in Argo Workflows Helm chart — parametrizes TLS verification for MinIO connections in cache and clone CWTs (#PIPELINE-MOD-6)
- `loopzero project delete <name>` CLI command with `--env`, `--yes`, `--json` flags — removes project resources from K8s via ArgoCD pruning (#PROJECT-DELETE)
- `DELETE /api/v1/projects/{name}` and `DELETE /api/v1/projects/{name}/environments/{env}` API endpoints (#PROJECT-DELETE)
- `ProjectDeleter` service orchestrating project deletion: lock-scoped build cancellation + target deletion, then GitOps removal outside lock (BLOCKER-2 pattern) (#PROJECT-DELETE)
- `GitOpsPusher.Remove` method for idempotent removal of project manifests from tenant GitOps repo (#PROJECT-DELETE)
- `ErrProjectNotFound` typed error with `PROJECT_NOT_FOUND` code (HTTP 404) (#PROJECT-DELETE)
- `ProjectStore.GetByName` and `ProjectStore.DeleteProject` for project lookup and removal (#PROJECT-DELETE)
- `BuildStore.CancelActiveBuilds` for bulk-cancelling QUEUED/RUNNING builds during project deletion (#PROJECT-DELETE)
- `EnvironmentTargetStore.ListByProject` and `DeleteTarget` for environment lifecycle management (#PROJECT-DELETE)

### Fixed
- `ReleaseReaper` implemented but not wired in `main.go` — releases stuck in READY/PENDING past timeout accumulated indefinitely; now started as background goroutine with configurable timeouts via `RELEASE_READY_TIMEOUT`, `RELEASE_PENDING_TIMEOUT`, `RELEASE_REAPER_INTERVAL` env vars (#WIRING-RELEASE-REAPER)
- WorkflowSubmitter integration tests failed when build cluster namespace `build-acme` didn't exist — `skipIfNoBuildCluster` now verifies cluster connectivity with 3s timeout and `ensureNamespace` helper creates per-tenant namespaces before test runs (#TEST-WORKFLOW-NS)
- Build pipeline clone step depended on Gitea (cross-cluster) — replaced with MinIO source bundle download, eliminating Gitea as SPOF for builds (#PIPELINE-BLOCKER-1)
- Workflow `VolumeClaimTemplates` created PVCs that leaked storage under load — replaced with `emptyDir` volumes with `sizeLimit`, auto-cleaned on pod termination (#PIPELINE-BLOCKER-2)
- Dead CWTs (`loopzero-notify`, `loopzero-kaniko`, `loopzero-manifests-generate`, `loopzero-manifests-validate`) still deployed in build cluster — removed (#PIPELINE-BLOCKER-3)
- All tenants shared the `argo` namespace for builds — `WorkflowSubmitter` now uses `build-{tenantID}` namespace, making per-tenant ResourceQuotas effective (#PIPELINE-MOD-2)
- Cache CWTs used `amazon/aws-cli` (~200MB) for MinIO operations — replaced with `minio/mc` (~30MB) for faster image pull on cache hit path (#PIPELINE-MOD-4)
- Cache CWTs had `--no-verify-ssl` hardcoded — now parametrized via `minio.insecure` Helm value (#PIPELINE-MOD-6)
- Failed workflow TTL was 6h — increased to 24h for production debugging (#PIPELINE-LEVE-3)
- `app.Path` from build requests was not validated — path traversal and shell injection now rejected at API boundary (#PIPELINE-SEC-3)
- `loopzero init --name "My Project!@#"` accepted names with special chars that break K8s namespace (RFC 1123) — now ALWAYS sanitizes project name to lowercase/hyphens/max 63 chars, with `sanitized_from` in JSON output (#DOGFOOD-F01)
- `loopzero login` auth failure message gave no guidance on how to get a new token — now includes actionable next step ("Generate a new token at your account settings") (#DOGFOOD-F02)
- CWTs corrected in repo but not applied to running build cluster — applied via `kubectl apply` (#DOGFOOD-F04)
- Old deployments from previous dogfood projects remain in CrashLoopBackOff — confirmed per-Deployment health check correctly ignores degraded apps from other projects; ArgoCD "Degraded" is cosmetic, not blocking. Cleanup requires future `loopzero project delete` feature (#DOGFOOD-F03)
- Kaniko CWT had `--insecure` and `--skip-tls-verify` hardcoded — flags are now conditionally added by WorkflowBuilder only when `RegistryInsecure=true`; CWT is secure by default (#BUILD-BLOCKER-1)
- Gitea token visible in plaintext in Workflow CR parameters — moved to K8s Secret `loopzero-gitea-credentials` via `envFrom`; token no longer appears in `kubectl get wf -o yaml` (#BUILD-BLOCKER-2)
- BuildReconcileLoop processed builds serially — one slow ArgoCD sync (120s) blocked all other builds; now processes up to 10 builds concurrently via semaphore pattern (#BUILD-BLOCKER-3)
- Manifest renderer used `fmt.Sprintf` string concatenation — special chars in values corrupted YAML; rewritten with typed Go structs + `sigs.k8s.io/yaml` serialization (#BUILD-MOD-1)
- Manifest renderer produced non-deterministic YAML from `map[string]string` iteration — `sigs.k8s.io/yaml.Marshal` sorts map keys, eliminating unnecessary GitOps diffs (#BUILD-MOD-5)
- Cache CWTs used `python2.7` (EOL since 2020) for tar operations — replaced with standard `tar` command (#BUILD-MOD-2)
- BuildReaper and ReconcileLoop competed on the same builds — reaper now checks `IsProcessing()` to skip builds under active reconciliation (#BUILD-MOD-6)
- Go Dockerfile generator used generic `go build ./...` — now targets `./cmd/...` or app-specific path (#BUILD-LEVE-1)
- Node.js Dockerfile had insecure fallback `npm install --production` when `npm ci` failed — removed fallback to enforce lockfile integrity (#BUILD-SEC-4)
- IngressRoute used `certResolver: letsencrypt` but infra uses self-signed CA — changed to `tls: {}` to reference default TLSStore (#BUILD-LEVE-2)
- Security scans (npm audit, govulncheck, pip-audit) were warning-only — now exit non-zero on vulnerabilities (#BUILD-LEVE-3)
- Workspace PVC was 2Gi — increased to 5Gi to accommodate large monorepos with node_modules (#BUILD-LEVE-4)
- Clone CWT had no retry strategy — added retry (2 attempts, exponential backoff) for transient network failures (#BUILD-CLONE-RETRY)

### Added
- `loopzero-gitea-credentials` Secret template in Argo Workflows Helm chart — Gitea credentials injected via `envFrom` instead of workflow parameters (#BUILD-BLOCKER-2)
- `gitea.url` and `gitea.token` values in Argo Workflows Helm chart (#BUILD-BLOCKER-2)
- `MaxConcurrent` config for BuildReconcileLoop — bounds concurrent Kubernetes API calls (default 10) (#BUILD-BLOCKER-3)
- `BuildReconcileChecker` interface — allows BuildReaper to coordinate with ReconcileLoop (#BUILD-MOD-6)
- `IsProcessing()` method on BuildReconcileLoop — exposes active build tracking for coordination (#BUILD-MOD-6)
- `RegistryInsecure` field on `WorkflowSubmitterConfig` — controls whether Kaniko uses insecure flags (#BUILD-BLOCKER-1)
- Renderer tests: MOD-1 special chars, MOD-5 determinism, LEVE-2 TLS, sorted env vars, multi-app (#BUILD-MOD-1)
- WorkflowBuilder tests: Kaniko inline verification, insecure flag conditional tests, gitea-token prohibition (#BUILD-BLOCKER-1)

### Changed
- **BREAKING**: Clone CWT now downloads source bundle from MinIO instead of cloning from Gitea — parameters changed from `tenant-id`+`project-id` to `tenant-id`+`build-id` (#PIPELINE-BLOCKER-1)
- **BREAKING**: `WorkflowSubmitter.Cancel()` and `GetStatus()` now accept a `namespace` parameter for per-tenant isolation (#PIPELINE-MOD-2)
- **BREAKING**: Kaniko build step changed from CWT ref (`loopzero-kaniko`) to inline template — WorkflowBuilder now generates container spec with conditional insecure flags (#BUILD-BLOCKER-1)
- **BREAKING**: Clone CWT no longer accepts `gitea-url` and `gitea-token` parameters — credentials come from K8s Secret `loopzero-gitea-credentials` (#BUILD-BLOCKER-2)
- Workflow volumes changed from `VolumeClaimTemplates` (PVC) to `emptyDir` with `sizeLimit` — eliminates storage provisioner dependency and PVC leak (#PIPELINE-BLOCKER-2)
- Cache CWT image changed from `amazon/aws-cli:2.15.17` to `minio/mc:RELEASE.2024-01-16T16-06-34Z` (#PIPELINE-MOD-4)
- `InfraParams` struct: removed `GiteaURL` and `GiteaToken` fields (#BUILD-BLOCKER-2)
- `WorkflowSubmitterConfig` struct: removed `GiteaURL`, `GiteaToken`, and `Namespace`; added `RegistryInsecure` (#BUILD-BLOCKER-1, #BUILD-BLOCKER-2, #PIPELINE-MOD-2)

### Removed
- `cwt-summary.yaml` — `emit-summary` step removed from onExit DAG. The CWT only echoed build metadata to step logs with no consumer. BuildReconcileLoop reads status from Workflow CRD labels, not step outputs (#REVIEW-LEVE-4)
- `secret-gitea-credentials.yaml` — Gitea credentials no longer needed in build cluster; source comes from MinIO (#PIPELINE-BLOCKER-1)
- `cwt-notify.yaml` — HTTP callback replaced by BuildReconcileLoop polling (#PIPELINE-BLOCKER-3)
- `cwt-kaniko.yaml` — replaced by inline Kaniko template in WorkflowBuilder (#PIPELINE-BLOCKER-3)
- `cwt-manifests-generate.yaml` and `cwt-manifests-validate.yaml` — manifest generation moved to API control plane (#PIPELINE-BLOCKER-3)
- `gitea.url` and `gitea.token` from Argo Workflows Helm values — no longer used (#PIPELINE-BLOCKER-1)

### Security
- `Config.Validate()` now rejects `REGISTRY_INSECURE=true` in production — prevents MITM attacks on container registry where Kaniko pushes images over HTTP, rendering supply chain security (cosign, SBOM) useless (#REVIEW-BLOCKER-2)
- Build namespace egress restricted via NetworkPolicy — prevents source code exfiltration, malware download, and internal network scanning from build pods (including Kaniko running as root) (#REVIEW-BLOCKER-NETPOL)
- CWT `loopzero-clone` source bundle download now uses authenticated S3 protocol (`mc cp`) instead of unauthenticated HTTP — MinIO bucket no longer needs to be public-read; any pod without credentials cannot read tenant source code (#REVIEW-SEC-MINIO)
- Per-tenant MinIO credentials with scoped S3 policies — each tenant's build user can only access `loopzero-builds/{tenantID}/*`, preventing cross-tenant source code access and cache poisoning (spec §17.2, §17.4) (#REVIEW-BLOCKER-3)
- App path injection vector closed — `ValidateAppPath` rejects `..`, absolute paths, shell metacharacters at API boundary before they reach workflow shell scripts (#PIPELINE-SEC-3)
- Build cluster no longer needs Gitea credentials — source comes from MinIO, reducing blast radius of credential compromise (#PIPELINE-BLOCKER-1)
- Per-tenant build namespaces enforce ResourceQuota isolation — one tenant cannot exhaust build resources of another (#PIPELINE-MOD-2)
- Gitea admin token no longer exposed in Workflow CR objects — moved to K8s Secret with `envFrom` (#BUILD-SEC-1)
- Kaniko insecure registry flags no longer hardcoded — conditional based on `RegistryInsecure` config; `Config.Validate()` enforces `RegistryInsecure=false` in production (#BUILD-SEC-KANIKO)
- Node.js Dockerfile enforces `npm ci` without insecure fallback — prevents dependency confusion via `npm install` (#BUILD-SEC-4)
- Security vulnerability scans now fail the build on detection — prevents deploying known-vulnerable code (#BUILD-SEC-SCAN)

### Fixed
- `loopzero init` did not detect apps in conventional monorepo directories (`apps/`, `services/`, `microservices/`) without workspace config — now scans nested subdirs for project markers (#dogfood-full2-F01)
- `loopzero init` generated `framework: custom` with no port for projects without a recognized framework — now defaults to port 3000 for server-type apps (#dogfood-full2-F06)
- `loopzero deploy --no-wait` suggested re-deploying as tracking command — message now states deploy is running in background (#dogfood-full2-F03)
- Lockfile warning shown even when lockfile exists in app subdirectory — now checks app paths from loopzero.yaml in addition to project root (#dogfood-full2-F04)
- Build failure error message exposed internal term "Workflow Failed in build cluster" — replaced with product language "Build failed" (#dogfood-full2-F02)
- Startup reconciler exposed internal terms "Workflow" and "build cluster" in error messages — all user-facing messages now use product language (#dogfood-full2-F02b)
- ArgoCD health check was per-Application (shared across all apps in tenant/environment) — one degraded app blocked all deploys. Now checks per-Deployment health for only the apps being deployed (#dogfood-full2-retest-F01)
- Kaniko CWT used `script` template with `/bin/sh` but Kaniko image is distroless — reverted to `container` spec with direct executor args (#dogfood-full3-F01)
- Kaniko CWT retry `maxDuration` was 60s — insufficient for cold-start builds. Increased to 300s with 10s initial backoff (#dogfood-full3-F01)
- Node.js Dockerfile generator set `WORKDIR /app` for all projects — monorepo apps with path `apps/api` couldn't find `package.json`. Now sets `WORKDIR` to the app's subdirectory (#dogfood-full3-F02)
- `loopzero whoami` read from local config without validating token against API — showed "Authenticated" even with invalid/expired tokens. Now calls `/api/v1/auth/whoami` to verify, returns exit 4 if token is rejected (#dogfood-edge-F01)

### Changed
- CLI `scanSubdirectories` now recurses into conventional app directories (`apps/`, `services/`, `microservices/`) to discover nested apps (#dogfood-full2-F01)
- Monorepo flat detection threshold lowered from 2 to 1 workspace — a single app inside `apps/` is correctly identified (#dogfood-full2-F01)
- Build error sanitization added to CLI deploy command — strips internal infrastructure terms from API error messages (#dogfood-full2-F02)
- ArgoCD cluster secret stale IP after k3d recreate — `update-cross-cluster-ips.sh` now patches operator WORKLOAD_CLUSTER_URL, ArgoCD cluster-workload secret, ArgoCD Application destinations, and configures insecure TLS for local dev (#dogfood3-F01)
- ArgoCD cannot clone Gitea repos — NetworkPolicy `allow-gitea` was missing ArgoCD repo-server as allowed ingress source; added cross-namespace selector for `argocd` namespace (#dogfood3-F01b)
- API ERROR logs emitted as plain text instead of structured JSON — `slog.SetDefault()` now sets the JSON handler as global default so all `slog.Error()`/`slog.Warn()` calls produce JSON output (#dogfood3-F04)
- klog (k8s.io/client-go) emitting plain text logs — silenced via `klog.InitFlags()` with `stderrthreshold=FATAL` in API main.go (#dogfood3-F04b)
- TenantController RBAC insufficient — ClusterRole missing permissions for `deployments`, `services`, `configmaps` that `rbacRulesForPlan` grants; Kubernetes RBAC escalation prevention blocked Role creation (#dogfood2-F02)
- TenantController RBAC rules split by API group — `rbacRulesForPlan` used combined `APIGroups: ["", "apps"]` causing cartesian product expansion; split into separate rules per API group (#dogfood2-F02)
- Tenant CR never created — `EnsureTenant` error silently discarded with `_ =`; now logged at ERROR level for observability (#dogfood2-F01)
- API ServiceAccount missing — API pod ran as `default` SA without Tenant CR permissions; added dedicated `loopzero-api` SA with `loopzero.io/tenants` RBAC (#dogfood2-F09)
- API returns generic 500 without logging cause — `mapBuildError` fallback now logs actual error at ERROR level with type info (#dogfood2-F03)
- CLI exit code 0 for unknown commands — now returns exit code 1 with helpful error message (#dogfood2-F04)
- Argo Workflow missing MinIO parameters — `minio-endpoint`, `minio-access-key`, `minio-secret-key` not passed to cache CWT templates (#dogfood2-WF01)
- Argo Workflow missing `manifests` volume — inline templates reference `/manifests` mount but volume not declared in workflow spec (#dogfood2-WF02)
- Cross-cluster IPs stale after cluster recreate — updated mgmt LB and workload LB IPs in api-deployment.yaml (#dogfood2-INFRA01)
- Image path mismatch between Kaniko and manifest generator — reconcile loop used `{registry}/{tenant}/{project}/{app}:{sha}` but Kaniko pushes to `{registry}/{tenant}/{project}:{sha}`; aligned to match (#dogfood2-WF03)
- Source upload intermittent 422 — `openAsBlob()` streaming Blob had unstable file handle during fetch; replaced with synchronous `readFileSync` (#dogfood2-F07)
- ArgoCD repo credential missing Gitea auth — added username/password to repo-creds Secret (#dogfood2-INFRA02)
- ArgoCD Application pointing to mgmt cluster instead of workload — registered workload cluster and fixed destination server (#dogfood2-INFRA03)

### Added
- `task cli:link` — symlinks `loopzero` to PATH for global access (#dogfood2-F08)
- `task cli:build` — builds CLI from TypeScript source (#dogfood2-F08)

### Changed
- WorkflowBuilder migrated from raw `map[string]interface{}` to loopzero-forge SDK typed structs — eliminates ~200 lines of type assertions and gains compile-time safety (#forge-migration)
- LanguageStrategy interface methods now return `*forge.Container` instead of `map[string]interface{}`
- Error message for internal server errors changed from generic "internal server error" to actionable guidance with next steps (#dogfood2-F05)
- TenantController ClusterRole expanded with ArgoCD Application/AppProject permissions (#dogfood2-F02)

### Fixed
- Source upload rejected `loopzero.yaml` sent as file upload (how CLI sends it via FormData) — API now accepts both text field (`FormValue`) and file upload (`FormFile`) (#dogfood-full-F01)
- `loopzero init` without `--name` generated ugly project names from temp directories (e.g., `tmp.DbFESo52xo`) — auto-detected names now sanitized to lowercase with hyphens (#dogfood-full-F02)
- `Idempotency-Key` header was optional on build mutation routes — now required, returns 400 `MISSING_IDEMPOTENCY_KEY` when absent; source upload and project creation correctly excluded (#dogfood-full-F03)
- `loopzero --json` without a command showed plain-text help — now returns structured JSON with error message and available commands list (#dogfood-full-F04)
- Login `--json` error responses missing `code` field — all 7 error paths now include typed error codes (`AUTH_FAILED`, `MISSING_TOKEN`, `SERVICE_UNAVAILABLE`, etc.) (#dogfood-full-F05)
- Server-side errors in deploy shown without context — upload errors now prefixed with "Upload rejected:" and build errors with "Deploy rejected:" to distinguish from local errors (#dogfood-full-F06)
- Flaky `TestEvictCache_ConcurrentAccess_NoDataRace` — concurrent `evictCache` goroutines double-decremented `cacheSize` when deleting the same key; `cache.Delete` replaced with `cache.LoadAndDelete` to only decrement when the entry was actually removed (#dogfood-full-F07)

### Changed
- Source upload and project creation routes no longer share idempotency middleware with build routes — middleware chain split into `rateLimitMiddlewares` (upload, projects) and `idempotentMiddlewares` (builds) (#dogfood-full-F03)

### Changed
- **BREAKING**: HTTP callback route `POST /api/v1/builds/{id}/callback` removed — build completion now detected by BuildReconcileLoop polling Argo Workflow CRDs from the control plane; eliminates cross-cluster HTTP dependency from build plane to control plane (#ARCH-C01)
- **BREAKING**: `notify-api` step removed from Argo Workflow onExit DAG — workflows no longer call the API; build results are read from Workflow CRDs by the reconcile loop (#ARCH-C01)
- **BREAKING**: `generate-manifests` and `validate-manifests` steps removed from Argo Workflow DAG — manifest generation moved to API (control plane) for correct build/release separation (#ARCH-C02)
- Circuit breaker for ArgoCD sync moved from PostgreSQL-backed to in-memory — eliminates write amplification and circular dependency; state is per-replica, which is acceptable for circuit breakers (#ARCH-M02)
- Health probing delegated to ArgoCD instead of direct HTTP probes from control plane to workload cluster — eliminates cross-cluster network dependency; ArgoCD WaitForSync already checks sync+healthy status (#ARCH-A03)
- SSE event polling interval increased from 1s to 5s (fallback) with pg_notify wake-up on event insert — reduces PostgreSQL QPS from ~200 (one per stream) to ~N (one per actual event) (#ARCH-A02)
- Build Reaper now verifies Argo Workflow state before marking builds as FAILED — prevents false positives when a legitimately slow build is still running in the build cluster (#ARCH-M03)
- `LOOPZERO_ARGO_CALLBACK_SECRET` env var no longer required — shared secret for build callback authentication eliminated (#ARCH-C01)

### Added
- `BuildReconcileLoop` service — polls non-terminal builds every 5s against Argo Workflow CRDs; subsumes both HTTP callback and StartupReconciler; control plane observes build plane instead of build plane pushing to control plane (#ARCH-C01, #ARCH-C03)
- `manifests.Render()` — generates Kubernetes manifests (Deployment, Service, IngressRoute) from app specs; correct separation: build plane builds images, control plane generates manifests (#ARCH-C02)
- `BUILD_RECONCILE_INTERVAL` env var — interval for BuildReconcileLoop polling (default 5s) (#ARCH-C01)
- `builds.apps_spec` JSONB column (migration 000008) — stores app specs at build submission for reconcile loop to generate manifests on workflow completion (#ARCH-C01)
- `BuildStore.SetAppsSpec()` / `GetBuildApps()` — persist and retrieve app specs per build (#ARCH-C01)
- `CircuitBreakerSyncClient` — in-memory circuit breaker with proper state machine (closed/open/half-open) replacing PostgreSQL-backed `PGCircuitBreakerSyncClient` (#ARCH-M02)

### Removed
- `POST /api/v1/builds/{id}/callback` route — replaced by BuildReconcileLoop (#ARCH-C01)
- `notify-api` step from Argo Workflow onExit DAG (#ARCH-C01)
- `generate-manifests` and `validate-manifests` steps from Argo Workflow main DAG (#ARCH-C02)
- `HealthProber` interface and direct HTTP health probing — replaced by ArgoCD health assessment (#ARCH-A03)
- `PGCircuitBreakerSyncClient` and `CircuitBreakerStore` interface — replaced by in-memory circuit breaker (#ARCH-M02)
- `StartupReconciler` — functionality merged into BuildReconcileLoop (#ARCH-C03)
- `manifests` volume claim from Argo Workflow spec — no longer needed since manifests are generated by API (#ARCH-C02)

### Security
- SSE stream handler now verifies tenant ownership — a user can only stream builds owned by their tenant; cross-tenant access returns 404 to avoid leaking build existence (#SSE-SEC-1)

### Fixed
- SSE stream had no tenant isolation — any authenticated user could stream events for any build ID, leaking app names, URLs, and deploy status across tenants (#SSE-SEC-1)
- SSE Prometheus metrics `loopzero_sse_connections_active` and `loopzero_sse_events_emitted_total` were defined but never instrumented — stream handler now increments/decrements on connect/disconnect and per event emitted (#SSE-BLOCKER-1)
- SSE streams had no concurrency limit — a runaway client could open unlimited connections, each polling PostgreSQL every 1s; now enforces max concurrent streams (default 200, configurable via `SSE_MAX_CONCURRENT_STREAMS`) with 503 rejection (#SSE-BLOCKER-2)
- SSE poll loop returned immediately on transient DB errors, disconnecting all streams during PG failover — now retries 3 times with continue before closing (#SSE-MOD-4)
- SSE `JOURNAL_EXPIRED` emitted as false positive when reconnecting with `Last-Event-ID` to a build with no events yet (fresh build) — now only emits when events exist but are pruned (#SSE-MOD-3)
- SSE event pruner `DELETE FROM sse_events WHERE created_at < $1` could lock table on large backlogs — now deletes in batches of 5000 rows (#SSE-MOD-2)
- SSE `writeSSEEvent` ignored write errors, causing poll loop to continue after client disconnect until next heartbeat — now returns error and caller closes stream immediately (#SSE-LEVE-1)
- SSE poll loop re-fetched build status every 1s even after build reached terminal state — now skips redundant `GetByID` calls once terminal (#SSE-LEVE-3)
- CLI SSE polling fallback (after `JOURNAL_EXPIRED`) had no terminal state detection — polled indefinitely; now closes when build status is `SUCCEEDED`, `FAILED`, or `CANCELLED` (#SSE-LEVE-4)
- SSE stream closing prematurely on build SUCCEEDED before deploy events arrive — stream now waits for stream-terminal events (`deploy.completed`, `deploy.sync_failed`, `deploy.git_push_failed`, `build.failed`, `build.cancelled`) with 5-minute safety timeout instead of closing on `IsBuildTerminal()` (#API-REVIEW-BLOCKER-1)

### Added
- `SSE_SAFETY_TIMEOUT` env var — configurable safety timeout for SSE streams waiting for deploy events after build completes (default 5m) (#SSE-LEVE-2)
- `SSE_MAX_CONCURRENT_STREAMS` env var — maximum number of simultaneous SSE connections (default 200) (#SSE-BLOCKER-2)
- `ActiveStreams()` method on `StreamHandler` — exposes current active SSE connection count for monitoring (#SSE-BLOCKER-1)
- Data race in auth token cache — `*cache = sync.Map{}` replaced with thread-safe `evictCache()` using `Range` + `Delete` (#API-REVIEW-BLOCKER-2)
- Background goroutines (build reaper, idempotency cleaner, SSE event pruner) now receive shutdown context — prevents queries against closed connection pool during graceful shutdown (#API-REVIEW-BLOCKER-3)
- `BuildHandler` created once and shared between auth routes and callback route — `callbackWg` now correctly tracks all in-flight callbacks for graceful shutdown (#API-REVIEW-LEVE-4)
- Callback `succeeded + empty apps` no longer returns 400 (which left build stuck in RUNNING for 30 min) — now treated as failed build to unblock environment immediately (#API-REVIEW-MOD-1)
- Terminal check moved before apps validation in callback handler — prevents 400 for duplicate callbacks with empty apps (#API-REVIEW-MOD-4)
- Callback handler poll loops in tests replaced with `WaitForCallbacks()` — eliminates data races under `-race` detector (#API-REVIEW-TEST-RACE)

### Changed
- `deploy_id` format increased from `d-{4hex}` (16 bits) to `d-{8hex}` (32 bits) — reduces collision probability from ~256 deploys to ~65K deploys (#API-REVIEW-MOD-5)
- `Config.Validate()` now returns `error` and fails fatally when `ARGOCD_INSECURE=true` in production — prevents MITM attacks (#API-REVIEW-SEC-2)
- Build reaper reason strings extracted to `reaperReason()` function — eliminates DRY violation (#API-REVIEW-LEVE-1)
- `ReleaseStore.SetGitCommit()` persists GitOps push commit SHA on release — enables end-to-end audit trail (#API-REVIEW-LEVE-2)

### Added
- `GetBuildTenantID()` on `BuildOrchestratorService` interface — enables tenant ownership verification (#API-REVIEW-SEC-4)
- `ReleaseStore.SetGitCommit()` method — persists git commit SHA from GitOps push (#API-REVIEW-LEVE-2)
- Context timeout (30s) on read-only build handlers (`Get`) — prevents slowloris on endpoints without `WriteTimeout` (#API-REVIEW-MOD-2)
- SSE stream safety timeout (5 min) — closes stream if build is terminal but no deploy events arrive (#API-REVIEW-BLOCKER-1)
- Cache eviction test with concurrent access under `-race` detector (#API-REVIEW-BLOCKER-2)
- SSE stream termination tests for deploy terminal events (#API-REVIEW-BLOCKER-1)

### Security
- Callback secret comparison uses `crypto/subtle.ConstantTimeCompare` — prevents timing attacks (#API-REVIEW-SEC-1)
- `GET /builds/:id` and `POST /builds/:id/cancel` now verify tenant ownership — returns 404 for cross-tenant access (#API-REVIEW-SEC-4)
- `ARGOCD_INSECURE=true` in production is now a fatal startup error, not just a log (#API-REVIEW-SEC-2)

### Added
- Bucket `loopzero-builds` created in MinIO dev and production values — cache CWTs now have a real bucket to write to (#MINIO-BLOCKER-1)
- MinIO credentials Secret `loopzero-minio-credentials` in build cluster — CWTs read credentials via `envFrom.secretRef` instead of plaintext workflow parameters (#MINIO-BLOCKER-2)
- MinIO lifecycle Job `minio-lifecycle-setup` — applies 30-day expiration rule on `loopzero-builds` bucket to prevent unbounded cache growth (#MINIO-MOD-2)
- `activeDeadlineSeconds: 120` on `cwt-cache-save` — prevents indefinite hangs when MinIO is slow or unreachable (#MINIO-MOD-4)
- S3 CLI timeouts `--cli-connect-timeout 10 --cli-read-timeout 30` on all `aws s3 cp` commands in cache CWTs (#MINIO-MOD-4)
- MinIO credential validation in `Config.Validate()` — logs error when production uses default dev credentials (#MINIO-MOD-5)

### Changed
- **MinIO PVC increased from 1Gi to 10Gi** — sized for ~30 projects with dependency cache (~300MB each compressed) (#MINIO-MOD-1)
- **MinIO console service changed from NodePort to ClusterIP** — console gives full admin access, use `kubectl port-forward` when needed (#MINIO-LEVE-1)
- **Production overlay bucket names aligned with CWT code** — `loopzero-cache` renamed to `loopzero-builds` to match `s3://loopzero-builds/` in cache CWTs (#MINIO-BLOCKER-1)
- **MinIO credentials removed from Workflow CR parameters** — `InfraParams` no longer contains `MinIOEndpoint`, `MinIOAccessKey`, `MinIOSecretKey`; CWTs read from K8s Secret instead (#MINIO-BLOCKER-2)
- **Cache CWT stderr no longer suppressed** — `2>/dev/null` replaced with `2>&1` so S3 errors are visible in Argo step logs (#MINIO-LEVE-3)
- **NetworkPolicy `allow-minio` permits cross-cluster ingress** — build cluster workflow pods can now reach MinIO on port 9000 via Docker networking (#MINIO-MOD-6)
- **ServiceMonitor label selector updated** — `app: minio` → `app.kubernetes.io/name: minio` to match MinIO Helm chart labels (#MINIO-LEVE-4)

### Security
- MinIO credentials no longer visible in plaintext in Workflow CR objects — moved to K8s Secret with `envFrom.secretRef` (#MINIO-SEC-2)
- MinIO console no longer exposed via NodePort — reduces attack surface (#MINIO-SEC-5)
- Production credential validation for MinIO access key and secret key (#MINIO-MOD-5)

- TLS termination for all deployed apps — Traefik `websecure` entryPoint (port 443) with self-signed CA chain via cert-manager (#INGRESS-BLOCKER-1)
- Self-signed CA chain: `ClusterIssuer/selfsigned-bootstrap` → `Certificate/loopzero-ca` (isCA) → `ClusterIssuer/loopzero-ca-issuer` → wildcard `Certificate/loopzero-wildcard` for `*.loopzero.local` (#INGRESS-BLOCKER-1)
- `TLSStore/default` configures Traefik to use wildcard cert for all IngressRoutes with `tls: {}` (#INGRESS-BLOCKER-1)
- HTTP→HTTPS redirect on Traefik `web` entryPoint — all HTTP traffic redirected to `websecure` (#INGRESS-BLOCKER-1)
- Traefik high availability: 2 replicas, PodDisruptionBudget (minAvailable: 1), pod anti-affinity by hostname (#INGRESS-BLOCKER-2)
- HTTPS port mapping (host:8443 → LB:443) in k3d workload cluster config (#INGRESS-BLOCKER-1)
- `Middleware/rate-limit` in Traefik namespace — 100 req/s average, 200 burst per source IP (#INGRESS-MOD-1)
- `Middleware/body-limit` in Traefik namespace — 10 MiB max request body (#INGRESS-MOD-2)
- `Middleware/security-headers` in Traefik namespace — HSTS, X-Content-Type-Options, X-Frame-Options, XSS filter (#INGRESS-SEC-4)
- `BASE_DOMAIN` env var (default: `loopzero.local`) — configurable base domain for app hostnames, eliminates hardcoded `.loopzero.local` (#INGRESS-MOD-5)
- `base-domain` parameter on `loopzero-manifests-generate` CWT — receives domain from API config (#INGRESS-MOD-5)
- `BaseDomain` field on `InfraParams`, `WorkflowSubmitterConfig`, and `Config` structs (#INGRESS-MOD-5)
- Readiness and liveness probes on generated Deployment manifests (`/health` endpoint) (#INGRESS-LEVE-3)
- Unit tests for `base-domain` parameter passing and custom domain configuration (#INGRESS-MOD-5)

### Changed
- **cert-manager moved from mgmt cluster to workload cluster** — cert-manager must be in the same cluster as Traefik and IngressRoutes to issue certificates (#INGRESS-BLOCKER-3)
- **Manifest generation refactored from string concatenation to JSON.stringify** — eliminates fragile YAML string building, uses valid JSON (subset of YAML) for Kubernetes resources (#INGRESS-MOD-4)
- **Generated IngressRoutes use `websecure` entryPoint with `tls: {}`** — all deployed apps get TLS automatically (#INGRESS-BLOCKER-1)
- **Generated IngressRoutes include rate-limit, body-limit, and security-headers middlewares** — cross-namespace middleware references (#INGRESS-MOD-1, #INGRESS-MOD-2, #INGRESS-SEC-4)
- **Callback app URLs use `https://` scheme** instead of `http://` (#INGRESS-BLOCKER-1)
- Traefik resource limits increased from 128Mi to 256Mi memory, 300m to 500m CPU — sized for multi-tenant workloads with access logs and Prometheus metrics (#INGRESS-MOD-3)
- Traefik metrics port no longer exposed on LoadBalancer — scrape via ServiceMonitor/NodePort only (#INGRESS-LEVE-1)
- Traefik access logs now filter to 4xx/5xx status codes and retry attempts — reduces log volume in production (#INGRESS-LEVE-4)
- Traefik access log headers filtered: only Host, X-Correlation-ID, User-Agent retained (#INGRESS-LEVE-4)
- Traefik global timeouts configured: 60s read/write on websecure, 30s dial, 120s idle (#INGRESS-MOD-6)
- Traefik `allowCrossNamespace: true` on CRD provider — enables tenant IngressRoutes to reference middlewares in `traefik` namespace (#INGRESS-MOD-1)

### Security
- All deployed apps now served over HTTPS with TLS certificates from self-signed CA — no more plaintext HTTP (#INGRESS-SEC-1)
- HTTP→HTTPS redirect prevents accidental plaintext communication (#INGRESS-SEC-2)
- Traefik metrics endpoint no longer publicly accessible via LoadBalancer (#INGRESS-SEC-3)
- Security headers (HSTS, X-Content-Type-Options, X-Frame-Options, XSS filter) applied to all tenant traffic via Traefik middleware (#INGRESS-SEC-4)

- `WaitForSync` call after `TriggerSync` in release activation — deploy no longer marked ACTIVE until ArgoCD confirms the new version is synced and healthy (#BLOCKER-2)
- Retry with exponential backoff on `TriggerSync` (3 attempts, 1s/2s/4s) — transient ArgoCD failures no longer cause immediate SYNC_FAILED (#MOD-1)
- `ARGOCD_SYNC_TIMEOUT` config (default 120s) — controls how long WaitForSync waits before failing (#BLOCKER-2)
- `WORKLOAD_CLUSTER_URL` env var for operator — ArgoCD Applications now target configurable workload cluster instead of hardcoded `kubernetes.default.svc` (#BLOCKER-1)
- `TenantControllerConfig` struct replaces variadic `giteaURL` parameter in `SetupTenantController` — cleaner dependency injection (#BLOCKER-1)
- `EnsureAppProject` and `DeleteAppProject` on ArgoCD Provisioner interface — each tenant now gets its own AppProject with scoped RBAC isolation (#MOD-5)
- `ARGOCD_INSECURE=true` logs error in production mode — prevents accidental TLS bypass (#SEC-3)

### Changed
- **ArgoCD Application path scoped to environment** — path changed from `"."` (root) to `{environment}` to prevent cross-environment manifest contamination (#BLOCKER-3)
- **GitOps manifest directory hierarchy inverted** — from `{project}/{env}/manifests.yaml` to `{env}/{project}/manifests.yaml` to enable environment-scoped ArgoCD Applications (#BLOCKER-3)
- **ArgoCD Application project scoped to tenant** — Applications now reference per-tenant AppProject instead of `"default"`, enforcing RBAC isolation between tenants (#MOD-5)
- **GitOps pusher credentials via HTTP header** — Gitea token passed via `http.extraHeader` instead of embedded in git URL, preventing exposure in `/proc/PID/cmdline` (#SEC-1)
- **GitOps pusher HTTP client with timeout** — replaced `http.DefaultClient` (no timeout) with dedicated client (15s timeout) in Gitea API calls (#MOD-2)
- **Gitea source store HTTP client with timeout** — same fix applied to source bundle operations (#MOD-2)
- **Gitea repo creation uses `json.Marshal`** — replaced `fmt.Sprintf` JSON construction to prevent injection via special characters in repo names (#SEC-4)

### Fixed
- ArgoCD Applications deployed to management cluster instead of workload cluster — `DestinationURL` now read from `WORKLOAD_CLUSTER_URL` env var (#BLOCKER-1)
- Deploy marked ACTIVE before ArgoCD completed sync — `WaitForSync` now runs before health probes to confirm new version is actually deployed (#BLOCKER-2)
- Deploy of project A triggered ArgoCD re-sync of all projects in the tenant — environment-scoped Application path prevents cross-project contamination (#BLOCKER-3)
- `BuildStore.SetWorkflowID()` persists Argo Workflow name on build record — startup reconciler and cancel now correctly correlate builds with external workflows (#BUILD-BLOCKER-1)
- `ReleaseStore.GetActiveByEnvTx()` reads active release within advisory lock transaction — eliminates read-outside-tx race in release supersession (#BUILD-MOD-4)
- Kaniko `registry-insecure` parameter — `--insecure` and `--skip-tls-verify` flags now controlled per-workflow via parameter instead of hardcoded in CWT (#BUILD-BLOCKER-3)
- Retry with exponential backoff in `WorkflowSubmitter.Submit()` for transient K8s API server errors — 3 attempts with 1s/2s/4s delays (#BUILD-MOD-1)
- Build reaper now emits `build.failed` SSE events for reaped builds — CLI gets notified immediately instead of waiting 10 minutes (#BUILD-MOD-5)
- 60-second context timeout on GitOps push operations — prevents indefinite hangs when Gitea is unresponsive (#BUILD-MOD-2)
- Token sanitization in gitops pusher logs — Gitea credentials are masked in error output (#BUILD-SEC-3)
- `getBuildStatus` fallback in CLI deploy — verifies actual build state via API when SSE stream breaks, preventing false timeout failures (#CLI-BLOCKER-1)
- Retry with exponential backoff in `ApiClient` for transient server errors (502/503/504) and network errors — 3 attempts with 1s/2s/4s delays (#CLI-MOD-3)
- `User-Agent: loopzero-cli/<version>` header on all API requests for server-side debugging and version tracking (#CLI-LEVE-5)
- Bundle size validation before upload — fails fast with actionable message when source exceeds 500MB limit (#CLI-MOD-5)
- `MAX_BUNDLE_SIZE_BYTES` constant in CLI config for pre-upload size check (#CLI-MOD-5)
- `maxRetries` option on `ApiClient` — allows disabling retries in tests (#CLI-MOD-3)

### Changed
- **Phase 2 release activation refactored to release-lock-between-I/O pattern** — GitOps push, ArgoCD sync, and health probes now run OUTSIDE advisory locks; locks held only for millisecond DB state mutations (#BUILD-BLOCKER-2)
- `WorkflowSubmitter.Submit()` now persists returned workflow name via `SetWorkflowID` — startup reconciler and cancel correctly reference external workflows (#BUILD-BLOCKER-1)
- `appsJSON()` now uses `json.Marshal` instead of manual `fmt.Sprintf` string concatenation — eliminates JSON injection risk from special characters in app names (#BUILD-LEVE-1)
- `createGitBundle` now uses async `execFile` instead of `execSync` — event loop no longer blocked during bundle creation, SIGINT works on large repos (#CLI-BLOCKER-2)
- Source upload now uses configurable `UPLOAD_TIMEOUT_MS` constant instead of hardcoded 30s — `LOOPZERO_UPLOAD_TIMEOUT` env var is now functional (#CLI-BLOCKER-3)
- `checkSensitiveFiles` now uses git pathspec filtering instead of listing all tracked files — efficient on large repos (#CLI-MOD-2)
- Idempotency key now includes 60-second time window — allows retry after build failure without requiring a new commit (#CLI-MOD-4)
- Secrets validation wraps network errors gracefully — deploy continues with warning instead of crashing on unreachable API (#CLI-MOD-6)
- Deploy final output now uses `DeployRenderer` for both intermediate and final events — single renderer, no duplication (#CLI-LEVE-2)

### Removed
- Dead dependencies `chalk` and `ora` from CLI package.json — were declared but never imported (#CLI-LEVE-1)
- Deprecated browser OAuth flow from `LoginCommand` — dead code path never wired in production (#CLI-LEVE-3)
- Duplicate `displayResult` method from `DeployCommand` — replaced by `DeployRenderer.renderResult()` (#CLI-LEVE-2)

### Fixed
- `.env.example` with documented dev credentials and `dotenv` support in Taskfile for override via `.env.local` (#E-01)
- ADR 0004: encryption at rest for Kubernetes secrets — documents production path and local dev acceptance (#F-03)
- Table of contents with anchor links to all sections and subsections in docs/technical.md (#H-01)
- ADR folder with 3 initial architecture decision records: advisory lock type, Gitea as GitOps store, Kaniko for builds (#H-03)
- Sprint 02 README.md with navigable index of all documents, categorized as current vs historical (#G-07)
- Rate limiting middleware (60 req/min per tenant) on mutation endpoints — prevents resource exhaustion (#C-01)
- Structured audit logging middleware for all mutation requests — logs user, tenant, method, path, status, duration (#F-01)
- NetworkPolicies for loopzero-system namespace: default deny ingress + allow rules for API, PostgreSQL, Gitea, MinIO, DNS (#E-11)
- Database migration 000007: covering indexes for builds, releases, idempotency, and secrets queries (#I-02)
- Cursor-based pagination helper (`ParsePagination`) for list endpoints — default 50, max 100, cursor support (#I-05)
- Unit tests for startup reconciler, PG circuit breaker, API config, CLI constants, pagination (#G-01, #G-02, #G-03, #I-05)
- Skeleton Ginkgo test file for PreviewEnvironment controller — documents all lifecycle test cases for Sprint 5 (#G-08)
- Production overlay for PostgreSQL Helm chart with HA replication, WAL archiving, backup CronJob, and TLS (#E-04)
- Production overlay for MinIO Helm chart with distributed mode, 4 replicas, TLS, and lifecycle policy docs (#E-05)
- ADR 0005: cross-cluster networking strategy — documents migration from hardcoded IPs to DNS-based discovery (#E-02)

### Changed
- Gitea database backend switched from SQLite to PostgreSQL — uses existing cluster PostgreSQL instance via initdb script (#E-06)
- Health probes during release activation run in parallel with concurrency limit of 5 — reduces multi-app deploy latency (#A-09)
- Circuit breaker depends on `CircuitBreakerStore` interface instead of concrete struct — enables unit testing via DIP (#G-02)
- PostgreSQL pool `MaxConns` increased from 20 to 25 for concurrent deploy workloads (#I-01)
- Shared HTTP transport with connection pooling for Gitea, ArgoCD, and health probe clients (#I-03)
- TenantController ClusterRole scoped to minimum required permissions with documentation (#F-02)
- JSON `--json` output is now compact (single line) instead of pretty-printed — spec-compliant for piping (#B-10)
- README updated to reflect implemented commands (login, logout, whoami, init, deploy) (#H-02)

### Security
- Metrics endpoint `/metrics` documented as ClusterIP-only — no authentication, relies on NetworkPolicy for access control (#C-05)

### Fixed
- NetworkPolicy redesigned: per-component egress rules replace global DNS-only egress that blocked API external access (GitHub auth, cross-cluster) (#dogfood-pre-prod-F01)
- TenantController ClusterRole: added `watch` for namespaces, `list`+`watch` for resourcequotas, limitranges, priorityclasses, secrets — controller-runtime cache requires these at cluster scope (#dogfood-pre-prod-F02)
- Deploy command handles `git rev-parse HEAD` failure gracefully — shows actionable message instead of raw git error (#dogfood-pre-prod-F03)
- Auth service errors (503 AUTH_SERVICE_UNAVAILABLE) now return exit code 4 (auth) instead of 3 (deploy) in source upload (#dogfood-pre-prod-F04)
- CLI default API URL changed from `localhost:3001` to `localhost:13001` to match k3d NodePort mapping (#dogfood-pre-prod-F05)
- ESLint error in `git.ts`: replaced `require('fs')` with top-level `import * as fs` (#dogfood-pre-prod-F06)
- Stale `argocd_client_test.go` removed — replaced by PG circuit breaker tests (#G-02)
- `builds_route_test.go` updated for `NewBuildHandler` shutdown context parameter (#G-02)
- CWT notify callback payload capped at 1MB — prevents oversized payloads for large monorepos (#E-09)
- Kaniko `--insecure` and `--skip-tls-verify` flags now documented as dev-only with production removal instructions (#E-07)
- CWT `manifests-validate` no longer blocks builds when GitHub is unreachable for CRD schema fetch — network failures warn instead of fail (#E-12)
- Cache key now includes lockfile hash (sha256, first 12 chars) — dependency changes invalidate stale caches (#I-04)
- `task setup` / `task dev:seed` no longer hang indefinitely — replaced `ensure-pg.sh` port-forward with `kubectl exec` for all DB tasks (#dogfood-f01)
- `tilt down` no longer destroys PostgreSQL, Gitea, and MinIO — infrastructure deps observed via `local_resource` probes instead of `helm_resource` lifecycle management (#dogfood-f02)
- `go.work` no longer references `loopzero-forge` side project — fixes API build failure in Tilt (#dogfood-f03)
- SSE deploy timeout now resets on every received event (activity-based) instead of firing after absolute duration — long builds no longer killed by false timeouts (#A-04)
- Callback goroutine now respects API shutdown signals via shutdown-aware context instead of `context.Background()` (#A-05)
- Callback endpoint now limits request body to 10MB via `http.MaxBytesReader` — prevents DoS via oversized payloads (#A-06)
- CLI config store uses atomic write-then-rename to prevent token exposure during file permission race (#B-06)
- CLI signal handlers (SIGINT/SIGTERM) now clean up SSE connections and temp files on Ctrl+C (#B-09)
- CLI env var parsing now validates `parseInt` results with `isNaN` fallback to prevent `NaN` timeouts (#B-12)
- CHANGELOG duplicate `### Changed` sections merged (#H-04)
- Build/Release ID entropy increased from 32-bit to 64-bit — reduces collision probability at scale (#A-07)
- `loopzero init` now validates git repository presence — fails early with actionable message instead of silent failure at deploy time (#B-02)
- `loopzero init` validates project names against RFC 1123 label rules (#B-16)
- SSE client reconnection uses exponential backoff (1s→2s→4s...60s cap) instead of constant 1s delay (#B-08)
- Custom YAML parser replaced with `js-yaml` library — handles tabs, comments, multiline strings, and non-standard indentation (#B-01)
- API mutation handlers (Create, Cancel, Upload) now set 150s context deadline to prevent indefinite operations (#C-02)
- API warns on startup when using default callback secret in non-production environments (#C-03)
- API graceful shutdown now waits for in-flight callback goroutines via `sync.WaitGroup` (#C-08)
- Operator `Status().Update()` errors now logged instead of silently discarded (#D-03)
- Operator deletion path now logs non-fatal cleanup failures with step name for traceability (#D-06)
- Operator namespace creation now uses `naming` module for RFC 1123 validation instead of raw `fmt.Sprintf` (#D-01)
- Gitea URL centralized in operator config — no longer hardcoded in both `main.go` and `tenant_gitops.go` (#D-05)
- CWT `manifests-generate` now sets `NODE_ENV` from environment parameter instead of hardcoding `production` (#E-08)
- Gitea token provisioning now uses fixed name `loopzero-api` with delete-before-create — prevents stale token accumulation (#E-13)
- CLI ANSI colors now gated on TTY detection in addition to CI mode — redirected output no longer includes escape codes (#B-11)
- HTTP 409 Conflict now maps to exit code 1 (usage error) instead of 3 (deploy failure) — correct semantics for concurrent deploy rejection (#B-13)
- `process.emit()` monkey-patch replaced with clean `warning` event listener for ExperimentalWarning suppression (#B-15)
- Git bundle now includes only HEAD instead of `--all` refs — reduces bundle size for large repos (#B-03)
- Dead in-memory circuit breaker removed — only PG-backed implementation remains (#A-02)
- SSE event store `GetAfter` now returns max 1000 events per query — prevents memory spikes on long builds (#C-04)
- Fallback callback parsing simplified to generic failed status — removes fragile string matching (#C-07)
- Error message capitalization standardized to sentence case across all API routes (#C-10)
- Token validation cache bounded to 10K entries — prevents unbounded memory growth from automated clients (#F-05)
- Release state machine `DRIFT_ALERT` and `ROLLBACK_FAILED` states now have `ManualResolve` transition to `ACTIVE` (#A-03)
- CRD types now include `kubebuilder:validation` markers for required fields and constraints (#D-02)
- `allConditionsTrue()` condition list extracted to `requiredProvisioningConditions` package var — single source of truth (#D-04)

### Added
- ResourceQuota and LimitRange for `argo` namespace — caps total resource consumption at 16 CPU / 32Gi and sets container defaults (#E-10)
- Idempotency key cleanup background job — prunes expired keys every hour (#A-08)
- SSE event pruner background job — prunes events older than 24h every hour (#C-06)
- `isUsingDefaultApiUrl()` export in CLI constants — allows commands to warn when using localhost fallback (#B-07)
- `ReleaseManualResolve` event for release state machine — recovers from `DRIFT_ALERT` and `ROLLBACK_FAILED` sink states (#A-03)
- Sensitive file detection in CLI deploy — warns about `.env`, `.key`, `.pem` files before upload (#F-04)
- Composable multi-language build pipeline: `LanguageStrategy` interface with Node.js, Go, Python, and Custom implementations (#pipeline-composable)
- `WorkflowBuilder` generates self-contained Workflow CRs with CWT refs + inline language-specific templates (#pipeline-composable)
- 8 ClusterWorkflowTemplates extracted from monolithic YAML: clone, cache-restore, cache-save, kaniko, manifests-generate, manifests-validate, notify, summary (#pipeline-composable)
- `AppSpec.Framework` field for language detection in `loopzero.yaml` (#pipeline-composable)

### Changed
- `WorkflowSubmitter.Submit()` now uses `WorkflowBuilder` instead of referencing a monolithic `WorkflowTemplate` (#pipeline-composable)
- Removed `WorkflowTemplate` config field and `WORKFLOW_TEMPLATE_NAME` env var — no longer needed with composable builder (#pipeline-composable)
- **API config centralization:** All hardcoded values (URLs, timeouts, credentials, pool settings) extracted to `api/internal/config/config.go` with env var overrides (#config-cleanup)
- **CLI constants:** Magic numbers (timeouts, retries, ports, limits) extracted to `cli/src/config/constants.ts` (#config-cleanup)
- **DB pool configurable:** `db.NewPool` now accepts `PoolConfig` struct — all pool parameters configurable via env vars (DB_MAX_CONNS, DB_STATEMENT_TIMEOUT, etc.) (#config-cleanup)
- **State machine timeouts:** Build and release timeout durations extracted to named constants in `pkg/statemachine/` (#config-cleanup)
- **HTTP status codes:** Operators use `http.StatusOK`/`http.StatusCreated`/etc. instead of raw integers (#config-cleanup)
- **Server timeouts configurable:** ReadTimeout, IdleTimeout, GracefulShutdown driven by env vars (#config-cleanup)
- **Taskfile hardening:** DRY port-forward boilerplate via `ensure-pg.sh`, db:reset prompt, build:cleanup safe iteration, monitoring CRD ordering, tokens:provision resilience (#taskfile-review)
- **Infra fixes:** cert-manager values updated for v1.20, PostgreSQL PDB `maxUnavailable`, OTel collector port conflict resolved, ArgoCD token `expiresIn` type fix (#taskfile-dogfood)

### Added
- `api/internal/config/config.go` — centralized env-var-driven configuration with 50+ configurable values and dev defaults (#config-cleanup)
- `cli/src/config/constants.ts` — centralized CLI constants (API URL, timeouts, SSE config, deploy limits) (#config-cleanup)
- `infra/scripts/ensure-pg.sh` — reusable port-forward helper for Taskfile DB tasks (#taskfile-review)
- `infra:install:monitoring-crds` Taskfile task — installs Prometheus CRDs before Helm charts that reference ServiceMonitors (#taskfile-dogfood)

### Changed
- **GitOps decoupling (Step 6):** Build pipeline no longer pushes to GitOps repo — API owns the entire release flow after build succeeds, enabling retry-without-rebuild (#S2-step6)
- `pushToGitOps` in release_activator.go now performs real git commit to Gitea tenant repo via `GitOpsPusher` interface (#S2-step6)
- Argo Workflow `notify-api` callback now includes generated manifests YAML in payload — API receives manifests instead of assuming gitops-push was done (#S2-step6)
- Argo Workflow `notify-api` uses `python:3.12-alpine` image for safe JSON encoding of manifests payload (#S2-step6)
- Release `Artifacts` field now stores manifests for retry support — if git push fails, API retries from stored manifests (#S2-step6)

### Added
- `GitOpsPusher` interface and `GiteaGitOpsPusher` implementation in `api/internal/gitops/` — commits manifests to tenant GitOps repo with git trailers (#S2-step6)
- `RetryRelease` method on `BuildOrchestrator` — retries GitOps push for releases in GIT_MIRROR_FAILED state by reading stored manifests from `release.Artifacts`, no new build triggered (#S2-step6)
- 7 unit tests for Step 6 gate criteria: git push failure → GIT_MIRROR_FAILED, manifests flow from callback to pusher, manifests stored in release artifacts, failed builds skip gitops push, retry-without-rebuild, reject retry on wrong state, retry failure keeps state safe (#S2-step6)
- 6 integration tests for `GiteaGitOpsPusher`: push manifests, correct file path, git trailers, idempotency, empty manifests rejection, update existing (#S2-step6)

### Removed
- `gitops-push` step removed from Argo Workflow DAG — build pipeline stops at `validate-manifests` (#S2-step6)

### Changed
- Split `tenant_controller.go` (790 lines) into 7 focused files by resource type: namespaces, RBAC, network policies, GitOps, build namespace, priority class (#M2)
- Split `build_orchestrator.go` (699 lines) — release activation methods extracted to `release_activator.go` (#M3)
- Generated manifests now use `envFrom: secretRef` instead of inline env vars for app secrets — secrets never appear in Git (#hardening-step1)
- Gitea token scope narrowed from `["all"]` to `["write:repository","read:user"]` (least privilege) (#hardening-step1)
- Build cluster kubeconfig uses scoped ServiceAccount (workflows + configmaps in argo ns) instead of admin cert (#hardening-step1)

### Added
- Startup reconciler: recovers orphaned builds on API boot — checks Argo Workflow state, finalizes or fails builds stuck in RUNNING/QUEUED (#hardening-step2)
- PG-backed circuit breaker: ArgoCD circuit breaker state shared across all API replicas via `circuit_breakers` table (#hardening-step2)
- Database migration `000006_circuit_breakers` for shared circuit breaker state (#hardening-step2)
- `BuildStore.FindActiveBuilds()` for startup reconciler queries (#hardening-step2)
- `WorkflowSubmitter.GetStatus()` for checking external workflow state during reconciliation (#hardening-step2)
- `loopzero.io/secret-hash` annotation in generated Deployment manifests — enables rolling updates when secrets change (#hardening-step1)
- Dedicated ArgoCD `loopzero-api` service account with `apiKey` capability — non-expiring tokens replace 24h session tokens that silently broke all deploys (#dogfood-S2-F01)
- ArgoCD RBAC policy for `loopzero-api`: sync + get on applications only (least privilege) (#dogfood-S2-F01)
- CLI `LOOPZERO_TOKEN` env var support — overrides config file token for CI/CD pipelines (#dogfood-S2-F02)
- CLI `deploy --help` now shows `--json` and `--wait` flags (#dogfood-S2-F04)
- Taskfile tasks: `networking:setup`, `tokens:provision`, `tokens:verify` — cross-cluster networking and token provisioning are now part of `task setup` (#dogfood-S2-F01)
- `task doctor` now verifies ArgoCD/Gitea token validity and expiration (#dogfood-S2-F01)
- PodDisruptionBudgets for PostgreSQL, Gitea, and ArgoCD — prevents correlated eviction during node drains (#hardening)

### Fixed
- ArgoCD sync failures caused by expired admin session token (24h TTL) — replaced with non-expiring `apiKey` token from dedicated service account (#dogfood-S2-F01)
- Stale manifests in Gitea tenant repo prevented ArgoCD sync — cleaned orphan project directories from previous test runs (#dogfood-S2-F01)
- CLI `--no-wait` message improved from "Wait for it to complete, or try again later" to actionable "Track status: loopzero deploy --env <env> --wait" (#dogfood-S2-F03)
- Production env validation: API fails fast on missing required vars when `LOOPZERO_ENV=production` (#hardening)
- Dedicated metrics server on port 9090 — `/metrics` no longer exposed on the public API port (#hardening)
- Production credential warnings in Helm values for postgresql, gitea, minio (#hardening)
- PDB manifests validated in `task validate:manifests` pipeline (#hardening)
- CLI `bin/loopzero` entrypoint — CLI can now be installed and invoked via `npx loopzero` or `npm install -g @loopzero/cli` (#dogfood-S2-F01)
- CLI lockfile pre-check warns before upload when package-lock.json/yarn.lock/pnpm-lock.yaml is missing (#dogfood-S2-R2-F03)
- WorkflowTemplate `ttlStrategy` — auto-cleanup of completed workflows (1h) and failed (6h) to prevent PVC accumulation (#dogfood-S2-R2-F05)

### Fixed
- Build pipeline `validate-manifests` rejected Traefik IngressRoute CRD — added datreeio/CRDs-catalog as secondary schema source for kubeconform so CRDs are validated against real schemas (#dogfood-S2-R2-F01)
- Build failure error propagated as generic "Build pipeline failed" — failing steps now write specific error to shared volume, callback includes step name and message (#dogfood-S2-R2-F02)
- CLI mapped `AUTH_SERVICE_UNAVAILABLE` (503) to exit code 3 (deploy failure) — now correctly maps to exit code 4 (auth error) (#dogfood-S2-R2-F04)

### Fixed
- CLI `--app` flag now correctly scopes secret validation to selected apps only — deploying an app without secrets no longer fails on sibling apps' secrets (#dogfood-F02)
- SSE `build.started` event now includes `apps` array with app names being built — CLI renders per-app "Building api..." lines instead of empty app name (#dogfood-F01)
- ArgoCD duplicate resource warnings caused by stale manifests using unprefixed K8s names — WorkflowTemplate updated in cluster to use project-prefixed resource names (#dogfood-F05)
- CLI error messages now suggest next action in all paths — "Not a git repository" suggests navigating to project, env validation shows example (#dogfood-S2-F03)
- SSE `JOURNAL_EXPIRED` event now includes `reason` and `action` fields for client recovery (#dogfood-S2-F07)
- API log in `source.go` replaced `tenant_id` with `team_id` to avoid Rule 3 term leaking into future log views (#dogfood-S2-F05)
- SSE stream handler test fixtures updated to include `apps[]` in `build.started` events, matching production format (#dogfood-S2-F02)
- Unit test for `build.started` SSE now asserts `apps[]` field is present — prevents regression of per-app progress rendering (#dogfood-S2-F04)

### Added
- Typed `code` field in CLI `--json` error responses for machine parsing (e.g., `AUTH_REQUIRED`, `BUILD_FAILED`, `INVALID_ENVIRONMENT`) — `exit_code` retained for backwards compatibility (#dogfood-F03)
- `build_log` field in `--json` output for failed apps pointing to `loopzero logs --build <id>` per spec §0.3 (#dogfood-F04)
- `ApiError.code` field propagating API error codes through CLI JSON output (#dogfood-F03)

### Fixed
- `validate-manifests` step had `continueOn: failed: true` allowing invalid manifests to reach GitOps repo — removed, now a hard gate; `gitops-push` depends on validation passing (#S2-helena-1)
- `validate-manifests` downloaded kubeconform via curl at runtime (network-dependent, non-deterministic) — replaced `alpine:3.21` with `ghcr.io/yannh/kubeconform:v0.6.7-alpine` (#S2-helena-4)
- Callback `image_digest` field contained image tag reference instead of real SHA256 digest — Kaniko now uses `--digest-file` and callback sends `registry/tenant/project@sha256:...` format (#S2-helena-5)
- Callback body missing `correlation_id` field breaking end-to-end traceability — now included in all 3 callback branches (succeeded, failed, fallback) plus `Correlation-ID` HTTP header (#S2-helena-6)

### Added
- `spec.parallelism: 4` on WorkflowTemplate — limits concurrent steps per workflow to prevent scheduler pressure (#S2-helena-2)
- Argo Workflows controller `workflowWorkers: 32` and `podWorkers: 32` — throttles concurrent workflow and pod reconciliations (#S2-helena-2)
- 6 new workflow template tests: validate-manifests hard gate, no curl download, callback correlation_id, Kaniko digest-file, callback real digest, parallelism limit (#S2-helena)

### Added
- QA tooling: `task doctor` — checks all prerequisites, clusters, services with pass/fail/warn and install hints (#QA-tooling)
- QA tooling: `task status` — shows environment health at a glance (clusters, pods, DB, API, monitoring) (#QA-tooling)
- QA tooling: `task test:smoke` — quick platform validation in <30s before running full test suites (#QA-tooling)
- QA tooling: `task db:reset` and `task db:reset:seed` — reset database to clean state with optional re-seed (#QA-tooling)
- QA tooling: `task db:shell` and `task db:port-forward` — interactive DB access for debugging (#QA-tooling)
- QA tooling: `task test:e2e:setup` — one-command E2E preparation (smoke + tokens + seed + verify) (#QA-tooling)
- QA tooling: `task clean` and `task clean:all` — cleanup test artifacts and full environment teardown (#QA-tooling)
- QA tooling: `task test:report`, `task test:report:unit`, `task test:report:e2e` — JUnit XML output for CI/QA tools via gotestsum (#QA-tooling)
- QA tooling: `task test:all` — run complete test pyramid (unit + integration + lint + manifests + E2E) (#QA-tooling)
- QA tooling: `task logs:api`, `task logs:operator`, `task logs:builds` — tail live logs for debugging (#QA-tooling)
- QA tooling: `task build:cleanup` — clean up completed/failed Argo Workflow pods in build cluster (#QA-tooling)
- QA tooling: `task db:status` — show database health: tables, row counts, connections, advisory locks (#QA-tooling)

### Changed
- HTTP server now enforces `ReadTimeout: 30s` and `IdleTimeout: 120s` — prevents connection exhaustion from slow clients (#hardening)
- Idempotency middleware limits body read to 10MB via `io.LimitReader` — prevents OOM on large uploads (#hardening)
- Operator Dockerfile now defaults to `--leader-elect=true` — prevents duplicate reconciliation with 2+ replicas (#hardening)
- Tilt dev loop continues using `--leader-elect=false` (single replica, no change to local DX) (#hardening)
- Standardized all PostgreSQL port-forwards to port 15432 (was inconsistent: 5432, 15432, 15433) (#QA-tooling)
- `task test` now runs unit + lint + manifest validation instead of unit only (#QA-tooling)
- `task dev:seed:e2e` now manages its own port-forward instead of requiring external DATABASE_URL (#QA-tooling)
- `task test:e2e` now sets `LOOPZERO_E2E=1` automatically — no need for manual env var (#QA-tooling)
- All DB tasks reuse existing port-forwards (e.g., from tilt) instead of conflicting (#QA-tooling)
- `monitoring:validate-targets` and `monitoring:status` use centralized port variables (#QA-tooling)

### Fixed
- K8s resource name collision when multiple projects in same tenant define apps with same name (e.g., both have "api") — manifests now prefix resource names with project-id (e.g., `dogfood-saas-api` instead of `api`) to prevent ArgoCD `RepeatedResourceWarning` and `OutOfSync` failures (#S2-dogfood3 F-01)
- Callback error message for unknown status always said "unexpected status: failed" instead of showing the original invalid status value — now captures original status before overwriting (#S2-review2)
- Dead parameter `apps []AppRelease` in `appTypeFromName` — removed unused parameter (#S2-review2)
- Build callback endpoint returns 400 on malformed Argo payload, leaving builds permanently stuck in RUNNING — now uses best-effort parsing with fallback to "failed" status, never returns 400 for callback payloads (#S2-dogfood2 F-01)
- Argo Workflow `notify-api` template dumps raw `{{workflow.failures}}` JSON array into error field, producing malformed callback payload — now extracts only displayName fields, avoiding nested JSON (#S2-dogfood2 F-01)
- FAILED builds have empty `error_message` in database — error from callback and reaper now persisted via `SetErrorMessage`, including descriptive timeout reasons for reaped builds (#S2-dogfood2 F-04)
- API returns plain text `404 page not found` for unknown routes — now returns JSON `{"code":"NOT_FOUND","message":"..."}` consistent with LoopZeroError format; same for 405 Method Not Allowed (#S2-dogfood2 F-05)
- Operator tests fail with `no such file: /usr/local/kubebuilder/bin/etcd` when run via `go test` directly — suite_test.go now resolves envtest binaries from `KUBEBUILDER_ASSETS`, then `~/.local/share/kubebuilder-envtest/`, then default path (#S2-dogfood2 F-06)
- API unit tests take 240s due to health prober unhealthy tests using default 10-retry backoff (~120s each) — tests now use single-attempt backoff, reducing suite from 240s to 0.15s (#S2-dogfood2 F-08)
- PoolEnvironmentLocker missing lock timeout detection and panic recovery — now delegates to lock.WithEnvironmentLock (single implementation, no duplication) (#S2-audit B1+B2+H1)
- Circuit breaker for ArgoCD sync not wired in main.go — CircuitBreakerSyncClient now wraps raw SyncClient (3 failures → OPEN, 2 recoveries → CLOSED) (#S2-audit B4)
- Git trailers in gitops-push using build-id for all 3 values — Correlation-ID now propagated end-to-end from CLI request through to git commit (#S2-audit B3)
- Silent error swallowing in CancelBuild workflow cancel (`_ = cancelErr`) — now logged via slog.Warn (#S2-audit C1)
- Silent error swallowing in activation failure SSE emission (`_ = err`) — now logged via slog.Error (#S2-audit C2)
- IsBuildTerminal reimplemented locally in adapter instead of using statemachine.IsBuildTerminal() (#S2-audit C3)
- Idempotency store error silently swallowed — now logged with key context (#S2-audit C4)
- Operator deployment missing /tmp volumeMount with readOnlyRootFilesystem: true (#S2-audit H5)
- `--app` partial deploy generates wrong hostname (isMonorepo=false when deploying 1 app from monorepo) — always use per-app hostname format for consistency (#S2-dogfood F-02)

### Changed
- `test:unit` task now includes CLI tests (`cd cli && npm test`) (#S2-audit H2)
- `lint` task now includes CLI linting (`cd cli && npx eslint src/`) (#S2-audit M8)
- `validate:manifests` task now validates monitoring manifests in `infra/helm/monitoring/manifests/` (#S2-audit H3)

### Removed
- Dead code `CallbackURL` field from `WorkflowSpec` and `SubmitBuildRequest` — WorkflowSubmitter uses config-level CallbackURL (#S2-audit M1)

### Fixed
- Build stuck in RUNNING permanently when ArgoCD sync fails — split OnBuildCompleted into two-phase transaction (DA-17): Phase 1 commits build SUCCEEDED independently, Phase 2 handles release activation with graceful failure (#S2-dogfood F-01)
- Health probe failing on fresh deploys due to insufficient retries — extended default backoffs from 3 attempts/6s to 10 attempts/~120s, matching spec §10.1 activation timeout (#S2-dogfood F-03)
- ArgoCD TriggerSync error propagating as hard error and rolling back entire transaction — sync failures now treated as probe failures, allowing release to be marked SYNC_FAILED instead of silently disappearing (#S2-dogfood F-04)

### Added
- Multi-app monorepo manifest generation in Argo Workflow — per-app Deployment, Service, and IngressRoute with correct ports and start commands (#S2-monorepo)
- Per-app callback payload from Argo Workflow — includes `type` field for each app, enabling frontend/worker skip logic (#S2-monorepo)
- `path` field on `AppSpec`/`AppInput` structs — CLI sends app path from loopzero.yaml through to workflow for correct monorepo `cd apps/<name> && npm start` commands (#S2-monorepo)
- `type` field on `AppRelease` struct — callback apps now carry type info for health probe skip decisions (#S2-monorepo)
- `parseLoopZeroYaml` unit tests covering secrets parsing, multi-app, malformed YAML (#S2-monorepo)
- Traefik IngressRoute generated in workflow manifests — deploys now get a real URL `http://{project}.{tenant}.loopzero.local` (#S2-QA2 F-02)
- CLI handles `build.failed` SSE event as terminal — exits immediately with code 2 and actionable error instead of waiting 5min timeout (#S2-QA2 F-03)
- Auto-generated Dockerfile for Node.js projects — Kaniko builds succeed without user-provided Dockerfile, respects user Dockerfile if present (#S2-QA3 F-01)
- `validate-workspace` step in build pipeline — fails fast (~2s) on missing package.json or lockfile instead of 3 retries (~180s) (#S2-QA3 F-05)
- SSE client max retries (default: 15) — prevents infinite reconnection loops on persistent failures (#S2-QA3 F-07)
- CLI handles `deploy.sync_failed` SSE event as terminal — exits with code 3 and reason instead of 5min hang (#S2-QA6 F-13)
- Health prober supports Ingress URL override via `WORKLOAD_INGRESS_URL` — probes through Traefik with Host header, no DNS needed (#S2-QA6 F-14)
- k3d workload cluster exposes Traefik HTTP on host port 8090 — apps reachable at `localhost:8090` with Host header (#S2-QA6 F-14)
- Centralized Grafana Home dashboard with health status for all system components — single entry point for operational visibility (#S7-observability)
- Drift Detection dashboard with timeline, mirror status table, and reconciler health panels (#S7-observability)
- Failure Investigation dashboard with error timeline, lock contention, and dependency health panels (#S7-observability)
- ServiceMonitors for Gitea, MinIO, and Tenant Controller — Prometheus auto-discovers and scrapes metrics (#S7-observability)
- Cross-cluster metrics scraping for Argo Workflows (build cluster) and Traefik (workload cluster) via NodePort + additionalScrapeConfigs (#S7-observability)
- ArgoCD metrics enabled with ServiceMonitor auto-creation via Helm chart (server, controller, repo-server) (#S7-observability)
- 23 PrometheusRule alerting rules — 11 critical + 12 warning — all with runbook annotations per spec §15.5 (#S7-observability)
- Alertmanager enabled with severity-based routing (critical: 10s group_wait, warning: 30s group_wait) (#S7-observability)
- Taskfile tasks: `monitoring:install`, `monitoring:validate-targets`, `monitoring:status` (#S7-observability)
- Navigation links between all 6 Grafana dashboards (hub-and-spoke from Home) (#S7-observability)
- Real `PostgresSecretStore` replaces placeholder `EmptySecretStore` — queries PostgreSQL `secrets` table for metadata, never exposes values (#S3-secrets)
- Migration `000005_secrets.up.sql` creates `secrets` table with unique constraint on `(tenant_id, project_id, environment, app, name)` (#S3-secrets)
- CLI deploy command now shows real-time progress during deploy: build started/completed, health checks, and app live events via DeployRenderer (F-04)
- `LoopZeroError.Unwrap()` method enables `errors.Is`/`errors.As` chains through the `Cause` field; `Wrap()` now preserves the original error as cause (#G19)
- Exported `IsBuildTerminal()` in `pkg/statemachine` — single source of truth for build terminal state checks (#G2)
- Dedicated unit tests for `source/validator.go` covering valid config, empty content, invalid YAML, missing name, no apps, server without port, invalid app type, worker and frontend without port (#G16)
- Unit tests for `allConditionsTrue()` verifying PriorityClassReady is required for Active transition (#G1)
- CLI adapter modules: `cli/src/adapters/git.ts`, `source-bundle.ts`, `source-upload.ts`, `loopzero-yaml.ts` — extracted from index.ts (#G7)

### Changed
- Health probe now skips frontend apps in addition to workers — Next.js and other frontends don't have `/health` endpoints (#S2-monorepo)
- Dockerfile generation uses `COPY . .` before `npm ci` — monorepo workspace resolution works correctly (#S2-monorepo)
- Deploy timeout increased from 5 to 10 minutes — monorepo builds with 4 apps need more time (#S2-monorepo)
- Image tag now uses real git commit SHA instead of hardcoded `dev-local` — K8s pulls new image on each deploy (#S2-QA2 F-01)
- `commit_sha` flows from CLI → API → Argo Workflow → Kaniko image tag — full pipeline wired (#S2-QA2 F-01)
- Deploy output shows `deployed` instead of `deployed (background worker)` for apps without URL (#S2-QA2 F-05)
- `build_duration_seconds` in SSE `deploy.completed` uses wall-clock time from build creation instead of always-zero callback value (#S2-QA2 F-04)
- `deploy.completed` SSE omits `url` field when empty instead of sending `""` — consistent null representation (#S2-QA2 F-09)
- Workflow callback includes generated URL in payload when build succeeds (#S2-QA2 F-02)
- Build failure error details propagated from Argo Workflow → API callback → SSE → CLI — "Build failed: build-image: Dockerfile not found" instead of "Build failed: failed" (#S2-QA3 F-02)
- SSE client filters comment lines (`:heartbeat`) per SSE spec — no more spurious `type=message` events (#S2-QA3 F-03)
- Argo callback includes `{{workflow.failures}}` details (failed step name + message) when build fails (#S2-QA3 F-02)
- `CancelBuild` and `OnBuildCompleted` now execute ALL state transitions inside the environment lock — eliminates race conditions between concurrent cancel+deploy and duplicate callbacks (#S2-review)
- PostgreSQL connection pool configured per spec §7.8: `MaxConns=20`, `MinConns=5`, `MaxConnIdleTime=30s`, `statement_timeout=30s`, `ConnectTimeout=5s` — prevents runaway queries and pool exhaustion (#S2-review)
- Circuit breaker `ErrPlatformDegraded` now handled in deploy flow — ArgoCD unavailable triggers fast-fail SYNC_FAILED instead of 2-minute timeout (#S2-review)
- `bin/loopzero` entrypoint now calls `program.parseAsync()` — CLI executes correctly when invoked via npm-installed binary (#S2-dogfood F-02)
- `git bundle create` changed from `-1 HEAD` (shallow, 1 commit) to `--all HEAD` (full history) — first deploys to fresh repos now work (#S2-dogfood F-01)
- `loopzero init` now scans first-level subdirectories when root has no detectable framework — projects with `api/`, `worker/` subdirs generate correct loopzero.yaml (#S2-dogfood F-04)
- Error message for source upload failures changed to actionable "Failed to upload source code. Ensure your git history is clean and try again." (#S2-dogfood F-05)
- Deploy "in progress" message no longer references `loopzero status` (Sprint 3) — now says "Wait for it to complete, or try again later." (#S2-QA F-06)
- `activateRelease()` decomposed from 180 lines into four focused sub-methods: `createRelease()`, `pushToGitOps()`, `waitForSync()`, `finalizeActivation()` — pure refactoring, no behavior change (#G15)
- Integration test `concurrent_builds_test.go` now imports adapter types from `api/internal/adapters/` instead of reimplementing them (#G17)
- `validate:manifests` task now validates infra/tilt/ deployment manifests and infra/helm/argocd/templates/ in addition to operators/config/ (#G8)
- `db:migrate` task now globs all `*.up.sql` migration files instead of hardcoding — automatically picks up new migrations (#G4)
- CLI `index.ts` reduced from 459 to 278 lines by extracting deploy wiring to adapter modules (#G7)
- CLI deploy action now uses `SSEClient` class instead of inline SSE implementation — eliminates DRY violation (#G6)
- E2E seed script (`infra/scripts/seed_e2e.go`) now delegates to `seed.Run()` package instead of duplicating SQL and CR logic (#G9)
- `PromotionSyncFailed` and `RollbackSyncFailed` marked as terminal states — eliminates dead-end states in state machines (#G5)

### Removed
- Duplicate state machines in `operators/internal/statemachine/` — all consumers now import from `pkg/statemachine/` (single source of truth) (#S2-review)
- Fake provisioners removed from production packages `operators/internal/gitea/fake.go` and `operators/internal/argocd/fake.go` — fakes moved to test-only files (#S2-review)
- Compiled binaries (`api/main`, `operators/main`, `cmd`, `scripts`) removed from git tracking — added to `.gitignore` (#S2-review)

### Fixed
- `parseLoopZeroYaml` secrets parser — leading whitespace was not stripped before removing YAML list dash, causing secret names like `      - DATABASE_URL` instead of `DATABASE_URL` (#S2-monorepo)
- CLI `getSecrets` response mapping — API returns `{name, environment, app}` objects but CLI was treating them as plain strings, causing all secrets to appear "missing" (#S2-monorepo)
- Argo Workflow `notify-api` sed delimiter — image URLs containing `/` broke the default sed `/` delimiter, now uses `|` (#S2-monorepo)
- Argo Workflow `generate-manifests` env vars — shell variables not exported to Node.js child process, causing `undefined` manifest directory (#S2-monorepo)
- Pre-existing lint errors fixed: `require()` imports replaced with ES imports in `loopzero-yaml.ts`, unused variables removed in `deploy.ts` and `init.ts` (#S2-lint)
- SSE stream handler: all silent error returns now log the error — enables diagnosing connection drops (#S2-QA7 F-01)
- SSE stream handler: poll loop distinguishes context cancellation (client disconnect) from DB errors in logs (#S2-QA7 F-01)
- SSE client `reader.cancel()` now catches rejected Promise — prevents uncaught AbortError crash when `close()` is called during active stream (#S2-QA4 F-10)
- `validate-workspace` shell script: replaced `ls | while read` with `for/if` loop — `pipefail` no longer kills the step when not all lockfile types exist (#S2-QA5 F-11)
- Workflow error extraction uses `grep -o` instead of `sed` grouping — BusyBox compatible, with raw fallback (#S2-QA5 F-12)
- `TestSourceUpload` assertion key corrected from `"acme:"` to `"acme:my-saas"` — test now validates actual store key format (#S2-review)
- Integration test `concurrent_builds_test.go` adapter updated to implement `CancelBuild` method — tests compile again (#S2-review)
- SSE events renamed from `release.*` to `deploy.*` — `release.pending`, `release.active`, `release.health_check`, `release.sync_failed` no longer leak internal terminology to users (F-05)
- Deploy duration now calculated from build creation time instead of callback arrival — `duration_seconds` in `deploy.completed` SSE event reports real end-to-end time (F-06)
- Validation error message changed from `project_id is required` to `Project name is required. Check your loopzero.yaml.` — no internal field names in user-facing errors (F-07)
- CLI `--no-wait --json` now outputs absolute `stream_url` instead of relative path (F-08)
- `allConditionsTrue()` now includes `PriorityClassReady` — tenants no longer transition to Active if PriorityClass provisioning fails (#G1)
- `isBuildTerminal()` unified: removed hardcoded string version from SSE handler, both orchestrator and SSE now use `statemachine.IsBuildTerminal()` (#G2)
- Build callback goroutine now logs errors instead of silently discarding them via `_ =` (#G3)
- `loopzero deploy` no longer silently exits 0 when API is unreachable — now throws with exit code 3 and actionable message after 30s timeout (#F-01)
- `loopzero deploy` upload errors no longer leak raw JSON in user message — parses server error response and shows human-readable message with correct exit code (#F-02)
- CLI `init --json` now outputs pure JSON without mixed human text — detection info included as structured `detected` field (#F-01)
- CLI `init --json` now emits JSON error/warning when project creation fails (409 or connection error) (#F-02)
- CLI `init` differentiates 409 (project already registered — normal) from real API errors (connection refused, 500) (#F-03)
- CLI deploy now shows wall-clock duration instead of "0s" when server reports zero duration (#F-04)
- CLI `init --json` now emits JSON error with exit_code when not authenticated (#F-05)
- CLI `init --json` now emits JSON error for loopzero.yaml exists and other early-return paths (#F-07)
- CLI `login --json` now emits JSON errors on all 8 error paths (invalid token, CI missing token, server unreachable, browser auth failure, team resolution) instead of plain text (#F-11)
- CLI deploy JSON output now uses `null` instead of `""` for worker app URLs (#F-08)
- CLI deploy description now mentions `--json` flag in help output (#F-10)
- CLI deploy step comments renumbered sequentially (steps 9-15) — duplicate step 9 and step 10 labels corrected (#G12)
- CLI deploy `handleError` uses proper `instanceof ApiError` / `instanceof Error` checks instead of loose type assertion (#G13)
- CLI init warns user when API registration fails instead of silently swallowing the error (#G14)
- SSE client uses typed `ApiError` with exit codes instead of bare `Error`, enabling proper CLI exit code propagation on connection and polling failures (#G11)
- Unused `apiClient` variable removed from CLI login action wiring (#G10)
- CLI login detects network errors (ECONNREFUSED, ETIMEDOUT, ENOTFOUND) as server unreachable instead of misreporting "Authentication failed" (F-12)
- CLI login generates noreply email (username@users.noreply.github.com) when GitHub email is private, instead of saving bare username as email (F-13)
- CLI deploy command shows worker apps as "deployed (background worker)" instead of silently omitting them from output (F-09)
- CLI deploy auth error message changed from "Not logged in" to "Not authenticated" for consistency with whoami and init (F-06)
- CLI login distinguishes server errors (500) from auth failures (401), showing "Service temporarily unavailable" instead of "Authentication failed" when the API is down (F-07)
- CLI strips `github-` prefix from user ID when GitHub email is private, showing the username instead of the raw internal ID (F-03)
- Node.js ExperimentalWarning for `buffer.File` suppressed to prevent noise in deploy output (F-02)
- Fake deploy URLs now include environment name in the hostname (F-05)

### Added
- Adapters package (`api/internal/adapters/`) with PoolEnvironmentLocker, BuildOrchestratorAdapter, BuildStoreReaderAdapter, PgxTxDBTX, and fake external services for Sprint 2 wiring (#S2.wire)
- Sprint 2 dependencies wired in `api/cmd/main.go`: build orchestrator, SSE event store, source upload, secrets, and Argo callback routes are now active when DATABASE_URL is set (#S2.wire)
- E2E concurrent deploy rejection test: two simultaneous `loopzero deploy` to same environment yields exactly one exit 0 and one exit 3 with DA-18 message, concurrent deploys to different environments both succeed, forbidden internal terms verified absent from all output (#S2.22)
- E2E seed helper: idempotent script that creates test tenant, user, membership, project via PostgreSQL UPSERT, applies Tenant CR via kubectl, and waits up to 60s for namespace reconciliation; reusable `seed.Run()` package for BeforeEach integration (#S2.21a)
- ClusterCredentialController: reconciles ClusterCredential CRs, reads kubeconfig from referenced Secret, validates cluster connectivity via /healthz, creates/updates ArgoCD cluster secrets, periodic recheck every 5min, finalizer-based cleanup on deletion (#S2.0b)
- Ginkgo+envtest tests for ClusterCredentialController: 8 tests covering happy path, secret not found, cluster unreachable, invalid kubeconfig, late secret creation, deletion cleanup, periodic recheck, idempotency (#S2.0b)
- Argo WorkflowTemplate `loopzero-build-nodejs-v1`: DAG-based Node.js build pipeline with clone, cache restore, security scan, install, build, Kaniko image push, manifest generation, kubeconform validation, GitOps push with commit trailers, and finalize (onExit) with cache save, summary, and API callback (DA-15, retry 3x backoff 5s); all containers have resource limits, non-root security context, pinned image tags; cache keys include tenant-id for isolation (#S2.10)
- Integration tests for advisory lock crash recovery: connection termination via pg_terminate_backend releases lock immediately, panic inside locked fn releases lock via rollback, error-then-retry proves clean transaction state (#S2.14)
- Integration tests for concurrent build rejection: two simultaneous deploys yield exactly one 202 and one 409, idempotency replay returns cached response, idempotency mismatch returns 422, lock released after build completes allows next build (#S2.13)
- CLI SSE client with auto-reconnect, Last-Event-ID replay, event deduplication, and polling fallback on JOURNAL_EXPIRED (#S2.16)
- CLI pre-deploy secrets validation: checks declared secrets in loopzero.yaml against configured secrets via API before upload, exits 1 with missing list and docs URL (DA-8) (#S2.19b)
- GET /api/v1/secrets: endpoint returning secret names (never values) for pre-deploy validation, with EmptySecretStore placeholder for Sprint 2, tenant isolation, SecretStore interface (DIP) (#S2.19a)
- Circuit breaker for deploy sync operations: 3 consecutive failures open the circuit (503 PLATFORM_DEGRADED), 2 recovery successes close it, 30s health check interval, in-process state (DA-7) (#S2.20)
- GET /api/v1/builds/:id/stream: SSE streaming endpoint with Last-Event-ID replay, JOURNAL_EXPIRED detection, 15s heartbeat, poll-based event delivery, terminal state auto-close, client disconnect cleanup (#S2.9)
- POST /api/v1/source/upload: multipart source code upload with loopzero.yaml validation, app type enforcement (server requires port, worker/frontend do not), 500MB size limit, SourceStore interface (DIP) with fake for tests (#S2.6)
- `WithEnvironmentLock` advisory lock wrapper: transaction-scoped `pg_advisory_xact_lock` on (tenant, project, env) with 5s timeout, multi-tenant isolation via hashtext, typed `ErrLockTimeout` error (#S2.2)
- PriorityClass reconciliation in TenantController: plan-based PriorityClasses (starter=100/Never, growth=500/PreemptLowerPriority, enterprise=1000/PreemptLowerPriority), condition-gated, not deleted on tenant removal (#S14.3)
- ArgoCD Application reconciliation in TenantController: one Application CR per environment, condition-gated, with deletion cleanup (#S1.6b)
- ESLint configured for CLI TypeScript with @typescript-eslint, flat config format (ESLint 9+) (#S1.9)
- XDG_CONFIG_HOME support: `ConfigStore.default()` uses `$XDG_CONFIG_HOME/loopzero/` when set, falls back to `~/.loopzero/` (#S1.10)
- TenantController with full reconcile loop: namespaces, RBAC, NetworkPolicy, build namespace, Gitea repo provisioning, phase transitions (#S1.1-S1.8)
- Ginkgo+envtest test suite for TenantController: 44 tests covering creation, deletion, idempotency, partial failure recovery (#S1.8)
- CLI scaffold: TypeScript + Commander with `--json` global flag, semver versioning (#S1.9)
- CLI config store: `~/.loopzero/config.json` with 0600 permissions for token, email, team (#S1.10)
- CLI output formatter: human + JSON + CI mode detection (GitHub Actions, GitLab CI, CircleCI, Jenkins) (#S1.11)
- CLI API client: Bearer token injection, Correlation-ID (UUID v4), HTTP error → exit code mapping (#S1.12)
- `loopzero login --token`: validates GitHub token, multi-tenant resolution (auto/select/CI), exit codes 0/4/1 (#S1.13)
- `loopzero logout` and `loopzero whoami`: full lifecycle with human + JSON output (#S1.14)
- Monorepo tool detection: Turborepo, Nx, Lerna, pnpm, yarn, npm workspaces, flat detection (#S1.15)
- Framework detection: 10 frameworks (nextjs, vite, remix, nestjs, express, fastify, fastapi, django, gin, node) with priority-based detection (#S1.16)
- API HTTP server: chi router, Correlation-ID middleware, request logging, graceful shutdown, health + deep health endpoints (#S1.17)
- Database migration 000002: users, tenants, tenant_members, projects tables with FKs, composite PKs, unique constraints (#S1.18)
- Auth middleware: GitHub token validation with 5-minute cache, multi-tenant resolution (Model C), X-LoopZero-Tenant header (#S1.19)
- GET /api/v1/auth/whoami: returns user info with product language (team, not tenant) (#S1.20)
- POST /api/v1/projects: RFC 1123 name validation, project creation, Tenant CR bridge to TenantController (#S1.21)
- `loopzero init`: monorepo + framework detection, loopzero.yaml generation, .gitignore update, API project creation (#S1.22)
- Idempotency-Key middleware: SHA256 payload hash comparison, cached response replay, 422 on key reuse with different payload (#S1.21)
- Vertical slice E2E test: proves full convergence from API auth → project creation → Tenant CR → controller reconciliation → K8s objects (#S1.23)
- Public test helper packages `api/apitest` and `operators/operatortest` for cross-module E2E testing (#S1.23)
- k3d cluster configurations for 3-cluster topology: mgmt, build, workload with 2 agents each and shared registry (#S0.1)
- Taskfile with full cluster lifecycle, Helm install, DB migration, registry, and test tasks (#S0.2)
- Helm value files for all platform components with resource limits, security contexts, and health probes (#S0.3)
- CRD type definitions in Go: Tenant, PreviewEnvironment, AppSecret, ClusterCredential, ReconciliationReport (#S0.4)
- PostgreSQL schema with Go migrations (golang-migrate): 6 tables (builds, releases, environment_targets, audit_log, idempotency_keys, sse_events) (#S0.5)
- State machines in Go for Build, Release, Preview, Promotion, Rollback — strict transitions, invalid transitions panic (#S0.7)
- Go unit tests for naming, IDs, errors, state machines, CRD types, logging, schema validation, k3d configs, Helm values (#S0.8)
- Prometheus + Grafana monitoring stack with cluster health dashboard (#S0.10)
- OpenTelemetry Collector with OTLP receivers and Prometheus exporter (#S0.11)
- Structured JSON logging via slog (Go) with correlation_id support for both API and Operators (#S0.12)
- GitHub Actions CI pipeline with parallel jobs: test-pkg, test-go, test-infra, lint-go (golangci-lint), validate-manifests (#S0.13)
- Taskfile composite tasks: setup, test:unit, test:integration, test:e2e, test:e2e:chaos, lint, validate:manifests (#S0.14)
- Shared `pkg/` Go module with canonical errors, logging, and ID generation — single source of truth for cross-component code (#S0-review-2)
- Go workspace (`go.work`) for monorepo multi-module development (#S0-review-2)
- API entrypoint skeleton `api/cmd/main.go` — validates configuration at startup (#S0-review-2)

### Fixed
- SQL migration now uses IF NOT EXISTS for all CREATE TABLE, CREATE INDEX, and ADD CONSTRAINT — `task setup` is idempotent on re-runs (#S0-review-3)
- Workload cluster k3d config now includes shared registry `k3d-loopzero-registry:5000` — images built in build cluster can be pulled (#S0-review-3)
- `validate:manifests` task and CI job no longer swallow kubeconform failures with `|| true` (#S0-review-3)
- OTel Collector image tag pinned to `0.96.0` — no longer relies on implicit chart default (#S0-review-3)
- CI Go version aligned to `1.25` to match workspace and k8s.io/apimachinery requirement (#S0-review-3)
- golangci-lint upgraded from v1.62.2 to v2.11.3 — v1.x was built with Go 1.23 and cannot analyze Go 1.25 code (#S0-review-3)
- kubeconform version pinned to `v0.6.7` in CI instead of downloading `/releases/latest/` (#S0-review-3)
- `lint` task now fails properly instead of silently falling back to `go vet` (#S0-review-3)
- `.gitignore` comments corrected to reflect Go API (not TypeScript) (#S0-review-3)
- Health probe test now requires BOTH livenessProbe AND readinessProbe instead of permissive OR (#S0-review-3)
- Schema test validates migration idempotency (IF NOT EXISTS on all DDL statements) (#S0-review-3)

### Changed
- TenantController now uses state machine for all phase transitions — direct status assignment replaced by `tenantSM.Transition()` (#S1.7)
- Operator entrypoint now enables leader election by default with LeaderElectionID `loopzero-operators.loopzero.io` (#S1.1)
- Build namespace ResourceQuota now enforces `maxBuildConcurrency` via `count/workflows.argoproj.io` quota (#S1.5)
- Server route wiring now uses chi `r.Group()` for auth-scoped routes with idempotency middleware on mutating endpoints (#S1.17)
- `SetCondition` now accepts `observedGeneration` parameter — kubebuilder best practice for tracking resource generation (#S0-review-3)
- `PlanTier` and `TenantPhase` now have `+kubebuilder:validation:Enum` markers — CRD rejects invalid values (#S0-review-3)
- Argo Workflows controller now has readinessProbe in addition to livenessProbe (#S0-review-3)
- `dev:up` task now gracefully handles missing Tiltfile (#S0-review-3)
- Added composite `test` task that runs all test suites (#S0-review-3)
- API rewritten from TypeScript+Drizzle to Go — unified Go stack eliminates cross-language duplication (#S0-review)
- k3d clusters updated to 2 agents per cluster per spec §19.3 (#S0-review)
- k3d mgmt and build clusters now reference shared container registry (#S0-review)
- All 9 Helm values files now include readiness/liveness probes (#S0-review)
- CI pipeline updated: removed test-ts job, added API Go tests and infra tests (#S0-review)
- db:migrate task updated from npx drizzle-kit to psql with golang-migrate SQL (#S0-review)
- Errors, logging, and ID generation extracted from api/ and operators/ into shared `pkg/` module — eliminates DRY violation (#S0-review-2)
- `crds:install` task now applies CRD manifests via kubectl instead of placeholder (#S0-review-2)
- CI lint jobs upgraded from `go vet` to `golangci-lint` for deeper static analysis (#S0-review-2)
- `test:unit` task now includes `pkg/` tests (#S0-review-2)

### Removed
- TypeScript API code (package.json, drizzle.config.ts, Pino logger, Jest tests) — replaced by Go (#S0-review)
- Duplicated `errors/`, `logging/`, and `id/` packages from api/ and operators/ — replaced by `pkg/` (#S0-review-2)
