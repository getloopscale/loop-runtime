# Flow Diagrams — Baseline

Generated: 2026-03-29

## Summary

- Total flows identified: 9
- Entry points: 9 (HTTP CLI: 7, Background workers: 3, Operator reconciler: 1, Webhook: 1)
- Flows with complete error handling: 6
- Flows with incomplete or partial recovery: 3
- Critical gaps identified: 4

---

## Flow 1: Container Deploy Flow (Most Critical)

### Overview
- Entry: `loopzero deploy` CLI command
- Exit: Running application at live URL; `deploy.completed` SSE event received by CLI
- Components: CLI, API BuildHandler, BuildOrchestrator, pg_advisory_xact_lock, Argo Workflows, BuildKit, Container Registry, Gitea (gitops), ArgoCD, ContainerDeliveryActivator, SyncCallbackActivator, ReleaseConvergenceLoop, SSE EventStore
- Estimated frequency: Very high
- Criticality: Critical

### Sequence Diagram — Happy Path

```
User -> CLI(deploy.ts): loopzero deploy --env production
CLI -> CLI: check auth token (getToken)
CLI -> CLI: findLoopZeroYaml() → parse config
CLI -> CLI: validateEnvironment(env)
CLI -> CLI: normalizeApps() → AppUnits
CLI -> API(GET /secrets): getSecrets(project, env) for pre-deploy check
API -> DB: list secret names for tenant/project/env
API -> CLI: [secret names]
CLI -> CLI: createGitBundle() → /tmp/bundle.git
CLI -> CLI: validate bundle size (< 500MB)
CLI -> API(POST /source): uploadSource(bundlePath, loopzeroYamlPath)
API -> S3(MinIO/Spaces): upload bundle
API -> Gitea(source repo): push bundle commit
API -> CLI: { commit_sha }
CLI -> CLI: cleanupFile(bundlePath)
CLI -> CLI: generateIdempotencyKey(commitSha, project, env, apps)
CLI -> API(POST /builds, Idempotency-Key: <key>): submitBuild(project, env, apps, commitSha)

API(builds.go) -> AuthMiddleware: validate Bearer token → GitHub API → UserContext
API(builds.go) -> IdempotencyMiddleware: check/record Idempotency-Key
API(builds.go) -> BuildHandler.Create: handle request
BuildHandler -> BuildOrchestrator.SubmitBuild(req):

  [PHASE 1 — INSIDE pg_advisory_xact_lock(tenant, project, env)]
  BuildOrchestrator -> lock.WithEnvironmentLock: acquire 5s timeout lock
  BuildOrchestrator -> BuildStore.HasActiveBuild(tx): concurrent build check (INSIDE lock)
  BuildStore -> DB: SELECT for QUEUED/RUNNING builds
  DB -> BuildStore: none found
  BuildOrchestrator -> BuildStore.Create(tx): INSERT build (status=QUEUED)
  BuildOrchestrator -> BuildStore.SetAppsSpec(tx): persist app specs
  BuildOrchestrator -> lock: COMMIT (lock released)
  [END PHASE 1]

  [PHASE 2 — OUTSIDE lock]
  BuildOrchestrator -> NormalizeApp: resolve AppUnits from AppSpec
  BuildOrchestrator -> CreateBuildPlan: deterministic image tags, paths
  BuildOrchestrator -> WorkflowSubmitter.Submit: submit Argo Workflow (wfSpec)
  WorkflowSubmitter -> ArgoWorkflowsAPI(build cluster): POST /apis/argoproj.io/v1alpha1/workflows
  ArgoWorkflowsAPI -> WorkflowSubmitter: workflowID
  BuildOrchestrator -> BuildStore.SetWorkflowID(nil, buildID, workflowID)
  BuildOrchestrator -> BuildStore.UpdateStatus(nil, buildID, BuildStarted): QUEUED → RUNNING
  BuildOrchestrator -> EventStore.Append(buildID, "build.started", {apps}): emit SSE
  [END PHASE 2]

BuildHandler -> CLI: 202 Accepted { build_id, stream_url }

CLI -> SSE(/api/v1/builds/{id}/stream): connectSSE(buildId)
SSE -> CLI: stream events...

  [BUILD PLANE — Argo Workflow in build cluster]
  ArgoWorkflow -> BuildKit: clone source, build image per app
  BuildKit -> ContainerRegistry(DO Registry): push image {tenant}/{project}-{app}:{commitSha}
  ArgoWorkflow -> MinIO/Spaces: upload static assets (if static app)
  ArgoWorkflow: workflow completes (Succeeded phase)

  [RECONCILE LOOP — polls build cluster from control plane]
  BuildReconcileLoop.reconcile: poll non-terminal builds every 30s
  BuildReconcileLoop -> WorkflowSubmitter.GetStatus(argo, workflowID)
  WorkflowSubmitter -> ArgoWorkflowsAPI: GET workflow status
  ArgoWorkflowsAPI -> WorkflowSubmitter: { phase: "Succeeded" }
  BuildReconcileLoop -> BuildStore.GetBuildApps(buildID): fetch stored app specs
  BuildReconcileLoop -> manifests.Render(RenderInput): generate K8s manifests in API
  BuildReconcileLoop -> BuildOrchestrator.OnBuildCompleted(buildID, result):

    [PHASE 1 — INSIDE lock — build terminal transition]
    BuildOrchestrator -> lock.WithEnvironmentLock: acquire lock
    BuildOrchestrator -> BuildStore.UpdateStatus(tx, BuildSucceededEvent): RUNNING → RELEASING
    BuildOrchestrator -> EventStore.Append(buildID, "build.succeeded")
    BuildOrchestrator -> lock: COMMIT
    [END PHASE 1]

    [STATIC ROUTING — OUTSIDE lock]
    BuildOrchestrator -> writeStaticRoutingEntries: for each static app
    BuildOrchestrator -> CFKVClient.Put(subdomain, {buildId, s3Prefix}): CF KV routing

    [PHASE 2 — ContainerDeliveryActivator.Activate]
    ContainerDelivery -> phaseACreateRelease (INSIDE lock): create Release(READY)
    ContainerDelivery -> phaseBDeliver (no lock): push manifests to Gitea gitops repo
    ContainerDelivery -> Gitea(gitops): PUT /repos/{tenant}/{gitops}/contents/{project}/{env}/manifests.yaml
    ContainerDelivery -> phaseCRecordResult (INSIDE lock):
      - set GitCommit on Release
      - Release: READY → PENDING
      - envTargetStore.SetDesired(release_id)
      - emit SSE "deploy.delivering"
    ContainerDelivery -> SyncClient.TriggerSync (3 retries, exponential backoff): trigger ArgoCD
    ContainerDelivery -> EventStore.Append("deploy.verifying"): emit SSE
    ContainerDelivery -> returns (async from here)

    BuildOrchestrator -> lock.WithEnvironmentLock: RELEASING → SUCCEEDED
    [END PHASE 2]

  [ASYNC ACTIVATION — push from ArgoCD or poll from convergence loop]
  ArgoCD(runtime cluster): detects gitops repo change, syncs manifests
  ArgoCD: Application → Synced + Healthy
  ArgoCD -> API(POST /internal/argocd-callback): ArgoCallbackPayload
  ArgoCallbackHandler -> SyncCallbackActivator.OnSyncCompleted(appName, revision)
  SyncCallbackActivator -> ReleaseStore.FindPendingReleases: find PENDING releases
  SyncCallbackActivator -> phaseEFinalize (INSIDE lock):
    - Release: PENDING → ACTIVE
    - envTargetStore.SetActive(release_id)
    - previous release: ACTIVE → SUPERSEDED
    - emit SSE "deploy.live"
    - emit SSE "deploy.completed" { deploy_id, status, duration, apps[] }

SSE -> CLI: deploy.completed event { status: "success", apps: [{url}] }
CLI -> User: display URL + deploy duration
CLI: exit 0
```

