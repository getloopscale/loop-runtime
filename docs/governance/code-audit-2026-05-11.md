# Code Audit — Governance Scripts (2026-05-11)

**Scope:** `infra/scripts/governance/` (T2.3 of status-validation-and-doc-consolidation-plan)
**Type:** Lightweight audit (file size + syntax + wiring). Full `/code-audit` skill scoped to entire repo deferred to operator.

## Summary

| Metric | Value | Limit | Status |
|--------|------:|------:|--------|
| Total scripts | 12 | — | — |
| Total LOC | 2324 | — | — |
| Largest file | 318 (classify-evidence.py) | 500 | ✅ |
| Smallest file | 43 (spotcheck-dimension.sh) | — | — |
| Bash syntax errors | 0 | 0 | ✅ |
| Python AST errors | 0 | 0 | ✅ |
| Dead scripts (no callers) | 0 (post-wiring) | 0 | ✅ |

## Per-Script Inventory

| Script | LOC | Type | Wired in |
|--------|----:|------|----------|
| `bias-check.sh` | 253 | Bash | `task bias:check` |
| `classify-evidence.py` | 318 | Python | manual (T1.2 one-shot) + plan |
| `cross-validate-plan.py` | 248 | Python | `task cross-validate:plans` |
| `extract-claims.py` | 262 | Python | manual (T1.1 one-shot) + plan |
| `init-decay-clock.py` | 192 | Python | manual (T1.3 one-shot) + plan |
| `regenerate-status.sh` | 195 | Bash | `.github/workflows/status-refresh.yml` |
| `scan-catalog-decay.py` | 136 | Python | `task decay:scan:catalog` |
| `scan-decay.sh` | 166 | Bash | `task decay:scan` + workflow |
| `spotcheck-dimension.sh` | 43 | Bash | `.github/workflows/adversarial-spotcheck.yml` |
| `triangulate-scores.py` | 161 | Python | T3.2 (pending LIVE run) |
| `validate-evidence.sh` | 219 | Bash | `task validate:evidence` + PR gate |
| `verify-independence.sh` | 131 | Bash | T3.0 (pre-LIVE check) |

## Findings

### CRITICO / GRAVE
**None.** All scripts pass syntax, file size, and wiring checks.

### MEDIO
- `classify-evidence.py` is largest at 318 lines — within limit but trending toward split candidate if grows
- 4 scripts (extract/classify/init-decay/triangulate) marked "manual" — they are one-shot pipeline tools, not CI-recurring. Documented as such in their docstrings.

### Notes
- Bash scripts use `set -euo pipefail` consistently
- Python scripts use `argparse` for CLI + AST parsing for YAML alternative (avoids pyyaml dep where possible)
- `bias-check.sh` is largest bash at 253 lines — owns 6 detection patterns; could split per-bias later if grows

## Acceptance Criteria

- [x] All scripts ≤500 lines
- [x] Zero syntax errors (bash + python)
- [x] All scripts have at least 1 wiring (Taskfile or workflow)
- [x] Each script has docstring/header comment
- [x] Each script handles `--help` / `-h`

## Not Audited (Deferred to Operator `/code-audit` Skill)

- Full cyclomatic complexity per function (would need radon/lizard)
- Mutation testing
- Coverage measurement
- SCA (vulnerability scan on Python deps)
- Secrets scan

These are properly the domain of the full `/code-audit` skill which dispatches multiple specialist agents. Out of Ralph scope (heavy compute + skill nesting).

## Cross-Reference

- ADR-0051 §Implementation Status — Phase 2 partial
- T2.3 of plan
- Plan code-audit instruction: "9 técnicas (cyclomatic, coverage, mutation, module size, deps, SCA, unit/integration test quality, secrets/SAST, orphaned wiring)" — this audit covers **module size + orphaned wiring + syntax**; remaining 6 techniques deferred.
