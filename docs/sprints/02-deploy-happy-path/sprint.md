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
[ ] loopzero deploy: packages code, calls API, streams build progress
[ ] loopzero deploy --env staging
[ ] loopzero deploy --app api (single service)
[ ] loopzero deploy --no-wait
[ ] CLI output: progress indicators (✓ Building api... [45s])
[ ] CLI output: final URLs on success
[ ] CLI exit codes: 0 (success), 2 (build failure), 3 (deploy failure), 5 (partial failure)
[ ] CI mode: CI=true disables spinners, colors, confirmations
```

### API — build orchestration + serialization

```
[ ] PostgreSQL advisory locks (EnvironmentLock with withEnvironmentLock wrapper)
[ ] SET LOCAL lock_timeout = '5s' in advisory lock transaction
[ ] pg_advisory_xact_lock (not session-scoped) with code comment
[ ] Connection pool: statement_timeout=30s, connection validation
[ ] Idempotency key middleware
[ ] POST /builds with serialization + idempotency
[ ] BuildOrchestrator + ReleaseStore
[ ] SSE event journal for build progress
[ ] GET /builds/:id/stream with Last-Event-ID replay
[ ] Partial unique index: idx_one_active_build_per_env
[ ] Step 5 validation inside advisory lock transaction (not pre-check)
```

### Build pipeline

```
[ ] WorkflowTemplate: loopzero-build-nodejs-v1
[ ] All build steps including cache restore/save
[ ] gitops-push with correlation ID dedup
[ ] Git commit format with trailers (Release-ID, Correlation-ID, Build-ID)
```

### ArgoCD sync

```
[ ] ArgoCD installation and configuration
[ ] ClusterCredentialsOperator
[ ] API sync wait with timeout semantics (2 min)
[ ] Release state machine: READY → PENDING → ACTIVE or SYNC_FAILED
[ ] ArgoCD health gate: 3 consecutive failures → circuit OPEN → 503 PLATFORM_DEGRADED
```

### Tests

```
[ ] Component tests: serialization, idempotency, concurrent rejection
[ ] E2E: deploy boilerplate → app running at URL
[ ] E2E: second build uses cache
[ ] Test: two concurrent deploys → exactly one 202, one 409
[ ] Test: API pod killed mid-lock → next operation succeeds immediately
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
