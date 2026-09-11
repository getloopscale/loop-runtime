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
| ~~D1~~ | ~~Is there a suspended state for an idle agent?~~ **ANSWERED 2026-09-11 — `replicas: 0` already is it.** | KEDA `ScaledObject` is already rendered; a `Suspended` phase would be a second source of truth |
| ~~D1~~ | ~~What happens during a platform upgrade? No `Draining` phase~~ **ANSWERED — the pod already drains.** | `preStop 5s + grace 30s` exists. What is short is the budget, not the lifecycle |
| ~~D2~~ | ~~Is there an egress allowlist for the customer's app?~~ **ANSWERED — apply the platform's own shape.** | `loopzero-system-egress.yaml` already excludes RFC1918 + `169.254/16`; it does not cover the tenant |
| ~~D2~~ | ~~Is tenant isolation namespace, node, or cell?~~ **ANSWERED 2026-09-11** — account for BYOC, namespace for projects, and a shared kernel on the managed door, deliberately | Was the one question that could not be retrofitted. Researched against Heroku, Railway, Vercel, Supabase, PlanetScale and Confluent |
| ~~D3~~ | ~~Build in the customer's cell or on platform infrastructure?~~ **ANSWERED — the customer's.** | Decided by C1 of the extraction RFC before this drawing asked. See below |
| ~~D4~~ | ~~Does agent state survive a pod restart?~~ **ANSWERED 2026-09-11 — yes.** | State lives in `loopzero-memory`'s Postgres, not in the pod. See below |
| ~~D4~~ | ~~Is an in-flight turn checkpointed before it completes?~~ **ANSWERED — durable state is the developer's, through the data plane.** | `loopzero-memory` / `loopzero-lens` / `loopzero-rag` already implement it. The framework builds nothing |

- [x] The six phases in D1 are the real lifecycle, and `Cleaned` is the only terminal one  <!-- signed-by: human/paulo -->
- [x] The trust boundary in D2 is the account boundary, and the open egress question is real
- [x] The failure paths in D3 are the ones a developer actually hits
- [x] The durability table in D4 is honest — agent state lives in `loopzero-memory`'s
      Postgres and outlives the pod, and the blast radius of an eviction is the
      in-flight turn
- [x] Every PIECE-N has a place in D5, and no component in D5 lacks a piece
- [x] Isolation is settled: the AWS account separates BYOC customers, the namespace
      separates one customer's projects, and two developers on the managed door share a
      kernel — deliberately, with the exit condition written
- [x] The four remaining questions are answered by one invariant — **no agent state lives
      in the pod** ([decision](../decisions/agent-state-never-lives-in-the-pod.md)) — and what is left is work, not decisions

**The last box was split on 2026-09-11, and the count it carried was wrong.** It read *"the
**seven** open questions above"* after two had been answered, and it held the backlog behind all of
them.

Only one of the five has to come first. Choosing namespace, node or cell decides the shape of
`PIECE-9` and of D2, and changing it later is not a change — it is a different platform. The other
four each have evidence, a possible owner and a closing criterion, which is the description of a
backlog item: holding them as pre-conditions inverts `cycle-backlog`, whose whole purpose is to take
the hypothesis and let the measurement decide.

So the reviewer now signs WHICH question blocks and why, rather than that five things come first.

**D2 is answered as of 2026-09-11** —
[`../decisions/shared-kernel-on-the-managed-door.md`](../decisions/shared-kernel-on-the-managed-door.md).
Two thirds came from code that already existed: the AWS account separates BYOC customers
([`two-doors-managed-and-byoc.md`](../decisions/two-doors-managed-and-byoc.md)), and the namespace
`loopzero-{tenant}-{project}-{env}` separates one customer's projects, sponsor-directed 2026-06-07 and
verbatim in `pkg/naming/naming.go`.

The last third was decided against the market rather than against taste. Heroku's standard dynos
*"share an underlying compute instance"*; Railway documents *"container isolation is shared-kernel"*;
Supabase and PlanetScale put BYOC at enterprise with no middle rung. **Vercel draws the real line**:
it runs normal deploys in containers and shipped a SEPARATE product, Firecracker-backed, for
*"untrusted or agent-generated code"*.

LoopZero's managed door is on the container side of that line, measured: the pod runs the image she
built, the PTY backend was deliberately moved out of the default path, and the hooks that can spawn
a shell are deny-by-default with a command fingerprint.

**The exit condition is written** — reopen the day LoopZero offers execution of agent-generated code as
a platform capability, and build it beside the PaaS rather than inside it.

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

## Rendered

[`walkthrough.html`](walkthrough.html) — the five drawings on one page, with what each decides and
what it leaves open. Its mermaid blocks are read from the `.md` files, so it cannot drift from them
silently.

**Five per-drawing `.html` files were deleted on 2026-09-11.** They were rendered before the design
panel and still showed the refuted drawings — `sequence.html` carried *"Degraded — route does not
resolve"*, which the panel disproved against `computePhase`. Nothing referenced them. A rendered
artifact that lags its source is worse than none: it has the authority of being rendered and the
content of being wrong.

## Related

- The five drawings: [`states.md`](states.md) · [`trust.md`](trust.md) ·
  [`sequence.md`](sequence.md) · [`durability.md`](durability.md) · [`system-map.md`](system-map.md)
- [`what-needs-your-review.md`](what-needs-your-review.md) — the same questions, as a worklist
- [`../product/trd.md`](../product/trd.md) — where `REQ-5` makes the promise D4 rests on

**Signed 2026-09-11 by `human/paulo` — a person, not a judge.**

What a signature asserts is that someone read this and is willing to say it holds. It does not assert that a machine checked it: the deterministic score sits above, and the two are separate claims on purpose.

**The signer is also an author of this document.** The reviewer and the author are normally separate, and here they are not. Reason given: solo project; no second reviewer exists

This signature is weaker than one from a reviewer who did not write the document, and the record says so rather than blurring it.
