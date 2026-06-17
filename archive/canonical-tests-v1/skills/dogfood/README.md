# Dogfood Skill — Guia de Uso

Audita o LoopZero como usuario real de caixa-preta usando o CLI `loopzero` compilado localmente.
Nao altera codigo, banco, config ou infra. CLI-first: toda acao usa `$LOOPZERO`, nunca curl quando CLI existe.

## Uso Rapido

```bash
# Teste completo (todas as jornadas + integracao vertical + observabilidade)
/dogfood full

# Teste do sprint atual
/dogfood

# Teste de um sprint especifico
/dogfood sprint-2

# Teste focado no fluxo de deploy
/dogfood deploy-flow

# Teste focado em login/auth
/dogfood login
```

## Escopos Disponiveis

| Escopo | O que testa | Quando usar |
|--------|------------|-------------|
| `full` | TUDO: jornadas + integracao vertical + observabilidade + todos os comandos + controllers + supply chain + SSE + cross-cutting | Antes de release ou milestone |
| `sprint-N` | Jornadas do sprint N | Ao finalizar um sprint |
| `deploy-flow` | Fluxo completo: login → init → deploy → prova de vida + SSE | Apos mudancas no pipeline de deploy |
| `promote` | Deploy staging → promote staging→production → PV no destino | Apos mudancas em promote |
| `preview` | PR webhook → preview criada → PV → TTL → cleanup | Apos mudancas em preview envs |
| `domains` | `loopzero domain add/verify/status/dns/list/remove` + state machine | Apos mudancas em custom domains |
| `multi-cell` | Cell metrics, ClusterCredential, DynamicRouter, routing | Apos mudancas em multi-cell |
| `static` | Deploy app estatico → S3 → CF KV → CF Worker | Apos mudancas em static delivery |
| `concurrent` | 2 deploys simultaneos → 1×202+1×409, lock, idempotency | Apos mudancas em concorrencia |
| `login` | Apenas autenticacao/sessao | Apos mudancas em auth |
| `scale` | `loopzero scale` replicas fixas + autoscaling + limites por plan | Apos mudancas em scaling/HPA |
| `connect` | `loopzero connect/disconnect` + auto-deploy via GitHub webhook | Apos mudancas em GitHub integration |
| `billing` | `loopzero billing` + plan enforcement + limites | Apos mudancas em billing/planos |
| `audit` | `loopzero audit` + entries apos acoes criticas | Apos mudancas em audit log |
| `platform` | `loopzero platform install/status/upgrade/uninstall/secrets` | Apos mudancas em platform mgmt |
| `admin` | `loopzero admin tenants pin/evict` + verificacao cross-layer | Apos mudancas em admin ops |
| `supply-chain` | Build attestation (cosign) + SBOM (CycloneDX) | Apos mudancas em supply chain |
| `sse` | SSE streaming de deploy + log streaming | Apos mudancas em SSE/streaming |
| `cross-cutting` | Correlation-ID + idempotency + flags + controllers | Verificacao transversal |
| `controllers` | ExposedRoute + GitRepoBinding + PlatformBootstrap | Apos mudancas em operators |
| `canary` | Rollout + TraefikService + weight stages + AnalysisTemplate | Apos mudancas em progressive delivery |
| `keda` | ScaledObject + ScaleTargetRef=Rollout + scale-to-zero | Apos mudancas em KEDA/autoscaling |
| `state-recovery` | Release SUPERSEDED/ROLLED_BACK/ARCHIVED + 1-ACTIVE invariant | Apos mudancas em state machines |
| `enforcement` | 4 plan caps isolados (project/build/env/seat) + HTTP 402 | Apos mudancas em billing |
| *(vazio)* | Infere do sprint atual | Uso diario |

## O que a Skill Faz (Etapas)

### Etapa 0 — Pre-Flight Check
Verifica que a infraestrutura esta operacional ANTES de testar:
- Clusters k3d (mgmt, build, workload) com nodes Ready
- Pods criticos Running (API, operator, gitea, postgres, minio, argocd, argo-workflows)
- API respondendo health check
- Migrations aplicadas
- Conectividade cross-cluster (build → mgmt)
- Registry acessivel de todos os clusters
- ArgoCD com credenciais Gitea configuradas
- Operator sem erros em loop

**Se qualquer check falhar, registra como achado S0.**

### Etapa 1 — Reconhecimento
Le CLAUDE.md (sprint plan + regras) e o código relevante para entender o comportamento esperado.

### Etapa 2 — Planejamento
Cria cenarios de teste com tipos obrigatorios:

| Tipo | O que valida |
|------|-------------|
| **Fluxo E2E** | Jornada completa com objetivo real |
| **Integracao Vertical** | CLI → API → DB → Operator → K8s → ArgoCD → Workload |
| **Prova de Vida** | Pod Running + health OK apos deploy exit 0 |
| Repeticao | Repetir tarefas para detectar inconsistencia |
| Erro realista | Provocar erros de usuario comum |
| Consistencia | Mesmo conceito aparece igual em todos os lugares |

