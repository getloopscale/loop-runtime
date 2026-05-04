# MEET-2026-05-03-065 — ASYNC AMENDMENT (Ralph loop iteration)

**Date:** 2026-05-03
**Topic:** `pre-launch-gaps-plan.md` — Phase 3 (cosign keyless cutover) + Phase 4 (CI cron evidence) governance-deferral via ADR-0022 async amendment workflow
**Type:** Task deferral (per ADR-0022 amendment categories)
**Loop context:** Ralph loop iteration 1 of `pre-launch-gaps-plan.md`. Agent runs without synchronous human meeting access.
**Pending ratification:** YES — this record is treated as pending human ratification before plan completion declared.

---

## Stage 1 — Context

### Phase 3 (Cosign keyless cutover)

Plan calls for 3-phase cutover with **wall-clock dependencies**:

- T3.1: Dev cluster keyless enforce + **7 days observation**
- T3.2: Staging keyless enforce after T3.1 GREEN + **7 days observation**
- T3.3: Production keyless enforce after T3.2 GREEN + **7 days observation**
- T3.4: COSIGN_KEY revoke after T3.3 GREEN + **24h confirmation**

Total wall-clock: **22 days minimum**. Cannot be compressed — observation periods exist precisely to detect latent failures (cert expiration, Rekor latency, admission webhook regressions) that require real time.

### Phase 4 (CI cron evidence)

Plan calls for:

- T4.1: `gh workflow run cell2-soaks.yml -f stage=stage1` — requires (a) `gh` CLI authenticated to GitHub, (b) ~75min wall-clock to complete the 1h soak + cleanup buffer.
- T4.2: `gh workflow run platform-health-7day-watch.yml` — requires same gh auth + ~1h.
- T4.3: Closure docs depending on T4.1/T4.2 artifacts.

Loop context lacks: gh CLI authentication (no token in environment), ability to wait 75 minutes wall-clock for a single workflow.

---

## Stage 2 — Synthesized specialist positions

### Sponsor (Platform Architect — synthesized)

**Position:** APPROVE deferral.

**Reasoning:** Plans with wall-clock dependencies cannot be completed inside a single ralph loop iteration without violating the loop's atomicity. Deferring P3 and P4 to operator-driven execution is the only honest path. The plan body documents the wall-clock requirement (Phase 3.1 → 3.2 dependency: "7 days GREEN observation"), so deferral is consistent with the plan's own internal logic.

**Conditions:**
- All P3+P4 acceptance criteria preserved exactly as plan v1.0 specified.
- Operator runbook references unchanged (`docs/runbooks/cosign-keyless-cutover.md`).
- Deferral does NOT relax the ADR D2 conditions (HALT criteria, identity regex, etc.).

### Challenger (SRE Engineer — synthesized)

**Position:** APPROVE deferral with conditions.

**Reasoning:** Cosign cutover is a security-sensitive operation. Doing it inside a loop without operator oversight + Rekor evidence collection would be reckless. Deferral preserves the safety-by-design.

**Conditions:**
- P3 cutover MUST happen with operator monitoring active during 7-day observation periods.
- Pre-cutover smoke test in dev MUST be green before T3.1 enforce flip.
- COSIGN_KEY revoke (T3.4) requires ≥ 1 senior engineer sign-off captured in runbook.

### Verification Architect (synthesized)

**Position:** APPROVE deferral. **Verification path:**
- This MEET record is the deferral evidence.
- Plan version bump from v1.0 → v1.1 in next commit referencing this MEET.
- Plan acceptance criteria for P3+P4 stay UNCHANGED — deferral != relaxation.
- Plan completion (`<promise>` emit) requires ALL non-deferred phases (P0, P1, P2, P5, P6) GREEN.

---

## Stage 3 — Decision

### Verdict: APPROVED — P3 and P4 deferred to operator-driven execution

**Amendment type:** Task deferral (per ADR-0022).

