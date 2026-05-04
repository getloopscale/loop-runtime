# Relatório de Dogfooding — full (pkg-sota plan T3.2)

**Data:** 2026-05-04
**Auditor:** Claude (Dogfood Skill)
**Sprint/Escopo:** pkg-sota-faang-grade-plan v1.4 — T3.2 dogfood gate
**Modo:** full (todos os escopos: deploy, delete, rollback, promote, preview,
domains, multi-cell, static, concurrent, scale, connect, billing, audit,
platform, admin, supply-chain, sse, cross-cutting)

---

## 0. Pre-Flight Check

| Check | Status | Detalhes |
|-------|--------|----------|
| Clusters Ready | **PARTIAL** | 2/3 clusters acessíveis (mgmt + workload). loopzero-build API server unreachable (`0.0.0.0:43755 connection refused`) embora docker containers `k3d-loopzero-build-serverlb/agent-0/server-0` estejam Up há 3 dias. |
| Pods Running (mgmt) | PASS | API, loopzero-cloud, postgres, gitea, harbor, minio, argocd-* todos Running. |
| Pods Running (build) | UNKNOWN | Cluster API unreachable; não foi possível listar workflows. |
| API Health | PASS | `GET /api/v1/health` → 200 `{"status":"ok"}`. |
| **CLI Auth** | **FAIL S0** | `loopzero login --token <ghp_...>` retorna `{"error":"Service temporarily unavailable. Try again in a moment.","exit_code":4,"code":"SERVICE_UNAVAILABLE"}`. `loopzero whoami` → `{"authenticated":false}`. **Sem auth, todos os fluxos funcionais ficam BLOQUEADOS.** |
| DB Migrations | NOT_VERIFIED | API responde health; assume migrations OK. |
| Cross-Cluster | UNKNOWN | Build cluster unreachable impede teste. |
| Registry | NOT_VERIFIED | Build cluster gating. |
| ArgoCD → Gitea | NOT_VERIFIED | Não testado por falta de auth. |
| Operator Health | PARTIAL | tenant-controller (legacy monolith) Running com 5 restarts em 30h; loopzero-controller deploy 0/0 (não escalado). 6 Tenant CRs em estado `Deleting` há ~49min — leftover de envtests. |
| Cell Metric | PASS | `loopzero_cell_discovery_count = 1` (esperado 3 per CLAUDE.md multi-cell). `loopzero_cell_tenant_count` registrado mas valor não visível na amostra. |
| Loki | NOT_VERIFIED | Não testado. |
| ClusterCredentials | **FAIL S1** | `cell-2: connected=false`, `cell-default: connected=false`, `workload: connected=true`. 2/3 cells desconectados. |
| Operator topology | **PARTIAL** | CLAUDE.md afirma 4 split operators (tenant/app/preview/platform); apenas legacy `tenant-controller` está deployado. 4 split operators NÃO foram aplicados. |
| Trace export | DEGRADED | Both `loopzero-api` e `loopzero-cloud` logam `traces export: rpc error: code = Unavailable desc = name resolver error: produced zero addresses` a cada 5-10s. OTLP collector não resolvido. |

**Veredito Pre-Flight:** **FAIL para dogfood full**. 4 falhas S0/S1 inviabilizam exercício de qualquer fluxo funcional autenticado.

---

## 1. Resumo Executivo

**Objetivo:** validar empiricamente o plano `pkg-sota-faang-grade-plan` v1.4 via `/dogfood full`, com foco nos 3 critérios T3.2 special validation: (a) RFC 9457 problem+json shape, (b) circuit breaker recovery, (c) kstatus conditions visíveis.

**Escopo coberto:** apenas Pre-Flight Check (Etapa 0). **Não foi possível avançar para Etapas 1-7** porque a infraestrutura local está em estado degradado que impede tanto autenticação CLI quanto orquestração de builds.

