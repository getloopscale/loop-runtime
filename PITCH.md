<!--
Pitch copy for LoopZeroCloud (loopzero/) landing surfaces — loopzero.example site copy, launch material, sales-led conversations.
Voice: LoopZeroKit aspirational voice — extended to LoopZeroCloud 2026-05-15 via Option A strategic review (5th and final formalization). See ../CLAUDE.md and ./CLAUDE.md.
HERO is LOCKED in the root CLAUDE.md "Locked Narrative" (Type an idea. Get a real backend.). Do not edit without an explicit strategic review.
Cross-Project Rule 9 (Cloud abstraction) is in force throughout this document: user-facing copy says `loopzero deploy`, `live URL`, `rollback`, `production`, `canary deploys`, `signed container images`, `audit log` — NEVER `Kubernetes`, `cluster`, `BuildKit`, `ArgoCD`, `Cosign`, `Argo Rollouts`, `KEDA`. Internals appear only in the Architecture section, marked as DEEP DIVE / Self-host.
Status: Production / GA — confirmed by founder 2026-05-16 (PITCH Audit). See loopzero-website/.claude/meetings/2026-05-16-pitch-audit.md.
-->

**Deploy** — the last verb of the **Chat. Build. Deploy.** ecosystem.

# Where AI agents actually run.

### Type an idea. Get a real backend. We build it, ship it, and hand you a live URL — in ~4 minutes.

*Real backends, real stacks — not generated previews.*

**~4 min deploy · ~3s rollback · LoopZeroKit-native (Python + Node) · signed images by default · multi-cell isolation**

---

## The deploy used to be the bottleneck.

There is a version of shipping where the rollback isn't a runbook, preview environments aren't a separate product, and the backend ships on the same `git push` as the frontend.

**Chat with LoopZeroCode. Build with LoopZeroCreate + LoopZeroKit. Deploy on LoopZero.** Three verbs, one ecosystem. Each verb auditable, each layer open-source (the runtime aside).

Four commands. Then just `git push`.

## What you get

- **Type an idea. Watch it ship.** LoopZeroCode drafts the change. LoopZeroCreate scaffolds the project. LoopZeroKit shapes the app. `loopzero deploy` puts it on a live URL — in ~4 minutes from prompt to production.
- **Every backend shape.** API servers, background workers, frontends, static sites — all from the same monorepo, on the **LoopZeroKit stack (Python + Node)**. One curated, hardened path instead of a long tail of guesses.
- **The whole monorepo, one deploy.** `loopzero init` reads your repo, generates `loopzero.yaml`, and every service ships together. Workers + API + frontend, one push.
- **Rollback in seconds.** `loopzero rollback` reverts to the previous release in ~3s. Cross-environment promote (`loopzero promote --from staging --to production`) ships the same artifact without rebuild.
- **Canary by tier.** Starter gets automatic canary. Pro adds analysis hooks. Team and Enterprise get full step control with auto-abort on failure. Every plan ships zero-downtime deploys and instant rollback.
- **Signed by default.** Every container image signed at build. Team+ gets CycloneDX SBOM per build and build attestation. Audit log filterable by action and resource.
- **Self-host when you need to.** Managed on Starter through Team. Dedicated infrastructure + self-host option + 99.9% SLA + dedicated support on Enterprise.

## Feel it

```bash
# Install
curl -fsSL https://get.loopzero.example | sh

# Four commands to ship
loopzero login
loopzero init        # detect monorepo, generate loopzero.yaml
loopzero connect     # link GitHub repo (one-time)
git push         # every push deploys automatically
```

What `git push` looks like:

```
$ git push origin main

# LoopZero deploys automatically:
  Building api...    [24s]
  Building web...    [18s]
  Deploying...       [15s]

Live:
  api    -> https://acme-my-saas-api-production.loopzero.example
  web    -> https://acme-my-saas-web-production.loopzero.example (CDN)
```

No infrastructure to manage. No build config required. Just push.

## What you'd ship