### State Transitions

```
Build:   QUEUED → RUNNING → RELEASING → SUCCEEDED
                           └─ [activation fail] RELEASING (retry loop)
         QUEUED → FAILED (workflow submit failure)
         RUNNING → FAILED (workflow failed)
         RUNNING → CANCELLING → CANCELLED

Release: READY → PENDING → ACTIVE
         READY → GIT_MIRROR_FAILED
         PENDING → SYNC_FAILED
         ACTIVE → SUPERSEDED (on next deploy)
         ACTIVE → ROLLING_BACK → ROLLED_BACK (on rollback)
```

### Error Paths

**Concurrent deploy (409):**
```
CLI -> API: POST /builds
API -> lock.WithEnvironmentLock: acquire
API -> BuildStore.HasActiveBuild: FOUND
API -> lock: release
API -> CLI: 409 { code: "DEPLOY_IN_PROGRESS" }
CLI -> User: exit 3
```

**Workflow submission failure:**
```
BuildOrchestrator -> ArgoWorkflowsAPI: submit (network error or quota exceeded)
ArgoWorkflowsAPI: error
BuildOrchestrator -> BuildStore.UpdateStatus(FAILED): QUEUED → FAILED
BuildOrchestrator -> EventStore.Append("build.failed")
SSE -> CLI: build.failed event
CLI -> User: exit 2
```

**GitOps push failure:**
```
ContainerDelivery -> Gitea: push manifests
Gitea: error (network, auth, repo not found)
ContainerDelivery -> ReleaseStore.UpdateStatus(GIT_MIRROR_FAILED): READY → GIT_MIRROR_FAILED
ContainerDelivery -> EventStore.Append("deploy.delivery_failed")
ContainerDelivery -> BuildOrchestrator: return error
BuildOrchestrator: build stays in RELEASING (reconcile loop retries)
SSE -> CLI: deploy.delivery_failed event → CLI exits 3
```

