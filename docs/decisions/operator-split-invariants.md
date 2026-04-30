# Operator Split Invariants

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 2.
> Source decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (condition C18).
> Last updated: 2026-04-30.

This document fixes the architectural invariants that the 4 split operator binaries (`loopzero-{tenant,app,preview,platform}-operator`) MUST preserve through their lifecycle. These are non-negotiable contracts; any change requires a new T3 meeting.

The invariants here cross-reference and EXTEND (not replace) ADRs 0015/0017/0018. Where this doc and an earlier ADR conflict, the EARLIER ADR wins until amended.

---

## I-1 — Cache scoping per ADR-0018

**Invariant.** Each split operator's cache MUST be scoped to the same namespace boundary as the monolith's cache, per ADR-0018 condition C9 (cache.Options ByObject for Tenant/Environment/PlatformBootstrap/GitRepositoryBinding/ClusterCredential = `loopzero-system` only).

**Why.** ADR-0018 condition C1 hardcoded `tenantNamespace = "loopzero-system"` to close the dogfood iter 7-8 incident where Tenants in `default` ns produced split-brain reconcile. Splitting the binary does NOT relax this — each split must inherit the same scope.

**Verification.** envtest case (planned T-07 per MEET-INFRA-NORMALIZE-T3-operator-split condition C16). Until envtest lands, the unit test in `infra/tests/unit/operator_metrics_registration_test.go` only verifies the metrics import; cache scoping verification awaits envtest infrastructure.

**Cross-references.**
- `docs/decisions/ADR-0018-tenant-namespace-sot.md` C9
- `docs/decisions/MEET-ADR-0018-decision.md` C16

---

## I-2 — Leader-election lease names are unique per binary

**Invariant.** Each split binary MUST use a distinct LeaderElectionID matching the binary name + suffix `.loopzero.io`:

| Binary | LeaderElectionID |
|---|---|
| loopzero-app-operator | `loopzero-app-operator.loopzero.io` |
| loopzero-platform-operator | `loopzero-platform-operator.loopzero.io` |
| loopzero-preview-operator | `loopzero-preview-operator.loopzero.io` |
| loopzero-tenant-operator | `loopzero-tenant-operator.loopzero.io` |

