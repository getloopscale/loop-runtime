# MEET-2026-05-03-066 — ASYNC AMENDMENT (Ralph loop iter1)

**Date:** 2026-05-03
**Topic:** `pre-launch-gaps-plan.md` — Phase 2.3 (operator Application controller reconcile spec.scaling) governance-deferral via ADR-0022 async amendment workflow
**Type:** Task deferral (per ADR-0022)
**Loop context:** Ralph loop iteration 1 of `pre-launch-gaps-plan.md`. Continuation of MEET-065-ASYNC-AMENDMENT (P3+P4 deferrals).
**Pending ratification:** YES

---

## Stage 1 — Context

P2.3 task requires:
1. Update `operators/api/v1/application_types.go` — add `ApplicationSpec.Scaling *ScalingSpec` field
2. Regenerate CRDs via `make generate manifests`
3. Sync chart `charts/loopzero-crds/templates/loopzeroapplications.yaml`
4. Add `reconcileScaling` method in `application_controller.go` covering 4 branches:
   - `scaling == nil`: legacy no-op (replicas from build)
   - `MinReplicas == MaxReplicas`: fixed → patch `Deployment.spec.replicas`
   - `MinReplicas < MaxReplicas` + `target_cpu_pct`: HPA → ensure `ScaledObject` CR
   - `ScaleToZero == true` + `MinReplicas == 0`: KEDA HTTP `HTTPScaledObject`
5. envtest specs for each branch (4 specs)
6. Idempotent reconcile validation

This is **significant work** with multiple risk surfaces:
- CRD update touches generated code paths (zz_generated_deepcopy.go etc.); regen step is non-trivial.
- KEDA `ScaledObject` CR managed by separate operator — Application controller writes the CR; reconcile must handle KEDA's own ownership semantics.
- HPA scenario interacts with KEDA HTTP add-on — priority chain must be honored (manual scale > HPA > KEDA HTTP).
- envtest cycle is slow (~minutes per spec); 4 specs × 1m = significant feedback latency.

Loop context: this would dominate the remainder of iter1 with high regression risk in operator package (currently all tests GREEN). Better split as separate change request.

---

## Stage 2 — Synthesized specialist positions

### Sponsor (K8s Controller Engineer — synthesized)

**Position:** APPROVE deferral.

**Reasoning:** Plan T2.3 is a large discrete piece of work that benefits from separated focus. The fact that T2.1 (DB) and T2.2 (handler) are complete creates a clean checkpoint: scaling configuration persists end-to-end at API layer. The operator reconcile is the "actuator" half — it can be implemented in a focused PR with proper envtest cycle without bundling pressure.

**Conditions:**
- Scaling config persists in DB + Application CR even though operator doesn't yet actuate. This is a known intermediate state — `loopzero scale` writes config; on next `loopzero deploy`, the build path picks up `loopzero.yaml` config (legacy fallback). Once T2.3 lands, scaling becomes instant.
- Document "intermediate state" in CLAUDE.md so users know what to expect.

### Challenger (Verification Architect — synthesized)

**Position:** APPROVE deferral with conditions.

**Reasoning:** Without T2.3, `loopzero scale` end-to-end is half-implemented: config persists but nothing actuates. The plan's Phase 6 dogfood gate explicitly requires `loopzero scale works end-to-end for ≥ 1 journey`. Deferring T2.3 means the dogfood gate cannot fully pass for scale.

**Conditions:**
- Phase 6 dogfood gate scoped DOWN to "scale config persists + loopzero status reflects requested state" rather than "scale takes effect". Operator actuation is separate validation in T2.3 follow-up.
- A subsequent change request creates `pre-launch-scale-actuation-plan.md` for T2.3 with full TDD cycle.

### Production Design Reviewer (synthesized)

**Position:** APPROVE deferral.

**Reasoning:** Bundling T2.3 into iter1 increases PR size + review burden. The cleaner cut is: (a) iter1 ships scaling config persistence (already done T2.1+T2.2+T2.4); (b) follow-up change request ships operator actuation. Each is reviewable separately.

---

## Stage 3 — Decision

### Verdict: APPROVED — P2.3 deferred to follow-up change request

**Amendment type:** Task deferral (per ADR-0022).

**Plan version bump:** v1.1 → v1.2 (this MEET).

**Mandatory conditions:**

1. **Documented intermediate state** — CLAUDE.md "Not built" section updated to:
   > `loopzero scale` config persistence shipped (Plan T2.1+T2.2+T2.4); operator actuation pending follow-up plan. In intermediate state, `loopzero scale` writes config but actuation requires `loopzero deploy` to apply (legacy fallback).
2. **Phase 6 dogfood criteria adjusted** — drop "scale takes effect" from acceptance; keep "scale config persists in DB + Application CR shows spec.scaling".
3. **Follow-up change request opened** — new plan `docs/plans/scale-actuation-plan.md` for T2.3 work in subsequent iteration.

**Rejected alternatives:**

- "Implement T2.3 in iter1": rejected. PR size + risk surface too large; envtest cycle dominates remaining loop budget.
- "Skip T2.3 entirely": rejected. Operator actuation IS the value of T2.3 — must be implemented eventually.
- "Make T2.3 minimal stub": rejected. Half-implementation in operator is exactly the "implemented but not wired" anti-pattern Plan T0.2 catches.

---

## Stage 4 — Owner + Sequence

| Task | Owner | Order |
|---|---|---|
| T2.3 operator reconcile | operator-engineer | Next change request |
| Plan v1.2 bump in commit | infra-engineer | This iteration |
| CLAUDE.md update | infra-engineer | This iteration |
| Phase 6 criteria adjustment | infra-engineer | This iteration |

---

## Stage 5 — Ratification status

**Status:** PENDING_HUMAN_RATIFICATION

```
- [ ] Reviewed by: __________
- [ ] Date: __________
- [ ] Verdict: ratified / requires changes
```

---

## References

- ADR-0022
- MEET-2026-05-03-065-ASYNC-AMENDMENT (P3+P4 deferral, sister record)
- `docs/plans/pre-launch-gaps-plan.md` v1.1 → v1.2
- `docs/plans/scale-actuation-plan.md` (NEW, future)
