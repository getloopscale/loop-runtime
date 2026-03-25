# PLANO DE EXECUÇÃO v2 — HARDENING DO LOOPZERO (Production-Hardened)

**Data:** 2026-03-24
**Base:** ROADMAP.md + project-scan + verificação de código + review de Staff Engineer
**Revisão:** Incorpora 8 pontos do review: serialização, dependências entre controllers, readiness gates, reclassificação de confiança, failover real, queue depth, SLOs, estabilidade antes de features.

---

## REGRAS GLOBAIS (INVIOLÁVEIS)

### R1 — Uma variável de sistema por vez
Nunca mudar runtime + infra + pipeline no mesmo bloco. Cada bloco muda UMA categoria. Entre blocos: checkpoint de estabilidade.

### R2 — Checkpoint de estabilidade
Antes de avançar para o próximo bloco:
- [ ] 3 deploys consecutivos estáveis (build → release → ACTIVE)
- [ ] Zero crash loops nos últimos 30 minutos
- [ ] Logs do operator sem erros de reconciliação contínuos
- [ ] API health deep retornando 200

Se qualquer item falhar → NÃO avançar. Diagnosticar e corrigir primeiro.

### R3 — Rollback antes de avançar
Cada bloco define como reverter a mudança se algo quebrar. Se não tem rollback definido, a mudança não é feita.

---

## PREMISSAS VERIFICADAS