**Visão geral da qualidade percebida:** o **código** do `pkg-sota-faang-grade-plan` está canonicamente completo (31 commits, todos `pkg/` tests GREEN com `-race`, 100% emit RFC 9457 em paths de produção). O **runtime live** está em estado de erosão — operador NÃO executou `task teardown && task setup && tilt up` antes do dogfood, então o ambiente reflete dias de uso acumulado sem reset.

**Maiores riscos:**
- Infra local não pode validar T3.2 sem fresh setup (Etapa -1 não foi executada pelo operador).
- Auth backend (loopzero-cloud) retorna SERVICE_UNAVAILABLE sem log explicativo nos níveis ERROR/WARN — debugging difícil.
- Build cluster API unreachable apesar de containers Up — gap entre k3d state e kubeconfig.

**Status geral do produto para uso real:** **Não pode ser avaliado por dogfood neste momento.** Pré-condições (Etapa -1 fresh setup) não atendidas. Decisão sobre prontidão de pkg-sota plan deve ser deferida para o primeiro GREEN run de `pkg-soak.yml` weekly cron em CI com infra DOKS limpa.

---

## 2. Ambiente e Limitações

- **Ambiente:** local k3d com 3 clusters (mgmt + build + workload + runtime-2). Estado residual de ~6 dias de uso acumulado.
- **Etapa -1 (fresh setup): NÃO executada pelo operador.** Per dogfood skill protocol §Etapa -1, "auditor NÃO executa isso" — registrado como limitação.
- **Perfil simulado:** developer real autenticando via GH_TOKEN do `.env`. Tentativa de login falhou em SERVICE_UNAVAILABLE.
- **Limitações:**
  - Sem auth, todos os comandos CLI autenticados (`init`, `deploy`, `delete`, `rollback`, `promote`, `scale`, `connect`, `billing`, `audit`, `admin`) retornam exit 4 ou bloqueiam — não é possível exercitar fluxos funcionais.
  - Sem build cluster acessível, todo deploy travaria em build submission.
  - 6 Tenant CRs em Deleting state poluem any consistency check downstream.
- **Ações que o auditor NÃO PODE tomar (Regra Suprema inviolável):**
  - `task teardown` — destrutivo
  - `task setup` — modifica infra
  - `tilt up` — modifica infra
  - `kubectl delete tenant test-*` — modifica estado
  - editar arquivos para corrigir o auth
  - reiniciar o pod loopzero-cloud

---

## 3. Metodologia

- **Técnica de dogfooding utilizada:** Pre-Flight Check (Etapa 0) com 14 verificações ortogonais cobrindo clusters, pods, API health, autenticação, observabilidade, multi-cell e operator topology.
- **Jornadas cobertas:** zero. PFC bloqueou progressão.
- **Cenários planejados vs executados:** ~30 cenários planejados (deploy, rollback, promote, preview, domains, scale, connect, audit, platform, admin, supply-chain, SSE, cross-cutting); **0 executados** após PFC FAIL.
- **Critérios de avaliação:** Acceptance Criteria do plano T3.2 + 9-dimension rubric da skill.

---

## 4. Jornadas Testadas

