---
type: objectives
document: objectives
scope: loopzero
owner: iris-product-designer
agreed_on: 2026-09-10
review_on: 2026-09-14
derives_from: .squad/wiki/product/product-vision.md
session: .squad/records/brainstorms/2026-09-10-session.md
---

# Objectives — LoopZero

Every objective below answers one question: **what must be true for the problem in the
vision to be solved?** The problem is that the developer *becomes an infrastructure
operator before she gets to be the agent's author*, and the cost that matters is not the
weeks — it is what she gives up finding out.

## The rule every metric here obeys

**We hold the stopwatch, always.** No metric depends on a person outside this team: not
external testers, not a customer's engineer, not "when the first client arrives". A metric
whose instrument belongs to somebody else is not slow, it is **blocked** — by someone who
does not answer to us — and an objective that cannot be measured cannot be judged, so the
work beneath it is never known to have served.

This is the same family as the retained classes in `rules/autonomy-envelope.md` (`access`,
`elapsed`, `liveness`): authority does not conjure what is missing. When the honest
instrument turned out to be a person, we changed the **instrument**, not the objective —
a clean container instead of an external tester, ourselves as the first customer instead
of a real one.

## The horizon means the number was taken

On the date, the question is **"was the number taken?"** — not "was the target hit?" A
baseline that says *17 minutes* or *the install stops at step 4* is the objective doing its
job. An objective whose number was never taken is one nobody is pursuing, and that is what
the horizon exists to make visible.

---

## OBJ-1 — She reaches a live agent without taking an infrastructure decision

metric: **≤ 30 min** median wall-clock from `npm create loopzerokit` to the agent answering on a public URL with TLS, in a clean container, executing only the steps of the published quickstart. Median over **≥5** scripted runs, reported **only alongside the count of attempts, failures and timeouts** — a median over runs that mostly failed is not a measurement, and three runs describe reliability badly.
horizon: 2026-09-13
why: it is the first thing she meets, and it is where she gives up. Everything else in the
vision is downstream of her getting this far.

**This metric does NOT measure comprehension, and that is a declared limitation rather than
an oversight** (decided 2026-09-11, after an external review raised it).

The number comes from a script executing the quickstart's commands, and **a script is written
by someone who already knows the answer**. It knows the current name of a flag, it knows the
deploy prints nothing for four minutes before returning, it knows which field the message
*name already taken* refers to. A first-time reader of the published text has none of that
knowledge. So the same quickstart can measure 8 minutes scripted and cost a person three
hours, with this metric green in both cases.

**What is measured:** whether the mechanism completes, and how long it takes when nothing is
misunderstood. **What is not:** whether the published text alone carries a reader to the end.

The alternative was one cold read-through by a member of this team. It was considered and
declined: the metric stays mechanical, and closing this gap is not scheduled. The honest
consequence is that **30 minutes here must never be quoted as a person's experience** — it is
the machine's. Should the gap ever be worth closing, it is a `B-NNN`, not a change to this
objective.

An external tester would close it directly and is refused on standing policy — a metric whose
instrument belongs to somebody outside this team is blocked rather than slow. That trade is
recorded, not hidden: we chose a number we can always take over a number that is fuller and
sometimes unobtainable.

## OBJ-2 — The four capabilities work without her building them

metric: **0** of the four capabilities require her to write persistence, schema or instrumentation code — memory that survives a restart, RAG over her documents, a trace of what the agent did, versioned prompts. Verified by following the quickstart and reading the project it generates.
horizon: 2026-09-13
why: this is what makes the second feature cost a week. She wrote the memory layer by hand,
so every change goes through her — and that is the half of the cost the vision calls *what
she gives up finding out*.

## OBJ-3 — Self-host is an install, not a project

metric: **≤ 1 day** for us to install on a cloud account that is not our platform, from published artifacts, with zero access to our infrastructure — the clock starting once the prerequisites REQ-14 declares (Postgres, object storage, identity provider) are present, because those are what the customer brings and timing their provisioning would measure their cloud rather than our install — and **0** bytes of source bundle or build artifact egressing outside the declared perimeter during a deploy, by network capture.
horizon: 2026-09-13
why: the vision already promises it, and the extraction RFC closed the commercial scope
(3 clusters, 44 releases) with network proof as an acceptance criterion — the promise the
customer buys has to be demonstrated, not documented.

**Known at the time of writing:** this objective cannot CLOSE on its horizon — it installs
artifacts the extraction has not produced yet, and only slice 1 is approved. What the
horizon requires is that the number be taken: attempt the install with what is published
today and record where it stops.

