---
name: edge-case-review-2026-05-21-comprehensive-quality-gates
description: Edge case review of the comprehensive quality gates plan (v1.0). All 15 findings incorporated into plan v1.1.
metadata:
  type: audit
  created_at: 2026-05-21
  updated_at: 2026-05-21
  updated_by: claude-opus-4-7
  plan: 2026-05-21-comprehensive-quality-gates-plan.md
  plan_version_reviewed: v1.0
  plan_version_post_incorporation: v1.1
  verdict: PLANO_PRECISA_DE_AJUSTE_INCORPORADO
---

# Edge Case Review — Comprehensive Quality Gates Plan

**Data:** 2026-05-21
**Plano:** `docs/plans/2026-05-21-comprehensive-quality-gates-plan.md`
**Tasks analisadas:** 19 (1 governance + 18 implementation)
**Edge cases encontrados:** 15 (MUST FIX: 6, SHOULD TEST: 6, DOCUMENT: 3)

## Status post-incorporação

Todos os 15 findings incorporados ao plan v1.1 conforme estratégia:
- 6 MUST FIX → T7.4 NEW (EC-1) + amendments em T2.2/T5.1/T5.2/T5.3/T6.2 (EC-2/5/3/6/4)
- 6 SHOULD TEST → novos test cases em TDD blocks (T1.1/T2.1/T3.2/T6.3/T6.4/T6.5)
- 3 DOCUMENT → nova seção "Risk Acknowledgements" (RA-1/2/3)

Coverage Matrix expandida de 11 → 23 gaps mapeados (100%).

## MUST FIX (6 — all incorporated as code amendments)

### EC-1: Branch protection rule update is NOT a task — gates implemented mas não enforced
- **Task afetada:** Plan-level gap (none of Phase 2/3/5/6 included it)
- **Família:** Implemented but not wired
- **Status:** **CLOSED via T7.4 NEW** (Branch Protection Rule Activation)
- **Cenário:** Todos os ~10 workflows criados, mas Required Status Checks no GitHub branch protection settings NÃO atualizados. PRs continuam mergeáveis sem passar gates.
- **Impacto:** Quality Gates ficam decorativos. CLAUDE.md systemic pattern direto.
- **Fix aplicado:** T7.4 com script `branch-protection-update.sh` + config YAML + runbook + phased rollout (advisory mode 7d → blocking)

### EC-2: PR splitting one package into two = chicken-and-egg
- **Task afetada:** T2.2 (baseline regression gate)
- **Família:** State
- **Status:** **CLOSED via T2.2 amendment** (escape valve label)
- **Fix aplicado:** Label `coverage-baseline-defer` permite one-PR exception com 7-day TTL + auto-issue follow-up

### EC-3: PR squash-merge perde "fix #N" do commit individual
- **Task afetada:** T5.2 (bug-fix regression gate)
- **Família:** Integration
- **Status:** **CLOSED via T5.2 amendment**
- **Fix aplicado:** Parse PR TITLE + BODY + commits (sources em ordem de prioridade); regex expandido para `fix #|Bug #|closes #|fixes #|resolves #`

### EC-4: Flaky tracker JSON race condition em parallel CI runs
- **Task afetada:** T6.2 (flaky test detection)
- **Família:** Concurrency
- **Status:** **CLOSED via T6.2 amendment**
- **Fix aplicado:** Tracker NÃO commitado via bot; per-run artifacts (30d retention) + single-writer daily cron via `concurrency: { group: flaky-aggregator, cancel-in-progress: false }`

### EC-5: Dependabot/Renovate PRs bloqueados por template gate
- **Task afetada:** T5.1 (PR template enforcement)
- **Família:** Permission/Integration
- **Status:** **CLOSED via T5.1 amendment**
- **Fix aplicado:** Bot allowlist (`dependabot[bot]`, `renovate[bot]`, `github-actions[bot]`) + label allowlist (`dependencies`, `automated`); configurable via `.github/quality-gates-config.yml`

### EC-6: Test file rename = false positive em deletion audit
- **Task afetada:** T5.3 (test deletion audit)
- **Família:** Format
- **Status:** **CLOSED via T5.3 amendment**
- **Fix aplicado:** Use `git diff --find-renames=80%` to detect rename vs delete; only true deletions require justification

## SHOULD TEST (6 — all incorporated as TDD test cases)

### EC-7: Rename-only PR should PASS coverage diff gate
- **Status:** **TEST ADDED** to T2.1 TDD: `test_coverage_diff_passes_for_rename_only_PR`

