# CI Gate Coverage Matrix — Deep Review Remediation Phase 6 Closure

> **Status:** Closure document for T6.x gates (Phase 6 of plan
> `2026-05-21-deep-review-remediation`). Records which gates LIVE
> as of 2026-05-21 and which are deferred with rationale.

Last updated: 2026-05-21 (T6.4 + T4.7 closure batch).

## Rule → Gate mapping

Per ADR `docs/decisions/2026-05-21-naming-conventions-adr.md` § Enforcement.

| Rule | Gate file | Status | Plan task |
|---|---|---|---|
| Set -euo pipefail in scripts | `infra/tests/scripts/check-shell-hygiene_test.sh` | **LIVE** (BLOCKING) | T6.1 |
| State writers support --dry-run | `infra/tests/scripts/check-dry-run-support_test.sh` | **LIVE** (BASELINE-AWARE blocking; 11 grandfathered) | T6.2 |
| Pulumi outputs wrap sensitive interpolations | `infra/tests/scripts/check-pulumi-secrets_test.sh` | **LIVE** (BLOCKING) | T6.3 |
| SKILL.md paths resolve | `infra/tests/skills/skill-paths_test.sh` | **LIVE** (BLOCKING) | T6.4 (covered by T0.1) |
| `withEnvironmentLock` on mutating API routes | `infra/tests/scripts/check-environment-lock_test.sh` | **LIVE** (BASELINE-AWARE blocking; 21 grandfathered) | T6.5 |
| `pg_advisory_xact_lock` only (no session-scoped) | `infra/tests/scripts/check-advisory-lock-scope_test.sh` | **LIVE** (BLOCKING) | T6.6 |
| CLI exit codes 0-5 only (signal handlers exempt) | `infra/tests/scripts/check-cli-exit-codes_test.sh` | **LIVE** (BLOCKING) | T6.7 |
| `continue-on-error: true` requires rationale comment | `infra/tests/workflows/advisory-mode-annotated_test.sh` | **LIVE** (BLOCKING) | T6.8 |
| Rule-file Enforcement sections populated | `.claude/rules/*.md` 'Enforcement' header tables | **LIVE** (documentation) | T6.9 |
| values.schema.json mandatory in charts | `infra/tests/charts/values-schema-coverage_test.sh` | **LIVE** (BLOCKING) | T3.1 |
| No hardcoded credential defaults | `infra/tests/docs/no-hardcoded-credentials_test.sh` | **LIVE** (BLOCKING) | T1.3 |
| Pulumi.<env>.yaml lists required secret keys | `infra/tests/docs/pulumi-required-secrets_test.sh` | **LIVE** (BLOCKING) | T1.5 |
| Credentials SoT lists every code-created Secret | `infra/tests/docs/credentials-sot_test.sh` | **LIVE** (BLOCKING) | T1.1 |
| Action versions pinned OR governance-tagged | `infra/tests/workflows/action-pinning-governance_test.sh` | **LIVE** (BLOCKING) | T4.3 |
| Workflow jobs have timeout-minutes (reusable callers exempt) | `infra/tests/workflows/job-timeout-coverage_test.sh` | **LIVE** (BLOCKING) | T4.4 |
| Drift detector (charts/ vs infra/helm/) | `infra/scripts/audit-helm-drift.sh` + `charts-drift-cron.yml` | **LIVE** (BLOCKING since 2026-05-21) | T3.2 |

## Net coverage

- **15 gates LIVE-blocking** across plans T1.1, T1.3, T1.5, T3.1, T3.2, T4.3, T4.4, T6.1, T6.2, T6.3, T6.5, T6.6, T6.7, T6.8 + T0.1 (skill-paths covers T6.4).
- **2 baseline-aware** (T6.2 + T6.5) — currently allowing legacy violations but blocking NEW additions; backlog tasks track the cleanup.

## T6.4 closure rationale

The plan's T6.4 was "SKILL.md drift gate" — a specific instance of the broader "docs match code" pattern. T0.1 of Phase 0 shipped `infra/tests/skills/skill-paths_test.sh` which:
- Scans every `.claude/skills/*/SKILL.md`
- Asserts every `infra/scripts/X` path mentioned exists on disk
- Asserts every `` `task X` `` invocation exists in Taskfile

This already implements T6.4's intent. T6.4 in the plan is closed as **"covered by T0.1"** — no separate gate needed.

## T4.7 closure rationale

The plan's T4.7 was "audit + classify all `continue-on-error: true`". T6.8 of Phase 6 shipped `infra/tests/workflows/advisory-mode-annotated_test.sh` which:
- Scans every `.github/workflows/*.yml`
- For each `continue-on-error: true` line, asserts adjacent rationale comment OR graduation marker within 5 lines above
- LIVE state: 11/11 entries annotated (zero un-governed)

T4.7's audit work is permanently embedded in the gate — there's no separate one-time audit needed because the gate audits on every PR. T4.7 in the plan is closed as **"covered by T6.8"**.

## Honor-system rules without machine enforcement

Per ADR D8 (honest disclosure), these 8 rules remain HONOR-SYSTEM:

1. **95% confidence floor** (`workflow.md`) — inherently unverifiable by automation
2. **Task completion gate** (`workflow.md`) — code review only
3. **Extreme honesty** (`workflow.md`) — code review only
4. **Project name vs UUID for builds/releases** (`loopzero-design.md`) — code review
5. **Placement sticky / DynamicRouter pure lookup** (`loopzero-design.md`) — code review
6. **LoopZeroError typed codes (not `errors.New`)** (`loopzero-design.md`) — code review
7. **State machines: `Transition()` only** (`loopzero-design.md`) — invalid transitions panic at runtime (semi-enforced)
8. **KISS / YAGNI / DRY / SOLID design principles** — these are quality dimensions, not booleans

Each rule file declares its enforcement status openly via the Enforcement table at the top.

## Next-phase backlog

- **T6.5 baseline cleanup:** triage 21 grandfathered handlers; reduce baseline toward 0
- **T6.2 baseline cleanup:** adopt `lib/dry-run.sh` in 11 grandfathered scripts; reduce baseline toward 0
- **skip-link baseline cleanup:** 109 grandfathered legacy violations (from prior comprehensive-quality-gates plan)
- **mutation testing baseline:** T3 mutation infrastructure (plan section currently parked)

## References

- Plan: `docs/plans/2026-05-21-deep-review-remediation-plan.md` Phase T6 + T4
- ADR: `docs/decisions/2026-05-21-naming-conventions-adr.md`
- Memory: `feedback_anti_lying_workaround_pattern` — advisory-without-graduation forbidden
