---
type: technical-pieces
document: technical-pieces
scope: loopzero
owner: iris-product-designer
proposed_on: 2026-09-10
review_on: 2026-09-14
derives_from: .squad/wiki/product/trd.md
session: .squad/records/brainstorms/2026-09-10-session.md
---

# Technical pieces — LoopZero

A piece is a **responsibility with a boundary** — not a file, not a service, not a
repository. The question each one answers is *what part of the system owns this?*, and the
granularity is the one where responsibilities differ.

**Cut by responsibility, deliberately not by repository.** Listing one piece per repo we
already have would describe the current system, which is what the requirements were supposed
to be free to contradict. Where a piece has no owner today, this document says so — a piece
with no repository is a finding worth having, and `/backlog-init` inventories repositories
from disk afterwards.

---

## PIECE-1 — Authoring surface
realises: REQ-9, REQ-16, REQ-17, REQ-18
responsibility: The programming model she writes against, and the guarantees that hold while
she is only writing — that two capabilities used together need no glue she authors, that
nothing leaves for us including telemetry, that what genuinely needs the platform says so,
and that no dependency edge runs from the framework to the commercial product.

## PIECE-2 — Agent state
realises: REQ-5
responsibility: What the agent knows outliving the process that learned it, without her
declaring a schema, choosing a store or writing a migration.

## PIECE-3 — Knowledge retrieval
realises: REQ-6
responsibility: Turning her documents into something the agent can query, without her
authoring chunking, embedding or index maintenance.

## PIECE-4 — Execution record
realises: REQ-7
responsibility: Answering *what did the agent do* about a run that already happened, without
her having added instrumentation.

## PIECE-5 — Prompt and skill catalogue
realises: REQ-8
responsibility: Holding prompts and skills under version, and resolving a past run to the
exact text it used — not to a label that may have been rewritten since.

## PIECE-6 — Entry path
realises: REQ-1, REQ-4
responsibility: That the route from nothing to running is **one** route with no fork, and
that every failure along it names what she must change.

## PIECE-7 — Publishing
realises: REQ-2, REQ-3
responsibility: Taking what she wrote and making it reachable — no resource she provisions
beyond a model key, and an address that answers over HTTPS with a certificate her browser
accepts.

## PIECE-8 — Distribution
realises: REQ-10, REQ-13, REQ-15
responsibility: What is published and how it is fetched — versioned artifacts that declare which
versions one may come from and whether the path is reversible, fetched into the cells we operate.

**Narrowed on 2026-09-11.** It used to require packages installable with no credential of ours,
because a self-hosting customer had to fetch them. Under BYOC nobody outside fetches, so the
entitlement leaves this piece's critical path — it is worth having, and it is no longer what stands
between a customer and a running cell.

## PIECE-9 — Cell provisioning
realises: REQ-12, REQ-14
responsibility: Bringing a cell into existence inside the customer's AWS account and reaching
workload-ready — reproducibly from a declared ClusterClass, declaring what the customer supplies and
refusing before changing anything when one of those is absent.

**This piece owns the cross-account role, and that is where the research bites.** WarpStream and
Nuon both argue against the assumed-role shape and propose an egress-only agent inside the customer
account instead. Changing that later means changing a live trust relationship in every customer
account, which is why it is named here while nothing is built. Full record in [`../decisions/byoc-as-the-only-mode-has-no-precedent.md`](../decisions/byoc-as-the-only-mode-has-no-precedent.md).

**Note also:** ClickHouse, whose BYOC is GA on AWS, **did not use Cluster API** — CloudFormation plus
Crossplane plus a bespoke upgrader. Our CAPI choice remains defensible and is no longer the obvious
one.

**Renamed from "Installation" on 2026-09-11**, when self-host was dropped. The responsibility did not
move; its actor did. An installer is run BY the customer from a written procedure; a provisioner is
run BY us from a declaration. REQ-12's reproducibility follows the declaration rather than a document
somebody follows, and REQ-14's early refusal is the D10 bootstrap contract rather than a
prerequisites check.

## PIECE-10 — Build path
realises: REQ-11
responsibility: Turning source into a runnable artifact, and owning **where** that happens —
which is what makes the customer's code and build outputs stay inside their infrastructure.

---

## Reverse check — is every requirement realised?

| Requirement | Piece |
|---|---|
| REQ-1 one path, no forks | PIECE-6 |
| REQ-2 nothing to provision | PIECE-7 |
| REQ-3 URL ready and trusted | PIECE-7 |
| REQ-4 a failure names the fix | PIECE-6 |
| REQ-5 memory survives a restart | PIECE-2 |
| REQ-6 RAG without a pipeline | PIECE-3 |
| REQ-7 traced without instrumenting | PIECE-4 |
| REQ-8 prompts versioned to exact text | PIECE-5 |
| REQ-9 the four compose without glue | PIECE-1 |
| REQ-10 ~~published artifacts alone~~ | withdrawn 2026-09-11 |
| REQ-11 their code does not leave | PIECE-10 |
| REQ-12 two installs match | PIECE-9 |
| REQ-13 upgrade declares direction | PIECE-8 |
| REQ-14 declares what they bring | PIECE-9 |
| REQ-15 no credential to install | PIECE-8 |
| REQ-16 no contact, telemetry included | PIECE-1 |
| REQ-17 what needs the platform is stated | PIECE-1 |
| REQ-18 no dependency edge | PIECE-1 |

**17 of 17 live requirements realised** (REQ-10 withdrawn 2026-09-11), and no piece realises none. A requirement nobody realises is agreed
work with nobody to do it; a piece realising nothing is architecture nobody asked for.

## Why these cuts

**PIECE-2 to PIECE-5 are four, and not because they are four repositories.** Their
responsibilities genuinely differ — holding state, retrieving knowledge, recording what
happened, cataloguing versioned text. And REQ-9 only means something if they ARE separate
things that must compose: as a single piece, that requirement would be vacuous.

**PIECE-8 and PIECE-9 separated is the distinction the whole extraction RFC pursues.**
Publishing is not standing up. It is the same line as engine versus composition.

**PIECE-10 exists because of a commercial decision, not a technical one.** The C1 scope put
the build plane inside self-host — *the customer builds on their own infrastructure* — and
that is what makes REQ-11 true. Whoever owns **where** the build runs owns the promise the
customer bought.

## PIECE-6 has no owner today, and that is the finding

The entry path is spread across `create-loopzerokit`, the `loopzero-cli` README and the
documentation — three places, none of them responsible for it being **one** path. REQ-4 makes
it worse: a failure message that names the fix has to be right on both sides of the seam, and
neither side owns the sentence.

Measured 2026-09-10: `create-loopzerokit/README.md` carries **0** getting-started sections and
`loopzero-cli/README.md` carries 1 — the package that IS the entry point does not document the
path.

**This does not block the sign-off, and the reason is precise.** The fourth box asks whether
*the pieces cover every requirement*. PIECE-6 covers REQ-1 and REQ-4, so coverage holds.
**Ownership is a different question, and none of the four boxes asks it.**

Blocking a signature on an unowned piece would make the cascade unable to close while any gap
exists — which is to say, unable to REPORT a gap, the opposite of what this phase is for. And
assigning the owner here would cross the phase's own line: the piece-to-repository mapping
belongs to `/backlog-init`, which inventories disk afterwards. This is the finding it will
act on.

## Status

**Proposed, not agreed.** These pieces are the author's proposal. The reviewer's judgement is
recorded in `alignment.md`, whose fourth box is *the pieces cover every requirement* — that is
where a disagreement with this cut belongs, and it costs nothing to revise before it is
ticked.
