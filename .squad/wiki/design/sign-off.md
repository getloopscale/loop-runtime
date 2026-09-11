---
type: design-signoff
title: The design sign-off, and the questions it leaves open
date: 2026-09-10
status: draft
---

# Design sign-off — LoopZero

## Sign-off

The drawings were derived from the code and the product documents on 2026-09-10. Two
kinds of claim are in them and they need different scrutiny.

**Measured** — the six `Application` phases and how often each is written, the two
storage backends, the eight CRDs, the four operators, the chart tests that exercise
NetworkPolicy and securityContext, and the wording of every `PIECE-N`. Re-runnable.

**Open** — listed under each drawing, not drawn as if settled. A placeholder in a
diagram reads as a decision that was made, which is worse than an absent one.

### The open questions, gathered

| Drawing | Question | Why it decides the product |
|---|---|---|
| D1 | Is there a suspended state for an idle agent? | Decides whether an idle long-running agent costs money |
| D1 | What happens to a running app during a platform upgrade? No `Draining` phase exists | Decides whether "long-running" survives our own deploys |
| D2 | Is there an egress allowlist for the customer's app? | An agent that can reach anything is a different product |
| D2 | Is tenant isolation namespace, node, or cell? | Cannot be retrofitted |
| ~~D3~~ | ~~Build in the customer's cell or on platform infrastructure?~~ **ANSWERED — the customer's.** | Decided by C1 of the extraction RFC before this drawing asked. See below |
| ~~D4~~ | ~~Does agent state survive a pod restart?~~ **ANSWERED 2026-09-11 — yes.** | State lives in `loopzero-memory`'s Postgres, not in the pod. See below |
| D4 | Is there a checkpoint interval? | Decides the blast radius of one pod eviction |

- [ ] The six phases in D1 are the real lifecycle, and `Cleaned` is the only terminal one
- [ ] The trust boundary in D2 is the account boundary, and the open egress question is real
- [ ] The failure paths in D3 are the ones a developer actually hits
- [ ] The durability table in D4 is honest — agent state lives in `loopzero-memory`'s
      Postgres and outlives the pod, and the blast radius of an eviction is the
      in-flight turn
- [ ] Every PIECE-N has a place in D5, and no component in D5 lacks a piece
- [ ] The seven open questions above are the right ones to answer before the backlog opens

Sign with `/sign`:

```bash
python3 .claude/skills/sign/scripts/sign_document.py .squad/wiki/design/sign-off.md --as paulo
```

---

## Two questions answered on 2026-09-11, and why each was answerable without new work

### D3 — the build runs on the customer's infrastructure

**It was never open; it was decided and this drawing did not know.** C1 of the extraction RFC
states it in one line: *"Build plane — **Included.** The customer builds on their own
infrastructure"*, and `REQ-11` makes it a network-capture criterion.

A question left open in a drawing while a signed RFC answers it invites somebody to decide it
again, differently. That is the only reason it is struck here rather than left to the reviewer.

### The fourth box was rewritten twice on 2026-09-11

It first read *"including that agent state is **unresolved**"*. Answering D4 made that false — the
table says `survives — Postgres` on all three rows.

**The replacement then over-corrected**, asking the reviewer to accept the `loopzero` ↔ `loopzero-memory`
edge as a standing risk. It is not one: `loopzero-memory` is part of this ecosystem and is treated as
ready and working, so a boundary between two of our own repositories is the ordinary topology here.
Dramatising it would have hung a permanent caveat on a promise that holds.

What the box asserts now is what the table shows: where the state lives, and what one eviction
costs.

### D4 — agent state survives a pod restart

**Yes, and the drawing's own instinct was right: nothing in `loopzero` shows it.**

Measured 2026-09-11 in `loopzero`: the renderer emits `Deployment` or `Rollout`, **never
`StatefulSet`**, and no `PersistentVolumeClaim` appears anywhere on the user's path. Read alone,
that says the state does not survive.

It survives because **it was never in the pod**. It lives in `loopzero-memory`'s Postgres —
34 migrations, and at head **17 tables**, none dropped:

> `admin_config, consolidation_history, entities, entity_edges, history, memories, memory_entities, recall_cache, recall_traces, reflection_counters, routing_audit, routing_policies, scope_usage_counters, loopzero_api_keys, users, workspace_users, workspaces`

**A number in this bundle was wrong and is corrected here.**
`what-needs-your-review.md` recorded *"34 migrations, 9 tables"*. The migrations match. The
tables are **17**, counted from `CREATE TABLE` across `packages/core/src/.../migrations` with
`DROP TABLE` subtracted — a `CREATE` count is not a live schema until the drops are taken out,
and here there are none. (An earlier pass of my own counted 102 migrations: the other 68 are
copies under `dist/`, which is a build artefact.)

### What answering D4 located

**The state was never in the pod, and looking for it in `loopzero` was looking in the wrong
repository.** The renderer emits `Deployment` or `Rollout`, never `StatefulSet`, and puts no
`PersistentVolumeClaim` on the user's path. Read alone that says the state does not survive.

It survives because it lives in `loopzero-memory`'s Postgres — part of this ecosystem, treated as ready
and working. So D4's answer is a plain yes, and what the drawing adds is WHERE. A drawing that
sends the next reader to the right repository has done its job.

**The blast radius of one pod eviction is therefore the in-flight turn, not the agent's memory** —
which is what the remaining D4 question is about.

## Related

- The five drawings: [`states.md`](states.md) · [`trust.md`](trust.md) ·
  [`sequence.md`](sequence.md) · [`durability.md`](durability.md) · [`system-map.md`](system-map.md)
- [`what-needs-your-review.md`](what-needs-your-review.md) — the same questions, as a worklist
- [`../product/trd.md`](../product/trd.md) — where `REQ-5` makes the promise D4 rests on
