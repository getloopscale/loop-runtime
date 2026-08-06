---
type: grill
title: "Roadmap-feature grill — `infra-tests-hermetic-to-quality-gates`"
generated: { by: process:roadmap-feature, at: 2026-07-10 }
status: stable
slug: infra-tests-hermetic-to-quality-gates
generated_by: roadmap-feature
generated_at: 2026-07-10
workflow_status: completed
target_milestone: M16
---

# Roadmap-feature grill — `infra-tests-hermetic-to-quality-gates`

## Q1 — Why NOW?

**Answer:** KPI-driven — M15 exige allowlist ≤ 30.

M15 está in-progress com KPI literal `allowlist ≤ 30`. Hoje: **27 hermetic scripts** em `loopzero/infra/tests/check-*_test.sh` (~4,000 LoC bash) totalmente elegíveis para migration. Sem M16, M15 fica com débito residual — o KPI de 30 não pode ser atingido apenas com seed migrations.

Precedent: M-Migrate2 já shipou **101 quality gates** em `cmd/loopzero-ops/internal/quality/` + 20 seeds shipados nesta sessão. Padrão bem-estabelecido.

## Q2 — Dependencies

**Answer:** M13 + M15.

- **M13** (Chaos Declarative + Bash Strangler Completion): fechou o gate `no-new-bash` invertido. Sem M13, novo bash pode aparecer paralelo à migration.
- **M15** (Operator Glue → Go: Allowlist ≤ 30): estabeleceu o padrão M-Migrate2 aplicado às 20 seeds. M16 refina o mesmo target `allowlist ≤ 30` estendendo para hermetic test scripts.

M13 é `[x]`. M15 é `[ ]` (em progresso — M16 elegível quando M15 fechar).

## Q3 — Definition of Done

**Answer (múltipla):**

1. **Todas as 3 waves migradas** (Wave 3 inclusa):
   - **Wave 1** — 3 syntax/layout gates (`check-sprint-syntax`, `check-archive-script-repo-root`, `check-no-cloud-dev-default`) → 3 novos gates em `cmd/loopzero-ops/internal/quality/*/` + registrados em `quality_registry.go`. Bash deletado. ~200 LoC bash → ~450 LoC Go.
   - **Wave 2** — 6 helm-template gates (`check-loki-vault-retention`, `check-loki-retention-schema`, `check-loopzero-external-secrets-{chart-sanity,creation-policy}`, `check-vault-prom-alerts`, `check-day30-day60-cronjobs`, `check-openbao-init-auth-mounts`) → 6 novos gates. Helm binary D1-exempt via `os/exec`. ~800 LoC bash → ~1,800 LoC Go.
   - **Wave 3** — 11 bash-target pair migrations (`check-migrate-legacy-namespaces` + target script; `check-rotate-secret` + target; `check-enable-vault-backup` + target; `check-restore-vault-snapshot` + target; `check-seed-loopzero-secrets` + target; `check-vault-sync` + target; `check-generate-secrets-vault-fallback` + target; `check-promtail-vault-{scrape,empty-targets}` + targets; `check-scoped-kubeconfig-{cold-bootstrap,vault}` + targets). Cada par migrado junto para não deixar test-only órfão. ~4,000 LoC bash → ~6,000 LoC Go.

2. **Allowlist reduction quantificada** — `allowlist -= 27` (target subset — não substitui M15 KPI global).

3. **`task quality:gates` + `go test ./...` GREEN** após cada wave. Zero regressão.

4. **Padrão M-Migrate2 seguido rigidamente** — cada gate: pkg Go + hermetic test + registro em `quality_registry.go` + `git rm` bash + CHANGELOG `§ Removed` + `§ Added`.

## Q4 — Top 2 NEW risks

**Answer (múltipla):**

1. **Perda de semântica assertion na tradução bash → Go.**
   Bash tests usam `grep -q` + `jq` + `bash -n` com sutilezas de escape/quoting que podem drift na Go port. Um teste que passa hermetico em bash mas silently permite falso-negativo em Go é worse que ruído.
   **Mitigação**: **byte-for-byte parity** por gate — rodar bash + rodar Go em modo comparativo antes de `git rm`. Salvar output diff em `knowledge-base/audits/` como evidence.

2. **Wave 3 scope creep — 11 pares bash-target.**
   Migrar 11 pares (test + target) é 2-3x maior que Waves 1+2 juntos (~4,000 LoC bash + ~6,000 LoC Go, 2-3 semanas). Risco de milestone não fechar em 1 release cycle.
   **Mitigação**: **split em Wave 3a (6 pares mais simples) + Wave 3b (5 mais complexos)** OU **promover Wave 3 para M17** se estourar prazo. Decision no meio do M16 baseado no ritmo de Waves 1+2.

## Downstream

- Slug: `infra-tests-hermetic-to-quality-gates`
- Target milestone: **M16**
- Not chained: this skill does NOT auto-invoke `/auto-plan M16`. Sponsor decides when to start.
