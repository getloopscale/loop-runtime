---
type: architecture-view
title: The target solution, by piece
measured_on: 2026-09-11
render: target-solution.html
---

# The target solution, by piece

**61 components, every one measured from disk on 2026-09-11** — not recalled. Recall is how
this project once reported 101 Helm releases where there are 59, and how it once reported
telemetry absent where `git clone` was running.

Interactive view: [`target-solution.html`](target-solution.html) — React Flow 12.11.6 bundled
inline, no CDN.

## What it shows

The ten pieces of [`../product/technical-pieces.md`](../product/technical-pieces.md), grouped
into three regions that follow the reading order: **what she touches → what puts it live →
what delivers it.** Every component carries a measured status.

| Status | Count | Means |
|---|---|---|
| ready | 23 | exists and is consumable |
| modify | 17 | exists and the plan changes it |
| build | 21 | does not exist yet |

## The distinction that keeps this drawing honest

**`build` includes a package that is written but not published.** `@loopzero/lens-sdk` has
code; nobody outside can use it. For OBJ-2 and OBJ-4, *written* and *published* are not the
same state, and collapsing them is what would make this drawing lie.

## Three things the measurement found

**`loopzero-lens` has zero published packages.** 1,048 commits, and `lens-sdk`, `lens-core`,
`lens-api`, `lens-web` are all `private`. REQ-7 — *every model call is traced without her
instrumenting it* — has no consumable artifact. PIECE-4 is red end to end.

**The four capabilities are at four different stages**, while OBJ-2 treats them as one:

| Capability | Package | State |
|---|---|---|
| memory | `@loopzero/memory` | 0.3.0 published |
| RAG | `@loopzero/rag` | 0.0.0-rc.0 |
| skills | `@loopzero/skills` + sdk, cli, mcp | 0.3.0, complete |
| prompts | `@loopzero/promptly-sdk` | 0.1.1, API private |
| trace | — | **nothing** |

**`loopzero-cluster-management` holds 16 commits**: README, CLAUDE.md and a wiki. The destination
of the extraction is empty, and the four compositions (59 releases) are still in the engine.

## Related

- [`engine-and-delivery.md`](engine-and-delivery.md) — R2 and R3 at component level
- [`as-is-to-be.md`](as-is-to-be.md) — the same ground, compared concern by concern
- [`../product/technical-pieces.md`](../product/technical-pieces.md) — the pieces this groups by
