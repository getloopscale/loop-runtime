---
type: decision
title: Shared kernel on the managed door — BYOC is the isolation product
date: 2026-09-11
status: stable
---

# Shared kernel on the managed door; BYOC is the isolation product

**Decided 2026-09-11 by the sponsor**, after research: *shared kernel for everyone on the managed
PaaS; only BYOC gets a separate account and their own cloud.*

This closes the last open question of D2 — the one the design declared could not be retrofitted.

## The decision

| | how two customers are separated |
|---|---|
| managed PaaS (`starter` … `enterprise`) | namespace + PodSecurity `restricted` + seccomp `RuntimeDefault`. **Shared kernel** |
| BYOC | a separate AWS account, in the customer's cloud. Nothing is shared |

There is **no middle tier**, and no kernel sandbox (gVisor, Kata, Firecracker) on either door.

## What the managed door already enforces

Measured in `api/internal/manifests/renderer_deployment.go:145-160` and
`operators/internal/environment/environment_quota.go:44`:

```
runAsNonRoot · runAsUser/Group 1000 · seccompProfile RuntimeDefault
readOnlyRootFilesystem · allowPrivilegeEscalation=false
AutomountServiceAccountToken=false · namespace PSA enforce=restricted
```

That is the ceiling of what Kubernetes offers without touching the kernel. There is no container
hardening left to add — what remains is the shared kernel, and only kernel isolation addresses it.

## Why this is the industry position, not a compromise

Researched 2026-09-11. Evidence graded: **[READ]** = primary source fetched and quoted,
**[SEARCH]** = reported by search results, not fetched.

| Platform | The customer's normal deploy | Strong isolation | Grade |
|---|---|---|---|
| Heroku | LXC — *"Eco, Basic and Standard dynos, while completely isolated, **share an underlying compute instance** with other dynos"* | Private Spaces (enterprise) | [SEARCH] |
| Railway | *"Container isolation is **shared-kernel** — do not treat containers as hard multi-tenant security boundaries for hostile tenants"* | not published | [SEARCH] |
| Vercel | containers / functions | **Vercel Sandbox**, a separate product | [READ] |
| Supabase · PlanetScale | shared infrastructure | **BYOC, enterprise tier only** | [SEARCH] |
| Confluent | multi-tenant with client quotas | Dedicated cluster + VPC peering | [SEARCH] |

And the platforms that DO isolate by kernel on every workload — E2B (Firecracker), Modal (gVisor),
Daytona (gVisor) — share one property, and it is not that they are newer.

## The line, and it is not PaaS versus not-PaaS

**Was the code written by the person who signed up, or generated at runtime?**

Vercel draws it in its own documentation, and did not convert the platform — it shipped a product
beside it [READ, https://vercel.com/docs/sandbox]:

> *"Run **untrusted or agent-generated code** in isolated Linux microVMs."*
> *"Each sandbox runs in a secure **Firecracker microVM** with its own filesystem and network."*
> *"Execute untrusted code safely: run **AI agent output**, user uploads, or third-party scripts."*

**LoopZero's managed door is on the left of that line, and this is measured rather than assumed:**

- the pod runs the image SHE built from HER code — the renderer emits `Deployment` or `Rollout` of a
  built image, and the LLM injects nothing into it
- `@loopzerokit/agents/pty` is a stub that throws; the PTY backend was deliberately moved out to
  `@loopzerokit/agents-pty` so a web app never installs a terminal it will not open (ADR 0004, #460)
- the hooks do `spawn(cmd, { shell: true, detached: true })` on every tool call, and they are
  **deny-by-default** with a command fingerprint and three states (`approved` / `unknown` /
  `modified`)

So the LLM chooses WHEN a hook fires; it does not choose WHAT runs. That is a webhook's risk profile,
which is the risk profile Heroku has carried for fifteen years.

## What was rejected, and why

**Fargate** — rejected by the sponsor on multi-cloud grounds, and the reason is stronger than
infrastructure lock-in: it would make the ISOLATION MODEL depend on one provider. A customer on AWS
would get a microVM and a customer on DigitalOcean a namespace, under the same price page. Two
products with one label is worse than picking either.

**gVisor** — the only kernel sandbox that survives the multi-cloud requirement (Kata and Firecracker
need nested virtualisation or bare metal, which DigitalOcean does not offer on standard droplets).
Rejected anyway: it is a permanent operational dependency — custom node images, `runsc` on every
node, a different debugging story — and its user-space netstack penalises network I/O, which is
precisely what an agent does all day.

**A middle tier (`team` → dedicated node)** — proposed by the assistant and rejected. Supabase and
PlanetScale have no middle rung: shared, then BYOC at enterprise. The customer who wants real
isolation **buys BYOC**, and that rung already exists. Building a third was complexity with no
concrete case behind it, which is the parsimony ladder's first rung answering no.

## The exit condition — written, because otherwise it becomes default by inertia

> **Reopen this decision when LoopZero offers execution of agent-generated code as a platform
> capability.**

Not "when there are many tenants" and not "if a free tier appears" — both were considered and are
the wrong trigger. The trigger is the line above: the day the platform runs code nobody on the
customer's side wrote, the adversary changes and so does the answer.

**And when that day comes, the precedent says to build a product beside the PaaS, not to change the
PaaS.** Vercel, Cloudflare, Modal and Ramp all shipped sandboxes in early 2026 and none of them
converted their deployment platform.

## What this closes

`cycle-design`'s D2 asked *"is tenant isolation namespace, node, or cell?"* — three exclusive answers
to what is a stack, including a level never used for tenant isolation (`nodeSelector` appears 21
times in this repository and every one is a platform deployment).

Restated and now answered in full:

- between BYOC customers — **the AWS account**
- between one customer's projects — **the namespace**, `loopzero-{tenant}-{project}-{env}`,
  sponsor-directed 2026-06-07, verbatim in `pkg/naming/naming.go`
- between two developers on the managed door — **the namespace, and a shared kernel.** Deliberately

## Related

- [`two-doors-managed-and-byoc.md`](two-doors-managed-and-byoc.md) — the two doors this applies to
- [`../design/trust.md`](../design/trust.md) — D2, which draws both
- [`../design/sign-off.md`](../design/sign-off.md) — where D2 was the blocking question