- **A SaaS MVP in a weekend.** LoopZeroKit scaffolds the app, LoopZero ships API + frontend + workers from one monorepo. Production URL on day one.
- **A B2B agent product.** LoopZeroCode codes the agent logic, LoopZeroKit ships the dashboard, LoopZero serves real customers. Onboarding, billing webhooks, audit log included.
- **A multi-tenant API.** Postgres template, plan-based limits, encrypted secrets per environment. Preview environment per PR for every customer review.
- **A real-time agent control panel.** WebSocket primitives from LoopZeroKit, deployed alongside your API and workers. The agent UI your customers actually use.
- **An internal platform team's runtime.** Self-host on Enterprise — your infrastructure, dedicated cells, SBOM per build, SSO/SAML (post-launch), dedicated support.
- **The Vercel piece Vercel doesn't ship.** Keep your frontend on Vercel if you want; bring your API, workers, and background jobs to LoopZero. Or move it all.

## Why LoopZero

**LoopZero is the production runtime for AI agents** — the deploy destination where the agent you built becomes the product your customers pay for. Real backends, real stacks — not generated previews.

The cloud platform market is split between **frontend-first** (Vercel sells "AI Cloud" anchored on the frontend), **agent infra-page** (Railway just pivoted there), and **sandbox-shaped** (Fly.io). **None of them ship the whole product** — the assistant that codes it, the scaffold that bootstraps it, the framework that shapes it, the runtime that hosts it.

Mastra is the closest comparable in the agent framework space — the difference is the deploy. Mastra runs in your backend; LoopZero runs the backend.

| Capability | LoopZero | Vercel | Railway | Fly.io | Roll your own |
|---|---|---|---|---|---|
| **Frame** | Type an idea. Get a real backend. | "Build and deploy on the AI Cloud." | "Railway for AI Agents." | "Build fast. Run any code fearlessly." | (you) |
| Frontends (React, Vite, Next.js, static) | ✓ (CDN) | ✓ (leader) | ✓ | ✓ | DIY |
| API servers | **✓ — LoopZeroKit stack (Python + Node)** | Serverless only | Yes | Yes | DIY |
| Background workers | **✓** | — | Yes | Yes | DIY |
| Monorepo-native (multiple services per repo) | **✓** | Limited | Partial | Limited | DIY |
| Auto-deploy on `git push` (~4 min) | **✓** | ✓ | Yes | Yes | DIY |
| Rollback in seconds (~3s) | **✓** | Yes | Yes | Yes | DIY |
| Promote across environments (no rebuild) | **✓** | — | Limited | Limited | DIY |
| Preview environment per PR | ✓ | ✓ | Limited | Limited | DIY |
| Signed container images by default | **✓** (every build) | — | — | — | DIY |
| Progressive delivery (canary by tier + auto-abort) | **✓** | — | — | — | DIY |
| SBOM per build (CycloneDX) | **✓** (Team+) | — | — | — | DIY |
| Self-host commercial (your infra) | **✓** (Enterprise) | — | — | Limited | N/A |
| Full cycle: assistant + scaffold + framework + runtime | **✓** (LoopZeroCode + LoopZeroCreate + LoopZeroKit + LoopZero) | Partial (v0 + Vercel) | No | No | DIY |

Vercel optimizes the frontend layer of AI Cloud. Railway pivoted to be an agent's infra page. Fly.io ships sandboxes. **LoopZero ships your product — real backend, real stack, end to end.**

## Pricing

| Plan | Price | Best for | Projects | Environments | Self-host |
|---|---|---|---|---|---|
| **Starter** | **Free** | Side projects, learning | 3 | 1 | — |
| **Pro** | **$29/mo** | Professional developers | 10 | 3 | — |
| **Team** | **$99/mo** | Growing teams (RBAC, SBOM, full canary control) | Unlimited | Unlimited | — |
| **Enterprise** | **Custom** | Organizations at scale (multi-cell, SSO/SAML, SLA) | Unlimited | Unlimited | **✓** |

Every plan includes auto-deploy on push, monorepo support, encrypted secrets, signed container images, instant rollback, `--json` CLI, and the same DX.

