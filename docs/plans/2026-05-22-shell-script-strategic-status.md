# Implementation Status — 2026-05-22 Shell Script Strategic Plan

> **Live progress tracker for Ralph loop iterations.** Updated at end of each
> iteration. Next iter reads this to pick up where previous left off.

**Plan version:** v1.3 (`docs/plans/2026-05-22-shell-script-strategic-plan.md`)
**Branch:** `refactor/shell-scripts-strategic`
**Promise:** "TODAS AS TASKS, CRITERIOS DE ACEITES, DODs CONCLUIDOS E VALIDADOS CLEANROON, DOGFOOD E CHAOS-TEST FUNCIIONAIS"
**Promise eligibility:** ❌ FALSE — Phases 1-7 + 3 LIVE gates pending

---

## Iteration log

### Iter 1 (2026-05-22) — Phase 0 T0.1 COMPLETE (engineering side)

**Commit:** `bd5ffa1e` — `feat(scriptsaudit): Phase 0 T0.1 — scripts audit Go tool + baseline CSV`

**Done:**
- `pkg/quality/scriptsaudit/` package (440 LOC) — ScanRepo + Disposition heuristics + CSV emitter
- `pkg/quality/scriptsaudit/check_test.go` — 21 tests GREEN, coverage 91.8%
- `cmd/loopzero-ops/` minimal — `quality scripts-audit` subcommand wired
- `docs/scripts-audit/2026-05-22.csv` — baseline 257 scripts / 42,793 LOC
- `docs/scripts-audit/REVIEWED.md` — operator review template + disposition distribution
- `.gitignore` exception for `/cmd/` (prior catastrophic block — same lesson as `/scripts` 2026-05-20)
- `go.work` adds `./cmd/loopzero-ops` module

**Pending (operator HUMAN GATE):**
- Operator signature in `REVIEWED.md` confirming baseline review before Phase 1 destructive actions

---

## Phase status matrix

| Phase | Task | State | Owner |
|-------|------|-------|-------|
| **0 Real Usage Audit** | T0.1 audit Go tool + baseline CSV | ✅ **DONE** (engineering); operator review PENDING | Ralph + Operator |
| **1 Delete Dead Code** | T1.1 dead-set validation (manual) | 🟡 Blocked by T0.1 operator review | Operator |
| | T1.2 bulk delete validated set | ⚪ Pending T1.1 | Ralph |
| **2 Delegate to Helmfile** | T2.1 audit bare-helm calls | ⚪ Pending Phase 1 | Ralph |
| | T2.2 fix helmfile releases (gitea PASSWD + kratos/hydra YAML escape) | ⚪ Pending T2.1 | Ralph |
| | T2.3 rewrite infra:install:mgmt (T2.3.a master-sync + T2.3.b portable base64) | ⚪ Pending T2.2 | Ralph |
| | T2.4 validate cleanroom funcional | 🔴 **HARD STOP** — requires `task clean:all` + LIVE | Operator |
| **3 Deprecate task setup** | T3.1 update CLAUDE.md + README + dev-loop.md | ⚪ Pending Phase 2 | Ralph |
| | T3.2 deprecation warning in task setup | ⚪ Pending T3.1 | Ralph |
| **4 Migrate to Ginkgo** | T4.1 audit parity/dogfood scripts | ⚪ Parallel-eligible post-Phase-3 | Ralph |
| | T4.2 convert top-5 parity scripts | ⚪ Pending T4.1 | Ralph |
| | T4.3 convert remaining parity + dogfood | ⚪ Pending T4.2 | Ralph |
| | T4.4 delete _legacy/ | ⚪ Pending T4.3 | Ralph |
| **5 SDK-native Go** | T5.1 Pulumi Automation API (pulumi-archive + pulumi-orphan-rm) | ⚪ Parallel-eligible post-Phase-3 | Ralph |
| | T5.2 migrate generators to //go:generate | ⚪ Pending T5.1 | Ralph |
| | T5.3 remaining pure-logic gates | ⚪ Pending audit results | Ralph |
| **6 Lock The Door** | T6.1 allowlist.md | ⚪ Pending Phase 5 | Ralph |
| | T6.2 no-new-bash gate (+ EC-04 fetch-depth + EC-06 rename) | ⚪ Pending T6.1 | Ralph |
| | T6.3 no-shell-wrap audit gate | ⚪ Pending T6.2 | Ralph |
| | T6.4 final inventory validation | ⚪ Pending T6.3 | Ralph |
| **7 Adopt External Tools** | T7.1 Pluto CI gate | ⚪ Parallel-eligible (zero overlap with Phase 6) | Ralph |
| | T7.2 OpenCost helm + Prometheus integration | ⚪ Parallel-eligible | Ralph |
| **Final Gates** | `/cross-validation 2026-05-22-shell-script-strategic` | 🔴 **HARD STOP** | Operator + Ralph |
| | `/dogfood full` on clean infra | 🔴 **HARD STOP** | Operator |
| | `/chaos-test --full` validation | 🔴 **HARD STOP** | Operator |

Legend: ✅ done · 🟡 in-progress · ⚪ pending · 🔴 hard stop (operator-required action)

---

## Next iteration: recommended action

**Option A (default — RECOMMENDED):** Continue with Phase 6+7 paralelo (zero dependency on Phase 1-5 for engineering side). T6.2 (no-new-bash gate) is high-value gate that locks regression even before phases 1-5 reduce inventory. T7.1 (Pluto) and T7.2 (OpenCost) are independent and unblock go-to-market gates.

**Option B:** Implement Phase 2 engineering (T2.1 + T2.2 + T2.3) preparing for operator validation in T2.4. EC-01 + EC-02 must be applied (master-sync, portable base64).

**Option C:** Wait for operator review of T0.1 baseline before any further work.

**Why not Phase 1.2 (delete):** T1.1 validation requires operator manual review per plan. Ralph cannot mass-delete without that signature.

---

## HARD STOPS for promise eligibility

Promise statement requires "CLEANROON, DOGFOOD E CHAOS-TEST FUNCIIONAIS" — all 3 require LIVE infrastructure that Ralph cannot operate:

| Gate | Requirement | Ralph capability |
|------|-------------|------------------|
| Cleanroom | `task clean:all` (destroys 4 k3d clusters) + `task setup` (30+min) + `tilt up` + `task doctor` PASS=38 FAIL=0 | ❌ Cannot — `task clean:all` is destructive operator action |
| Dogfood | `/dogfood full` on clean infra + 12 journey suite PASS + Prova de Vida + Prova de Morte | ❌ Cannot — requires LIVE pods + JWT (TTL=1h) |
| Chaos-test | `/chaos-test --full` 10 scenarios with chaos-mesh injection + baseline measure + cleanup | ❌ Cannot — requires running cluster + chaos-mesh |

**Conclusion:** Ralph can complete all engineering side (Phases 0-7 code/tests/CI gates) but the 3 LIVE gates require operator handoff. Promise will remain false until operator validates.

---

## Honest assessment

- **Engineering progress:** 1/22 tasks (T0.1 complete)
- **Hard stops:** 4 (T2.4 cleanroom + final dogfood + chaos + cross-validation operator action)
- **Promise eligibility:** FALSE until all 22 done + 4 LIVE gates PASS
