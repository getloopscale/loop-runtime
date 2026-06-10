# Evaluations: External Tooling — Deep Review 2026-05-21 Phase 8

**Date:** 2026-05-21
**Status:** Recommendations recorded; adoption deferred to per-tool sponsor decision
**Plan:** `docs/plans/completed/2026-05-21-deep-review-remediation-plan.md` T8.1–T8.4

## Context

Deep review v2 (`.claude/knowledge-base/reviews/2026-05-21-deep-review-naming-credentials-toolchain.md`) cross-referenced LoopZero's open pain points against `awesome-helm` (cdwv) and `awesome-pulumi` (pulumiverse). This document records the resulting per-tool evaluation. None of these are committed adoptions — each is a recommendation with adoption cost + benefit analysis. Sponsor decides per tool.

---

## T8.1 — helm-chartsnap (snapshot-based drift detection)

- **Source:** [jlandowner/helm-chartsnap](https://github.com/jlandowner/helm-chartsnap)
- **Maturity:** Active 2024–2025. Single-maintainer but stable surface area.
- **Maps to LoopZero pain:** dual-chart drift (`infra/helm/X` vs `charts/X`) — currently handled by `infra/scripts/audit-helm-drift.sh` (graduated to blocking 2026-05-21 per T3.2). The drift gate operates on **template count + name**, not on rendered output. `helm-chartsnap` snapshots rendered manifests + diffs them.
- **What it adds:**
  - True structural diff of rendered output (catches values divergence in addition to template-set divergence)
  - Reproducible: same input → same snapshot → byte-exact diff
  - CI-friendly: snapshot files committed to repo as test fixtures
- **What it costs:**
  - One snapshot test per chart-pair (3 today: loopzero-api, loopzero-cloud, loopzero-admission-policies)
  - Updates needed on every legitimate values change (snapshot churn — opt-in via `--update`)
  - Dependency on jlandowner (single maintainer; bus-factor 1)
- **Decision:** **Evaluate via POC** for loopzero-api/loopzero-cloud/loopzero-admission-policies pair. If POC confirms drift detection >= the current grep-based gate AND snapshot churn is manageable, adopt as **complement** to (not replacement of) `audit-helm-drift.sh`. Combined gate: helm-chartsnap for structural diff + audit-helm-drift for ADR-approval tracking.
- **Adoption risk:** **LOW** (testing tool, not runtime)
- **Sponsor decision required:** budget for ~4h POC + integration time.

---

## T8.2 — helm-schema (auto-generate values.schema.json from values.yaml)

- **Source:** [dadav/helm-schema](https://github.com/dadav/helm-schema)
- **Maturity:** Active 2024–2025. Single-maintainer but small surface area.
- **Maps to LoopZero pain:** T3.1 generated permissive minimal schemas for 16 charts. Per-chart schema specificity (`type`, `enum`, `required`, `pattern`) is manual today. `helm-schema` auto-generates from values.yaml comments using a `# @schema ...` annotation convention.
- **What it adds:**
  - Schemas auto-stay-in-sync with values (no human-rebase needed)
  - Enforces a single source of truth (values.yaml + comments) instead of two (values.yaml + values.schema.json)
  - Optionally generates Markdown docs alongside schema
- **What it costs:**
  - Adds `helm-schema` as build dependency for chart development
  - Migration: existing manual schemas (12 in `charts/`, 18 in `infra/helm/`) need annotation conversion
  - Annotation convention adds visual noise to values.yaml
- **Decision:** **Defer adoption.** Reason: T3.1 + values-schema-coverage gate is sufficient at current maturity. Schemas being permissive isn't blocking issues (no operator has reported "I edited values and got cryptic error"). Re-evaluate when (a) values.schema.json drift becomes a real incident class, OR (b) LoopZero publishes charts to broader audience (where strict schemas guard third-party install).
- **Adoption risk:** **LOW** if adopted; deferral risk also LOW.
- **Sponsor decision:** parked for v1.x.

---

## T8.3 — `@pulumi/command` (replace bespoke doctl orchestration)

- **Source:** [pulumi/pulumi-command](https://github.com/pulumi/pulumi-command) (official, not community)
- **Maturity:** Pulumi-official package, multi-maintainer, GA stable.
- **Maps to LoopZero pain:** `infra/scripts/dr/teardown-validator.sh` (268 LOC) implements bespoke doctl scan + force-delete loop for any drill resources Pulumi's state diverged from. This is a "shell glue around Pulumi" anti-pattern that could be expressed as a Pulumi Custom Resource using `@pulumi/command`.
- **What it adds:**
  - Teardown validation becomes part of Pulumi's resource graph (preview shows what would be deleted)
  - Idempotency built-in (Pulumi tracks resource state across runs)
  - No more shell glue with retry-on-API-error logic — Pulumi handles it
- **What it costs:**
  - Rewrites a 268-LOC bash script into TypeScript Pulumi resource definitions
  - Behavior subtly differs (Pulumi delete vs doctl delete; recovery semantics need verification)
  - Adds dependency on `@pulumi/command` package (low risk — official)
- **Decision:** **Evaluate via POC** — replace `dr/teardown-validator.sh` doctl-scan section with `@pulumi/command` invocations. Compare: reliability (does it catch the same orphan resources?), cost (Pulumi up time delta), readability.
- **Adoption risk:** **MEDIUM** — touches drill teardown safety path. POC must demonstrate equivalence before swap.
- **Sponsor decision required:** ~8h POC + drill cycle to validate (cost ~$1 drill provision).

---

## T8.4 — pulumi-local (LocalStack for Pulumi state backend)

- **Source:** [localstack/pulumi-local](https://github.com/localstack/pulumi-local)
- **Maturity:** LocalStack-org maintained (org backing, not single individual).
- **Maps to LoopZero pain:** Pulumi S3 state backend tests are LIVE-only today — `pulumi-archive.yml` runs against the actual `s3://loopzero-infra-pulumi-state` bucket. No hermetic test path for state-backend behavior changes.
- **What it adds:**
  - Hermetic test environment for Pulumi state operations
  - Faster iteration on state-related code (Pulumi locking, conflict resolution, race recovery)
  - CI can run state-backend tests without real S3 cost
- **What it costs:**
  - LocalStack covers AWS S3 only — LoopZero uses **DO Spaces** for prod state (compatible S3 API). Need to verify LocalStack's S3 emulation matches DO Spaces semantics (probably yes — Spaces is S3-API-compatible by design).
  - Adds LocalStack as test dependency
  - Maintenance: keep LocalStack version aligned with prod S3 behavior
- **Decision:** **Defer adoption.** Reason: state-backend bugs have NOT been a recurring issue (memory + git log show no past incidents specific to state racing or backend behavior). Adopting now is speculative tooling without a triggering pain.
- **Adoption risk:** **MEDIUM** (DO Spaces vs LocalStack S3 emulation gap).
- **Sponsor decision:** parked unless state-backend incident materializes.

---

## Summary

| Tool | Recommendation | Effort estimate | Sponsor decision needed |
|---|---|---|---|
| **T8.1 helm-chartsnap** | EVALUATE NOW (POC) | ~4h + integration | Yes — budget POC |
| **T8.2 helm-schema** | DEFER to v1.x | — | No — parked |
| **T8.3 @pulumi/command** | EVALUATE NOW (POC) | ~8h + drill cycle (~$1) | Yes — budget POC + drill spend |
| **T8.4 pulumi-local** | DEFER (no triggering pain) | — | No — parked |

## What this evaluation NOT does

- Does not commit to adoption — sponsor approval gates each path
- Does not modify build pipelines today
- Does not benchmark tools beyond paper analysis — POCs are sponsor-budgeted future work

## References

- Deep review v2: `.claude/knowledge-base/reviews/2026-05-21-deep-review-naming-credentials-toolchain.md` § "External tools — concrete adoption recommendations"
- Naming Conventions ADR: `docs/decisions/2026-05-21-naming-conventions-adr.md`
- Plan: `docs/plans/completed/2026-05-21-deep-review-remediation-plan.md` T8.1–T8.4