Full plan matrix (limits, features, security tiers) lives in [`README.md`](./README.md#plans).

## Why now

AI agents shipped in 2025–2026 went from demo to revenue-generating product. The deploy story they need — managed runtime, signed images, rollback in seconds, monorepo-native — doesn't exist yet. That's the gap.

---

## How it works

> Below this line, full technical vocabulary is in play. Architecture, self-host, supply chain.

### Install

```bash
curl -fsSL https://get.loopzero.example | sh
```

Four commands to ship:

```bash
loopzero login         # OAuth Device Code Flow
loopzero init          # detect monorepo, generate loopzero.yaml
loopzero connect       # link GitHub repo (one-time)
git push           # every push deploys automatically
```

### Quickstart

```bash
cd my-monorepo
loopzero init                      # reads your monorepo, generates loopzero.yaml
loopzero connect                   # GitHub App, auto-deploy on push
loopzero deploy                    # first deploy: ~4 min E2E
loopzero promote --from staging --to production  # no rebuild
loopzero rollback                  # ~3s instant revert
loopzero logs --app api --follow   # stream logs
loopzero status                    # active release + URL + scaling
```

Every command supports `--json` for CI/CD. **JSON field names are a stability contract** — they don't change without a major version bump.

### CLI surface

**40 command groups** organized into core workflow + resources:

| Category | Commands |
|---|---|
| Core workflow | `login` · `logout` · `whoami` · `init` · `connect` · `disconnect` · `deploy` · `status` · `logs` · `rollback` · `promote` · `cancel` · `validate` |
| Projects | `project list / inspect / create / delete` |
| Environments | `env list / inspect / create / delete` |
| Teams + RBAC | `team list / add / remove / role` |
| Secrets | `secret set / list / delete` |
| Builds | `build list / inspect` |
| Domains | `domain add / status / dns / list / remove` |
| Scaling | `scale --app <name> --min N --max M` |
| Audit | `audit` |
| Platform (self-host) | `platform install / status / upgrade / uninstall` |

Exit codes are permanent: `0` success · `1` usage · `2` build · `3` deploy · `4` auth · `5` partial.

### Architecture

LoopZero's runtime is hidden behind `loopzero deploy`. The internals are open under commercial license — full reference in [`README.md`](./README.md#architecture).

The shape:

- **Four-plane separation.** Developer plane (CLI + API), control plane (auth + operators + GitOps), build plane (signed image builds), workload plane (your apps).
- **Multi-cell isolation.** Tenant workloads run on independent cells. Routing is sticky (placement once, route many) — Enterprise gets dedicated cells.
- **GitOps under the hood.** Every deploy is reproducible from a manifest. Every promote is the same artifact, no rebuild.
- **Supply-chain by default.** Every image signed at build. Team+ tier ships CycloneDX SBOM and build attestation per release.

### Self-host (Enterprise)

```bash
loopzero platform install -f customer-values.yaml
```

A 10-step automated installer brings up the full platform on your infrastructure (validates the runtime, installs dependencies, bootstraps credentials, runs migrations, verifies health). Then:

```bash
loopzero platform status     # component health, versions, endpoints
loopzero platform upgrade    # upgrade to new version
loopzero platform uninstall  # remove platform (preserves data)
```

Enterprise tier includes dedicated cells, custom SLA (99.9%+), dedicated support, and SSO/SAML (deferred to v1.x per the public roadmap).

### Stability contract

- **Commands are permanent** — no removal without major version bump.
- **`--json` shape is a contract** — field names don't change without major version.
- **Exit codes are permanent** — `0–5` as listed above.
- **Default behavior is frozen** — opt-in flags for new behavior.

## Where this fits

LoopZero is the **apex** of the [loopzero](https://loopzero.example) ecosystem.

| Step | Product | What it does |
|---|---|---|
| 1 | **LoopZeroCode** | Plan / Code / Infra modes. CLI + Desktop. The coding agent. |
| 2 | **LoopZeroCreate** | Scaffolds the project — LoopZeroKit for Full-Stack AI Agents, or one of 18 multi-language stacks. |
| 3 | **LoopZeroKit** | The framework where the app lives. Routing, auth, real-time, deploy. |
| 4 | **LoopZero** *(this — the apex)* | Managed runtime. `loopzero deploy` → live URL in ~4 minutes. **The paid product; the OSS stack is the funnel.** |

The OSS funnel is free. LoopZeroCloud is the commercial product. Self-host of the PaaS is available via commercial license — use your own infrastructure, pay for the right to ship under our terms.

## Mission

**LoopZero's mission.** From prompt to production. We give every developer the opinion, the infrastructure, and the speed to build and ship real AI agents and applications — with no repetitive setup, no vendor lock-in, and no manual ops.

**LoopZero's vision.** Be to AI agents what Vercel became to the web: the default, obvious, developer-respected path — with an open runtime end to end.

**LoopZeroCloud's vision.** The deploy destination every AI agent and modern application ships to — where `git push` becomes production for the next generation of software.

> Mission and vision as stated above are the canonical wording; there is no separate identity
> document. Voice and banned framings are governed by [`.claude/rules/public-copy.md`](.claude/rules/public-copy.md).

## Common questions

- **What's outside the $29/mo Pro tier?** Pro covers up to 10 projects and 3 environments with full deploy / rollback / promote / canary, signed images, and encrypted secrets. Bandwidth and build minutes are generous and metered per plan — overage is opt-in. Full plan matrix in [`README.md`](./README.md#plans).
- **Can I move off LoopZero later?** Yes. Your source code lives in your GitHub repo. LoopZero reads `loopzero.yaml` (open format), builds standard container images, and runs them. LoopZeroCloud is opt-in convenience, not lock-in. Self-host is available via commercial license.
- **How is billing calculated for Enterprise?** Custom annual contract per workload size, dedicated cells, SLA tier, and SSO/SAML requirements. Talk to us before signup — no surprise bills.
- **What about cold start?** Production workloads stay warm by default. Preview environments scale to zero between PR pushes; warm-up is ~2s for most stacks.
- **How does this compare to Vercel/Railway/Fly.io?** Vercel ships your frontend; Railway pivoted to agent infra; Fly.io ships sandboxes. LoopZero ships the whole product — assistant + scaffold + framework + runtime. See the comparison table above.

## Status

- **Generally Available.** Production runtime, multi-cell architecture, managed Postgres, Cloudflare-managed TLS. CLI shipped: 40 command groups with a `--json` stability contract and permanent exit codes 0-5.
- **Pricing live across all tiers.** Starter Free, Pro $29/mo, Team $99/mo self-serve. Enterprise (custom) is sales-led — no public pricing, no signup form, contact for terms.
- **What ships today** — full deploy / rollback / promote pipeline, preview environments per PR, custom domains with auto-TLS and WAF/DDoS, signed container images on every plan, audit log, plan enforcement, encrypted secrets, monorepo support on the LoopZeroKit stack (Python + Node, server and worker), observability (126 metrics, dashboards, SLO alerts).
- **On the roadmap (v1.x).** Keyless image signing, cross-cluster log streaming, expanded SSO/SAML providers.

## License

**Proprietary / Commercial** — see [LICENSE](LICENSE).

LoopZeroCloud is a managed offering operated by [loopzero.example](https://loopzero.example). Self-host is available under a commercial license — contact `getloopscale@gmail.com`.

## Next step

**Primary:** `loopzero login` — start shipping in 4 minutes.

```bash
curl -fsSL https://get.loopzero.example | sh
loopzero login
```

**Secondary:** [Install LoopZeroCode](https://install.loopzero.example/loopzero-code) to code your way to `loopzero deploy`, or `npm create loopzero@latest` to scaffold a deploy-ready project.

**Tertiary:** [Docs](https://docs.loopzero.example/loopzero) · [GitHub](https://github.com/getloopscale/loopzero) · [Pricing](https://loopzero.example/pricing)

## Community

- Discord: https://discord.loopzero.example/
- X: https://x.com/getloopscale
- LinkedIn: https://linkedin.com/company/getloopscale
