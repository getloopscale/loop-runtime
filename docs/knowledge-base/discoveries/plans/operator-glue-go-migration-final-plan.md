---
version: 1.0
slug: operator-glue-go-migration-final
milestone_id: M15
owner: claude-code (autonomous per sponsor goal-directive 2026-07-10)
created_at: 2026-07-10
generated_by: /discover-plan
---

# Discovery Plan: Operator Glue → Go — Allowlist ≤ 30 (M15 Final)

## Context

**Motivation NOW** — CTO mandate 2026-07-08 to eliminate shell scripts, replaced by FAANG-level Go tooling. M15 W1+W2+W3 shipped 20 seeds via v0.59.0 (19 bash equivalents deleted), yet `.claude/rules/shell-scripts-allowlist.md` still holds **121 allowlisted entries** — the literal DoD `≤ 30` is unmet. The `no-new-bash` gate (M13) prevents growth but does not shrink the stock. Grill for this milestone was answered 2026-07-08 by sponsor delegation; strategic questions resolved. **The remaining unknown is CLASSIFICATION** — which of the 121 entries are migratable (which target: `loopzero-ops seed`, `loopzero-ops quality`, helmfile hook, Ginkgo test) vs KEEP-OPERATOR irredutível.

**Evidence triggering discovery now:**

- `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` = 121
- 20 seeds registered in `cmd/loopzero-ops/seed_subcmd.go` via `loopzero-ops seed list`
- 19 of 20 bash seed equivalents already deleted (only `argocd-gitea-creds` bash residual, semantically different)
- Directory breakdown: infra/scripts/local=31, infra/helmfile/scripts=24, infra/tests/e2e=13, infra/scripts/drill=12, infra/scripts/production=7, infra/scripts/test=6, infra/scripts/lib=6, others=22

## Objective

Produce a blueprint that classifies every one of the 121 allowlist entries into ONE of five migration categories with D4 justification, so `/to-plan` can wave-plan the strangler to `≤ 30` residual with zero big-bang risk.

**Measurable success criteria for the blueprint:**

- Every allowlist entry classified into: MIGRATE-TO-GO-SEED / MIGRATE-TO-GO-QUALITY / DELEGATE-HELMFILE / MIGRATE-GINKGO / KEEP-OPERATOR
- KEEP-OPERATOR count ≤ 30 (else the DoD is unreachable and requires roadmap revision)
- Every KEEP-OPERATOR entry has a D4 re-justification (Necessary + Simple + Operator-time + Allowlisted) beyond "grandfathered"
- 3-6 migration waves proposed with target directory grouping + acyclic dependency order

## In-Scope / Out-of-Scope

### In-scope reference implementations (5 sources)

1. `cmd/loopzero-ops/internal/seed/` — 25 Go source files (20 seeds + 5 helpers) — M15 W1+W2+W3 shipped precedent
2. `cmd/loopzero-ops/internal/quality/` — 100+ Go source files — M-Migrate2 quality-gate migration pattern (M16 precedent)
3. `.claude/rules/shell-scripts-allowlist.md` — the SoT with 121 rows to classify
4. `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md` — D4 taxonomy (Necessary + Simple + Operator-time + Allowlisted)
5. `knowledge-base/grills/operator-glue-go-migration-feature-grill.md` — Q1-Q4 answered 2026-07-08 by sponsor

### In-scope directories to touch during blueprint research

- `cmd/loopzero-ops/internal/seed/` (read-only reference)
- `cmd/loopzero-ops/internal/quality/` (read-only reference)
- `.claude/rules/shell-scripts-allowlist.md` (read-only classification target)
- `infra/scripts/local/`, `infra/helmfile/scripts/`, `infra/tests/e2e/`, `infra/scripts/drill/`, `infra/scripts/production/`, `infra/scripts/test/`, `infra/scripts/lib/`, `infra/scripts/ci/`, `infra/scripts/security/` (read-only source scripts to classify)

### Out-of-scope

- Implementation itself (that's cycle-implement's job)
- Deletion of any script during discovery (blueprint outputs classification only, no mutations)
- Changing the D4 taxonomy itself (per shell-script-strategic-plan.md — LOCKED)
- Migrating scripts NOT in the allowlist (out-of-M15 scope; the allowlist IS the migration surface)

## ADRs (decisions about HOW to investigate)

### D1 — Read the allowlist as ground truth, not the file-system

**Context:** File-system has 189 `.sh` files but allowlist has 121 entries (some scripts are in `archive/` or unused paths, some are exempted per grandfathering rules).