**ArgoCD sync trigger failure (3 retries exhausted):**
```
ContainerDelivery -> TriggerSync: 3 attempts, exponential backoff
All attempts fail
ContainerDelivery -> phaseEFinalize(verifyFailed=true):
  - Release: PENDING → SYNC_FAILED
  - emit SSE "deploy.sync_failed" { reason: HEALTH_PROBE_FAILED }
SSE -> CLI: deploy.sync_failed event
CLI -> User: exit 3
```

**API crash mid-flow (RELEASING recovery):**
```
API crashes while build is in RELEASING state
API restarts
BuildReconcileLoop.Start: immediate reconcile on boot
BuildReconcileLoop -> BuildStore.FindActiveBuilds: finds RELEASING build
BuildReconcileLoop -> retryReleaseActivation: check existing release
  → If no release: reconstruct from BuildStore.GetBuildApps → retry Activate
  → If release ACTIVE: completeBuild (RELEASING → SUCCEEDED)
  → If release SYNC_FAILED: failBuild (RELEASING → FAILED)
```

### Recovery Mechanisms

| Failure | Recovery |
|---------|----------|
| Workflow submission fails | Immediate FAILED transition; build reaper 5min backup |
| API crashes with QUEUED build | Build reaper after 5min: QUEUED → FAILED |
| API crashes with RUNNING build | Reconcile loop detects Argo status on restart |
| API crashes with RELEASING build | Reconcile loop retries release activation |
| GitOps push fails | build stays RELEASING, reconcile loop retries |
| ArgoCD sync slow | ReleaseConvergenceLoop polls every 10s, 180s timeout |
| SSE stream drops | CLI calls getBuildStatus fallback (BLOCKER-1 fix) |
| Duplicate idempotency key | Middleware returns cached 202 response |

### Concerns

1. **RELEASING build retry creates duplicate release guard is partial** (`retryReleaseActivation` checks for existing release but only handles ACTIVE/SYNC_FAILED/GIT_MIRROR_FAILED states — a READY or PENDING release from a previous interrupted attempt causes the loop to skip: "transient state, wait for it to resolve". If the ArgoCD sync was triggered but the release stays PENDING indefinitely due to a missing ArgoCD callback, the reconcile loop does not retry the sync trigger.)

2. **CF KV write happens before RELEASING→SUCCEEDED transition** — if the API crashes between the KV write and the lock acquisition for SUCCEEDED transition, the KV table points to a build that has no Release record. Static assets ARE served but no auditable release exists.

3. **`failRollback` acquires lock with empty tenant/project/env** (line 240 of rollback_orchestrator.go: `o.deps.Locker.WithLock(ctx, "", "", "", ...)`) — this is a lock on an empty key, which may not correctly serialize with other operations on the same environment.

---

## Flow 2: Static Frontend Deploy Flow

### Overview
- Entry: `loopzero deploy` CLI with `rendering: static` app
- Exit: Static assets served from DO Spaces via CF Worker
- Components: CLI, API, Argo Workflows, BuildKit, DO Spaces (S3), Cloudflare KV, StaticDeliveryActivator
- Estimated frequency: Medium
- Criticality: High

### Sequence Diagram — Happy Path

```
[Same CLI and build submission as Flow 1, up to workflow completion]

ArgoWorkflow -> BuildKit: build static assets (npm run build)
BuildKit -> DOSpaces(S3): upload assets to /{tenant}/{project}/{app}/{buildId}/
ArgoWorkflow: workflow completes (Succeeded phase)

BuildReconcileLoop -> onWorkflowSucceeded:
  For static app (rendering=static): Rendering="static" in BuildAppSpec
  BuildOrchestrator -> writeStaticRoutingEntries (OUTSIDE lock):
    BuildOrchestrator -> CFKVClient.Put(subdomain, { buildId, s3Prefix }): write routing entry
    Cloudflare KV: entry propagates to CF edge globally (< 1s typical)

  BuildOrchestrator -> delivery.Activate (StaticDeliveryActivator):
    StaticDelivery -> phaseACreateRelease (lock): create Release(READY)
    StaticDelivery -> phaseBDeliver: kvClient.Put(subdomain, RoutingEntry)
      [Note: KV write happens TWICE — once in writeStaticRoutingEntries, once here]
    StaticDelivery -> phaseCRecordResult (lock):
      - Release: READY → PENDING
      - envTargetStore.SetDesired
      - emit SSE "deploy.delivering"
    StaticDelivery -> phaseDVerify: emit "deploy.verifying", return false (always passes)
    StaticDelivery -> phaseEFinalize (lock):
      - Release: PENDING → ACTIVE
      - envTargetStore.SetActive
      - emit SSE "deploy.completed" { delivery: "static", url: "https://{subdomain}.{baseDomain}" }

SSE -> CLI: deploy.completed { status: "success", apps: [{ url, delivery: "static" }] }
CLI -> User: display URL
CLI: exit 0

[On user request]
User -> Browser: GET https://{subdomain}.{baseDomain}
Browser -> CloudflareEdge: request
CloudflareEdge -> CFWorker: route request
CFWorker -> CloudflareKV: GET subdomain → { buildId, s3Prefix }
CFWorker -> DOSpaces(CDN): GET s3Prefix/index.html
DOSpaces -> CFWorker: static asset
CFWorker -> Browser: response
```

