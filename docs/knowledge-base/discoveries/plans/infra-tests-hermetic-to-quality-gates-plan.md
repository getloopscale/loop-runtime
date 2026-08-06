---
type: discovery-plan
title: "Discovery Plan: Hermetic Test Migration `infra/tests/check-*_test.sh` → Go Quality Gates"
generated: { by: process:discover-plan, at: 2026-07-10 }
version: 1.1
slug: infra-tests-hermetic-to-quality-gates
milestone_id: M16
owner: claude-code (autonomous per sponsor goal-directive 2026-07-10)
created_at: 2026-07-10
generated_by: /discover-plan
target_cycle: cycle-discover phase 1
---

# Discovery Plan: Hermetic Test Migration `infra/tests/check-*_test.sh` → Go Quality Gates

## Context

**What motivates this discovery NOW:**

M16 milestone was added 2026-07-10 targeting migration of 27 hermetic bash tests to Go quality gates in `cmd/loopzero-ops/internal/quality/`. M15 (sibling milestone) already shipped 12 M-Migrate2 gates covering `infra/tests/check-*_test.sh` variants — those 12 existing gates ARE the reference implementations for the M16 migration pattern.

**Evidence that triggered the discovery:**

- `grep -l "Migrated from infra/tests/check-" cmd/loopzero-ops/internal/quality/*/*.go` returns 12 files (2026-07-10 measurement)
- The pattern is well-established: each migration is `check.go` (~150-200 LoC) + `check_test.go` (~50-150 LoC) + registration in `cmd/loopzero-ops/quality_registry.go`
- 27 remaining hermetic scripts identified in M16 grill (Waves 1+2+3 breakdown)
- `.claude/rules/shell-scripts-allowlist.md` currently 121 entries; M16 target reduces by 27

**Why this needs formal /discover-plan** despite being internal migration:

Per goal-directive 2026-07-10 "Execute o CYCLE discover (deep research, Staff Enginneer Google)" — sponsor explicitly demands FAANG-level rigor including the discover phase. The blueprint output codifies the M-Migrate2 pattern-of-record so no wave in Waves 1/2/3 introduces drift.

## Objective

**One sentence:** Derive a codified M-Migrate2 migration pattern from the 12 existing `cmd/loopzero-ops/internal/quality/*` gates (specifically the subset migrated from `infra/tests/check-*_test.sh`) so that all 27 remaining migrations across Waves 1+2+3 follow byte-identical structure, error handling, and test shape.

**Measurable success criteria for the resulting blueprint:**

1. Blueprint documents the canonical `Config` struct + `Check(cfg) Outcome` signature used by all 12 precedents
2. Blueprint documents the canonical `check_test.go` fixture pattern (t.TempDir + fake filesystem OR `--exec` stub)
3. Blueprint documents the `quality_registry.go` registration line format
4. Blueprint documents the `taskfiles/quality.yml` invocation shape
5. Blueprint documents the CHANGELOG § Removed + § Added entries per gate
6. Zero fabricated citations — every internal reference resolves to a real path

## In-scope / Out-of-scope

### In-scope reference implementations (12 gates)

- `cmd/loopzero-ops/internal/quality/allowlistsunsets/` — Python-subprocess pattern (T5.2.a precedent)
- `cmd/loopzero-ops/internal/quality/esooverlay/` — helm-render precedent
- `cmd/loopzero-ops/internal/quality/installgithooks/` — file-layout assertion precedent
- `cmd/loopzero-ops/internal/quality/k6loadrigor/` — YAML structure assertion precedent
- `cmd/loopzero-ops/internal/quality/caddyfilenodockerip/` — grep-forbidden-pattern precedent
- `cmd/loopzero-ops/internal/quality/canonicalfixturerepo/` — string-in-file precedent
- `cmd/loopzero-ops/internal/quality/oidccredadminloopzero/` — credential-shape assertion precedent
- `cmd/loopzero-ops/internal/quality/optionbeso/` — doc-completeness precedent
- `cmd/loopzero-ops/internal/quality/routeauthcoverage/` — route-map assertion precedent
- `cmd/loopzero-ops/internal/quality/vaultbootstrapinmgmt/` — task-target shape precedent
- `cmd/loopzero-ops/internal/quality/vaultbootstrapunsealloop/` — bash-idiom assertion precedent
- `cmd/loopzero-ops/internal/quality/webhookcanonicalpath/` — HTTP-path invariant precedent

