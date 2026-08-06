---
type: decision
title: "ADR: Naming Conventions — Deep Review 2026-05-21 Ratification"
---

# ADR: Naming Conventions — Deep Review 2026-05-21 Ratification

**Date:** 2026-05-21
**Status:** Accepted
**Plan:** [`docs/plans/completed/2026-05-21-deep-review-remediation-plan.md`](../plans/completed/2026-05-21-deep-review-remediation-plan.md) T7.1
**Trigger:** User `/goal` 2026-05-21 flagging `cell2-soaks.yml` as a clean-code violation. Deep review found 5+ workflows with stale-name pattern + cascading anchor references.

## Context

The 2026-05-21 file-by-file deep review identified naming as the worst per-domain grade among 9 audited domains (2.4/4.0). Specific patterns:

1. **Stale-name workflows** — `cell2-soaks.yml` was created when cell-2 was new. Today it provisions a generic 4-cluster drill stack. The "2" in the filename has no semantic correspondence. 6 other workflows referenced this stale name as a cron-anchor in comments ("after cell2-soaks", "1h offset from cell2-soaks"), propagating the lie.
2. **Phase/sprint number leakage** — `phase6-dogfood-gate.yml` references "Phase 6" which appears in NO ADR, plan, or CLAUDE.md. Internal-only jargon in a filename.
3. **Dead-concept references** — `provision-homolog.sh` references the pre-drill "homolog" environment that no longer exists.
4. **Numeric suffixes without semantic context** — Pulumi outputs `runtime2ClusterName`, `runtime2Kubeconfig`, etc. Helm + kubectl contexts use `runtime-2` (with hyphen); Pulumi uses `runtime2` (without). Operators must remember the mismatch.
5. **External-document letter references in filenames** — `file-corrections-item-I-upstream.sh`, `verify-corrections-item-C.sh` reference an external `CORRECTIONS.txt` that may not exist long-term.
6. **`drill-` prefix inconsistency** — both `drill-mint-jwt.sh` (drill-specific tool) and patterns like "drill" (action verb). Mixed semantics.
7. **Three different "health-check.sh"** at different paths — ambiguous identity.
8. **Three different JWT-related scripts** (`drill-mint-jwt.sh`, `dr/jwt-diagnose.sh`, `lib/hydra-mint.sh`) — duplication candidates.
9. **Workflow filename prefix inconsistency** — `infra-*`, `drill-*`, `pulumi-*`, bare names, quoted names. No enforced convention.

## Decision

We ratify the following 9 naming conventions as binding. CI gates (T6.x of the deep-review-remediation plan) enforce machine-checkable rules; human review enforces the rest at PR time.

### Rule 1 — Workflow filenames MUST NOT contain cell/tenant/environment identifiers

Workflows orchestrate actions. The action is the identity; the cell or tenant is a parameter the action operates on.

| BAD | GOOD |
|---|---|
| `cell2-soaks.yml` | `drill-prod-identical-soak.yml` |
| `homolog-validation.yml` | `infrastructure-validation.yml` |
| `tenant-x-canary.yml` | `tenant-canary.yml` (with tenant as input) |

**Enforced by:** Human review at PR time + T6.x CI gate.

### Rule 2 — All scripts open with `set -euo pipefail`

Sourced libs (`**/lib/*.sh`) exempt because `set -e` propagates to the caller.

**Enforced by:** `infra/scripts/quality/check-shell-hygiene.sh` (T6.1) — added Phase 6.

### Rule 3 — State-writing scripts MUST support `--dry-run`

Detection heuristic: scripts containing `kubectl apply`, `helm install`, `pulumi up`, `doctl`, or `git push` MUST honor a `--dry-run` flag.

**Enforced by:** `infra/scripts/quality/check-dry-run-support.sh` (T6.2) — added Phase 6.

### Rule 4 — `pulumi.secret()` wrap mandatory for sensitive outputs

Any Pulumi output that embeds an `Output<string>` of a sensitive value (token, password, key) MUST be wrapped in `pulumi.secret(...)`. Static-grep allowlist for non-sensitive interpolations (regions, IDs).

**Enforced by:** `infra/scripts/quality/check-pulumi-secrets.sh` (T6.3) — added Phase 6.

### Rule 5 — `values.schema.json` mandatory in new charts

Existing charts: ALL 47 (34 infra/helm + 13 charts/) have schemas as of 2026-05-21 (T3.1). Library charts (`loopzero-common`) exempt.

**Enforced by:** `infra/tests/charts/values-schema-coverage_test.sh` (T3.1, LIVE).