### Concerns

1. **CF KV write is duplicated** — `writeStaticRoutingEntries` in BuildOrchestrator and `phaseBDeliver` in StaticDeliveryActivator both call `kvClient.Put` with identical data. This is harmless (idempotent) but wastes a KV write call and creates confusion about which path is authoritative.

2. **Static delivery has no ArgoCD dependency** — but the `StaticDeliveryActivator` struct still holds `sync` and `gitops` fields (unused for static apps). There is no container deploy path for static frontends. This is correct by design but the unused deps suggest incomplete cleanup.

3. **Rollback path exists** — `StaticDeliveryActivator.Rollback` updates CF KV to point to the previous build's S3 prefix. This works without any cluster involvement.

---

## Flow 3: Auth Flow

### Overview
- Entry: `loopzero login --token <github-token>`
- Exit: Token stored in `~/.loopzero/config.json`; API requests authorized
- Components: CLI, API auth middleware, GitHub API (token validation), DB membership store, local config store
- Estimated frequency: Low (once per session)
- Criticality: High (gates all other flows)

### Sequence Diagram — Happy Path

```
User -> CLI(login.ts): loopzero login --token ghp_xxx
CLI -> CLI: check opts.token exists
CLI -> API(POST /api/v1/auth/validate or GET /api/v1/auth/whoami): validateToken(token)
  [CLI sends token as Bearer in Authorization header]
  API -> AuthMiddleware: validate Bearer token
  AuthMiddleware -> GitHubAPI: GET /user (with token)
  GitHubAPI -> AuthMiddleware: { login, email, id }
  AuthMiddleware -> DB(membership_store): GetMemberships(userID)
  DB -> AuthMiddleware: [{ tenantID, role }]
  AuthMiddleware -> UserContext: inject into request context
  API -> CLI: { user_id, email, team, role }
CLI -> CLI: resolveTeam(teams, opts.team)
CLI -> LocalConfig(~/.loopzero/config.json): saveToken(token, email, team)
CLI -> User: "Authenticated as email (team: team)"
CLI: exit 0
```

### Token Validation Cache (API middleware)

```
Request -> AuthMiddleware: Bearer ghp_xxx
AuthMiddleware -> tokenCache: lookup(token)
  [HIT, not expired]: return cached UserContext
  [MISS or expired]:
    AuthMiddleware -> GitHubAPI: validate token
    AuthMiddleware -> DB: get memberships
    AuthMiddleware -> tokenCache: store(token, result, TTL=5min)
    AuthMiddleware: inject UserContext
```

### Error Paths

```
[Invalid token]
CLI -> API: validateToken(expired_token)
GitHub API -> AuthMiddleware: 401 Unauthorized
AuthMiddleware -> CLI: 401
CLI -> User: "Authentication failed. Token may be expired." → exit 4

[No teams]
GitHub API -> AuthMiddleware: valid user
DB -> AuthMiddleware: [] (no memberships)
AuthMiddleware -> CLI: 403 (no tenant)
CLI -> User: "No teams available for this account." → exit 1

[Multiple teams, no --team flag]
CLI: resolveTeam(["team-a", "team-b"], undefined) → null
CLI -> User: "Multiple teams available. Use --team to select." → exit 1
```

### Concerns

1. **Interactive multi-team selection is not implemented** — `LoginCommand.resolveTeam()` returns null when multiple teams exist and `--team` is not specified (line 103: "interactive prompt not yet implemented"). Users with multiple teams cannot log in without knowing the team name in advance.

2. **Token stored in plaintext** — `~/.loopzero/config.json` holds the raw GitHub token. No encryption at rest. If disk is compromised, the token is exposed.

---

## Flow 4: Init Flow (Project Registration)

### Overview
- Entry: `loopzero init` in a project directory
- Exit: `loopzero.yaml` written to disk; project registered in API
- Components: CLI, monorepo detection, framework detection, API projects route, DB
- Estimated frequency: Low (once per project)
- Criticality: Medium

### Sequence Diagram — Happy Path

```
User -> CLI(init.ts): loopzero init
CLI -> CLI: check auth token
CLI -> CLI: detect git repo (hasGitRepo)
CLI -> CLI: detectMonorepo(cwd) → tool + workspaces
CLI -> CLI: detectFramework(dir) for each workspace → framework + port
CLI -> CLI: inferAppType(name, framework, port) → server|worker|frontend
CLI -> CLI: sanitizeProjectName (RFC 1123)
CLI -> CLI: generateYaml(projectName, apps) → loopzero.yaml content
CLI -> fs: write loopzero.yaml
CLI -> fs: ensureGitignore(.env.local)
CLI -> API(POST /api/v1/projects): createProject({ name, apps })
API -> DB: INSERT INTO projects (tenant_id, name)
DB -> API: { id, name }
API -> CLI: { id, name, team }
CLI -> User: "Generated loopzero.yaml with N apps. Run: loopzero deploy"
CLI: exit 0
```