### In-scope orchestration files

- `cmd/loopzero-ops/quality_registry.go` — canonical registration table
- `taskfiles/quality.yml` — canonical invocation shape
- `.claude/rules/shell-scripts-allowlist.md` — the KPI target (121 → 94 post-M16)

### Out-of-scope

- The 89 non-M-Migrate2 gates in `cmd/loopzero-ops/internal/quality/*` — many pre-date M-Migrate2 and use older shapes (e.g., `pkg/quality/*` compat). Migrating M16 by copying those would introduce drift.
- LIVE cluster tests (`infra/tests/e2e/*.sh`, `infra/tests/dogfood/*.sh`) — these are irreducibly LIVE per sponsor JUNTOS mandate, out of M16 scope.
- Chaos tests (`infra/tests/chaos/*.go`) — already migrated to Go.
- The 27 target scripts themselves — those are ANSWERS the M16 plan generates, not references studied during discover.
- External SOTA / GitHub projects — this is internal migration; the pattern-of-record lives in our repo.

## ADRs (decisions about HOW to investigate)

### D1 — Internal precedents ARE the SOTA reference set for M16

**Context:** External projects (e.g., testify, ginkgo, upstream K8s quality gates) do not use our `cmd/loopzero-ops` architecture. External SOTA would be misleading noise.

**Decision:** Restrict discovery to the 12 internal M-Migrate2 gates in `cmd/loopzero-ops/internal/quality/*`. Skip WebSearch entirely.

**Alternatives considered + rejected:**
- Search external Go test frameworks → REJECTED: overlaps zero with our `Config`/`Outcome` shape
- Extend to older 89 gates → REJECTED: shape drift risk (older `pkg/quality/*` compat)

### D2 — Time budget

**Decision:** 4h wall-clock for the entire discovery. Rationale: 12 references × ~200 LoC each = ~2,400 LoC of pattern extraction. At Staff Engineer read-rate this is achievable in 2-3h; +1h buffer for blueprint writing.

**Alternatives rejected:** 8h → over-budget for internal-only scope; 1h → cannot resolve corner-4 questions properly.

### D3 — Zero WebFetch

**Decision:** No external URLs consulted. All citations point to `cmd/loopzero-ops/internal/quality/*` OR `.claude/rules/*` OR `cmd/loopzero-ops/quality_registry.go`.

**Rationale:** M-Migrate2 is a repo-internal pattern. External SOTA would create fabrication risk without value.

## Research Questions

Total: 8 questions across 4 corners. Max 3 per corner respected. Min 1 per corner.

| Q# | Question summary | Corner | Fase A (static method) | Fase B (LIVE probe) |
|---|---|---|---|---|
| Q1 | `check_test.go` fixture structure for allowlistsunsets | tests | Read `cmd/loopzero-ops/internal/quality/allowlistsunsets/check_test.go` | SKIP (hermetic gate — no LIVE probe needed) |
| Q2 | Canonical `check_test.go` shape across 3 gates | tests | Read + grep helpers across 3 test files | SKIP (hermetic) |
| Q3 | Beyond-stdlib imports across 12 gates | deps | grep imports across 4 gates | SKIP (hermetic) |
| Q4 | External binary requirements + pre-flight guards | deps | grep exec.Command + taskfile guards | SKIP (validated via `task quality:gates` GREEN in CI) |
| Q5 | Row format in `quality_registry.go` | tools | Read quality_registry.go by function-name | SKIP (hermetic) |
| Q6 | Taskfile wiring style | tools | grep taskfiles/quality.yml | SKIP (hermetic) |
| Q7 | 3 canonical bash test shapes → Go equivalents | techniques | Read 3 representative bash tests | SKIP (hermetic) |
| Q8 | Canonical `Outcome` struct shape | techniques | Read check.go across 4 gates | SKIP (hermetic) |

