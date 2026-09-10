---
type: product-vision
document: product-vision
scope: loopzero
owner: iris-product-designer
agreed_on: 2026-09-10
review_on: 2027-03-10
session: .squad/records/brainstorms/2026-09-10-session.md
---

# Product vision — LoopZero

## Who it is for

**The developer who already has an agent working on a laptop and cannot turn it into a product.**

Not "developers", not "teams building with AI" — those are categories, and a category settles no
trade-off. The situation is what settles trade-offs: something works locally, and the whole distance
between that and something a user can reach is infrastructure she did not set out to build.

She is one person, or two. She has the domain knowledge that makes the agent worth building and no
particular interest in operating a database. She is the reason the data-plane repositories exist —
`loopzero-memory`, `loopzero-rag`, `loopzero-lens`, `loopzero-skills`, `loopzero-promptly`, `loopzero-trust` are of no use
to anyone who is not building an agent.

**Not the first buyer:** the platform team adopting LoopZero across an organisation. Real, and a later
one — but designing for them first produces an admin product, and she never arrives.

## The problem

**She becomes an infrastructure operator before she gets to be the agent's author.**

What she does today: stands up Postgres and finds she needs `pgvector`; picks a separate vector store
and now has two sources of truth; writes the memory layer by hand because every framework has its own
and none persists the way she needs; instruments the agent with `print` because tracing an LLM call
does not come for free; and when it finally works in the notebook, discovers that standing it up
means Docker, a registry, TLS, a domain.

The cost has two halves, and the second is the one that matters:

- **Weeks before the second feature.** Real — and this is the cost she sees and accepts.
- **What she gives up finding out.** Every week spent on infrastructure is a week without contact
  with the user's problem. Agents are a domain where the right answer is rarely the first one.
  Someone who spends the beginning on plumbing reaches the learning with less budget left, or does
  not reach it.

This is stated as an observation, not as the absence of a product. The problem is not "there is no
platform for agents". The problem is that the scarcest resource in a project — the start, while
changing your mind is still cheap — is spent on work that brings her no closer to knowing whether
the agent solves anything.

## What it is

**LoopZero is the complete solution for building and running agentic applications.** You write with
**LoopZeroKit**, an open-source full-stack framework that ships what an agent needs — memory that
survives a restart, RAG over your documents, a trace of what the agent did, versioned prompts and
skills. One command puts it live with a URL and TLS. And the same platform runs on your own
infrastructure when you need it to.

Three things that paragraph is careful about:

**"Building and running"**, in that order. LoopZeroKit is the half she meets first; she starts by
writing, not by publishing. A vision that leads with deployment describes the second half of her
day.

**The capabilities are named, not summarised.** "Everything an agent needs" is the promise; memory,
RAG, trace and prompts are the reason to believe it. Each is a repository that exists.

**"When you need it to"**, not "in the Enterprise tier". Self-host answers a need — sovereignty,
compliance, a customer whose code cannot leave the building. Framed as a price tier the sentence
becomes about us; framed as a need it stays about her.

**Kubernetes is deliberately absent from that paragraph.** LoopZeroKit does not know what Kubernetes is,
by design (ADR-0015), and Rule 3 of this repository forbids the term in user-facing text. If the
paragraph needed it in order to be understood, the product would not yet be described by what it
delivers.

### The two halves, and the seam between them

| | **LoopZeroKit** | **LoopZeroCloud** |
|---|---|---|
| What | full-stack TypeScript framework | Go platform |
| Licence | open source | proprietary |
| Distribution | npm | hosted service, or self-hosted |
| Knows Kubernetes? | **no, by design** | yes |

They meet at one file — `.loopzero/services.json`. Neither imports the other's code. That seam is what
lets someone adopt LoopZeroKit without adopting us, which is the honest form of the open-source half.

## What it is NOT

Each of these earns its place by what it makes us refuse. A non-goal that refuses nothing concrete
is decoration.

- **Not a framework for agent reasoning.** LoopZeroKit does not decide how the agent thinks — no
  reasoning loop of its own, no tool selection, no chain orchestration. That belongs to LangChain,
  LlamaIndex, CrewAI or the provider's SDK, and LoopZeroKit talks to them.
  *Refuses:* the first request to add a ReAct loop to LoopZeroKit. This is the uncomfortable one — it is
  the most natural thing to build once memory and RAG are in hand, and it is exactly where a
  defensible advantage gets traded for a fight with projects that have a thousand contributors.

- **Not a model provider.** We do not host LLMs, do not train, do not serve inference. You bring
  your key.
  *Refuses:* "can you run a Llama for us?" Running GPUs is a different business with a different
  cost structure.

- **Not managed Kubernetes.** The customer never operates a cluster, never sees a manifest, never
  learns an orchestration concept in order to use the product.
  *Refuses:* the "let advanced users edit the YAML" escape hatch. The moment it exists it becomes
  the documentation, and the product starts being judged on how well the abstraction leaks.

- **Not a general-purpose deploy platform.** We do not compete with Vercel, Railway or Render for
  the app that is not agentic. No agent, no advantage — and saying so out loud is what keeps the
  roadmap honest.
  *Refuses:* the next feature asked for by a customer whose app is a CRUD. The most expensive
  refusal commercially, and the most protective: every week spent reaching parity with Vercel is a
  week not spent on what only we do.

## How this document is used

Every objective in `objectives.md` derives from **The problem** above. Every requirement in `trd.md`
cites an objective. Every backlog item traces to one. A piece of work that traces to nothing is not
forbidden — it is *visible*, which is the whole point: on the day this was written, **165 shipped
items traced to no objective**, because there was nothing to trace them to.

This document is living. Revise it and re-score; do not re-run the cascade as a ritual.
