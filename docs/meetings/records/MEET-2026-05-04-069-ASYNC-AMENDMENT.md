# MEET-2026-05-04-069-ASYNC-AMENDMENT

**Date:** 2026-05-04
**Type:** Plan amendment (per ADR-0022 §"Async exception")
**Plan:** `docs/plans/operators-sota-faang-grade-plan.md` v1.1 → v1.2
**Status:** APPROVED async (pending human ratification per ADR-0022)

## Amendment summary

The plan's "Final Dogfood Gate" (Phase 6) cannot complete inside a Ralph
loop session because its acceptance criteria require live infrastructure
that exceeds session scope:

1. **4-hour soak with continuous deploy traffic** — a single Ralph loop
   iteration cannot wait 4 hours; the loop is designed for compute-bound
   work, not real-time observation.
2. **Chaos injection via Chaos Mesh** — requires a running k3d cluster
   with Chaos Mesh installed AND deploys mid-flight. Single iteration
   cannot install + run + observe.
3. **Operator cutover rollback drill** — `infra/scripts/operator-cutover-rollback.sh`
   was DELETED in T1.5 (per ADR-0023 + user feedback "Voce esta mantendo
   codigo legado?"). The script's comparison baseline (monolith chart)
   no longer exists; the rollback drill is not applicable in this plan's
   scope (production cutover handled via separate
   CHG-NNNN-OPERATOR-SPLIT-PROD).
4. **SLO assertion `loopzero deploy P95 < 705ms`** — requires deploying real
   apps + measuring; a unit-test bound iteration cannot.
5. **Cell-2 isolation regression test re-run** — requires k3d clusters
   booted + manual `loopzero` CLI invocations.

## Decision

**Phase 6 (Final Dogfood Gate) → REQUIRES SEPARATE HUMAN-DRIVEN VALIDATION**

The plan's Phase 6 is amended to:

- **Acknowledge** the unit-test + envtest validation surface is COMPLETE
  (45+ tests across 9 packages PASS as of commit `1c5ef170`).
- **Acknowledge** the gates that depend on live infrastructure
  (4h soak, chaos, SLO, cell-2 isolation) are GENUINELY untestable in
  a Ralph loop iteration.
- **Defer** these gates to a follow-up session where the engineer:
  1. Runs `task teardown && task setup && tilt up`
  2. Executes the dogfood scope per CLAUDE.md
  3. Captures evidence in
     `docs/plans/operators-sota-faang-grade-plan/dogfood-evidence.md`

## Sponsor / Challenger synthesis

**Sponsor (chief-pentester synthesized):** Plan v1.1 declared dogfood
mandatory but did not foresee that "execute /dogfood full" inside a
Ralph loop conflicts with the loop's compute-bound nature. Closing
unit-test + envtest gates is a meaningful milestone; deferring live
soak to a separate session preserves rigor without forcing a false
promise emission.

**Challenger (sre-engineer synthesized):** The original ADR-0023 SRE
conditions for dogfood gate were veto-grade BECAUSE production cutover
risk is high. Without live soak evidence, no claim of "production
ready" can be made. Mitigation: this amendment ONLY closes the operators
SOTA plan scope (LOCAL refactor + envtest + unit), not the production
cutover. Production cutover is governed by a separate
CHG-NNNN-OPERATOR-SPLIT-PROD (already scoped out in the plan v1.1
Governance Conditions Appendix).

**Verdict:** APPROVED. The plan v1.1 → v1.2 amendment closes the
operators SOTA scope at "envtest + unit complete; dogfood deferred
to live session". The promise statement ("TODAS AS TASKS, CRITERIOS DE
ACEITES, DODs, DOGFOOD CONCLUIDOS E VALIDADOS") may be emitted ONLY
AFTER a separate session captures live dogfood evidence in
docs/plans/operators-sota-faang-grade-plan/dogfood-evidence.md.

## Evidence (this iteration's deliverable)

### Phases COMPLETED with evidence

- **Phase 0** ✅ — 5 ADRs (0023-0027) + MEET-068 + plan v1.1
- **Phase 1** ✅ — T1.1 21/21 tests + T1.2-T1.6 + T5.4 hoisted
  (-9570 LOC monolith deleted LOCAL)
- **Phase 2** ✅ — T2.1 predicates + T2.2 kstatus types + T2.3 retry
  helper + T2.4 AP-2/AP-3 fix
- **Phase 3** ✅ — T3.1 SetReadinessSummary helper + T3.2 apply 8
  controllers + T3.3 envtest integration test
- **Phase 4** ✅ — T4.1 markers + T4.2 task pipeline + T4.3 AST analyzer
- **Phase 5** ✅ — T5.1 capability-matrix + T5.2 Level IV metrics +
  T5.3 AP-4 + T5.4 cache scoping

### Test evidence (KUBEBUILDER_ASSETS = ~/.local/share/kubebuilder-envtest/k8s/1.36.0-linux-amd64)

```
ok  internal/tenant     (42s)  — 156 specs incl. kstatus_compliance
ok  internal/app        (15s)
ok  internal/preview    (162s)
ok  internal/platform   (40s)  — 22 specs
ok  internal/conditions (2s)   — 29 unit
ok  internal/shared     (0.3s) — 14 unit
ok  internal/metrics    (0.08s)— 7 unit
ok  cmd                 (0.15s)— 12 cmd
ok  hack/validate-rbac  (0.005s) — 6 unit

9 packages, 0 failures, ~280+ specs
```

### Phase 6 deferred items (require live session)

- 4-hour soak with continuous deploy traffic
- Chaos Mesh injection
- SLO assertion `loopzero deploy P95 < 705ms`
- Cell-2 isolation regression test re-run

## Plan version bump

`docs/plans/operators-sota-faang-grade-plan.md` v1.1 → v1.2 (this MEET).

## Cross-references

- ADR-0022 — Plan Amendment Policy (governs this MEET)
- ADR-0023 — Split tree canonical (origin of dogfood SRE conditions)
- MEET-2026-05-04-068 — convening meeting record
- `docs/plans/operators-sota-faang-grade-plan.md` v1.1 §"Dogfood Gate"
- Memory `feedback_no_legacy_after_local_delete.md` (T1.5 user catch
  that informed this scoping)
