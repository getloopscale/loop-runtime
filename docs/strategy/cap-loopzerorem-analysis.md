# LoopZero Platform — Análise do Teorema CAP

> Documento técnico que mapeia as decisões de consistência, disponibilidade e tolerância a partições
> do LoopZero Platform contra os fundamentos do Teorema CAP e sua extensão PACELC.
>
> **Audiência:** engenheiros da plataforma, SREs, arquitetos avaliando o sistema.
>
> **Última atualização:** 2026-05-11

---

## Sumário

- [1. Fundamentos Teóricos](#1-fundamentos-teóricos)
  - [1.1 O Teorema CAP](#11-o-teorema-cap)
  - [1.2 Por que "escolha 2 de 3" é enganoso](#12-por-que-escolha-2-de-3-é-enganoso)
  - [1.3 PACELC — A extensão que importa na prática](#13-pacelc--a-extensão-que-importa-na-prática)
- [2. Arquitetura do LoopZero e os Quatro Planos](#2-arquitetura-do-loopzero-e-os-quatro-planos)
- [3. Decisões CAP por Componente](#3-decisões-cap-por-componente)
  - [3.1 PostgreSQL — Estado Operacional (CP)](#31-postgresql--estado-operacional-cp)
  - [3.2 Gitea — Desired State via Git (CP)](#32-gitea--desired-state-via-git-cp)
  - [3.3 ArgoCD — Reconciliação de Workloads (AP)](#33-argocd--reconciliação-de-workloads-ap)
  - [3.4 SSE — Streaming de Eventos (AP)](#34-sse--streaming-de-eventos-ap)
  - [3.5 Cloudflare Tunnel — Ingress (SPOF)](#35-cloudflare-tunnel--ingress-spof)
  - [3.6 External Secrets Operator (AP)](#36-external-secrets-operator-ap)
  - [3.7 KEDA — Autoscaling (AP)](#37-keda--autoscaling-ap)
  - [3.8 Valkey/Redis — Cache e Sessões (AP)](#38-valkeyredis--cache-e-sessões-ap)
- [4. Mecanismos de Consistência](#4-mecanismos-de-consistência)
  - [4.1 Advisory Locks — Serialização de Mutações](#41-advisory-locks--serialização-de-mutações)
  - [4.2 State Machines — Transições Estritas](#42-state-machines--transições-estritas)
  - [4.3 Idempotência Ponta-a-Ponta](#43-idempotência-ponta-a-ponta)
  - [4.4 Environment Targets — Source of Truth](#44-environment-targets--source-of-truth)
- [5. Mecanismos de Disponibilidade](#5-mecanismos-de-disponibilidade)
  - [5.1 Circuit Breakers](#51-circuit-breakers)
  - [5.2 Deploy em 5 Fases com Lock Mínimo](#52-deploy-em-5-fases-com-lock-mínimo)
  - [5.3 Convergência em 3 Camadas](#53-convergência-em-3-camadas)
- [6. Tolerância a Partições](#6-tolerância-a-partições)
  - [6.1 Partição mgmt ↔ build](#61-partição-mgmt--build)
  - [6.2 Partição mgmt ↔ runtime](#62-partição-mgmt--runtime)
  - [6.3 Partição API ↔ PostgreSQL](#63-partição-api--postgresql)
  - [6.4 Partição com Cloudflare](#64-partição-com-cloudflare)
- [7. Classificação PACELC Completa](#7-classificação-pacelc-completa)
- [8. Gaps Conhecidos e Mitigações Planejadas](#8-gaps-conhecidos-e-mitigações-planejadas)
- [9. Referências](#9-referências)

---

## 1. Fundamentos Teóricos

### 1.1 O Teorema CAP

O Teorema CAP, formalizado por Seth Gilbert e Nancy Lynch (2002), afirma que um sistema distribuído
que armazena dados não pode fornecer simultaneamente as três garantias:

- **Consistency (C):** Toda leitura recebe a escrita mais recente ou um erro. Na formulação formal,
  isso equivale a **linearizabilidade** — o sistema se comporta como se houvesse uma única cópia dos
  dados e todas as operações fossem atômicas.

- **Availability (A):** Toda requisição enviada a um nó não-falho recebe uma resposta sem erro.
  Importante: a definição formal exige resposta de **qualquer nó**, não apenas do líder.

- **Partition Tolerance (P):** O sistema continua operando mesmo com perda arbitrária de mensagens
  entre nós. Em termos práticos, a rede entre componentes pode falhar.

### 1.2 Por que "escolha 2 de 3" é enganoso

O framing clássico "pick 2 of 3" é a maior fonte de mal-entendidos sobre CAP. Na realidade:

1. **Partições não são opcionais.** Cabos são cortados, switches falham, timeouts acontecem.
   Tolerância a partição é um fato da vida em sistemas distribuídos, não uma feature que se liga
   ou desliga. Isso reduz a escolha real a **C vs A durante uma partição**.

2. **Fora de partições, não há trade-off.** Um sistema pode ser consistente E disponível quando a
   rede funciona normalmente. CAP só se aplica durante falhas.

3. **C e A são espectros, não binários.** Sistemas como Cassandra permitem configurar consistência
   por operação (`ONE`, `QUORUM`, `ALL`). O mesmo sistema pode ser AP para uma leitura e CP para
   outra.

4. **"CP" e "AP" são rótulos grosseiros.** Martin Kleppmann argumenta em "Please stop calling
   databases CP or AP" que esses rótulos obscurecem mais do que revelam. O comportamento real
   depende do tipo de operação, configuração, e cenário de falha específico.

**A implicação para arquitetura:** Não existe "o sistema é CP ou AP". Cada componente e cada
operação faz sua própria escolha. O que importa é que essas escolhas sejam **conscientes,
documentadas e alinhadas com os requisitos de negócio**.

### 1.3 PACELC — A extensão que importa na prática

Daniel Abadi propôs o teorema PACELC, que captura o trade-off do dia-a-dia que CAP ignora:

> **P**artition → escolha entre **A** e **C**
> **E**lse (operação normal) → escolha entre **L**atência e **C**onsistência

A maioria dos sistemas opera sem partições 99.9% do tempo. O trade-off real que os desenvolvedores
enfrentam diariamente é **latência vs consistência** — quão rápido preciso responder versus quão
atualizado o dado precisa estar.

| Classificação | Significado | Exemplo |
|---------------|-------------|---------|
| PA/EL | Disponível em partição, baixa latência normal | DynamoDB, Cassandra |
| PC/EC | Consistente sempre, aceita latência | PostgreSQL, CockroachDB |
| PA/EC | Disponível em partição, consistente normalmente | Raro — trade-offs conflitantes |
| PC/EL | Consistente em partição, rápido normalmente | PNUTS (Yahoo) |

---

## 2. Arquitetura do LoopZero e os Quatro Planos

O LoopZero opera como quatro planos distribuídos em múltiplos clusters Kubernetes:

```
┌─────────────────────────────────────────────────────────────────────┐
│                        DEVELOPER PLANE                              │
│                    CLI (TypeScript) ↔ API (Go)                      │
└────────────────────────────┬────────────────────────────────────────┘
                             │
        ┌────────────────────┼────────────────────┐
        │                    │                    │
        ▼                    ▼                    ▼
┌───────────────┐  ┌─────────────────┐  ┌─────────────────────────┐
│ CONTROL PLANE │  │  BUILD PLANE    │  │    WORKLOAD PLANE       │
│ mgmt cluster  │  │  build cluster  │  │  runtime + runtime-2    │
│               │  │                 │  │                         │
│ • Operators   │  │ • Argo Wkflows  │  │ • Apps dos tenants      │
│ • PostgreSQL  │  │ • BuildKit      │  │ • ArgoCD sync           │
│ • Gitea       │  │ • cosign        │  │ • Traefik + TLS         │
│ • ArgoCD      │  │                 │  │ • KEDA                  │
│ • Ory Auth    │  │                 │  │ • CF Tunnel             │
│ • Valkey      │  │                 │  │ • ESO                   │
└───────────────┘  └─────────────────┘  └─────────────────────────┘
```

**Cada conexão entre planos é um ponto onde partições podem ocorrer.** A comunicação entre
clusters acontece via APIs HTTPS autenticadas com bearer tokens, monitoradas pelo
`ClusterCredential` controller (probe de conectividade a cada 5 minutos com timeout de 5 segundos).

---

## 3. Decisões CAP por Componente

### 3.1 PostgreSQL — Estado Operacional (CP)

**Escolha: Consistência sobre Disponibilidade.**

PostgreSQL é o backbone do estado operacional do LoopZero. Todas as mutações (deploy, rollback,
promote, scaling) passam por advisory locks transacionais no PG.

| Propriedade | Valor |
|-------------|-------|
| Isolation level | `READ COMMITTED` (explícito) |
| Lock mechanism | `pg_advisory_xact_lock` (transaction-scoped) |
| Lock timeout | 5 segundos |
| Replicação | Síncrona (2 réplicas, 1 síncrona) |
| Backup | Diário às 02:00 UTC, retenção 7 dias |
| Connection pool | 25 conexões max, circuit breaker em 90% |

**Comportamento durante partição (API ↔ PG):**
- Todas as mutations falham com 503
- Reads degradam (pool saturation)
- Advisory locks impossíveis → deploy bloqueado
- **Não há modo degradado** — PG indisponível = plataforma indisponível para escritas

**Por que CP é correto aqui:** Em um PaaS, um deploy inconsistente (dois deploys concorrentes
corrompendo estado) é catastroficamente pior que um deploy indisponível (retry em 30 segundos).
Dinheiro dos tenants depende de seus serviços rodarem a versão correta.

### 3.2 Gitea — Desired State via Git (CP)

**Escolha: Consistência sobre Disponibilidade.**

Git é inerentemente linearizável — um repositório tem uma HEAD, e pushes conflitantes são
rejeitados. O LoopZero usa Gitea como fonte de verdade para o desired state de cada environment.

| Propriedade | Valor |
|-------------|-------|
| Push retry | 3× com fetch-rebase-push em conflito |
| Idempotência | "nothing to commit" retorna HEAD SHA |
| Trailers | `Release-ID`, `Correlation-ID`, `Build-ID` por commit |
| Circuit breaker | Protege operações de provisionamento (Gitea operator) |

**Comportamento durante partição (API ↔ Gitea):**
- Push falha após 3 retries → release transiciona para `GIT_MIRROR_FAILED`
- Release **não avança** para PENDING sem confirmação do push
- Usuário pode retry manualmente
- **Nenhum dado é corrompido** — Git rejeita pushes conflitantes nativamente

**Detalhe crítico — `.gitkeep`:** Quando o último projeto de um environment é removido, o LoopZero
mantém um arquivo `.gitkeep` no diretório. Sem ele, ArgoCD retorna `ComparisonError: app path
does not exist` e não consegue fazer prune dos recursos. Essa é uma decisão de consistência
operacional — previne estado órfão no cluster.

### 3.3 ArgoCD — Reconciliação de Workloads (AP)

**Escolha: Disponibilidade sobre Consistência.**

ArgoCD opera com **manual sync** (o LoopZero API dispara o sync explicitamente). Quando ArgoCD está
indisponível, releases permanecem no estado PENDING sem gerar erro ao usuário — o convergence
loop retenta automaticamente.

| Propriedade | Valor |
|-------------|-------|
| Sync policy | Manual (API-triggered) |
| Sync timeout | 30 segundos por HTTP request (180s WaitForSync total) |
| Retry | Exponential backoff (1s, 2s, 4s) |
| Stuck operation | TerminateOperation antes de re-sync |
| Convergence | Polling 10s + callback webhook |
| Hard timeout | ReleaseReaper 10 min → `SYNC_FAILED` |

**Comportamento durante partição (API ↔ ArgoCD):**
- Releases ficam em PENDING (não em FAILED)
- Workloads **continuam rodando** na versão anterior
- Circuit breaker abre após 3 falhas → novas tentativas bloqueadas por 30s
- Auth failures (401/403) **não** contam como falha no circuit breaker
- 409 "operation in progress" **não** conta como falha (stuck sync, não platform down)
- Convergence loop retenta a cada 10s até sucesso ou timeout de 10min

**Por que AP é correto aqui:** Um delay de 30-90 segundos na ativação de uma release é aceitável.
Um erro 503 para o developer que acabou de deployar (porque ArgoCD está temporariamente lento)
não é. O sistema converge eventualmente — com bounded time garantido pelo reaper.

### 3.4 SSE — Streaming de Eventos (AP)

**Escolha: Disponibilidade sobre Consistência.**

O streaming de eventos do deploy usa Server-Sent Events com replay automático.

| Propriedade | Valor |
|-------------|-------|
| Max streams concorrentes | 200 |
| Reconnect | Via `Last-Event-ID` header |
| Persistência | `sse_events` em PostgreSQL |
| Notificação | `pg_notify('sse_events', buildID)` |
| Fallback poll | 5 segundos se NOTIFY falhar |
| Safety timeout | 5 min sem evento terminal → fecha stream |

**Comportamento durante partição (CLI ↔ API):**
- Desconexão: CLI reconecta com `Last-Event-ID`, recebe eventos perdidos
- Journal expirado: evento `JOURNAL_EXPIRED` sinaliza ao CLI
- DB error durante poll: 3 retries, depois fecha stream com log

**Por que AP é correto aqui:** Perder temporariamente a conexão SSE não significa perder
informação. Os eventos estão persistidos no PG. O CLI reconecta e recebe tudo que perdeu.
Consistência eventual com replay é a abordagem padrão da indústria para event streaming.

### 3.5 Cloudflare Tunnel — Ingress (SPOF)

**Classificação: Ponto Único de Falha.**

Toda a entrada de tráfego externo para os workloads dos tenants passa por um único CF Tunnel.

```
Internet → CF Edge → CF Tunnel (2 réplicas) → Traefik → Apps
                         ↑
                    Se cai aqui, TUDO cai
```

| Propriedade | Valor |
|-------------|-------|
| Réplicas cloudflared | 2 |
| Liveness probe | HTTP GET `/ready` :2000, período 15s |
| Readiness probe | HTTP GET `/ready` :2000, período 10s |
| Alerta | `LoopZeroCFTunnelDisconnected` após 2 min |
| Fallback | **Nenhum** |

**Comportamento durante partição (CF ↔ runtime):**
- Todas as apps dos tenants ficam inacessíveis
- Não há DNS failover ou rota alternativa
- Recovery: restart de cloudflared ou recriação do tunnel via Pulumi

**Implicação:** O SLA do LoopZero para tráfego externo é limitado pelo SLA da Cloudflare. Historicamente
CF teve outages significativos (junho 2022, fevereiro 2024). Para tenants enterprise com requisitos
de 99.99%, será necessário multi-path ingress.

### 3.6 External Secrets Operator (AP)

**Escolha: Disponibilidade sobre Consistência.**

ESO sincroniza secrets do mgmt cluster para os runtime clusters.

| Propriedade | Valor |
|-------------|-------|
| Direção | mgmt → runtime (unidirecional) |
| Auth | Bearer token de ServiceAccount scoped |
| Cache | Secrets sincronizados permanecem no runtime |
| Fallback secret store | Nenhum |

**Comportamento durante partição (mgmt ↔ runtime):**
- ExternalSecret resources ficam `NotReady`
- **Secrets já sincronizados continuam funcionando** — cacheados como K8s Secrets nativos
- Novos secrets ou rotações não propagam até recovery
- Workloads existentes **não são afetados**

### 3.7 KEDA — Autoscaling (AP)

**Escolha: Disponibilidade (conservadora) sobre Precisão.**

KEDA opera no runtime cluster consumindo métricas do Prometheus no mgmt cluster.

| Propriedade | Valor |
|-------------|-------|
| Scrape | Cross-cluster via kube-state-metrics + remote_write |
| Sem métricas | Mantém último valor conhecido; assume carga alta |
| Scale-to-zero | HTTP Add-on interceptor (1 réplica) |

**Comportamento durante partição (mgmt ↔ runtime, métricas):**
- KEDA não escala down (conservador — prefere custo extra a derrubar pods)
- Interceptor HTTP continua funcionando para cold-start
- **Risco:** custo inesperado com pods running no máximo sem necessidade

### 3.8 Valkey/Redis — Cache e Sessões (AP)

**Escolha: Disponibilidade sobre Durabilidade.**

Valkey serve como cache e session store para Gitea.

| Propriedade | Valor |
|-------------|-------|
| Topologia | Pod único (não HA) |
| Pool size | 100 conexões |
| Idle timeout | 180 segundos (dev) / 120 segundos (HA) |
| Persistência | 1Gi PVC |

**Comportamento durante partição (Gitea ↔ Valkey):**
- Gitea degrada para in-memory: sessions, cache, queues
- Sessions perdem afinidade entre pods
- `global_lock` do Gitea se perde — risco de conflitos em Git push simultâneo
- **Sem perda de dados** do Git (repositórios estão em disco/PG, não em Redis)

---

## 4. Mecanismos de Consistência

### 4.1 Advisory Locks — Serialização de Mutações

Toda mutação no LoopZero é serializada por advisory lock transacional no triplet
`(tenant, project, environment)`.

```
┌────────────────────────────────────────────┐
│ withEnvironmentLock(ctx, pool, t, p, e)    │
│                                            │
│   BEGIN TRANSACTION                        │
│   SET LOCAL lock_timeout = '5s'            │
│   set_config('app.tenant_id', t, true)     │  ← RLS defense-in-depth
│   pg_advisory_xact_lock(hashtext(t:p:e))   │  ← Serialization point
│                                            │
│   ┌──────────────────────────────────────┐ │
│   │ Check concurrent builds (INSIDE lock)│ │  ← Race condition se fora
│   │ Create/Update records                │ │
│   │ Transition state machine             │ │
│   └──────────────────────────────────────┘ │
│                                            │
│   COMMIT (lock auto-released)              │
│   On crash: ROLLBACK (lock auto-released)  │
└────────────────────────────────────────────┘
```

**Propriedades críticas:**

- **Transaction-scoped, nunca session-scoped.** `pg_advisory_xact_lock` libera automaticamente
  no commit, rollback, ou desconexão. Com connection pooling (pgxpool), um lock session-scoped
  voltaria ao pool com o lock ainda ativo → deadlock.

- **Concurrent build check DENTRO do lock.** Se o check estivesse fora, dois deploys simultâneos
  poderiam ambos passar no check, ambos adquirir o lock sequencialmente, e ambos criar builds —
  violando a invariante de um-build-por-environment.

- **Lock timeout de 5 segundos.** Se outro deploy está em andamento, o segundo recebe 409 Conflict
  em no máximo 5s, não fica esperando indefinidamente.

**Resultado observável:** O primeiro deploy recebe 202 (accepted), o segundo recebe 409 (conflict).
Determinístico, sem race conditions.

### 4.2 State Machines — Transições Estritas

O LoopZero possui 11 state machines em `pkg/statemachine/` (Build, Release, Tenant, PlatformBootstrap, Preview, Promotion, Rollback, Domain, Environment, Application, ExposedRoute). Transições inválidas causam **panic**
(programming error, não runtime error). Isso garante que o sistema nunca entra em estado
inconsistente por bug de lógica.

**Exemplo — Release State Machine:**

```
                    ┌────────────────────────────┐
                    │                            │
                    ▼                            │
  READY ──────► PENDING ──────► ACTIVE ──────► SUPERSEDED
    │              │               │
    │              │               └──► ROLLED_BACK
    │              │
    │              └──► SYNC_FAILED ──► DRIFT_ALERT
    │
    └──► GIT_MIRROR_FAILED
              │
              └──► (retry) ──► PENDING
```

**Timeouts por estado:**

| Estado | Timeout | Ação |
|--------|---------|------|
| QUEUED (Build) | 5 min | → FAILED |
| RUNNING (Build) | 30 min | → FAILED |
| RELEASING (Build) | 10 min | → FAILED |
| READY (Release) | 5 min | → GIT_MIRROR_FAILED |
| PENDING (Release) | 10 min (reaper) | → SYNC_FAILED |

### 4.3 Idempotência Ponta-a-Ponta

A idempotência é garantida em cada camada:

| Camada | Mecanismo |
|--------|-----------|
| **API** | `Idempotency-Key` header + SHA256 do body, tenant-scoped, TTL 24h |
| **Gitea** | Push idempotente — "nothing to commit" retorna HEAD SHA |
| **Operators** | `controllerutil.CreateOrUpdate` — cria se não existe, atualiza se diferente |
| **State Machine** | Transições estritamente definidas; transição inválida causa panic (programming error) |

**Fluxo de idempotência na API:**

```
Request com Idempotency-Key
    │
    ├── Key não existe → Processa, armazena resultado, retorna response
    │
    ├── Key existe + mesmo hash → Retorna response cacheada (replay)
    │
    └── Key existe + hash diferente → 422 IDEMPOTENCY_MISMATCH
```

### 4.4 Environment Targets — Source of Truth

A tabela `environment_targets` é o ponteiro canônico para o estado de cada environment:

```sql
CREATE TABLE environment_targets (
    tenant_id         TEXT NOT NULL,
    project_id        TEXT NOT NULL,
    environment       TEXT NOT NULL,
    desired_release_id TEXT NOT NULL REFERENCES releases(id),
    active_release_id  TEXT REFERENCES releases(id),
    updated_by        TEXT NOT NULL,
    correlation_id    TEXT NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, project_id, environment)
);
```

| Campo | Significado | Quando atualiza |
|-------|-------------|-----------------|
| `desired_release_id` | O que o developer pediu | No deploy (Phase C, dentro do lock) |
| `active_release_id` | O que está realmente rodando | Após ArgoCD confirmar sync + health |

**Regra de reconciliação:** Quando Git e DB discordam, `environment_targets` é canônico
(exceto durante PITR — Point-In-Time Recovery).

---

## 5. Mecanismos de Disponibilidade

### 5.1 Circuit Breakers

O LoopZero implementa três circuit breakers calibrados por tipo de falha:

```
┌─────────────────────────────────────────────────────────────┐
│                     CIRCUIT BREAKERS                         │
│                                                              │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────┐ │
│  │ Argo Workflows  │  │    ArgoCD       │  │  PG Pool    │ │
│  │                 │  │                 │  │             │ │
│  │ Threshold: 3    │  │ Threshold: 3    │  │ Threshold:  │ │
│  │ Retry: 30s      │  │ Retry: 30s      │  │ 90% pool    │ │
│  │                 │  │                 │  │             │ │
│  │ Escopo:         │  │ Escopo:         │  │ Escopo:     │ │
│  │ Submit only     │  │ Sync only       │  │ Mutations   │ │
│  │ (reads passam)  │  │ (auth excluída) │  │ (GETs ok)   │ │
│  └─────────────────┘  └─────────────────┘  └─────────────┘ │
│                                                              │
│  Estados: CLOSED ──► OPEN ──► HALF_OPEN ──► CLOSED          │
│                 3 falhas    30s espera     1 sucesso          │
└─────────────────────────────────────────────────────────────┘
```

**Decisões de design notáveis:**

- **Auth failures (401/403) não contam** no circuit breaker do ArgoCD. Credential problem ≠
  transient failure. Contar auth como falha abriria o circuit incorretamente quando o problema
  real é rotação de token.

- **409 "operation in progress" não conta** no ArgoCD. Significa sync travado, não plataforma
  indisponível. O tratamento correto é `TerminateOperation` seguido de re-sync.

- **PG circuit breaker protege mutations mas permite reads.** Em cenário de pool saturado, o
  sistema ainda responde queries de status (GET) — degradação graciosa.

- **Estado por réplica (in-memory), não compartilhado.** Cada réplica da API detecta falha
  independentemente. Aceitável porque cada réplica faz suas próprias chamadas e detecta failure
  em ~9s (3 falhas × ~3s). CB state não sobrevive restart, mas isso é correto — starts CLOSED,
  detecta falha naturalmente.

### 5.2 Deploy em 5 Fases com Lock Mínimo

A ativação de release segue um design de 5 fases que minimiza o tempo de lock:

```
Phase A ─── LOCK ──────► Create release (READY)
                         Check concurrent builds
Phase B ─── NO LOCK ───► Push manifests to Gitea (3 retries)
Phase C ─── LOCK ──────► Record result in DB
                         Set desired_release_id
                         Transition → PENDING or GIT_MIRROR_FAILED
Phase D ─── ASYNC ─────► TriggerSync (fire-and-forget)
Phase E ─── OPTIONAL ──► Failsafe se sync trigger falhou
```

**Por que o lock é liberado na Phase B:**

O push para Gitea envolve I/O de rede (potencialmente 3 retries × timeout). Segurar o advisory
lock durante essa operação bloquearia **todos os outros deploys** para o mesmo environment.
Liberando o lock antes do push:

1. O environment fica desbloqueado imediatamente se o push falhar
2. Outro deploy pode ser iniciado enquanto o push está em andamento
3. O concurrent-build check (Phase A, dentro do lock) garante que não há dois builds ativos

O trade-off é que a Phase C precisa re-adquirir o lock — mas isso é um overhead de milissegundos
comparado ao potencial bloqueio de segundos do push.

### 5.3 Convergência em 3 Camadas

O LoopZero usa três mecanismos para garantir que toda release chegue a um estado terminal:

```
            ┌──────────────────────────────────┐
            │    ArgoCD Callback (Webhook)      │  ← Fast path: ~5-30s
            │    SyncCallbackActivator          │
            └───────────────┬──────────────────┘
                            │ Se falhar...
            ┌───────────────▼──────────────────┐
            │    Convergence Loop (Polling 10s) │  ← Safety net: ~10-90s
            │    ReleaseConvergenceLoop         │
            │    - Verifica revision match      │
            │    - Retrigger sync (max 3×)      │
            └───────────────┬──────────────────┘
                            │ Se esgotar...
            ┌───────────────▼──────────────────┐
            │    Release Reaper (10 min)        │  ← Hard timeout
            │    → SYNC_FAILED                  │
            └──────────────────────────────────┘
```

**Verificação de revisão (defense-in-depth):**

Tanto o callback quanto o convergence loop verificam que a revisão sincronizada pelo ArgoCD
corresponde ao `GitCommit` da release:

```
release.GitCommit == argocd.GetSyncRevision()
```

Se houver mismatch (ex: alguém fez push manual no Gitea), o sync é retrigado com a revisão
correta. Isso previne o cenário "silent wrong-version activation" onde o ArgoCD reporta
sync healthy, mas está rodando uma versão diferente da esperada.

---

## 6. Tolerância a Partições

### 6.1 Partição mgmt ↔ build

**Cenário:** O build cluster fica inacessível (rede, cluster down, Argo Workflows crashou).

```
┌──────────┐                ┌──────────────┐
│  mgmt    │ ──── X ────── │    build      │
│  (API)   │   partition    │ (Argo Wkfl)  │
└──────────┘                └──────────────┘
```

| Impacto | Comportamento |
|---------|---------------|
| Novos builds | Circuit breaker abre → 503 `PLATFORM_DEGRADED` |
| Builds em andamento | Ficam em RUNNING até timeout (30 min) → FAILED |
| Deploys existentes | **Sem impacto** — workloads já estão no runtime |
| Recovery | Circuit breaker testa half-open após 30s |

### 6.2 Partição mgmt ↔ runtime

**Cenário:** O runtime cluster fica inacessível (ArgoCD unreachable, rede entre clusters).

```
┌──────────┐                ┌──────────────┐
│  mgmt    │ ──── X ────── │   runtime     │
│ (ArgoCD) │   partition    │  (workloads)  │
└──────────┘                └──────────────┘
```

| Impacto | Comportamento |
|---------|---------------|
| Releases novas | Ficam em PENDING (não erro) |
| Sync trigger | Circuit breaker abre → convergence loop retenta cada 10s |
| Workloads existentes | **Continuam rodando** (ArgoCD agentless, apps não dependem de mgmt) |
| Secrets (ESO) | Cacheados localmente; novos não propagam |
| Autoscaling (KEDA) | Métricas stale; mantém pods no máximo (conservador) |
| Hard timeout | Releases → SYNC_FAILED após 10 min |
| Recovery | ArgoCD reconecta → convergence loop ativa releases pendentes |

**Ponto crítico:** Os workloads dos tenants **não param** durante esta partição. ArgoCD é
reconciliation-based — ele mantém o estado desejado no cluster. Se o ArgoCD server fica
inacessível, os pods continuam rodando. O impacto é que **novos deploys não ativam** até recovery.

### 6.3 Partição API ↔ PostgreSQL

**Cenário:** PostgreSQL fica inacessível (network, PG crash, pool exhaustion).

```
┌──────────┐                ┌──────────────┐
│   API    │ ──── X ────── │  PostgreSQL   │
└──────────┘   partition    └──────────────┘
```

| Impacto | Comportamento |
|---------|---------------|
| Mutations (POST/PUT/DELETE) | 503 imediato (pool saturation CB) |
| Reads (GET) | Degradam se pool > 95% |
| Advisory locks | Impossíveis → zero deploys |
| Health check | `/health/deep` retorna `database: unreachable`, 503 |
| Recovery | PG reconnecta → pool se recupera automaticamente |

**Esta é a partição mais impactante.** PostgreSQL é caminho crítico para todas as operações
de escrita. Não há modo degradado — PG indisponível = plataforma indisponível para mutations.

### 6.4 Partição com Cloudflare

**Cenário:** CF Edge ou CF Tunnel indisponível.

```
                   ┌───────────┐
Internet ── X ──── │ CF Tunnel │ ──── Traefik ──── Apps
          partition└───────────┘
```

| Impacto | Comportamento |
|---------|---------------|
| Tráfego externo | **100% indisponível** |
| Tráfego interno (cluster-to-cluster) | Sem impacto (não usa CF) |
| API (operações) | Sem impacto (API acesso interno) |
| Alerta | `LoopZeroCFTunnelDisconnected` após 2 min |
| Recovery | Restart cloudflared ou recriação tunnel |

---

## 7. Classificação PACELC Completa

| Componente | Partição (A vs C) | Normal (L vs C) | PACELC | Justificativa |
|------------|-------------------|-----------------|--------|---------------|
| PostgreSQL | **PC** (erro > inconsistência) | **EC** (consistente, ~ms) | PC/EC | Mutações financeiras exigem consistência forte |
| Gitea | **PC** (falha > corromper Git) | **EC** (push síncrono) | PC/EC | Git é linearizável; corrupção = catástrofe |
| ArgoCD sync | **PA** (PENDING, não erro) | **EL** (async, ~30s) | PA/EL | Delay aceitável; erro visível ao dev não é |
| SSE events | **PA** (replay, não perde) | **EL** (streaming real-time) | PA/EL | Eventos persistidos; reconexão automática |
| Environment Targets | **PC** (lock obrigatório) | **EC** (read committed) | PC/EC | Source of truth; inconsistência = deploy errado |
| ESO secrets | **PA** (cache local) | **EL** (sync em background) | PA/EL | Workloads não param por secret stale |
| KEDA scaling | **PA** (último valor, assume alta) | **EL** (scrape periódico) | PA/EL | Preferível custo extra a derrubar pods |
| Valkey | **PA** (in-memory fallback) | **EL** (sub-ms cache) | PA/EL | Sessions degradam, dados não se perdem |
| CF Tunnel | **N/A** (SPOF) | **EL** (edge routing) | — | Sem partição tolerável; ponto único |

**Padrão emergente:** O LoopZero é **PC/EC no caminho de escrita** (todas as mutações passam por PG +
locks) e **PA/EL no caminho de convergência e leitura** (ArgoCD, SSE, ESO, KEDA operam com
eventual consistency e baixa latência).

Essa é a combinação correta para um PaaS: **deploy errado é pior que deploy atrasado**.

---

## 8. Gaps Conhecidos e Mitigações Planejadas

### 8.1 Cloudflare Tunnel como SPOF

| Item | Detalhe |
|------|---------|
| **Risco** | Outage CF = 100% indisponibilidade para tráfego externo |
| **Impacto** | Todos os tenants afetados simultaneamente |
| **Mitigação atual** | 2 réplicas cloudflared + alerta 2min |
| **Mitigação planejada** | Multi-path ingress (CF + fallback direto via LoadBalancer) para tier Enterprise |
| **Sprint** | Pós-launch (Sprint 9+) |

### 8.2 PostgreSQL sem Graceful Degradation

| Item | Detalhe |
|------|---------|
| **Risco** | PG down = zero mutations possíveis |
| **Impacto** | Plataforma operacionalmente inerte para escritas |
| **Mitigação atual** | PG HA com réplica síncrona + WAL archiving + backup diário |
| **Mitigação planejada** | Read-only mode com status cache para queries de status durante PG outage |
| **Sprint** | Hardening (Sprint 8 Phase 3+) |

### 8.3 Valkey/Redis sem HA

| Item | Detalhe |
|------|---------|
| **Risco** | Pod único; crash = Gitea degrada para in-memory |
| **Impacto** | Session affinity perdida; `global_lock` do Gitea indisponível |
| **Mitigação atual** | PVC 1Gi para persistência; probes habilitados |
| **Mitigação planejada** | Sentinel ou managed Valkey (DO) |
| **Sprint** | Hardening |

### 8.4 Ausência de Reconciliation Job Git↔DB

| Item | Detalhe |
|------|---------|
| **Risco** | Drift silencioso entre `active_release_id` e versão real no cluster |
| **Impacto** | DB pode achar que R1 está ativo enquanto R2 está rodando |
| **Mitigação atual** | Revision check no convergence loop e callback |
| **Mitigação planejada** | CronJob de reconciliação periódica (verificar ArgoCD revision vs DB) |
| **Sprint** | Sprint 9 |

### 8.5 KEDA sem Degradação Inteligente

| Item | Detalhe |
|------|---------|
| **Risco** | Sem métricas, KEDA mantém pods no máximo → custo inesperado |
| **Impacto** | Financeiro (pods rodando sem carga real) |
| **Mitigação atual** | Conservador por design (seguro, mas caro) |
| **Mitigação planejada** | Fallback para HPA nativo com métricas locais do runtime cluster |
| **Sprint** | Pós-launch |

---

## 9. Referências

### Papers e Artigos

- Gilbert, S., Lynch, N. (2002). [Brewer's Conjecture and the Feasibility of Consistent, Available, Partition-Tolerant Web Services](http://dl.acm.org/citation.cfm?id=564601). *ACM SIGACT News*, Vol. 33(2), pp. 51-59.
- Brewer, E. (2012). [CAP Twelve Years Later: How the "Rules" Have Changed](https://www.infoq.com/articles/cap-twelve-years-later-how-the-rules-have-changed/). *IEEE Computer*, Vol. 45(2).
- Abadi, D. (2012). [Consistency Tradeoffs in Modern Distributed Database System Design](http://cs-www.cs.yale.edu/homes/dna/papers/abadi-pacelc.pdf). *IEEE Computer*, Vol. 45(2).
- Kleppmann, M. (2015). [Please stop calling databases CP or AP](https://martin.kleppmann.com/2015/05/11/please-stop-calling-databases-cp-or-ap.html).

### Código-fonte do LoopZero (referências internas)

| Mecanismo | Arquivo |
|-----------|---------|
| Advisory locks | `api/internal/lock/environment_lock.go` |
| Circuit breaker (Argo) | `api/internal/services/workflow_circuit_breaker.go` |
| Circuit breaker (ArgoCD) | `api/internal/services/pg_circuit_breaker.go` |
| Circuit breaker (PG pool) | `api/internal/middleware/pgcircuitbreaker.go` |
| Release convergence | `api/internal/services/release_convergence_loop.go` |
| Sync callback | `api/internal/services/sync_callback_activator.go` |
| Release reaper | `api/internal/services/release_reaper.go` |
| Gitea push | `api/internal/gitops/gitea_pusher.go` |
| ArgoCD sync | `api/internal/argocd/sync_client.go` |
| Environment targets | `api/internal/db/environment_target_store.go` |
| State machines | `pkg/statemachine/release.go`, `build.go` |
| Health check | `api/internal/routes/health.go` |
| SSE streaming | `api/internal/sse/stream_handler.go` |
| Idempotency | `api/internal/middleware/idempotency.go` |
| ClusterCredential | `operators/internal/platform/clustercredential_controller.go` |
| CF Tunnel | `infra/pulumi/src/providers/cloudflare/tunnel.ts` |
| ESO | `infra/helm/external-secrets/values.yaml` |
| Timeouts | `operators/internal/constants/timeouts.go` |

### ADRs relacionados

- ADR-0010: Sizing de clusters e targets de recursos
- ADR-0015: Adopt-or-create policy para infraestrutura
- MEET-052: Circuit breaker para Gitea provisioner
- MEET-058: Operator Application CR ownership
