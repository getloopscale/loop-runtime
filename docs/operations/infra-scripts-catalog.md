# infra/scripts — Catálogo de Scripts (utilidade + criticidade + impacto-PROD)

> Catálogo de referência dos **51 scripts** em `infra/scripts/` (recursivo).
> (`check-drill-credentials-isolation.sh` migrado para Go em 2026-05-24 →
> `loopzero-ops quality drill-creds-isolation`; bash deletado.)
> Criado 2026-05-24. Colunas: utilidade, criticidade, ambiente/impacto-PROD,
> LOC·última-mod, quem invoca, disposição allowlist.
>
> Fonte: header de cada script + `docs/operations/drill-migration-map.md` (Track C)
> + `git log` (mod) + grep de callers + grep de sinais de ambiente (prod/k3d/drill/test).

## Pergunta-chave: "deletar gera problema em PRODUÇÃO?"

**Camada 1 — runtime de prod (servindo clientes): NÃO, para nenhum script.**
O caminho de request de prod é loopzero-api (Go) + operators (Go) + charts K8s. Nenhum
shell script está no caminho de request. Deletar não derruba prod em execução.

**Camada 2 — ciclo de vida de prod (provisionar/migrar/operar/destruir): SIM, ~9.**
Não rodam no runtime, mas gerenciam prod. Deletar quebra setup/migração/recovery.

**Camada 3 — local/drill/test/build: não tocam prod**, mas quebram dev-local,
o drill (gate de maturidade `a`), os gates de qualidade, ou a publicação de imagens.

## Taxonomia de ambiente

| Tag | Ambiente de efeito | Deletar afeta prod? |
|---|---|---|
| **PROD-LIFE** | Provisiona/migra/opera/destrói a produção DOKS real | **SIM (lifecycle, não runtime)** |
| **LOCAL** | Só clusters k3d locais (dev loop) | Não (zero prod) — quebra dev local |
| **DRILL** | Stack DOKS drill efêmero e isolado (`loopzero-drill`) | Não (isolado) — quebra o gate de prod-readiness |
| **TEST-CI** | Chaos/cobertura/dogfood/gates de qualidade | Não — quebra validação que protege prod |
| **BUILD** | Build/push de imagens | Não (runtime) — quebra pipeline de release |
| **LIB** | Sourced por outros scripts | Efeito via consumidor |

## Rubrica de criticidade

| Nível | Significado |
|---|---|
| **CRÍTICO** | Mutação destrutiva ou load-bearing de cluster/cloud LIVE; setup/migração/recovery/destroy |
| **ALTO** | Recursos pagos (drill DOKS), injeção de chaos, networking/recovery |
| **MÉDIO** | CI/qualidade/diagnóstico read-only/build |
| **BAIXO** | Lib sourced / helper |

**Invocado por:** `Task` · `SKILL` · `script` · `go-test` · `operador` (sem caller automatizado).

---

## Raiz `infra/scripts/` (25)

