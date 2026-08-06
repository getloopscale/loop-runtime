---
slug: engine-architecture-hardening
generated_by: roadmap-feature
new_milestone_id: M9
date: 2026-07-02
status: completed
source_report: architect-output/architecture-report.md
---

# Feature grill — Engine Architecture Hardening (M9)

Amendment of `loopzero/ROADMAP.md` (dashboard-journey roadmap) with one new engineering
milestone derived from the FAANG-level architecture audit (score 77/100, verdict
`REFACTOR LIGHTLY`).

## Pre-flight notes

- ROADMAP.md uses `### M<N> — [ ]` (3-hash) template variant; skill's `## M<N>`
  parser adapted. Next free ID = M9 (existing M0..M8 are dashboard journeys).
- Category note (surfaced to sponsor, decision recorded): the findings are internal
  refactor (`behavior_change: none` on 7 of 8 steps). Sponsor chose to track it as M9
  in the dashboard roadmap despite the skill's "refactor → ad-hoc" guidance.
- Working tree was DIRTY at Step 0 → no auto-commit (Step 6.6). Files edited:
  ROADMAP.md + CHANGELOG.md; sponsor commits manually.

## Step 3 — out-of-scope cross-check

PASS — no keyword overlap with the 4 off-limits items (SSO/SAML, multi-cell
auto-rebalance, Vercel-style preview, CLI parity). No removal, no false-positive.

## Grill answers

### Q1 — What is this feature and why NOW?

Engine architecture hardening: apply the 8-step behavior-preserving migration plan
from the audit to fix the 5 MEDIUM findings.

**Why now: de-risk before extractions.** Finding #3 — `pkg/quality` (14.6k LOC ≈ 59%
of `pkg/`) would be dragged into `loopzero-contracts` on extraction — is a boundary
misfit that hardens into real debt post-split. Doing the relocation + package splits
now, while every step is behavior-preserving, keeps the contracts base lean and
de-risks the imminent decomposition cut.

### Q2 — Dependencies (which milestones must be [x] first)?

**None — orthogonal.** The hardening is an internal engine refactor, orthogonal to
the dashboard journeys M0-M8. It can start at any time independent of journey
progress. (Note: the report's "before contracts extraction" urgency references the
*decomposition* roadmap in the umbrella `loopzero-cloud/ROADMAP.md`, not the dashboard
journeys — so within THIS roadmap the functional dependency is genuinely none.)

### Q3 — Definition of Done (derived from the audit's 8-step migration plan)

See the M9 milestone block in ROADMAP.md. 5 verifiable bullets covering steps 1-8;
success oracle is `go build ./... && go test ./... && task quality:gates && task lint`
green after every step, no new import cycles, and an architect-output re-score ≥ 85
(Keep band).

### Q4 — Top 2 NEW risks

1. Import rewiring across ~38k LOC of flat packages (db/services/routes) can break
   call sites the compiler misses at the edges (reflection, build tags). Mitigation:
   incremental per-step `go build && go test` gate; every step `behavior_change: none`.
2. `pkg/quality` relocation is a T2 change (shared `pkg/`) that could destabilize the
   77 CI quality gates if a gate importer path is missed. Mitigation: `task
   quality:gates` after steps 3/4/7; a T2 governance meeting is required before
   implementation per `.claude/rules/pre-release-governance.md`.

## Step 5 — SOTA delta

No — existing references sufficient (internal refactor; design source is the audit
report). peers_added: 0. `_catalog.md` untouched.
