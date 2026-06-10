# ArgoCD App of Apps — Day-2 Drift Management

CHG-2026-04-28-LAYERED-BOOTSTRAP — Phase 6 / T6.1

## Purpose

Helmfile installs the platform charts (Day-0). After bootstrap, ArgoCD takes
over via the App of Apps pattern: declarative drift detection + GitOps upgrades.

## Day-0 → Day-2 Transition

```
1. loopzero platform install            # Helmfile sync (Day-0 — initial install)
2. kubectl apply -f root-app.yaml   # ArgoCD takes ownership (Day-2 starts)
3. ArgoCD detects all releases as in-sync (because Helmfile already installed them)
4. From this point: drift detection, auto-sync, upgrades via PR → Git → ArgoCD
```

ArgoCD self-management:
- The ArgoCD Application for ArgoCD itself uses `selfHeal: false` to prevent
  ArgoCD from rewriting its own deployment without an explicit PR.
- **B53 (CHG-2026-04-28-LAYERED-BOOTSTRAP)** — All other applications also
  use `selfHeal: false` + `prune: false` until Apps include `helm.values`
  overrides matching the helmfile values. Without these, ArgoCD with
  `selfHeal: true` re-renders the chart with Bitnami defaults
  (postgresql-ha, redis-cluster, minio distributed) and **destroys the
  helmfile state**. Detected empirically on standalone bootstrap 2026-04-28:
  minio went from 1 standalone pod to 16 distributed pods (Pending due to
  insufficient memory), Ory Kratos/Hydra lost their `dsn` config. Re-
  enabling `selfHeal: true` requires the T2 follow-up below.

## Outstanding Work — T2 Follow-up Required (B53)

The 38 Application manifests in `apps/*.yaml` currently use chart-upstream
sources WITHOUT helm.values overrides. To enable full Day-2 management with
auto-sync, choose ONE of:

1. **Inline values** (`spec.source.helm.values: |...`) — Simple but couples
   chart values to App spec. Hard to share with helmfile.
2. **Multi-source apps** (`spec.sources: [chart, git]`) — Clean, idiomatic.
   Requires Gitea repo populated with `infra/helm/<chart>/values.yaml`
   accessible via `repoURL: https://gitea.loopzero-system.svc.cluster.local`.
3. **ApplicationSet + valuesObject ConfigMap** — Scales for many envs but
   adds complexity. Recommended for cell architecture (Sprint 9+).

Decision: T2 governance meeting required before re-enabling auto-sync.
Current state validates structure (38 Apps + AppProject) but pauses Day-2
drift correction until values alignment is solved.

## Sync Wave Convention

Sync wave annotations match the Helmfile `needs:` ordering:

| Wave | Components |
|------|------------|
| -3   | cert-manager, metrics-server (foundation) |
| -2   | traefik, nfs-server-provisioner |
| -1   | gitea, gitea-redis, harbor-redis, external-secrets |
| 0    | argocd, prometheus, grafana, tempo, otel, harbor, minio, keda, policy-controller |
| 1    | ory-kratos, ory-hydra, cluster-issuer, admission-policies |
| 2    | loopzero-api, loopzero-controller, tenant-controller, loopzero-cloud |
| 99   | smoke-test |

## Files

- `root-app.yaml` — root Application that watches `apps/` directory
- `apps/<release>.yaml` — one Application per Helm release
- `appproject.yaml` — AppProject `loopzero-platform` (RBAC + source repos allowlist)

## Bootstrap

```bash
# After `loopzero platform install` succeeds:
kubectl apply -f infra/argocd-bootstrap/appproject.yaml
kubectl apply -f infra/argocd-bootstrap/root-app.yaml
# ArgoCD UI shows root-app + all child apps as Synced + Healthy.
```