| Script | Utilidade | Crit. | Ambiente / impacto-PROD | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|---|
| `run-migrations.sh` | Migrations forward-only do banco (inclui prod) | **CRÍTICO** | **PROD-LIFE** — migração de schema prod | 261 · 05-21 | Task, script | KEEP-OPERATOR |
| `pre-destroy-cleanup.sh` | Remove finalizers + cloud antes de `pulumi destroy` | **CRÍTICO** | **PROD-LIFE** — teardown seguro de prod | 305 · 05-21 | operador | KEEP-OPERATOR |
| `bootstrap-platform.sh` | Setup idempotente da plataforma pós `pulumi up` | **CRÍTICO** | **PROD-LIFE** — bootstrap de prod | 276 · 05-21 | operador | KEEP-OPERATOR |
| `provision-api-tokens.sh` | Tokens Gitea/ArgoCD + kubeconfigs cross-cluster | **CRÍTICO** | **PROD-LIFE** — provisionamento de prod | 357 · 05-21 | Task, script | KEEP-OPERATOR |
| `break-glass.sh` | Procedimento de emergência break-glass (MEET-055) | **CRÍTICO** | **PROD-LIFE** — acesso de emergência a prod | 145 · 04-23 | Task | KEEP-OPERATOR |
| `check-prod-vap-readiness.sh` | Pré-flight LIVE antes do deny-flip de VAP em prod | **MÉDIO** | **PROD-LIFE** — flip de segurança prod | 122 · 05-23 | operador | KEEP-OPERATOR |
| `operator-cutover-execute.sh` | Cutover do operator split-binary | **CRÍTICO** | **PROD-LIFE** — rollout de operator prod | 286 · 05-21 | operador | KEEP-OPERATOR |
| `migrate-tenant-namespace.sh` | Migração de namespace de tenant (F-04) | **ALTO** | **PROD-LIFE** — pode migrar tenant prod | 229 · 04-29 | Task | REVIEW-NEEDED |
| `health-check-platform.sh` | Valida plataforma operacional (pods/API/tokens) | **MÉDIO** | **PROD-LIFE** — diagnóstico (pode rodar vs prod) | 202 · 05-21 | operador | KEEP-OPERATOR |
| `bootstrap-seed-tenant-cr.sh` | Semeia o Tenant CR de bootstrap (B26) | **ALTO** | LOCAL/bootstrap | 58 · 05-05 | Task | REVIEW-NEEDED |
| `clean-incremental.sh` | Remove builds/tenants/projects órfãos | **CRÍTICO** | LOCAL — limpeza de dev | 61 · 04-29 | Task, SKILL | KEEP-OPERATOR |
| `ensure-project.sh` | Cria projeto default via API se não existir | **MÉDIO** | LOCAL/CI | 49 · 03-30 | Task | KEEP-CI-ORCHESTRATOR |
| `k3d-cross-cluster-heal.sh` | Recovery idempotente cross-cluster (H1-H15) | **CRÍTICO** | **LOCAL** — só k3d (zero prod) | 1005 · 05-22 | Task | KEEP-OPERATOR |
| `regen-all-scoped-kubeconfigs.sh` | Regenera SA tokens scoped por ClusterCredential | **CRÍTICO** | LOCAL — k3d (exclui cell-default) | 220 · 05-21 | Task | KEEP-OPERATOR |
| `setup-cross-cluster-networking.sh` | Alcançabilidade entre nós k3d | **ALTO** | **LOCAL** — só k3d | 86 · 03-30 | Task | KEEP-OPERATOR |
| `setup-host-inotify-limits.sh` | Persiste limites de inotify do kernel | **MÉDIO** | **LOCAL** — host de dev | 96 · 05-13 | Task | KEEP-OPERATOR |
| `update-cross-cluster-ips.sh` | Atualiza IPs cross-cluster nos manifests | **ALTO** | **LOCAL** — k3d | 171 · 03-31 | Task, script | KEEP-OPERATOR |
| `build-and-push-dockerfile-lint.sh` | Build + push da imagem `loopzero-dockerfile-lint` | **MÉDIO** | **BUILD** — registry (local) | 64 · 05-07 | Task, script | KEEP-OPERATOR |
| `build-and-push-loopzero-packs.sh` | Build + push da imagem `loopzero-packs` v0.4.0 | **MÉDIO** | **BUILD** — registry (local) | 83 · 05-07 | Task, script | KEEP-OPERATOR |
| `dr-drill.sh` | Orquestração do DR drill | **ALTO** | **DRILL** — isolado | 297 · 05-21 | Task, script | DELEGATE-ARGOCD |
| `drill-helmfile-bootstrap.sh` | Bootstrap prod-identical do drill (provisioning) | **CRÍTICO** | **DRILL** — isolado | 1126 · 05-21 | Task, SKILL | DELEGATE-HELMFILE |
| `drill-mint-jwt.sh` | Minta JWT do drill via Hydra; assert TTL≥7200s | **ALTO** | **DRILL** — isolado | 300 · 05-23 | Task, SKILL, script | KEEP-OPERATOR |
| `velero-dr-drill.sh` | Drill de backup/restore Velero (T4.2) | **ALTO** | **DRILL** — isolado | 413 · 05-06 | operador | REVIEW-NEEDED |
| `pkg-soak-smoke.sh` | Soak smoke de `pkg/` vs localhost:13001 | **MÉDIO** | **TEST-CI** — local | 161 · 05-04 | Task | KEEP-OPERATOR |

