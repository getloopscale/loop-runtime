---
type: decision
title: No agent state lives in the pod
date: 2026-09-11
status: stable
---

# No agent state lives in the pod

**Decided 2026-09-11.** One invariant, from which the four open questions of `cycle-design` fall
out. It is what makes the shared-kernel decision coherent: if nothing valuable is in the pod, the pod
is disposable, and a disposable pod needs no microVM snapshot to be resumed.

## The invariant

> **No agent state lives in the pod.**

## What falls out of it

### D1 · an idle agent — `replicas: 0` is already the suspended state

**Do not add a `Suspended` phase to the `Application` lifecycle.** `buildScaledObject`
(`api/internal/manifests/renderer_deployment.go:227`) already renders a KEDA `ScaledObject`, and
`keda-http-addon` is already in the cell composition. The CRD would gain a phase KEDA already
represents, and two sources of truth about *"is it running?"* diverge at the first incident.

The design question assumed a state that does not need to exist.

### D1 · `Draining` on upgrade — the pod already drains; the budget is what is short

**Do not add a `Draining` phase either.** `preStop 5s + grace 30s` is already rendered, with the
reason in the code: *"endpoint removal wins the race"* (`renderer_rollout.go:58,116,467`). That is the
standard shape and it is correct.

**30 seconds is short for an agent turn**, which chains LLM calls and tool calls. The fix is a grace
period measured against how long a turn actually takes — a number nobody has taken — not a new phase
in the CRD.

### D2 · egress — apply to the tenant the shape the platform already uses

`infra/helm/network-policies/templates/loopzero-system-egress.yaml` already allows `0.0.0.0/0` with
`except` on `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, **`169.254.0.0/16`** and `224.0.0.0/4`,
and its own comment says why: *"keeps the egress out of any in-cluster service we did not explicitly
approve above."*

**It applies to `loopzero-system`. It does not apply to the tenant's namespace.** Measured 2026-09-11:
that CIDR appears in exactly one file in this repository. So **the platform protects itself from the
instance metadata endpoint and the customer's app does not.**

Apply the same shape to the tenant namespace. A domain allowlist would be stronger and would make the
product useless — she cannot declare in advance which APIs her agent will call.

### D4 · the turn's checkpoint — see the next section

## Persistence is the developer's, through the data plane

**Decided by the sponsor 2026-09-11:** the state machine is already implemented, in `loopzero-memory`,
`loopzero-lens`, `loopzero-rag` and their siblings. **A developer who wants durable state uses them.**

So the framework builds nothing here: no `drizzle` checkpoint backend, no runs table, no state-machine
API. The parsimony ladder's first rung answers this — the capability exists; what was missing was the
sentence saying it is the answer.

### What this deliberately does NOT promise, and the distinction matters

`REQ-5` promises *memory survives a restart without her modelling it*, and it holds: facts the agent
remembers live in `loopzero-memory`'s Postgres.

**Workflow state is a different thing.** *"Which step of a two-week onboarding are we on"* is not a
memory, and nothing in the data plane models it as one. A developer building the ADK pattern — an
explicit state machine read into the prompt instead of chat history — **builds that machine herself**
on top of `@loopzero/memory`.

That is a legitimate framework position and it is the common one. It is written here because a reader
of `OBJ-2` (*the four capabilities work without her building them*) could reasonably expect otherwise,
and the gap between memory and workflow state is exactly where that expectation breaks.

### A known defect this leaves standing

`CheckpointStorage` declares `'memory' | 'filesystem' | 'drizzle' | 'redis'`. **`drizzle` and `redis`
have no implementation** — measured 2026-09-11, they appear only in `types.ts` and the generated
`.d.ts`. And `CheckpointCapability` warns that only `'filesystem'` resumes across requests, which on
this platform is unreachable: the renderer emits `Deployment` or `Rollout` with no
`PersistentVolumeClaim`.

**So the framework offers four checkpoint backends, two do not exist, and the one it recommends
cannot work on LoopZero's own platform.** The `ttl` default of one hour says what it was built for: crash
recovery, not dormancy measured in days.

This is a framework defect, not a platform one, and it belongs in `loopzerokit`'s backlog. Naming it here
because the decision above makes it survivable rather than urgent — nothing depends on that
checkpoint now.

## What we lose to competitors, and it is three things

Asked directly by the sponsor. Answered from the same evidence, not from comfort.

**1 · Resume latency.** Vercel Sandbox and Microsoft Foundry resume a suspended workload in ~125-150ms
with the process intact — memory, filesystem, session identity. We cold-start a container in seconds
and reconstruct from whatever was persisted. For a user waiting on an agent, that is perceptible.

**2 · Isolation as a sales line.** E2B, Modal and Daytona put "microVM" or "gVisor" in the first line
of the page. When a technical buyer asks how we isolate, "namespace" is a weaker answer — even though
it is the same answer Heroku, Railway, Supabase and PlanetScale give.

**3 · Agent-generated code execution, and this is the serious one.** We cannot offer it on the managed
door. E2B, Modal, Daytona, Vercel Sandbox and Cloudflare Sandbox all can. If the agent market
converges on *"the agent writes and runs code"*, we are outside it until a separate product exists —
which is precisely the exit condition already written in
[`shared-kernel-on-the-managed-door.md`](shared-kernel-on-the-managed-door.md).

**4 · Durability out of the box.** ADK gives it in one line — `session_service_uri` — and every
`ToolContext.state` write is durable. We say *"use the data plane"*, which is more capable and less
immediate.

## What we win, and one of them is structural

**1 · No session ceiling.** Vercel Sandbox caps a session at 45 minutes; Cloudflare Sandbox at 30. An
agent that pauses for three days does not fit in either. **Our model has no ceiling by construction**,
because the state was never in the process — and the vision's *long-running agent* is precisely what
those caps exclude.

**2 · Multi-cloud, and BYOC.** E2B, Modal, Vercel Sandbox and Cloudflare Sandbox are each bound to
one provider's infrastructure. A cell in the customer's own account is something none of them offer.

**3 · Isolation stays an independent choice.** Because the state is outside the pod, the pod is
substitutable: container today, microVM tomorrow, with no change to the agent's architecture. The
Google ADK article proves this by omission — it never mentions isolation once, and does not need to.

## Order of work, by decreasing risk

1. **Tenant egress.** Smallest effort of the set — the policy exists, the target changes — and it
   closes the most serious exposure measured today. An agent that can reach `169.254.169.254` on a
   cell of ours is an agent that can read instance credentials.
2. **How long a turn actually takes**, measured. Without that number the grace period is a guess.
3. **The grace period**, set from (2).

## What is NOT built

A `Draining` phase · a `Suspended` phase · gVisor, Kata or Firecracker · a middle isolation tier · a
domain egress allowlist · a checkpoint backend in the framework · a runs table in `loopzero-memory`.

Each is a mechanism for a problem another mechanism already solves, or that the market does not sell.

## Related

- [`shared-kernel-on-the-managed-door.md`](shared-kernel-on-the-managed-door.md) — why the pod may be
  disposable in the first place
- [`two-doors-managed-and-byoc.md`](two-doors-managed-and-byoc.md) — which door this applies to
- [`../design/durability.md`](../design/durability.md) — D4
- [`../design/states.md`](../design/states.md) — D1, the lifecycle that does NOT gain two phases
- [`../design/trust.md`](../design/trust.md) — D2, the egress boundary
