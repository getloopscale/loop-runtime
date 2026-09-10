---
type: alignment
document: alignment
scope: loopzero
date: 2026-09-10
session: .squad/records/brainstorms/2026-09-10-session.md
---

# Product alignment — LoopZero

The four documents of `cycle-brainstorm` are written:

| Phase | Document | Structure |
|---|---|---|
| 1 | [`product-vision.md`](product-vision.md) | 1 named user · 1 problem · 4 non-goals |
| 2 | [`objectives.md`](objectives.md) | 4 objectives · 4/4 with a metric · 4/4 with a horizon |
| 3 | [`trd.md`](trd.md) | 18 requirements · 18/18 citing an objective · 0 dangling |
| 4 | [`technical-pieces.md`](technical-pieces.md) | 10 pieces · 18/18 requirements realised |

## What the score cannot decide

`score_product_alignment.py` measures **structure**, because structure is what a script can
measure. It reports three things as explicitly not scored, and it is right to:

- whether the stated problem is the real one
- whether these are the right objectives
- whether these are the right metrics

The rubric is a proxy for whether somebody could disagree with you. Beating the proxy beats
nobody. The checklist below covers exactly what the number cannot, plus coverage — so the two
together mean something the score alone does not.

## Reviewer sign-off

- [ ] The problem stated in the vision is the real one
- [ ] These are the right objectives, and the metrics are the right metrics
- [ ] The requirements follow from the objectives
- [ ] The pieces cover every requirement

<!-- signed-by: -->

**Only a person signs this one.** `alignment-threshold.md § Amended 2026-09-01` lets
`alignment_judge.py` sign an *item's* brief, because the judge reads the item's evidence —
something that exists independently of the brief and can contradict it. At product level
there is no such evidence: the vision is what everything else is measured against, so a judge
scoring it would grade the document against itself. The scorer enforces this — a
`signed-by: judge/…` returns `AWAITING_REVIEW`.

The agent that wrote these documents generated this section and **may never tick a box**.
Ticking a reviewer's box fabricates a judgement nobody made.

## The three known disagreements — one reopened 2026-09-11

All three were resolved before this document was first offered for signature. **D2 was then
reopened by an external review**, which found the resolution rested on a weaker instrument
than the requirement names. That review also tightened seven acceptance criteria and three
objective metrics — so the 2026-09-10 signature no longer covers the text it signed, and has
been withdrawn.

**D2 — REQ-16 forbids telemetry by default. → NOT resolved. Corrected 2026-09-11.**
This section claimed it was *already true*, measured. That claim was stronger than the
instrument behind it: REQ-16's own acceptance is a **network capture during install and first
run**, and what was run was a source grep for `fetch`/`axios`. Declaring a requirement
satisfied by a weaker instrument than the one it names is precisely the failure this cascade
exists to prevent.

An external review caught it, and probing the paths a grep cannot see found a real one:
**`create-loopzerokit` shells out to `git clone`** via `execFileSync` — a network call invisible
to that grep. It is opt-in and goes to GitHub rather than to us, so REQ-16 may still hold, but
that is a different claim and only the capture answers it.

**Status: static inspection favourable, acceptance pending.** The inspection did also confirm
no lifecycle scripts in any published package, which is the other common hiding place.

**D1 — OBJ-3 cannot close on its horizon. → Resolved: the horizon stays, and the prediction
is written first.**
Measured: release `v0.76.6` and **12 OCI charts** are published; the composition that stands
them up is not. The attempt is therefore not vacuous, and we predict it stops at **REQ-10**.
Writing that prediction before the measurement is what makes the attempt worth making — if it
stops elsewhere we learned something we did not know, and a result recorded after the fact
can only agree with itself.

**D3 — PIECE-6 has no owner. → Resolved: it is a finding, and a finding does not block this
signature.**
Measured: `create-loopzerokit/README.md` has **0** getting-started sections; the package that IS
the entry point does not document the path. The fourth box asks whether the pieces cover every
requirement — PIECE-6 covers REQ-1 and REQ-4, so coverage holds. **Ownership is a different
question and no box asks it.** Blocking on it would make the cascade unable to close while any
gap exists, which is to say unable to report one. `/backlog-init` acts on this.


## Read this before re-signing — the scope change has no precedent

**Self-host was dropped on 2026-09-11 and BYOC became the whole offer. Research the same day found
no vendor doing that.**

In every vendor verified, BYOC is a **third** option beside a fully managed mode — Confluent,
MongoDB, Temporal, Porter. **None was found whose only enterprise mode is BYOC with a vendor-hosted
control plane.** And Databricks, the largest vendor of customer-account compute, is walking back from
it: serverless GA January 2026, classic managed-VPC workspace creation slated for deprecation.

