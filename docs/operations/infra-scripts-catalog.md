# infra/scripts — Catálogo de Scripts (utilidade + criticidade)

> Catálogo de referência dos **52 scripts** em `infra/scripts/` (recursivo).
> Criado 2026-05-24. Colunas: utilidade, criticidade, LOC·última-mod, quem invoca,
> disposição no allowlist (`.claude/rules/shell-scripts-allowlist.md`).
>
> Fonte: header de cada script + auditoria `docs/operations/drill-migration-map.md`
> (Track C) + `git log` (mod) + grep de callers (Taskfile / SKILL / script / go-test).

## Rubrica de criticidade

| Nível | Significado | Falha causa |
|---|---|---|
| **CRÍTICO** | Mutação destrutiva ou load-bearing de cluster/cloud LIVE; setup/deploy/recovery/migração de banco | Setup quebrado, recovery falho, perda de dados, prod afetado |
| **ALTO** | Provisiona/destrói recursos pagos (drill DOKS), injeta chaos, ou recovery/networking importante | Custo não controlado, infra drill zumbi, recovery incompleto |
| **MÉDIO** | Gate de CI / qualidade / geração / diagnóstico read-only / build de imagem | Sinal de qualidade perdido; sem dano direto à infra |
| **BAIXO** | Biblioteca sourced (sem efeito direto) / helper | Quebra só via consumidor |

**Invocado por:** `Task` = taskfiles · `SKILL` = `.claude/skills/*` · `script` = outro script/teste shell · `go-test` = teste Go · `operador` = sem caller automatizado (rodado à mão).

---

## Raiz `infra/scripts/` (25)

| Script | Utilidade | Crit. | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|
| `bootstrap-platform.sh` | Setup idempotente da plataforma após `pulumi up` | **CRÍTICO** | 276 · 05-21 | operador | KEEP-OPERATOR |
| `bootstrap-seed-tenant-cr.sh` | Semeia o Tenant CR de bootstrap (B26) | **ALTO** | 58 · 05-05 | Task | REVIEW-NEEDED |
| `break-glass.sh` | Procedimento de emergência break-glass (MEET-055 M3) | **CRÍTICO** | 145 · 04-23 | Task | KEEP-OPERATOR |
| `build-and-push-dockerfile-lint.sh` | Build + push da imagem `loopzero-dockerfile-lint` | **MÉDIO** | 64 · 05-07 | Task, script | KEEP-OPERATOR |
| `build-and-push-loopzero-packs.sh` | Build + push da imagem `loopzero-packs` v0.4.0 (H16) | **MÉDIO** | 83 · 05-07 | Task, script | KEEP-OPERATOR |
| `check-drill-credentials-isolation.sh` | Verifica isolamento de credenciais drill vs prod (D7) | **MÉDIO** | 125 · 05-21 | script | MIGRATE-GO-PUREFN |
| `check-prod-vap-readiness.sh` | Pré-flight LIVE antes do deny-flip de VAP em prod | **MÉDIO** | 122 · 05-23 | operador | KEEP-OPERATOR |
| `clean-incremental.sh` | Remove builds/tenants/projects órfãos | **CRÍTICO** | 61 · 04-29 | Task, SKILL | KEEP-OPERATOR |
| `dr-drill.sh` | Orquestração do DR drill (prepare→recover→teardown) | **ALTO** | 297 · 05-21 | Task, script | DELEGATE-ARGOCD |
| `drill-helmfile-bootstrap.sh` | Bootstrap prod-identical do drill (provisioning) | **CRÍTICO** | 1126 · 05-21 | Task, SKILL | DELEGATE-HELMFILE |
| `drill-mint-jwt.sh` | Minta JWT do drill via Hydra; assert TTL≥7200s | **ALTO** | 300 · 05-23 | Task, SKILL, script | KEEP-OPERATOR |
| `ensure-project.sh` | Cria projeto default via API se não existir | **MÉDIO** | 49 · 03-30 | Task | KEEP-CI-ORCHESTRATOR |
| `health-check-platform.sh` | Valida plataforma operacional (pods/API/tokens) | **MÉDIO** | 202 · 05-21 | operador | KEEP-OPERATOR |
| `k3d-cross-cluster-heal.sh` | Recovery idempotente cross-cluster local (H1-H15) | **CRÍTICO** | 1005 · 05-22 | Task | KEEP-OPERATOR |
| `migrate-tenant-namespace.sh` | Migração de namespace de tenant (F-04) | **ALTO** | 229 · 04-29 | Task | REVIEW-NEEDED |
| `operator-cutover-execute.sh` | Cutover do operator split-binary | **CRÍTICO** | 286 · 05-21 | operador | KEEP-OPERATOR |
| `pkg-soak-smoke.sh` | Soak smoke de `pkg/` vs localhost:13001 | **MÉDIO** | 161 · 05-04 | Task | KEEP-OPERATOR |
| `pre-destroy-cleanup.sh` | Remove finalizers + cloud antes de `pulumi destroy` | **CRÍTICO** | 305 · 05-21 | operador | KEEP-OPERATOR |
| `provision-api-tokens.sh` | Provisiona tokens Gitea/ArgoCD + kubeconfigs | **CRÍTICO** | 357 · 05-21 | Task, script | KEEP-OPERATOR |
| `regen-all-scoped-kubeconfigs.sh` | Regenera SA tokens scoped por ClusterCredential | **CRÍTICO** | 220 · 05-21 | Task | KEEP-OPERATOR |
| `run-migrations.sh` | Migrations forward-only do banco | **CRÍTICO** | 261 · 05-21 | Task, script | KEEP-OPERATOR |
| `setup-cross-cluster-networking.sh` | Alcançabilidade entre nós k3d (redes Docker) | **ALTO** | 86 · 03-30 | Task | KEEP-OPERATOR |
| `setup-host-inotify-limits.sh` | Persiste limites de inotify do kernel | **MÉDIO** | 96 · 05-13 | Task | KEEP-OPERATOR |
| `update-cross-cluster-ips.sh` | Atualiza IPs cross-cluster nos manifests | **ALTO** | 171 · 03-31 | Task, script | KEEP-OPERATOR |
| `velero-dr-drill.sh` | Drill de backup/restore Velero (T4.2) | **ALTO** | 413 · 05-06 | operador | REVIEW-NEEDED |

