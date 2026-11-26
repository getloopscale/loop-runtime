---
type: operational-runbook
title: "Option B — Deploy ESO Controller in mgmt cluster"
---

# Option B — Deploy ESO Controller in mgmt cluster

> **Pre-authored 2026-06-10 by iter 33 productive-idle** of plan
> `2026-06-10-j22-openbao-transit-eso-sync` v1.2.
>
> **Purpose:** capture the architectural sub-paths + canonical commands
> needed to bridge the iter 31 ESO-in-mgmt gap (per runbook § Open
> Question). NOT a turn-key script — applying this REQUIRES sponsor
> verbatim "Option B go" authorization per Rule 4 (mgmt helmfile sync
> is hard-to-reverse).
>
> **When to use:** sponsor picks Option B (full chain LIVE 6/6) AFTER
> Option A subset LIVE (write-path scenarios A+D+E) has passed.

## Background — iter 31 architectural finding

LIVE pre-flight 2026-06-10 14:11Z discovered: `kubectl get pods -n external-secrets` in `k3d-loopruntime-core` returns no resources. ESO controller deployed only in workload + runtime-2 clusters (per `infra/helmfile/cell.helmfile.yaml.gotmpl` line 108-110).

Plan T2.4 ESO ClusterSecretStore template + iter 25 dev-public overlay both assumed ESO would be in mgmt. The CSS reconcile flow requires ESO controller in the same cluster as the OpenBao server it reads from. Under Option (c) (OpenBao in mgmt), full T4.1 6/6 scenarios cannot succeed without ESO ALSO in mgmt.

## 4 sub-paths to bridge the gap

### B1 — Deploy upstream `external-secrets` chart in mgmt (RECOMMENDED)

**Canonical pattern:** mirror exactly what `cell.helmfile.yaml.gotmpl` does for workload cluster. Single chart, single namespace, ~3min sync. Operationally simplest.

**Pros:**
- Minimal helmfile diff (1 repo + 1 release)
- Identical ESO version (0.14.3) across all clusters — no version drift
- ESO upgrade path stays unified (Renovate PR bumps both)
- Works with existing `loopruntime-external-secrets` umbrella chart (the OCI-published chart that wraps ESO with LoopRuntime CRDs)

**Cons:**
- 2 ESO controllers running cluster-wide (mgmt + workload) — slight resource overhead (~50m CPU + ~64Mi RAM per controller, doubled)
- Both controllers process ClusterSecretStore CRs in their own cluster — must scope CSS labels to avoid cross-reconcile noise

**Time estimate:** ~5min (1 helmfile diff + sync + wait for pods)

**Canonical commands (run AFTER sponsor verbatim "Option B go"):**

```bash
# Step B1.1 — Edit infra/helmfile/core.helmfile.yaml.gotmpl
# Add to repositories: block (around line 97 after chaos-mesh):
#
#   - name: external-secrets
#     url: https://charts.external-secrets.io
#
# Add to releases: block (anywhere — no `needs:` deps required for the
# bootstrap chart since it has no transitive deps):
#
#   - name: external-secrets
#     namespace: external-secrets
#     chart: external-secrets/external-secrets
#     version: 0.14.3
#     values:
#       - ../helm/external-secrets/values.yaml

# Step B1.2 — Apply the new release ONLY (idempotent — no-op for
# existing 44 releases under `-l name=external-secrets` filter)
helmfile -e dev-public --kube-context k3d-loopruntime-core --selector name=external-secrets sync

# Step B1.3 — Wait for ESO controller pods Ready
kubectl --context k3d-loopruntime-core -n external-secrets wait \
  --for=condition=Available deployment --all --timeout=120s

# Step B1.4 — Verify CRDs installed (ClusterSecretStore, ExternalSecret)
kubectl --context k3d-loopruntime-core get crd \
  clustersecretstores.external-secrets.io \
  externalsecrets.external-secrets.io
```

**Validation post-deploy:**

```bash
# Expected: 2 pods Running (controller + cert-controller)
kubectl --context k3d-loopruntime-core get pods -n external-secrets

# Expected: STATUS=Running 1/1 for both
```

After B1 succeeds, proceed with runbook step 2.3.bis (ESO ClusterSecretStore enable) — which now CAN reconcile because ESO is local to the same cluster as OpenBao.

### B2 — Deploy the `loopruntime-external-secrets` umbrella chart in mgmt

The OCI-published `loopruntime-external-secrets` chart bundles ESO + LoopRuntime CRDs (vault-INT P0 closure). It exposes the iter 25 `openbaoTransit.*` values block directly + ships its own ExternalSecret CRs for loopruntime-api/loopruntime-cloud/operators.

**Pros:**
- Integrates with the existing iter 25 dev-public overlay (`infra/helmfile/values/dev-public/loopruntime-external-secrets.yaml.gotmpl`) without modification
- Brings the J-22 ClusterSecretStore CR template along as part of the chart (no separate apply step)

