---
type: overview
title: "LoopZero docs — index"
---

# LoopZero docs — index

Entry point for `docs/`. Start here rather than browsing the tree: about two thirds of the files
below are **dated audit trail** (completed plans, baselines, meeting records) that read as history,
not as current instruction.

| Kind | Where | Read it as |
|---|---|---|
| **Living reference** | `architecture/`, `operations/`, `runbooks/`, `api/`, `design/` | Current truth. Fix it when it drifts. |
| **Decision records** | `decisions/` (15 ADRs) | Immutable. A decision as it stood on its date; do not rewrite the body. |
| **Audit trail** | `plans/completed/`, `operations/baselines/`, `meetings/records/`, `operations/findings/`, `operations/evidence/`, `reviews/`, `handoff/` | History. Preserved indefinitely per `.claude/rules/audit-trail-rotation.md`. Never edited to match present state. |
| **In-flight plans** | `plans/` (not `completed/`) | Work agreed but not finished. |

Engineering contracts that govern *how* work is done are no longer versioned in this repo — the
`.claude/` tree (rules, skills, agents) and the root `CLAUDE.md` were removed in the docs cleanup
(`1c941b91`). The operator's global contract (`~/.claude/CLAUDE.md`) is the surviving source.

---

## Start here

| If you want to… | Read |
|---|---|
| Set up and run the dev loop | [`operations/dev-loop.md`](operations/dev-loop.md) |
| Understand the system shape | [`architecture/index.md`](architecture/index.md) |
| Touch the LoopZeroKit ↔ LoopZeroCloud seam | [`architecture/loopzerokit-loopzerocloud-integration.md`](architecture/loopzerokit-loopzerocloud-integration.md) — **mandatory pre-read** |
| Know which credentials an environment needs | [`operations/credentials-required.md`](operations/credentials-required.md) |
| Run the canonical cleanroom validation | [`operations/cleanroom-canonical-runbook.md`](operations/cleanroom-canonical-runbook.md) |
| See what the CI gates cover | [`operations/ci-gate-coverage-matrix.md`](operations/ci-gate-coverage-matrix.md) · [`operations/ci-runbook.md`](operations/ci-runbook.md) |
| Find a cross-repo contract | [`cross-repo-contract-matrix.md`](cross-repo-contract-matrix.md) |

## Architecture

| Doc | Subject |
|---|---|
| [`architecture/index.md`](architecture/index.md) | Index of architecture material |
| [`architecture/loopzerokit-loopzerocloud-integration.md`](architecture/loopzerokit-loopzerocloud-integration.md) | The `services.json` v2 wire contract between LoopZeroKit and LoopZeroCloud |
| [`architecture/api/withenvironmentlock-coverage.md`](architecture/api/withenvironmentlock-coverage.md) | Which mutating routes hold the environment lock |
| [`architecture/build-pipeline/tenant-build-pipeline-as-is.md`](architecture/build-pipeline/tenant-build-pipeline-as-is.md) | Tenant build pipeline as built |
| [`architecture/ci-cd/argo-workflows-build-pipeline.md`](architecture/ci-cd/argo-workflows-build-pipeline.md) | Argo Workflows build pipeline |
| [`architecture/multi-cell/argocd-cluster-secret-flow.md`](architecture/multi-cell/argocd-cluster-secret-flow.md) | How ArgoCD learns about cells |
| [`architecture/platform/clustercredential.md`](architecture/platform/clustercredential.md) | The `ClusterCredential` CRD |
| [`architecture/aws-region-posture-decision.md`](architecture/aws-region-posture-decision.md) | AWS region posture for the re-platform |
| [`architecture/eraser-diagrams-index.md`](architecture/eraser-diagrams-index.md) | Diagram sources |
| [`architecture/helm-testing/README.md`](architecture/helm-testing/README.md) | Helm chart testing approach |
| `architecture/*/diff/*.md` | Point-in-time as-is vs to-be deltas — closer to audit trail than reference |
| [`api/source-upload-wire-protocol.md`](api/source-upload-wire-protocol.md) | Multipart source-upload protocol |

## Operations — runbooks

Grouped by what you are actually doing. `operations/` holds 72 files; these are the ones that
answer an operational question.

**Environment and dev loop**

- [`operations/dev-loop.md`](operations/dev-loop.md) — `task setup` vs `tilt up`, the canonical workflow
- [`operations/cleanroom-canonical-runbook.md`](operations/cleanroom-canonical-runbook.md) — the destructive rebuild chain
- [`operations/dev-public-canonical-closure-runbook.md`](operations/dev-public-canonical-closure-runbook.md) — dev-public environment
- [`operations/local-public-dashboard.md`](operations/local-public-dashboard.md) · [`operations/operator-local-caddy-workflow.md`](operations/operator-local-caddy-workflow.md) — local edge
- [`operations/credentials-required.md`](operations/credentials-required.md) — what each environment needs

