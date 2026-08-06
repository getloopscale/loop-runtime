---
type: architecture
title: Helm Chart Render-Time Testing Architecture
---

# Helm Chart Render-Time Testing Architecture

> **REDIRECT NOTICE (2026-05-21):**
>
> Per CLAUDE.md, `docs/architecture/` was deleted in the 2026-05-11
> governance consolidation as an "empty placeholder". This file exists
> ONLY to satisfy the literal path required by Plan
> [`docs/plans/completed/2026-05-20-helm-render-time-testing-terratest-plan.md`](../../plans/completed/2026-05-20-helm-render-time-testing-terratest-plan.md)
> task T5.2.
>
> **Canonical architecture documentation lives in the knowledge-base:**
>
> - **Operator handoff + gate row:**
>   `.claude/knowledge-base/concepts/operations/operator-handoff-2026-05-20-helm-render-time-testing.md`
> - **Pattern Catalog + helper API + edge cases:**
>   `infra/tests/helm/README.md`
> - **Plan + ADRs (D1-D13):**
>   [`docs/plans/completed/2026-05-20-helm-render-time-testing-terratest-plan.md`](../../plans/completed/2026-05-20-helm-render-time-testing-terratest-plan.md)
> - **MEET ratification:**
>   `.claude/knowledge-base/meetings/MEET-2026-05-20-helm-render-tests.md`
> - **Runtime amendment:**
>   `.claude/knowledge-base/meetings/MEET-ASYNC-AMENDMENT-2026-05-21-001-helm-render-runtime.md`
> - **Cross-validation report:**
>   `.claude/knowledge-base/reviews/cross-validation-2026-05-20-helm-render-time-testing-terratest/report.md`
>
> The 2026-05-11 consolidation moved documentation into the
> knowledge-base because `docs/architecture/` had degraded into stale
> placeholder content. Resurrecting active documentation here would
> violate that decision; this redirect is the honest reconciliation
> between the plan's literal path and current governance.

## Quick Summary

Helm chart render-time tests are tier 2 of validation (between schema
lint and LIVE drill). 22 test packages cover 18 charts + 2 published
variants with ~210 assertions, gated by CI in `helm-charts.yml`. See
canonical docs above for details.