**Decision:** Blueprint classifies ONLY the 121 allowlist entries. Non-allowlisted scripts on disk are out-of-scope (they will be deleted separately by whoever owns them; not M15's job).

**Alternatives rejected:**
- Classify all 189 filesystem `.sh` — expands scope beyond M15 KPI
- Classify by `git ls-files '*.sh'` — same problem

### D2 — Rely on existing seed pattern as canonical shape

**Context:** 20 M15 W1+W2+W3 seeds already shipped establish the pattern.

**Decision:** Blueprint's "MIGRATE-TO-GO-SEED recipe" section references the shipped seed shape verbatim; no new pattern research needed. Same for MIGRATE-TO-GO-QUALITY (M16 precedent).

**Alternatives rejected:**
- Design a new Go pattern — YAGNI; 20 shipped seeds are the pattern of record
- Study external tooling — reinvents the wheel

### D3 — Classification is deterministic + reviewable by reading each script

**Context:** 121 entries require judgment on each. Categories must be based on script content, not filename heuristics.

**Decision:** For each entry, the blueprint's classification MUST cite the actual script's purpose (`head -5` verbatim or 1-line summary from Justificativa column) + which category token applies + why. No filename-based guessing.

**Alternatives rejected:**
- Filename heuristics (`seed-*.sh` → seed category) — misclassifies `seed-post-cleanroom-all.sh` (master orchestrator, KEEP-OPERATOR) as a migratable seed

## Research Questions

Total: 8 questions across 4 corners. Max 3 per corner respected. Min 1 per corner.

| Q# | Question summary | Corner | Fase A (static method) | Fase B (LIVE probe) |
|---|---|---|---|---|
| Q1 | Canonical shape of a shipped `loopzero-ops seed <name>` Go file | tests | Read 3 sample seeds (broker.go, github-app-dev.go, admin-loopzero-dev.go) + verify pattern | SKIP (hermetic reference) |
| Q2 | Canonical hermetic test shape for a seed subcommand | tests | Read 3 sample `*_test.go` files matching Q1 seeds | SKIP (hermetic reference) |
| Q3 | Beyond-stdlib dependencies used by shipped seeds | deps | grep imports across cmd/loopzero-ops/internal/seed/*.go | SKIP (source-static) |
| Q4 | Which live-cluster orchestration primitives (kubectl/psql/curl) do the 20 shipped seeds use vs bash equivalents | deps | grep `exec.Command` + `pgConnectFn` + `httpClient` across shipped seeds | SKIP |
| Q5 | Registration mechanism for a new seed subcommand (init function convention) | tools | Read seed_subcmd.go + one seed_wave*.go init pattern | SKIP |
| Q6 | Per-seed lifecycle: idempotency (read → guard → CreateOrUpdate) + --verify parity | tools | Read broker.go end-to-end + trace Run() vs Verify() branches | SKIP |
| Q7 | Classification methodology for the 121 allowlist entries into 5 categories | techniques | Read allowlist entries with their Justificativa column + apply D4 taxonomy | SKIP |
| Q8 | Wave ordering — which categories/directories to migrate first for lowest risk | techniques | Analyze dependency graph: `seed-post-cleanroom-all.sh` chain (Step 1-12) is the critical strangler dependency | SKIP |

**Coverage:** 100% — every question has an executable static method.

## Coverage Matrix

| Corner | Questions | Status |
|---|---|---|
| tests | Q1, Q2 | 2 populated |
| deps | Q3, Q4 | 2 populated |
| tools | Q5, Q6 | 2 populated |
| techniques | Q7, Q8 | 2 populated |

**Coverage: 4/4 corners populated, 8 questions, all deterministic.**

## Halt-loop Checkpoints

For `/discover-execute`:

- **Checkpoint 1** — All 8 questions answered with concrete file:line citations for Q1-Q6, allowlist row citations for Q7-Q8
- **Checkpoint 2** — Q7 classification table has EXACTLY 121 rows (matches allowlist count) with 5-category tokens
- **Checkpoint 3** — KEEP-OPERATOR count from Q7 ≤ 30 (else HALT + surface to human as ROADMAP_BLOCKED)
- **Checkpoint 4** — Q8 proposes ≥ 3 waves with acyclic dependency ordering
- **Checkpoint 5** — Blueprint contains a "Migration recipe" section codifying the strangler workflow (per-seed --verify GREEN before `git rm`)

Blueprint incomplete on any checkpoint → halt-loop continues; blueprint complete → emit `<promise>BLUEPRINT_COMPLETE</promise>`.

## Acceptance Criteria

- Every research question has a concrete answer with `file:line` citation (Q1-Q6) or allowlist row citation (Q7-Q8)
- Every citation resolves (Path.exists check at blueprint-write time)
- 121-row classification table in Q7 covers 100% of allowlist entries
- KEEP-OPERATOR subset ≤ 30 (blueprint DoD KPI)
- 3-6 waves proposed with dependency ordering (Q8)
- 14-step Migration recipe codified (mirroring M16 blueprint pattern)

## Global Definition of Done

Blueprint reaches `SHIPPABLE_WITH_CAVEATS` verdict on `/discover-confidence` re-score. Downstream `/to-plan` can consume the classification table directly as wave input.

## Cross-references

- Grill: `knowledge-base/grills/operator-glue-go-migration-feature-grill.md`
- Strategic plan: `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md` (D4 taxonomy)
- Rules: `.claude/rules/shell-scripts-allowlist.md` (SoT), `.claude/rules/parsimony-ladder.md`, `.claude/rules/cycle-discover.md`
- Prior M15 milestone shipped in v0.59.0 (`knowledge-base/releases/v0.59.0-release.md`)
- M16 precedent: `knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`
- Unbreakable Rules: 3 (honesty), 4 (git), 6 (CHANGELOG), 9 (Don't Reinvent), 10 (KISS), 11 (YAGNI)
