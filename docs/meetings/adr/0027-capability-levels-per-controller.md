# ADR-0027 — Operator Capability Levels Declared Per-Controller, Not Globally

## Status

Accepted — 2026-05-04 (MEET-2026-05-04-068)

## Context

LoopZero's 8 controllers are heterogeneous in operational maturity:

- **PreviewEnvironment:** TTL-based auto-cleanup, retry logic, state machine that survives crash-mid-reconcile, finalizer for namespace deletion. Solid Operator SDK Capability **Level IV** with Level V evidence in cleanup path.
- **PlatformBootstrap:** one-shot reconcile, no auto-healing, minimal metrics surface beyond reconcile counters. **Level III** — basic install/upgrade.
- **Application:** healthy reconcile + status conditions but no auto-recovery from app-level failures (relies on K8s deployment restartPolicy). **Level III/IV** boundary.
- **Tenant:** finalizer + multi-resource ensure + cross-cluster ClusterCredential. **Level IV.**
- **ClusterCredential:** periodic poll + Secret watch + connectivity probe. **Level IV** with monitoring gap.

Averaging these into a single "LoopZero is Level X" claim hides exactly the gaps Sprint 9 needs to close. CloudNativePG (the FAANG-bar reference for Level V declaration) maps capabilities per-feature with evidence pointers — that's why their Level V claim is credible.

