---
claim_id: validate-evidence-script-tested
dimension: tests-confidence
score_impact: 0.1
evidence:
  - type: T1
    ref: commit:e5c25a3c:infra/scripts/governance/validate-evidence.sh
  - type: T2
    ref: live:2026-05-11T00:00:00Z:docs/evidence/_fixtures
  - type: T3
    ref: ci:decay-scan.yml:pending-first-run
ttl_days: 30
last_validated: 2026-05-11
expires: 2026-06-10
---

# Claim: validate-evidence.sh classifies evidence files correctly (5 fixtures)

## Why this claim matters

`infra/scripts/governance/validate-evidence.sh` is the gate that enforces ADR D1's 3-source requirement. If the validator misclassifies, the entire governance system is silently broken — claims could pass without 3 sources, or with non-distinct refs (EC-6 violation).

This script is the foundational `task validate:evidence` target and is invoked by the future pre-merge gate (T6.4). Its correctness is a precondition for everything downstream.

## How to revalidate

```bash
# 1. Run the task wrapper
task validate:evidence:fixtures
# Expected output: 5 fixtures exercised
#   - valid-three-sources.md → exit 0 (EVIDENCED)
#   - single-source.md → exit 0 (PARTIAL_CONFIDENCE)
#   - invalid-duplicate-refs.md → exit 1 (EVIDENCE_REFS_NOT_INDEPENDENT)
#   - future-timestamp.md → exit 1 (last_validated in the future)
#   - expired.md → exit 2 (TTL elapsed)

# 2. Verify shellcheck (when available)
shellcheck infra/scripts/governance/validate-evidence.sh

# 3. Update frontmatter last_validated + expires after manual confirmation
```

## Evidence detail

### T1 — Code + Test
- Commit: `e5c25a3c` — Phase 0 commit
- Test fixtures: `docs/evidence/_fixtures/*.md` (5 files exercising pass/fail/expired paths)
- What the tests assert:
  - Valid 3-source claim → exit 0 with EVIDENCED label
  - Single-source claim → exit 0 with PARTIAL_CONFIDENCE warning
  - Duplicate refs in T1/T2/T3 → exit 1 with EVIDENCE_REFS_NOT_INDEPENDENT
  - Future `last_validated` → exit 1
  - Expired TTL → exit 2 with EXPIRED label

### T2 — LIVE Run
- Date: 2026-05-11 (Phase 0 + Phase 1 sessions)
- Log location: `docs/evidence/_fixtures/` (in-repo, no external storage needed for <1MB)
- Command executed: `task validate:evidence` and `task validate:evidence:fixtures`
- Output highlights: 5 fixtures classified per spec (see "How to revalidate")

### T3 — CI Run
- Workflow: `decay-scan.yml` (calls scan-decay.sh which validates evidence frontmatter)
- Run ID: _pending first scheduled Sunday run (2026-05-17 02:00 UTC)_
- Status: validator script is currently smoke-tested locally only

## Notes / Open concerns

- T3 is a placeholder until first scheduled CI run completes (workflow armed but never executed).
- After first CI run, this evidence file should be updated with real run ID + workflow URL.
- This evidence file itself is a **prova-de-conceito** that the evidence pipeline works end-to-end (extract → classify → validate → file → STATUS.md link).

## Cross-references

- Plan: `docs/plans/status-validation-and-doc-consolidation-plan.md` v1.2 T0.1
- ADR D1: confidence cap 95-97%, 3-source requirement
- Related runbooks: `docs/GOVERNANCE.md (§1 Evidence Bar)`