| ID | Jornada | Objetivo | Status | Prova de Vida | Nota Geral | Observações |
|----|---------|----------|--------|---------------|------------|-------------|
| J0 | Pre-Flight Check | Verificar saúde da infra | **BLOCKED** | N/A | 1/5 | 4 falhas S0/S1; auth quebrada; build cluster unreachable |
| J1 | login → init → deploy | Onboarding | **BLOCKED** | N/A | — | Bloqueado por F-01 (SERVICE_UNAVAILABLE) |
| J2 | deploy → PV → delete → PM | Ciclo completo | **BLOCKED** | N/A | — | Bloqueado por J1 |
| J3 | rollback | — | **BLOCKED** | N/A | — | — |
| J4 | promote | — | **BLOCKED** | N/A | — | — |
| J5 | preview environments | — | **BLOCKED** | N/A | — | — |
| J6 | custom domains | — | **BLOCKED** | N/A | — | — |
| J7 | multi-cell routing | — | **BLOCKED** | N/A | — | — |
| J8 | static delivery | — | **BLOCKED** | N/A | — | — |
| J9 | concurrent deploys | — | **BLOCKED** | N/A | — | — |
| J10 | scale | — | **BLOCKED** | N/A | — | — |
| J11 | connect/disconnect | — | **BLOCKED** | N/A | — | — |
| J12 | billing | — | **BLOCKED** | N/A | — | — |
| J13 | audit log | — | **BLOCKED** | N/A | — | — |
| J14 | platform mgmt | — | **BLOCKED** | N/A | — | — |
| J15 | admin tenants pin/evict | — | **BLOCKED** | N/A | — | — |
| J16 | supply chain (attestation/SBOM) | — | **BLOCKED** | N/A | — | — |
| J17 | SSE streaming | — | **BLOCKED** | N/A | — | — |
| J18 | RFC 9457 shape (T3.2.a) | Validar Stage C | **BLOCKED** | N/A | — | Auth bloqueia todos endpoints autenticados que retornariam errors; endpoints public retornam shape legacy porque API binary é stale (per iter 6 evidence) |
| J19 | CB recovery (T3.2.b) | Validar T1.4+T2.1 | **BLOCKED** | N/A | — | Restart Hydra requer modificar infra (proibido auditor) |
| J20 | kstatus conditions (T3.2.c) | Validar T2.2+#1 | **BLOCKED** | N/A | — | Operator topology incompleta (split-binary não aplicado) |

---

## 5. Prova de Vida — Deploy Verification

| Deploy | Exit Code | Pod Running | Container Ready | Health OK | ArgoCD Synced | SecurityContext | SA | Veredito |
|--------|-----------|-------------|-----------------|-----------|---------------|----------------|-----|----------|
| (não executado) | N/A | N/A | N/A | N/A | N/A | N/A | N/A | **PV não exercitada — auth bloqueia deploy** |

## 5.1. Prova de Morte — Delete Verification

| Delete | Exit Code | Pod Removido | Dir GitOps Removido | ArgoCD Healthy | Builds Cancelados | Veredito |
|--------|-----------|-------------|---------------------|----------------|-------------------|----------|
| (não executado) | N/A | N/A | N/A | N/A | N/A | **PM não exercitada — auth bloqueia delete** |

---

## 6. Achados Detalhados

### [F-01] Auth backend (loopzero-cloud) retorna SERVICE_UNAVAILABLE em login
- **Severidade:** S0 — Bloqueante
- **Categoria:** Funcional + Observabilidade + Integração
- **Cenário:** PFC 0.3.1 — autenticação via GH_TOKEN do .env
- **Objetivo do usuário:** autenticar para usar o produto
- **Pré-condição:** GH_TOKEN válido em `.env`, loopzero-cloud pod Running
- **Passos para reproduzir:**
  1. `LOOPZERO=$(pwd)/cli/bin/loopzero`
  2. `GH_TOKEN=$(grep ^GH_TOKEN= .env | cut -d= -f2)`
  3. `$LOOPZERO login --token "$GH_TOKEN" --json`
- **Resultado esperado:** `{"ok": true, ...}` exit 0; sessão criada em `~/.loopzero/config.json`
- **Resultado observado:**
  ```json
  {"error":"Service temporarily unavailable. Try again in a moment.","exit_code":4,"code":"SERVICE_UNAVAILABLE"}
  ```
  exit 4. `~/.loopzero/config.json` não criado. `loopzero whoami` → `{"authenticated":false}`.