### Concerns

1. **API call is fire-and-forget from UX perspective** — if the project creation API fails with a 409 (already exists), init still succeeds (warning only). If it fails with a network error, init still writes loopzero.yaml and succeeds. The user gets a warning but no failure exit code. This is intentional for offline-first UX.

2. **No Tenant CR creation triggered by init** — the Tenant CR that the operator manages is created via the `PlatformBootstrap` flow or manually. `loopzero init` does not trigger tenant provisioning. A project can be created via `loopzero init` before the tenant infrastructure is ready, and `loopzero deploy` will fail at the workflow submission step.

---

## Flow 5: Rollback Flow

### Overview
- Entry: `loopzero rollback`
- Exit: Previous release active at runtime
- Components: CLI, API RollbackHandler, RollbackOrchestrator, pg_advisory_xact_lock, Gitea (git history), ArgoCD
- Estimated frequency: Low (incident response)
- Criticality: Critical

### Sequence Diagram — Happy Path

```
User -> CLI(rollback.ts): loopzero rollback --env production
CLI -> CLI: check auth token
CLI -> CLI: findLoopZeroYaml → project name
CLI -> User: display migration warning (always shown)
CLI -> API(POST /api/v1/projects/{name}/rollback):
  { environment: "production", to_release_id: (optional) }

API -> RollbackHandler.Create:
  API -> ProjectStore.GetByName: lookup project
  API -> RollbackOrchestrator.Rollback(req):

    [PHASE 1 — INSIDE lock]
    RollbackOrchestrator -> lock.WithEnvironmentLock: acquire
    RollbackOrchestrator -> ReleaseStore.GetActiveByEnvTx: find ACTIVE release
    RollbackOrchestrator -> ReleaseStore.GetPreviousRelease (or GetByIDForTenant): find target
    RollbackOrchestrator -> ReleaseStore.UpdateStatus(current, ReleaseRollbackStarted): ACTIVE → ROLLING_BACK
    RollbackOrchestrator -> lock: COMMIT
    [END PHASE 1]

    [PHASE 2 — OUTSIDE lock — delivery-specific]
    RollbackOrchestrator -> ContainerDeliveryActivator.Rollback(req):
      ContainerDelivery -> GitReader.GetManifestsAtCommit(tenant, project, env, gitCommit)
      GitReader -> Gitea: GET file at commit SHA
      Gitea -> GitReader: old manifests YAML
      ContainerDelivery -> GitOpsPusher.Push: commit old manifests to gitops repo
      ContainerDelivery -> SyncClient.TriggerSync(tenantID-environment): trigger ArgoCD
    [END PHASE 2]

    [PHASE 5 — INSIDE lock — finalize]
    RollbackOrchestrator -> lock.WithEnvironmentLock: acquire
    RollbackOrchestrator -> ReleaseStore.UpdateStatus(current, ReleaseRollbackCompleted): ROLLING_BACK → ROLLED_BACK
    RollbackOrchestrator -> ReleaseStore.UpdateStatus(target, ReleaseReactivated): SUPERSEDED → ACTIVE
    RollbackOrchestrator -> EnvTargetStore.SetDesired: point to target release
    RollbackOrchestrator -> EnvTargetStore.SetActive: confirm active
    RollbackOrchestrator -> lock: COMMIT
    [END PHASE 5]

API -> CLI: 200 { status: "completed", rolled_back_release, reactivated_release, url }
CLI -> User: display result
CLI: exit 0
```

### Error Paths

```
[No active release]
RollbackOrchestrator -> ReleaseStore.GetActiveByEnvTx: null
RollbackOrchestrator -> return ErrNoActiveRelease
API -> CLI: 404 { code: "NO_ACTIVE_RELEASE" }
CLI -> User: exit 3

[Delivery fails]
RollbackOrchestrator -> ContainerDelivery.Rollback: error
RollbackOrchestrator -> failRollback: transition ROLLING_BACK → ROLLBACK_FAILED
[BUG: failRollback calls Locker.WithLock(ctx, "", "", "") — empty key!]
API -> CLI: error
CLI -> User: exit 3
```

### Concerns

1. **`failRollback` uses empty lock key** — `rollback_orchestrator.go:240`: `o.deps.Locker.WithLock(ctx, "", "", "", ...)`. This calls the advisory lock with empty tenant/project/env, which means it acquires a lock on the empty-key hash rather than the actual environment. The ROLLING_BACK → ROLLBACK_FAILED transition may race with concurrent operations on the same environment.

2. **No SSE events during rollback** — unlike deploy, rollback is synchronous HTTP: no streaming, no intermediate status updates to the CLI. If ArgoCD is slow, the HTTP request hangs until ArgoCD sync triggers. The 150s handler timeout may expire before the rollback completes.

3. **Rollback does not wait for ArgoCD sync completion** — `TriggerSync` is called (best-effort for container delivery) but the rollback handler returns immediately after the gitops push + trigger. The response says "completed" even though the runtime may not yet be running the old version.

