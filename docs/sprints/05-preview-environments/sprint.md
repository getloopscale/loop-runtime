# Sprint 5 — Preview Environments + GitHub Webhook

> **Weeks:** 16-18
> **System Design Reference:** §23 (Sprint 5)

---

## Objective

Preview environments work automatically on PR open. This completes the "Vercel parity" for the core workflow.

## Product Exit Gate

- Opening a GitHub PR creates a preview environment.
- URL appears as PR comment.
- Merging/closing the PR destroys the preview.

## CLI Commands Delivered

`preview`

## Tasks

### Webhook + ingestion

```
[ ] GitHub webhook receiver: PR opened → trigger preview build
[ ] GitHub webhook receiver: push to main → trigger production deploy
[ ] Code ingestion Mode A: GitHub mirror via webhook
[ ] Code ingestion Mode B: CLI push via git bundle (loopzero deploy)
[ ] PR comment bot: posts preview URLs on PR
```

### CLI — preview commands

```
[ ] loopzero preview list: active preview environments
[ ] loopzero preview delete <preview-id>
[ ] loopzero preview open <preview-id>: open in browser
[ ] loopzero deploy --preview: manual preview creation
[ ] loopzero deploy --preview --branch feat/payments
```

### Preview lifecycle

```
[ ] PreviewEnvironment CRD + controller + state machine
[ ] TTL: deadline + reaper + garbage collector
[ ] Cleanup order with failure tolerance
[ ] Concurrent preview delete + build handling
[ ] Per-project wildcard TLS cert (*.{project}.{tenant}.loopzero.example) via cert-manager DNS-01
```

### Multi-tenancy enforcement

```
[ ] Build queue depth enforcement (maxBuildQueueDepth in Tenant spec)
[ ] PriorityClassName injection in WorkflowSpec based on tenant plan
[ ] Ephemeral credential generation per build (Gitea, MinIO, registry)
[ ] Per-tenant cache isolation in MinIO
[ ] Build egress NetworkPolicy (namespaceSelector + podSelector, no hardcoded IPs)
```

### Tests

```
[ ] E2E: preview lifecycle end-to-end (PR open → URL → PR close → cleanup)
[ ] E2E: TTL expiry triggers cleanup
[ ] E2E: controller restart during TTL → preview still cleaned up
[ ] Test: tenant at maxBuildQueueDepth → new build returns 429 BUILD_QUEUE_FULL
[ ] Chaos: 3 Enterprise tenants saturating queue → Starter build eventually completes
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| Core CLI workflows — preview | §0.1 (Workflow 5) |
| CLI Reference — Environments and previews | §25.9 |
| PreviewEnvironment state machine | §5.3 |
| Preview environment flow | §10.4 |
| Code ingestion (Mode A + Mode B) | §7.1 |
| ArgoCD capacity note (previews) | §5.3 |
| Noisy neighbor protection | §14.2 |
| PriorityClass scheduling | §14.3 |
| Build queue fairness (GAP-01) | §14.3 |
| Ephemeral credentials per build | §17.2 |
| Cache isolation | §17.4 |
| Network isolation (build namespace) | §14.4 |
| TLS certificates (per-project wildcard) | §7.7 |
| Cleanup order | §5.3, §11 |