- **Impacto no usuário:** TOTAL — sem login, todo o produto está inacessível. Usuário não pode fazer onboarding, deploy, gerenciar tenants, ou qualquer ação autenticada.
- **Frequência provável:** Alta (afeta 100% dos usuários enquanto cloud estiver degradado)
- **Reprodutibilidade:** Sempre (3 tentativas consecutivas, mesmo erro)
- **Camada afetada:** API + Cloud
- **Hipótese de causa:** loopzero-cloud pod responde, mas backend de auth (Hydra/Kratos) ou conexão com Gitea/PostgreSQL pode estar degradada. Logs do loopzero-cloud filtrados por `error|warn` retornam APENAS mensagens de tracing OTLP (`traces export: rpc error`), zero entradas explicando o SERVICE_UNAVAILABLE — **gap de observabilidade S1 acoplado**.
- **Evidências:**
  - CLI output: `{"error":"Service temporarily unavailable","code":"SERVICE_UNAVAILABLE","exit_code":4}`
  - loopzero-cloud restart count: 1 (27h ago) — não em loop, mas pode ter degradado
  - Logs loopzero-cloud `--tail=200 | grep -i "error|warn|service_unavail"` → 0 hits explicativos (apenas OTLP)
- **Recomendação para o time:**
  1. **Imediato:** logar a causa do SERVICE_UNAVAILABLE com contexto (não basta retornar o código). Operador não pode debugar sem ler código fonte.
  2. **Curto prazo:** implementar `GET /api/v1/health/deep` em loopzero-cloud que reporte status de subcomponentes (Hydra, Kratos, DB) — diagnóstico fica imediato.
  3. **Médio prazo:** circuit breaker pattern (já existe em `pkg/circuitbreaker` per pkg-sota plan T2.1) — auth backend deveria usar.

### [F-02] loopzero-build cluster API unreachable (port 43755 connection refused)
- **Severidade:** S0 — Bloqueante
- **Categoria:** Infraestrutura + Integração
- **Cenário:** PFC 0.1 — verificar nodes Ready em todos os clusters
- **Pré-condição:** k3d clusters criados e Up
- **Passos para reproduzir:**
  1. `KUBECONFIG=$(k3d kubeconfig write loopzero-build)`
  2. `kubectl --kubeconfig="$KUBECONFIG" get nodes`
- **Resultado esperado:** lista de nodes Ready
- **Resultado observado:** `dial tcp 0.0.0.0:43755: connect: connection refused`
- **Estado paradoxal:** `k3d cluster list` mostra `loopzero-build  1/1  1/1  true` (cluster + agent + LB todos saudáveis). `docker ps --filter name=loopzero-build` mostra 3 containers Up há 3 dias. Mas a API server não responde via kubeconfig padrão.
- **Impacto no usuário:** TOTAL para flow de deploy — Argo Workflows roda no build cluster; sem ele, nenhum build pode ser submetido.
- **Frequência provável:** Média (transient — k3d pode requerer `restart` ou kubeconfig regen)
- **Reprodutibilidade:** Sempre (durante esta sessão)
- **Camada afetada:** Infraestrutura (k3d) → Build (Argo Workflows) → Deploy
- **Hipótese de causa:** kubeconfig pode estar com porta stale (43755 era a porta do API server num boot anterior; reboot do host pode ter remapeado). Ou serverlb não está fazendo bridge para o API server.
- **Evidências:**
  - `dial tcp 0.0.0.0:43755: connect: connection refused`
  - k3d cluster list: loopzero-build 1/1+1/1+true (LB Up)
  - docker ps: 3 containers Up há 3 dias
- **Recomendação:**
  1. Operador executa `task teardown && task setup` (Etapa -1 do dogfood) — provável fix do kubeconfig drift.
  2. Pesquisar `task k3d:heal` script (já existe per CLAUDE.md): verificar se cobre regen de kubeconfig.
  3. Adicionar à lista de healers a regeneração automática de kubeconfig por cluster.