**Why.** Co-existence during cutover (D1 forward-only migration's atomic cutover window — even with `helmfile sync` the new replicas come up before old ones drain). Sharing leases would cause split-brain reconcile.

**Verification.** `grep "LeaderElectionID:" operators/cmd/{op}-operator/main.go` returns exactly one line per binary, per the table above.

**Cross-references.**
- MEET-INFRA-NORMALIZE-T3-operator-split conditions C16/C18
- `operators/cmd/{app,platform,preview,tenant}-operator/main.go`

---

## I-3 — RBAC subset (split union ⊆ monolith ∪ allowlist)

**Invariant.** The set of `(apiGroup, resource, verb)` triples granted across the 4 split ClusterRoles MUST be a subset of the monolith ClusterRole's expanded triples plus the explicit allowlist in `infra/scripts/rbac-subset-allowlist.txt`.

**Why.** Splitting a privileged binary is an opportunity for accidental privilege escalation (a split chart adds a permission no Security review approved). The CI gate `rbac-subset-check` enforces this on every PR; the allowlist with corresponding runbook entries is the audit surface.

**Verification.** `bash infra/scripts/rbac-subset-check.sh` returns exit 0; `.github/workflows/ci.yml` job `rbac-subset` runs it on every PR.

**Cross-references.**
- MEET-INFRA-NORMALIZE-T3-operator-split conditions C10, C11, C20
- `docs/runbooks/operator-split-rbac-audit.md`
- `infra/scripts/rbac-subset-{check.sh,allowlist.txt}`

---

## I-4 — Metrics registration via blank-import

**Invariant.** Each split binary MUST contain `_ "github.com/loopzero/loopzero/operators/internal/metrics"` (blank-import) in its `main.go` import block.

**Why.** The monolith uses an actual import (references metrics symbols directly); splits typically don't reference symbols, so they must use blank-import to trigger the package's `init()` for `promauto.New*` registration. Without this, controller-runtime's metrics registry stays empty and pre-existing metrics (e.g., `loopzero_misplaced_tenant_total` from ADR-0018) silently disappear post-cutover.

**Verification.** `infra/tests/unit/operator_metrics_registration_test.go` covers all 4 binaries.

**Cross-references.**
- MEET-INFRA-NORMALIZE-T3-operator-split condition C17
- `operators/internal/metrics/metrics.go`

---

## I-5 — `replicaCount: 2` minimum on production environments

**Invariant.** All 4 split-operator charts MUST have `replicaCount: 2` (or higher) in `values.yaml` (default) and in production overlays. PDB renders only when replicaCount > 1 (loopzero-common helper guard); replicaCount=1 violates the atomicity criterion (>30s reconciler gap during pod restart).

**Why.** During routine pod restart (rolling update, node drain, OOM), a single replica means zero reconcile activity until the new pod is Ready. The monolith was acceptable single-replica because LEADER election doesn't add value when there's only one candidate; the splits inherit this constraint at higher cost (4× the restart impact). Keeping replicaCount=2 ensures the lease holder stays available during the secondary's restart.

**Verification.** `helm template ... | grep "replicas:"` returns `replicas: 2` for each chart. `task validate:matrix` includes `helm:lint-split-operators` gate.

**Cross-references.**
- MEET-INFRA-NORMALIZE-T3-operator-split conditions C13, C14
- `charts/loopzero-{tenant,app,preview,platform}-operator/values.yaml`

---

## I-6 — ImagePullSecret consistency across 4 charts × 4 environments

**Invariant.** Each environment overlay MUST consistently configure `global.imagePullSecrets` for ALL 4 split charts. Drift (chart A has secret X, chart B has secret Y in same env) silently breaks deployment.

| Environment | imagePullSecrets |
|---|---|
| `dev` (DOKS) | inherits from default `values.yaml` (TBD when dev DOCR access wires) |
| `prod` | `[name: loopzero-registry-pull]` (DOCR access) |
| `standalone-multi` (k3d) | `[]` empty (k3d in-cluster registry, no auth) |
| `standalone` (single k3d) | `[]` (same as standalone-multi) |

**Why.** kubelet refuses to pull images without correct auth → infinite ImagePullBackOff → reconcile gap. Different secrets per chart in same env = different chart fails differently → opaque debugging.

**Verification.** `infra/tests/unit/split_operator_pull_secrets_test.go` (NEW in this commit).

**Cross-references.**
- MEET-INFRA-NORMALIZE-T3-operator-split condition C12
- `charts/loopzero-{tenant,app,preview,platform}-operator/values{,-prod,-standalone-multi}.yaml`

---

## I-7 — `clusterroles` + `clusterrolebindings` grants stay tenant-operator-only

**Invariant.** Of the 4 split charts, ONLY `loopzero-tenant-operator` may hold `apiGroups: rbac.authorization.k8s.io, resources: clusterroles+clusterrolebindings, verbs: create+update+delete`. The other 3 (app/preview/platform) MUST be denied these grants.

**Why.** ClusterRole creation is a privilege-escalation primitive: a controller with `create clusterroles` can grant itself any permission (verb=*). Tenant-operator legitimately needs it to provision per-tenant scoped roles when onboarding tenants; the others have no controller code path that creates ClusterRoles — granting them this would be unjustified escalation.

This invariant SUPPLEMENTS I-3 (RBAC subset) — I-3 ensures no permission is added vs monolith; I-7 ensures the permission is restricted to the SPLIT chart that needs it.

**Verification.** `infra/tests/unit/split_operator_clusterrole_grants_test.go` (NEW in this commit).

**Cross-references.**
- MEET-INFRA-NORMALIZE-T3-operator-split condition C11
- `charts/loopzero-tenant-operator/templates/clusterrole.yaml` (legitimate)
- `charts/loopzero-{app,preview,platform}-operator/templates/clusterrole.yaml` (must NOT have)

---

## I-8 — Adopt-or-create lifecycle preserved (ADR-0015)

**Invariant.** Resources owned by Pulumi L1 (DOCR registry, VPC, Cloudflare zones) continue to follow the adopt-or-create pattern after the operator split. The split does NOT touch L1 lifecycle; only L2 (Helm/operators) is restructured.

**Why.** ADR-0015 mandated adopt-or-create after Sprint 8 v3.2 incidents where Pulumi state drifted from cloud reality. Operator split changes K8s reconciliation; cloud lifecycle is orthogonal and unchanged.

**Verification.** Sprint 8 vitest suite (`adopt-or-create.test.ts`) continues to pass; no operator split commit modifies `infra/pulumi/src/components/`.

**Cross-references.**
- `docs/decisions/ADR-0015-adopt-or-create-bootstrap.md` (or equivalent)
- `infra/pulumi/src/__tests__/adopt-or-create.test.ts`

---

## How to amend an invariant

These are not editable by single-commit. Process:

1. Open a T3 governance meeting via `/meeting` with `platform-architect`, `platform-security-engineer`, `sre-engineer`, `production-design-reviewer`.
2. Decision record at `docs/decisions/MEET-INFRA-NORMALIZE-T3-invariant-amendment-N.md`.
3. PR amending this doc references the decision record in the commit body.
4. The amendment MUST update or remove the invariant's verification mechanism (test/script/CI) in the same PR.

---

## Cross-references

- `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` — sources I-1..I-7 conditions C13/C14/C16/C17/C18/C20
- `docs/decisions/ADR-0017-argocd-auto-sync-evidence.md` — referenced by C18
- `docs/decisions/ADR-0018-tenant-namespace-sot.md` — referenced by I-1
- `docs/decisions/ADR-0021-infra-normalize-decisions.md` — D1 forward-only migration
- `docs/runbooks/operator-split-rbac-audit.md` — I-3 audit surface
- `docs/runbooks/operator-cutover-runbook.md` (NEW this commit) — C23 deliverable
