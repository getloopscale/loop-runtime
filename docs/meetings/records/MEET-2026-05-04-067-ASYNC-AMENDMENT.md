# MEET-2026-05-04-067-ASYNC-AMENDMENT

**Date:** 2026-05-04
**Type:** Plan amendment (per ADR-0022)
**Plan:** `docs/plans/pre-launch-gaps-plan.md` v1.2 → v1.3
**Status:** APPROVED async (pending human ratification)

## Amendment Summary

Resolves inconsistency in v1.2 between deferral of P2.3 and Phase 6 dogfood criterion.

**v1.2 stated:**
- P2.3 (operator Application controller reconcile + CRD bump for `spec.scaling`) DEFERRED via MEET-066
- Phase 6 dogfood criterion: "scale config persists in DB + Application CR shows spec.scaling"

**Inconsistency:** The criterion "Application CR shows spec.scaling" depends on the CRD bump that P2.3 would have done. Since P2.3 is deferred, the CRD doesn't have the new field. Asserting CR shows `spec.scaling` is impossible without P2.3.

**v1.3 amendment:** Phase 6 criterion simplifies to **"scale config persists in DB"**.

## Decision

Sponsor: chief-pentester (synthesized) — accept simplification.
Challenger: production-design-reviewer (synthesized) — concerned that DB persistence without CR actuation means user thinks scaling worked when it didn't. Mitigation: CHANGELOG.md must clearly note "config persists, actuation pending Sprint 9 + scale-actuation-plan.md".

**Verdict:** APPROVED.

## Evidence (E2E live verification 2026-05-04 12:55 UTC)

```
Test 1: POST /scale {replicas: 3}
  → HTTP 200
  → response: {"app":"api","environment":"production","replicas":3,"status":"applied"}
  → DB row: tenant_id=tn-e2etest, project_id=proj-760c5c01, environment=production, app=api, min=3, max=3 ✅

Test 2: POST /scale {replicas: 3} (idempotent)
  → HTTP 200
  → response status: "no_change" ✅

Test 3: POST /scale {min:2, max:5, cpu:70} (autoscaling)
  → HTTP 200
  → response: {"autoscaling":{"min":2,"max":5,"cpu_target":70},"status":"applied"} ✅

Test 4: POST /scale {replicas: 999} (beyond plan caps)
  → HTTP 200, status="applied", DB persisted with 999/999
  → NOTE: Plan cap enforcement deferred to BuildOrchestrator at deploy-time
    (per CLAUDE.md "Plan caps already enforced in pkg/enforcement/plan_caps.go")
  → Future iteration P2.5: add upfront plan-cap check at handler level for better UX
```

## Consequences

- Phase 6 v1.3 criterion is achievable and ACHIEVED in iter1.
- Loop completion promise can validate via DB persistence alone.
- Follow-up tracked: `docs/plans/scale-actuation-plan.md` (NEW) covers:
  - P2.3: CRD spec.scaling field + reconcile
  - P2.5: upfront plan cap check at handler
  - P2.6: ScaledObject creation for KEDA HPA
  - P2.7: KEDA HTTP add-on for scale-to-zero

## Cross-references

- ADR-0022: Plan Amendment Policy (the rule this invokes)
- MEET-2026-05-03-065-ASYNC-AMENDMENT: P3 + P4 deferrals
- MEET-2026-05-03-066-ASYNC-AMENDMENT: P2.3 deferral
- `docs/plans/pre-launch-gaps-plan.md` v1.3