Detailed methods + expected answer shapes below.

**Q1** (corner: tests) How does `cmd/loopzero-ops/internal/quality/allowlistsunsets/check_test.go` structure its test fixtures? Does it use `t.TempDir()` + inline fixture creation, or embedded `testdata/`?

- **Method:** Read `cmd/loopzero-ops/internal/quality/allowlistsunsets/check_test.go`
- **Expected answer shape:** Fixture strategy (inline vs testdata) + fake file count + assertion style (exit-code matching vs Outcome.Failures matching)

**Q2** (corner: tests) What is the canonical shape of `check_test.go` across 3 sampled gates (`esooverlay`, `installgithooks`, `webhookcanonicalpath`)? Do they share a helper file OR duplicate setup?

- **Method:** Read the 3 test files + grep for shared helpers via `grep -l "func setup" cmd/loopzero-ops/internal/quality/*/`
- **Expected answer shape:** Presence/absence of shared helper package + per-gate boilerplate LoC

**Q3** (corner: deps) Do the 12 M-Migrate2 gates import any beyond-stdlib packages? (Assumption: only stdlib + `os/exec` for external tool invocation.)

- **Method:** `grep -h '"' cmd/loopzero-ops/internal/quality/{allowlistsunsets,esooverlay,installgithooks,webhookcanonicalpath}/*.go | grep -E "^\s*\"" | sort -u`
- **Expected answer shape:** Deduplicated import set — should be small (< 15 entries)

**Q4** (corner: deps) Does the migration pattern require any dev-dependency for helm/kubectl/jq shell-outs? (E.g., `helm template` requires helm binary on PATH.) **AND** verify each such binary has a documented pre-flight guard in `taskfiles/quality.yml` (pattern precedent: `command -v helmfile`) so Wave 2 gates cannot silently NOOP when the binary is absent from CI.

- **Method:** `grep -l "exec.Command" cmd/loopzero-ops/internal/quality/*/check.go | head -5 | xargs grep "exec.Command"` + `grep -E "command -v [a-z]+" taskfiles/quality.yml`
- **Expected answer shape:** List of external binaries invoked + which gates require them + which have pre-flight guards in taskfile (per EC-3 of edge-case review 2026-07-10)

**Q5** (corner: tools) How are the 12 gates registered in `cmd/loopzero-ops/quality_registry.go`? What is the canonical row format?

- **Method:** Read `cmd/loopzero-ops/quality_registry.go` + grep by function-name pattern (`run{PkgName}` — e.g., `runAllowlistSunsets`), NOT by package-string literal (registered strings may be kebab-case while packages are camelCase — per EC-1 of edge-case review 2026-07-10)
- **Expected answer shape:** Canonical row template `{"name", "description with M-Migrate2 date reference", runFuncName}` — count how many carry the standard "(M-Migrate2 YYYY-MM-DD replaces infra/tests/check-X_test.sh …)" description phrasing; grep must yield exactly 12 hits before answer accepted

**Q6** (corner: tools) How are the 12 gates wired into `taskfiles/quality.yml`? Is there one line per gate or a bulk-invocation?

- **Method:** `grep "loopzero-ops quality" taskfiles/quality.yml | head -30`
- **Expected answer shape:** Wiring style (per-gate line vs bulk) + inline vs docs comment on M-Migrate2 origin

**Q7** (corner: techniques) What are the 3 canonical "test shapes" bash used that Go must reproduce? (Hypothesis: syntax-check via `bash -n`, helm-template + jq/grep, and file-layout asserts.)