| Premissa | Evidência | Fonte |
|---|---|---|
| 5 controllers implementados mas não wired | `operators/cmd/main.go` só chama `SetupTenantController` e `SetupClusterCredentialController` | main.go:137-150 |
| Setup functions existem para todos os 5 | Assinaturas verificadas via grep | controller/*.go |
| PostgreSQL é single-node managed | db-s-1vcpu-2gb DigitalOcean, sem réplica | scan infra DO |
| TLS 3/4 falhando | Apenas tls-api emitido | kubectl get certificates |
| Nenhum comando operacional existe | logs, status, secret, rollback, promote — zero | scan CLI |
| Prometheus + Grafana rodando mas sem dashboards custom | kube-prometheus-stack instalado | scan infra |
| Último build falhou com SIGTERM | install-deps step killed | kubectl get workflows |

## INCERTEZAS DECLARADAS

| Incerteza | Impacto | Mitigação |
|---|---|---|
| Causa exata TLS (3 certs) | Fix pode ser trivial ou depender de rate-limit reset | Investigar ANTES de agir |
| Causa SIGTERM no build | Pode ser OOM ou deadline | Verificar pod events antes de mudar resource limits |
| Comportamento de advisory locks pós-failover PG | Lock pode se comportar diferente com endpoint change | Testar failover DURANTE deploy ativo |
| Ordem de startup dos controllers no manager | Controllers podem competir por recursos no startup | Wire um por vez, validar isoladamente |

---

# BLOCO 1 — RUNTIME (SERIALIZADO, SEM PARALELISMO)

**Objetivo:** Todo estado passa por reconcile loop.
**Regra:** Wire um controller por vez. Validar cada um isoladamente antes do próximo.
**Rollback:** Remover a chamada Setup do main.go e re-deploy.

---

## PRÉ-REQUISITO — Readiness Gate

Antes de tocar qualquer controller, confirmar que o sistema base está saudável:

```
GATE-0: Sistema base saudável
- [ ] ArgoCD server respondendo: kubectl -n platform-system get pod -l app.kubernetes.io/name=argocd-server → Running
- [ ] Gitea acessível: curl -sf http://gitea-http.platform-system:3000/api/v1/version (via port-forward)
- [ ] API respondendo: curl -sf https://api.dev.loopzero.example/api/v1/health → 200
- [ ] Runtime cluster acessível: kubectl --context <runtime> get nodes → Ready
- [ ] Build cluster acessível: kubectl --context <build> get nodes → Ready
- [ ] TenantController reconciliando: kubectl -n platform-system get tenant tn-loopzero → Active
```

**Se qualquer item falhar → PARAR. Corrigir primeiro.**

---

### 1.1 — Wire EnvironmentController

**Diagnóstico:**
`SetupEnvironmentController(mgr, runtimeClient)` em `environment_controller.go:506`. Não chamado no main.go. Sem ele, Environment CRs criadas pelo TenantController ficam em `Pending` eternamente — namespaces, RBAC, NetworkPolicies e quotas no runtime cluster não são criados.

**Dependências deste controller:**
- `runtimeClient` (já existe, linha 112 do main.go)
- Runtime cluster acessível
- Nenhuma dependência de outros controllers não-wired

**Por que este é primeiro:**
- É o controller mais independente (só precisa de mgr + runtimeClient)
- Não depende de Application, Exposure, ou PlatformBootstrap
- TenantController já cria Environment CRs — este é o próximo na cadeia

**Ação:**
Arquivo: `operators/cmd/main.go`, após linha 150:
```go
if err := controller.SetupEnvironmentController(mgr, runtimeClient); err != nil {
    setupLog.Error(err, "unable to setup EnvironmentController")
    os.Exit(1)
}
```

**Validação isolada (OBRIGATÓRIA antes de prosseguir):**
```
GATE-1.1:
- [ ] go test ./internal/controller/ -run Environment -v -count=1 → PASS
- [ ] Deploy operator (Tilt ou push)
- [ ] Logs: "starting controller" controller="environment"
- [ ] kubectl -n platform-system get environment → existe (criada pelo TenantController)
- [ ] kubectl --context <runtime> get ns loopzero-tn-loopzero-production → criado em <60s
- [ ] kubectl --context <runtime> get serviceaccount -n loopzero-tn-loopzero-production → existe
- [ ] Esperar 5 minutos — zero crash, zero error loop nos logs
```

**Rollback:** Remover a chamada `SetupEnvironmentController` do main.go, re-deploy.

**Confiança:** ALTA — Controller testado, assinatura simples, dependências satisfeitas.

---

### 1.2 — Wire ApplicationController

**Diagnóstico:**
`SetupApplicationController(mgr, baseDomain)` em `application_controller.go:222`. Atribui hostnames, cria ExposedRoute CRs, rastreia health.

**Dependências deste controller:**
- `mgr` (já existe)
- `baseDomain` string (env var nova, mas valor default funciona)
- ExposureController **NÃO é dependência hard** — ApplicationController cria ExposedRoute CRs e lê seu status, mas funciona mesmo se ExposureController não estiver rodando (CRs ficam em Pending, App fica em Progressing)

**Por que segundo:**
- Cria ExposedRoute CRs que o ExposureController (próximo) vai reconciliar
- Pode ser validado isoladamente — hostname assignment é autossuficiente

**Ação:**
Arquivo: `operators/cmd/main.go`:
```go
baseDomain := os.Getenv("BASE_DOMAIN")
if baseDomain == "" {
    baseDomain = "dev.loopzero.example"
}
if err := controller.SetupApplicationController(mgr, baseDomain); err != nil {
    setupLog.Error(err, "unable to setup ApplicationController")
    os.Exit(1)
}
```
Arquivo: deployment do operator (Helm ou Tilt) — adicionar env var `BASE_DOMAIN`.

**Validação isolada:**
```
GATE-1.2:
- [ ] go test ./internal/controller/ -run Application -v -count=1 → PASS
- [ ] Deploy operator
- [ ] Logs: "starting controller" controller="application"
- [ ] Criar LoopZeroApplication CR manualmente → status.URL populado com hostname
- [ ] ExposedRoute CR criado com ownerRef → fase Pending (esperado — ExposureController ainda não roda)
- [ ] Esperar 5 minutos — zero crash loops
```

**Rollback:** Remover `SetupApplicationController` + env var, re-deploy.

**Confiança:** ALTA

---

### 1.3 — Wire ExposureController

**Diagnóstico:**
`SetupExposureController(mgr)` em `exposure_controller.go:232`. Verifica TLS certs e faz probes de reachability nas ExposedRoutes.

**Dependências deste controller:**
- `mgr` apenas (assinatura mais simples possível)
- Depende de ExposedRoute CRs existirem → criados pelo ApplicationController (1.2)
- Depende de cert-manager para verificar Certificate status

**Por que terceiro:**
- Consome ExposedRoute CRs criados pelo ApplicationController
- Cadeia: Tenant → Environment → Application → **ExposedRoute** → ExposureController

**Ação:**
```go
if err := controller.SetupExposureController(mgr); err != nil {
    setupLog.Error(err, "unable to setup ExposureController")
    os.Exit(1)
}
```

**Validação isolada:**
```
GATE-1.3:
- [ ] go test ./internal/controller/ -run Exposure -v -count=1 → PASS
- [ ] Deploy operator
- [ ] ExposedRoute CR (criado em 1.2) → transiciona de Pending para CertIssued (se cert Ready) ou permanece Pending (se cert not ready — aceitável, TLS fix vem no Bloco 2)
- [ ] Se cert Ready: probe HTTP roda e ExposedRoute → Reachable ou Failed
- [ ] Esperar 5 minutos — zero crash loops
```

**Rollback:** Remover `SetupExposureController`, re-deploy.

**Confiança:** ALTA

---

### 1.4 — Wire GitRepositoryBindingController

**Diagnóstico:**
`SetupGitRepositoryBindingController(mgr, httpClient)` em `gitrepositorybinding_controller.go:340`. Sincroniza credenciais Git para ArgoCD Secrets.

**Dependências:**
- `mgr` + `httpClient`
- ArgoCD namespace acessível (para criar Secrets)
- Gitea acessível (para verificar reachability — opcional, graceful fallback)

**Por que quarto (antes de PlatformBootstrap):**
- Mais simples que PlatformBootstrap (não faz bootstrap de múltiplos serviços)
- PlatformBootstrap pode criar GitRepositoryBinding CRs — melhor ter este controller pronto antes

**Ação:**
```go
import "net/http"
import "time"

httpClient := &http.Client{Timeout: 30 * time.Second}

if err := controller.SetupGitRepositoryBindingController(mgr, httpClient); err != nil {
    setupLog.Error(err, "unable to setup GitRepositoryBindingController")
    os.Exit(1)
}
```

**Validação isolada:**
```
GATE-1.4:
- [ ] go test ./internal/controller/ -run GitRepositoryBinding -v -count=1 → PASS
- [ ] Deploy operator
- [ ] Criar GitRepositoryBinding CR apontando para Gitea → ArgoCD Secret criado no consumer namespace
- [ ] Mudar token no Secret referenciado → ArgoCD Secret atualizado (hash muda em status.credentialHash)
- [ ] Esperar 5 minutos — zero crash loops
```

**Rollback:** Remover `SetupGitRepositoryBindingController`, re-deploy.

**Confiança:** ALTA

---

### 1.5 — Wire PlatformBootstrapController

**Diagnóstico:**
`SetupPlatformBootstrapController(mgr, httpClient)` em `platformbootstrap_controller.go:181`. Faz bootstrap declarativo: Gitea token → ArgoCD JWT → API DB seed. Hoje feito por shell script imperativo no CI.

**Dependências (MAIS COMPLEXO):**
- `mgr` + `httpClient`
- Gitea HTTP acessível e autenticável
- ArgoCD HTTP acessível
- API HTTP acessível (health + bootstrap endpoint)
- Credenciais corretas em Secrets referenciados pelo CR

**Por que último:**
- Mais dependências externas que qualquer outro controller
- Se URLs ou credenciais estiverem erradas, entra em retry loop
- Precisa de TODOS os serviços base saudáveis (gate-0)

**Ação:**
Reutilizar `httpClient` de 1.4:
```go
if err := controller.SetupPlatformBootstrapController(mgr, httpClient); err != nil {
    setupLog.Error(err, "unable to setup PlatformBootstrapController")
    os.Exit(1)
}
```

**Validação isolada (MAIS RIGOROSA):**
```
GATE-1.5:
- [ ] go test ./internal/controller/ -run PlatformBootstrap -v -count=1 → PASS
- [ ] Deploy operator
- [ ] NÃO criar PlatformBootstrap CR ainda — apenas validar que controller inicia sem erros
- [ ] Criar PlatformBootstrap CR com URLs corretos → observar transição:
      Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → BootstrapComplete
- [ ] Se ficar stuck em qualquer fase >2min: verificar logs, conditions do CR
- [ ] Re-criar CR (delete + apply) → idempotente (não duplica tokens)
- [ ] Esperar 10 minutos — zero crash loops, zero retry storms
```

**Rollback:** Remover `SetupPlatformBootstrapController`, deletar PlatformBootstrap CR, re-deploy.

**Confiança:** MÉDIA — Código testado com httptest, mas integração real depende de URLs e credentials corretos no cluster. Retry storms possíveis se misconfigured.

---

### 1.6 — Validação E2E do Bloco 1

**CHECKPOINT DE ESTABILIDADE (R2):**

```
GATE-BLOCO-1:
- [ ] 7 controllers running nos logs do operator
- [ ] kubectl -n platform-system get tenant tn-loopzero → Active
- [ ] kubectl -n platform-system get environment → Active
- [ ] Zero crash loops nos últimos 30 minutos
- [ ] API health deep → 200
- [ ] Executar 3 deploys consecutivos via loopzero deploy:
      Deploy 1: ___  resultado: ___
      Deploy 2: ___  resultado: ___
      Deploy 3: ___  resultado: ___
- [ ] Todos os 3 → build SUCCEEDED, release ACTIVE
- [ ] ArgoCD app synced + healthy após cada deploy

SE QUALQUER ITEM FALHAR → NÃO AVANÇAR PARA BLOCO 2.
```

---

# BLOCO 2 — ESTABILIZAÇÃO INFRA

**Objetivo:** TLS funcional + sistema comprovadamente estável.
**Pré-requisito:** GATE-BLOCO-1 passou.

---

### 2.1 — Investigar e corrigir TLS (3 certificados falhando)

**Diagnóstico:**
Certificados `tls-argocd`, `tls-git`, `tls-grafana` não Ready. Apenas `tls-api` emitido.

**INCERTEZA:** Causa desconhecida. Possibilidades:
1. HTTP-01 challenge não chega ao solver (Traefik routing)
2. Let's Encrypt rate limiting (5 certs/domínio/semana)
3. DNS propagation issue

**Ação (investigar ANTES de mudar qualquer coisa):**
1. `kubectl -n platform-system describe certificate tls-argocd` → ler Events e Conditions
2. `kubectl -n platform-system get challenges -o wide` → ver estado das challenges
3. `kubectl -n platform-system logs -l app.kubernetes.io/name=cert-manager --tail=100` → erros
4. `dig +short argocd.dev.loopzero.example` → confirmar que resolve para IP do Traefik mgmt
5. `curl -sf http://argocd.dev.loopzero.example/.well-known/acme-challenge/test` → verificar se Traefik roteia challenges

**Fix depende da causa:**
- Se routing: ajustar Ingress/IngressRoute para ACME solver
- Se rate-limit: esperar reset ou usar staging issuer temporariamente
- Se DNS: corrigir registro

**Critério de aceite:**
```
GATE-2.1:
- [ ] kubectl get certificates -A → 4/4 Ready=True
- [ ] curl -s https://argocd.dev.loopzero.example → sem erro de certificado
- [ ] curl -s https://git.dev.loopzero.example → sem erro de certificado
- [ ] curl -s https://grafana.dev.loopzero.example → sem erro de certificado
```

**Rollback:** Nenhuma mudança destrutiva — investigação + fix pontual.

**Confiança:** MÉDIA — Depende da causa raiz.

---

### 2.2 — Checkpoint pós-TLS

```
GATE-BLOCO-2:
- [ ] TLS 4/4 Ready
- [ ] 3 deploys consecutivos estáveis (GATE-BLOCO-1 ainda válido)
- [ ] ArgoCD acessível via HTTPS
- [ ] Grafana acessível via HTTPS
- [ ] Gitea acessível via HTTPS

SE QUALQUER ITEM FALHAR → NÃO AVANÇAR.
```

---

# BLOCO 3 — BANCO (POSTGRES HA)

**Objetivo:** PostgreSQL deixa de ser single point of failure.
**Pré-requisito:** GATE-BLOCO-2 passou.

---

### 3.1 — Upgrade para HA

**Diagnóstico:**
DigitalOcean Managed PG v16, single-node. API usa pgx pool com reconnect nativo. Advisory locks são transaction-scoped.

**CORREÇÃO DO REVIEW: NÃO é trivial.** Problemas reais:
- Failover muda endpoint IP → pool precisa reconectar
- Latência pode mudar pós-failover (replica promovida)
- Advisory locks em transação aberta durante failover → transação abortada → lock liberado (correto, mas precisa confirmar)

**Ação:**
1. Mudar `infra/pulumi/Pulumi.dev.yaml`: `database.nodes: 2`
2. Executar `pulumi up` (via CI ou manual)
3. Aguardar provisionamento da réplica (~10-15min no DO)

**Validação PRÉ-FAILOVER:**
```
- [ ] doctl databases list → 2 nodes
- [ ] API health deep → 200
- [ ] 1 deploy estável
```

### 3.2 — Teste de failover DURANTE deploy ativo

**ESTE TESTE É OBRIGATÓRIO. SEM ELE, HA É TEÓRICO.**

**Ação:**
1. Iniciar `loopzero deploy` em um terminal
2. Durante o build (quando status = RUNNING), triggerar failover:
   `doctl databases maintenance-window update <db-id> --day $(date +%A) --hour $(date +%H:%M)`
   Ou via DO Console: Database → Actions → Promote standby
3. Observar:
   - API reconectou ao novo primary? (checar logs da API)
   - Build completou ou falhou gracefully?
   - Lock foi liberado corretamente? (transação abortada = lock released)
   - Próximo deploy funciona normalmente?

**Critério de aceite:**
```
GATE-3.2:
- [ ] Failover executado durante deploy ativo
- [ ] API reconectou em <30s (verificar logs: pool reconnect)
- [ ] Deploy em andamento: falhou gracefully (FAILED, não stuck) OU completou normalmente
- [ ] Deploy subsequente: SUCCEEDED
- [ ] Advisory lock: nenhum lock orphanado (verificar pg_locks)
- [ ] 3 deploys consecutivos pós-failover → todos SUCCEEDED
```

**Rollback:** Failover é reversível (promote standby novamente). pgx pool reconecta automaticamente.

**Confiança:** MÉDIA — pgx reconecta nativamente, mas comportamento exato durante advisory lock + failover precisa ser comprovado empiricamente.

---

### 3.3 — Monitoramento de locks e pool

**Ação:**
1. Verificar quais métricas já existem: `curl -s http://<api>:9090/metrics | grep loopzero_`
2. Se não existirem, adicionar em `api/internal/metrics/`:
   - `loopzero_lock_wait_duration_seconds` (histogram)
   - `loopzero_lock_timeout_total` (counter)
   - `loopzero_db_pool_active_connections` (gauge)
   - `loopzero_db_pool_idle_connections` (gauge)
3. Instrumentar: `api/internal/lock/environment_lock.go` para emitir métricas

**Critério de aceite:**
```
GATE-BLOCO-3:
- [ ] PG com 2 nodes (HA)
- [ ] Failover testado durante deploy ativo
- [ ] Métricas de lock visíveis no Prometheus
- [ ] 3 deploys consecutivos estáveis pós-HA
```

---

# BLOCO 4 — OBSERVABILIDADE BASE

**Objetivo:** Cada etapa do deploy é mensurável. Definir SLOs.
**Pré-requisito:** GATE-BLOCO-3 passou.

---

### 4.1 — Métricas do deploy pipeline

**Ação:**
Verificar `api/internal/metrics/` e adicionar se não existirem:

| Métrica | Tipo | Labels | Onde instrumentar |
|---|---|---|---|
| `loopzero_build_duration_seconds` | histogram | tenant, status | BuildReconcileLoop (ao completar) |
| `loopzero_build_queue_seconds` | histogram | tenant | QUEUED → RUNNING delta |
| `loopzero_release_activation_seconds` | histogram | tenant | READY → ACTIVE delta |
| `loopzero_deploy_total` | counter | tenant, status | BuildOrchestrator.SubmitBuild |
| `loopzero_lock_wait_seconds` | histogram | — | withEnvironmentLock |
| `loopzero_argocd_sync_seconds` | histogram | tenant, status | SyncClient |
| `loopzero_circuit_breaker_state` | gauge | — | CircuitBreakerSyncClient |
| `loopzero_build_queue_depth` | gauge | — | Count de builds em QUEUED/RUNNING |

**Critério de aceite:**
```
- [ ] curl <metrics> | grep loopzero_ → todas as 8 métricas presentes
- [ ] Após 1 deploy: loopzero_build_duration_seconds tem observação
```

**Confiança:** ALTA — Adicionar métricas não muda behavior.

---

### 4.2 — Definir SLOs mínimos

**CORREÇÃO DO REVIEW:** Métricas sem SLO não dizem nada.

| SLO | Target | Métrica | Alerta se violado |
|---|---|---|---|
| Build queue time p95 | < 60s | `loopzero_build_queue_seconds` | `LoopZeroQueueSlow` |
| Build duration p95 | < 300s (5min) | `loopzero_build_duration_seconds` | `LoopZeroBuildSlow` |
| Deploy success rate | > 90% (rolling 24h) | `loopzero_deploy_total{status=success} / total` | `LoopZeroDeployFailureRate` |
| Lock wait time p95 | < 2s | `loopzero_lock_wait_seconds` | `LoopZeroLockContention` |
| ArgoCD sync p95 | < 120s | `loopzero_argocd_sync_seconds` | `LoopZeroSyncSlow` |

**INCERTEZA:** Targets são estimativas baseadas nos timeouts configurados (build queue timeout=5min, lock timeout=5s, ArgoCD sync timeout=300s). Ajustar após 1 semana de dados reais.

**Ação:**
1. Criar PrometheusRule em `infra/helm/monitoring/manifests/loopzero-alerts.yaml`
2. Cada SLO vira uma recording rule + alerting rule

**Critério de aceite:**
```
- [ ] kubectl get prometheusrules -n platform-system → loopzero-slos existe
- [ ] Grafana mostra SLO compliance no dashboard
```

**Confiança:** MÉDIA para os targets (precisam de dados reais), ALTA para a mecânica (PrometheusRule padrão).

---

### 4.3 — Dashboard Grafana "LoopZero Platform"

**Ação:**
1. Criar `infra/helm/monitoring/dashboards/loopzero-platform.json`
2. Panels:
   - Deploys/hora (por status)
   - Build duration (p50, p95) vs SLO
   - Queue time (p50, p95) vs SLO
   - ArgoCD sync duration vs SLO
   - Lock wait time
   - Circuit breaker state
   - Build queue depth
   - Deploy success rate vs SLO (90%)
   - API error rate (por endpoint)
   - PG connection pool utilization

**Critério de aceite:**
```
GATE-BLOCO-4:
- [ ] Métricas emitindo (8/8)
- [ ] SLOs definidos e alertas configurados (5/5)
- [ ] Dashboard acessível e mostrando dados
- [ ] Após 3 deploys: "por que esse deploy demorou?" respondível em <30s olhando o dashboard
- [ ] 3 deploys consecutivos estáveis
```

---

# BLOCO 5 — PIPELINE

**Objetivo:** Builds previsíveis. Build failure investigado. Cache validado.
**Pré-requisito:** GATE-BLOCO-4 passou (preciso das métricas para medir impacto das mudanças).

---

### 5.1 — Investigar build failure (SIGTERM)

**Ação:**
1. `kubectl --context <build> describe pod -n build-tn-loopzero -l workflows.argoproj.io/workflow=loopzero-build-build-83375c3021faaeac-trtth` → Events (OOMKilled? Evicted?)
2. `kubectl --context <build> describe nodes` → Allocated resources (memory pressure?)
3. Ler resource limits no CWT values: `infra/helm/argo-workflows/values.yaml`
4. Se OOM: aumentar memory limit do step install-deps com margem 50%
5. Se deadline: aumentar `activeDeadlineSeconds` do step

**Critério de aceite:**
```
- [ ] Causa raiz documentada (OOM ou timeout ou outro)
- [ ] Build subsequente SUCCEEDED
- [ ] Nenhum SIGTERM em 3 builds consecutivos
```

**Confiança:** ALTA para investigação, MÉDIA para fix.

---

### 5.2 — Validar cache pipeline

**Ação:**
1. Disparar build 1 de projeto limpo → observar duração total
2. Disparar build 2 do mesmo projeto, sem mudança de lockfile → observar duração
3. Build 2 deve ser significativamente mais rápido (install-deps skip ou <30% do tempo)

**Critério de aceite:**
```
- [ ] Build 2 usa cache (visível nos logs do step cache-restore)
- [ ] Duração de install-deps reduzida >50%
```

**Confiança:** ALTA

---

### 5.3 — Queue depth e fairness

**CORREÇÃO DO REVIEW:** Fila implícita no Argo é risco.

**Ação:**
1. Métrica `loopzero_build_queue_depth` já adicionada em 4.1
2. Verificar: ResourceQuota por build namespace limita concurrência por tenant
3. Verificar: `maxBuildConcurrency` no Tenant CR spec é respeitado pela API (concurrent-build check dentro do lock)
4. Teste: tenant A com 2 builds + tenant B tenta build → B não é bloqueado por A

**Critério de aceite:**
```
GATE-BLOCO-5:
- [ ] Build failure investigado e resolvido
- [ ] Cache funcional (build 2 mais rápido)
- [ ] Queue depth visível no dashboard
- [ ] Build concurrency por tenant enforced (API + ResourceQuota)
- [ ] 3 deploys consecutivos estáveis
```

---

# BLOCO 6 — OPERAÇÃO (UMA FEATURE POR VEZ)

**Objetivo:** Day-2 operável sem dependência humana.
**Pré-requisito:** GATE-BLOCO-5 passou. Sistema comprovadamente estável.

**CORREÇÃO DO REVIEW:** Confiança reclassificada. Ordem corrigida. Uma feature por vez.

---

### 6.1 — `loopzero status` (PRIMEIRO)

**Por que primeiro:** É read-only. Zero risco de corrupção. Agrega dados que já existem. Base para validar as outras features.

**Diagnóstico:**
Dados distribuídos entre tabelas `builds`, `releases`, `environment_targets` + ArgoCD Application status. Nenhum endpoint de agregação existe.

**Ação:**
1. API: `GET /api/v1/projects/{name}/status?env={env}` — agrega último build, release ativo, ArgoCD sync, pod health
2. CLI: `loopzero status [--env] [--json]`
3. TDD: teste primeiro, implementa depois

**Critério de aceite:**
```
- [ ] loopzero status → mostra: environment, último deploy, release ativo, sync status, pods running/desired
- [ ] --json retorna schema documentado
- [ ] Exit codes: 0 (success), 1 (usage), 4 (auth)
- [ ] 3 deploys estáveis com loopzero status verificável após cada um
```

**Confiança:** MÉDIA — Design claro, dados existem, mas implementação nova.

---

### 6.2 — `loopzero logs` (SEGUNDO)

**CORREÇÃO DO REVIEW: Confiança BAIXA (não MÉDIA).**

**Por que log streaming cross-cluster é difícil:**
- API no mgmt cluster precisa ler logs de pods no build cluster
- Pods de build são efêmeros (Workflow pods deletados após TTL)
- Streaming (follow mode) precisa de long-lived connection cross-cluster

**Ação:**
1. API: `GET /api/v1/builds/{id}/logs?follow=true`
   - Build RUNNING: proxy de pod logs via build cluster client (PodLogOptions)
   - Build terminal: retorna logs do SSE event store ou Argo archived logs
2. CLI: `loopzero logs --build <id> [--follow] [--json]`
3. **Limitação aceita:** logs de builds antigos podem não estar disponíveis (TTL de archived workflows)

**Critério de aceite:**
```
- [ ] loopzero logs --build <id> → output do build
- [ ] --follow em build ativo → streaming até terminar
- [ ] Build antigo → retorna o que tiver (event store) ou mensagem clara "logs expired"
- [ ] Exit codes: 0, 1, 4
```

**Confiança:** BAIXA — Cross-cluster log streaming é implementação nova com edge cases (pod deleted, network interruption, timeout).

---

### 6.3 — `loopzero rollback` (TERCEIRO)

**CORREÇÃO DO REVIEW: Confiança BAIXA.**

**Por que rollback é perigoso se mal feito:**
- Pointer swap no `environment_targets` precisa ser atômico (dentro do lock)
- Gitops-push do manifest anterior precisa existir (e se foi garbage-collected?)
- Release state machine tem transitions específicas (ACTIVE → ROLLING_BACK → ROLLED_BACK)
- ArgoCD sync do manifest anterior pode falhar (circuit breaker)

**Ação:**
1. API: `POST /api/v1/rollback` com environment lock
   - Busca release anterior em `environment_targets`
   - Cria novo Release apontando para manifests do anterior
   - Gitops-push dos manifests
   - ArgoCD sync
   - Pointer swap: desired_release_id = novo release
2. CLI: `loopzero rollback [--env] [--to <release-id>] [--json]`
3. **Teste obrigatório:** rollback após deploy → app volta ao estado anterior em <30s

**Critério de aceite:**
```
- [ ] Deploy A → Deploy B → loopzero rollback → app rodando versão A
- [ ] Release B → SUPERSEDED, novo release → ACTIVE
- [ ] --to <id> funciona para rollback específico
- [ ] Rollback dentro do environment lock (sem race condition)
- [ ] 404 se release anterior não existe
```

**Confiança:** BAIXA — State machine transitions definidas, mas implementação do fluxo completo (lock + gitops-push reverso + ArgoCD sync) é nova e tem múltiplos pontos de falha.

---

### 6.4 — `loopzero secret` (QUARTO — POR ÚLTIMO)

**CORREÇÃO DO REVIEW: Confiança BAIXA. Deixar para o final.**

**Por que último:**
- Requer implementação de controller NOVO (AppSecretController — não existe)
- Cross-namespace Secret sync é operação sensível (segurança)
- Rotação de secrets em workload cluster precisa ser atômica
- Risco de segurança se mal implementado (secret leak, stale secrets)

**Ação:**
1. Implementar AppSecretController (controller novo, Sprint 3)
2. API: `POST /api/v1/secrets`, `DELETE /api/v1/secrets/{name}`
3. CLI: `loopzero secret set`, `loopzero secret list`, `loopzero secret delete`
4. **Nunca:** valores em logs, --json output, ou git

**Critério de aceite:**
```
- [ ] loopzero secret set DB_URL=<value> → Secret criado no mgmt
- [ ] AppSecretController sincroniza para namespace workload
- [ ] loopzero secret list → apenas nomes (nunca valores)
- [ ] Rotação: set com novo valor → sincronizado em <30s
- [ ] Audit log de quem mudou qual secret
```

**Confiança:** BAIXA — Controller novo + cross-namespace sync + segurança.

```
GATE-BLOCO-6 (após cada feature):
- [ ] Feature implementada e testada
- [ ] 3 deploys consecutivos estáveis
- [ ] Nenhuma regressão nas features anteriores
```

---

# SEQUÊNCIA FINAL (SERIALIZADA)

```
BLOCO 1: Runtime (serializado, 1 controller por vez)
  ├─ GATE-0: sistema base saudável
  ├─ 1.1 EnvironmentController     → GATE-1.1
  ├─ 1.2 ApplicationController     → GATE-1.2
  ├─ 1.3 ExposureController        → GATE-1.3
  ├─ 1.4 GitRepositoryBindingCtrl  → GATE-1.4
  ├─ 1.5 PlatformBootstrapCtrl     → GATE-1.5
  └─ 1.6 Validação E2E             → GATE-BLOCO-1 (3 deploys estáveis)

BLOCO 2: Estabilização
  ├─ 2.1 Investigar + fix TLS      → GATE-2.1
  └─ 2.2 Checkpoint                → GATE-BLOCO-2

BLOCO 3: Banco
  ├─ 3.1 PG HA upgrade             → validação pré-failover
  ├─ 3.2 Failover DURANTE deploy   → GATE-3.2
  └─ 3.3 Métricas de locks/pool    → GATE-BLOCO-3

BLOCO 4: Observabilidade
  ├─ 4.1 Métricas pipeline (8)
  ├─ 4.2 SLOs (5) + alertas
  └─ 4.3 Dashboard Grafana         → GATE-BLOCO-4

BLOCO 5: Pipeline
  ├─ 5.1 Investigar SIGTERM
  ├─ 5.2 Validar cache
  └─ 5.3 Queue depth + fairness    → GATE-BLOCO-5

BLOCO 6: Operação (1 feature por vez)
  ├─ 6.1 loopzero status               → 3 deploys estáveis
  ├─ 6.2 loopzero logs                 → 3 deploys estáveis
  ├─ 6.3 loopzero rollback             → 3 deploys estáveis
  └─ 6.4 loopzero secret               → 3 deploys estáveis
```

---

# TABELA DE CONFIANÇA (CORRIGIDA)

| Item | Confiança v1 | Confiança v2 | Justificativa da correção |
|---|---|---|---|
| Wire Environment/Application/Exposure | ALTA | ALTA | Sem mudança — assinaturas simples, testes existem |
| Wire GitRepositoryBinding | ALTA | ALTA | Sem mudança |
| Wire PlatformBootstrap | MÉDIA | MÉDIA | Sem mudança — dependências externas reais |
| TLS fix | MÉDIA | MÉDIA | Sem mudança — depende da causa raiz |
| PG HA upgrade | ALTA → | **MÉDIA** | **Corrigido:** failover não é trivial, precisa teste real |
| PG failover durante deploy | — | **MÉDIA** | **Novo:** teste empírico obrigatório |
| Métricas | ALTA | ALTA | Sem mudança — instrumentação sem side effects |
| SLOs | — | **MÉDIA** | **Novo:** targets são estimativas, ajustar com dados |
| Dashboard | ALTA | ALTA | Sem mudança — read-only |
| Build failure fix | MÉDIA | MÉDIA | Sem mudança |
| Cache validation | ALTA | ALTA | Sem mudança |
| Queue depth | — | **ALTA** | **Novo:** métrica já planejada, verificação simples |
| loopzero status | MÉDIA | MÉDIA | Sem mudança |
| loopzero logs | MÉDIA → | **BAIXA** | **Corrigido:** cross-cluster streaming é difícil |
| loopzero rollback | MÉDIA → | **BAIXA** | **Corrigido:** state machine + gitops reverso + lock |
| loopzero secret | BAIXA | BAIXA | Sem mudança — controller novo |

---

# VEREDITO v2

**Confiança global:** ~93-95%

**O que mudou do v1:**
1. ✅ Mudanças serializadas — uma variável por vez
2. ✅ Readiness gates antes de cada bloco
3. ✅ Ordem de controllers por dependência (Environment → Application → Exposure → GitRepositoryBinding → PlatformBootstrap)
4. ✅ Confiança rebaixada para logs (BAIXA), rollback (BAIXA), PG HA (MÉDIA)
5. ✅ Failover testado DURANTE deploy ativo (não teórico)
6. ✅ Queue depth como métrica obrigatória
7. ✅ SLOs definidos com targets mensuráveis
8. ✅ Regra de estabilidade: 3 deploys consecutivos antes de avançar
9. ✅ Cada feature operacional isolada com checkpoint próprio

**O que este plano NÃO inclui:**
- Features de Sprint 5+ (preview, security, chaos)
- Migração de cloud provider
- Otimizações prematuras (BuildKit, sharding)
- Qualquer mudança sem evidência no estado atual