**Cons:**
- The `loopruntime-external-secrets` chart includes ESO as a subchart dependency — extra resource overhead than B1
- Adds 13 ExternalSecret CRs that target loopruntime-api/loopruntime-cloud/operators — these will be PENDING until OpenBao paths are populated, may surface as noise

**Time estimate:** ~10min (chart deploy + CR reconcile + cleanup of PENDING noise)

**Recommendation:** B1 (simpler) for dev-public iter 1; consider B2 for prod cutover.

### B3 — Cross-cluster ExternalSecret push (NOT recommended for iter 1)

Keep ESO in workload cluster + design an ExternalSecret CR pattern where cell-0-cluster ESO reads from core-cluster OpenBao via NodePort + writes K8s Secrets to workload tenant ns.

**Pros:** preserves the 5-cluster isolation (per ADR D4) without deploying ESO in 2 clusters.

**Cons:**
- Cross-cluster reachability via NodePort breaks SPKI pin (cert SAN mismatch)
- ExternalSecret CR needs to be aware of which cluster ESO runs in (added complexity)
- ESO ClusterSecretStore vault provider expects single endpoint; rotation/HA gets complex

**Time estimate:** ~3h+ (architectural design + manifest authoring + testing).

**Recommendation:** defer to a future J-22 v2 design phase.

### B4 — Skip ESO entirely, dashboard reads PG directly (architectural pivot)

Bypass the ESO sync entirely. Have loopruntime-cloud read decrypted secrets directly from PG (with transit decrypt called server-side at deploy time, injecting plaintext into the Deployment spec at apply time).

**Pros:** no ESO controller, no ClusterSecretStore, no cross-cluster anything.

**Cons:**
- Plaintext in K8s Deployment spec (rests in etcd) — violates the J-22 "no plaintext at rest" mandate
- Deviation from the canonical ESO + CRD pattern the rest of LoopRuntime uses
- Adds decrypt call into the deploy hot path (latency)

**Recommendation:** REJECTED — defeats the entire J-22 zero-leak design.

## Decision matrix

| Path | Time | Risk | Aligns with plan T2.4? | Recommended for dev-public iter 1? |
|---|---|---|---|---|
| B1 (upstream ESO in mgmt) | ~5min | Low | YES | **YES** |
| B2 (loopruntime-external-secrets in mgmt) | ~10min | Low-Medium | YES + extra noise | Prod cutover, not iter 1 |
| B3 (cross-cluster push) | ~3h+ | High | DEVIATION (re-design) | NO — future v2 |
| B4 (skip ESO) | ~30min | CRITICAL | DEVIATION (mandate violation) | REJECTED |

## When to invoke this doc

The full LIVE walkthrough sequence becomes:

```
1. Sponsor picks Option A → execute Option A subset (~20min)
                          → 3/6 scenarios PASS (write-path canonical)
2. Sponsor picks Option B path (this doc) → choose B1/B2/B3/B4
   → execute B1 (recommended)            → ~5min ESO deploy in mgmt
3. Run runbook step 2.3.bis → ClusterSecretStore wired
4. Run full T4.1 6/6 LIVE     → all 6 scenarios PASS (full chain)
5. Sponsor browser walkthrough → verbatim "Funcionou!"
6. Emit IMPLEMENTATION_COMPLETE
```

This doc closes the iter 23 Open Question by providing concrete commands for the recommended path AND rejecting the speculative paths with documented rationale. Future Option B execution becomes ~5min commands instead of ~1h design exercise.

## Rollback path

If B1 deployment causes issues:

```bash
# Remove ESO release
helmfile -e dev-public --kube-context k3d-loopruntime-core --selector name=external-secrets destroy

# Revert helmfile changes
git restore infra/helmfile/core.helmfile.yaml.gotmpl

# Verify clean state
kubectl --context k3d-loopruntime-core get ns external-secrets
# Expected: NotFound (or already-cleaning-up)
```

ESO controller in workload cluster is UNAFFECTED by this — the destroy is namespace-scoped to mgmt only.

## Cross-references

- Plan: `.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md`
- Operator runbook: [`docs/operations/openbao-transit-cutover-runbook.md`](./openbao-transit-cutover-runbook.md)
- iter 31 architectural finding: runbook § "Architectural finding — ESO controller absent in mgmt"
- iter 31 Option A subset LIVE script: `infra/tests/e2e/dev-public-secrets-write-path-subset_test.sh`
- iter 25 dev-public ESO overlay: `infra/helmfile/values/dev-public/loopruntime-external-secrets.yaml.gotmpl`
- Runtime helmfile ESO release (canonical pattern source): `infra/helmfile/cell.helmfile.yaml.gotmpl` line 108-110
- Productive-idle mandate: memory `feedback_productive_idle_runbook_preauthor_2026_06_10`
- per-feature LIVE workflow mandate: memory `feedback_post_feature_live_regression_workflow_2026_06_09`
