# ADR-0011: Scaling Fields — Precedence, Plan Caps, and Enforcement Placement

**Status:** ACCEPTED
**Date:** 2026-04-23
**Origin:** MEET-2026-04-23-059 — expedited T2 meeting, authorized in pre-release phase
**Change ID:** CHG-2026-04-23-B15
**Deciders:** Platform Security, SRE, API/Domain Architect, Verification Architect, DX, Pipeline Reviewer
**Supersedes:** None. First ADR to formalize behavior of `replicas` and `autoscaling` in `loopzero.yaml`.

---

## Context

### The Problem

E2E validation (Phase 1.2 of `docs/plans/e2e-validation-plan.md`) surfaced the following symptom on an existing fixture:

```
$ loopzero deploy
Warning: Unknown field 'apps.api.replicas' -- ignored
Warning: Unknown field 'apps.api.autoscaling' -- ignored
```

Both fields exist in the parsed `LoopZeroYamlApp` interface and the `AppUnit` normalizer. The warning was produced by a CLI validator whose `knownFields` allow-list had never been extended. The apparent "CLI validator bug" was the surface of a deeper chain of gaps:

1. **CLI validator**: `replicas` / `autoscaling` not in `knownFields` → spurious warning.
2. **CLI parser**: `parseLoopZeroYaml` silently dropped invalid ranges (replicas < 1, max < min, targetCPU outside 1–100) → validator had no value to reject explicitly.
3. **CLI → API payload**: `AppInput` in `deploy-types.ts` and the `submitBuild` call in `deploy.ts` never forwarded `replicas` / `autoscaling`. The fields died at the HTTP boundary.
4. **API route DTO**: `routes.AppInput` in `api/internal/routes/builds.go` had no `Replicas` or `Autoscaling` field — JSON decoding silently discarded the bytes.
5. **API adapter**: `adapters.BuildOrchestratorAdapter.SubmitBuild` did not copy scaling fields from `routes.AppInput` to `services.AppSpec`.
6. **Orchestrator**: `services.BuildOrchestrator.SubmitBuild` persisted scaling metadata (renderer consumes it) but performed NO plan-cap enforcement — users on any plan could request any replica count.
7. **GitHub App auto-deploy path**: the webhook adapter extracted no scaling fields, so pushes bypassed enforcement entirely.

The combined effect was a silent no-op: scaling was configurable in the spec, documented in the CLI help, rendered by the manifest renderer, persisted to `build_apps_spec`, and completely unenforced.

### Why "just fix the CLI validator" is insufficient

A one-line allow-list patch would eliminate the warning but leave issues 2–7 intact. The user would still see scaling fields accepted, still have no enforcement, and still hit the plan-cap gap when the platform launches.

This is exactly the "Implemented but not wired" and "Implemented but not documented" systemic pattern documented in `CLAUDE.md` — each layer looked correct in isolation; the chain was broken at every boundary.

### Scaling is a GA blocker

Per MEET-057 FB-3 and ADR-0008 §Implementation Phases (Starter tier launch), per-service replica count is **load-bearing** for:

- **Resource quota coherence** — per-tenant namespace ResourceQuotas (Starter tier) allow 3 services × 2 CPU requests × 2 replicas. Without enforcement, a single starter tenant requesting 100 replicas would trip the quota after the first deploy (breaking all subsequent deploys with cryptic K8s errors).
- **Billing fairness** — unmetered replica counts collapse Starter/Pro/Team differentiation.
- **DoS protection** — an accidental `replicas: 1000` triggers cluster autoscaler spend.

---

## Decision

### Field semantics

1. `replicas` is an integer ≥ 1 describing a fixed pod count.
2. `autoscaling` is an object `{min, max, targetCPU}` describing an HPA.
3. `replicas` and `autoscaling` are **mutually exclusive**. Setting both is rejected as `SCALING_AMBIGUOUS` (HTTP 422) — no silent precedence, no "last one wins", no renderer heuristic.
4. Default when neither is set: `replicas: 1`.

### Plan caps (new metric `max_replicas_per_service`)

