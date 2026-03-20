# Sprint 2 — `loopzero deploy` Happy Path End-to-End

> **Weeks:** 6-9
> **System Design Reference:** §23 (Sprint 2)

---

## Objective

The core promise works. A user runs `loopzero deploy` and gets a live URL. This is the first demo-able product increment.

## Product Exit Gate

- `loopzero login` → `loopzero init` → `loopzero deploy` produces a running app with a real URL.
- The "5-minute promise" is testable.

## CLI Commands Delivered

`deploy`

## Tasks

### CLI — deploy command

```
[x] loopzero deploy: packages code, calls API, streams build progress
[x] loopzero deploy --env staging
[x] loopzero deploy --app api (single service)
[x] loopzero deploy --no-wait
[x] CLI output: progress indicators (✓ Building api... [45s])
[x] CLI output: final URLs on success
[x] CLI exit codes: 0 (success), 2 (build failure), 3 (deploy failure), 5 (partial failure)
[x] CI mode: CI=true disables spinners, colors, confirmations
```

### API — build orchestration + serialization

```
[x] PostgreSQL advisory locks (EnvironmentLock with withEnvironmentLock wrapper)
[x] SET LOCAL lock_timeout = '5s' in advisory lock transaction
[x] pg_advisory_xact_lock (not session-scoped) with code comment
[x] Connection pool: statement_timeout=30s, connection validation
[x] Idempotency key middleware
[x] POST /builds with serialization + idempotency
[x] BuildOrchestrator + ReleaseStore
[x] SSE event journal for build progress
[x] GET /builds/:id/stream with Last-Event-ID replay
[x] Partial unique index: idx_one_active_build_per_env
[x] Step 5 validation inside advisory lock transaction (not pre-check)
```

### Build pipeline

```
[x] WorkflowTemplate: loopzero-build-nodejs-v1
[x] All build steps including cache restore/save
[x] gitops-push with correlation ID dedup
[x] Git commit format with trailers (Release-ID, Correlation-ID, Build-ID)
```

### ArgoCD sync

```
[x] ArgoCD installation and configuration
[x] ClusterCredentialsOperator
[x] API sync wait with timeout semantics (2 min)
[x] Release state machine: READY → PENDING → ACTIVE or SYNC_FAILED
[x] ArgoCD health gate: 3 consecutive failures → circuit OPEN → 503 PLATFORM_DEGRADED
```

### Tests

```
[x] Component tests: serialization, idempotency, concurrent rejection
[x] E2E: deploy boilerplate → app running at URL
[x] E2E: second build uses cache
[x] Test: two concurrent deploys → exactly one 202, one 409
[x] Test: API pod killed mid-lock → next operation succeeds immediately
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| Core CLI workflows — first deploy | §0.1 (Workflow 1) |
| CLI Output & Error Contract | §0.3 |
| Exit codes | §0.3 |
| Partial failure semantics | §0.3 |
| 5-minute phase breakdown | §1 |
| Concurrency, serialization & idempotency | §4 |
| Environment lock (advisory locks) | §4.2 |
| Idempotency | §4.3 |
| Conflict matrix | §4.4 |
| Build state machine | §5.1 |
| Release state machine | §5.2 |
| loopzero API endpoints | §7.2 |
| withEnvironmentLock (Drizzle snippet) | §4.2 |
| Argo Workflows (build engine) | §7.4 |
| `loopzero deploy` complete flow | §10.1 |
| SSE reconnection protocol | §7.1 |
| PostgreSQL connection pool | §7.8 |
