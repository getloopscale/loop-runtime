---
type: baseline
title: "Steady-state baseline — 2026-06-08 pre dashboard-screens-review-fixes"
---

# Steady-state baseline — 2026-06-08 pre dashboard-screens-review-fixes

> 4-layer baseline per `.claude/rules/steady-state-baseline.md` captured
> at start of `/implement` halt-loop for plan
> `2026-06-08-dashboard-screens-review-fixes-plan.md` v1.2 (verdict
> SHIPPABLE_WITH_CAVEATS, final_score 70, weighted_avg 99.4).

## Why this baseline

This baseline is the bisect anchor for the dashboard 5-screen
remediation. Plan ships 15 tasks (T0.1 → T5.3) closing 30 P0-P3
findings + 7 absorbed edge cases. Any regression surfacing post-merge
will be diff-bounded by this snapshot.

---

## Layer 1 — Code state

```
commit:  3aca9f9c26a1d1f6ecaebaaa9ea710b7fa60d213
branch:  worktree-glowing-growing-babbage
relation: 225 commits ahead of origin/develop, 0 behind
prior commit:  9c99f836 fix(m2): T3.3 — restore project-agnostic HasEnvironment shallow check
this baseline commit: pending (T0.1 closes within this commit)
```

The previous milestone baseline is
[`docs/operations/baselines/2026-06-07-pre-runtime-envs-m2-refactor.md`](./2026-06-07-pre-runtime-envs-m2-refactor.md)
(M2 plan); intermediate work in `worktree-glowing-growing-babbage`
delivered M2 engineering 22/22 + preview-as-category Phase 0-8 +
J-23 batch (8 commits, 4-layer canonical flow proven LIVE).

---

## Layer 2 — Runtime state (dev-public; mgmt+build+workload+runtime-2 k3d clusters via Caddy + CF)

Probe time: 2026-06-08 ~04:00 UTC

```
kubectl get pods -n loopzero-system: loopzero-api, loopzero-cloud, ory-kratos, ory-hydra, postgresql, gitea, minio (Running)
helm list -A: ~32 releases per helmfile L2 (mgmt cluster); ArgoCD App per env per tenant on workload
ArgoCD Applications: live (sync-cycle ~3min); no Degraded apps from prior M2/J-23/preview-category work
Tenant CRs: admin tenant tn-08fa2ecedc3c7e77 active, project "teste" deployed via webhook autodeploy
PreviewEnvironment CR: preview-pr-1 / preview-pr-2 (if any) tracked per single-slot model
```

Edge: `*.preview-dev.loopzero.example` wildcard via CF DNS + Caddy DNS-01 LE
cert (per memory `reference_j23_batch_2026_06_07`).

---

## Layer 3 — Config state

```
PG migrations: 000032_plan_caps_m2 applied (M2 plan T7.1; LIVE-applied per memory reference_m2_runtime_envs_per_project_session_2026_06_07)
PG migrations: 000027 preview-as-category applied (preview-as-category plan)
Plan caps schema: plan_limits table with starter/pro/team/enterprise rows (M2 D4 source-of-truth)
Hydra OAuth clients: loopzero-cli + loopzero-dashboard + loopzerokit clients persistent per dev-public helmfile env
Kratos identity schema v1.4 (J-04 recovery + J-05 device flow registered)
ConfigMap loopzero-cosign-config: keyed mode (cutover deadline 2026-08-T+90d per ADR 2026-05-12)
governance-gate.local.md: APPROVED scope for plan 2026-06-08-dashboard-screens-review-fixes (sponsor verbatim 2026-06-08)
```

Stripe webhook secret: `loopzero-stripe-webhook-dev` Secret in loopzero-system
(per memory `reference_dev_public_canonicalization_2026_06_04`).

---

## Layer 4 — LIVE functional probes (dev-public, 2026-06-08 ~04:00 UTC)

Each probe = command + observed result + invariant asserted.

### Probe 1 — `/` (dashboard root)
```
curl -s -o /dev/null -w "%{http_code} | %{time_total}s" --max-time 5 https://app-dev.loopzero.example/
→ 200 | 0.092s
```
Invariant: dashboard SPA root loads OK (asset bundle reachable via CF + Caddy + nginx).

### Probe 2 — `/projects` (project list page)
```
curl -s -o /dev/null -w "%{http_code} | %{time_total}s" https://app-dev.loopzero.example/projects
→ 200 | 0.070s
```
Invariant: project list page reachable (data binds via /api/v1/projects via dashboard SSR fallback or client-side).

### Probe 3 — `/projects/teste/deployments` (deployments list page)
```
curl -s -o /dev/null -w "%{http_code} | %{time_total}s" https://app-dev.loopzero.example/projects/teste/deployments
→ 200 | 0.079s
```
Invariant: per-project deployments table reachable; SSE wiring per J-17.

### Probe 4 — `/projects/teste/deployments/build-ad5b329b17dad143` (deployment detail page)
```
curl -s -o /dev/null -w "%{http_code} | %{time_total}s" https://app-dev.loopzero.example/projects/teste/deployments/build-ad5b329b17dad143
→ 200 | 0.099s
```
Invariant: build-specific deployment detail loads; raw `build.error` (the leak) was observed in this exact build during 2026-06-08 dashboard review.

### Probe 5 — `/projects/teste/previews` (previews page — slated for DELETION via T1.1)
```
curl -s -o /dev/null -w "%{http_code} | %{time_total}s" https://app-dev.loopzero.example/projects/teste/previews
→ 200 | 0.070s
```
Invariant: current state — page renders. POST-T1.1 invariant: this URL must return client-side redirect to `/environments?project=teste`.

### Probe 6 — `/environments` (environments page — post-M2 + Phase 5)
```
curl -s -o /dev/null -w "%{http_code} | %{time_total}s" https://app-dev.loopzero.example/environments
→ 200 | 0.082s
```
Invariant: M2-shipped UX with project picker, grouping, delete-with-?project= propagation.

---

## What changes between this baseline and the next (forward-looking)

After `/implement 2026-06-08-dashboard-screens-review-fixes` completes,
the next baseline should show:

- Probe 5 redirects (301-equivalent) to `/environments?project=teste`.
- Probe 4 renders sanitized error (no `/workspace/`, no `gyp ERR!`, no
  stack frames).
- Probes 2, 3, 6 carry M2-correct semantics: per-project env counts,
  dynamic env filter chips, Live-envs StatusDot fed by exhaustive 7-state
  build mapper.

---

## Cross-link

- Plan: `.claude/knowledge-base/plans/2026-06-08-dashboard-screens-review-fixes-plan.md`
- Edge cases: `.claude/knowledge-base/reviews/2026-06-08-dashboard-screens-review-fixes-edge-cases.md`
- Deps audit: `.claude/knowledge-base/reviews/2026-06-08-dashboard-screens-review-fixes-deps-audit.md`
- Plan-confidence: `.claude/knowledge-base/reviews/2026-06-08-dashboard-screens-review-fixes-plan-confidence.md`
- Governance approval: `.claude/governance-gate.local.md` § Approved Scope — 2026-06-08 dashboard-screens-review-fixes
- Implementation contract: `.claude/knowledge-base/implementations/2026-06-08-dashboard-screens-review-fixes-implementation.md`