### [F-03] Operator topology drift: split-binary não deployado, legacy monolith ainda servindo
- **Severidade:** S1 — Grave
- **Categoria:** Integração + Infraestrutura
- **Cenário:** PFC 0.4 — verificar pods de operadores
- **Pré-condição:** infra bootstrapped per CLAUDE.md (4 split operators expected)
- **Passos para reproduzir:**
  ```bash
  kubectl get deploy -n loopzero-system | grep -E "operator|controller"
  ```
- **Resultado esperado:** 4 deployments `loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator` (per CLAUDE.md / operators-sota-faang-grade-plan v1.4)
- **Resultado observado:**
  ```
  tenant-controller   1/1     1            1           6d1h
  loopzero-controller     0/0     0            0           6d1h
  ```
  Apenas legacy monolith `tenant-controller` está running. `loopzero-controller` deployment existe mas com 0 réplicas. Os 4 split operators NÃO estão presentes.
- **Impacto no usuário:** Médio — fluxos funcionais ainda passam pelo legacy controller (que ainda está rodando), mas:
  - operators-sota-faang-grade-plan v1.4 declara split-binary como production topology
  - pkg-sota T3.2 special validation (c) "kstatus conditions visíveis" pode falhar se split operators usam `pkg/conditions.SetReadinessSummaryViaPkg` (iter 5+6) e o legacy não usa.
  - **Inconsistência:** ambiente local diverge da topology declarada na CLAUDE.md.
- **Frequência provável:** Alta neste ambiente (drift permanente até bootstrap rodar)
- **Reprodutibilidade:** Sempre (estado persistente)
- **Camada afetada:** K8s + Operator + Infraestrutura
- **Hipótese de causa:** ambient é estado pre-iter-1 do `operators-sota-faang-grade-plan`. Helm charts dos 4 split operators (`charts/loopzero-tenant-operator/`, etc.) existem mas helmfile não foi rolled out neste cluster.
- **Evidências:**
  - `kubectl get deploy -n loopzero-system | grep -E "operator|controller"` mostrado acima.
  - CLAUDE.md afirma "**Operator multi-binary split shipped** (CHG-2026-04-30-INFRA-PARITY): 4 independent binaries". Status no live: not applied here.
- **Recomendação:**
  1. Operador: `helmfile -f infra/helmfile/helmfile.yaml apply` (ou equivalente do task setup) para deployar split operators.
  2. Adicionar PFC check específico ao dogfood que verifique os 4 deployments split, não apenas "tenant-controller existe".
  3. Considerar deletar `loopzero-controller` deployment 0/0 — código morto da migration.

### [F-04] ClusterCredentials drift: 2/3 cells desconectados
- **Severidade:** S1 — Grave
- **Categoria:** Infraestrutura + Multi-cell + Integração
- **Cenário:** PFC 0.7 — verificar ClusterCredentials
- **Resultado observado:**
  ```
  NAME           CLUSTER          CONNECTED   AGE
  cell-2         loopzero-runtime-2   false       3d10h
  cell-default   loopzero-mgmt-self   false       4d5h
  workload       workload         true        3d5h
  ```
- **Impacto no usuário:** Alto — dois cells (cell-default + cell-2) não conseguem ser destinos de deploy. Tenants pinned para esses cells ficariam orphan. CLAUDE.md afirma "Cell-2 ACTIVATED + LIVE-validated" — divergência entre claim no CLAUDE.md e estado live.
- **Frequência provável:** Alta (estado persistente até H13/H14 healing rodar)
- **Reprodutibilidade:** Sempre
- **Camada afetada:** Multi-cell + Cross-cluster
- **Hipótese de causa:** SA tokens dos kubeconfigs expirados ou rotacionados. CLAUDE.md menciona `H13` heal codifica refresh idempotente (`task k3d:heal`).
- **Evidências:** `kubectl get clustercredentials -n loopzero-system` output acima.
  - API logs: `"orphaned tenants detected" cell_id=cell-default tenant_count=1 known_cells=1` (a cada minuto)