**Secrets and identity**

- [`operations/openbao-init-runbook.md`](operations/openbao-init-runbook.md) — Shamir unseal ceremony
- [`operations/openbao-transit-cutover-runbook.md`](operations/openbao-transit-cutover-runbook.md) · [`operations/openbao-multi-cluster-auth-runbook.md`](operations/openbao-multi-cluster-auth-runbook.md) · [`operations/openbao-aws-resources-runbook.md`](operations/openbao-aws-resources-runbook.md)
- [`operations/secrets-cluster-local-runbook.md`](operations/secrets-cluster-local-runbook.md) · [`operations/vault-secret-migration-runbook.md`](operations/vault-secret-migration-runbook.md)
- [`operations/service-auth-rotation.md`](operations/service-auth-rotation.md) · [`operations/session-token-rotation-runbook.md`](operations/session-token-rotation-runbook.md) · [`operations/build-cluster-token-rotation.md`](operations/build-cluster-token-rotation.md)
- [`operations/break-glass-runbook.md`](operations/break-glass-runbook.md) — time-boxed emergency cluster-admin

**Supply chain**

- [`operations/cosign-signing-key-rotation-runbook.md`](operations/cosign-signing-key-rotation-runbook.md)
- [`operations/tuf-root-ceremony.md`](operations/tuf-root-ceremony.md) · [`operations/sigstore-storage-model.md`](operations/sigstore-storage-model.md) · [`operations/rekor-retention.md`](operations/rekor-retention.md)
- [`operations/m07-cutover-sequence.md`](operations/m07-cutover-sequence.md) · [`operations/rollback-m07-cutover-runbook.md`](operations/rollback-m07-cutover-runbook.md) — keyed → keyless

**Platform lifecycle**

- [`operations/k8s-upgrade-runbook.md`](operations/k8s-upgrade-runbook.md) — DOKS minor upgrades
- [`operations/cell-drain-runbook.md`](operations/cell-drain-runbook.md) · [`operations/production-canary-stage4-runbook.md`](operations/production-canary-stage4-runbook.md) · [`operations/production-canary-stage5-runbook.md`](operations/production-canary-stage5-runbook.md)
- [`operations/crd-conversion-procedure.md`](operations/crd-conversion-procedure.md) · [`operations/pv-rebind-runbook.md`](operations/pv-rebind-runbook.md)
- [`runbooks/migration-rollback.md`](runbooks/migration-rollback.md) · [`runbooks/bootstrap-regression-matrix.md`](runbooks/bootstrap-regression-matrix.md)
- [`runbooks/vap-delete-guardrail.md`](runbooks/vap-delete-guardrail.md) — the core-cluster delete guardrail: which identities are exempt and why, what to do when it fires, and the rollback (a values toggle, no rebuild). Read the failure mode at the end before touching it: a `DELETE` policy that stops exempting the cluster GC deadlocks namespaces in `Terminating`.
- [`runbooks/alerts/`](runbooks/alerts/) — one runbook per Prometheus alert

**Cost, observability, billing**

- [`operations/cost-attribution.md`](operations/cost-attribution.md) — OpenCost per-tenant attribution
- [`operations/build-pipeline-otel.md`](operations/build-pipeline-otel.md) — build tracing
- [`operations/stripe-prod-setup.md`](operations/stripe-prod-setup.md) · [`operations/plan-limits-m2-decision.md`](operations/plan-limits-m2-decision.md)

**Tooling and maps**

- [`operations/loopzero-ops.md`](operations/loopzero-ops.md) — the ops + quality binary
- [`operations/infra-scripts-catalog.md`](operations/infra-scripts-catalog.md) · [`operations/helmfile-delegation-map.md`](operations/helmfile-delegation-map.md) · [`operations/ginkgo-migration-map.md`](operations/ginkgo-migration-map.md) · [`operations/drill-migration-map.md`](operations/drill-migration-map.md)
- [`version-pin-cutover-runbook.md`](version-pin-cutover-runbook.md) · [`api-keys.md`](api-keys.md)

`operations/loopzero-bottleneck-simulator-*.md` (20 files) are the simulator's own plan/review pairs —
audit trail, not runbooks.

## Design

- [`design/2026-05-25-loopzero-dashboard-design-system.md`](design/2026-05-25-loopzero-dashboard-design-system.md) — the dashboard design system
- [`design/2026-05-24-dashboard-design-language.md`](design/2026-05-24-dashboard-design-language.md)

The dashboard itself was extracted to `../loopzero-cloud` at M4; these documents describe its design
language and stayed with the historical record.

## Conventions

- Every claim that carries a number states how it was measured, or says it is an estimate.
- A path in prose must resolve. If a referenced artifact was deleted, say so inline rather than
  leaving a dangling link — a reader cannot tell the difference between "moved" and "gone".
- Runbooks state whether a step is automated or manual. A runbook that describes a retired
  automation as if it still runs is worse than no runbook.