**Two vendors argue against our specific mechanism**, not against BYOC: WarpStream and Nuon both
reject the assumed cross-account role and propose an egress-only agent inside the customer account.
That is the finding with the most leverage, because nothing is built yet.

**None of this refutes the decision.** It says the configuration is unprecedented — a smaller claim,
and the accurate one. The question it opens is not *bring self-host back*; it is **what the second
door is**, since every verified vendor has one.

**Signing box 2 now means agreeing to a scope no verified vendor runs.** That may be right. It should
be deliberate. Full record, with evidence graded read / search / unverified, in [`../decisions/byoc-as-the-only-mode-has-no-precedent.md`](../decisions/byoc-as-the-only-mode-has-no-precedent.md).

## The scope change to weigh before re-signing

**Self-host was dropped on 2026-09-11. BYOC is the whole of it.**

**The vision did not break; it tightened.** *"The same platform runs on your own infrastructure when
you need it to"* still holds — a BYOC cell lives in the customer's account. And the non-goal *"not
managed Kubernetes: the customer never operates a cluster"* gets **stronger**, because self-host was
the one place where they would have.

**Four of five requirements survive; one dissolves.** REQ-11, 12, 13 and 14 all hold — REQ-14 turns
out to be the D10 eight-item bootstrap contract under another name. **REQ-10 is withdrawn**: under
BYOC we perform the install with CAPI's authority at cell birth, so "runs on published artifacts
alone" describes an act nobody performs. The id is struck in place, never deleted — other documents
cite it.

**It disarms the standing blocker.** `getloopscale/loopzero#358` reported that a customer cannot pull the
published charts. Its premise was that a customer must fetch; under BYOC nobody outside does.

**And it costs one thing, stated here rather than discovered later.** D5 promises `AssumeRole` +
`ExternalId` with no long-lived key, and D11 records that this has **no DigitalOcean equivalent**.
Under self-host the customer's provider was their business. Under BYOC it is ours: **AWS is the only
provider where the security promise holds as written.**

## A declared limitation the reviewer should know before signing

**OBJ-1 does not measure comprehension.** Its number comes from a script running the
quickstart's commands, and a script is written by someone who already knows the answer — the
current name of a flag, that the deploy prints nothing for four minutes, which field *name
already taken* refers to. A first-time reader has none of that. The same quickstart can
measure 8 minutes scripted and cost a person three hours, with the metric green both times.

One cold read-through by a member of this team was offered and **declined on 2026-09-11**. The
metric stays mechanical and the gap is not scheduled. **30 minutes must therefore never be
quoted as a person's experience** — it is the machine's.

An external tester would close it directly and is refused on standing policy: a metric whose
instrument belongs to somebody outside this team is blocked rather than slow. The trade is
recorded rather than hidden — a number we can always take, over a number that is fuller and
sometimes unobtainable.

## Amendment history

| Date | What changed | Effect on the signature |
|---|---|---|
| 2026-09-10 | Four documents written; three disagreements resolved | signed by `paulo` |
| 2026-09-11 | External review: 7 acceptance criteria tightened (REQ-1, 3, 6, 8, 9, 11, 12), 3 objective metrics disambiguated (OBJ-1, 3, 4), D2 reopened, OBJ-1's comprehension gap declared | **signature withdrawn** — re-signature required |
| 2026-09-11 | Amended text re-read | **re-signed by `paulo`** |
| 2026-09-11 | `type:` added to frontmatter — OKF v0.2's one hard requirement | **signature stands** — a filing key asserts nothing any box claims |
| 2026-09-11 | **Self-host dropped; BYOC is the whole of it.** OBJ-3 rewritten, REQ-10 withdrawn, PIECE-9 renamed, vision amended | **signature withdrawn** — box 2 asserts *these are the right objectives* and an objective changed |
| 2026-09-11 | Market research recorded: **BYOC-only has no precedent found**, and two vendors argue against the cross-account-role mechanism | still withdrawn — the finding is evidence for box 2, not a change to it |

**Why the withdrawal.** The four boxes assert things about a specific text. Box 2 says *these
are the right metrics* and three metrics changed; box 3 says *the requirements follow from the
objectives* and seven acceptance criteria changed. Leaving the signature attached would make
it approve wording nobody read.

The agent withdrew the marks rather than asking, because withdrawal is the fail-safe
direction: it can never fabricate an approval, only remove one. Re-signing is the reviewer's,
as it was the first time.