- **Method:** Read `infra/tests/check-sprint-syntax_test.sh` + `infra/tests/check-loki-vault-retention_test.sh` + `infra/tests/check-archive-script-repo-root_test.sh` (one representative per Wave 1's 3 subtypes)
- **Expected answer shape:** Categorization table `{ bash idiom → Go equivalent }` — e.g., `grep -q` → `strings.Contains`, `bash -n foo.sh` → `exec.Command("bash", "-n", "foo.sh").Run()`, `find -name` → `filepath.Walk`

**Q8** (corner: techniques) How do the M-Migrate2 gates handle the "assertion failure → structured Outcome" translation? What is the canonical `Outcome` struct shape AND how do they translate bash exit codes (0/1/2) into typed Go outcomes?

- **Method:** Read `cmd/loopzero-ops/internal/quality/allowlistsunsets/check.go` (find `type Outcome struct`) + verify shape identical in 3 other gates. **If 4-sample check yields divergent shapes → extend sample to ALL 12 and select modal shape; if bimodal, escalate to human via halt-loop BLOCKED report (per EC-2 of edge-case review 2026-07-10).** Also probe how the gate translates bash-side `exit 2 (parse error)` vs `exit 1 (assertion fail)` into distinct Go outcomes.
- **Expected answer shape:** Struct fields + serialization strategy + fail-fast vs collect-all-failures policy + exit-code → outcome mapping table

## Coverage Matrix

| Question | Corner | Method | Expected answer format | Status |
|---|---|---|---|---|
| Q1.1 | Integration tests | Read allowlistsunsets/check_test.go | Fixture strategy string + count | Planned |
| Q1.2 | Integration tests | Read 3 test files + grep helpers | Shared helper existence + LoC table | Planned |
| Q2.1 | Dependencies | grep imports across 4 gates | Deduplicated import list | Planned |
| Q2.2 | Dependencies | grep exec.Command | External binary requirements | Planned |
| Q3.1 | Tools | Read quality_registry.go | Canonical row format template | Planned |
| Q3.2 | Tools | grep taskfiles/quality.yml | Wiring style + comment style | Planned |
| Q4.1 | Techniques | Read 3 representative bash tests | Categorization table | Planned |
| Q4.2 | Techniques | Read 4 check.go files | Outcome struct shape | Planned |

**Coverage: 100%** — every question mapped to an executable method. Zero deferred questions.

## Halt-loop checkpoints

For `/discover-execute` autonomous iteration:

- **Checkpoint 1** — All 8 questions answered with concrete file+line citations (not just filenames)
- **Checkpoint 2** — Every citation resolves via `Path.exists()` at blueprint-write time
- **Checkpoint 3** — All 4 coverage corners populated (each has ≥ 1 question fully answered)
- **Checkpoint 4** — ≥ 1 ADR section in the blueprint (initially D1-D3 above, plus any new decisions surfaced during execute)
- **Checkpoint 5** — Blueprint contains a "Migration recipe" section codifying the canonical M-Migrate2 workflow as a numbered checklist (per goal-directive "SEM WORKAROUNDS")

Blueprint incomplete on any checkpoint → halt-loop continues; blueprint complete → emit `<promise>BLUEPRINT_COMPLETE</promise>`.

## Acceptance Criteria

- Every research question has a concrete answer with `file:line` citation
- Every citation resolves (Path.exists check at blueprint-write time)
- All 4 coverage corners have ≥ 1 answered question
- ≥ 1 ADR section preserved from this plan into the blueprint
- Blueprint stored at [`knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`](../blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md)
- Blueprint size 1,500-3,000 words (not thin, not bloated)
- Zero WebFetch invocations (per ADR D3)
- Time budget ≤ 4h wall-clock (per ADR D2)

## Global Definition of Done

Discovery is DONE when `/discover-confidence` (subsequent phase) returns verdict `SHIPPABLE_WITH_CAVEATS` or `SHIPPABLE` against the golden rule `.claude/rules/discover-blueprint-golden-rule.md`:

- No `empty_corner_*` hard cap fires
- No `fabricated_citation` hard cap fires
- ≥ 1 ADR section present
- All mandatory blueprint sections filled

## Cross-references

- Cycle rule: `.claude/rules/cycle-discover.md`
- Golden rule (blueprint): `.claude/rules/discover-blueprint-golden-rule.md`
- Downstream phase: `/discover-edge-cases infra-tests-hermetic-to-quality-gates`
- Roadmap milestone: `ROADMAP.md § M16`
- Milestone grill: [`knowledge-base/grills/infra-tests-hermetic-to-quality-gates-feature-grill.md`](../../grills/infra-tests-hermetic-to-quality-gates-feature-grill.md)
