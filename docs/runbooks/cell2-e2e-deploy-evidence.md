# Cell-2 E2E Deploy Evidence — 2026-05-03

> fix-all-platform-issues-plan T4.3 — empirical proof that `loopzero deploy`
> end-to-end produces a workload Pod running on cell-2 (loopzero-runtime-2)
> after `loopzero admin tenants pin --cell cell-2`.

## Context

Iter 5 ran `bash infra/tests/dogfood/full-loop.sh --only journey-01-solo-deploy`
against:
- Tenant: `tn-e2etest` (pre-pinned to cell-2)
- Project: `solo-server` (Node.js Express server)
- Build: `build-b04e0905c112b23d`
- Release: `rel-6b22241d012f71ce`

## Evidence Captured

### 1. Build pipeline GREEN

```
[journey-01-solo-deploy.sh] === loopzero deploy --no-wait ===
[journey-01-solo-deploy.sh] build_id=build-b04e0905c112b23d
```

Build transitioned through QUEUED → RUNNING → RELEASING → SUCCEEDED in
~2-3min via Argo Workflows on build cluster.

### 2. Release recorded with cell-2

```sql
SELECT id, status, cell_id FROM releases WHERE id='rel-6b22241d012f71ce';
-- rel-6b22241d012f71ce | PENDING | cell-2
```

`cell_id=cell-2` confirms the multi-cell routing layer correctly persisted
the placement decision via the placement engine.

### 3. ArgoCD Application targets cell-2

```
spec.destination.name=cell-2
status.sync.status=Synced
status.health.status=Progressing
```

The operator's `tenant_gitops.go` correctly emitted the Application CR
with `destination.name=cell-2` (per cell-2-activation-plan iter 4 BUG-FIX
in `EnvironmentReconciler.runtimeClientForEnv`).

### 4. Workload Pod running ON cell-2 cluster (loopzero-runtime-2)

```
kubectl --context k3d-loopzero-runtime-2 -n loopzero-tn-e2etest-production get pods
NAME                              READY   STATUS    RESTARTS   AGE
solo-server-api-fbb76747c-qdv6j   1/1     Running   0          4m52s
```

ArgoCD Application controller picked up the cluster Secret `cell-2-argocd-cluster`
(created/maintained by ClusterCredential controller — fix-all-platform-issues-plan
T1.2 reordering ensures bearer token stays fresh) and synced the manifests to
the runtime-2 cluster.

### 5. Pod /health returns 200

```
$ kubectl --context k3d-loopzero-runtime-2 -n loopzero-tn-e2etest-production \
    exec solo-server-api-fbb76747c-qdv6j -- wget -qO- http://localhost:3001/health
{"status":"ok"}
```

Application is functional and responding.

### 6. Cell-1 isolation confirmed

```
kubectl --context k3d-loopzero-workload get ns loopzero-tn-e2etest-production
Error from server (NotFound): namespaces "loopzero-tn-e2etest-production" not found
```

The legacy cell-1 namespace was NOT recreated by the operator (validates
the cell-2-activation-plan iter 4 EnvironmentReconciler factory fix).

## Caveats — Why "PENDING" not "ACTIVE"

The release stays in `PENDING` because:

1. cert-manager Certificate `tls-solo-server-api` shows `Ready=False` —
   letsencrypt-prod ClusterIssuer cannot issue for `tn-e2etest-solo-server-api-production.loopzero.local`
   from k3d local (no DNS-resolvable hostname for ACME challenge).
2. ArgoCD Application stays `Progressing` because Certificate is not Ready.
3. Release activator transitions PENDING → ACTIVE only when Application is `Healthy`.

**This is an environment limitation, not a deploy-flow bug.** Production
infrastructure uses Cloudflare-managed TLS (per CLAUDE.md "Custom domains v2 ...
CF handles TLS"), so this PENDING-stuck-on-cert is local-k3d-only behavior.

The deploy MECHANISM (build → image → manifests → ArgoCD → cell-2 → Pod →
HTTP 200) is fully validated by evidence #1-#6 above.

## Cleanup

The journey-01 background process was killed at HTTP-200-wait timeout (300s)
because the cert-manager local limitation prevents the external-hostname
probe from succeeding. The Pod, namespace, and release record remain
intact on cell-2 — they are valid evidence and do not need cleanup.

## References

- Plan: `docs/plans/fix-all-platform-issues-plan.md` T4.3
- ADR: `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md` C10 (tenant data isolation)
- Cell-2 activation: `docs/runbooks/cell-2-activation.md`
- Iter 4 bug fix: `operators/internal/shared/runtime_client_factory.go` + matrix entry 159
