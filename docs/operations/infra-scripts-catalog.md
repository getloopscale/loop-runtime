# infra/scripts — Catálogo de Scripts (utilidade + criticidade)

> Catálogo de referência dos **52 scripts** em `infra/scripts/` (recursivo).
> Criado 2026-05-24. Uma entrada por script: caminho, utilidade, criticidade,
> disposição no allowlist (`.claude/rules/shell-scripts-allowlist.md`).
>
> Fonte da utilidade: header de cada script + a auditoria
> `docs/operations/drill-migration-map.md` (Track C) + a disposição allowlist.

## Rubrica de criticidade

| Nível | Significado | Falha causa |
|---|---|---|
| **CRÍTICO** | Mutação destrutiva ou load-bearing de cluster/cloud LIVE; parte do caminho de setup/deploy/recovery/migração de banco | Setup quebrado, deploy/recovery falho, perda de dados, prod afetado |
| **ALTO** | Provisiona/destrói recursos pagos (drill DOKS), injeta chaos, ou recovery/networking importante | Custo não controlado, infra drill zumbi, recovery incompleto |
| **MÉDIO** | Gate de CI / qualidade / geração / diagnóstico read-only / build de imagem | Sinal de qualidade perdido; sem dano direto à infra |
| **BAIXO** | Biblioteca sourced (sem efeito direto ao ser sourced) / helper | Quebra só se o consumidor a invocar errado |

---

## Raiz `infra/scripts/` (25)

| Script | Utilidade | Criticidade | Allowlist |
|---|---|---|---|
| `bootstrap-platform.sh` | Setup idempotente da plataforma após `pulumi up` (CRDs, releases, wiring) | **CRÍTICO** | KEEP-OPERATOR |
| `bootstrap-seed-tenant-cr.sh` | Semeia o Tenant CR de bootstrap (B26) no cluster mgmt | **ALTO** | REVIEW-NEEDED |
| `break-glass.sh` | Procedimento de emergência break-glass (MEET-055 M3) — acesso privilegiado | **CRÍTICO** | KEEP-OPERATOR |
| `build-and-push-dockerfile-lint.sh` | Build + push da imagem `loopzero-dockerfile-lint` ao registry | **MÉDIO** | KEEP-OPERATOR |
| `build-and-push-loopzero-packs.sh` | Build + push da imagem `loopzero-packs` v0.4.0 (heal H16) | **MÉDIO** | KEEP-OPERATOR |
| `check-drill-credentials-isolation.sh` | Verifica que credenciais drill estão isoladas de prod (D7) | **MÉDIO** | MIGRATE-GO-PUREFN |
| `check-prod-vap-readiness.sh` | Pré-flight LIVE antes de flipar VAP `noRuntimeImageMutations` em prod | **MÉDIO** | KEEP-OPERATOR |
| `clean-incremental.sh` | Remove builds/tenants/projects órfãos (limpeza incremental) | **CRÍTICO** | KEEP-OPERATOR |
| `dr-drill.sh` | Orquestração do DR drill (prepare→disaster→recover→verify→teardown) | **ALTO** | DELEGATE-ARGOCD |
| `drill-helmfile-bootstrap.sh` | Bootstrap prod-identical do drill (1126 LOC — provisioning orchestration) | **CRÍTICO** | DELEGATE-HELMFILE |
| `drill-mint-jwt.sh` | Minta JWT para o drill via Hydra (port-forward + curl); assert TTL≥7200s | **ALTO** | KEEP-OPERATOR |
| `ensure-project.sh` | Cria um projeto default via API se não existir | **MÉDIO** | KEEP-CI-ORCHESTRATOR |
| `health-check-platform.sh` | Valida que a plataforma está totalmente operacional (pods/API/tokens) | **MÉDIO** | KEEP-OPERATOR |
| `k3d-cross-cluster-heal.sh` | Recovery idempotente da conectividade cross-cluster local (H1-H15) | **CRÍTICO** | KEEP-OPERATOR |
| `migrate-tenant-namespace.sh` | Migração de namespace de tenant (CHG-2026-04-29 F-04) | **ALTO** | REVIEW-NEEDED |
| `operator-cutover-execute.sh` | Executa o cutover do operator split-binary (pré-flight + cutover) | **CRÍTICO** | KEEP-OPERATOR |
| `pkg-soak-smoke.sh` | Soak smoke local de `pkg/` (RFC 9457 + CB obs) vs localhost:13001 | **MÉDIO** | KEEP-OPERATOR |
| `pre-destroy-cleanup.sh` | Remove finalizers + recursos cloud ANTES de `pulumi destroy` | **CRÍTICO** | KEEP-OPERATOR |
| `provision-api-tokens.sh` | Provisiona tokens Gitea/ArgoCD + kubeconfigs cross-cluster | **CRÍTICO** | KEEP-OPERATOR |
| `regen-all-scoped-kubeconfigs.sh` | Regenera SA tokens scoped por ClusterCredential (CORRECTIONS N, T2.1) | **CRÍTICO** | KEEP-OPERATOR |
| `run-migrations.sh` | Aplica migrations forward-only do banco (golang-migrate) | **CRÍTICO** | KEEP-OPERATOR |
| `setup-cross-cluster-networking.sh` | Garante alcançabilidade entre nós dos clusters k3d (redes Docker) | **ALTO** | KEEP-OPERATOR |
| `setup-host-inotify-limits.sh` | Persiste limites de inotify do kernel (topologia k3d multi-cluster) | **MÉDIO** | KEEP-OPERATOR |
| `update-cross-cluster-ips.sh` | Atualiza IPs cross-cluster em todos os manifests | **ALTO** | KEEP-OPERATOR |
| `velero-dr-drill.sh` | Drill de validação de backup/restore Velero (T4.2) | **ALTO** | REVIEW-NEEDED |

