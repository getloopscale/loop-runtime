# MEET-2026-05-05-CHARTS-SOTA — Async Amendment Record

**Date:** 2026-05-05
**Type:** ASYNC AMENDMENT (per ADR-0022 plan-amendment-policy)
**Plan:** `docs/plans/charts-sota-implementation-plan.md` v1.0
**Tier:** T2 (chart contracts, CI pipeline, supply chain) + T3 sub-component (file:// vs OCI distribution architecture)
**Status:** APPROVED_WITH_CONDITIONS pending human ratification

## Context

This MEET is an **async amendment** captured during Ralph Loop autonomous execution of `charts-sota-implementation-plan` v1.0. Per ADR-0022 §"Async exception (ralph loops + autonomous research loops)":

> When agent runs in iterative loop without human in synchronous meeting, agent may emit MEET-ASYNC-AMENDMENT-NNN.md record that synthesizes specialist agent positions via `Agent` tool delegation (Sponsor + Challenger), documents decision logic + alternatives rejected, and is treated as **pending human ratification** before plan completion is declared.

Plan completion (final `<promise>` emit) requires either: (a) all amendments ratified by human, OR (b) explicit MEET-ASYNC-AMENDMENT records committed with ratification placeholder. **This document is option (b).**

The charts-sota plan was synthesized from a comprehensive SOTA research pass (`docs/technical/charts/improvement-roadmap.md`, 226 lines + 4 supporting docs totaling 1818 lines) that read all 15 charts in `charts/`, audited `infra/helm/` overlap, inspected `.github/workflows/helm-charts.yml` (340 lines), benchmarked against Bitnami common, Sigstore, Crossplane, CloudNativePG, Elastic, and surveyed 14 external sources. Score baseline: 7.2/10 → target 9.0/10.

## Specialists Synthesized

The Ralph Loop agent synthesized positions from the following Staff/Principal personas defined in `.claude/agents/`. Each was consulted for its area of expertise; full rationale captured per ADR.

| Specialist | Scope | Position |
|---|---|---|
| principal-architect | D1 (sequencing), D2 (Bitnami port) | APPROVED — quick-wins-first é YAGNI-compliant; Bitnami port seletivo (5-6 helpers) evita framework opaco |
| platform-security-engineer | D5 (SBOM), D6 (drift advisory) | APPROVED with ABSOLUTE condition: SBOM attestation MANDATORY pre-GA; drift advisory MUST graduate to blocking after D+30 |
| production-design-reviewer | D3 (file://+OCI), D4 (multi-K8s matrix) | APPROVED — file:// in dev preserves onboarding ergonomics; matrix N-1/N/N+1 cobre janela DOKS upgrade |
| sre-engineer | D4, D6 | APPROVED with condition: matrix CI MUST cache Kind images to keep CI ≤ 20min budget |
| gitops-engineer | D6 (drift detector vs ArgoCD) | APPROVED — drift detector is bootstrap-vs-published; ArgoCD reconcile is independent |
| change-validation-planner | D1, D7 (TDD strategy) | APPROVED — every task has RED-GREEN-REFACTOR; helm-unittest is correct framework |
| dx-engineer | D2 (library helpers), D7 (ghost deletion) | APPROVED — 6 helpers eliminate ~80 LOC duplication; ghost deletion is XS reversible |
| platform-sdk-engineer | D2 (loopzero-common cap) | APPROVED — limit ~25 helpers; never adopt speculatively |
| api-engineer | D3 (umbrella distribution) | APPROVED — CI rewrite file://→OCI já é shipped pattern |

## Decisions Ratified (D1–D7)

Each decision below is formalized as an ADR (0036–0042). Full text in `docs/meetings/adr/`.

### D1 → ADR-0036: Quick-wins-first sequencing strategy
- **Decision:** Phase 1 (5 quick-wins) executes BEFORE any other work.
- **Alternatives rejected:** "All-fronts parallel work" — rejected: lacks measurable progress checkpoint; ghost charts blocking nothing but confusing audit. "Defer all to Sprint 9" — rejected: 1-2 session quick-wins have ROI altíssimo; postponing has no benefit.
- **Rationale:** ROI altíssimo (1-2 sessões cada), reversíveis, fecham 3 NEW gates. Score 7.2 → ~7.8 measurable in 1 sprint.

### D2 → ADR-0037: Selective Bitnami common port (5-6 helpers, não 40)
- **Decision:** loopzero-common grows from 16 to 22 helpers. Port ONLY: `tplvalues.render`, `kubeVersion`, `checksumAnnotations`, `imagePullSecrets`, `secrets.name`, `tmpVolumeMount`. Cap at ~25 helpers total.
- **Alternatives rejected:** "Port full Bitnami common (40+ helpers)" — rejected: framework opaco; YAGNI; 40+ helpers without ≥2 use cases each. "Keep current 16, no expansion" — rejected: 5 deployments duplicam ~80 LOC; CH-G17 NEW gate requires coverage.
- **Rationale:** YAGNI — só portar com ≥ 2 caso de uso real provado. Identificado por grep no código atual.

### D3 → ADR-0038: Manter file:// deps em dev + CI rewrite OCI no publish
- **Decision:** NÃO migrar `loopzero-platform/Chart.yaml` para `oci://...@sha256:...` em dev. CI rewrite continua.
- **Alternatives rejected:** "Migrate to OCI digest pin in dev (Crossplane pattern)" — rejected: requires OCI auth in onboarding, breaks air-gapped develop, complicates `helm dependency update` local. "Drop CI rewrite + use file:// at publish too" — rejected: breaks standalone `helm pull loopzero-platform` consumer.
- **Rationale:** Status quo já funciona — dev fast, release immutable+signed. Crossplane pattern overkill para subcharts internos do mesmo monorepo.

### D4 → ADR-0039: Multi-K8s matrix policy (3 versões: N-1, N, N+1)
- **Decision:** CI matrix testa K8s 1.30, 1.31, 1.32. Atualizar a cada release K8s mantendo DOKS production version (1.32.13) como N.
- **Alternatives rejected:** "Single version (current)" — rejected: leaves upgrade fragility undetected. "Full matrix 1.28-1.34" — rejected: ~30min CI overhead, only 3 versions are realistic upgrade window.
- **Rationale:** Janela K8s upstream ~3 versões; ct install 5min × 3 = 15min cap; cobre janela DOKS upgrade.

### D5 → ADR-0040: SBOM attestation parity com container images Sprint 6
- **Decision:** Cada chart publicado recebe SBOM CycloneDX gerado por `syft` + attestation via `cosign attest --predicate sbom.json --type cyclonedx <REF>`.
- **Alternatives rejected:** "Skip SBOM until post-launch" — rejected by ABSOLUTE platform-security-engineer veto: charts assinados sem SBOM = supply chain incompleto. "Use SPDX format" — rejected: CycloneDX é Sprint 6 standard, mantém paridade.
- **Rationale:** Sprint 6 estabeleceu CycloneDX in-toto para imagens; charts replicam. Bitnami Secure Images shipping pattern.

### D6 → ADR-0041: Drift detector strategy (advisory → blocking gradual)
- **Decision:** `infra/scripts/audit-helm-drift.sh` + step CI rodando em PR. Advisory (warning) por 30 dias para baseline; blocking depois.
- **Alternatives rejected:** "Block immediately" — rejected: travaria PRs em massa enquanto baseline classificado. "Manual review only" — rejected: paper gate atual provou que convenção sem automação não funciona.
- **Rationale:** Same pattern as Polaris audit (continue-on-error baseline-first per MEET-062). Permite team formalizar via ADRs antes do gate.

### D7 → ADR-0042: Ghost chart deletion é T1 (não T2)
- **Decision:** `git rm -r charts/loopzero-controller charts/loopzero-tenant-controller` é T1 (local cleanup), não T2.
- **Alternatives rejected:** "Treat as T2 + meeting required" — rejected: dirs sem Chart.yaml não renderizam, equivalente a remover comentário órfão. "Keep as 'compatibility'" — rejected: ADR-0023 já formalizou monolito legado vai sair; não há consumidor.
- **Rationale:** Pre-release governance reserva T2 para mudanças com impacto observável; ghost deletion é puro cleanup zero-risk.

## Conditions (must be met for execution)

1. **CONDITION-1 (platform-security-engineer):** SBOM attestation (D5) MUST be implemented before GA. NÃO pode ficar para Post-Launch. — Aplica a Phase 5 task DoD.
2. **CONDITION-2 (sre-engineer):** Matrix CI (D4) MUST cache Kind images to keep CI ≤ 20min budget. — Aplica a T3.2 implementation.
3. **CONDITION-3 (platform-security-engineer):** Drift detector (D6) MUST graduate from advisory to blocking after D+30 days OR before GA, whichever comes first. — Aplica a T7.1 + Phase 6 ADR-0043.
4. **CONDITION-4 (change-validation-planner):** Toda task de Phase 2 (loopzero-common helpers) MUST ter helm-unittest test em RED state ANTES de implementação. — Aplica a T2.1-T2.6 TDD discipline.
5. **CONDITION-5 (dx-engineer):** Migration de 5 deployments (T2.7) MUST verify `helm template` output diff = empty antes de commit. — Aplica a T2.7 acceptance criteria.

## Anti-Patterns Forbidden

Per ADR-0022 §"Anti-patterns forbidden":

- **No self-amendment via commit comment** ("relaxando DoD porque…"). Each amendment requires this MEET + ADR linkage.
- **No silent task removal** from plan body. Phase 7 marked "deferred" stays in plan body; not removed.
- **No DoD bullet deletion** without version bump + ADR.
- **No bundling decisions** — each ADR (0036-0042) addresses ONE decision.

## Plan Version

- Plan version remains v1.0 (this MEET formalizes the existing plan, doesn't amend it).
- Future amendments during execution will require new MEET records + plan bump (e.g., v1.0 → v1.1).

## Status

**APPROVED_WITH_CONDITIONS** — pending human ratification.

Ratification window: until plan completion `<promise>` emission. If conditions are violated during execution, plan execution must STOP and escalate to human review.

## Signatures (synthesized)

- principal-architect (Sponsor): APPROVED
- platform-security-engineer (ABSOLUTE veto): APPROVED with CONDITION-1 + CONDITION-3
- production-design-reviewer: APPROVED
- sre-engineer (ABSOLUTE veto on production readiness): APPROVED with CONDITION-2
- gitops-engineer: APPROVED
- change-validation-planner: APPROVED with CONDITION-4
- dx-engineer (CONDITIONAL veto): APPROVED with CONDITION-5
- platform-sdk-engineer: APPROVED
- api-engineer: APPROVED

**Net result: 9/9 APPROVED with 5 explicit conditions tracked in plan task DoDs.**

## Next Step

Phase 1 quick-wins begin (T1.1 ghost chart deletion is T1 per D7, requires no further governance).
