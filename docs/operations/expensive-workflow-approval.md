# Expensive Workflow Approval — Operator Guide

> **Status:** Operator-owned configuration (T4.5 of plan
> `2026-05-21-deep-review-remediation`). This document defines the
> pattern; activation requires GitHub repo settings + GitHub Environment
> setup. Both are operator actions (cannot be done via repo PR).

## Purpose

Expensive workflows (drill provision, L2 scale, prod canary, dogfood
full) cost real money each run. Today they can be:
1. Triggered by ANY collaborator with workflow_dispatch permission
2. Triggered by ANY PR on cron path with no PR-author allowlist
3. Re-run accidentally via "Re-run workflow" button

Per CTO Rule 0 + Rule 5 (50k tenant target = production-priced infra),
each unauthorized run wastes drill stack hours. T4.5 codifies the
approval gate pattern.

## Pattern A — GitHub Environment + reviewers

For workflows that provision drill / consume drill credentials:

```yaml
# .github/workflows/drill-prod-identical-soak.yml (already in place)
jobs:
  provision:
    runs-on: ubuntu-latest
    environment:
      name: drill-spend-approval     # ← GitHub Environment with required reviewers
      url: https://github.com/${{ github.repository }}/settings/environments
    steps:
      - ...
```

**Operator setup (one-time per repo):**
1. Settings → Environments → New environment → `drill-spend-approval`
2. Under "Deployment protection rules":
   - Enable "Required reviewers" → add sponsor + 1 backup
   - Enable "Wait timer" → 5 minutes (allow cancel before $)
3. Restrict secret access: only secrets under `drill-spend-approval` env scope reach this workflow

**Apply to:**
- `drill-prod-identical-soak.yml` (Stage 1 + Stage 2)
- `drill-skills-cron.yml` (drill-test cycle)
- `drill-l2-scale.yml` (L2 harness)
- `drill-hermetic-tests.yml` (drill verification cycle)
- `pulumi-reconstruction-drill.yml` (quarterly DR drill)
- `cosign-rotation-drill.yml` (quarterly cosign rotation)

## Pattern B — PR-author allowlist for load-test-pr

For `load-test-pr.yml` (triggered by `pull_request`):

```yaml
# .github/workflows/load-test-pr.yml
on:
  pull_request:
    paths: ['api/internal/services/**', 'infra/tests/load/**']

jobs:
  l1-smoke:
    if: |
      github.event.pull_request.user.login == 'getloopscale' ||
      github.event.pull_request.user.login == 'paulohenriquevn' ||
      contains(github.event.pull_request.labels.*.name, 'load-test-approved')
    runs-on: ubuntu-latest
    ...
```

**Effect:**
- PRs from collaborators ≠ sponsor + backup → workflow skipped (no $)
- PRs explicitly labeled `load-test-approved` → workflow runs
- Sponsor + backup PRs → workflow runs unconditionally

## Pattern C — manual-only with explicit confirmation token

For destructive infra workflows (`infra-destroy.yml`, `infra-state-reset.yml`):

```yaml
on:
  workflow_dispatch:
    inputs:
      confirm_token:
        description: "Type DESTROY-<stack-name> to confirm"
        required: true
        type: string

jobs:
  destroy:
    if: github.event.inputs.confirm_token == format('DESTROY-{0}', inputs.stack)
    runs-on: ubuntu-latest
    ...
```

## Status (2026-05-21)

| Workflow | Pattern needed | Status |
|---|---|---|
| `drill-prod-identical-soak.yml` | A (env approval) | TODO operator |
| `drill-l2-scale.yml` | A | TODO operator |
| `drill-skills-cron.yml` | A | TODO operator |
| `drill-hermetic-tests.yml` | A | TODO operator |
| `cosign-rotation-drill.yml` | A | TODO operator |
| `pulumi-reconstruction-drill.yml` | A | TODO operator |
| `load-test-pr.yml` | B (PR-author allowlist) | TODO operator |
| `infra-destroy.yml` | C (confirm token) | TODO operator |
| `infra-state-reset.yml` | C | TODO operator |

## What this document is NOT

- Not an alternative to GitHub Environment configuration (operator must do that in repo settings).
- Not enforceable by CI gate — this is operator policy + sponsor accountability.
- Not retroactive for past runs that happened without approval.

## References

- Plan T4.5: `docs/plans/completed/2026-05-21-deep-review-remediation-plan.md`
- CTO Rule 0 (local-fidedigno-ao-prod) + Rule 5 (50k target prod sizing)
- Per memory `feedback_golden_rules_cto_mandate`: drill credentials isolation + cost control are sponsor-priority
