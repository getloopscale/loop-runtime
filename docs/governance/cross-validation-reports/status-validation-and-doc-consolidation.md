# Cross-Validation Report — status-validation-and-doc-consolidation

**Date:** 2026-05-11
**Tasks scanned:** 36

## Summary

| Severity | Count |
|----------|------:|
| CRITICO | 15 |
| GRAVE | 9 |
| MEDIO | 3 |
| OK | 9 |

## CRITICO — 15 tasks

### T1.4 — Stale Claims Report

- **Files total:** 2
- **Existing:** 0
- **Missing:** 2

  Missing files:
  - `docs/governance/stale-claims-2026-05-12.md`
  - `scripts/scan-decay.sh`

### T2.1 — Cross-Validation on All Open Plans

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `docs/governance/cross-validation-summary.md`

### T2.2 — Dead Code & Wiring Audit

- **Files total:** 3
- **Existing:** 0
- **Missing:** 3

  Missing files:
  - `docs/governance/dead-code-2026-05-12-go.md`
  - `docs/governance/dead-code-2026-05-12-ts.md`
  - `docs/governance/wiring-gaps-summary.md`

### T2.3 — Code-Audit on Critical Paths

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `docs/governance/code-audit-2026-05-12.md`

### T2.4 — Divergence Report (Claim vs Reality)

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `docs/governance/divergence-report-2026-05-12.md`

### T3.1 — Run 3 Personas Independently

- **Files total:** 4
- **Existing:** 0
- **Missing:** 4

  Missing files:
  - `docs/governance/scoring-2026-05-12/p1-vercel-dd.yaml`
  - `docs/governance/scoring-2026-05-12/p2-hostile-vc.yaml`
  - `docs/governance/scoring-2026-05-12/p3-paranoid-sre.yaml`
  - `docs/governance/scoring-2026-05-12/raw-runs.md`

### T3.2 — Triangulation Analysis

- **Files total:** 2
- **Existing:** 0
- **Missing:** 2

  Missing files:
  - `scripts/triangulate-scores.py`
  - `docs/governance/scoring-2026-05-12/triangulation.yaml`

### T3.3 — Reconciliation Meeting for High-Delta Dimensions

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `docs/governance/scoring-2026-05-12/reconciled.yaml`

### T4.4 — Auto-Generated Sections

- **Files total:** 2
- **Existing:** 0
- **Missing:** 2

  Missing files:
  - `scripts/regenerate-status.sh`
  - `github/workflows/status-refresh.yml`

### T5.1 — CI Cross-Validation Weekly

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `github/workflows/cross-validation-weekly.yml`

### T5.2 — CI Decay Scanner Weekly

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `github/workflows/decay-scan.yml`

### T5.3 — Nightly Adversarial Spot-Check

- **Files total:** 2
- **Existing:** 0
- **Missing:** 2

  Missing files:
  - `github/workflows/adversarial-spotcheck.yml`
  - `scripts/spotcheck-dimension.sh`

### T6.1 — ADR-XXXX Documenting Claim-Evidence Contract

- **Files total:** 2
- **Existing:** 0
- **Missing:** 2

  Missing files:
  - `docs/meetings/adr/00XX-claim-evidence-decay-contract.md`
  - `docs/meetings/records/MEET-2026-05-XX-XXX.md`

### T6.3 — Plan Template Update

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `claude/skills/to-plan/SKILL.md`

### T6.4 — Pre-Merge Gate

- **Files total:** 1
- **Existing:** 0
- **Missing:** 1

  Missing files:
  - `github/workflows/status-pr-gate.yml`

## GRAVE — 9 tasks

### T0.3 — Decay TTL Policy + Detector Script

- **Files total:** 3
- **Existing:** 1
- **Missing:** 2

  Missing files:
  - `scripts/scan-decay.sh`
  - `github/workflows/decay-scan.yml`

### T0.4 — Bias Taxonomy + Self-Check Script

