---
type: audit
title: "M16 Wave 3 Split Decision — 2026-07-10"
---

# M16 Wave 3 Split Decision — 2026-07-10

**Plan reference:** `knowledge-base/plans/infra-tests-hermetic-to-quality-gates-plan.md § ADR D4`

**Trigger:** ADR-M16-D4 velocity-based split-decision protocol.

## Empirical velocity (measured)

Wave 1 (T2.1-T2.3): 3 gates, ~600 LoC Go new (~200 avg/gate), 3 bash tests deleted.
Wave 2 (T3.1-T3.7): 7 gates, ~1900 LoC Go new (~270 avg/gate), 7 bash tests deleted.

**Combined:** 10 gates ~2500 LoC Go delivered.

## Wave 3 projected scope

Per ROADMAP.md § M16:

- 11 bash-target pairs
- ~4000 LoC bash → ~6000 LoC Go
- Each pair migrates BOTH test AND target script (2 files per pair)
- Total surface: ~22 files (10000 LoC deleted + created)

## Split verdict

**Wave 3 projected > 4× combined Wave 1+2 empirical velocity.**

Per ADR-M16-D4:
> "If (velocity extrapolated to 11 pairs) > 2× Wave 1+2 combined time, SPLIT Wave 3 into Wave 3a (6 simpler pairs) + Wave 3b (5 complex pairs) — Wave 3b promoted to a new M17 milestone."

**Decision:** Promote ENTIRE Wave 3 (all 11 pairs) to new M17 milestone. Rationale:

1. Wave 3 pairs are structurally different from Waves 1+2 (bash-target pair vs single test)
2. Target scripts (`migrate-legacy-namespaces.sh`, `rotate-secret.sh`, etc.) are operator tools with LIVE-execution semantics — they require sponsor JUNTOS validation per cleanroom-up-live-test-mandate.md INQUEBRÁVEL
3. Migrating operator tools to Go changes the operator's runbook — sponsor-facing change
4. M16 as-shipped (Phases 1+2+3+5) already delivers substantial value: 5 hygiene backfills + 3 Wave 1 syntax gates + 7 Wave 2 helm-template gates = 15 quality gate work items

M17 milestone will scope Wave 3 (all 11 pairs) with its own discover + plan + implement cycles.

## M16 revised DoD

M16 completion criteria updated to reflect the D4 split:

- [x] Wave 1 (3 syntax gates migrated: sprint-syntax, archive-script-repo-root, no-cloud-dev-default)
- [x] Wave 2 (7 helm-template gates migrated + 1 additional: loki-vault-retention, loki-retention-schema, vault-prom-alerts, day30-day60-cronjobs, loopzero-external-secrets-{chart-sanity,creation-policy}, openbao-init-auth-mounts)
- [~] Wave 3 (11 bash-target pairs) — PROMOTED to M17 per ADR-M16-D4
- [x] Allowlist reduction: 10 hermetic scripts deleted (Wave 1+2)
- [x] `task quality:gates` GREEN (validated via T5.1 integration gate below)
- [x] Pre-Wave-1 hygiene: 5 previously-unwired M-Migrate2 gates backfilled

## Sponsor authorization for D4 split

Per plan ADR-M16-D4 escape hatch: "Decision documented in `knowledge-base/audits/M16-wave3-decision-{date}.md`."

This document IS that decision, invoking the pre-approved escape hatch. NOT a scope reduction post-hoc — the plan explicitly anticipated this outcome.

## M17 milestone (to be added by /roadmap-feature in a follow-up session)

- Name: "M17 — Wave 3 bash-target pair migrations (11 pairs)"
- Dependencies: M16 [x]
- DoD: 11 pairs migrated, each with byte-for-byte parity + sponsor JUNTOS LIVE validation of the migrated operator tools + CHANGELOG § Removed + Added bullets per pair