---

## Flow 6: Secret Management Flow

### Overview
- Entry: `loopzero secret set KEY=value`
- Exit: Secret stored in DB; K8s Secret + ExternalSecret CR synced (eventual consistency)
- Components: CLI, API SecretHandler, DB secret store, K8s API (ESO), ExternalSecrets Operator
- Estimated frequency: Medium
- Criticality: High

### Sequence Diagram — Set Secret (Happy Path)

```
User -> CLI(secret.ts): loopzero secret set DATABASE_URL=postgres://...
CLI -> CLI: check auth token
CLI -> CLI: findLoopZeroYaml → project name
CLI -> CLI: validate NAME=value format
CLI -> CLI: validate name matches ^[A-Z][A-Z0-9_]*$
CLI -> API(POST /api/v1/secrets?project={name}):
  { name, value, environment, app }

API -> SecretHandler.Set:
  API -> DB: store.Set(tenantID, project, env, app, name, value_bytes)
  DB: upsert secret record (name+env+app unique index)
  DB -> API: { created: bool }
  
  [Optional K8s sync — if k8sSync configured]
  API -> K8sSync.CreateSecret(tenantID, project, env, app, name, value):
    K8sSync -> K8sAPI(mgmt cluster): create Secret loopzero-secret-{name} in tenant namespace
    K8sSync -> K8sAPI(mgmt cluster): create ExternalSecret CR pointing to the Secret
    [ESO picks up ExternalSecret and syncs to workload cluster — eventual consistency]
  
  API -> CLI: 200 { name, environment, app, created }

CLI -> User: "Secret DATABASE_URL set for production/api."
CLI: exit 0
```

### Concerns

1. **K8s sync failure is silently swallowed** — `secrets.go:163`: `slog.Error(...)` is called but the response is still 200 OK. If the K8s sync fails, the secret is stored in DB but NOT available to pods. The user gets a success response even though the secret may not work at deploy time. The deploy-time secret check (`getSecrets`) only validates that the name is in DB — it does not verify K8s sync succeeded.

2. **No environment lock for secret operations** — secrets are not locked by environment advisory lock. A concurrent deploy might pick up the old secret value if it starts exactly as a secret is being updated.

3. **Secret values stored as raw bytes in DB** — no encryption at rest mentioned in the code. The `Set` method signature takes `[]byte` but there is no evidence of encryption in the stores.

---

## Flow 7: Log Streaming Flow

### Overview
- Entry: `loopzero logs --build <id>`
- Exit: Build log entries printed to stdout
- Components: CLI, API LogsHandler, Loki HTTP client, Loki (build cluster)
- Estimated frequency: High
- Criticality: Medium

### Sequence Diagram — Happy Path

```
User -> CLI(logs.ts): loopzero logs --build bld_abc123
CLI -> CLI: check auth token
CLI -> API(GET /api/v1/builds/{id}/logs?tail=N&step=S):

API -> LogsHandler.Get:
  API -> BuildStore.GetByID(buildID): fetch build record
  DB -> API: Build { tenantID, status }
  API: check build.TenantID == uc.TenantID (tenant isolation)
  API -> LokiClient.QueryBuildLogs(buildID, opts):
    LokiClient: build LogQL query: {workflow=~".*{buildID}.*"}
    LokiClient -> Loki(build cluster): GET /loki/api/v1/query_range?query=...
    Loki -> LokiClient: log streams
    LokiClient: parse + sort by timestamp
    LokiClient -> API: []LogEntry
  API -> CLI: 200 { build_id, status, entries[], total_lines }

CLI -> User: print log entries
CLI: exit 0
```

### Error Paths

```
[Loki unavailable]
LokiClient -> Loki: connection refused
LokiClient -> API: error
API -> CLI: 200 { entries: [], total_lines: 0 }
  [Note: Loki failure returns 200 with empty entries, not 5xx]
CLI -> User: "No logs available. Build may still be starting."
CLI: exit 0
```

### Concerns

1. **`--follow` flag exists in CLI but is not wired** — `LogsOptions.follow` is defined but the implementation is a single HTTP fetch (no streaming, no polling). The user sees `--follow` in help but it has no effect.

2. **Loki query uses regex match on workflow name** — `{workflow=~".*{buildID}.*"}` — this can be slow on large log datasets and may match incorrect workflows if two build IDs share a substring.

3. **No auth on Loki itself** — logs from all tenants' builds are stored in Loki. Tenant isolation is enforced by the API (checks `TenantID`), but Loki is a shared store.

---

## Flow 8: Project Delete Flow

### Overview
- Entry: `loopzero project delete <name>`
- Exit: Project removed from all environments; GitOps manifests deleted; ArgoCD sync triggered; K8s resources pruned
- Components: CLI, API ProjectDeleteHandler, ProjectDeleter, pg_advisory_xact_lock, Argo Workflows (cancel), Gitea (gitops), ArgoCD
- Estimated frequency: Low
- Criticality: High (irreversible)

### Sequence Diagram — Happy Path (All Environments)

