---
name: architecture-index
description: Index of architecture documentation organized by domain. Current state (`.md` em domain root) + PROPOSED state diffs (`diff/` subdirs) per `/architecture-docs` skill conventions.
type: index
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: plan T1.5 (iter 8 — index doc)
---

# Architecture Documentation Index

Architecture docs organized per domain. Per `/architecture-docs` skill conventions:
- `<domain>/*.md` — AS-IS current state (Mode 1/2)
- `<domain>/diff/*.md` — PROPOSED state with explicit `<!-- PROPOSED STATE -->` header (Mode 3)
- After implementation + operator approval, PROPOSED docs promote to main (Mode 4 — replaces AS-IS, deletes diff/)

## Index by domain

### Capacity & cost tooling (cross-cutting — AWS-first target)
- [`aws-region-posture-decision.md`](aws-region-posture-decision.md) — **MADR decision memo (M6)**: single vs sharded vs active-active region posture for the re-platform, quantified by the simulator. Recommends **levers-first** (raise `maxPerCell` + prefix delegation → −46% cost, review 32→52) **then sharded multi-region** (+$5,778/mo cuts region blast radius 100%→33%), **defer active-active** (+$49,564/mo post-levers) until an SLA contract justifies it. Every number is a LIVE `capsim_*` MCP output (reproducible).
- [`../operations/loopzero-bottleneck-simulator.html`](../operations/loopzero-bottleneck-simulator.html) — Interactive **bottleneck simulator** (M0–M9): seeds LoopZero's real topology, finds capacity/latency/chaos bottlenecks + **cost per region posture** at scale (pure engine `loopzero-capsim.mjs`, unit-tested 62/62; RDS max_connections per instance class; **chaos trilogy AZ / cell / region** — AZ drops survivor capacity, **cell** contains blast (`killCell`: ~1% at 50k, survivors untouched — the cell-isolation value prop), region single/sharded/active-active failover; cost-coupled single-source w/ the calculator; tunable BFF/Engine replicas that **pack onto mgmt / control-plane nodes** — `mgmtNodes()` + a `mgmt_nodes` bottleneck, floor-dominated at default / coupled at scale). MCP server `../operations/loopzero-capsim-mcp/` exposes `capsim_simulate/chaos/review/cost/compare` (chaos `mode: az|cell|region`). Plans + reviews: `../operations/loopzero-bottleneck-simulator{,-m1..-m9}-{plan,review}.md`.
- [`../operations/loopzero-capsim-mcp/`](../operations/loopzero-capsim-mcp/) — **MCP server** (M2) exposing the simulator as tools (`capsim_simulate` / `capsim_chaos` / `capsim_review`) so Claude reviews LoopZero's architecture grounded on the engine (rubric score + verdict + findings). `claude mcp add loopzero-capsim -- node .../server.mjs`.
- **`loopzero-capsim/` (standalone React app — sibling repo `../../../loopzero-capsim/`)** — the simulator + cost calculator ported to **Vite + React + TS + Tailwind** (tab shell: Simulador | Cost Calculator). Vendors the engine `capsim.mjs` byte-identical (same $76,656 anchor + 85 engine tests (+16 topology) via `node --test`, +3 vitest component tests); `pnpm dev` / `pnpm build` / `pnpm test`. Use when the tool needs a real app surface (embedding, richer UX) rather than the single-file HTML. The HTML tools below remain the zero-build canonical reference.
- [`../operations/aws-cost-calculator.html`](../operations/aws-cost-calculator.html) — Interactive **cost model** for the AWS-first target @ 50k tenants — **engine-backed (M5)**: imports `loopzero-capsim.mjs` and computes via `cost()`/`costBreakdown()` (one source of truth; cells derived from `maxPerCell=500`; Vantage RI prices). Same $ number as the simulator + MCP `capsim_cost` ($76,656 @ 50k). Serve over http (ES modules).

