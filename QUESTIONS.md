# QUESTIONS.md — Architectural & Technical Review

> **Reviewer:** Claude (Tech Lead / Code Reviewer)
> **Date:** 2026-03-18
> **Scope:** Full codebase audit — CLI, API, Operators, Infra, Tests, Docs
>
> Answer each question inline below the `> Answer:` block. I'll use your answers to prioritize and implement improvements.

---

## Table of Contents

- [A. Architecture & Design](#a-architecture--design)
- [B. CLI Component](#b-cli-component)
- [C. API Component](#c-api-component)
- [D. Operators & State Machines](#d-operators--state-machines)
- [E. Infrastructure & DevEx](#e-infrastructure--devex)
- [F. Security](#f-security)
- [G. Testing](#g-testing)
- [H. Documentation & Process](#h-documentation--process)
- [I. Performance & Scalability](#i-performance--scalability)

---

## A. Architecture & Design

### A-01: Idempotency key generation is non-deterministic

The CLI generates a new random `Idempotency-Key` on every `loopzero deploy` invocation (`crypto.randomUUID()`). Per spec §4.2, idempotency protects against concurrent-build rejection — but if the user retries the same deploy, they get a different key, so the API treats it as a new request. Should the idempotency key be deterministic (e.g., hash of `commit + environment + project + timestamp-bucket`) so that retries within a window are truly idempotent?

> Answer:

### A-02: Two circuit breaker implementations exist

`api/internal/services/` has both an in-memory `CircuitBreakerSyncClient` and a PG-backed `PGCircuitBreakerSyncClient`. `main.go` uses the PG-backed one, making the in-memory version dead code. Should the in-memory implementation be removed entirely, or is it kept intentionally for unit testing or single-replica scenarios?

> Answer:

### A-03: Release state machine has "sink states" with no outgoing transitions

`DRIFT_ALERT` and `ROLLBACK_FAILED` are non-terminal states (not listed in `IsReleaseTerminal()`) but have zero outgoing transitions. Any attempt to transition from these states will panic. Per spec §5.2, these require "manual intervention" — but there's no mechanism (API endpoint, CLI command, or operator reconciliation) that performs this intervention. Should these be:
- (a) Made terminal states, or
- (b) Given explicit transitions (e.g., `ManualResolve` event → `ACTIVE`), or
- (c) Left as-is with documentation that they're intentional dead ends?

> Answer:

### A-04: SSE timeout fires mid-build instead of resetting on events

`cli/src/commands/deploy.ts` sets a single `setTimeout(DEPLOY_TIMEOUT_MS)` at SSE connection start. If a build takes longer than the timeout (e.g., 11 minutes with a 10-minute timeout), the timeout fires even though events are actively streaming. Should the timeout reset on every received event (activity-based timeout) instead of being absolute?

> Answer:

### A-05: Callback goroutine uses `context.Background()` — ignores shutdown

In `routes/builds.go`, the Argo callback handler spawns `go func() { ctx := context.Background(); ... }()`. This goroutine ignores API shutdown signals. If the API is shutting down, this goroutine runs indefinitely. Should it use a shutdown-aware context (e.g., derived from a `context.WithCancel` created at server startup)?

> Answer:

### A-06: No request body size limit on callback endpoint

`routes/builds.go` reads the full callback body with `io.ReadAll(r.Body)` without a size limit. A malicious or buggy Argo callback could send a multi-GB payload, exhausting API memory. Should we add `http.MaxBytesReader(w, r.Body, 10*1024*1024)` (10MB cap)?

> Answer:

### A-07: Build/Release ID entropy is only 32 bits

`pkg/id/id.go` generates IDs like `build-{8hexchars}` using only 4 random bytes (32-bit entropy). By the birthday paradox, collision probability reaches 50% at ~65K IDs. At scale (millions of deploys), this is a real risk. Should we increase to `randomHex(8)` (64 bits) or use full UUIDs?

> Answer:

### A-08: Idempotency key cleanup job is defined but never scheduled

`db/idempotency_store.go` has a `Cleanup()` method that deletes expired keys, but nothing calls it. The `idempotency_keys` table will grow unbounded. Should we add a background goroutine (like `build_reaper`) or a Kubernetes CronJob to periodically clean up?

> Answer:

### A-09: Health probes run sequentially for multi-app deploys

`services/release_activator.go` probes each app's health endpoint sequentially. For a monorepo with 10+ apps, this could take minutes (each app gets multiple retries with exponential backoff). Should probes run in parallel with a concurrency limit (e.g., semaphore of 5)?

> Answer:

### A-10: `WriteTimeout` intentionally omitted from HTTP server for SSE

`server/server.go` omits `WriteTimeout` because SSE streams are long-lived. This means a slow client could hold a connection indefinitely without reading. Should non-SSE routes use `http.TimeoutHandler` as a wrapper, while SSE routes remain unbounded?

> Answer:

---

## B. CLI Component

### B-01: Custom YAML parser instead of library

`cli/src/adapters/loopzero-yaml.ts` implements a home-grown YAML parser with regex and hardcoded indentation assumptions (exactly 2-space indent). It doesn't handle tabs, multiline strings, comments, or YAML anchors. A `loopzero.yaml` with 4-space indentation silently misparses. Should we replace this with `js-yaml` (~10KB gzipped) or `yaml` from npm?

> Answer:

### B-02: `loopzero init` doesn't validate git repository presence

Running `loopzero init` outside a git repo succeeds and generates `loopzero.yaml`. But `loopzero deploy` later fails because it needs git for source bundling. Should `init` check for `.git` directory and fail early with "Not a git repository. Run: git init"?

> Answer:

### B-03: Source bundle includes full git history

`cli/src/adapters/git.ts` creates a git bundle with `--all HEAD`, including the entire commit history. For large repos (10K+ commits), this is slow and bandwidth-heavy. Should we use `git archive` (just files, no history) or `git bundle --shallow` to limit scope?

> Answer:

### B-04: Source upload loads entire bundle into memory

`cli/src/adapters/source-upload.ts` uses `fs.readFileSync()` to load the entire git bundle before uploading. For repos >100MB, this causes OOM. Should we use streaming `FormData` with `fs.createReadStream()`?

> Answer:

### B-05: Token stored in plaintext on disk

`cli/src/config/store.ts` saves the GitHub token to `~/.loopzero/config.json` with `0o600` permissions but no encryption. On shared machines or if a backup captures the file, the token is exposed. Should we integrate with OS keychains (macOS Keychain, Linux Secret Service, Windows Credential Manager) via `keytar` or similar?

> Answer:

### B-06: File permissions race condition in config store

`store.ts` writes the config file then calls `chmod(0o600)`. Between write and chmod, the file may be readable by other users (depends on umask). Should we use `fs.writeFileSync(path, data, { mode: 0o600 })` for atomic permission setting?

> Answer:

### B-07: `LOOPZERO_API_URL` silently defaults to `localhost:3001` in production

`cli/src/config/constants.ts` falls back to `http://localhost:3001` if `LOOPZERO_API_URL` is not set. In production, this causes silent failures instead of a clear error. Should the CLI error out with "LOOPZERO_API_URL not configured. Set it via `loopzero config set api-url <url>`" when no URL is configured?

> Answer:

### B-08: SSE client reconnect uses constant delay instead of exponential backoff

`cli/src/api/sse-client.ts` reconnects with a fixed 1-second delay. If the server is down, this hammers it with 1 req/s up to `maxRetries`. Should we implement exponential backoff (1s, 2s, 4s, 8s, ..., max 60s)?

> Answer:

### B-09: No SIGINT/SIGTERM signal handlers for cleanup

The CLI has no signal handlers. If the user presses Ctrl+C during deploy, SSE connections, temp files (git bundles), and progress spinners are not cleaned up. Should we add `process.on('SIGINT', ...)` and `process.on('SIGTERM', ...)` to close resources gracefully?

> Answer:

### B-10: JSON output uses pretty-printing (`JSON.stringify(data, null, 2)`)

`cli/src/output/formatter.ts` formats JSON with 2-space indentation. Spec §0.3 doesn't specify compact vs pretty, but strict JSON consumers may break on multi-line output, and piping through `jq` adds unnecessary processing. Should `--json` output be compact (single line)?

> Answer:

### B-11: ANSI colors not gated on TTY detection

`formatter.ts` checks `isCI` to strip colors but doesn't check `process.stdout.isTTY`. Redirecting output to a file (`loopzero deploy > log.txt`) includes ANSI escape codes. Should we also gate colors on TTY detection?

> Answer:

### B-12: `parseInt` without `isNaN` validation for env var timeouts

`cli/src/config/constants.ts` parses env vars like `LOOPZERO_API_TIMEOUT` with `parseInt()` but doesn't check for `NaN`. Setting `LOOPZERO_API_TIMEOUT=abc` results in `NaN` timeouts, causing unpredictable behavior. Should we validate and fall back to defaults?

> Answer:

### B-13: Exit code 3 for 409 Conflict seems wrong

In `cli/src/api/client.ts`, HTTP 409 (concurrent deploy rejection) maps to exit code 3 (deploy failure). But 409 means "another deploy is already running" — it's a usage/timing issue, not a deploy failure. Should 409 map to exit code 1 (usage error) instead?

> Answer:

### B-14: Browser OAuth flow is dead code

`cli/src/commands/login.ts` has a `browserOAuthFlow()` method (lines 86-131) that is never called from `index.ts`. No dependencies are wired for it. Should this be removed, or is it planned for a future sprint? If planned, should it be in a separate branch?

> Answer:

### B-15: `ExperimentalWarning` suppression hijacks `process.emit()`

`index.ts` lines 3-10 monkey-patch `process.emit()` to suppress Node's experimental warnings. This could mask important warnings from dependencies and break with Node.js internal changes. Should we use `NODE_OPTIONS=--no-warnings=ExperimentalWarning` instead?

> Answer:

### B-16: No validation of project names in `loopzero init`

User-provided project names (`--name`) are not validated against K8s namespace rules (lowercase, alphanumeric + hyphens, max 63 chars). Invalid names pass `init` but fail at deploy time. Should `init` validate using the same rules as environment names?

> Answer:

---

## C. API Component

### C-01: No rate limiting on any endpoint

The API has no rate limiting. A malicious client could flood `/builds` or `/callback` endpoints, causing resource exhaustion. Should we add rate limiting middleware (e.g., per-tenant, per-IP) at least on mutation endpoints?

> Answer:

### C-02: No request context deadline on route handlers

Route handlers don't set `context.WithTimeout()` on incoming requests. A long-running operation (health probe, sync wait) could run indefinitely if the client disconnects. Should all handlers set a deadline (e.g., 150s per spec §10.1)?

> Answer:

### C-03: Callback secret in env var with insecure default

`config.go` defaults `LOOPZERO_ARGO_CALLBACK_SECRET` to `"loopzero-dev-callback-secret"`. If the env var is missing in production, this insecure default is silently used. Should the API refuse to start if the callback secret is the default value and `NODE_ENV=production`?

> Answer:

### C-04: SSE event store `GetAfter` returns unbounded results

`sse/stream_handler.go` calls `GetAfter(buildID, lastEventID)` which returns ALL events since the last ID. For long-running builds with thousands of events, this could cause memory spikes. Should we add pagination (e.g., `LIMIT 1000`)?

> Answer:

### C-05: Metrics endpoint has no authentication

The Prometheus metrics server on port 9090 has no auth. In Kubernetes, this is typically internal-only, but misconfiguration could expose operational data. Is this acceptable, or should we add basic auth or restrict to pod-local access?

> Answer:

### C-06: SSE event pruning has no background scheduler

The SSE event store has cleanup logic but no scheduled invocation. Events accumulate in the `sse_events` table indefinitely. Should we add a periodic cleanup goroutine (similar to `build_reaper`) that prunes events older than N hours?

> Answer:

### C-07: Fallback callback parsing uses string matching instead of proper JSON

`routes/builds.go` `fallbackParseCallback()` uses `strings.Index()` to extract values from malformed JSON. This is unreliable with escaped quotes or nested structures. Should we just default to "failed" with a generic error instead of attempting manual string parsing?

> Answer:

### C-08: Graceful shutdown doesn't wait for in-flight callbacks

When the API shuts down, it stops accepting new requests and drains existing HTTP connections. But callback goroutines spawned with `go func() { ... }()` are not tracked. Should we use a `sync.WaitGroup` to track in-flight callbacks and wait for them during shutdown?

> Answer:

### C-09: Database pool not closed on shutdown

`main.go` creates a `pgxpool.Pool` but I didn't see explicit `pool.Close()` in the shutdown path. If the pool isn't closed, connections may linger. Is `pool.Close()` called somewhere I missed, or should it be added to the shutdown sequence?

> Answer:

### C-10: Error message capitalization is inconsistent

Some API error messages start with uppercase ("Failed to read request body"), others with lowercase ("callback reported success but provided no apps"). Should we standardize on one style?

> Answer:

---

## D. Operators & State Machines

### D-01: Naming module exists but isn't used consistently

`operators/internal/naming/` provides `NamespaceName()`, `BuildNamespaceName()`, etc. with RFC 1123 validation. But `tenant_namespaces.go` creates namespace names with raw `fmt.Sprintf("%s-%s", tenant.Name, env.Name)` — bypassing validation. Should all namespace/resource name creation go through the naming module?

> Answer:

### D-02: CRD types lack `kubebuilder:validation` markers

CRD types in `operators/api/v1/` don't have validation markers like `// +kubebuilder:validation:MinLength=1` or `// +kubebuilder:validation:Required`. This pushes all validation into controllers, increasing the attack surface. Should we add CRD-level validation for required fields, min/max lengths, enum values, and format constraints?

> Answer:

### D-03: Status update errors are silently swallowed in TenantController

When `r.Status().Update()` fails in the TenantController (e.g., network error), the error is discarded (`_ = r.Status().Update(ctx, &tenant)`). While Kubernetes reconciliation is idempotent (it'll retry), the silent error swallowing makes debugging harder. Should we at least log these failures?

> Answer:

### D-04: `allConditionsTrue()` hardcodes the required condition list

The function that checks if all reconciliation steps are done has a hardcoded list of condition names. If a new step is added (e.g., "SecretStoreReady"), the list must be manually updated. Should we extract this to a const slice or derive it from the reconciliation steps?

> Answer:

### D-05: Gitea URL is hardcoded in both `main.go` and `tenant_gitops.go`

The Gitea in-cluster URL (`http://gitea-http.loopzero-system.svc.cluster.local:3000`) appears in at least two places. If the Gitea deployment changes namespace or service name, both must be updated. Should this be a single configurable constant or env var?

> Answer:

### D-06: TenantController deletion doesn't log non-fatal failures

When deletion steps fail non-fatally (ArgoCD app deletion, Gitea repo archival), the code continues with `// Non-fatal: continue with cleanup` but doesn't log the failure. Should we add explicit warning logs?

> Answer:

### D-07: No concurrent reconciliation tests for operators

Controller tests don't verify behavior when multiple reconcile loops run simultaneously (e.g., rapid spec updates). Could concurrent reconciliation cause duplicate namespace creation or RBAC races? Should we add tests for this?

> Answer:

### D-08: Operator runs as single replica with no documentation on HA

The operator deployment uses 1 replica. Leader election is enabled (good), but there's no documentation about running 2-3 replicas for HA. Should we document the HA strategy and update the deployment manifest to support it?

> Answer:

---

## E. Infrastructure & DevEx

### E-01: All dev credentials are hardcoded in VCS-tracked files

`loopzero-dev-password` appears in: `Taskfile.yml`, `postgresql/values.yaml`, `gitea/values.yaml`, `minio/values.yaml`, `monitoring/values.yaml`, `api-deployment.yaml`, `provision-api-tokens.sh`. These are marked "DEV-ONLY" but are still in git history forever. Should we:
- (a) Move to `Taskfile.local.yml` (git-ignored) and reference via env vars?
- (b) Use SOPS/sealed-secrets for encrypted values?
- (c) Accept this for local dev and document the risk?

> Answer:

### E-02: Cross-cluster IPs are hardcoded and patched by fragile sed scripts

`api-deployment.yaml` and `monitoring/values.yaml` contain hardcoded Docker network IPs (e.g., `10.0.2.7:30000`) that are updated by `update-cross-cluster-ips.sh` using `sed`. These IPs change on every cluster recreate. Should we use DNS-based service discovery (CoreDNS, ExternalName services) instead?

> Answer:

### E-03: `tilt down` used to destroy stateful infrastructure (PostgreSQL, Gitea, MinIO)

This was fixed in this session (F-02), but the broader question: should Tilt manage infrastructure at all, or should `task setup` be the sole owner of infrastructure lifecycle? What's the intended separation of concerns?

> Answer:

### E-04: PostgreSQL has no replication, backup, or HA

PostgreSQL runs as a single instance with no replication, no WAL archiving, and no backup CronJob. Loss of the PostgreSQL pod means total data loss. Is this acceptable for the current development phase, or should we implement at minimum WAL archiving to MinIO?

> Answer:

### E-05: MinIO runs in standalone mode (no replication)

MinIO runs a single instance. All cached build artifacts and source bundles are lost if the pod dies. Should we switch to distributed mode (4+ nodes) or accept data loss for the cache layer?

> Answer:

### E-06: Gitea uses SQLite backend

Gitea is configured with SQLite, limiting it to single-instance. For HA or multi-replica Gitea, PostgreSQL backend is required. Should we switch Gitea to use the existing PostgreSQL instance?

> Answer:

### E-07: CWT `cwt-kaniko.yaml` disables TLS verification

Kaniko uses `--insecure` and `--skip-tls-verify` for the local registry. This is fine for k3d but a security risk if templates are reused in production. Should we make TLS verification configurable (on in prod, off in dev)?

> Answer:

### E-08: CWT `cwt-manifests-generate.yaml` hardcodes `NODE_ENV=production`

The manifest generator sets `NODE_ENV: "production"` for all environments, including staging and preview. Should this match the target environment name?

> Answer:

### E-09: `cwt-notify-api.yaml` sends unbounded payload size

The notify callback sends generated Kubernetes manifests in the HTTP body. For large monorepos (50+ apps), this could exceed HTTP body limits. Should we compress the payload or send manifests via a separate artifact (MinIO presigned URL)?

> Answer:

### E-10: Argo Workflows has no resource quotas per workflow

A single build workflow can consume all available cluster resources (CPU, memory). Should we add Argo Workflow-level resource limits or Kubernetes ResourceQuotas on the `argo` namespace?

> Answer:

### E-11: No NetworkPolicies defined for any namespace

There are no NetworkPolicies restricting traffic between namespaces. Any pod in any namespace can reach any service. Should we add deny-all + allow-list NetworkPolicies at least for `loopzero-system` and `argocd` namespaces?

> Answer:

### E-12: Datreeio CRD catalog in `cwt-manifests-validate.yaml` fetched at build time

`kubeconform` downloads schemas from `https://raw.githubusercontent.com/datreeio/CRDs-catalog` during every build. If GitHub is down, validation fails and blocks all builds. Should we cache the schemas locally (ConfigMap or init container)?

> Answer:

### E-13: `provision-api-tokens.sh` creates a new Gitea token every run

The script creates `api-$(date +%s)` tokens on each invocation. Over time, Gitea accumulates stale tokens. Should we use a fixed token name with conditional update, or add cleanup of old tokens?

> Answer:

---

## F. Security

### F-01: No audit logging for any operation

There's no audit trail for who deployed what, when, or from which IP. For compliance (SOC 2, ISO 27001) and incident investigation, this is essential. Should we add structured audit logs for all mutations (deploy, cancel, secret changes)?

> Answer:

### F-02: TenantController ClusterRole is overly broad

The operator has cluster-wide permissions to create/delete Secrets, NetworkPolicies, ResourceQuotas, etc. A compromised operator could affect any namespace. Should we scope permissions to tenant-owned namespaces only (using namespace-scoped Roles instead of ClusterRoles)?

> Answer:

### F-03: No encryption at rest for Kubernetes secrets

K8s secrets in etcd are not encrypted by default. ArgoCD tokens, Gitea tokens, and database credentials are stored as base64 in etcd. Should we enable encryption at rest (`EncryptionConfiguration`) or use external secret stores (Vault, AWS Secrets Manager)?

> Answer:

### F-04: `git bundle` in CLI could include sensitive files

The git bundle includes all committed files. If a user accidentally committed `.env` or credentials, the bundle (and thus the build) exposes them. Should we add a pre-upload check that warns about potentially sensitive committed files?

> Answer:

### F-05: Token validation cache has no size limit

`middleware/auth.go` caches token validation results in a `sync.Map` with no eviction policy beyond TTL. A large number of unique tokens (e.g., from automated systems) could consume unbounded memory. Should we use an LRU cache with a maximum size?

> Answer:

---

## G. Testing

### G-01: No unit tests for `startup_reconciler.go`

The startup reconciler handles orphaned builds after API crashes — a critical recovery mechanism per spec §12.1. But it has no unit tests. Should we add tests for: API boot → finds orphaned builds → queries Argo Workflow state → finalizes or fails builds?

> Answer:

### G-02: No unit tests for `pg_circuit_breaker.go`

The PG-backed circuit breaker manages ArgoCD outage resilience (3 failures → circuit open → 503 PLATFORM_DEGRADED). But state transitions (closed → open → half-open → closed) have no unit tests. Should we add these?

> Answer:

### G-03: No config validation tests

Neither `api/internal/config/config.go` nor `cli/src/config/constants.ts` have tests. Invalid env vars (e.g., `LOOPZERO_API_TIMEOUT=abc`) cause `NaN` timeouts or silently wrong behavior. Should we add tests for config loading, validation, and defaults?

> Answer:

### G-04: CLI tests use mocks for everything — no real filesystem/git tests

All CLI test dependencies are mocked. This tests wiring but misses real-world issues (file permissions, git behavior, network timeouts). The `CLAUDE.md` says "NUNCA usar mocks/fakes/stubs" — but CLI tests heavily use them. Should we add integration tests that hit real git repos and real APIs?

> Answer:

### G-05: E2E tests only cover happy path

E2E tests (`infra/tests/e2e/`) only verify "deploy succeeds and app is accessible". Missing: build failure, deploy failure, sync timeout, preview environments, rollback, cancel, concurrent deploys, secret management. Should we expand E2E coverage for failure scenarios?

> Answer:

### G-06: No chaos engineering tests

No tests for: API pod restart during deploy, database connection pool exhaustion, Argo Workflow timeout, network partition between clusters. Should we add chaos tests (e.g., using Litmus, chaos-monkey, or simple `kubectl delete pod` scripts)?

> Answer:

### G-07: Sprint 2 docs folder has 390KB across 22 files

`docs/sprints/02-deploy-happy-path/` contains 22 files totaling ~390KB. It's unclear which are current vs. historical. There's no index or README. Should we consolidate, archive old files, and add a sprint README with navigation?

> Answer:

### G-08: Missing tests for preview environment lifecycle

The preview environment state machine exists in `pkg/statemachine/preview.go` with tests, but there are no controller tests or E2E tests for the actual preview environment workflow (PR → preview URL → cleanup on merge). Is this planned for Sprint 5, or should we add skeleton tests now?

> Answer:

---

## H. Documentation & Process

### H-01: `docs/technical.md` is 5,029 lines — hard to navigate

The spec is comprehensive but massive. Finding specific sections requires search. Should we:
- (a) Split into multiple files by topic (architecture.md, state-machines.md, cli-spec.md, etc.)?
- (b) Add a generated table of contents with anchor links?
- (c) Keep as-is since it's the "single source of truth"?

> Answer:

### H-02: README says some features are "planned" but code exists

README.md says "Additional commands like logs, rollback, secrets management are planned" — but sprint 2 already implements deploy, and sprint docs plan for these commands. Should the README be updated to reflect actual implementation status?

> Answer:

### H-03: No ADR (Architecture Decision Record) folder

Major decisions (why `pg_advisory_xact_lock` vs. session lock, why Gitea over GitHub as GitOps store, why Kaniko over BuildKit) are documented in comments or spec sections but not in a structured ADR format. Should we create `docs/adr/` with numbered decision records?

> Answer:

### H-04: CHANGELOG has duplicate `### Changed` sections

`CHANGELOG.md` has two `### Changed` blocks under `[Unreleased]` (lines 17 and 20). This is valid Markdown but looks like a copy-paste error. Should we merge them?

> Answer:

---

## I. Performance & Scalability

### I-01: PostgreSQL connection pool settings may be too conservative

Default pool config is `MaxConns: 10`, `MinConns: 2`. With concurrent deploys across multiple tenants, the pool could exhaust connections quickly (each deploy holds a connection for the advisory lock duration). Should we increase defaults or make them more dynamic?

> Answer:

### I-02: No database index strategy documented

Migrations create tables but indexing strategy isn't documented. Queries like "find active build for tenant+project+environment" rely on the partial unique index `idx_one_active_build_per_env`, but other common queries (list builds by project, list releases by environment) may lack indexes. Should we audit query patterns and add covering indexes?

> Answer:

### I-03: No connection pooling for Gitea/ArgoCD HTTP clients

The API creates new HTTP connections for every Gitea and ArgoCD API call. For high-throughput scenarios, this creates connection overhead. Should we use `http.Client` with connection pooling and keep-alive?

> Answer:

### I-04: Argo Workflow cache strategy could cause stale builds

Build cache is keyed by `{tenant}/{project}/cache-{language}`. If dependencies change (e.g., new npm package), the old cache is used until it's manually purged. Should we include a hash of the lockfile in the cache key?

> Answer:

### I-05: No pagination on list endpoints

`GET /api/v1/builds` and `GET /api/v1/secrets` return all results without pagination. For tenants with hundreds of builds, this causes slow responses and large payloads. Should we add cursor-based pagination?

> Answer:

---

## End of Questions

Total: **68 questions** across 9 categories.

Answer each question inline. I'll use your responses to create a prioritized implementation plan.