```
User -> CLI(project-delete.ts): loopzero project delete my-app
CLI -> CLI: check auth token
CLI -> CLI: interactive confirmation (unless --yes)
CLI -> API(DELETE /api/v1/projects/{name}):

API -> ProjectDeleteHandler:
  API -> ProjectDeleter.Delete(req):

  [For each environment in envTargetStore.ListByProject]
  For env in [production, staging, ...]:
    [PHASE 1 — INSIDE lock(tenant, project, env)]
    ProjectDeleter -> lock.WithEnvironmentLock: acquire
    ProjectDeleter -> BuildStore.CancelActiveBuilds(tx): cancel all non-terminal builds (DB)
    ProjectDeleter -> EnvTargetStore.DeleteTarget(tx): delete env target row
    ProjectDeleter -> lock: COMMIT
    [END PHASE 1]

    [PHASE 2 — OUTSIDE lock]
    ProjectDeleter -> WorkflowCanceller.Cancel(workflowIDs): cancel Argo Workflows (best-effort)
    ProjectDeleter -> GitOpsPusher.Remove(tenant, project, env): delete from gitops repo
    Gitea -> ProjectDeleter: gitops manifests deleted
    ProjectDeleter -> ArgoSyncer.TriggerSync(tenantID-env): trigger ArgoCD prune
    ArgoCD: detects missing manifests, prunes K8s resources

  [After all environments removed]
  ProjectDeleter -> ProjectStore.DeleteProject: DELETE FROM projects

API -> CLI: 200 { project, deleted_environments, cancelled_builds }
CLI -> User: "Deleted project from environments: production, staging"
CLI: exit 0
```

### Error Paths

```
[No environments found (fallback path)]
EnvTargetStore.ListByProject: []
ProjectDeleter: attempt gitops cleanup for "production" (safety net)
ProjectDeleter -> GitOpsPusher.Remove(tenant, project, "production"):
  → may fail (404 not found in gitops) — logged as warning, not error
ProjectDeleter -> ArgoSyncer.TriggerSync (if remove succeeded)
```

### Concerns

1. **GitOps removal failure is non-fatal for the API response** — if `gitops.Remove` fails (line 209), the DB records are already deleted (phase 1 committed) but the K8s resources remain running. The user gets a success response but orphaned pods continue running. Only an ArgoCD manual sync or the next deploy would clean them up.

2. **ArgoCD sync failure after delete is a warning** — if `TriggerSync` fails, resources remain running. The warning is logged but the HTTP response is still 200. Without a subsequent sync, users will see orphaned resources in the cluster.

---

## Flow 9: Tenant Provisioning Flow (Operator)

### Overview
- Entry: Tenant CR created in mgmt cluster (by API or bootstrap)
- Exit: Tenant fully provisioned: Gitea repos, ArgoCD AppProject + Application, build namespace, runtime namespaces, RBAC
- Components: TenantController, GitProvisioner (Gitea), ArgoCDProvisioner, MinIOProvisioner, BuildClient, RuntimeClient
- Estimated frequency: Very low (once per tenant)
- Criticality: Critical (gates all deploys for a tenant)

### Sequence Diagram — Happy Path

```
User/Bootstrap -> K8sAPI(mgmt): CREATE Tenant {name, spec.environments}

TenantController.Reconcile (triggered by informer):
  TenantController -> Get(Tenant): fetch tenant object
  TenantController: add finalizer if missing
  TenantController: set Phase = PROVISIONING (via state machine)

  [STEP 1] reconcileGitRepo:
    TenantController -> GitProvisioner.EnsureRepo(tenant): create Gitea repo
    Gitea -> TenantController: repo URL
    TenantController: set condition GitRepoReady=True

  [STEP 2] reconcileArgoCDApp:
    TenantController -> ArgoCDProvisioner.EnsureAppProject(tenant): create AppProject
    TenantController -> ArgoCDProvisioner.EnsureApplication(tenant): create ArgoCD Application
      App name: {tenantID}-{environment} (e.g., "tn-loopzero-production")
      Source: Gitea gitops repo, path: {projectID}/{environment}/
      Destination: runtime cluster, namespace: {tenantID}-{environment}
    TenantController: set condition ArgoCDAppReady=True

  [STEP 3] reconcileBuildNamespace:
    TenantController -> BuildClient.Create: namespace "build-{tenantID}"
    TenantController -> BuildClient.CreateOrUpdate: ResourceQuota for build namespace
    TenantController -> BuildClient.Create: registry pull secrets
    TenantController -> BuildClient.Create: MinIO/Spaces credentials secret
    TenantController: set condition BuildNamespaceReady=True

  [STEP 4] reconcilePriorityClass:
    TenantController -> BuildClient.CreateOrUpdate: PriorityClass for tenant builds
    TenantController: set condition PriorityClassReady=True

  [STEP 5] reconcileEnvironmentCRs:
    For each environment in spec.environments:
      TenantController -> RuntimeClient.Create: Environment CR in mgmt cluster
      (EnvironmentController picks this up and creates runtime namespace + RBAC)

  [ALL CONDITIONS TRUE → PROVISIONING → ACTIVE transition]
  TenantController -> Status().Update: Phase=ACTIVE

EnvironmentController.Reconcile (triggered by Environment CR creation):
  EnvironmentController -> RuntimeClient.Create: namespace "{tenantID}-{environment}"
  EnvironmentController -> RuntimeClient.Create: ResourceQuota
  EnvironmentController -> RuntimeClient.CreateOrUpdate: RBAC (Role, RoleBinding)
  EnvironmentController -> RuntimeClient.CreateOrUpdate: NetworkPolicy
  EnvironmentController: set conditions
```