### Etapa 3 — Execucao
Executa cenarios usando APENAS interfaces de usuario:
- CLI: `loopzero login`, `loopzero init`, `loopzero deploy`, `loopzero project delete`, etc.
- API publica: `curl` em `GET/POST/DELETE /api/v1/*`
- Arquivos gerados: `loopzero.yaml`, `~/.loopzero/config.json`

**Para testes de deploy, usa app real com `/health` endpoint.**

**Apos CADA deploy exit 0, executa Prova de Vida:**
1. Pod existe e esta Running
2. Container Ready (1/1)
3. Health endpoint responde
4. ArgoCD mostra Synced + Healthy
5. Nenhum erro nos logs da API

**Apos CADA project delete exit 0, executa Prova de Morte:**
1. Pod removido (No resources found)
2. Diretorio removido do repo GitOps
3. ArgoCD health normalizado (Healthy, nao Degraded)
4. Builds cancelados (nenhum QUEUED/RUNNING)
5. Nenhum erro nos logs da API

### Etapa 3.5 — Acelerador Ginkgo (RECOMENDADO para scope "full")

Roda a suite Ginkgo E2E como baseline antes da avaliacao qualitativa:

```bash
# Baseline automatico (pass/fail de todas as jornadas)
task e2e:journey

# Edge cases (concorrencia, falhas, state machine)
task e2e:edge

# Tudo com JUnit XML
task e2e:report
```

O auditor usa os resultados Ginkgo para focar a execucao manual:
- Jornadas que PASSARAM → verificar Prova de Vida completa + UX qualitativa
- Jornadas que FALHARAM → investigar manualmente, classificar por severidade

### Etapa 4 — Observabilidade
Verifica saude interna: logs, metricas, operator status, pods em erro.

### Etapa 5-7 — Registro e Relatorio
Classifica achados por severidade (S0-S4), categoria e camada afetada.
Gera relatorio com prioridades, padroes sistemicos e veredito final.

## Regra Suprema

**A skill NUNCA altera o sistema.** Proibido editar codigo, config, banco, ou infra. Apenas observa, executa como usuario, e reporta.

## Anti-Patterns (aprendidos de dogfoodings anteriores)

1. NAO aceite exit 0 como prova de sucesso — sempre execute Prova de Vida
2. NAO teste apenas CLI isolado — teste a cadeia completa
3. NAO ignore logs do operator/API — erros silenciosos sao os mais perigosos
4. NAO teste deploy com diretorio vazio — use app real com `/health`
5. NAO assuma que a infra esta OK — execute Pre-Flight Check
6. NAO pare no primeiro erro — continue para encontrar TODOS os problemas
7. NAO confunda "testes unitarios passam" com "sistema funciona"
8. NAO deixe projetos orfaos entre rodadas — use `loopzero project delete` para limpar
9. NAO aceite delete exit 0 sem Prova de Morte — pod pode ainda estar Running

## Categorias de Achados

| Categoria | Quando usar |
|-----------|------------|
| Funcional | Feature nao funciona como esperado |
| UX | Friccao, confusao, feedback ruim |
| **Integracao** | Componentes funcionam isolados mas a cadeia esta quebrada |
| **Observabilidade** | Erros silenciosos, logs insuficientes, metricas ausentes |
| **Infraestrutura** | RBAC, networking, registry, storage, cross-cluster |
| Consistencia | Nomenclatura, formato, comportamento inconsistente |
| Performance | Lentidao percebida em pontos criticos |
| Confianca | Usuario nao entende o que aconteceu/acontece |

## Criterio de "Pronto para Uso Real"

Todos devem ser verdadeiros:
- [ ] Fluxo `login → init → deploy` retorna exit 0
- [ ] Prova de Vida passa (pod Running, health OK, ArgoCD Synced)
- [ ] Fluxo `project delete` retorna exit 0 e Prova de Morte passa
- [ ] Nenhum achado S0 aberto
- [ ] Observabilidade permite diagnosticar problemas sem ler codigo

## Exemplos de Uso em Contexto

```bash
# Finalizou sprint 2 — auditar antes de marcar como done
/dogfood sprint-2

# Mudou algo no pipeline de build — verificar deploy ponta a ponta
/dogfood deploy-flow

# Release candidate — auditar tudo
/dogfood full

# Mudou auth middleware — verificar login
/dogfood login

# Nao sabe qual sprint — deixa a skill inferir
/dogfood
```

## Integracao com Ralph Loop

Para auditoria autonoma com iteracao:

```bash
/start-loop "/dogfood full" --completion-promise "AUDIT COMPLETE" --max-iterations 5
```

A skill gera o relatorio, e se a promise nao for verdadeira (achados S0 abertos), o loop continua na proxima iteracao.