**Plan version bump:** v1.0 → v1.1 — registered in this MEET + plan body annotation.

**Mandatory conditions (carried forward):**

1. P3.1 dev cutover proceeds with full 7-day observation. Operator runs:
   ```bash
   yq -i '.clusterImagePolicyKeyless.mode = "enforce"' infra/helm/admission-policies/values.dev.yaml
   argocd app sync admission-policies
   # Observe 7 days; track p95 verify_duration_seconds.
   ```
2. P3.2 staging cutover only after dev 7-day GREEN. Same playbook.
3. P3.3 prod cutover only after staging 7-day GREEN. Plus key-based step removal from CI.
4. P3.4 COSIGN_KEY revoke only after prod 24h GREEN. Senior engineer sign-off in runbook.
5. P4.1+4.2 manual `gh workflow run` triggers + artifact capture by operator.
6. P4.3 closure docs after artifacts collected.

**Plan completion criteria (revised):**

- Original v1.0: complete P0-P6.
- Amended v1.1: complete P0, P1, P2, P5, P6 (in-loop). P3 + P4 marked "operator-deferred per MEET-2026-05-03-065" — not blocking loop completion, but blocking Phase 6 Dogfood Gate criteria that explicitly mention them (`loopzero scale` E2E and "cosign keyless verify GREEN").
- Operator executes P3 + P4 outside loop in subsequent calendar weeks.

**Rejected alternatives:**

- "Compress 7-day observation to 1h": rejected. Defeats safety purpose.
- "Skip P3/P4 entirely": rejected. They are real production-readiness requirements.
- "Relax DoD to 'CODE complete only'": already done in iter1 of `infra-sota`; risk of cumulative DoD relaxation.

---

## Stage 4 — Owner + Sequence

| Task | Owner | Order | Trigger |
|---|---|---|---|
| P3.1 dev cutover | platform-security-engineer + sre-engineer | post-loop, iter1 of operator | Operator decides timing |
| P3.2 staging cutover | same | post-loop, after T3.1 7d GREEN | Wall-clock dependency |
| P3.3 prod cutover | same + change-validation-planner | post-loop, after T3.2 7d GREEN | Wall-clock dependency |
| P3.4 COSIGN_KEY revoke | platform-security-engineer | post-loop, after T3.3 24h GREEN | Wall-clock dependency |
| P4.1 cell2-soaks Stage 1 trigger | infra-engineer | post-loop, any time with gh auth | Manual operator action |
| P4.2 platform-health 1h trigger | infra-engineer | post-loop, parallel to P4.1 | Manual operator action |
| P4.3 closure docs | report-writer | post-loop, after P4.1+4.2 artifacts | Sequential |

---

## Stage 5 — Ratification status

**Status:** PENDING_HUMAN_RATIFICATION

This record is emitted via async-amendment workflow per ADR-0022 because the loop cannot synchronously convene a real meeting. Human reviewer (operator) must review this MEET record and either:

- **Ratify:** acknowledge in PR review. Record becomes equivalent to a real MEET.
- **Override:** request changes. Loop must re-emit amended MEET-ASYNC.

If no ratification arrives by next operator session, the loop should NOT re-emit `<promise>` claiming completion that depends on these deferrals. Loop will continue.

**Ratification placeholder:**

```
- [ ] Reviewed by: __________
- [ ] Date: __________
- [ ] Verdict: ratified / requires changes
```

---

## References

- ADR-0022 (`docs/meetings/adr/0022-plan-amendment-policy.md`)
- `docs/plans/pre-launch-gaps-plan.md` v1.0 (current) → v1.1 (post this MEET)
- `docs/runbooks/cosign-keyless-cutover.md` (P3 operator runbook)
- `.github/workflows/cell2-soaks.yml` (P4.1 trigger target)
- `.github/workflows/platform-health-7day-watch.yml` (P4.2 trigger target)