## `infra/scripts/chaos/` (5)

| Script | Utilidade | Crit. | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|
| `baseline.sh` | Captura baseline steady-state (curl /health + kubectl) | **MÉDIO** | 228 · 05-22 | SKILL, script, go-test | REVIEW-NEEDED |
| `cleanup.sh` | Remove TODOS os recursos chaos-mesh do cluster | **ALTO** | 113 · 05-21 | script | REVIEW-NEEDED |
| `report.sh` | Relatório markdown (recebe `--verdict`; só formatação) | **BAIXO** | 197 · 05-21 | SKILL, script, go-test | REVIEW-NEEDED |
| `run-full.sh` | Roda os 10 cenários chaos sequencialmente | **ALTO** | 181 · 05-22 | go-test | REVIEW-NEEDED |
| `run-scenario.sh` | Orquestrador `/chaos-test`; verdict → `loopzero-ops chaos slo-compare` | **ALTO** | 284 · 05-24 | SKILL, script, go-test | DELEGATE-HELMFILE |

## `infra/scripts/dr/` (11)

| Script | Utilidade | Crit. | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|
| `cancel-refresh.sh` | Cancela operação pulumi presa + refresh do state | **CRÍTICO** | 142 · 05-20 | SKILL, script | REVIEW-NEEDED |
| `chaos-orchestrator.sh` | Sequencia chaos no drill (skill-driven) | **ALTO** | 275 · 05-22 | SKILL, script | DELEGATE-HELMFILE |
| `cleanroom-orchestrator.sh` | Sequencia cleanroom no drill (skill-driven) | **ALTO** | 257 · 05-22 | SKILL, script | REVIEW-NEEDED |
| `cost-report.sh` | Custo do drill (aritmética → `loopzero-ops drill cost`; `--auto` lê pulumi) | **MÉDIO** | 118 · 05-24 | go-test | REVIEW-NEEDED |
| `dogfood-orchestrator.sh` | Sequencia `/dogfood` no drill via `$LOOPZERO` | **ALTO** | 290 · 05-22 | SKILL | REVIEW-NEEDED |
| `evidence-capture.sh` | Coleta evidências do drill (kubectl/doctl/pulumi+git) | **MÉDIO** | 298 · 05-20 | script | KEEP-OPERATOR |
| `jwt-diagnose.sh` | Diagnóstico read-only de 401 JWT (Hydra vs loopzero-api) | **MÉDIO** | 372 · 05-22 | script | REVIEW-NEEDED |
| `preflight.sh` | Pré-flight do drill (cred-isolation + reachability) | **MÉDIO** | 381 · 05-22 | Task, SKILL, script | REVIEW-NEEDED |
| `state-reset.sh` | Reset do state pulumi/doctl do drill | **CRÍTICO** | 340 · 05-21 | SKILL, script | REVIEW-NEEDED |
| `teardown-validator.sh` | Valida zero residuais + force-delete doctl (custo-crítico) | **CRÍTICO** | 282 · 05-23 | Task, SKILL, script | KEEP-OPERATOR |
| `watchdog.sh` | Cron de auto-destroy do drill (TTL excedido) | **CRÍTICO** | 150 · 05-23 | Task, script | REVIEW-NEEDED |

