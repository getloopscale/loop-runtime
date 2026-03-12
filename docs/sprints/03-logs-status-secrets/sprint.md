# Sprint 3 — `loopzero logs` + `loopzero status` + `loopzero secret`

> **Weeks:** 10-12
> **System Design Reference:** §23 (Sprint 3)

---

## Objective

All daily-use commands work. Lucas can deploy, check status, view logs, and manage secrets without leaving the terminal.

## Product Exit Gate

All 5 workflows from section 0.1 are functional (except rollback and preview).

## CLI Commands Delivered

`logs`, `status`, `inspect`, `secret`

## Tasks

### CLI — daily commands

```
[ ] loopzero status: status of all services in environment
[ ] loopzero status --env staging, loopzero status --app api
[ ] loopzero inspect <deploy-id>: details of specific deploy
[ ] loopzero logs: logs for all services (default env)
[ ] loopzero logs --app api, loopzero logs --env staging
[ ] loopzero logs --follow: continuous streaming (SSE)
[ ] loopzero logs --since 2h
[ ] loopzero logs --build <build-id>: build logs
[ ] loopzero secret set KEY=value (default: production)
[ ] loopzero secret set KEY=value --env staging --app api
[ ] loopzero secret list (names only, never values)
[ ] loopzero secret delete KEY
[ ] loopzero secret pull (download to .env.local)
[ ] loopzero secret pull --env staging --app api
[ ] --json flag for all commands (machine-readable output per section 0.3)
[ ] Unit tests: JSON output schema stability
```

### API — secrets + logs

```
[ ] AppSecretOperator: Kubernetes Secret creation from API calls
[ ] POST /internal/secret-hash-changed (serialized via environment lock)
[ ] loopzero.io/secret-hash always in Git manifests (set during generate-manifests)
[ ] Idempotency: if secret-hash already matches, return 200 (no-op)
[ ] Log streaming endpoint (SSE from pod logs)
```

### Resilience

```
[ ] gitops-push retry policy (5 retries, exponential backoff)
[ ] Fetch-merge-push on non-fast-forward Git rejection (runtime-mutable field registry)
[ ] Gitea health check integration in API deep health
[ ] Gitea backup: git bundle to MinIO every 6 hours
[ ] LoopZeroGiteaDown + LoopZeroGiteaBackupStale alerts
```

### Tests

```
[ ] Component tests: secret CRUD, log streaming, JSON output contracts
[ ] Test: Gitea down during gitops-push → release GIT_MIRROR_FAILED, assets orphaned, sweeper cleans
[ ] Test: secret-hash-changed idempotency (same hash → no-op)
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| Core CLI workflows — logs | §0.1 (Workflow 2) |
| Core CLI workflows — secrets | §0.1 (Workflow 4) |
| CLI Reference — Status and inspection | §25.4 |
| CLI Reference — Logs | §25.5 |
| CLI Reference — Secrets | §25.8 |
| `--json` output contract | §0.3 |
| AppSecretOperator | §7.3.2 |
| Secret lifecycle | §16.2 |
| Secret hash change serialization | §16.2 (Option C) |
| gitops-push conflict resolution | §7.4 |
| Gitea failure scenarios | §7.5 |
| SSE event journal | §7.1 |
