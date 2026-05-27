---
name: real-ci-active
description: PROPOSED — real CI platform integration replacing GitHub Actions (removed 2026-05-23). T0.2 + T1.5 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
type: decision
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: plan T1.5 + T0.2 design
---

<!-- PROPOSED STATE — depends on operator T0.2 HARD STOP -->

# Real CI Platform Active — CI/CD Diff

> **Plan reference:** T0.2 + T1.5 of `docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md`
>
> **Depends on:** Sponsor T3 meeting to choose CI platform (6 options enumerated em T0.2). Decision blocks Phase 5 re-audit.

## AS-IS (post-GitHub-Actions removal 2026-05-23)

```
┌─────────────────────────────────────────────────────┐
│  Local / operator workstation                       │
│                                                     │
│  task quality:gates  ◄── operator runs manually    │
│  /dogfood full       ◄── operator runs manually    │
│  /chaos-test --full  ◄── operator runs manually    │
│  task drill:l2-scale ◄── operator runs manually    │
└─────────────────────────────────────────────────────┘
         │
         ▼
   PR review (GitHub UI) — no automated gates blocking merge
   Operator approval based on manual run results
```

**Gaps:**
- Zero automated PR-time gates (lint, tests, quality:gates)
- Drill watchdog cron + soaks weekly + coverage reports — operator-mediated
- Trivy + TruffleHog scan: no automated wiring (T4.7 deferred)
- Branch protection: configured but unenforced (no required status checks)

## PROPOSED (post-T0.2 — CI platform chosen)

Architecture varies by sponsor choice. Example (Argo Events + Workflows):

```
PR opened/updated
   │
   ▼
GitHub webhook ──► Argo EventBus ──► Argo Sensor
                                          │
                                          ▼
                                  Argo Workflow:
                                      ├─ task quality:gates     (~5min)
                                      ├─ task validate:manifests (~2min)
                                      ├─ go test ./...           (~10min)
                                      ├─ npm test (cli + dashboard) (~5min)
                                      ├─ trivy fs scan           (T4.7)
                                      └─ trufflehog scan         (T4.7)
                                  │
                                  ▼
                          Status posted back to GitHub PR
                                  │
                                  ▼
                          Branch protection enforces required checks
```

Scheduled CronWorkflows:
- Drill watchdog (every 6h) — kill orphan drill stacks
- Drill soak (weekly Sunday 02:00 UTC) — L2 + Stage 2 24h
- Coverage report (weekly Monday 09:00 UTC)
- Cosign cutover deadline check (daily 00:00 UTC)

## What changes

| Surface | AS-IS | PROPOSED |
|---------|-------|----------|
| PR-time lint | Operator runs locally | Auto-blocking required check |
| PR-time tests | Operator runs locally | Auto-blocking required check |
| PR-time security scan | Skipped | Trivy + TruffleHog blocking |
| PR-time quality gates | Operator runs locally | `task quality:gates` auto-runs |
| Drill watchdog | Operator notes every 6h | Cron auto-fires |
| Soaks | Operator runs weekly | Cron auto-fires |
| Coverage report | Operator generates | Cron auto-publishes |
| Required status checks | None (branch protection has no checks) | All quality gates required |

## Sponsor decision criteria (per T0.2 of plan)

| Option | Pros | Cons | Cost estimate |
|--------|------|------|---------------|
| GitLab CI | Integrated, mature | Requires GitLab SaaS or self-host | $$ |
| Jenkins | Most flexible | High operational overhead, sec concerns | $ |
| Drone CI | K8s-native, simple | Smaller community | $ |
| BuildKite | Hybrid cloud+on-prem | Cost | $$$ |
| Argo Events + Workflows | Re-uses existing infra | Less mature CI than Argo Workflows for builds | $ |
| Self-hosted GH Actions runners | Re-uses workflows | Undoes 2026-05-23 decision | $$ |

## Operator HARD STOP

1. Sponsor + reviewers (Production Design + Security + SRE) attend T3 meeting per T0.2
2. ADR ratified with explicit decision
3. Operator implements CI platform per chosen option (effort varies $4-40h)
4. Cron migration: drill watchdog + soaks + coverage + drift audit + cross-validation
5. Branch protection updated with required status checks

## Operator quick-fallback (if decision delayed > 4 weeks)

Per `customer-trial-onboarding-plan.md` EC-15 (DOCUMENT) — default fallback after timeout: restore self-hosted GitHub Actions runners (status quo of pre-removal 2026-05-23). Tactical preservation; revisit at next quarter.

## Related

- ADR 2026-05-23 (GitHub Actions removal)
- `taskfiles/quality.yml` (current gate harness)
- `cmd/loopzero-ops/quality_subcmd.go` (gate CLI)
- T0.2 of plan (decision driver)