### Rule 6 — Hardcoded credential default literals are forbidden

Replace literal `loopzero-dev-password` / `loopzero-gitea-dev-2026` (or any future credential literal) with:
- Helmfile-templated `requiredEnv` calls,
- Empty string (chart fail-fast),
- K8s Secret reads in scripts (`kubectl get secret X -o jsonpath`),
- Fail-fast script fallbacks (`${VAR:?required}`),
- OR constified sentinel for production-startup guards (single source of truth).

**Enforced by:** `infra/tests/docs/no-hardcoded-credentials_test.sh` (T1.3, LIVE).

### Rule 7 — `.env.example` linted against code

Every env var consumed by code must appear in `.env.example`. Every example var must have at least one consumer.

**Enforced by:** TBD CI gate (T6.x follow-up).

### Rule 8 — SKILL.md vs implementation drift gate

`.claude/skills/*/SKILL.md` paths and `task X` invocations MUST resolve against the filesystem + Taskfile.

**Enforced by:** `infra/tests/skills/skill-paths_test.sh` (T0.1, LIVE).

### Rule 9 — `Pulumi.<env>.yaml` must list required `secure:` keys

Either as committed encrypted entries OR as comments enumerating the required `pulumi config set --secret X` invocations.

**Enforced by:** `infra/tests/docs/pulumi-required-secrets_test.sh` (T1.5, LIVE).

## Additional naming conventions (NOT yet ratified — open questions)

These were surfaced by the deep review but require further discussion before becoming binding:

- **Pulumi numeric suffixes:** `runtime2ClusterName` → `runtimeCell2ClusterName`. T7.5 of the plan defers this as a coordinated rename touching 5 projects + helmfile consumers.
- **Phase/sprint numbers in filenames:** `phase6-dogfood-gate.yml` rename to `dogfood-slo-gate.yml` (T7.2) requires coordinated update across observability dashboard + prometheus rule + k6 traffic generator.
- **`drill-` prefix policy:** drill-specific tools vs generic tools with `--env drill` flag. Need usage audit before ratifying.
- **Three different `health-check.sh`:** rename per purpose (T2.5 follow-up).

## Consequences

### Positive
- New PRs must follow the 9 conventions or be rejected by automated gates (Rules 2, 3, 4, 5, 6, 8, 9 enforced as of 2026-05-21).
- Stale names (`cell2-soaks`, `phase6-dogfood-gate`, `provision-homolog`) are now flaggable in human review.
- Future debt accumulation around hardcoded credentials is prevented by Rule 6's enforcement gate.
- PR template (post-T7.1 follow-up) will reference this ADR.

### Negative
- Existing files violating Rule 1 require coordinated rename (cell2-soaks done in T0.7; others deferred to T7.2 + T7.5).
- Rule 7 not yet enforced — manual review for now.
- Library charts exempt from Rule 5 means `loopzero-common` is a manual-review-only chart for the foreseeable future.

## Alternatives considered

### A. Single ADR per rule (9 separate ADRs)
- **Rejected because:** atomicity at this granularity creates 9 files for 9 minor rules. The 9 rules are coherent as a single artifact (naming hygiene policy). They share the same evidence (deep review v2 2026-05-21) and the same enforcement model (CI gates).

### B. Codify in CLAUDE.md instead of ADR
- **Rejected because:** CLAUDE.md is the live operations manual; ADRs are the immutable decision record. CLAUDE.md may reference this ADR but the canonical decision lives here.

### C. Defer ratification until all CI gates are blocking
- **Rejected because:** ratifying now (with 7 of 9 gates LIVE-blocking and 2 deferred to T6.x) gives PR authors something to point at. Waiting for 100% gates means the rules accumulate in nobody's head.

## References

- Deep review v2: `.claude/knowledge-base/reviews/2026-05-21-deep-review-naming-credentials-toolchain.md`
- Remediation plan: [`docs/plans/completed/2026-05-21-deep-review-remediation-plan.md`](../plans/completed/2026-05-21-deep-review-remediation-plan.md)
- Memory: `feedback_anti_lying_workaround_pattern` (per binding `NUNCA MINTA PARA MIM`)
- Memory: `feedback_skill_md_outdated_vs_code` (Rule 8 origin)
- Related ADRs (older): `2026-04-26-operators-remediation-classification.md` (precedent for multi-rule ADR)

## Sign-off

This ADR is ratified by the deep-review-remediation plan as a Scope-refinement amendment (per `.claude/rules/pre-release-governance.md` § Plan Amendment Policy) — no separate MEET required because the 9 rules are direct codifications of findings already in the plan body.
