---
slug: operator-glue-go-migration-final
milestone_id: M15
created_at: 2026-07-10
plan_version: 1.2
goal: "Reduzir a allowlist de shell scripts de 121 → 120 entries executando Wave A (delete-only 1 file: reset-for-e2e.sh per sponsor mandate 2026-06-08) em v0.61.0. Waves B/C/D promovidas a M18 via /roadmap-feature per ADR-D6 empirical LIVE-cluster boundary — Wave B strangler requires Cleanroom+UP+sponsor JUNTOS per rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL. M15 checkbox permanece [ ] pending M18 closure."
authored_by: claude-opus-4-7
---

# M15 — Operator Glue → Go: Waves A + B + Split-Decision C (final plan)

**Blueprint:** `knowledge-base/discoveries/blueprints/operator-glue-go-migration-final-blueprint.md` (SHIPPABLE 98.8)
**Discover plan:** `knowledge-base/discoveries/plans/operator-glue-go-migration-final-plan.md` (SHIPPABLE 100.0)
**ROADMAP.md M15:** literal DoD `grep -cE '^\| .*\.sh ' allowlist ≤ 30`
**Sponsor decision 2026-07-10:** Path A (Waves A+B+C + Wave D → M18); M15 [ ] stays unchecked (DoD literal not met until Wave D closes in M18).

## Goal

Reduzir a allowlist de shell scripts de **121 entries → ≤ 90 residual** delivering Waves A+B em v0.61.0 (12–17 file migrations com per-file `--verify` GREEN em Go BEFORE `git rm` bash), com Wave C promoted to phase-3 split-decision, and Wave D promoted to M18 (mirror of M16→M17 D4 split precedent). **Metric (observable):** `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 90** ao final da Phase 5.

## Context

- **Why now:** CTO mandate 2026-07-08 (elimination shell scripts / FAANG-level tooling). M13 fechou DoD#3 como *trajetória*; alvo literal do `shell-script-strategic-plan` (2026-05-22) permanece aberto. M16 fechou 10 hermetic scripts; M15 é a última onda strangler antes de M18.
- **Blueprint honest finding (Rule 3):** KEEP subtotal = 61 empírico. DoD literal `≤ 30` requer Wave D (aggressive KEEP-OPERATOR re-audit) que precisa sponsor sign-off explícito. Sponsor 2026-07-10 escolheu ADR-D4 escape hatch → Wave D → M18.
- **Precedente técnico:** M9 estabeleceu `loopzero-ops` subcommand pattern. M15-Waves-1+2+3 já shipou 20 seeds em `loopzero-ops seed`. M16 shipou 10 quality gates com mesmo pattern.
- **Scope amendment vs blueprint recommendation:** Blueprint recomendou A+B+C em M15. Sponsor escolheu Path A com Wave C promoted to phase-3 split-decision (velocity-gated post-Wave-B). Pragmática: Wave C é ~30 files REVIEW-NEEDED que exigem per-file audit; ship what's proven, defer what needs re-audit.

## Baseline Context

### Files that will be touched (Waves A + B)

| Path | LoC | Last commit | Wave | Migration target |
|---|---|---|---|---|
| `infra/scripts/local/reset-for-e2e.sh` | ~170 | `f5b8c0d1` (2026-06-08) | A | DELETE (sponsor 2026-06-08 verbatim) |
| `infra/helmfile/scripts/bootstrap-patch-admission-policies.sh` | 41 | 2026-05-22 baseline | B | helmfile hook (post-sync) |
| `infra/helmfile/scripts/bootstrap-patch-sigstore-policy-controller.sh` | 36 | 2026-05-22 baseline | B | helmfile hook (post-sync) |
| `infra/helmfile/scripts/bootstrap-patch-argocd-gitea-creds.sh` | 45 | 2026-05-22 baseline | B | helmfile hook |
| `infra/helmfile/scripts/bootstrap-patch-argocd-repo-server-restart.sh` | 29 | 2026-05-22 baseline | B | helmfile hook |
| `infra/helmfile/scripts/create-wiring-configmap.sh` | 85 | 2026-05-22 baseline | B | helmfile hook |
| `infra/helmfile/scripts/generate-build-secrets.sh` | 120 | 2026-05-22 baseline | B | `loopzero-ops seed build-secrets` (Go) |
| `infra/helmfile/scripts/generate-runtime-secrets.sh` | 140 | 2026-05-22 baseline | B | `loopzero-ops seed runtime-secrets` (Go) |
| `infra/helmfile/scripts/generate-secrets.sh` | 370 | 2026-05-22 baseline | B | `loopzero-ops seed platform-secrets` (Go) |
| `infra/tests/dogfood/repro-findings.sh` | 302 | 2026-05-22 baseline | B | Ginkgo integration test (`infra/tests/e2e/dogfood_repro_test.go`) |
| `infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh` | 111 | 2026-05-22 baseline | B | Ginkgo integration helper |
| `infra/helmfile/scripts/lib/pulumi-output.sh` | 127 | 2026-05-22 baseline | B | Go Pulumi SDK caller (`pkg/pulumi/output.go`) |
| `.claude/rules/shell-scripts-allowlist.md` | 700+ | recent | A/B | Remove entries per lot (§ Removed) + CHANGELOG entries |
| `cmd/loopzero-ops/seed_registry.go` | ~200 | recent (M15-W3) | B | Register 3 new seed subcommands + wire runners |
| `taskfiles/quality.yml` | recent | recent | A/B | Remove `bash <deleted-path>` invocations |
| `CHANGELOG.md` | recent | recent | A/B | `[Unreleased] § Removed` + `§ Added` per lot |

Total Wave A: 1 file DELETE. Total Wave B: 12 file migrations (6 helmfile hooks + 3 Go seed subcommands + 2 Ginkgo helpers + 1 Go SDK caller) + registry updates + task refs + CHANGELOG.

### Current callers / dependents

- `reset-for-e2e.sh`: 3 refs in `.claude/skills/` docs (memories), 0 in `taskfiles/`, 0 in seed-post-cleanroom-all.sh chain. Sweep gate.
- `bootstrap-patch-*.sh` (4 files): called by `taskfiles/k3d.yml § dev-public:bootstrap` targets — swap for `helmfile sync` hooks.
- `create-wiring-configmap.sh`: called by 1 hermetic seed test. Migrate seed test to consume the helmfile-hook contract.
- `generate-*secrets*.sh` (3 files): called by helmfile via `hooks[].command` today. Migrate to `loopzero-ops seed <name>` invocation.
- `pulumi-output.sh`: called by 5 other bash scripts (all lib callers). Replace lib source with Go SDK call in the callers OR wrap in a thin bash shim in first iteration.

### Architecture boundaries affected

- **DIP compliance:** New Go seeds live in `cmd/loopzero-ops/internal/seed/<name>/` following M15-W3 canonical shape. Registry pattern already established at `cmd/loopzero-ops/seed_registry.go`. Zero new interfaces required (per ADR-D5 no new third-party deps).
- **Composition root:** `cmd/loopzero-ops/main.go` remains the wire-up point. New subcommands register via `seed_registry.go` init.
- **Layering:** helmfile hooks are declarative-YAML consumers of Go seeds; they call `loopzero-ops seed <name> --verify` in `hook.command` list. No Go imports helmfile; helmfile calls Go via CLI.

### Domain glossary

- **Strangler-fig:** Deploy Go seed with `--verify` LIVE green in parallel with existing bash; `git rm` bash only after 1 clean run. Same pattern as M-Migrate2 established in `shell-script-strategic-plan` v1.3.
- **DELEGATE-HELMFILE:** Move script logic into `helmfile.yaml.gotmpl § hooks` OR `helm chart § post-install` template; script disappears.
- **MIGRATE-GENERATOR:** Move script logic into a `//go:generate` directive OR a `loopzero-ops seed <name>` command that outputs the same Kubernetes Secret/ConfigMap manifest.
- **MIGRATE-GINKGO:** Move bash test scaffolding into a Ginkgo suite under `infra/tests/e2e/`.
- **MIGRATE-GO-SDK:** Replace `pulumi stack output` subprocess call with `pulumi/sdk-v3` Go package direct call.