### Deletion Flow

```
User -> K8sAPI: DELETE Tenant
TenantController: detect DeletionTimestamp
TenantController: Phase = DELETING (via state machine)
TenantController: wait for child Environment CRs to disappear (OwnerRef cascade)
  [EnvironmentControllers delete runtime namespaces concurrently]
  [Once all environments deleted:]
TenantController -> ArgoCDProvisioner.DeleteApplications(tenantName)
TenantController -> ArgoCDProvisioner.DeleteAppProject(tenantName)
TenantController -> GitProvisioner.ArchiveRepo(tenantName)
TenantController -> BuildClient.Delete: build namespace
TenantController -> controllerutil.RemoveFinalizer
TenantController -> Update: finalizer removed → K8s GC deletes Tenant CR
```

### Concerns

1. **All reconcile steps run unconditionally** — this is by design for drift correction. However, a persistent failure in step 1 (Gitea) blocks steps 2-5 from being reached even if they were previously succeeding. The controller re-runs ALL steps regardless of which one failed, which wastes API calls on healthy resources.

2. **No TenantController invocation from `loopzero init`** — the Tenant CR is not created by the CLI. It must be created via `PlatformBootstrap` or manually. The gap between `loopzero init` (project registered) and the Tenant CR being created means deploys fail until the operator is invoked.

---

## Flow 10: Background Worker — Build Reaper

### Overview
- Entry: Scheduled timer (every minute)
- Exit: Stale QUEUED/RUNNING/CANCELLING/RELEASING builds transitioned to terminal state
- Components: BuildReaper, BuildStore, EventStore, BuildReconcileLoop (coordination)
- Estimated frequency: Every 1 minute
- Criticality: High (prevents stuck builds from blocking environments)

### Sequence Diagram

```
BuildReaper.Start: ticker every 1 minute

BuildReaper.reap(ctx):
  BuildReaper -> BuildStore.FindActiveBuilds: all non-terminal builds
  For each build:
    BuildReaper: check BuildReconcileLoop.IsProcessing(build.ID)
      → If processing: skip (MOD-6 coordination)
    BuildReaper: check timeout by status:
      QUEUED:     > 5 minutes  → failBuild("stale QUEUED")
      RUNNING:    > 30 minutes → failBuild("stale RUNNING")
      CANCELLING: > 60 seconds → failBuild("stale CANCELLING")
      RELEASING:  > 10 minutes → failBuild("stale RELEASING")
    BuildReaper -> locker.WithLock: transition to FAILED
    BuildReaper -> EventStore.Append("build.failed"): unblock CLI SSE stream
```

### Concerns

1. **BuildReaper and BuildReconcileLoop may compete on RELEASING builds** — both check `r.active` (MOD-6), but the reaper only checks `IsProcessing`. If the reconcile loop's goroutine has finished (removed from active map) but the release is still PENDING and stuck (ArgoCD callback never arrived), the reaper may fail the build before the convergence loop activates the release.

---

## Summary of Findings

| # | Finding | Severity | File |
|---|---------|----------|------|
| 1 | `failRollback` acquires advisory lock with empty key (tenant/project/env are all empty strings) | High | `api/internal/services/rollback_orchestrator.go:240` |
| 2 | Rollback returns "completed" before ArgoCD confirms sync — runtime may still be on old version | Medium | `api/internal/services/rollback_orchestrator.go:156-190` |
| 3 | K8s secret sync failure silently returns HTTP 200 — secret may not be available to pods | Medium | `api/internal/routes/secrets.go:161-164` |
| 4 | `--follow` flag in `loopzero logs` has no implementation — silently no-ops | Low | `cli/src/commands/logs.ts:43-45` |
| 5 | CF KV written twice per static deploy (writeStaticRoutingEntries + phaseBDeliver) | Low | `api/internal/services/build_orchestrator.go:663`, `api/internal/services/static_delivery.go:184` |
| 6 | GitOps removal failure on project delete is non-fatal — orphaned K8s pods remain | High | `api/internal/services/project_deleter.go:207-209` |
| 7 | Interactive multi-team login not implemented — users with >1 team must use `--team` | Medium | `cli/src/commands/login.ts:103` |
| 8 | RELEASING build with PENDING release state in retryReleaseActivation is skipped without retry or sync re-trigger | Medium | `api/internal/services/build_reconcile_loop.go:350-353` |
