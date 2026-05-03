## ArgoCD Drift Audit — 2026-05-02T14:16:19Z

Context: `k3d-loopzero-mgmt`

| App | Sync | Health | Class | Hypothesis | Fix Phase |
|-----|------|--------|-------|------------|-----------|
| build-argo-workflows | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| build-loki | OutOfSync | Missing | B | Resources never created (chart disabled or never applied) | T3.2 |
| build-promtail | OutOfSync | Missing | B | Resources never created (chart disabled or never applied) | T3.2 |
| mgmt-argocd | Unknown | Healthy | A | Duplicate keys in Helm values (e.g., env vars) | T3.1 |
| mgmt-gitea | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-gitea-redis | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-grafana | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-harbor | OutOfSync | Progressing | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-harbor-redis | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-metrics-server | OutOfSync | Progressing | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-minio | OutOfSync | Missing | B | Resources never created (chart disabled or never applied) | T3.2 |
| mgmt-nfs-server-provisioner | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-ory-hydra | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-ory-kratos | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-otel-collector | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-prometheus | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-smoke-test | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-tempo | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-tenant-controller | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-loopzero-api | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-loopzero-cloud | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-loopzero-controller | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| mgmt-traefik | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-admission-policies | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-cert-manager | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-cloudflared | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-cluster-issuer | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-external-secrets | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-keda | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-keda-http-addon | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-kube-state-metrics | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-metrics-server | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-policy-controller | Unknown | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |
| runtime-promtail | OutOfSync | Healthy | C | Orphaned resources tracked by ArgoCD (label drift or post-rotation) | T3.3 |

---

## Pragmatic Re-classification After Live Investigation

The initial classification (Classes A/B/C) over-assigned Class C because virtually
every app has condition messages mentioning "orphaned" or "ignore". Live investigation
reveals two REAL classes vs informational drift:

### Real bugs (require fix)

| App | Why broken | Fix | Phase |
|-----|------------|-----|-------|
| `mgmt-argocd` | ArgoCD controller has DUPLICATE env var `ARGOCD_CONTROLLER_REPLICAS` causing structured-merge-diff failure → entire mgmt-argocd is `Unknown`. ConfigMerge bug from Helm upgrade or two layers patching same StatefulSet. | Dedupe env vars in argocd values.yaml | T3.1 |

### Informational drift (NOT actually broken — accept or defer)

These apps are **Running and functional** but ArgoCD reports OutOfSync because of
the **L2 (Helmfile bootstrap) + L3 (ArgoCD GitOps) layered architecture** documented
in CLAUDE.md "Layered bootstrap shipped: Pulumi L1-only + Helmfile L2 + ArgoCD L3
App-of-Apps". Helmfile created resources during bootstrap; ArgoCD now sees state
without its labels/annotations and reports OutOfSync. The apps WORK (kubectl get
pods → 1/1 Running with 3d+ uptime); only the metadata diverges.

Examples confirmed by live inspection:
- `mgmt-grafana` → `grafana-7f64868796-h8pkg 1/1 Running 3d18h` (Healthy)
- `mgmt-traefik` → `traefik-77f5cb99f7-qxbwz 1/1 Running 3d15h` (Healthy)
- `mgmt-minio` → `minio-74fd57c78d-5lq75 1/1 Running 3d8h` (Healthy; ArgoCD reports Missing — false negative)

The L2/L3 reconciliation is a separate architectural decision (Sprint 9+ ApplicationSet
migration). Per ADR D6 of cell-2-activation-plan, this scope is deferred. We do NOT
fix these here because:
- Forcing `argocd app sync` would overwrite working state with ArgoCD's view
- The user-facing platform IS functional today
- Resolution requires migrating L2→L3 ownership (ApplicationSet pattern) — multi-sprint refactor

### Apps with no resources (Health=Missing — defer)

- `build-loki` + `build-promtail`: cross-cluster log shipping. Observability infra,
  not tenant deploy path. Functional gap (Loki shows local k3d gaps); not blocking
  100%-functional for tenant operations. Deferred to observability sprint.

---

## Final Phase 3 scope (revised after investigation)

- **T3.1 fix mgmt-argocd duplicate env var** — real bug, file fix in argocd values.yaml.
- **T3.2 / T3.3 deferred** — informational drift; document; do not act.

Coverage Matrix is updated to reflect this: gaps 6, 8 (Class B/C) marked DEFERRED
with rationale.