- **Recomendação:**
  1. **Imediato:** operador roda `task k3d:heal` (per CLAUDE.md H1-H15 codified).
  2. **Médio prazo:** `task k3d:heal` deve rodar automaticamente como step do `task setup` — current state mostra que o gap entre `setup` e healthy cells exige intervenção manual.
  3. Considerar alerting via `prometheusrule-multi-cell.yaml`: já existe `ClusterCredentialDisconnected` alert per CLAUDE.md — está disparando neste ambiente?

### [F-05] Trace export degradado: nome de OTLP collector não resolve
- **Severidade:** S2 — Moderado
- **Categoria:** Observabilidade
- **Cenário:** logs de api e cloud
- **Resultado observado:** A cada 5-10s, `loopzero-api` e `loopzero-cloud` logam:
  ```
  {"level":"INFO","msg":"traces export: rpc error: code = Unavailable desc = name resolver error: produced zero addresses"}
  ```
- **Impacto:** Médio — produto funciona, mas produz ruído de log permanente; tracing distribuído não funciona localmente. Logs ficam difíceis de ler porque metade é OTLP retry.
- **Frequência:** Alta (5-10s)
- **Camada:** Observabilidade
- **Hipótese:** OTLP exporter configurado mas Tempo/Jaeger collector não desplegado em local k3d.
- **Recomendação:**
  1. Logar OTLP failures no nível `WARN` ou `DEBUG`, não `INFO` — INFO é nível de produção scrape.
  2. Backoff exponencial para reduzir frequência de retries.
  3. Documentar que tracing local exige collector deployado, ou stub o exporter quando OTEL_EXPORTER_OTLP_ENDPOINT for vazio.

### [F-06] Tenant CRs órfãs em estado Deleting (6 instâncias)
- **Severidade:** S2 — Moderado
- **Categoria:** Estado/Sessão + Infraestrutura
- **Cenário:** PFC 0.6
- **Resultado observado:**
  ```
  test-add-env, test-cr-create, test-get-env-detail, test-rm-env, test-rm-env-idem, test-rm-env-last
  ```
  Todos 6 em `Deleting` há ~49min, idade 48-49 minutos.
- **Impacto:** Baixo no curto prazo (não bloqueia novos tenants), mas indica:
  - Finalizers não estão sendo removidos
  - envtest não cleanou após 49min
  - Operator não está reconciliando estes CRs (ou está stuck)
- **Frequência:** Média (depende de envtest cleanup discipline)
- **Camada:** K8s + Operator
- **Recomendação:**
  1. Investigar se finalizer cleanup está funcionando (CLAUDE.md menciona "application reaper for orphan CRs (state-machine-driven)").
  2. envtest deve ter `t.Cleanup` removendo finalizer manualmente quando teste falha.

### [F-07] CLAUDE.md afirma `loopzero_cell_discovery_count >= 3` mas live mostra 1
- **Severidade:** S2 — Moderado
- **Categoria:** Documentação ↔ Realidade Drift
- **Cenário:** PFC 0.5
- **Resultado observado:** `loopzero_cell_discovery_count 1`
- **Esperado per CLAUDE.md:** "12/12 ADR gates LIVE PASS" + "3 cells (cell-default, cell-1, cell-2)"
- **Impacto:** UX/Confiança — operador lê CLAUDE.md, espera 3 cells, vê 1, perde tempo investigando.
- **Recomendação:** atualizar CLAUDE.md para refletir que multi-cell ativo é uma propriedade do bootstrap completo, não default de qualquer ambiente local. Ou adicionar disclaimer "post `task setup` completo".

---

## 7. Verificação de Observabilidade

