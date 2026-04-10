# 05 — Cell Provisioning

**Phase:** B (Cell-Ready)
**Complexity:** 5/5
**Dependencies:** 01 (stable base), 03 (cell-scoped queries — prevents cross-cell contamination)
**Unblocks:** Multi-cell existence, L4+
**Cost delta:** +$300/mo per cell during testing
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately via TDD. Validate on DigitalOcean. Infra cost authorized — destroy cells after validation to control spend.

---

## Problem

`cell.ts` returns placeholder strings (line 83: `placeholder-kubeconfig-${config.cellId}`). No second cell can be provisioned. Without this, the entire ADR-0008 cell architecture is a diagram, not reality.

Evidence: `infra/pulumi/src/components/cell.ts`

## Scope

`createCell()` must provision a **complete, operational cell:**

1. **DOKS cluster** via `digitalocean.KubernetesCluster`
2. **ArgoCD** Helm release (connected to shared Gitea)
3. **Traefik** Helm release (with LoadBalancer IP or CF Tunnel)
4. **cert-manager** (Let's Encrypt DNS-01 via Cloudflare)
5. **Network policies** (tenant isolation)
6. **Prometheus agent** (local scrape, remote-write to central Grafana)
7. **ClusterCredential CR** on mgmt cluster (for controller + ArgoCD access)

Additional:
- `cell-default` backward compatibility (maps to existing workload cluster)
- Pulumi config: `loopzero:cells: [{cellId: "cell-sfo3-02", region: "sfo3", ...}]`
- Destroy/decommission support

### PostgreSQL Model (follows ADR-0008)

Per ADR-0008, PostgreSQL is **shared on the control plane, partitioned by cell_id** — NOT one PG per cell. This is an explicit architectural choice:

- **Why shared:** Placement, tenant queries, billing, audit, and cross-cell admin queries require a single source of truth. PG per cell would require distributed transactions or eventual consistency for control plane operations.
- **Partitioning strategy:** Table partitioning by `cell_id` on `builds`, `releases`, and `environment_targets`. This keeps cell-scoped queries fast (partition pruning) while allowing cross-cell admin queries.
- **Blast radius mitigation:** Cell isolation comes from workload separation (separate K8s clusters, ArgoCD instances), not from DB separation. PG HA (primary + 2 replicas on DO) protects against PG failure.
- **Future option:** If PG becomes the bottleneck at 10+ cells (validated by L5+ data), PG per cell can be evaluated as an ADR amendment. That decision requires a T3 meeting with evidence, not a roadmap assumption.

## Acceptance Criteria

1. **`createCell()` provisions real infrastructure.** `kubectl --kubeconfig=cell-sfo3-02.kubeconfig get nodes` returns 2+ Ready nodes.
2. **ClusterCredential CR on mgmt cluster.** `kubectl get clustercredential cell-sfo3-02 -o jsonpath='{.status.conditions[?(@.type=="Connected")].status}'` returns "True".
3. **Tenant deploys to new cell.** Create project in cell-sfo3-02. Deploy succeeds. App reachable.
4. **Cell destroy is safe.** `pulumi destroy --target cell-sfo3-02` removes only cell-sfo3-02 resources.
5. **Provisioning time < 15 minutes.** From `pulumi up` to all nodes Ready.

## Required Tests

All tests below must be **implemented as part of this item**. None exist today.

| Test | Type | Exists? | What It Proves |
|---|---|---|---|
| `TestCellProvisioning_CreatesCluster` | Infra | **No — implement** | Real DOKS cluster created |
| `TestCellProvisioning_ArgoCD_Healthy` | Infra | **No — implement** | ArgoCD running on new cell |
| `TestCellProvisioning_TenantDeploy` | E2E | **No — implement** | Deploy to new cell succeeds end-to-end |
| `TestCellDestroy_Isolation` | Infra | **No — implement** | Destroy cell-sfo3-02 leaves cell-default untouched |
| `TestClusterCredential_Connected` | Integration | **No — implement** | Mgmt cluster can reach new cell |
| `TestL0_Rollback` (on new cell) | E2E | **No — implement** | Rollback works on non-default cell |

## SLO Targets

| SLI | Target |
|---|---|
| Cell provisioning time | < 15 min |
| System pods Running post-provision | < 5 min after nodes Ready |
| Deploy success to new cell | 100% |
| Cell destroy time | < 10 min |

## Cost

+$300/mo per cell (2 medium runtime nodes + ArgoCD + monitoring overhead). Destroy after validation to avoid ongoing cost.

## Rollback Plan

`pulumi destroy --target "urn:pulumi:dev::loopzero::cell-sfo3-02*"` removes all cell resources. Existing cell-default untouched. MTTR: 10 minutes.

Safety gate: Cell provisioning behind `loopzero:cells` config array. Empty array = single cell only.

## DoD

```bash
# 1. No placeholders in cell.ts
grep -c "placeholder" infra/pulumi/src/components/cell.ts
# 0

# 2. New cell cluster accessible
kubectl --kubeconfig=/tmp/cell-sfo3-02-kubeconfig get nodes
# 2+ Ready nodes

# 3. ArgoCD running on new cell
kubectl --kubeconfig=/tmp/cell-sfo3-02-kubeconfig get pods -n argocd -l app.kubernetes.io/name=argocd-server
# 1/1 Running

# 4. Full deploy lifecycle on new cell (not just L0 baseline)
LOOPZERO_LOAD_TEST=1 LOOPZERO_CELL=cell-sfo3-02 go test ./infra/tests/load/ -v -run TestL0_Baseline -timeout 10m
# SUCCEEDED
LOOPZERO_LOAD_TEST=1 LOOPZERO_CELL=cell-sfo3-02 go test ./infra/tests/load/ -v -run TestL0_Rollback -timeout 10m
# SUCCEEDED

# 5. Destroy isolation — preview shows ONLY cell-sfo3-02 resources
cd infra/pulumi && npx pulumi preview --target "urn:pulumi:dev::loopzero::cell-sfo3-02*" 2>&1 | grep -c "cell-default"
# 0

# 6. ClusterCredential connected
kubectl get clustercredential cell-sfo3-02 -n loopzero-system -o jsonpath='{.status.conditions[?(@.type=="Connected")].status}'
# True
```