## `infra/scripts/chaos/` (5) — todos **TEST-CI** (chaos-mesh local/drill; zero prod runtime)

| Script | Utilidade | Crit. | Impacto-PROD | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|---|
| `run-scenario.sh` | Orquestrador `/chaos-test`; verdict → `loopzero-ops chaos slo-compare` | **ALTO** | TEST-CI — quebra validação de resiliência | 284 · 05-24 | SKILL, script, go-test | DELEGATE-HELMFILE |
| `run-full.sh` | Roda os 10 cenários chaos sequencialmente | **ALTO** | TEST-CI | 181 · 05-22 | go-test | REVIEW-NEEDED |
| `cleanup.sh` | Remove TODOS os recursos chaos-mesh do cluster | **ALTO** | TEST-CI | 113 · 05-21 | script | REVIEW-NEEDED |
| `baseline.sh` | Captura baseline steady-state (curl /health + kubectl) | **MÉDIO** | TEST-CI | 228 · 05-22 | SKILL, script, go-test | REVIEW-NEEDED |
| `report.sh` | Relatório markdown (recebe `--verdict`) | **BAIXO** | TEST-CI | 197 · 05-21 | SKILL, script, go-test | REVIEW-NEEDED |

## `infra/scripts/dr/` (11) — todos **DRILL** (DOKS efêmero isolado; zero prod)

| Script | Utilidade | Crit. | Impacto-PROD | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|---|
| `state-reset.sh` | Reset do state pulumi/doctl do drill | **CRÍTICO** | DRILL — isolado (quebra capacidade de drill) | 340 · 05-21 | SKILL, script | REVIEW-NEEDED |
| `teardown-validator.sh` | Valida zero residuais + force-delete doctl (custo) | **CRÍTICO** | DRILL — isolado | 282 · 05-23 | Task, SKILL, script | KEEP-OPERATOR |
| `watchdog.sh` | Cron de auto-destroy do drill (TTL excedido) | **CRÍTICO** | DRILL — isolado | 150 · 05-23 | Task, script | REVIEW-NEEDED |
| `cancel-refresh.sh` | Cancela pulumi preso + refresh do state drill | **CRÍTICO** | DRILL — isolado | 142 · 05-20 | SKILL, script | REVIEW-NEEDED |
| `chaos-orchestrator.sh` | Sequencia chaos no drill | **ALTO** | DRILL — isolado | 275 · 05-22 | SKILL, script | DELEGATE-HELMFILE |
| `cleanroom-orchestrator.sh` | Sequencia cleanroom no drill | **ALTO** | DRILL — isolado | 257 · 05-22 | SKILL, script | REVIEW-NEEDED |
| `dogfood-orchestrator.sh` | Sequencia `/dogfood` no drill via `$LOOPZERO` | **ALTO** | DRILL — isolado | 290 · 05-22 | SKILL | REVIEW-NEEDED |
| `evidence-capture.sh` | Coleta evidências do drill (kubectl/doctl/pulumi+git) | **MÉDIO** | DRILL — isolado | 298 · 05-20 | script | KEEP-OPERATOR |
| `jwt-diagnose.sh` | Diagnóstico read-only de 401 JWT no drill | **MÉDIO** | DRILL — isolado | 372 · 05-22 | script | REVIEW-NEEDED |
| `preflight.sh` | Pré-flight do drill | **MÉDIO** | DRILL — isolado | 381 · 05-22 | Task, SKILL, script | REVIEW-NEEDED |
| `cost-report.sh` | Custo do drill (aritmética → `loopzero-ops drill cost`) | **MÉDIO** | DRILL — isolado | 118 · 05-24 | go-test | REVIEW-NEEDED |

## `infra/scripts/lib/` (7) — **LIB** (sourced; impacto via consumidor)

