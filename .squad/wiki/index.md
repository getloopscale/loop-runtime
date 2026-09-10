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

## Architecture views — measured 2026-09-11

Each view ships a Markdown concept and an interactive React Flow render, bundled inline with no
CDN. **Every status in them was measured from disk**, never recalled.

- [architecture/target-solution.md](architecture/target-solution.md) — 61 components grouped by
  piece: 23 ready, 17 changed, 21 absent
- [architecture/engine-and-delivery.md](architecture/engine-and-delivery.md) — the 59 releases
  named one by one, and the exact point where delivery breaks today
- [architecture/as-is-to-be.md](architecture/as-is-to-be.md) — seven concerns aligned row by
  row, with what stands behind each promise
- [architecture/alignment-review.md](architecture/alignment-review.md) — the review surface,
  and why 100% was never approval

## What these views agree on, and it is not comfortable

Two things carry signed requirements and **no plan at all**:

1. **A customer cannot fetch the published charts** — 401, and the entitlement the extraction
   RFC assumes was never built. None of the nine slices covers it
   ([#358](https://github.com/getloopscale/loopzero/issues/358)).
2. **The entry path has no owner**, and `loopzero-lens` — the whole execution-record capability —
   has zero published packages.

Both were found by measuring rather than by reading, and both are visible in every view above
because a drawing that hid them would be the wrong drawing.