Reference: [Operator SDK Capability Levels](https://sdk.operatorframework.io/docs/overview/operator-capabilities/) — 5 levels (I Basic → V Auto Pilot).

Roadmap §S9-T3 + gate OP-G20 require formal declaration.

## Decision

Each of the 8 LoopZero controllers gets an explicit Capability Level (I-V) declaration in `docs/technical/operators/capability-matrix.md`, with a 3-column evidence matrix per row:

| Capability | Evidence (file:line) | Verifying test |
|---|---|---|

The module has no single "LoopZero is Level X" claim. Per-controller declarations are the only authoritative format. ADR follow-ups F1-F3 below address rigor + decay risk.

This ADR ratifies plan T5.1 (matrix) + T5.2 (Level IV-specific metrics) with the following follow-ups integrated.

### Follow-ups (per platform-architect F1-F3)

1. **Rubric per level in `capability-matrix.md` itself:** Each level has measurable criteria. Example for Level IV ("Deep Insights"):
   - Required: ≥10 controller-specific metrics (per CloudNativePG reference)
   - Required: ≥3 SLO alerts per controller
   - Required: runbook in `docs/runbooks/operator-<name>.md`
   - Required: envtest spec proving deep insights surface
   - Without rubric, level claims are subjective; matrix becomes opinion forum.

2. **CI gate for matrix freshness (per dx-engineer):** `infra/scripts/capability-matrix-audit.sh` runs in `task validate:docs`:
   - Greps every `Setup*` function in `internal/{tenant,app,preview,platform}/`.
   - Asserts each appears in matrix with file:line + test name references.
   - Asserts referenced tests exist and pass: `go test -run "$test_name" ./operators/...`.
   - PR template includes mandatory checkbox: "I have updated capability-matrix.md if my change affects controller capability."

3. **External review per major release (or annually):** Matrix reviewed by someone who didn't write the controllers (platform-architect + sre-guardian + security-reviewer triad pre-launch). Catches self-promotion bias.

### Heal-script clarification (per platform-architect R4)

`task k3d:heal` H12-H15 are operator-INVOKED recovery scripts that run OUTSIDE the controller process. They do NOT count as Level V "Auto Pilot" (which means controller itself self-heals in-process without operator action). Matrix MUST distinguish:

- Level IV evidence: in-process auto-heal (e.g., reaper transitions in `application_reaper.go`, condition-wait in `preview_controller.go`).
- Operator-invoked scripts (heal scripts, runbooks): listed as Level III "Full Lifecycle" / Level IV "Deep Insights" supplements, NOT Level V.

### Level IV-specific metrics (per T5.2)

Required metrics per controller for Level IV claim:

- `loopzero_self_heal_total{controller, action}` — count auto-heal events (e.g., reaper transitions)
- `loopzero_orphan_cr_total{kind}` — count orphan CRs detected
- `loopzero_finalizer_blocked_seconds{kind}` — histogram of finalizer hold time
- `loopzero_reconcile_queue_depth{controller}` — current queue depth

Plus the metrics from ADR-0024 conditions:
- `loopzero_status_update_conflicts_total{controller}`
- `controller_runtime_active_workers{controller, binary}`
- `workqueue_depth{controller}` per binary
- `leader_election_master_status` per binary

## Rationale

### Why per-controller, not global

Single global level claim is dishonest by construction when controllers are heterogeneous. PreviewEnvironment (auto-cleanup, TTL, retry) is materially different from PlatformBootstrap (one-shot install). Averaging hides the gaps. CNPG-style per-controller table is the SOTA practice.

### Why 3-column evidence matrix

- **Capability** alone is opinion.
- **Capability + file:line** is verifiable but stale-prone.
- **Capability + file:line + verifying test** is verifiable AND machine-checkable (CI runs the test). This is the only format that survives review and survives decay.

### Why CI gate (vs honor-system DoD)

Per dx-engineer's analysis: "Honor-system DoDs decay in 2 sprints." Without machine enforcement, capability claims drift from reality the first time a controller is refactored. CI gate via `capability-matrix-audit.sh` is mechanical — engineers can't accidentally skip it.

### Why "heal scripts ≠ Level V"

Level V "Auto Pilot" means the controller itself self-heals without operator intervention. `task k3d:heal H12-H15` requires operator to run a script. Conflating these would overclaim capability — exactly the dishonest claim we're avoiding.

## Alternatives considered

- **Single global level claim with per-controller caveats in appendix:** Rejected — appendices are where honest information goes to die.
- **No formal level declaration:** Rejected — Operator SDK Capability Levels are the industry-standard framework; not declaring leaves LoopZero's maturity uncommunicated to integrators.
- **Honor-system DoD without CI gate:** Rejected — DoD lines decay within sprints.
- **External-only review (no internal claim):** Rejected — internal claim is needed for sprint planning ("PlatformBootstrap needs L4 metrics to advance").

## Consequences

### Enables

- Honest external communication: "Tenant: L4, Application: L4, Preview: L5, PlatformBootstrap: L3."
- Internal targeting: "PlatformBootstrap needs N metrics + M alerts + runbook for L4."
- Per-PR self-check: "Did my change affect a capability? Update the matrix row."
- Eligibility signal for production GA: every controller must reach declared L4 minimum before launch.

### Constrains

- Matrix maintenance is a recurring burden — every controller PR may require row update.
- CI gate adds ~30s to `task validate:docs` (acceptable).
- External review per major release adds governance overhead.

### Reversibility

- ADR reversible via new MEET + ADR.
- Matrix can be deleted; loses honest capability surface but doesn't break code.

## Risks

- **MEDIUM:** Matrix decay despite CI gate (engineers might `chmod -x` the audit script in frustration). Mitigation: gate runs in GitHub Actions, not just locally — cannot be bypassed.
- **MEDIUM:** Subjectivity at level boundaries (especially L4 "Deep Insights" vs L5 "Auto Pilot"). Mitigation: rubric per level (follow-up F1).
- **LOW:** Self-promotion bias in level claims. Mitigation: external review (follow-up F3).

## References

- `docs/plans/operators-sota-faang-grade-plan.md` v1.1 D5 + Phase 5 T5.1 + T5.2
- `docs/technical/operators/improvement-roadmap.md` §S9-T3
- `docs/technical/operators/sota-gates.md` OP-G20
- Operator SDK Capability Levels (https://sdk.operatorframework.io/docs/overview/operator-capabilities/)
- CloudNativePG capability matrix reference (per-feature)
- ADR-0023 — D1 split tree (per-controller capability requires per-binary clarity)
- ADR-0024 + ADR-0025 + ADR-0026 — required L4 metrics + status semantics + RBAC
- MEET-2026-05-04-068