| Script | Utilidade | Crit. | Impacto-PROD | LOC·mod | Consumidores | Allowlist |
|---|---|---|---|---|---|---|
| `dry-run.sh` | Helper `--dry-run` para scripts que escrevem state | **BAIXO** | LIB (9 consumidores) | 86 · 05-21 | 9 | KEEP-OPERATOR |
| `drill-skill-common.sh` | Helpers das skills de drill | **BAIXO** | LIB → DRILL (6) | 464 · 05-18 | 6 | REVIEW-NEEDED |
| `bootstrap-cell-cc-common.sh` | Lib de ClusterCredential cell | **BAIXO** | LIB → bootstrap (2) | 161 · 05-14 | 2 | REVIEW-NEEDED |
| `drill-jwt-gate.sh` | Gate de auth e2e (minta + curl LIVE) | **MÉDIO** | LIB → DRILL (1) | 68 · 05-20 | 1 | KEEP-OPERATOR |
| `drill-dogfood-journeys.sh` | Lib de jornadas `$LOOPZERO` do dogfood drill | **BAIXO** | LIB → DRILL (1) | 242 · 05-19 | 1 | REVIEW-NEEDED |
| `bootstrap-cell-cc-drill-common.sh` | Lib de CC cell para o drill | **BAIXO** | LIB → DRILL (1) | 166 · 05-17 | 1 | REVIEW-NEEDED |
| `sops.sh` | Helper de criptografia SOPS (secrets) | **MÉDIO** | LIB (manuseia secrets) | 109 · 05-23 | — | KEEP-OPERATOR |

## `infra/scripts/ci/` (4) — todos **TEST-CI** (gates de qualidade; zero prod runtime)

| Script | Utilidade | Crit. | Impacto-PROD | LOC·mod | Invocado por | Allowlist |
|---|---|---|---|---|---|---|
| `coverage-go.sh` | Coleta cobertura Go em todos os pacotes | **MÉDIO** | TEST-CI — quebra gate de cobertura | 120 · 05-23 | Task | KEEP-CI-ORCHESTRATOR |
| `coverage-cli.sh` | Coleta cobertura TS/JS do `cli/` (Jest) | **MÉDIO** | TEST-CI | 58 · 05-21 | Task | KEEP-CI-ORCHESTRATOR |
| `branch-protection-update.sh` | Ativa branch protection via `gh` (EC-1) | **MÉDIO** | TEST-CI — governança de repo | 84 · 05-21 | operador | KEEP-CI-ORCHESTRATOR |
| `test-inventory.sh` | Cataloga arquivos de teste por categoria (ADR D7) | **MÉDIO** | TEST-CI | 173 · 05-21 | operador | KEEP-OPERATOR |

---

## Resumo por ambiente / impacto-PROD

| Ambiente | Qtd | Deletar → problema em prod? |
|---|---|---|
| **PROD-LIFE** | 9 | **SIM** — quebra provisionar/migrar/operar/destruir prod (não o runtime). NÃO deletar. |
| **LOCAL** | 8 | Não (zero prod) — mas quebra o dev loop k3d |
| **DRILL** | 16 (5 raiz + 11 dr/) | Não (isolado) — mas quebra o gate de prod-readiness (maturity gate `a`) |
| **TEST-CI** | 10 (5 chaos + 4 quality + soak) | Não (runtime) — mas remove a validação que mantém prod saudável |
| **BUILD** | 2 | Não (runtime) — quebra publicação de imagens |
| **LIB** | 7 | Efeito via consumidores |

**Conclusão:** zero scripts são deletáveis sem **algum** custo — os 9 PROD-LIFE são
intocáveis; os outros 43 não tocam o runtime de prod mas são wired em local/drill/CI/build
(todos com consumidores comprovados). O eixo correto para deletar é **morto/redundante**,
não criticidade nem ambiente — e o ciclo trash-validate-delete já removeu os mortos.

> **Nota:** redução real de contagem vem por **migração** (lógica → Go/helmfile/Ginkgo,
> depois deletar o bash), não por deleção direta. `cost-report`/`run-scenario` já tiveram
> a lógica pura extraída (Track C → `loopzero-ops`); o bash remanescente é orquestração que
> permanece KEEP por design (ADR D1 no-shell-wrap). LOC·mod via `git log` em 2026-05-24.