### Diagrams (cross-cutting)
- [`eraser-diagrams-index.md`](eraser-diagrams-index.md) — Living index of the 17 Eraser architecture diagrams (workspace "LoopZero"), incl. a unified **System Landscape (View 0)** entry point + **multi-cell placement/routing (View 12)** + **AWS-first target (View 13 runtime + View 14 IaC + View 15 exposure)**: Core Four-Planes deploy flow + supply-chain/observability/product-surface/memory/MCP-gateway/promptly views + META Model B credential-broker fabric + LoopZeroKit build-source seam + CI (Argo Events) + DR (Velero). Each view scoped + cross-validated against `file:line` evidence (2026-07-01).

### API
- [`api/withenvironmentlock-coverage.md`](api/withenvironmentlock-coverage.md) — withEnvironmentLock audit per mutating route. 34 routes classified (18 LOCKED-SERVICE + 16 NO-LOCK-REQUIRED + 0 REVIEW). Closes M02 HIGH gap; T1.3 + iter 6 follow-up of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.

### Auth
- [`auth/diff/stripe-portal-sdk.md`](auth/diff/stripe-portal-sdk.md) — PROPOSED — Stripe Portal SDK integration (T2.7 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap). Operator HARD STOP: sponsor sign-off for prod Stripe key.

### Build Pipeline
- [`build-pipeline/diff/keyless-active.md`](build-pipeline/diff/keyless-active.md) — PROPOSED — Cosign keyless active post Fulcio + Rekor provisioning. Depends on T2.2 sponsor sign-off + Pulumi component (~16h eng).

### CI/CD
- [`ci-cd/diff/real-ci-active.md`](ci-cd/diff/real-ci-active.md) — PROPOSED — Real CI platform replacing GitHub Actions (removed 2026-05-23). 6 options enumerated em T0.2 of plan; sponsor T3 meeting required.

### Helm Testing
- [`helm-testing/README.md`](helm-testing/README.md) — Helm render-time testing pattern catalog (existing — pre-plan).

### Infrastructure
- [`infra/diff/cosign-digest-enforcement.md`](infra/diff/cosign-digest-enforcement.md) — PROPOSED — Chart deployments emit image@sha256:digest references + ClusterImagePolicy enforces digest. Progressive rollout per EC-5 (warn-mode 7-day → enforce). Depends on T2.3 Change Governor meeting.

### Multi-Cell
- [`multi-cell/argocd-cluster-secret-flow.md`](multi-cell/argocd-cluster-secret-flow.md) — ArgoCD cluster Secret flow (existing — pre-plan).

### Observability
- [`observability/diff/loki-federation.md`](observability/diff/loki-federation.md) — PROPOSED — Loki cross-cluster federation. 3 options analyzed; Option C recommended (relocate to mgmt + Ruler, 6h eng + 24h drill soak). T4.6 of plan.

### Platform
- [`platform/clustercredential.md`](platform/clustercredential.md) — ClusterCredential architecture (existing — pre-plan).

## Naming + structure conventions

Per `/architecture-docs` skill:
- Domains group related concerns (api, auth, build-pipeline, ci-cd, infra, multi-cell, observability, platform, helm-testing)
- AS-IS docs live em domain root with `<!-- AS-IS STATE -->` header (optional)
- PROPOSED docs live em `<domain>/diff/` with mandatory `<!-- PROPOSED STATE -->` header
- All docs have YAML frontmatter per `.claude/rules/auto-docs.md`: `name`, `description`, `type`, `created_at`, `updated_at`, `updated_by`

## Promotion workflow (Mode 4)

When PROPOSED implementation approved + LIVE-validated:
1. Operator runs `/architecture-docs` Mode 4 OR manually:
2. Copy `<domain>/diff/<name>.md` → `<domain>/<name>.md` (overwrites AS-IS)
3. Remove `<!-- PROPOSED STATE -->` header
4. Delete `<domain>/diff/<name>.md`
5. Update this index entry to point to promoted version
6. Append log.md entry per `.claude/rules/auto-docs.md` Rule 6

## Related

- `.claude/rules/auto-docs.md` — page schema + write rules
- `.claude/knowledge-base/index.md` — wider knowledge-base index (concepts, audits, ADRs, plans)
- `.claude/skills/architecture-docs/` — skill that generates these
- Plan `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md` T1.5 — context for new PROPOSED diff docs em this index