## Prior Art & Related Work

- **Blueprint (SoT):** `knowledge-base/discoveries/blueprints/operator-glue-go-migration-final-blueprint.md` v1.0 (Q1-Q8 answered, 121 entries classified, 5 ADRs, SHIPPABLE 98.8).
- **M9 precedent:** `loopzero-ops quality <gate>` subcommand pattern (2026-05-23) — 40+ pure-logic gates shipped.
- **M13 precedent:** `no-new-bash` gate invertido (Phase-5 ledger) + Phase-5 sponsor ratification of KEEP-OPERATOR entries (2026-05-23).
- **M15 Waves 1+2+3 precedent (shipped in v0.59.0):** 20 seeds migrated to `loopzero-ops seed` pattern. Canonical Go shape = stdlib `flag` + `context` + `pgx/v5` OR `client-go` + `yaml.v3`. Zero third-party additions.
- **M16 precedent (shipped v0.60.0 2026-07-10):** 10 hermetic gates migrated via same strangler pattern. ADR-D4 velocity-split (Wave 3 → M17) already exercised — this plan applies the same D4 to Wave D → M18.
- **Shell-script-strategic-plan v1.3 (2026-05-22):** established the M-Migrate2 pattern (D3 no-new-bash + D4 KEEP criteria + Anti-Pattern #8 gh/npm/go wrapping legitimacy).

## Drawbacks & Risks

| # | Risk | Severity | Mitigation | Owner |
|---|---|---|---|---|
| R1 | Wave C velocity uncertainty — 22 REVIEW-NEEDED entries in `infra/helmfile/scripts/*` require per-file audit; empirical M15-W1+W2+W3 velocity was ~5 files/session for well-classified targets, REVIEW-NEEDED needs classification pass first | MEDIUM | Phase 4 split-decision gate (ADR-D3) — if audit shows > 8h remaining post-Wave-B, promote Wave C to M15b (new plan) or extend to M18 alongside Wave D | Claude at Phase 4 boundary |
| R2 | helmfile hook migration may lose bash idempotency — bootstrap-patch-*.sh scripts have subtle idempotency (existing-secret detect, restart-only-if-changed); naive port to `helmfile hook.command` risks losing that | MEDIUM | Per-file capture bash `--verify` output first, then port, then compare Go/hook `--verify` output byte-diff (± timestamp); only `git rm` on byte-identical or documented-diff | Claude in Wave B execution |
| R3 | Ginkgo migration for infra/tests/dogfood/*.sh may block on cluster-state deps — repro-findings.sh + cleanup-terminating-namespaces.sh depend on cluster state; Ginkgo migration must consume `BeforeSuite` fixture | LOW | Ginkgo suite skips gracefully if no cluster; runs full when `LOOPZERO_E2E_LIVE=1` | Claude Wave B task T3.4 |
| R4 | CHANGELOG § Removed classification — DoD#5 requires per-lot CHANGELOG entries; Wave B migrations map 1:1 (`§ Removed <path>` + `§ Added loopzero-ops seed <name>` OR `§ Changed helmfile hook <name>`) | LOW | Discipline gate in per-task DoD; T5.2 audit sweep verifies all entries present | Claude per commit |
| R5 | Wave D → M18 delay leaves DoD literal `≤ 30` unclosed on M15 checkbox — sponsor 2026-07-10 accepted this via Path A escape hatch, but external observers may misread M15 `[ ]` as incomplete | LOW | Explicit doc handoff in T5.2 + M18 `/roadmap-feature` invocation post-release; ROADMAP.md M15 entry references v0.61.0 partial delivery + M18 dependency | Claude at T5.2 |

## Unresolved Questions

- Q1 — Wave C phase-3 split-decision hard boundary between "extend session" vs "commit at Waves A+B and open M15b for Wave C" — decidido empiricamente pós-Wave-B baseline capture (Phase 4 split-decision gate).
- Q2 — `pulumi-output.sh` (MIGRATE-GO-SDK) has 5 downstream bash lib callers — porta-se todos in-lot ou shim first. Recomendação in-plan resolved as **shim first** (bash calls Go), `git rm` original + refactor callers in a follow-up. Documented in ADR-D5.
- Q3 — Empirical `pulumi/sdk-v3` presence in `go.mod` needs verification at T3.5 GREEN phase before commit (blueprint Q3 asserted no new deps needed but not verified in situ).

## ADRs

### D1 — Aplicar Path A blueprint escape hatch (Waves A+B in v0.61.0; Wave C split-decision; Wave D → M18)

**Status:** Accepted (sponsor 2026-07-10).

**Context:** Blueprint recomendou Waves A+B+C em M15. Empirical: KEEP subtotal 61 > 30 mesmo pós-A+B+C. Wave D exige sponsor re-authorization do Phase-5 ledger. Sponsor 2026-07-10 escolheu ADR-D4 escape hatch.

**Decision:** M15 v0.61.0 delivers Waves A+B (12–17 files, achievable). Phase 4 boundary gates Wave C via empirical velocity. Wave D → M18 via `/roadmap-feature` post-release. M15 checkbox `[ ]` stays unchecked until Wave D closes in M18.

**Alternatives rejected:**
- Autorizar Wave D agora (revoga Phase-5 sponsor ratification, risco alto).
- Waves A+B only + flip M15 [x] (violates Rule 3 — DoD literal ≤ 30 não met).
- Revisar M15 DoD para `≤ 80` (evita M18 promotion, mas rewrite DoD mid-flight fere M15 spec integrity).

### D2 — Strangler per-entry: `--verify` GREEN em Go BEFORE `git rm` bash (canonical M-Migrate2)

**Status:** Accepted.

**Context:** M15-W1+W2+W3 shipou 20 seeds Go paralelamente ao bash. M16 shipou 10 gates com mesmo pattern. Both used strangler-fig. Bath (2004) + Google SRE ch. 25 canonical.

**Decision:** Cada file Wave B: (a) implement Go equivalent, (b) run Go `--verify` LIVE contra dev-public, (c) capture output vs bash `--verify` output, (d) only if byte-identical (± documented tolerated diff) → `git rm` bash + update refs + update allowlist § Removed row + CHANGELOG lot entry.

**Alternatives rejected:**
- Big-bang deletion (precedent 2026-05-23 failed).
- Feature flag toggle Go/bash (adiciona complexidade sem benefit — YAGNI).

### D3 — Wave C velocity split-decision gate at Phase 4 boundary

**Status:** Accepted.

**Context:** Wave C = ~22 REVIEW-NEEDED files + ~5 test_helpers. Per-file audit + classify + migrate. Empirical velocity gate empirical.

**Decision:** After Phase 3 (Wave B close), capture 3 metrics: (a) total wall-clock elapsed, (b) unresolved Wave C classification questions, (c) reference-sweep gap count. If (a) < 4h AND (b) < 5 files AND (c) = 0 → proceed Wave C. Else emit `M15 Wave C SPLIT` note + escalate to `/roadmap-feature` for M15b.

**Alternatives rejected:**
- Force Wave C completion (risk M15 non-close).
- Skip split gate (loses ADR-D4 mirror of M16 D4).

### D4 — Wave D → M18 promotion authorized (mirror of M16 → M17 precedent)

**Status:** Accepted (sponsor 2026-07-10).

**Context:** Wave D = 30+ KEEP-OPERATOR re-audit files. Requires sponsor sign-off (Phase-5 ledger ratification re-visit). M16 shipped Wave 3 → M17 D4 split 2026-07-10 — proven precedent same day.

**Decision:** Post-release, `/roadmap-feature operator-glue-go-migration-wave-d --dependency M15` creates M18 with Wave D scope + explicit sponsor re-authorization for Phase-5 ledger delta.

**Alternatives rejected:**
- Bundle Wave D into M15 (blueprint honest finding: needs sponsor re-authorization).
- Drop Wave D (leaves DoD literal ≤ 30 unclosable, violates roadmap spec integrity).

### D6 — Empirical LIVE-cluster boundary: Waves B/C/D promoted to M18 (v1.2 amendment)

**Status:** Accepted (empirical session 2026-07-10 gate).

**Context:** ADR-D2 (strangler per-entry: `--verify` GREEN em Go LIVE BEFORE `git rm` bash) collides with `rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL: LIVE validation requires Cleanroom TOTAL + UP + sponsor JUNTOS presencial. Empirical session 2026-07-10 has no LIVE cluster + no sponsor JUNTOS. Wave B's 12 targets all require LIVE parity proof to satisfy ADR-D2.

**Decision:** M15 v0.61.0 delivers Wave A only (1 file: `reset-for-e2e.sh` per sponsor 2026-06-08 mandate — this delete does NOT require LIVE because the file's replacement is the existing Cleanroom chain per rules/cleanroom-up-live-test-mandate.md, not new Go code). Waves B + C + D promoted to M18 (single milestone) via `/roadmap-feature operator-glue-go-migration-remainder --dependency M15` post-release. M18 milestone scope = 11 helmfile hooks + 3 Go seeds + 2 Ginkgo suites + 1 Pulumi shim + REVIEW-NEEDED cluster (~30 files) + aggressive KEEP-OPERATOR re-audit (~30 files).

**Alternatives rejected:**
- Ship Wave B hermetic-only + defer LIVE `git rm` to next session — violates ADR-D2 (strangler in-flight state is anti-pattern per M-Migrate2 v1.3).
- Skip ADR-D2 for Wave B — violates the strangler pattern that M-Migrate2 v1.3 canonicalized post-2026-05-23 big-bang failure.
- Force LIVE in this session — no sponsor JUNTOS, no cluster access, violates cleanroom-up-live-test-mandate.md INQUEBRÁVEL.

### D5 — Shim-first for pulumi-output.sh MIGRATE-GO-SDK (5 downstream bash callers)

**Status:** Accepted.

**Context:** `lib/pulumi-output.sh` sourced by 5 other bash scripts (not deleted this milestone). Direct delete breaks 5 callers.

**Decision:** Wave B replaces `pulumi-output.sh` internals with Go call via `loopzero-ops util pulumi-output`. Bash file becomes 5-line shim `loopzero-ops util pulumi-output "$@"`. `git rm` waits until 5 callers refactored (M18 Wave D scope OR follow-on).

**Alternatives rejected:**
- Delete pulumi-output.sh + refactor 5 callers in same lot (scope creep — 5 unrelated migrations).
- Skip shim (breaks 5 callers on delete).

## Dependency Graph

```
Phase 1 (Baseline capture + orphan sweep) — depends on: [none, blueprint approved]
    ↓
Phase 2 (Wave A: delete-only) — depends on: Phase 1 sweep complete
    ↓
Phase 3 (Wave B: 12 migrations, strangler per-entry) — depends on: Phase 2 clean
    ↓
Phase 4 (Wave C velocity split-decision) — depends on: Phase 3 close, DoD partial
    ↓
Phase 5 (Integration validation) — depends on: Phase 4 decided (proceed or split)
```

## Phase 1 — Baseline capture + orphan bash sweep

### T1.1 — Capture pre-migration allowlist baseline

**Files to edit:** `knowledge-base/audits/M15-pre-migration-baseline-2026-07-10.md` (NEW)

**Deep file dependency analysis:** `.claude/rules/shell-scripts-allowlist.md` (read-only inventory). No downstream callers of the baseline audit — audit trail only.

**Why this step:** Post-migration DoD verification requires the pre-state anchor. Without baseline count, we cannot prove "121 → ≤ 90" delta empirically. Steady-state-baseline mandate 2026-06-06.

**#### TDD**
- **RED**: `test_baseline_captured_before_wave_a` (hermetic) — assert `knowledge-base/audits/M15-pre-migration-baseline-2026-07-10.md` exists AND contains a line matching `entries_count:\s*\d+`.
- **GREEN**: write audit file with `grep -c` output + wc -l summary + 20-file random sample check.
- **REFACTOR**: N/A (audit doc).

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:** `test -f knowledge-base/audits/M15-pre-migration-baseline-2026-07-10.md && grep -q 'entries_count' $_`.

**DoD:** File committed as `docs(audit): M15 pre-migration baseline`.

### T1.2 — Orphan bash sweep (already-shipped seeds without deleted .sh)

**Files to edit:** `knowledge-base/audits/M15-orphan-bash-sweep-2026-07-10.md` (NEW)

**Deep file dependency analysis:** Cross-reference `cmd/loopzero-ops/seed_registry.go` shipped seeds (20 entries) against `infra/scripts/local/seed-*.sh` files that still exist. Identify orphans (Go seed shipped + bash NOT deleted) → Wave A candidates.

**Why this step:** M15 W1+W2+W3 might have shipped Go seeds without deleting bash counterparts (Rule 3 honesty gap discovery). Sweep produces exact Wave A file list.

**#### TDD**
- **RED**: `test_orphan_sweep_captured` — assert audit file contains section `## Orphans identified` with at least the header (may be empty list if no orphans).
- **GREEN**: Run `for seed in $(ls cmd/loopzero-ops/internal/seed/); do test -f infra/scripts/local/seed-${seed}-*.sh && echo ORPHAN: $seed; done` capture output → audit.
- **REFACTOR**: N/A.

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:** File committed; if any orphans found, added to Wave A candidate list at T2.1.

**DoD:** `docs(audit): M15 orphan bash sweep`.

## Phase 2 — Wave A (delete-only, 5 files max)

### T2.1 — Delete `infra/scripts/local/reset-for-e2e.sh` (sponsor 2026-06-08)

**Files to edit:** `infra/scripts/local/reset-for-e2e.sh` (DELETE), `.claude/rules/shell-scripts-allowlist.md` (remove row + add § Removed entry), `CHANGELOG.md` (`[Unreleased] § Removed`), sweep-callers audit.

**Deep file dependency analysis:** 3 refs in `.claude/skills/` memory files (docs, not callable), 0 in `taskfiles/`, 0 in seed-post-cleanroom-all.sh chain. Sponsor 2026-06-08 verbatim mandate "TEMPORARY-DELETE-AFTER umbrella milestone PASS". Umbrella umbrella shipped (v0.60.0).

**Why this step:** Sponsor mandate explicit — file marked TEMPORARY-DELETE, umbrella milestone completion condition met (v0.60.0 shipped 2026-07-10). Rule 3 requires acting on sponsor mandate.

**#### TDD**
- **RED**: `test_reset_for_e2e_deleted` (hermetic) — assert `! test -f infra/scripts/local/reset-for-e2e.sh`.
- **GREEN**: `git rm infra/scripts/local/reset-for-e2e.sh` + remove allowlist row + CHANGELOG entry.
- **REFACTOR**: grep sweep confirms 0 hits in `taskfiles/`, `docs/` (excluding archive/memory).

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:**
- File not present.
- `grep -R 'reset-for-e2e.sh' taskfiles/ docs/ --exclude-dir=archive` returns 0.
- Allowlist entry gone.
- CHANGELOG `[Unreleased] § Removed` has entry.

**DoD:** `chore(cleanup): delete reset-for-e2e.sh per sponsor mandate 2026-06-08 (M15 Wave A)`.

### T2.2 — Delete orphan seed .sh (per T1.2 sweep output)

**Files to edit:** For each orphan identified in T1.2: delete .sh + allowlist row + CHANGELOG § Removed entry.

**Deep file dependency analysis:** Per T1.2 output. Each orphan has a shipped Go replacement in `cmd/loopzero-ops/internal/seed/<name>/`; the shipped seed is the caller substitute.

**Why this step:** Rule 3 honesty — shipped Go replacement + surviving bash = duplication. Delete duplicate closes M-Migrate2 loop for W1+W2+W3.

**#### TDD**
- **RED**: For each orphan: `test_orphan_<name>_bash_deleted` — assert `! test -f <path>`.
- **GREEN**: `git rm <path>` per orphan.
- **REFACTOR**: Update allowlist rows + CHANGELOG per lot.

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:** For each orphan: bash gone, `loopzero-ops seed <name> --verify` GREEN LIVE.

**DoD:** `chore(cleanup): delete orphan bash for shipped seed-<name> (M15 Wave A)` per file.

## Phase 3 — Wave B (12 mechanical migrations, strangler per-entry)

### T3.1 — DELEGATE-HELMFILE: 4 `bootstrap-patch-*.sh` → helmfile hooks

**Files to edit:**
- `infra/helmfile/scripts/bootstrap-patch-admission-policies.sh` (DELETE post-verify)
- `infra/helmfile/scripts/bootstrap-patch-sigstore-policy-controller.sh` (DELETE post-verify)
- `infra/helmfile/scripts/bootstrap-patch-argocd-gitea-creds.sh` (DELETE post-verify)
- `infra/helmfile/scripts/bootstrap-patch-argocd-repo-server-restart.sh` (DELETE post-verify)
- `infra/helmfile/<env>.yaml.gotmpl` (add `hooks[].command` per release)
- `.claude/rules/shell-scripts-allowlist.md` (remove 4 rows + § Removed entries)
- `CHANGELOG.md` (lot entry)

**Deep file dependency analysis:** Each bootstrap-patch-*.sh is called from `taskfiles/k3d.yml § dev-public:bootstrap` OR helmfile `hooks[].command`. Move logic into `helmfile.yaml.gotmpl § hooks: [postsync]` block per release. Bash equivalent: single-shot `kubectl patch` invocation.

**Why this step:** ADR-D2 strangler. helmfile is declarative-native for post-sync patches. Removing 4 bash wrappers with equivalent inline `hooks[].command` is a mechanical port.

**#### TDD**
- **RED**: For each: `test_patch_<name>_via_hook_applied` (LIVE against dev-public post-Cleanroom) — apply hook, assert patched resource state matches bash `--verify` output byte-identical.
- **GREEN**: Port `kubectl patch` invocation into `hooks[].command` inline; test LIVE.
- **REFACTOR**: `git rm` bash + update caller in `taskfiles/k3d.yml` + allowlist row + CHANGELOG.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

- **helmfile hook fails mid-execution:** `hooks[].command` returns non-zero → helmfile reports error, sync aborts. Test: inject invalid patch, assert helmfile exits non-zero with clear stderr. Expected behavior: fail-fast per rules/error-handling.md.
- **kubectl unreachable (cluster down):** hook command fails immediately. Test: run against non-existent context, assert exit non-zero + clear error. Expected: fail-clear.
- **Patched resource missing (target CR not created yet):** hook failed pre-condition. Test: run before dependency release syncs, assert helmfile respects `needs:` ordering. Expected: helmfile dependency graph gates this.

**Acceptance criteria:**
- 4 bash files deleted.
- 4 corresponding `hooks[].command` present in `infra/helmfile/*.yaml.gotmpl`.
- LIVE `helmfile sync --selector <release>` applies patches equivalent to prior bash `--verify` output byte-identical (± timestamp).
- Allowlist 4 rows removed; CHANGELOG lot entry `[Unreleased] § Changed: bootstrap-patch-*.sh → helmfile hooks (M15 Wave B)`.

**DoD:** `feat(helmfile): migrate 4 bootstrap-patch-*.sh to inline hooks (M15 Wave B DELEGATE-HELMFILE)`.

### T3.2 — DELEGATE-HELMFILE: `create-wiring-configmap.sh` + `bootstrap-oauth-clients.sh` (2 files)

**Files to edit:** Same shape as T3.1 (DELETE, add hooks, update caller, allowlist, CHANGELOG).

**Deep file dependency analysis:** `create-wiring-configmap.sh` creates a ConfigMap from Pulumi outputs — declarative equivalent is a helm template + values-from-secret. `bootstrap-oauth-clients.sh` is a stateful OAuth client creation flow — helmfile hook + hydra CLI wrapper.

**Why this step:** Same rationale as T3.1 — declarative helmfile-native replaces bash imperative.

**#### TDD:** identical shape to T3.1 (per-file --verify byte-identical gate).

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:** 2 bash deleted; 2 hooks/templates added; LIVE parity.

**DoD:** `feat(helmfile): migrate create-wiring-configmap.sh + bootstrap-oauth-clients.sh to hooks (M15 Wave B)`.

### T3.3 — MIGRATE-GENERATOR: 3 `generate-*secrets*.sh` → `loopzero-ops seed <name>` (Go)

**Files to edit:**
- `cmd/loopzero-ops/internal/seed/buildsecrets/seed.go` (NEW ~200 LoC)
- `cmd/loopzero-ops/internal/seed/runtimesecrets/seed.go` (NEW ~220 LoC)
- `cmd/loopzero-ops/internal/seed/platformsecrets/seed.go` (NEW ~450 LoC)
- `cmd/loopzero-ops/seed_registry.go` (register 3 subcommands)
- `infra/helmfile/scripts/generate-build-secrets.sh` (DELETE post-verify)
- `infra/helmfile/scripts/generate-runtime-secrets.sh` (DELETE post-verify)
- `infra/helmfile/scripts/generate-secrets.sh` (DELETE post-verify)
- `taskfiles/dev.yml` (swap `bash generate-*secrets*.sh` for `loopzero-ops seed <name>`)
- Allowlist rows (3) + CHANGELOG lot entry.

**Deep file dependency analysis:** All 3 bash scripts produce Kubernetes Secrets from Pulumi outputs (build tokens, runtime credentials, platform secrets). Go equivalent uses `pgx/v5` for PG lookups (already in dep envelope per ADR-D5) + `client-go` for K8s Apply. Canonical shape matches M15-W3 shipped seeds.

**Why this step:** M15-W3 already established canonical Go seed pattern (see 20 shipped seeds). Migrating remaining 3 generator scripts closes the MIGRATE-GENERATOR category (Q7 count = 3).

**#### TDD**
- **RED**: For each: `test_seed_<name>_verify_matches_bash` — LIVE against dev-public post-Cleanroom, capture Go `--verify` output, diff against bash `--verify` output. Assert byte-identical.
- **GREEN**: Implement 3 Go seed subcommands per canonical shape; register.
- **REFACTOR**: `git rm` 3 bash files; update taskfile callers; allowlist rows; CHANGELOG.

#### Concurrency tests

- `test_seed_<name>_serial_kubernetes_apply_race_safe` — Two goroutines invoke `Run(ctx, cfg)` simultaneously. Assert `client.Patch` + `resourceVersion` optimistic-concurrency gates the race; second goroutine either succeeds or retries with typed error. **Race detector** required (`go test -race ./...`).

#### Failure scenarios

- **Pulumi output unreachable (network down):** Go seed `pulumi stack output` fails → wrap in typed `LoopZeroError`. Test: mock `os/exec` failure, assert error surfaced + no partial write to K8s.
- **PG connection reset mid-transaction:** `pgx/v5` returns `PgError` → typed retry with backoff (max 3). Test: inject connection close, assert retry OR fail-clear.
- **K8s API server 5xx during Apply:** `client-go` returns transient error → retry OR bubble up. Test: fake client returns 503, assert typed error + no silent swallow.
- **--verify LIVE fails (secret contents don't match Pulumi output):** Go seed reports mismatch + exits non-zero with clear diagnostic. Test: mutate a secret post-apply, run --verify, assert exit non-zero.

**Acceptance criteria:**
- 3 Go seeds shipped, registered.
- LIVE `loopzero-ops seed <name> --verify` GREEN against dev-public.
- Byte-identical parity with bash `--verify` output (± timestamp).
- 3 bash deleted; taskfile callers updated; allowlist rows removed; CHANGELOG lot entry `[Unreleased] § Added: loopzero-ops seed {build,runtime,platform}-secrets (M15 Wave B MIGRATE-GENERATOR)` + `§ Removed: 3 generate-*secrets*.sh`.

**DoD:** `feat(seed): migrate 3 generate-*secrets* to loopzero-ops seed (M15 Wave B MIGRATE-GENERATOR)`.

### T3.4 — MIGRATE-GINKGO: 2 `infra/tests/dogfood/*.sh` → Ginkgo integration

**Files to edit:**
- `infra/tests/e2e/dogfood_repro_test.go` (NEW ~250 LoC Ginkgo suite)
- `infra/tests/e2e/dogfood_cleanup_helper.go` (NEW ~120 LoC)
- `infra/tests/dogfood/repro-findings.sh` (DELETE)
- `infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh` (DELETE)
- Allowlist rows + CHANGELOG.

**Deep file dependency analysis:** Both scripts orchestrate LIVE dev-public state cleanup + repro. Ginkgo integration test using existing `BeforeSuite` fixture pattern (see `infra/tests/e2e/deploy_suite_test.go`).

**Why this step:** MIGRATE-GINKGO category (Q7 count = 2). Canonical migration — bash test → Ginkgo Go test. Removes 2 allowlist entries.

**#### TDD**
- **RED**: `TestDogfoodReproFindings` skeleton fails without cluster (expected in hermetic CI).
- **GREEN**: Implement Ginkgo suite replicating bash steps 1:1. Run LIVE against dev-public post-Cleanroom.
- **REFACTOR**: `git rm` 2 bash + allowlist + CHANGELOG.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

- **Cluster unreachable during BeforeSuite:** Ginkgo skips suite with typed message. Test: assert `LOOPZERO_E2E_LIVE=0` cleanly skips.
- **Namespace cleanup times out (finalizer stuck):** helper reports timeout + surface stuck resource. Test: assert timeout produces clear diagnostic per rules/error-handling.md.

**Acceptance criteria:**
- Ginkgo suite compiles + runs LIVE parity to bash `--verify`.
- 2 bash deleted; allowlist rows removed; CHANGELOG entry.

**DoD:** `test(e2e): migrate 2 dogfood scripts to Ginkgo (M15 Wave B MIGRATE-GINKGO)`.

### T3.5 — MIGRATE-GO-SDK: `pulumi-output.sh` → shim + Go SDK internals (ADR-D5)

**Files to edit:**
- `cmd/loopzero-ops/util/pulumi_output.go` (NEW ~180 LoC — direct Pulumi Go SDK call)
- `cmd/loopzero-ops/main.go` (register `loopzero-ops util pulumi-output` subcommand)
- `infra/helmfile/scripts/lib/pulumi-output.sh` (REDUCE to 5-line shim: `exec loopzero-ops util pulumi-output "$@"`)
- Allowlist row updated (Justificativa → "SHIM — pending Wave D refactor of 5 callers to direct Go") + CHANGELOG entry.

**Deep file dependency analysis:** 5 bash lib callers source this file today. Direct delete breaks 5 callers. Shim preserves callers; internals port to Go SDK. Callers refactored to direct-Go in M18 Wave D scope.

**Why this step:** ADR-D5 — shim-first avoids scope-creep of refactoring 5 unrelated bash files in M15.

**#### TDD**
- **RED**: `test_pulumi_output_go_parity` — LIVE parity: `bash lib/pulumi-output.sh <stack>` (via shim → Go) output equals prior bash implementation output byte-identical for 3 canonical stacks.
- **GREEN**: Implement Go SDK call using `pulumi/sdk-v3` (already in `go.mod` — verify).
- **REFACTOR**: Reduce bash to 5-line shim.

**Verification (before GREEN):** Confirm `pulumi/sdk-v3` is in `loopzero/go.mod` or in `loopzero-cloud`'s go.mod. If not, ADR-D5 needs revision (per ADR-D5 no new deps). Blueprint Q3 confirmed no new deps needed — validate against real go.mod.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

- **Pulumi backend unreachable:** Go SDK returns typed error → bubble up as `LoopZeroError`. Test: mock unreachable backend, assert clear error.
- **Stack does not exist:** Go SDK returns `StackNotFound` → typed error + exit code 1. Test: query non-existent stack, assert error message.

**Acceptance criteria:**
- Go SDK caller shipped.
- Shim bash reduced to 5 lines.
- 5 downstream bash callers UNCHANGED (verified via `git diff <caller-paths>` = empty).
- Allowlist Justificativa updated to reflect SHIM status.
- CHANGELOG entry.

**DoD:** `feat(loopzero-ops): replace pulumi-output.sh internals with Go SDK (shim-first, M15 Wave B MIGRATE-GO-SDK)`.

## Phase 4 — Wave C velocity split-decision (ADR-D3)

### T4.1 — Post-Wave-B empirical baseline capture

**Files to edit:** `knowledge-base/audits/M15-wave-b-close-2026-07-XX.md` (NEW)

**Deep file dependency analysis:** Read allowlist post-Wave-B (grep -c), compare to pre-migration baseline (T1.1). Capture wall-clock elapsed from Phase 1 start. Compute residual + velocity.

**Why this step:** ADR-D3 gates Wave C decision on empirical metrics. Without baseline, decision is guess.

**#### TDD**
- **RED**: `test_wave_b_baseline_captured` — assert file exists with 3 sections.
- **GREEN**: Write audit with metric table.
- **REFACTOR**: N/A.

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:**
- Audit file has: (a) allowlist_count_delta, (b) wall_clock_elapsed_hours, (c) unresolved_wave_c_review_items_count.

**DoD:** `docs(audit): M15 Wave B close baseline (Phase 4 pre-gate)`.

### T4.2 — Split-decision execution (ADR-D3 criteria applied)

**Files to edit:** `knowledge-base/audits/M15-wave-c-split-decision-2026-07-XX.md` (NEW) — decision doc with verdict.

**Deep file dependency analysis:** Reads T4.1 baseline. Applies criteria: (a) wall_clock_elapsed < 4h, (b) unresolved_review_items < 5, (c) reference_sweep_gaps = 0.

**Why this step:** ADR-D3 codifies the decision protocol. Explicit gate here prevents scope creep OR premature stop.

**#### TDD**
- **RED**: `test_split_decision_documented` — assert decision file has `verdict: PROCEED_WAVE_C | SPLIT_TO_M15B | SPLIT_TO_M18` line.
- **GREEN**: Read T4.1 baseline, apply criteria, write verdict.
- **REFACTOR**: N/A.

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:**
- Decision file has explicit verdict.
- If PROCEED: Phase 5 becomes Wave C tasks (dynamically appended).
- If SPLIT: Phase 5 becomes Integration Validation only; Wave C promoted via `/roadmap-feature` in follow-up session.

**DoD:** `docs(audit): M15 Wave C split-decision (Phase 4 gate)`.

## Phase 5 — Integration Validation

### T5.1 — `task quality:gates` GREEN + `go test ./...` GREEN + grep sweep

**Files to edit:** (validation only — no source edits expected)

**Deep file dependency analysis:** Run `task quality:gates` (invokes 40+ gates including `no-new-bash`, `dangling-refs`). Run `go test ./cmd/loopzero-ops/... ./infra/tests/...`. Run `grep -R '<deleted-path>' taskfiles/ docs/ .claude/skills/` for each Wave A + Wave B deleted script.

**Why this step:** DoD#4 mandate. Zero references quebradas.

**#### TDD**
- **RED**: (validation phase — tests already exist per T2-T3).
- **GREEN**: Run all validation commands, capture output.
- **REFACTOR**: If any FAIL: return to responsible task, fix, re-run.

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:**
- `task quality:gates` exits 0.
- `go test ./...` exits 0.
- `grep -R <deleted-path>` for each deleted path returns 0 hits (excluding archive/memory).
- Allowlist count matches T4.1 baseline delta.

**DoD:** `test: M15 Phase 5 integration validation GREEN (Waves A+B closed)`.

### T5.2 — CHANGELOG audit + ROADMAP.md read (do NOT flip M15 checkbox)

**Files to edit:** `CHANGELOG.md` (audit only — entries added per task in Waves A+B), `ROADMAP.md` (read-only — verify DoD not met, checkbox stays `[ ]`).

**Deep file dependency analysis:** DoD#5 requires per-lot CHANGELOG entries. Verify all 17ish removals + 3ish additions are present. `grep -cE '^\| .*\.sh ' allowlist` must be > 30 (else M15 DoD literal met and flip is warranted; expected value ~90).

**Why this step:** Rule 6 CHANGELOG discipline. Rule 3 honesty — M15 `[ ]` stays unchecked because DoD literal not met (per sponsor 2026-07-10 Path A choice).

**#### TDD**
- **RED**: `test_changelog_removals_count` — assert `[Unreleased] § Removed` has N lines matching deleted file paths.
- **GREEN**: Audit + fix any missing entries.
- **REFACTOR**: N/A.

#### Concurrency tests

(none — single-threaded)

**Acceptance criteria:**
- All Wave A + Wave B deletions have `[Unreleased] § Removed` entries.
- ROADMAP M15 checkbox is `[ ]` (unchanged).
- Post-release: `/roadmap-feature operator-glue-go-migration-wave-d --dependency M15` runs (Phase 6 handoff).

**DoD:** `docs(changelog): M15 Wave A+B closure audit (checkbox stays [ ] per ADR-D4)`.

## Coverage Matrix

| # | Gap / Goal claim | Source | Task(s) |
|---|---|---|---|
| G1 | Reduce allowlist count 121 → ≤ 90 residual | ROADMAP.md M15 DoD#1 (literal `≤ 30`, partial delivery ≤ 90 per ADR-D1 Path A) | T2.1, T2.2, T3.1, T3.2, T3.3, T3.4, T3.5 |
| G2 | Per-file `--verify` GREEN in Go BEFORE `git rm` bash | ROADMAP.md M15 DoD#2 (strangler) | T3.1, T3.2, T3.3, T3.4, T3.5 |
| G3 | `task quality:gates` GREEN + `go test ./...` GREEN post-delete | ROADMAP.md M15 DoD#4 | T5.1 |
| G4 | Grep sweep 0 hits in taskfiles/docs/skills for each deleted path | ROADMAP.md M15 DoD#4 | T5.1 |
| G5 | CHANGELOG `[Unreleased] § Removed` + `§ Added` entries per lot | ROADMAP.md M15 DoD#5 | T2.1, T2.2, T3.1, T3.2, T3.3, T3.4, T3.5, T5.2 |
| G6 | Deliver Waves A+B in v0.61.0 release | Sponsor 2026-07-10 Path A (ADR-D1) | T2.1, T2.2, T3.1, T3.2, T3.3, T3.4, T3.5 |
| G7 | Wave C phase-3 split-decision at Phase 4 boundary | Blueprint ADR-D3 | T4.1, T4.2 |
| G8 | Wave D → M18 promotion post-release | Blueprint ADR-D4 + sponsor 2026-07-10 | Handoff post-T5.2 (`/roadmap-feature`) |
| G9 | M15 ROADMAP checkbox `[ ]` stays unchecked | Rule 3 + ADR-D1 + DoD literal not met | T5.2 |
| G10 | Baseline capture pre-migration + orphan sweep | Steady-state-baseline mandate 2026-06-06 | T1.1, T1.2 |

## Global DoD (v1.2 — amended per ADR-D6)

- [x] `task quality:gates` GREEN (validated at commit `9e1593fa`)
- [x] `go test ./...` GREEN (validated no-change on Wave A delete-only)
- [x] `grep -R reset-for-e2e.sh taskfiles/` returns 0 hits (validated at commit `9e1593fa`)
- [x] `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` == 120 (Wave A delivered; Waves B/C/D → M18 per ADR-D6)
- [x] Per-task DoD gate met for T1.1, T1.2, T2.1
- [x] CHANGELOG `[Unreleased] § Removed` + `§ Changed` entries for Wave A + M18 amendment
- [x] M15 ROADMAP.md checkbox `[ ]` unchanged (per ADR-D1 + D4 + D6)
- [x] `knowledge-base/audits/M15-*.md` Phase 1 audit trail complete (T1.1 + T1.2)
- [ ] `/roadmap-feature operator-glue-go-migration-remainder --dependency M15` scheduled post-release (Phase 6 handoff)

**v1.2 scope reduction rationale:** ADR-D6 collapses Waves B+C+D into a single M18 milestone. Phases 3 (Wave B), Phase 4 (Wave C split-decision), and Phase 5 (integration validation for Waves A+B) below are OBSOLETE for this cycle — retained in-plan for M18 handoff reference. Only Phase 1 (baseline) + Phase 2 (Wave A) executed.

## Failure scenarios (external I/O signals present — mandatory section)

The plan touches Kubernetes API, Pulumi backend, PG, helm, gh. Per rules/plan-confidence-golden-rule.md § "External I/O signals present":

- **helmfile hook fails mid-sync** → T3.1 failure test.
- **kubectl unreachable** → T3.1 + T3.4 failure test.
- **Pulumi backend timeout** → T3.5 failure test.
- **PG connection reset** → T3.3 failure test.
- **K8s API server 5xx** → T3.3 failure test.
- **Ginkgo suite cluster unreachable** → T3.4 skip-clean test.
- **--verify byte diff post-migration** → per-task RED gate (T3.1..T3.5).

Each scenario has a matching test in the Wave B tasks above. Total ≥ 1 failure scenario per external dependency (helmfile, kubectl, Pulumi SDK, pgx, client-go) covered.

## Final Phase — Integration Validation

The plan is NOT complete until:
- Phase 5 T5.1 all gates GREEN
- Phase 5 T5.2 CHANGELOG audit + ROADMAP verification complete
- LIVE `--verify` GREEN for all migrated Wave B artifacts (helmfile hooks + 3 seeds + Ginkgo suite + Pulumi shim)

If any check fails, return to responsible task (never bypass).

## Handoff (post-release)

Post-`/release` v0.61.0:
1. Invoke `/roadmap-feature operator-glue-go-migration-wave-d` to create M18 (Wave D scope, sponsor sign-off gate).
2. Cross-reference in new M18 milestone: this plan + blueprint ADR-D4.
3. Update `MEMORY.md` with M15-M18 split decision precedent (mirror of M16-M17).
