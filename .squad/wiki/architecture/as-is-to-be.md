---
type: architecture-view
title: As-is and to-be
measured_on: 2026-09-11
render: as-is-to-be.html
---

# As-is and to-be

Seven concerns, **aligned row by row**. Two loose figures make the reader hunt for what
changed; here each height is one comparison and the eye travels horizontally.

Interactive view: [`as-is-to-be.html`](as-is-to-be.html).

**The left column was measured on disk. The right column is a plan** — and the mark between
them says what stands behind that promise today, which is the thing an as-is/to-be usually
hides.

```
2 approved  ·  3 planned  ·  2 with no plan at all
```

| Concern | As-is | To-be | Driver | State |
|---|---|---|---|---|
| Where the composition lives | 59 releases in the engine | `loopzero-cluster-management` | slice 1 | approved |
| Where the gates live | 43, all in the engine | 25 stay · 15 travel · 3 die | slices 2, 5 | planned |
| **How the customer obtains artifacts** | **401, no entitlement, no procedure** | entitlement · ORAS bundle · written procedure | **#358** | **no plan** |
| What the customer raises | nothing — self-host does not exist | 3 clusters · 44 releases | C1 | approved |
| Who builds their code | our build plane | theirs, perimeter declared | C1 · REQ-11 | planned |
| What proves two installs match | nothing | inventory + versions + config digest + probe | REQ-12, 14 | planned |
| **Where the developer enters** | **no owner, lens unpublished** | one path, one owner | REQ-1, 4, 16 | **no plan** |

## The two rows with no plan are this drawing's finding

**How the customer obtains artifacts.** None of the nine slices covers it. The extraction moves
the composition and does nothing about the fact that a customer cannot download what they are
about to install. A missing procedure is a documentation task; **a missing entitlement is a
product capability nobody has built.**

**Where the developer enters.** PIECE-6 has no owner, `loopzero-lens` has zero published packages,
and `create-loopzerokit` shells out to `git clone`. These are requirements signed on 2026-09-11
with no work assigned to anyone.

## The cleanest row is C1

59 → 44 releases, 5 → 3 clusters, and the whole `secrets` composition disappears when the
customer brings their own store. It is the only row where the to-be is **smaller** than the
as-is, and it is what takes `clustermgmtd` off the critical path for self-host.

## Related

- [`target-solution.md`](target-solution.md) · [`engine-and-delivery.md`](engine-and-delivery.md)
- [`../product/trd.md`](../product/trd.md) — the requirements the to-be column cites