## `infra/scripts/chaos/` (5)

| Script | Utilidade | Criticidade | Allowlist |
|---|---|---|---|
| `baseline.sh` | Captura de baseline steady-state (curl /health + kubectl) para `/chaos-test` | **MÉDIO** | REVIEW-NEEDED |
| `cleanup.sh` | Remove TODOS os recursos chaos-mesh de um cluster-alvo (cleanup obrigatório) | **ALTO** | REVIEW-NEEDED |
| `report.sh` | Gera relatório markdown do run (recebe `--verdict`; só formatação) | **BAIXO** | REVIEW-NEEDED |
| `run-full.sh` | Roda os 10 cenários chaos sequencialmente com sumário | **ALTO** | REVIEW-NEEDED |
| `run-scenario.sh` | Orquestrador principal do `/chaos-test` (injeção chaos-mesh; verdict → `loopzero-ops chaos slo-compare`) | **ALTO** | DELEGATE-HELMFILE |

## `infra/scripts/dr/` (11)

| Script | Utilidade | Criticidade | Allowlist |
|---|---|---|---|
| `cancel-refresh.sh` | Cancela operação pulumi presa + refresh do state do drill | **CRÍTICO** | REVIEW-NEEDED |
| `chaos-orchestrator.sh` | Sequencia chaos no drill (skill-driven) | **ALTO** | DELEGATE-HELMFILE |
| `cleanroom-orchestrator.sh` | Sequencia cleanroom no drill (skill-driven) | **ALTO** | REVIEW-NEEDED |
| `cost-report.sh` | Calcula custo do drill (aritmética delega a `loopzero-ops drill cost`; `--auto` lê pulumi) | **MÉDIO** | REVIEW-NEEDED |
| `dogfood-orchestrator.sh` | Sequencia o `/dogfood` no drill via `$LOOPZERO` CLI (skill-driven) | **ALTO** | REVIEW-NEEDED |
| `evidence-capture.sh` | Coleta evidências do drill (kubectl/doctl/pulumi + git commit), fault-tolerant | **MÉDIO** | KEEP-OPERATOR |
| `jwt-diagnose.sh` | Diagnóstico read-only de 401 JWT (Hydra vs loopzero-api) — decode + kubectl logs | **MÉDIO** | REVIEW-NEEDED |
| `preflight.sh` | Pré-flight do drill (credential-isolation + cluster-reachability) | **MÉDIO** | REVIEW-NEEDED |
| `state-reset.sh` | Reset do state pulumi/doctl do drill (42 chamadas de orquestração) | **CRÍTICO** | REVIEW-NEEDED |
| `teardown-validator.sh` | Valida zero recursos drill residuais + force-delete via doctl (custo-crítico) | **CRÍTICO** | KEEP-OPERATOR |
| `watchdog.sh` | Cron de auto-destroy do drill (destrói stack se exceder TTL) | **CRÍTICO** | REVIEW-NEEDED |

