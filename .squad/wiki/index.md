---
okf_version: "0.2"
type: index
---

# LoopZero — project knowledge

The durable half of what this project knows: what the product is, what it is made of, and how
it looks today against what it is meant to become. Dated run records live in
`.squad/records/`; this bundle holds what outlives a run.

## Product — the signed cascade

`cycle-brainstorm`, signed by `paulo` on 2026-09-11. Every backlog item traces to an `OBJ-N`
declared here.

- [product/product-vision.md](product/product-vision.md) — who it is for, the problem, and the
  four things LoopZero explicitly is **not**
- [product/objectives.md](product/objectives.md) — `OBJ-1..4`, each with a metric we can take
  ourselves and a horizon of 2026-09-13
- [product/trd.md](product/trd.md) — `REQ-1..18`, each citing the objective that justifies it
- [product/technical-pieces.md](product/technical-pieces.md) — `PIECE-1..10`, responsibilities
  with a boundary, cut deliberately **not** by repository
- [product/alignment.md](product/alignment.md) — the sign-off, its amendment history, and the
  limitation declared before signing

## Design — the system drawn before a backlog is filed against it

`cycle-design`, phase 0. Four drawings force the decisions no product retrofits; the fifth is
DERIVED from them, because a map drawn first looks like design happened and forces no choice.

- [design/states.md](design/states.md) — D1, the lifecycle an application moves through
- [design/trust.md](design/trust.md) — D2, where the trust boundaries sit
- [design/sequence.md](design/sequence.md) — D3, call order including failures
- [design/durability.md](design/durability.md) — D4, what survives what
- [design/system-map.md](design/system-map.md) — D5, the component map derived from the four
- [design/sign-off.md](design/sign-off.md) — the sign-off, **unsigned**, 7 boxes — every design question answered
- [design/what-needs-your-review.md](design/what-needs-your-review.md) — what a person must decide

**All seven design questions are answered as of 2026-09-11.** D3 by C1 of the extraction RFC, before
the drawing asked. D4 by locating agent state in `loopzero-memory` — ours, and ready. D2 by
[decisions/shared-kernel-on-the-managed-door.md](decisions/shared-kernel-on-the-managed-door.md),
researched against six platforms. And the last four by one invariant —
[**no agent state lives in the pod**](decisions/agent-state-never-lives-in-the-pod.md) — from which a
suspended phase, a draining phase, the egress shape and the checkpoint question all fall out.

**What remains is work, not decisions**: the tenant egress policy, a measurement of how long a turn
takes, and the grace period set from it.

## Architecture views — measured 2026-09-11

Each view ships a Markdown concept and an interactive render — React Flow bundled inline for four of
them, hand-authored SVG for the paved road — with no CDN. **Every status in them was measured from disk**, never recalled.

- [architecture/target-solution.md](architecture/target-solution.md) — 61 components grouped by
  piece: 23 ready, 17 changed, 21 absent
- [architecture/engine-and-delivery.md](architecture/engine-and-delivery.md) — the 59 releases
  named one by one, and the exact point where delivery breaks today
- [architecture/as-is-to-be.md](architecture/as-is-to-be.md) — seven concerns aligned row by
  row, with what stands behind each promise
- [architecture/alignment-review.md](architecture/alignment-review.md) — the review surface,
  and why 100% was never approval
- [architecture/paved-road.md](architecture/paved-road.md) — 8 nodes: which routes cross which
  trust boundary, drawn to the `diagram-design` contract

## Decisions

- [decisions/byoc-as-the-only-mode-has-no-precedent.md](decisions/byoc-as-the-only-mode-has-no-precedent.md)
- [decisions/two-doors-managed-and-byoc.md](decisions/two-doors-managed-and-byoc.md) — managed PaaS for the solo developer, BYOC for privacy
- [decisions/shared-kernel-on-the-managed-door.md](decisions/shared-kernel-on-the-managed-door.md) — shared kernel on the managed door; BYOC is the isolation product
- [decisions/agent-state-never-lives-in-the-pod.md](decisions/agent-state-never-lives-in-the-pod.md) — the invariant that answers the four remaining design questions
  — self-host was dropped on 2026-09-11 and BYOC became the whole offer. Research the same day found
  **no vendor whose only enterprise mode is BYOC with a vendor-hosted control plane**, and two
  vendors arguing against the cross-account-role mechanism specifically. The decision stands; the
  evidence is recorded while reversing is still cheap.
- [decisions/eks-via-awsmanagedcontrolplane.md](decisions/eks-via-awsmanagedcontrolplane.md) — a
  cell's control plane is EKS via CAPA's `AWSManagedControlPlane`, decided on the one row nobody
  had checked: EKS standard support reaches 1.36 and our libraries are 1.36, so the constraint
  that would have flipped it does not bite. `AWSManagedMachinePool` stays out — still
  experimental — which means AMIs remain ours to build.

## What these views agree on, and it is not comfortable

1. **The entry path has no owner**, and `loopzero-lens` — the whole execution-record capability — has
   zero published packages. Signed requirements, no work assigned.
2. **BYOC is the only mode, and no verified vendor runs that configuration.** Not a defect — an
   unprecedented shape, recorded above.

**One thing dropped off this list on 2026-09-11.** *A customer cannot fetch the published charts*
([#358](https://github.com/getloopscale/loopzero/issues/358)) was the loudest gap here until self-host was
dropped. Its premise was that a customer must fetch; under BYOC nobody outside does. The issue stays
open for the narrower defect underneath — the extraction RFC still describes an entitlement token in
the present tense — but it is no longer between a customer and a running cell.

Both were found by measuring rather than by reading, and both are visible in every view above
because a drawing that hid them would be the wrong drawing.