- **Files total:** 3
- **Existing:** 1
- **Missing:** 2

  Missing files:
  - `scripts/bias-check.sh`
  - `claude/skills/to-plan/SKILL.md`

### T0.5b — Conceptual Diagrams: Bias Taxonomy + Decay Lifecycle (Excalidraw)

- **Files total:** 3
- **Existing:** 1
- **Missing:** 2

  Missing files:
  - `docs/architecture/governance/concept-bias-taxonomy.excali`
  - `docs/architecture/governance/concept-decay-lifecycle.excali`

### T1.1 — Claim Extraction Script

- **Files total:** 3
- **Existing:** 1
- **Missing:** 2

  Missing files:
  - `scripts/extract-claims.py`
  - `docs/governance/extraction-patterns.md`

### T1.3 — Decay Clock Initialization

- **Files total:** 2
- **Existing:** 1
- **Missing:** 1

  Missing files:
  - `scripts/init-decay-clock.py`

### T3.0 — Independence Hardening (per EC-1 MUST FIX)

- **Files total:** 3
- **Existing:** 1
- **Missing:** 2

  Missing files:
  - `docs/governance/independence-verification.md`
  - `scripts/verify-independence.sh`

### T3.0b — Independence Matrix Diagram (Excalidraw)

- **Files total:** 3
- **Existing:** 1
- **Missing:** 2

  Missing files:
  - `docs/architecture/governance/concept-independence-matrix.excali`
  - `docs/governance/independence-verification.md`

### T4.0 — Evidence Triangulation Pipeline Diagram (Excalidraw)

- **Files total:** 2
- **Existing:** 1
- **Missing:** 1

  Missing files:
  - `docs/architecture/governance/concept-evidence-triangulation-pipeline.excali`

### T4.1 — STATUS.md Template + Initial Content

- **Files total:** 2
- **Existing:** 1
- **Missing:** 1

  Missing files:
  - `docs/governance/status-template.md`

## MEDIO — 3 tasks

### T0.1 — Evidence Bar Policy + Validator Script

- **Files total:** 3
- **Existing:** 2
- **Missing:** 1

  Missing files:
  - `scripts/validate-evidence.sh`

### T1.2 — Evidence Tier Classifier

- **Files total:** 3
- **Existing:** 2
- **Missing:** 1

  Missing files:
  - `scripts/classify-evidence.py`

### T4.6 — STATUS.md Information Architecture Diagram (Excalidraw)

- **Files total:** 3
- **Existing:** 2
- **Missing:** 1

  Missing files:
  - `docs/architecture/governance/concept-status-md-information-architecture.excali`

## OK — 9 tasks

### T0.2 — Adversarial Persona Library

- **Files total:** 4
- **Existing:** 4
- **Missing:** 0

### T0.5 — Documentation Domain Architecture (`/architecture-docs` Mode 2 AS-IS)

- **Files total:** 5
- **Existing:** 5
- **Missing:** 0

### T3.4 — Final Score per Dimension with Evidence Trail

- **Files total:** 0
- **Existing:** 0
- **Missing:** 0

### T4.2 — Migrate `maturity-assessment.md` to History

- **Files total:** 2
- **Existing:** 2
- **Missing:** 0

### T4.3 — Refactor `CLAUDE.md` Status Line

- **Files total:** 0
- **Existing:** 0
- **Missing:** 0

### T4.5 — Evidence File Population

- **Files total:** 1
- **Existing:** 1
- **Missing:** 0

### T5.4 — Notification Channel

- **Files total:** 0
- **Existing:** 0
- **Missing:** 0

### T6.2 — CLAUDE.md Rule 14 (Claim Evidence)

- **Files total:** 0
- **Existing:** 0
- **Missing:** 0

### T6.5 — Documentation Architecture PROPOSED (`/architecture-docs` Mode 3)

- **Files total:** 4
- **Existing:** 4
- **Missing:** 0

