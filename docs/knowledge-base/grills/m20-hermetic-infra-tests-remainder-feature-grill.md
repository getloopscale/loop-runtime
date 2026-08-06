---
type: grill
title: "M20 grill — hermetic-infra-tests-remainder"
generated: { by: process:roadmap-feature, at: 2026-07-10 }
status: stable
slug: m20-hermetic-infra-tests-remainder
generated_by: roadmap-feature
generated_at: 2026-07-10
workflow_status: completed
---

# M20 grill — hermetic-infra-tests-remainder

## Q1 — What is this feature and why is it being added NOW?

**Answer (2026-07-10):**

Migrar os **17 hermetic `check-*_test.sh` restantes em `infra/tests/`** + `infra/tests/helm/ory-kratos-public-urls_test.sh` (18 total) para novos gates Go em `cmd/loopzero-ops/internal/quality/<name>/` seguindo o padrão M-Migrate2 estabelecido pelo M16 (2026-07-10 mesmo dia).

**Why now:**
- (a) M16 provou o padrão empiricamente shipando 10 gates em 1 sessão (v0.60.0 2026-07-10);
- (b) os 17 restantes são pure-logic (helm-template + bash-n + file-layout), zero cluster I/O;
- (c) allowlist 118 → ~100 hermetic sem precisar sponsor JUNTOS + Cleanroom cluster;
- (d) fecha o débito do KPI original M15/M18/M19 sem esperar LIVE session.

**Resíduo pós-M20 esperado:** apenas 13 e2e/*.sh (LIVE canonical KEEP) + 4 orchestrators (KEEP-CI) + 1 dogfood residual = ~18 scripts em `infra/tests` total (down from 36 atual).

## Q2 — Which existing milestones must be [x] before M20 starts?

**Answer:** **M16 [x]** apenas (padrão M-Migrate2 canonical estabelecido). M9 (`loopzero-ops quality <gate>` subcommand) está implícito na cadeia via M16. M15/M18/M19 são o backlog operator-glue paralelo e NÃO bloqueiam M20 porque atacam superfície diferente (`infra/scripts/` + `infra/helmfile/scripts/` vs `infra/tests/`).

**M20 elegível imediatamente** — M16 já está [x] desde v0.60.0 (2026-07-10).

## Q3 — What is the verifiable Definition of Done?

**Answer (5-bullet canonical M-Migrate2):**

1. **18 gates Go shipados** em `cmd/loopzero-ops/internal/quality/<name>/` com hermetic tests GREEN sob `-race`.
2. **18 bash scripts DELETED** de `infra/tests/` (17 `check-*_test.sh` + 1 `helm/ory-kratos-public-urls_test.sh`).
3. **Allowlist reduction:** `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 100** (net -18 from 118).
4. **`task quality:gates` + `go test ./...` GREEN** após cada lote de DELETE; grep sweep 0 hits para cada script deletado.
5. **CHANGELOG discipline per lot:** `§ Added` com Go gates + `§ Removed` com bash scripts + `§ Changed` com `taskfiles/quality.yml` wiring.

**Split-decision protocol (mirror M16 D4):** se empirical velocity < 8 gates/sessão, promover residual a M21 via `/roadmap-feature` mid-milestone com ADR. Aplicação canonical do padrão velocity-split precedent estabelecido 2026-07-10.

## Q4 — Top 2 NEW risks

**R1 (MEDIUM): Semantic drift na tradução bash → Go.** 6-8 dos 17 scripts usam padrões de assertion complexos (jq multi-line, yq com escape edge cases, YAML round-trip). Naive port pode perder cobertura silenciosamente — teste que passa hermetic em bash mas silently permite falso-negativo em Go é worse que ruído. **Mitigação:** byte-identical parity capture bash-then-Go antes de `git rm` (mesmo pattern M-Migrate2 T2.0 baseline do M16); salvar output diff em `knowledge-base/audits/` como evidence.

**R2 (LOW): Velocity split-decision timing.** Se decidir muito tarde (após gate 12+), custo de context-switch alto; se decidir muito cedo (gate 6), não dá sinal empirical suficiente para decisão informada. **Mitigação:** baseline capture at gate 8 boundary (metade do caminho) + explicit ADR-D<n> velocity-split at that boundary conforme mirror M16 D4.

## Grill status
completed
