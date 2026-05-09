# loopzero-system Egress Inventory + Default-Deny Migration Plan

**Plan:** docs/plans/deep-review-remediation-plan.md (T8.5a)
**Closes (in part):** find_infra_007 MEDIUM
**Status:** Inventory complete. T8.5b (default-deny + complementary egress allow templates) NOT YET applied.

---

## Why this exists

CLAUDE.md mandates "Local equals production" and CLAUDE.md Rule 8 mandates
fail-closed defense. The current `infra/helm/network-policies/templates/loopzero-system.yaml`
already has per-component egress allow rules but lacks a `default-deny-egress`
fallback. Result: any new pod added to `loopzero-system` (operator, sidecar,
debug pod) gets unrestricted outbound by default — a misbehaving or
compromised pod can DNS-tunnel exfiltrate or pivot to other services.

T8.5 makes the namespace **fail-closed** by adding default-deny-egress
AFTER ensuring every existing pod's required egress is explicitly allowed.

EC-5 of edge-case review: applying default-deny-egress without complete
allow-list inventory breaks DNS, kube-apiserver, registry pulls — **the
inventory below is the gate**.

---

## Pod inventory (loopzero-system)

| Pod role | Selector | Egress targets required |
|---|---|---|
| `loopzero-api` | `app: loopzero-api` | DNS (kube-system), kube-apiserver (in-cluster), PostgreSQL (in-namespace), Gitea (in-namespace), MinIO (in-namespace), Hydra (loopzero-system or loopzero-auth), ArgoCD (loopzero-system), build cluster Argo Workflows API (cross-cluster), DOCR/Harbor (TCP/443 external), GitHub API (TCP/443 external), CF API (TCP/443 external), Loki (build cluster TCP/3100 external), Tempo OTel (TCP/4317 external) |
| `loopzero-tenant-operator` | `app.kubernetes.io/component: loopzero-operator, app.kubernetes.io/name: loopzero-tenant-operator` | DNS, kube-apiserver, PostgreSQL, MinIO (read-only) |
| `loopzero-app-operator` | `app.kubernetes.io/component: loopzero-operator, app.kubernetes.io/name: loopzero-app-operator` | DNS, kube-apiserver, PostgreSQL, workload-cluster API (cross-cluster via loopzero-cluster-kubeconfigs Secret) |
| `loopzero-preview-operator` | `app.kubernetes.io/component: loopzero-operator, app.kubernetes.io/name: loopzero-preview-operator` | DNS, kube-apiserver, workload-cluster API |
| `loopzero-platform-operator` | `app.kubernetes.io/component: loopzero-operator, app.kubernetes.io/name: loopzero-platform-operator` | DNS, kube-apiserver, MinIO, ESO bridge |
| `postgresql-0` | `app.kubernetes.io/name: postgresql` | DNS only (no outbound — sole writer per CLAUDE.md) |
| `gitea-X` | `app.kubernetes.io/name: gitea` | DNS, MinIO (LFS storage), in-namespace mTLS |
| `minio-X` | `app: minio` | DNS, in-namespace |
| `argocd-server` | `app.kubernetes.io/name: argocd-server` | DNS, kube-apiserver, Gitea (TCP/3000), workload cluster Kubernetes API |
| `argocd-application-controller` | `app.kubernetes.io/name: argocd-application-controller` | DNS, kube-apiserver, Gitea, workload-cluster API, repo-server |
| `argocd-repo-server` | `app.kubernetes.io/name: argocd-repo-server` | DNS, Gitea (clone repos) |
| `loopzero-cloudflared` (CF Tunnel) | `app.kubernetes.io/name: cloudflared` | DNS, CF edge (TCP/443 + UDP/7844 outbound), loopzero-api (in-namespace target) |
| `hydra-X` (Ory) | `app.kubernetes.io/name: hydra` | DNS, PostgreSQL, kube-apiserver |
| `kratos-X` (Ory) | `app.kubernetes.io/name: kratos` | DNS, PostgreSQL, mail (smtp:587 if configured), kube-apiserver |

## Required egress endpoints (consolidated)

| Endpoint | Port | Pods that need it |
|---|---|---|
| kube-dns | UDP/53, TCP/53 | ALL |
| kube-apiserver | TCP/443 → ClusterIP | loopzero-api, all 4 operators, ArgoCD components, Hydra, Kratos |
| In-namespace PostgreSQL | TCP/5432 | loopzero-api, all operators, Hydra, Kratos |
| In-namespace Gitea | TCP/3000 | loopzero-api, ArgoCD components |
| In-namespace MinIO | TCP/9000 | loopzero-api, loopzero-tenant-operator, loopzero-platform-operator, gitea (LFS) |
| Workload cluster (cell-default/1/2) Kubernetes API | TCP/443 → external IP | loopzero-api, loopzero-app-operator, loopzero-preview-operator |
| Build cluster Argo Workflows API | TCP/443 → external IP | loopzero-api |
| GitHub API | TCP/443 → 140.82.112.0/20 + 192.30.252.0/22 | loopzero-api (autodeploy) |
| DOCR / Harbor | TCP/443 → registry.digitalocean.com / harbor.loopzero.cloud | loopzero-api (image push), workload pods (image pull — but those are NOT in loopzero-system) |
| Cloudflare API | TCP/443 → 173.245.48.0/20 etc. | loopzero-api (custom domain provisioning) |
| Loki | TCP/3100 → external (build cluster) | All pods (log shipping via Promtail sidecars — Promtail handles egress) |
| Tempo OTel | TCP/4317 → external | All pods with tracing enabled |
| CF edge (cloudflared) | TCP/443 + UDP/7844 → 198.41.192.0/24 | cloudflared only |
| SMTP (Kratos email) | TCP/587 → external | kratos (if configured) |

---

## T8.5b — Migration plan (NOT YET APPLIED)

The complementary work is creating a NEW `infra/helm/network-policies/templates/loopzero-system-egress.yaml`
that:

1. Adds `default-deny-egress` policy at namespace level (`policyTypes: [Egress]`, empty egress rules).
2. Adds per-component allow-egress policies covering EVERY endpoint above.
3. Uses `to: [namespaceSelector + podSelector]` for in-cluster targets, and `to: [ipBlock]` for external CIDRs.

This work is gated behind T8.5c (staging dry-run) per EC-5. Apply order:

1. T8.5b creates per-component allow-egress policies (additive — no breakage).
2. T8.5c applies `default-deny-egress` to staging cell-1 ONLY, observe 1h.
3. If staging clean for 1h: apply to cell-default + cell-2 in subsequent PRs (one cell per PR).
4. Rollback procedure (single command):

```bash
kubectl --context cell-X delete networkpolicy loopzero-system-default-deny-egress -n loopzero-system
```

---

## Why this is shipped as inventory-only first

Per EC-5: applying default-deny-egress without inventory = self-DoS.
Inventory + reviewer sign-off + staging dry-run is the gate. Operators
reviewing this doc must confirm:

- [ ] Every pod role above is mapped to its required egress.
- [ ] No pod role is missing from the table.
- [ ] External CIDRs (CF, GitHub, DOCR) are current.

Once approved, T8.5b can land as a separate PR. T8.5c (staging soak) is
the final gate before production rollout.

## References

- find_infra_007 (deep-review-remediation Phase 5)
- EC-5 of edge-case-plan review
- CLAUDE.md Rule 8 — fail-closed defense
- ADR-0008 (Multi-cell): cross-cluster egress is mandatory for loopzero-app-operator + loopzero-preview-operator
