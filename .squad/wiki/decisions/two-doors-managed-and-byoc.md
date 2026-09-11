---
type: decision
title: Two doors — managed multi-tenant for the solo developer, BYOC for privacy
date: 2026-09-11
status: stable
---

# Two doors: managed for the solo developer, BYOC for privacy

**Decided 2026-09-11 by the sponsor**, in those words: *a company that offers the PaaS to solo
developers and BYOC to companies that want privacy.*

## What it settles

| | the solo developer | the company that wants privacy |
|---|---|---|
| where the cell runs | **ours**, shared | **theirs**, one AWS account |
| isolation between customers | namespace + PodSecurity `restricted` | the AWS account boundary |
| isolation within one customer | namespace per `(tenant, project, env)` | the same |
| what she brings | nothing — `npm create loopzerokit` | an AWS account satisfying the D10 bootstrap contract |
| which objectives describe it | `OBJ-1`, `OBJ-2`, `OBJ-4` | `OBJ-3` |
| which piece provisions it | placement into an existing cell | `PIECE-9`, via CAPA |

## Why this was found rather than decided

**Both doors were already built.** Neither is new work; what was missing was the sentence saying
there are two.

- `MAX_TENANTS_PER_CELL` defaults to **5000** (`api/internal/config/config.go:511`). A cell sized
  for five thousand tenants is not a cell in one customer's account.
- `RoundRobinEngine` assigns a new tenant to the least-loaded cell and is sticky thereafter
  (ADR-0008, *multi-cell only, no single-cell mode*).
- `PIECE-9` provisions a cell **into an account that is not ours**, which only the second door needs.

Three numbers that cannot describe one product: a cell for 5000 tenants, a cell per customer, and
`OBJ-4`'s *"adoptable with **no account**"*.

## What it resolves that was open

**The market research asked for exactly this and did not recognise it.**
[`byoc-as-the-only-mode-has-no-precedent.md`](byoc-as-the-only-mode-has-no-precedent.md) found that
in every vendor verified — Confluent, MongoDB, Temporal, Porter — BYOC is an option **beside** a
fully managed mode, never the only one, and closed with: *"the question it opens is not bring
self-host back; it is what the second door is."*

The second door was never missing. It is the managed PaaS, and it had not been written down. So the
shape is now the ordinary one those vendors run, and **the "no precedent found" finding no longer
applies to this product** — it applied to a description of it that omitted half.

What the research still says, unchanged: two vendors argue against the **assumed cross-account
role** (WarpStream, Nuon), and that is about the BYOC mechanism, not about having two doors.

## The isolation question this leaves open — and it is the one that cannot be retrofitted

Between BYOC customers, the boundary is an AWS account: the strongest available, and decided.
Between projects of one customer, it is the namespace: decided 2026-06-07, sponsor-directed,
recorded verbatim in `pkg/naming/naming.go`.

**Between two solo developers on the same cell, it is a namespace and a shared kernel.**
`runtimeClass` appears in **zero** files; there is no gVisor, no Kata, no Firecracker. `nodeSelector`
appears 21 times and every one is a platform deployment — no tenant workload is pinned anywhere.

That is standard Kubernetes soft multi-tenancy, and it is the right default for most platforms. This
one runs **agents**: code an LLM generated, calling tools, on behalf of someone we have never met.
Whether that surface is the same surface as a web app is a product decision, not an implementation
detail, and choosing kernel isolation later means re-platforming the managed door.

**That is the open D2 question, restated.** The original — *namespace, node or cell?* — offered three
exclusive answers to what is a stack, and included a level (node) never used for tenant isolation.

## What this does NOT change

- **No objective changes.** `OBJ-1`…`OBJ-4` all stand; what changes is knowing which door each
  describes.
- **No requirement is withdrawn.** `REQ-10` stays withdrawn on its own reasoning — under BYOC nobody
  outside fetches an artifact — and that reasoning is untouched by the managed door existing.
- **Self-host stays dropped.** A cell WE create in the customer's account is not a cluster THEY
  created, and the non-goal *"the customer never operates a cluster"* holds on both doors.

## Related

- [`byoc-as-the-only-mode-has-no-precedent.md`](byoc-as-the-only-mode-has-no-precedent.md) — the
  research that asked for the second door
- [`../design/trust.md`](../design/trust.md) — D2, which draws one door and now must draw two
- [`../product/objectives.md`](../product/objectives.md) — `OBJ-3` is the BYOC door
- [`../product/technical-pieces.md`](../product/technical-pieces.md) — `PIECE-9` provisions it