**The prediction, written before the measurement — 2026-09-10.** Measured today:
release `v0.76.6` (2026-08-18) exists and **12 OCI charts are published**; the composition
that stands them up is **not** published. So the attempt is not vacuous — there are
artifacts — but it should stop early, and we predict it stops at **REQ-10** (*the install
runs on published artifacts alone*): we have the charts and not the published procedure that
raises them.

Writing the prediction first is what makes the attempt worth making. If it stops at REQ-10 we
confirm what we already believed and the baseline was cheap. **If it stops anywhere else, we
learned something we did not know** — and that is the only outcome that pays for the
attempt. A result recorded after the fact can only ever agree with itself.

**The measurement was taken early, on 2026-09-11 — and it refuted the prediction's cause.**

```
helm pull oci://ghcr.io/getloopscale/charts/loopzero-api --version 0.2.0   401 unauthorized
anonymous ghcr token                                                  denied
"entitlement" in real code                                            1 hit: --allow-insecure-entitlement (BuildKit flag)
"entitlement" in the extraction RFC                                   1 hit: "reuses ... the entitlement token"
published install procedure                                           none
```

**REQ-10 is the requirement that fails, as predicted — for a cause that was not.** The
prediction said it would stop for want of the published PROCEDURE that raises the charts. It
stops earlier: **a customer cannot fetch the charts at all.** The entitlement the RFC's O3
design assumes — *"reuses the registry, the entitlement token and the keyless cosign the
pipeline already runs on"* — exists in that sentence and nowhere else.

The difference matters. A missing procedure is a documentation task. **A missing entitlement is
a product capability nobody has built**, and the bundle design is written as though it were
already there.

**REQ-12 falls with it.** A customer could be granted access by hand — added to the
organisation — and that would work; it is not a written procedure, so *two installs of the same
version produce the same system* has no procedure to be reproducible from.

**What could NOT be measured, and why:** the *≤ 1 day* half needs a cloud account this session
cannot provision. That is `access` in `rules/decision-delegation.txt`, a retained class where
authority does not conjure what is missing. The baseline covers REQ-10 and REQ-12; the duration
is still unmeasured, and saying so is the whole point.

**This is the outcome that paid for writing the prediction down.** Confirming REQ-10 cheaply was
the expected case. Learning that the cause is a capability nobody built — while the architecture
RFC describes it in the present tense — is what a result recorded after the fact could never
have surfaced.


## OBJ-4 — LoopZeroKit is adoptable with no account

metric: **0** credentials **of ours** and **0** requests to a domain of ours before the agent runs, installing from the public registry in a clean container. Verified by network capture. The model provider key is the one credential permitted, per REQ-2 and the vision's *not a model provider* non-goal — the earlier wording said "0 credentials" flat, which contradicted both.
horizon: 2026-09-13
why: without this the open-source half is a lead magnet rather than half the product, and
the one-file seam (`.loopzero/services.json`, ADR-0015) exists precisely to make it possible.

**Measured 2026-09-10, and it changed this objective:** publishing is already done —
`loopzerokit@0.64.0`, `create-loopzerokit@1.25.3`, `@loopzerokit/http@2.0.0`, `@loopzerokit/agents@12.1.0`,
`@loopzero/cli@0.4.0` are all on npm. "Publish LoopZeroKit" would be an objective that cannot
fail, which is not an objective. What is NOT true yet is that the published artifacts come
up clean from a machine that has never seen us, and that is what the metric measures.

---

## Sequence

`OBJ-4 → OBJ-2 → OBJ-1 → OBJ-3`, and the order survives any change to the dates: OBJ-4 is
the cheapest and fails fastest; OBJ-2 is the substance; OBJ-1 is a number that only exists
once both hold; OBJ-3 depends on gates that are not closed.

Measured: a **single author** across `loopzero`, `loopzero-cloud`, `loopzero-data-cells` and
`loopzero-cluster-management` over the last 6 months. That makes sequence matter more than
parallelism — and it is why the dates are the weakest thing in this document. The metrics
are anchored to an instrument; the dates are anchored to nothing but order.

## What was considered and dropped

**"The second feature costs a day, not a week."** It is the deeper cost the vision names,
and it was dropped because we cannot observe it today without instrumentation that does not
exist — and a number nobody can measure is what gate G-B2 refuses. OBJ-2 attacks its cause.
If we later want to measure the effect, that instrumentation is a `B-NNN`, not an objective.

## Ids

Monotonic, never reused. Backlog items cite them through `traces_to`, so renumbering
silently repoints every citation. A dropped objective keeps its number and is marked
withdrawn in the session record.