## `infra/scripts/lib/` (7) — bibliotecas sourced

| Script | Utilidade | Crit. | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|
| `bootstrap-cell-cc-common.sh` | Lib para `bootstrap-cell-{default,1,2}-cc.sh` | **BAIXO** | 161 · 05-14 | script (sourced) | REVIEW-NEEDED |
| `bootstrap-cell-cc-drill-common.sh` | Lib de ClusterCredential cell para o drill | **BAIXO** | 166 · 05-17 | script (sourced) | REVIEW-NEEDED |
| `drill-dogfood-journeys.sh` | Lib de jornadas `$LOOPZERO` do dogfood drill | **BAIXO** | 242 · 05-19 | script (sourced) | REVIEW-NEEDED |
| `drill-jwt-gate.sh` | Gate de auth e2e (`verify_jwt_auth_e2e`: minta + curl LIVE) | **MÉDIO** | 68 · 05-20 | script (sourced) | KEEP-OPERATOR |
| `drill-skill-common.sh` | Helpers compartilhados das skills de drill | **BAIXO** | 464 · 05-18 | SKILL, script (sourced) | REVIEW-NEEDED |
| `dry-run.sh` | Helper `--dry-run` para scripts que escrevem state | **BAIXO** | 86 · 05-21 | script (sourced) | KEEP-OPERATOR |
| `sops.sh` | Helper de criptografia SOPS (manuseia secrets) | **MÉDIO** | 109 · 05-23 | Task | KEEP-OPERATOR |

## `infra/scripts/quality/` (4)

| Script | Utilidade | Crit. | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|
| `branch-protection-update.sh` | Ativa branch protection via `gh` (EC-1) | **MÉDIO** | 84 · 05-21 | operador | KEEP-CI-ORCHESTRATOR |
| `coverage-cli.sh` | Coleta cobertura TS/JS do `cli/` (Jest) | **MÉDIO** | 58 · 05-21 | Task | KEEP-CI-ORCHESTRATOR |
| `coverage-go.sh` | Coleta cobertura Go em todos os pacotes | **MÉDIO** | 120 · 05-23 | Task | KEEP-CI-ORCHESTRATOR |
| `test-inventory.sh` | Cataloga arquivos de teste por categoria (ADR D7) | **MÉDIO** | 173 · 05-21 | operador | KEEP-OPERATOR |

---

## Resumo

| Criticidade | Qtd |
|---|---|
| **CRÍTICO** | 12 |
| **ALTO** | 12 |
| **MÉDIO** | 16 |
| **BAIXO** | 5 |

Por diretório: raiz **25** · chaos **5** · dr **11** · lib **7** · quality **4** = 52.
Maior: `drill-helmfile-bootstrap.sh` (1126) · `k3d-cross-cluster-heal.sh` (1005).
Sem caller automatizado (só operador): bootstrap-platform, check-prod-vap-readiness,
health-check-platform, operator-cutover-execute, pre-destroy-cleanup, velero-dr-drill,
quality/branch-protection-update, quality/test-inventory.

> **Nota:** "Allowlist" reflete a disposição em `.claude/rules/shell-scripts-allowlist.md`
> (KEEP-OPERATOR = mantido por necessidade operacional; MIGRATE-* = alvo de migração;
> REVIEW-NEEDED = ainda não reclassificado). `cost-report`/`run-scenario` já tiveram a
> **lógica pura** extraída para `loopzero-ops drill cost` / `loopzero-ops chaos slo-compare`
> (Track C); o bash remanescente é orquestração (pulumi/doctl/kubectl/curl) que permanece
> KEEP por design (ADR D1 no-shell-wrap). LOC·mod via `git log` em 2026-05-24.