## `infra/scripts/lib/` (7) — bibliotecas sourced

| Script | Utilidade | Criticidade | Allowlist |
|---|---|---|---|
| `bootstrap-cell-cc-common.sh` | Lib compartilhada para `bootstrap-cell-{default,1,2}-cc.sh` | **BAIXO** | REVIEW-NEEDED |
| `bootstrap-cell-cc-drill-common.sh` | Lib compartilhada de ClusterCredential cell para o drill | **BAIXO** | REVIEW-NEEDED |
| `drill-dogfood-journeys.sh` | Lib de jornadas `$LOOPZERO` para o dogfood do drill | **BAIXO** | REVIEW-NEEDED |
| `drill-jwt-gate.sh` | Gate de auth e2e (`verify_jwt_auth_e2e`: minta + curl LIVE loopzero-api) — sourced | **MÉDIO** | KEEP-OPERATOR |
| `drill-skill-common.sh` | Helpers compartilhados das skills de drill | **BAIXO** | REVIEW-NEEDED |
| `dry-run.sh` | Helper `--dry-run` compartilhado para scripts que escrevem state | **BAIXO** | KEEP-OPERATOR |
| `sops.sh` | Helper de criptografia SOPS (manuseia secrets) | **MÉDIO** | KEEP-OPERATOR |

## `infra/scripts/quality/` (4)

| Script | Utilidade | Criticidade | Allowlist |
|---|---|---|---|
| `branch-protection-update.sh` | Ativa regras de branch protection via `gh` (EC-1) | **MÉDIO** | KEEP-CI-ORCHESTRATOR |
| `coverage-cli.sh` | Coleta cobertura TS/JS do `cli/` via Jest --coverage | **MÉDIO** | KEEP-CI-ORCHESTRATOR |
| `coverage-go.sh` | Coleta cobertura Go em todos os pacotes de um diretório | **MÉDIO** | KEEP-CI-ORCHESTRATOR |
| `test-inventory.sh` | Cataloga arquivos de teste do repo por categoria (ADR D7) | **MÉDIO** | KEEP-OPERATOR |

---

## Resumo por criticidade

| Nível | Qtd | Observação |
|---|---|---|
| **CRÍTICO** | 12 | Setup/recovery/migração/destroy LIVE — nunca rodar sem entender o efeito |
| **ALTO** | 12 | Drill (custo) + chaos injection + networking — atenção a custo/orphans |
| **MÉDIO** | 16 | CI/qualidade/diagnóstico/build — sem dano direto à infra |
| **BAIXO** | 5 | Libs sourced — efeito só via consumidor |
| **(outros REVIEW-NEEDED)** | — | Disposição allowlist a ser reclassificada em fases futuras do programa de minimização |

> **Nota:** "Allowlist" reflete a disposição em `.claude/rules/shell-scripts-allowlist.md`
> (KEEP-OPERATOR = mantido por necessidade operacional; MIGRATE-* = alvo de migração;
> REVIEW-NEEDED = ainda não reclassificado). Os scripts de drill `cost-report`/`chaos
> run-scenario` já tiveram sua **lógica pura** extraída para `loopzero-ops drill cost` /
> `loopzero-ops chaos slo-compare` (Track C); o bash remanescente é a orquestração
> (pulumi/doctl/kubectl/curl) que permanece KEEP por design (ADR D1 no-shell-wrap).
