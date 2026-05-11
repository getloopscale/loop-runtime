# Evidence — Status Post-Cleanup 2026-05-11

The 6 subdirectories under `docs/evidence/` (`core-deploy/`, `dx-cli/`, `infra-ops/`, `product-completeness/`, `scale-mt/`, `security/`) are currently **empty**. This is intentional, not an oversight.

## What happened

Commits `d18e3b89` (consolidate to 4 canonical docs) and `fc1793dd` (subsequent cleanup, 383 files / 70055 deletions) removed the per-dimension evidence files that previously lived here:

- `docs/evidence/_TEMPLATE.md` (deleted)
- `docs/evidence/_fixtures/*` (deleted — 5 fixture files)
- `docs/evidence/tests-confidence/*` (deleted — 1 LIVE evidence file)
- Per-dimension evidence YAML/MD that fed the governance scoring pipeline (deleted)

Together with these, the consolidation also removed: `docs/STATUS.md`, `docs/maturity-assessment.md`, `docs/governance/**`, `docs/meetings/**`, `docs/adr/**`, `docs/architecture/**`, `docs/cli-reference.md`.

## Current evidence model

The canonical evidence for every claim that affects the maturity score lives in:

- **`docs/audit/2026-05-11/`** — 12 module audits (`01-cli.md` ... `12-cicd-testing.md`) + `SUMMARY.md`. Each audit cites `file:line` references and Bash grep counts inline, replacing the dimension-folder model.
- **`CHANGELOG.md`** — chronological record of every change with `(#NNN)` / `(MEET-...)` / `(CHG-...)` references.
- **The code itself** — per Rule 2 of `CLAUDE.md` ("Code is authoritative"), the codebase is the spec.

## What about CLAUDE.md Rule 11?

Rule 11 (`Claims need evidence`) used to require updates under `docs/evidence/{dim}/`. After the 2026-05-11 consolidation, Rule 11 was updated to point at `docs/audit/2026-05-11/SUMMARY.md` as the latest baseline; evidence is now consolidated into the audit folder rather than fragmented per-dimension.

## Should this directory be deleted?

That is a **T2 governance decision**, not a doc fix. The empty subdirectories are preserved for now because:

1. Pre-release governance (`.claude/rules/pre-release-governance.md`) requires T2/T3 review before structural deletions.
2. Future evidence might land here again if a new dimension-based scoring model is adopted.
3. Deleting them now would require updating tools/CI that scan `docs/evidence/` — out of scope for a doc-only cleanup.

If the team decides to delete the empty subdirectories, that change should go through Change Governor and update any tooling that references them.

---

**Last updated:** 2026-05-11
**Authority:** This file describes the actual state of `docs/evidence/`. If a future cleanup re-populates the subdirectories with concrete evidence files, update this README to reflect the new model.