### EC-8: t.SkipNow() + t.Skipf() + xit + xdescribe variants devem ser caught
- **Status:** **4 TESTS ADDED** to T6.5 TDD covering all skip API variants (Go + TS)

### EC-9: jest.fn() callback distinction
- **Status:** **2 TESTS ADDED** to T6.4 TDD: `test_mock_budget_distinguishes_callback_from_mock` + `test_mock_budget_gomock_controller_counted`

### EC-10: Doc-only Go package handling
- **Status:** **TEST ADDED** to T1.1 TDD: `test_coverage_go_handles_doc_only_package`

### EC-11: Mutation framework non-determinism
- **Status:** **TEST ADDED** to T3.2 TDD: `test_mutation_baseline_run_3_times_within_5pp_variance`

### EC-12: 50+ E2E sprint deliverable + pyramid gate
- **Status:** **2 TESTS ADDED** to T6.3 TDD + deep-dive on label override mechanism with auto-issue

## DOCUMENT (3 — captured in Risk Acknowledgements section)

### RA-1 (EC-13): CI cost +15-20min per PR may hit GitHub Actions free-tier cap
- **Status:** **DOCUMENTED** with monitoring trigger + 3 mitigation paths

### RA-2 (EC-14): Go atomic coverage fluctuation across version bumps
- **Status:** **DOCUMENTED** with revisit trigger (>5 false positives/week)

### RA-3 (EC-15): api/internal/server refactor may be required
- **Status:** **DOCUMENTED** with escalation trigger (>10 mocks needed)

## Padrões Sistêmicos Detectados

| Padrão | Encontrado? | Onde | Status |
|--------|-------------|------|--------|
| Implemented but not wired | **SIM** | EC-1: branch protection | **CLOSED** via T7.4 |
| Correct code in wrong place | NAO | — | n/a |
| Project name vs ID | N/A | Plano não toca builds/releases | n/a |
| ArgoCD/GitOps patterns | N/A | Plano não toca ArgoCD | n/a |
| Race condition em estado compartilhado | **SIM** | EC-4: flaky tracker | **CLOSED** via T6.2 |
| Dependabot/bot PRs quebram convenção humana | **SIM** | EC-5: template gate | **CLOSED** via T5.1 |

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT | Status |
|------|-------------------|----------|-------------|----------|--------|
| Plan-level (branch protection) | 1 | 1 | 0 | 0 | CLOSED (T7.4 NEW) |
| T1.1 (coverage tooling) | 1 | 0 | 1 | 0 | CLOSED (TDD) |
| T2.1 (diff gate) | 1 | 0 | 1 | 0 | CLOSED (TDD) |
| T2.2 (baseline gate) | 1 | 1 | 0 | 0 | CLOSED (amendment) |
| T3.1 (mutation install) | 0 | 0 | 0 | 0 | — |
| T3.2 (mutation baseline) | 1 | 0 | 1 | 0 | CLOSED (TDD) |
| T4.3 (api/internal coverage push) | 1 | 0 | 0 | 1 | DOCUMENTED (RA-3) |
| T5.1 (PR template) | 1 | 1 | 0 | 0 | CLOSED (amendment) |
| T5.2 (bug-fix gate) | 1 | 1 | 0 | 0 | CLOSED (amendment) |
| T5.3 (test deletion) | 1 | 1 | 0 | 0 | CLOSED (amendment) |
| T6.1 (runtime SLO) | 0 | 0 | 0 | 0 | — |
| T6.2 (flaky tracker) | 1 | 1 | 0 | 0 | CLOSED (amendment) |
| T6.3 (pyramid) | 1 | 0 | 1 | 0 | CLOSED (TDD + deep-dive) |
| T6.4 (mock budget) | 1 | 0 | 1 | 0 | CLOSED (TDD + deep-dive) |
| T6.5 (skip link) | 1 | 0 | 1 | 0 | CLOSED (TDD + deep-dive) |
| Cross-cutting (CI cost, Go versions) | 2 | 0 | 0 | 2 | DOCUMENTED (RA-1/2) |
| **TOTAL** | **15** | **6** | **6** | **3** | **15/15 CLOSED** |

**Veredicto inicial:** PLANO PRECISA DE AJUSTE
**Veredicto post-incorporação (v1.1):** PLANO OK

Plan v1.1 incorporou TODOS os 15 findings:
- 6 MUST FIX viraram code amendments (T7.4 novo + 5 task amendments)
- 6 SHOULD TEST viraram TDD test cases
- 3 DOCUMENT viraram Risk Acknowledgements section

Coverage Matrix expandida de 11 → 23 gaps (100% mapeados).
