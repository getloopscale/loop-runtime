---
type: design-drawing
title: D5 — the component map, DERIVED from the four decisions above
date: 2026-09-10
status: draft
---

# D5 — Components, derived from D1–D4

**Drawn last, on purpose.** A component map produced before the four decisions looks
like design happened and forces no choice. This one is a summary of them, and every
`PIECE-N` from `technical-pieces.md` has a place in it.

```mermaid
flowchart TB
    subgraph author[Authoring]
        p1[PIECE-1 Authoring surface]
        p5[PIECE-5 Prompt and skill catalogue]
    end
    subgraph path[From nothing to running]
        p6[PIECE-6 Entry path]
        p10[PIECE-10 Build path]
        p7[PIECE-7 Publishing]
        p8[PIECE-8 Distribution]
    end
    subgraph cell[Cell - customer AWS account]
        p9[PIECE-9 Cell provisioning]
        app[Application workload]
    end
    subgraph state[State]
        p2[PIECE-2 Agent state]
        p3[PIECE-3 Knowledge retrieval]
        p4[PIECE-4 Execution record]
    end

    p1 --> p6
    p5 --> p1
    p6 --> p10
    p10 --> p7
    p7 --> p8
    p8 --> app
    p9 --> cell
    app --> p2
    app --> p3
    app --> p4
```

## What the map says that the list did not

- **`PIECE-9 Cell provisioning` sits beside the path, not on it.** A cell is brought
  into existence once; an app is deployed many times. Drawing them in sequence would
  put a one-time act on the hot path.
- **The three state pieces are one subgraph and three components.** D4 shows why: they
  have different durability answers, and two of them are open questions.
- **`PIECE-5` feeds `PIECE-1` and nothing else.** The catalogue is an authoring input.
  If it is also read at runtime, this edge is missing and the map is wrong.

## Coverage

Ten pieces declared, ten placed. `check_design_completeness.py` verifies this rather
than trusting the drawing.

## Related

- [`states.md`](states.md) · [`trust.md`](trust.md) · [`sequence.md`](sequence.md) ·
  [`durability.md`](durability.md) — the four decisions this map is DERIVED from
- [`../product/technical-pieces.md`](../product/technical-pieces.md) — the `PIECE-N` this covers
- [`../architecture/paved-road.md`](../architecture/paved-road.md) — the same ground at the AWS target