| Plan | Cap | Rationale |
|---|---|---|
| Starter | 2 | Quota math: 2 × 3 services × per-pod CPU/memory ≤ Starter namespace ResourceQuota (SRE validated) |
| Pro | 5 | Matches projects cap (3) × replicas (5) × 3 envs = 45 pods typical ceiling |
| Team | 10 | Collaboration-scale ceiling |
| Enterprise | NULL (unlimited) | Unlimited by contract; handled as enterprise short-circuit in `PlanLimiter.CheckValue` |

Seeded via forward-only migration `000036_plan_limits_max_replicas.up.sql` (gate originally named `000032` but sequence had advanced).

### Enforcement placement

The authoritative gate lives in `services.BuildOrchestrator.SubmitBuild`, **inside the environment advisory lock, before `buildStore.Create`**. This single location covers:

- The CLI deploy path (`POST /api/v1/builds` via `BuildOrchestratorAdapter`).
- The GitHub App auto-deploy path (`buildSubmitterAdapter.SubmitBuildFromWebhook` in `api/cmd/autodeploy_adapters.go`).

Neither the route handler nor any middleware is the authoritative gate — both are defense-in-depth layers that can be bypassed by adding a new entry point, while the orchestrator is the single funnel every build passes through.

### New API surface

- `pkg/enforcement.PlanLimiter.CheckValue(tenantID, metric, requested)` — answers "is this requested value within the plan cap?" (contrast with `CheckLimit` which answers "is current usage under the cap?"). Same nil-safety and fail-closed semantics as `CheckLimit`.
- `pkg/errors.ErrReplicasExceedPlan` + factory `NewReplicasExceedPlan(plan, limit, requested, appName)` — 422 with message citing plan name, cap, requested count, app name, upgrade URL.
- `pkg/errors.ErrScalingAmbiguous` — 422 citing both field names.
- `services.PlanCapChecker` interface — minimal contract injected into `BuildOrchestratorDeps.PlanCap`; bridged by `planLimiterCapAdapter` in `api/cmd/setup_orchestrator.go`.

### CLI contract

- Parser (`loopzero-yaml.ts`) PRESERVES all numeric values (including invalid ones) so the validator can reject them explicitly instead of silently dropping.
- Validator (`validate-config.ts`) rejects `replicas < 1`, `replicas > 100`, `autoscaling.max < autoscaling.min`, `targetCPU` outside `[1, 100]`, and the mutual-exclusion case.
- Deploy command (`deploy.ts`) forwards scaling config only when non-default (`replicas == 1` omitted) — additive JSON contract, no schema break.

---

## Alternatives rejected

### A. CLI-only fix (allow-list extension)

Eliminates the warning but leaves 6 downstream gaps. Explicitly rejected in MEET-059 Stage 1 — "this is load-bearing, not cosmetic."

### B. Silent precedence (e.g. `replicas` wins over `autoscaling`)

Rejected. Creates invisible failure modes: users set `autoscaling: {min:2,max:10}` after migration, forget to remove `replicas: 3`, and quietly get a fixed 3-pod deploy with no autoscaling. First sign of failure: missing autoscaling metrics days later. Mutual exclusion with rejection is the Vercel/Fly.io standard.

### C. Route-handler middleware as authoritative gate

Rejected. A middleware attached to `POST /api/v1/builds` is trivially bypassed by the GitHub App auto-deploy path (which does not go through the same route). The orchestrator is the only funnel both paths share.

### D. Orchestrator uses `CheckLimit` (reuse existing method)

Rejected. `CheckLimit` takes `currentCount` meaning "how many do you already have?" — semantically wrong for a per-resource intent check. A new `CheckValue` method preserves single-responsibility: `CheckLimit` handles rate-metered resources (projects, envs, seats), `CheckValue` handles per-resource value caps (replicas, CPU, memory).

### E. Clamp silently to the plan cap

Rejected. Silent clamping hides capacity problems until they manifest as outages at 3 AM. A user asking for 10 and getting 2 without any indication is a violation of the principle of least surprise. 422 with a specific message + upgrade URL is the correct UX.

### F. Defer to a post-launch ADR

