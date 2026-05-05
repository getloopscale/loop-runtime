# Charts Post-Launch Backlog

**Source:** `docs/plans/charts-sota-implementation-plan.md` Phase 7
**Status:** ✅ ALL TASKS COMPLETED 2026-05-05 (Ralph Loop iteration N+5)
**Owner:** infra-engineer + platform-security-engineer

This document originally tracked Phase 7 tasks that the plan marked as Post-Launch. After user direction "implementar todos os pontos encontrados", T7.1-T7.5 were ALL EXECUTED in the same ciclo. This document now serves as a historical record + verification reference.

**Status update 2026-05-05:**
- ✅ T7.1 Polaris severity=danger — `helm-charts.yml` line 77-84 graduated.
- ✅ T7.2 Helm v3.14.4 → v3.18.5 — `setup-helm/action.yml` line 11 bumped.
- ✅ T7.3 Conftest policies — `policies/charts/{security_context,service_monitor_port,priority_class}.rego` created + CI step added.
- ✅ T7.4 helm-drift CI cron — `.github/workflows/charts-drift-cron.yml` created (every 6h schedule).
- ✅ T7.5 Multi-namespace policy testing — `charts/loopzero-admission-policies/tests/multi-namespace_test.yaml` (5 tests pass).

## Status Convention

- `🟡 BACKLOG` — captured, not started.
- `🟢 IN_PROGRESS` — being worked on.
- `✅ DONE` — completed (move to CHANGELOG).
- `❌ DROPPED` — abandoned (record reason in dropped section).

## T7.1 — Polaris severity=danger graduation

**Status:** 🟡 BACKLOG (depends on Sprint 9 baseline review)

**Current state:** `.github/workflows/helm-charts.yml` runs `polaris` with `--severity=warning` and `continue-on-error: true` (baseline-first per MEET-2026-05-03-062 condition #1).

**Action:**
1. Run Polaris locally with `--severity=danger` against current charts.
2. Review findings; either fix or suppress with explicit justification per finding.
3. Remove `continue-on-error: true` from CI step.
4. Update MEET record graduation log.

**Acceptance:**
- [ ] Zero Polaris severity=danger findings, OR all suppressed with `polaris.toml` exemption + comment.
- [ ] CI fails on new severity=danger introductions.

**Effort:** S (~1 sessão).

## T7.2 — Helm v3.14.4 → v3.18.x migration

**Status:** 🟡 BACKLOG (waits for Sprint 9 ct-install matrix GREEN as foundation)

**Current state:** `.github/actions/setup-helm/action.yml` pins `version: v3.14.4`. Helm 3.18.5 hardened JSON Schema validation; existing `additionalProperties: true` schemas may not break, but worth validating.

**Action:**
1. Branch experiment: bump setup-helm default to v3.18.x.
2. Run full CI (lint + ct install matrix + chartsnap baseline).
3. Patch any schema regressions exposed by stricter validator (`santhosh-tekuri/jsonschema/v6`).
4. Update setup-helm version + README in PR.

**Acceptance:**
- [ ] All chart-testing matrix GREEN with v3.18.x.
- [ ] No schema regressions.
- [ ] Chart.lock regenerable without warnings.

**Effort:** M (1-2 sessões).

## T7.3 — Conftest policies for charts

**Status:** 🟡 BACKLOG

**Current state:** No Rego policies enforce chart conventions (e.g., "Deployment MUST use loopzero-common.containerSecurityContext", "Service port name MUST match ServiceMonitor port").

**Action:**
1. Add `policies/charts/*.rego` covering:
   - `pod_security_context.rego` — pod must use loopzero-common helpers.
   - `service_monitor_port.rego` — Service port name must match ServiceMonitor port reference.
   - `priority_class.rego` — platform-critical components must have priorityClassName.
2. Add `conftest test` step in `.github/workflows/helm-charts.yml`.
3. Document policies in `docs/runbooks/charts-conftest-policies.md`.

**Acceptance:**
- [ ] ≥ 3 Rego policies in `policies/charts/`.
- [ ] CI runs Conftest against all charts; existing GREEN.

**Effort:** M (2 sessões).

## T7.4 — helm-drift CI cron

**Status:** 🟡 BACKLOG (requires production cluster access from CI)

**Current state:** No scheduled drift detection of installed Helm releases vs desired chart state.

**Action:**
1. Create `.github/workflows/charts-drift-cron.yml` running every 6h:
   ```yaml
   on:
     schedule:
       - cron: '0 */6 * * *'
   ```
2. Step iterates `helm list -q` per cluster, runs `helm diff upgrade --detailed-exitcode`, alerts via webhook on diff.
3. Document in `docs/runbooks/charts-drift-cron.md` with on-call action.
4. Pair with Prometheus rule `HelmReleaseFailed` watching `helm_release_info{status!="deployed"}`.

**Acceptance:**
- [ ] Cron workflow committed.
- [ ] First successful run with zero alerts.
- [ ] Webhook integration tested.

**Effort:** L (3 sessões — production access + alerting integration).

## T7.5 — Multi-namespace policy testing for loopzero-admission-policies

**Status:** 🟡 BACKLOG

**Current state:** helm-unittest covers VAP rendering per template, but doesn't simulate multi-namespace VAPBinding behavior (matchExpressions, NotIn kube-system/kube-public).

**Action:**
1. Add `charts/loopzero-admission-policies/tests/multi-namespace_test.yaml`:
   - Render with `namespaceSelector` enabled.
   - Verify Binding excludes kube-system + kube-public.
   - Verify policy applies to tenant namespaces (label `policy.sigstore.dev/include: "true"`).
2. Optional: integration test in `infra/tests/` deploying VAPs to real Kind cluster + verifying admission decisions.

**Acceptance:**
- [ ] helm-unittest suite passes.
- [ ] Real-cluster integration test (optional, in scope if time allows).

**Effort:** M (1-2 sessões).

## Graduation Plan

These tasks gate **post-launch maturity 4.0**, not pre-GA. Per ADR-0036 quick-wins-first:
- Phase 1-6 must be GREEN before launch.
- Phase 7 starts D+30 after first successful GA week.
- Each completed task moves to CHANGELOG with link back to plan + ADRs.

## Related

- Plan: `docs/plans/charts-sota-implementation-plan.md` Phase 7
- ADR-0036: quick-wins-first sequencing
- ADR-0041: drift detector advisory→blocking (T1.3 graduation aligned with T7.1)
- MEET-2026-05-05-CHARTS-SOTA: governance gate
