# Steady-State Baseline — 2026-06-11 Pre Code-Review Remediation

**Captured at**: 2026-06-11
**Captured by**: T0.1 of plan `2026-06-11-code-review-remediation-147-findings` v1.2 (Phase 0 governance gate)
**Rule**: `.claude/rules/steady-state-baseline.md`

## Context

Captured immediately before executing the 7-phase remediation chain that closes 147 findings produced by `/loop-code-review` on the 9 delivered dashboard journeys. This is the **bisect anchor** for any regression that surfaces during Phases 1-5 of the remediation. Per ADR D-sepa-defer in the implementation contract, SEPA agent spawn was deferred — the halt-loop consults plan + ADRs + edge-case review + deps audit directly.

Cross-referenced from:
- Plan `.claude/knowledge-base/plans/2026-06-11-code-review-remediation-147-findings-plan.md` v1.2 Phase 0 T0.1
- Implementation contract `.claude/knowledge-base/implementations/2026-06-11-code-review-remediation-147-findings-implementation.md`
- Code review final report `code-review-output/final_report.md` (147 findings)
- Plan amendment chain: v1.0 → v1.1 (14 MUST FIX absorbed) → v1.2 (deps-audit FAIL_INSECURE absorbed)

## Layer 1 — Code state

```
HEAD:                 2292674bd7e8632653b02b0c3e55f016389a4c94
Branch:               worktree-glowing-growing-babbage
Ahead of develop:     409 commits
Working tree:         clean post-T0.1 capture (will diff baseline file itself)
Last commit:          2292674b chore(pre-implement): clean working tree for 2026-06-11 code-review remediation plan
```

Recent commits (last 5):
```
2292674b chore(pre-implement): clean working tree for 2026-06-11 code-review remediation plan
7dea5571 docs(plan): ship 2026-06-11-code-review-remediation-147-findings v1.2 chain
0985711c docs(claude): J-07 + J-08 + J-23 ✅ DELIVERED — recover from git history with 95% certainty
ee1b74f2 docs(claude): insert Delivered (histórico canonical) block to preserve closure timeline
6a932641 chore(plans): archive 19 delivered plans into knowledge-base/plans/completed/
```

## Layer 2 — Runtime state

**Cluster:** k3d-loopzero-mgmt (4-cluster local k3d stack)
**Helm releases (14 deployed):** argocd v2.11.7 | blackbox-exporter v0.25.0 | cert-manager v1.15.3 | cluster-issuer v1.0.0 | external-secrets v0.14.3 | gitea v1.22.3 | gitea-redis v7.2.5 | grafana v11.2.1 | harbor v2.12.2 | harbor-redis v7.2.5 | minio RELEASE.2024-12-18 | nfs-server-provisioner v4.0.8 | openbao v2.5.4 | openbao-cert v1.0.0 (+ loopzero workloads not listed)

**Pod counts per namespace:**
- argocd: 12
- cert-manager: 3
- external-secrets: 3
- kube-system: 6
- monitoring: 11
- nfs-system: 1
- loopzero-auth: 4
- loopzero-system: 20
- traefik: 2
- vault-system: 1
- velero-system: 1

**CRDs registered (loopzero.io + external-secrets + argoproj relevant):**
- `clustercredentials.loopzero.io`, `environments.loopzero.io` (loopzero CRDs verified registered)
- `applications.argoproj.io`, `applicationsets.argoproj.io`, `appprojects.argoproj.io` (ArgoCD)
- `clustersecretstores.external-secrets.io`, `clusterexternalsecrets.external-secrets.io` (ESO)

**ClusterCredentials (multi-cell):** cell-1=Connected | cell-2=Connected | cell-default=Connected (3/3 cells healthy)

**ArgoCD apps observed (sample):** m2-test-remove-tuple-staging | m2-test-same-name-diff-proj-staging | test-get-env-detail-production | test-rm-env-production (test artifacts from previous /implement sessions — may need cleanup at T6.x)

## Layer 3 — Config state

**Schema migrations:**
- loopzero database: version=32 dirty=false
- (auxiliary schema): version=10 dirty=false

**Active config baseline (no values captured — only versions):**
- `ory-kratos-config` ConfigMap (loopzero-auth ns) — Kratos public/admin URLs
- `ory-hydra-config` ConfigMap (loopzero-auth ns) — Hydra public/admin URLs
- `loopzero-service-auth` Secret — resourceVersion tracked
- 14 Helm releases listed above

## Layer 4 — LIVE functional probes

**dev-public surface (Caddy + LE TLS):**
- `https://app-dev.loopzero.example/` → HTTP 200 ✅
- `https://auth-dev.loopzero.example/health/ready` → HTTP 200 ✅
- `https://cloud-dev.loopzero.example/health` → HTTP 200 ✅
- `https://cloud-dev.loopzero.example/health/deep` → HTTP 404 ⚠️ (endpoint missing — investigate at T6.1 re-audit)
- `https://cloud-dev.loopzero.example/api/v1/projects` (no auth) → HTTP 404 (expected — auth required, endpoint masked when unauthenticated)

**Multi-cell routing:** 3/3 ClusterCredentials Connected (cell-1, cell-2, cell-default)

**Test contracts to be preserved during remediation:**
- B40 plan-cap REPLICAS_EXCEED_PLAN canonical contract
- M2 canonical namespace formula `loopzero-{tenant}-{project}-{env}` (T2.2 will fix loki/client.go regression)
- J-22 broker secret fetch chain (T3.1 will fix 5 RED tests)

## Pre-existing known issues (NOT introduced by this plan)

These will be confirmed RESOLVED at T6.1 re-audit per plan goal "≤30 retained findings, zero CRITICAL":
- 21 Go stdlib vulns including 2 HIGH crypto/x509 — T0.8 closes via `go 1.26` → `go 1.26.1` bump
- 28 hardcoded literals in `infra/tilt/{api,cloud}-deployment.yaml` — T1.1 closes via ConfigMap+Secret mount
- 6 RED tests masking J-22/M2 regressions — T3.1 + T3.2 close
- 20 CRITICAL CC≥26 hot-spots — T4.1-T4.5 close top 5 (R16-R20); remaining 15 deferred to T5.x batch via allowlist

## Verdict

✅ **CAPTURED** — 4-layer; serves as bisect anchor for the 37-task remediation chain. Operator HARD STOPs: Layer 4 `cloud-dev.loopzero.example/health/deep` 404 noted for T6.1 investigation; ArgoCD test-artifact apps noted for T6.x cleanup.