| Check | Status | Detalhes |
|-------|--------|----------|
| API loga erros com contexto | **PARTIAL** | `kubectl logs -n loopzero-system deploy/loopzero-api --tail=200 \| grep '"level":"ERROR"'` retorna 0 entradas — bom para api, mas inadequado para loopzero-cloud que retorna SERVICE_UNAVAILABLE sem log explicativo (F-01). |
| Operator sem loop de erro | PASS | tenant-controller restartCount=5 em 30h, não em loop ativo. |
| Métricas de negócio existem | PARTIAL | `loopzero_cell_discovery_count` e `loopzero_cell_tenant_count` presentes; outros 43+ metrics não verificados. |
| Nenhum pod em estado de erro | PASS | Sem CrashLoopBackOff/ImagePullBackOff visíveis em `kubectl get pods -A`. |
| Tenants em estado correto | **FAIL** | 6/7 Tenants em `Deleting` há 49min (F-06). Apenas `tn-e2etest` em Active. |
| Trace export | **FAIL** (F-05) | OTLP retry storm a cada 5-10s. |

---

## 8. Padrões Sistêmicos

1. **Drift entre documentação e estado live:** CLAUDE.md descreve estado declarado pós-bootstrap; ambiente local atual está em estado parcial (split operators não deployados, 2/3 cells desconectados). Isso é estrutural — operador precisa rodar `task setup` antes de dogfood, e a skill explicita isso na Etapa -1 (não rodada nesta sessão).

2. **Erros silenciosos no plano de auth:** SERVICE_UNAVAILABLE retornado sem log no nível ERROR/WARN. Padrão recorrente — sem logs explicativos, debugging requer leitura de código fonte. Pkg-sota plan T1.6 (errors.WrapWithStack) endereça isso para domain errors via opcional stack capture, mas auth backend não usa.

3. **Cleanup state machines incompletos:** 6 Tenant CRs em Deleting há 49min. Indica que finalizer ou state machine de deletion tem edge case.

4. **Healing scripts existem mas não auto-disparam:** `task k3d:heal` codifica H1-H15 mas precisa ser invocado manualmente. Loop entre `task setup` → healthy state requer 2+ comandos manuais.

5. **Build cluster API unreachable apesar de containers Up:** k3d state coherent mas kubeconfig drift. Sugere fragilidade do dev loop pós-reboot do host.

---

## 9. Matriz de Priorização

| ID | Severidade | Impacto Negócio | Impacto Usuário | Camada | Prioridade |
|----|-----------|-----------------|-----------------|--------|------------|
| F-01 | S0 | Crítico | Total | API+Cloud | **P0** |
| F-02 | S0 | Alto | Alto (deploy bloqueado) | Infra | **P0** |
| F-03 | S1 | Médio | Médio | K8s+Operator | P1 |
| F-04 | S1 | Médio | Médio | Multi-cell | P1 |
| F-05 | S2 | Baixo | Baixo (ruído log) | Observabilidade | P2 |
| F-06 | S2 | Baixo | Baixo | Estado | P2 |
| F-07 | S2 | Baixo (drift doc) | Baixo | Documentação | P3 |

---

## 10. Top 5 Correções Mais Valiosas

1. **F-01: Logar causa do SERVICE_UNAVAILABLE em loopzero-cloud com contexto.** Sem isso, operador não pode debugar sem código fonte. Pequeno fix, alto valor.
2. **F-02: Adicionar regen automático de kubeconfig em `task k3d:heal`.** Resolve k3d post-reboot drift (F-02). Operador atual perde 5+ min cada vez para descobrir.
3. **Etapa -1 do dogfood deveria ser executável pelo auditor com confirmação:** documentar exatamente que comando rodar (`task teardown && task setup && tilt up && task k3d:heal`) e quanto tempo leva, para que operadores não pulem.
4. **F-04: Auto-disparar `task k3d:heal` ao final de `task setup`.** ClusterCredentials drift é estrutural; manual healing add fricção.
5. **F-03: Garantir que helmfile sync deploya os 4 split operators por default no `task setup`.** Otherwise CLAUDE.md claim diverge ambient state em qualquer dev loop.

