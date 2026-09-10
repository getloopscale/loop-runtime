---
type: architecture-view
title: The product alignment review
measured_on: 2026-09-11
render: alignment-review.html
---

# The product alignment review

The reading surface for the four documents of `cycle-brainstorm` before and after signature.
Interactive view: [`alignment-review.html`](alignment-review.html).

It carries what the score cannot: the four boxes with **what signing each one asserts**, the
three disagreements and how they were resolved, and the traceability explorer that lights the
whole chain from an objective to the pieces realising it.

## Why 100% was never approval

`score_product_alignment.py` measures **structure**, because structure is what a script
measures. It prints three things as explicitly not scored: whether the stated problem is the
real one, whether these are the right objectives, whether these are the right metrics. The
rubric is a proxy for whether somebody could disagree with you, and beating the proxy beats
nobody.

## The signature, and why it was withdrawn once

| Date | What happened |
|---|---|
| 2026-09-10 | signed by `paulo` |
| 2026-09-11 | external review tightened 7 acceptance criteria and 3 metrics → **withdrawn** |
| 2026-09-11 | amended text re-read → **re-signed** |

The agent withdrew the marks without asking, because withdrawal is the fail-safe direction: it
can never fabricate an approval, only remove one. Box 2 asserts *these are the right metrics*
and three had changed.

**What the second signature covers that the first did not:** that OBJ-1's 30 minutes is the
machine's number and not a person's, and that REQ-16 is not yet demonstrated by the instrument
it names.

## What the external review demonstrated

It found nothing requiring knowledge this project lacked. It found things nobody had **looked
at** — REQ-8 promised skills and tested prompts inside a paragraph written the same day, and
the `git clone` was one grep away from the grep that ran.

**What was missing was not information. It was a second reader** — which is exactly the
argument `cycle-discover.md` and `cycle-plan.md` make for their review panels, proven in the
one phase of the chain that has none.

## Related

- [`../product/alignment.md`](../product/alignment.md) — the sign-off itself
- [`target-solution.md`](target-solution.md) · [`as-is-to-be.md`](as-is-to-be.md)