Rejected. Scaling is a Starter-tier GA blocker (MEET-057 FB-3). The cost of landing it post-launch is forcing plan-cap enforcement through a ratchet of customer data migrations, and rejecting user payloads already accepted by the platform.

---

## Consequences

### Positive

- Scaling config end-to-end functional (was silently discarded before).
- Unified enforcement point closes the GitHub App auto-deploy gap.
- Typed errors with actionable messages (plan, cap, requested, app, upgrade URL).
- No renderer changes required — the renderer already supports both cases; precedence behavior was the gate that flipped.

### Negative

- Adds a DB round-trip per build submit (one `CheckValue` call per app — typically 1-2). Mitigation: query hits the `plan_limits` table (tiny, entirely in PG buffer cache); latency < 1ms per lookup.
- Plan-cap row missing → fail-closed (denies). Mitigation: migration seeds all 4 plan rows; the test for missing row explicitly asserts deny to prevent silent bypass on schema drift.
- `pkg/enforcement` gains one new method, slightly inflating the interface surface. Justified by the functional requirement (intent-check ≠ usage-check).

### Neutral

- Forward-only migration is additive; rollback leaves unused rows behind.
- `--json` deploy output is unchanged (scaling config is not echoed back).
- Exit codes unchanged (validator rejection → 1, API 422 → 3 via existing `mapBuildError`).

---

## Validation

Per the CHG-2026-04-23-B15 gate validation plan, all 14 items must pass before deletion:

1. `cd cli && npm test` — 980 tests green (was 858 before B15; +3 new for parser preserve, +9 for validator, +3 for deploy payload = 980 total).
2. `cd api && go test ./...` — all packages green.
3. `cd pkg && go test ./...` — 9 packages green, including new CheckValue tests.
4. `task lint` — CLI B15 files clean; pre-existing auth.ts/infra.ts errors out of scope.
5. `task validate:manifests` — pending.
6. Red-first commit evidence required per test target.
7. `grep -n "replicas\|autoscaling" cli/src/adapters/validate-config.ts` — confirmed.
8. `grep -n "Replicas\|Autoscaling" api/internal/routes/builds.go` — confirmed.
9. `grep -rn "ErrReplicasExceedPlan\|ErrScalingAmbiguous" pkg/errors/ api/internal/` — confirmed.
10. `grep -n "max_replicas_per_service" api/internal/db/migrations/000036_*.sql pkg/enforcement/` — confirmed.
11. Manual E2E (Layer 2) — **validated 2026-04-23 on droplet 165.227.121.20**: replicas=2 @ starter → 202+persisted; replicas=3 → 422 REPLICAS_EXCEED_PLAN; autoscaling.max=5 → 422; replicas+autoscaling → 422 SCALING_AMBIGUOUS; CLI warning eliminated.
12. ADR committed (this file) — done.
13. CHANGELOG.md updated — done.
14. Rule 3 sweep — clean (0 leaks in B15 CLI files + 15 --help outputs + 8 --json outputs).

### B15 MEET-059 action #9 — Prometheus metric (added during E2E)

After E2E, `loopzero_plan_limit_violation_total{metric,plan}` was MISSING from the metrics endpoint despite enforcement working correctly. This violated MEET-059 Plan-Limit Verification Strategy item #4. Added as scope amendment:

- New counter `metrics.PlanLimitViolations` with labels `metric` + `plan`.
- Emitted in `enforceScaling`:
  - On `SCALING_AMBIGUOUS` rejection: `{metric="scaling_ambiguous", plan=""}`.
  - On `REPLICAS_EXCEED_PLAN` rejection: `{metric="max_replicas_per_service", plan=<plan>}`.
- Validated in production-parity: both labels fire under their respective 422 conditions.

Gate deletion blocked until all 14 items signed off.

---

## Pre-release bias applied

Per `rules/pre-release-governance.md`, the bias was explicit: "what is the cleanest design if we redesigned this today?" The answer pointed to mutual exclusion + authoritative orchestrator-level enforcement, not the incremental patch that a post-launch ADR would prefer (careful backward-compat on the `replicas: 0` silent drop; preserving silent precedence because "apps exist in production"). Pre-release means we can break the bad pre-existing behavior before users depend on it.
