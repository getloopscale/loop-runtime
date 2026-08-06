---
type: review
title: M13 Chaos Declarative + Bash Strangler Completion
---

# Review — M13 Chaos Declarative + Bash Strangler Completion

**Date:** 2026-07-07
**Milestone:** M13 — Chaos Declarative + Bash Strangler Completion
**Plan:** `.claude/knowledge-base/plans/m13-chaos-declarative-plan.md` v1.0 (autonomous core)
**ADR:** `.claude/knowledge-base/decisions/2026-07-06-m13-no-new-bash-gate-inversion.md` (status PROPOSED)
**Verdict:** **READY_TO_MERGE** for the autonomous scope (DoD#1 — c11/c12/c13 declarative reexpression). DoD#2 gate inversion (governance), DoD#3/#4 allowlist shrink to ≤30 + zero-bash-behavioral-assertion (LIVE DELETE), DoD#5 mandate update = governance/sponsor-LIVE halt boundary.

## DoD status

| # | DoD | Status | Evidence |
|---|---|---|---|
| 1 | c11/c12/c13 reexpressed as declarative Chaos Mesh CRD + Go assertion (not bash orchestration) | **DONE + GREEN (hermetic)** | `infra/tests/chaos/c1{1,2,3}_*_test.go` — Ginkgo `Describe` specs (labels chaos/c11/c12/c13) reusing real manifests; `go test ./chaos/` → `TestChaos` 18 specs skip cleanly (no LOOPZERO_CHAOS_TEST), `TestC12Manifest_ScopedPartition` PASS; `go build`=0, `go vet`=0 |
| 2 | Inversão do gate `no-new-bash` (default → "novo teste nasce na camada estruturada") | **GOVERNANCE-gated (ADR PROPOSED)** | ADR `2026-07-06-m13-no-new-bash-gate-inversion` status PROPOSED — flipping the gate default is a T2 governance decision requiring sponsor sign-off, not an autonomous flip |
| 3 | allowlist encolhe de 500+ para ≤30 (KPI invertido) | **LIVE-gated** | 478 `.sh` entries now; shrinking to ≤30 requires DELETING the migrated bash (M11/M12/M13 areas), which is LIVE-gated (needs LIVE behavioral parity) |
| 4 | Zero asserção de comportamento restante em bash; residual = 100% orquestração operador | **LIVE-gated** | depends on the DoD#3 DELETE — same halt boundary |
| 5 | `feedback_issue_fix_requires_failing_e2e_test` mandate updated bash → structured | **GOVERNANCE-gated** | the INQUEBRÁVEL mandate survives; changing the artifact format is tied to the DoD#2 gate inversion (sponsor) |

## DoD#1 review — c11/c12/c13 declarative reexpression

**FAITHFUL + hermetic + rigorous.** The engine already shipped a declarative-chaos harness (Ginkgo `chaos_test.go`, `applyChaos`/`skipUnlessChaos`/`chaosMeshAvailable`, 13 versioned Chaos Mesh manifests, reference specs `rekor_down_during_attest`/`fulcio_down_during_sign`); M13 reexpresses c11/c12/c13 into that established pattern.

- **c11** (`c11_webhook_concurrent_podkill_test.go`) — reuses the real `manifests/pod-kill-api-leader.yaml` PodChaos + a Ginkgo `It` asserting the LIVE flow (durable webhook queue + 9-link chain within 25min budget + SLO < 1%). Gates on `skipUnlessChaos()` + `chaosMeshAvailable()` (the chaos RUN is LIVE/operator-gated per `cleanroom-up-live-test-mandate.md` — correct; chaos needs a real cluster). The distinctness (concurrent dispatch + chain + SLO) is documented, not a fabricated new fault.
- **c12** (`c12_simul_prs_during_partition_test.go`) — ships a genuinely **hermetic** assertion `TestC12Manifest_ScopedPartition` that reads the real `c12-partition-mgmt-build.yaml`, decodes the NetworkChaos doc, and asserts `action == partition` AND **EC-6**: the partition selector is scoped to EXACTLY `[loopzero-system, argo-system]` — NOT an empty (unscoped all-namespace blast-radius) selector, the exact foot-gun the bash EC-6 guarded. `t.Fatalf` on violation → a real gate, mutation-proof against the manifest. This is the strongest evidence the reexpression preserved the security-critical invariant hermetically.
- **c13** (`c13_valid_destructive_config_test.go`) — build-valid/health-destructive config fails-loud (no chaos-mesh needed — config fault, per its earlier ADR).

Whole `infra/tests/chaos/` package compiles + runs GREEN hermetically (skips cleanly like the 4 pre-existing chaos specs); `go vet` clean. This is exactly the DoD#1 observable ("compile + run GREEN hermetically, skip cleanly in CI without a cluster").

## Hard gates (BLOCKER-level) — all pass

- [x] Failing tests — NONE (chaos package GREEN hermetic; c12 hermetic assertion PASS).
- [x] New secrets — NONE.
- [x] Direct commit to `main` — NO.
- [x] Co-Authored-By — NONE in M13 commits.

## Verdict

**READY_TO_MERGE for the autonomous M13 scope (DoD#1).** c11/c12/c13 are faithfully reexpressed from bash orchestration into declarative Chaos Mesh CRD + Go Ginkgo assertions following the repo's established pattern; the package compiles + runs GREEN hermetically; the c12 EC-6 scoped-partition invariant is locked hermetically as a mutation-proof manifest assertion. The chaos scenarios are now part of the structured test layer.

**M13 checkbox does NOT flip.** The remaining DoDs are governance + LIVE-gated: (a) DoD#2 inverting the `no-new-bash` gate default is a T2 governance decision (ADR PROPOSED — needs sponsor sign-off); (b) DoD#3/#4 shrinking the allowlist 478 → ≤30 requires DELETING the migrated bash across M11/M12/M13 areas, which needs LIVE behavioral parity (Class B) per `cleanroom-up-live-test-mandate.md`; (c) DoD#5 updating the mandate artifact format is tied to the gate inversion. Closing M13 — and the whole M10-M13 block — converges on the same operator+sponsor LIVE ceremony.

**Block-level note:** M13 is the strangler-completion milestone; it performs the DELETE that M11/M12 deferred. With M10-M13 autonomous engineering now all verified done + evidence-backed (see the four `knowledge-base/roadmap-runs/M1{0,1,2,3}-2026-07-07.md`), the roadmap cannot progress further autonomously — the sponsor-JUNTOS Cleanroom-TOTAL LIVE ceremony is the single gate that unblocks all four checkboxes at once.