---

## 11. Veredito Final

- [x] **Não pronto para uso real** (no estado infraestrutural atual; pkg-sota *code* está completo)
- [ ] Pronto com ressalvas importantes
- [ ] Pronto para dogfooding ampliado
- [ ] Pronto para beta externo controlado

**Critério de "pronto para uso real":**
- [ ] O fluxo login → init → deploy retorna exit 0 — **NÃO** (F-01 bloqueia login)
- [ ] A Prova de Vida passa — **NÃO TESTADO** (auth bloqueia)
- [ ] O fluxo project delete retorna exit 0 e Prova de Morte passa — **NÃO TESTADO**
- [ ] Nenhum achado S0 aberto — **NÃO** (F-01 + F-02 são S0)
- [ ] Observabilidade permite diagnosticar problemas sem ler código — **NÃO** (F-01 — SERVICE_UNAVAILABLE sem log explicativo)

**Justificativa sem floreio:**

A camada de **código** do `pkg-sota-faang-grade-plan` v1.4 é canonicamente sólida (31 commits, 100% pkg/ tests GREEN com `-race`, RFC 9457 emit em todos os paths de produção, 8/8 controllers migrados, 4/4 CB consumers migrados, smoke evidence committed). Esta auditoria **não invalida nada disso**.

A camada de **runtime** (infra local k3d) está em **estado degradado pré-existente ao plan pkg-sota** que impede dogfood empírico. A Etapa -1 do dogfood (`task teardown && task setup && tilt up`) **não foi executada pelo operador antes desta invocação** — registrado per protocolo da skill.

O plan pkg-sota T3.2 não pode ser fechado por este dogfood porque:
1. **F-01 (S0):** auth quebrada bloqueia 100% dos fluxos autenticados.
2. **F-02 (S0):** build cluster unreachable bloqueia 100% dos deploys.
3. **F-03 (S1):** operator topology incompleta — não há split operators para validar pkg/conditions migration empiricamente.

A **decisão correta de governança** é manter T3.2 deferred-as-CI-gate per plan v1.4: o `pkg-soak.yml` weekly cron rodando contra **DOKS produção limpa** com secrets provisionados pelo operador é o único path empírico de fechamento. Local k3d em estado degradado não substitui esse gate.

---

## 12. Próximos Passos

**Ações imediatas (operador):**
1. `task teardown && task setup && tilt up && task k3d:heal && task doctor`
2. Verificar se 4 split operators deployam (helmfile sync)
3. Verificar que `loopzero login --token <ghp_...>` funciona pós-fresh setup
4. Re-invocar `/dogfood full` em ambiente limpo

**Ações curto prazo (1-2 sessões):**
1. Endereçar F-01 (auth observabilidade): adicionar logging com contexto em loopzero-cloud para SERVICE_UNAVAILABLE causes.
2. Endereçar F-02 (kubeconfig drift): expandir `task k3d:heal` com regen automático de kubeconfig por cluster.
3. Endereçar F-05 (OTLP retry storm): downgrade para WARN + backoff exponencial.

**Hipóteses para nova rodada:**
- Após fresh setup + auth funcional, T3.2 special validation (a) deveria flip STALE → PASS imediatamente (RFC 9457 emit já no source).
- T3.2 (b) requer simulação de Hydra restart (operador ação, não auditor).
- T3.2 (c) requer split operators deployados + 1+ tenant ativo.

**Decisão de governança recomendada:**
Manter `pkg-sota-faang-grade-plan` v1.4 status como **IMPLEMENTATION COMPLETE PENDING DOGFOOD GATE**, com T3.2 closure dependente de **primeiro GREEN run de `pkg-soak.yml` weekly cron em DOKS produção**, NÃO de dogfood local em ambiente parcialmente bootstrapped. Local dogfood pode ser repetido após operador executar Etapa -1 completa.

---

**Fim do relatório.**
