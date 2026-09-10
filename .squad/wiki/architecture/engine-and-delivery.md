---
type: architecture-view
title: The engine and the delivery
measured_on: 2026-09-11
render: engine-and-delivery.html
---

# The engine and the delivery

PIECE-7, PIECE-8, PIECE-9 and PIECE-10 at component level. **145 nodes**, the 59 Helm releases
named one by one, measured 2026-09-11.

Interactive view: [`engine-and-delivery.html`](engine-and-delivery.html).

```
56 ready  ·  41 changed by the extraction  ·  18 do not exist  ·  16 leave under self-host
```

## The compositions, named

| Composition | Releases | Under self-host |
|---|---|---|
| `core` | 31 | 9 leave (C1) |
| `cell` | 15 | 1 leaves (`cloudflared`) |
| `build` | 7 | all stay — the customer builds on their own infrastructure |
| `secrets` | 6 | absent when the customer brings their own store |

`59 − 44 = 15` = 9 excluded by C1 + 6 from `secrets`. Counted with `awk` under the `releases:`
key at release indentation — a `grep -c '^\s*- name:'` also matches `- name:` inside `set:`
blocks and the `repositories:` section, which is how 101 was once reported.

## Where the delivery breaks, measured

```
helm pull oci://ghcr.io/getloopscale/charts/loopzero-api --version 0.2.0   401 unauthorized
anonymous ghcr token                                                  denied
"entitlement" in production code                                      1 hit: --allow-insecure-entitlement (BuildKit)
published install procedure                                           none
```

The edge from distribution to installation is drawn broken because it is. **A customer cannot
fetch the charts**, and the entitlement the extraction RFC's O3 design assumes — *"reuses the
registry, the entitlement token and the keyless cosign the pipeline already runs on"* — exists
in that sentence and nowhere else. Filed as
[getloopscale/loopzero#358](https://github.com/getloopscale/loopzero/issues/358).

## Three things only visible at this depth

**The whole `secrets` composition is struck through.** Six releases, `openbaoEnabled` set in
1 of 8 environment files. The fifth cluster is conditional; under self-host with an external
store it does not exist.

**The three dying gates have a mandatory order.** `chart-pairs`, `helm-drift` and
`helm-overlap-parity` police the duplicated pairs that slice 2 kills. Deleting them earlier
would remove the only checks on a divergence that is still live — so they die **inside** the
slice, not before it.

**Class D is empty.** Nothing today tests chart-versus-composition compatibility, because there
was no seam to test. It is the class the extraction creates the need for, and **it appears in
none of the nine slices.**

## Related

- [`target-solution.md`](target-solution.md) — the same system grouped by piece
- [`as-is-to-be.md`](as-is-to-be.md) — what changes, concern by concern
