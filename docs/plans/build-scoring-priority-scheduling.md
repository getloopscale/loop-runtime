# Plan: Build Scoring & Priority Scheduling

> **Version 1.0** — Implementar scoring de builds baseado em frequencia x duracao historica para priorizar pipelines rapidos e frequentes sobre lentos e raros. Inspirado na tecnica do Flipkart Flow que dobrou throughput de 4K para 8K builds/dia sem hardware adicional. Aplicado ao contexto do LoopZero usando Argo Workflows priority + Kubernetes PriorityClass.
>
> **Referencia tecnica:** [arXiv:2504.18705](https://arxiv.org/abs/2504.18705) — "On Queueing LoopZerory for Large-Scale CI/CD Pipelines Optimization" (Bournassenko, 2025). Formaliza size-based scheduling para CI/CD.
>
> **Pre-requisito:** Nenhum. Phase 1 e pura coleta de dados e pode ser implementada imediatamente. Phases 2-3 dependem dos dados coletados.

## Context

### O problema

O LoopZero hoje trata todos os builds como iguais — FIFO sem prioridade. Um microservico Go de 2 min e um app Next.js de 15 min competem pela mesma fila e pelos mesmos nos. Com o build cluster atual (2 nos, ~10 builds concorrentes), isso funciona. Quando escalar para dezenas de tenants com centenas de builds/dia, builds rapidos ficarao bloqueados atras de builds lentos.

### O que existe hoje

**Admission control (2 camadas):**

| Camada | Limite | Config |
|---|---|---|
| Cell-wide | 20 builds | `MAX_CONCURRENT_BUILDS_PER_CELL` |
| Per-tenant | 3 builds | `MAX_CONCURRENT_BUILDS_PER_TENANT` |

**Reconcile loop (2 semaforos):**

| Camada | Limite | Config |
|---|---|---|
| Global | 10 concurrent | `BuildReconcileLoopConfig.MaxConcurrent` |
| Per-tenant | 3 concurrent | `BuildReconcileLoopConfig.MaxBuildsPerTenant` |

**Workflow scheduling:**
- `workflowParallelism = 4` (DAG steps dentro de 1 workflow)
- Pod affinity: todos os pods co-locados no mesmo no (RWO PVC)
- Zero priority, zero nodeSelector, zero PriorityClass
- FIFO: builds processados na ordem de descoberta do DB

**Duracao ja rastreada:**
- `builds.duration_seconds` — calculado no estado terminal (`finished_at - started_at`)
- `builds.started_at`, `builds.finished_at` — timestamps existem
- `build_apps.duration_ms` — duracao por app individual
- `loopzero_build_duration_seconds` — histogram Prometheus com buckets [5, 10, 30, 60, 120, 180, 300, 600]

**O que NAO existe:**
- Nenhuma coluna de score/priority no schema
- Nenhum campo `priority` ou `priorityClassName` no Workflow CR gerado
- Nenhum historico agregado por projeto/app (media movel, p95)
- Nenhum mecanismo de roteamento baseado em perfil
- Nenhum node pool separado para builds

### Referencia: como o Flipkart resolveu

O Flipkart Flow enfrentou o mesmo problema a 4K builds/dia. Solucao:

1. **Observou** que 90% dos pipelines rodam em <10 min (frequentes) e 10% levam 15-60 min (infrequentes)
2. **Criou um score** = f(frequencia, duracao) por pipeline
3. **Particionou** o node pool em 2: short-running (alta rotatividade) e long-running (menos contencao)
4. **Score dinamico**: se um build fica mais rapido, migra automaticamente de pool

Resultado: 4K → 8K builds/dia, zero hardware adicional.

### Termos da industria para o pattern

| Termo | Dominio |
|---|---|
| **Multi-Level Feedback Queue (MLFQ)** | OS scheduling — jobs migram entre filas por comportamento observado |
| **Size-based scheduling** | Queueing loopzerory — rotear por tempo esperado de execucao |
| **Workload partitioning** | Cloud/HPC — separar por perfil em pools distintos |
| **Hot/cold path splitting** | Arquitetura — fast-path para leve e frequente |

### Ferramentas disponiveis

**Argo Workflows (ja em uso):**
- `spec.priority` (integer) — ordenacao na fila do controller. Maior = processado primeiro
- `spec.priorityClassName` — mapeia para K8s PriorityClass. Controla scheduling e preemption de pods

**Kubernetes nativo:**
- `PriorityClass` — define prioridade + preemption behavior
- `nodeSelector` / `tolerations` — roteamento para node pools especificos
- Multiple scheduler profiles — bin-packing para batch, spread para services

**Kueue (kubernetes-sigs/kueue) — alternativa futura:**
- LocalQueue → ClusterQueue com quotas
- WorkloadPriorityClass separado do K8s PriorityClass
- Fair sharing com weighted share entre tenants
- Preemption nativa
- Avaliacao: recomendado apenas se Phase 2 nao for suficiente

## Objective

**Done = builds rapidos e frequentes nao ficam bloqueados por builds lentos, com dados para provar.**

Metas mensuraveis:
1. Dashboard com perfil de cada projeto/app (media, p95, frequencia, score)
2. Builds com score alto (rapidos/frequentes) tem p95 queue wait < 30s
3. Builds com score baixo (lentos/raros) nao degradam throughput dos rapidos
4. Zero regressao em builds existentes (nenhum build fica mais lento)

## Phases

### Phase 1 — Build Profiling (coleta de dados)

> **Objetivo:** Coletar dados historicos de duracao e frequencia por projeto/app para calcular scores. Nenhuma mudanca de scheduling — apenas observabilidade.
>
> **Esforco estimado:** Pequeno. 1 migration, 1 funcao SQL, 1 endpoint API, metricas Prometheus.

#### 1.1 — Migration: tabela `build_profiles`

Criar tabela para armazenar perfil agregado por projeto+app+environment:

```sql
-- migration: 000XXX_create_build_profiles.up.sql

CREATE TABLE build_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Chave composta: perfil por projeto+app+environment
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    app_name TEXT NOT NULL,
    environment TEXT NOT NULL,

    -- Metricas agregadas (ultimos 30 dias, rolling window)
    total_builds INTEGER NOT NULL DEFAULT 0,
    successful_builds INTEGER NOT NULL DEFAULT 0,
    avg_duration_seconds INTEGER NOT NULL DEFAULT 0,
    p95_duration_seconds INTEGER NOT NULL DEFAULT 0,
    max_duration_seconds INTEGER NOT NULL DEFAULT 0,
    min_duration_seconds INTEGER NOT NULL DEFAULT 0,
    builds_per_day NUMERIC(6,2) NOT NULL DEFAULT 0.0,

    -- Score calculado
    score INTEGER NOT NULL DEFAULT 50,           -- 1 (lento/raro) a 100 (rapido/frequente)
    score_class TEXT NOT NULL DEFAULT 'default',  -- 'fast', 'default', 'heavy'

    -- Metadata
    last_build_at TIMESTAMPTZ,
    recalculated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (tenant_id, project_id, app_name, environment)
);

CREATE INDEX idx_build_profiles_score ON build_profiles (score_class, score DESC);
CREATE INDEX idx_build_profiles_tenant ON build_profiles (tenant_id);
```

**Regras:**
- Forward-only migration (padrao LoopZero — CLAUDE.md Rule)
- Tabela separada (nao poluir `builds` com colunas agregadas)
- Score recalculado periodicamente, nao em cada build

#### 1.2 — Score calculation function

Funcao SQL para recalcular o score de todos os perfis (executada periodicamente):

```sql
-- Funcao de recalculo (chamada via cron ou reconcile loop)
-- Score = f(frequencia_normalizada, velocidade_normalizada)
-- Frequencia alta + duracao baixa = score alto (fast lane)
-- Frequencia baixa + duracao alta = score baixo (heavy lane)

-- Implementacao em Go (nao SQL puro) para manter logica testavel:
```

```go
// api/internal/services/build_profile.go

const (
    ScoreClassFast    = "fast"    // score >= 70
    ScoreClassDefault = "default" // score 30-69
    ScoreClassHeavy   = "heavy"   // score < 30
)

func CalcBuildScore(avgDuration time.Duration, buildsPerDay float64) (int, string) {
    // Normalizar duracao: <5min = 100, >30min = 0, linear entre
    durationScore := 100 - clamp(int(avgDuration.Minutes()-5)*4, 0, 100)

    // Normalizar frequencia: >5/dia = 100, <0.1/dia = 0, log scale
    freqScore := clamp(int(math.Log10(buildsPerDay*10)*50), 0, 100)

    // Score composto: 60% duracao, 40% frequencia
    // Duracao pesa mais porque e o que bloqueia a fila
    score := clamp((durationScore*60 + freqScore*40) / 100, 1, 100)

    class := ScoreClassDefault
    if score >= 70 {
        class = ScoreClassFast
    } else if score < 30 {
        class = ScoreClassHeavy
    }

    return score, class
}
```

**Pesos da formula:**
- **60% duracao** — builds longos sao o que bloqueiam a fila. Um build de 30 min ocupa o no inteiro por 30 min.
- **40% frequencia** — builds frequentes sofrem mais com espera. Se roda 10x/dia e espera 2 min cada vez, sao 20 min desperdicados/dia.

**Thresholds de classe:**
- `fast` (score >= 70): builds rapidos (<10 min) E frequentes (>1/dia). Ex: microservico Go, API Express
- `default` (score 30-69): perfil misto. Ex: app Next.js medio, serviço Python
- `heavy` (score < 30): builds lentos (>15 min) OU muito raros (<0.1/dia). Ex: app Android, monolito Java, projeto abandonado

#### 1.3 — Recalculo periodico

Adicionar ao reconcile loop existente (ou criar goroutine dedicada):

```go
// api/internal/services/build_profile_reconciler.go

// Executar a cada 1 hora (nao precisa ser real-time)
func (r *BuildProfileReconciler) Recalculate(ctx context.Context) error {
    // 1. Agregar builds dos ultimos 30 dias por (tenant, project, app, env)
    // 2. Calcular avg, p95, max, min, builds_per_day
    // 3. Calcular score via CalcBuildScore()
    // 4. UPSERT em build_profiles
    return nil
}
```

Query de agregacao:

```sql
SELECT
    tenant_id, project_id, app_name, environment,
    COUNT(*) as total_builds,
    COUNT(*) FILTER (WHERE status = 'SUCCEEDED') as successful_builds,
    AVG(duration_seconds)::int as avg_duration,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_seconds)::int as p95_duration,
    MAX(duration_seconds) as max_duration,
    MIN(duration_seconds) as min_duration,
    COUNT(*)::numeric / 30.0 as builds_per_day,
    MAX(finished_at) as last_build_at
FROM builds b
JOIN build_apps ba ON ba.build_id = b.id
WHERE b.finished_at > NOW() - INTERVAL '30 days'
  AND b.status IN ('SUCCEEDED', 'FAILED')
  AND b.duration_seconds IS NOT NULL
GROUP BY tenant_id, project_id, app_name, environment;
```

#### 1.4 — Metricas Prometheus

```go
// api/internal/metrics/metrics.go

// Score distribution
BuildProfileScore = NewGaugeVec(
    "loopzero_build_profile_score",
    []string{"tenant", "project", "app", "environment", "score_class"})

// Builds por classe
BuildsByScoreClass = NewCounterVec(
    "loopzero_builds_by_score_class_total",
    []string{"score_class", "status"})

// Queue wait time por classe (quanto tempo entre QUEUED e RUNNING)
BuildQueueWaitByClass = NewHistogramVec(
    "loopzero_build_queue_wait_seconds",
    []string{"score_class"},
    buckets: [1, 5, 10, 30, 60, 120, 300])
```

#### 1.5 — Endpoint API

```go
// GET /api/v1/admin/build-profiles?tenant_id=xxx
// Retorna perfis com scores para dashboard/debugging

// GET /api/v1/admin/build-profiles/distribution
// Retorna: { "fast": 45, "default": 30, "heavy": 25 }
// (percentual de builds em cada classe — equivalente ao "90/10" do Flipkart)
```

#### 1.6 — Testes (Phase 1)

| Teste | Tipo | O que valida |
|---|---|---|
| `TestCalcBuildScore_FastFrequent` | Unit | Build de 3 min, 5x/dia → score >= 70, class=fast |
| `TestCalcBuildScore_SlowRare` | Unit | Build de 25 min, 0.1x/dia → score < 30, class=heavy |
| `TestCalcBuildScore_Medium` | Unit | Build de 8 min, 1x/dia → score 30-69, class=default |
| `TestCalcBuildScore_EdgeCases` | Unit | duracao=0, frequencia=0, valores negativos |
| `TestBuildProfileReconciler_Aggregation` | Integration | 50 builds inseridos, recalculo produz perfis corretos |
| `TestBuildProfileReconciler_RollingWindow` | Integration | Builds antigos (>30 dias) nao afetam score |
| `TestBuildProfileReconciler_ScoreMigration` | Integration | Build fica mais rapido → score sobe → class muda |
| `TestBuildProfileAPI_ListProfiles` | Component | GET retorna perfis com paginacao |
| `TestBuildProfileAPI_Distribution` | Component | GET retorna distribuicao por classe |

#### Phase 1 — Criterio de conclusao

- [ ] Migration aplicada, tabela `build_profiles` existe
- [ ] `CalcBuildScore()` implementado com testes unitarios
- [ ] Reconciler roda a cada 1h e popula perfis
- [ ] Metricas Prometheus emitidas (score, distribution, queue wait by class)
- [ ] Endpoint API funcional
- [ ] Dashboard Grafana com painel de distribuicao de scores
- [ ] Rodar 1 semana coletando dados antes de prosseguir para Phase 2

---

### Phase 2 — Priority Scheduling (Argo Workflows)

> **Objetivo:** Usar o score calculado na Phase 1 para definir prioridade nos Workflows do Argo. Builds rapidos/frequentes sao processados primeiro quando ha contencao.
>
> **Pre-requisito:** Phase 1 completa + 1 semana de dados coletados para validar distribuicao.
>
> **Esforco estimado:** Medio. Mudancas em workflow_builder.go, 2 PriorityClasses, ajuste no admission gate.

#### 2.1 — Kubernetes PriorityClasses

Criar via Helm chart (`infra/helm/argo-workflows/templates/priority-classes.yaml`):

```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-build-fast
value: 100
globalDefault: false
preemptionPolicy: Never  # NAO evict builds em andamento
description: "Fast/frequent builds - processed first in queue"
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-build-default
value: 50
globalDefault: false
preemptionPolicy: Never
description: "Default priority builds"
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-build-heavy
value: 10
globalDefault: false
preemptionPolicy: Never
description: "Heavy/infrequent builds - yields to faster builds in queue"
```

**Decisao critica: `preemptionPolicy: Never`**

NAO queremos preemption (evictar um build em andamento para dar lugar a outro). Um build evictado perde todo o progresso e precisa recomecar. O custo de reiniciar um build de 20 min e maior que o custo de esperar 2 min na fila. Prioridade afeta apenas a **ordem da fila**, nao evicta nada.

#### 2.2 — Injetar priority no Workflow Builder

```go
// api/internal/argo/workflow_builder.go

func (b *WorkflowBuilder) Build(spec services.WorkflowSpec) (*forge.Workflow, error) {
    // ... existing code ...

    wf := &forge.Workflow{
        // ... existing fields ...
        Spec: forge.WorkflowSpec{
            Priority:          spec.Priority,          // NEW
            PriorityClassName: spec.PriorityClassName, // NEW
            // ... rest unchanged ...
        },
    }
}
```

```go
// api/internal/services/build_orchestrator_submit.go
// Na Phase 2 (OUTSIDE lock), antes de Submit:

func (o *BuildOrchestrator) resolvePriority(ctx context.Context, req SubmitBuildRequest) (int, string) {
    profile, err := o.profileStore.Get(ctx, req.TenantID, req.ProjectID, req.Apps[0].Name, req.Environment)
    if err != nil || profile == nil {
        // Primeiro build ou perfil nao existe: default
        return 50, "loopzero-build-default"
    }

    switch profile.ScoreClass {
    case "fast":
        return 100, "loopzero-build-fast"
    case "heavy":
        return 10, "loopzero-build-heavy"
    default:
        return 50, "loopzero-build-default"
    }
}
```

#### 2.3 — Metrica de eficacia

Adicionar metrica para medir se a prioridade esta funcionando:

```go
// Comparar queue wait ANTES e DEPOIS da Phase 2
// Se Phase 2 funciona: p95 queue wait de "fast" deve cair, "heavy" pode subir (aceitavel)
BuildQueueWaitByPriority = NewHistogramVec(
    "loopzero_build_queue_wait_by_priority_seconds",
    []string{"priority_class"},
    buckets: [1, 5, 10, 30, 60, 120, 300])
```

#### 2.4 — Testes (Phase 2)

| Teste | Tipo | O que valida |
|---|---|---|
| `TestWorkflowBuilder_InjectsPriority` | Unit | Workflow gerado tem priority e priorityClassName corretos |
| `TestResolvePriority_FastProfile` | Unit | Profile fast → priority=100, class=loopzero-build-fast |
| `TestResolvePriority_NoProfile` | Unit | Sem perfil → default (nao quebra builds novos) |
| `TestResolvePriority_HeavyProfile` | Unit | Profile heavy → priority=10, class=loopzero-build-heavy |
| `TestPriorityClasses_NoPreemption` | E2E | Build heavy em andamento NAO e evictado por build fast |
| `TestPriorityOrdering_UnderContention` | E2E (capacity) | 10 builds simultaneos: fast starts before heavy |

#### Phase 2 — Criterio de conclusao

- [ ] 3 PriorityClasses criadas no build cluster
- [ ] Workflow builder injeta priority baseado no perfil
- [ ] Builds novos (sem perfil) recebem prioridade default
- [ ] Metrica queue_wait_by_priority mostra diferenca mensuravel entre classes
- [ ] Zero regressao: nenhum build existente fica mais lento (validar com capacity test L2)
- [ ] preemptionPolicy=Never confirmado — builds em andamento nunca sao evictados

---

### Phase 3 — Node Pool Partitioning (condicional)

> **Objetivo:** Separar builds rapidos e pesados em node pools distintos para isolamento de recursos. So implementar se Phase 2 nao for suficiente.
>
> **Pre-requisito:** Phase 2 completa + evidencia de que priority sozinha nao resolve (queue wait de fast > 30s sob carga).
>
> **Trigger para Phase 3:** `p95(loopzero_build_queue_wait_by_priority_seconds{priority_class="loopzero-build-fast"}) > 30s` por 7 dias consecutivos.
>
> **Esforco estimado:** Grande. Mudancas em Pulumi (infra), Helm, workflow builder, admission gate.

#### 3.1 — Dois node pools no build cluster

```typescript
// infra/pulumi/src/components/build-argo.ts

// Pool para builds rapidos: nos menores, mais quantidade, alta rotatividade
buildCluster.addNodePool("fast-builds", {
    size: "s-2vcpu-4gb",
    minNodes: 2,
    maxNodes: 6,  // auto-scale agressivo
    autoScale: true,
    labels: { "loopzero.io/build-pool": "fast" },
    taints: [{
        key: "loopzero.io/build-pool",
        value: "fast",
        effect: "NoSchedule"
    }]
});

// Pool para builds pesados: nos maiores, menos quantidade
buildCluster.addNodePool("heavy-builds", {
    size: "s-4vcpu-8gb",
    minNodes: 1,
    maxNodes: 3,
    autoScale: true,
    labels: { "loopzero.io/build-pool": "heavy" },
    taints: [{
        key: "loopzero.io/build-pool",
        value: "heavy",
        effect: "NoSchedule"
    }]
});

// Pool default para system services (Argo controller, etc)
// Mantido sem taints
```

#### 3.2 — Tolerations e nodeSelector no Workflow

```go
// api/internal/argo/workflow_builder.go

func (b *WorkflowBuilder) buildNodeScheduling(scoreClass string) (map[string]string, []corev1.Toleration) {
    pool := "fast" // default para builds novos sem perfil
    if scoreClass == "heavy" {
        pool = "heavy"
    }

    nodeSelector := map[string]string{
        "loopzero.io/build-pool": pool,
    }

    tolerations := []corev1.Toleration{{
        Key:      "loopzero.io/build-pool",
        Value:    pool,
        Operator: corev1.TolerationOpEqual,
        Effect:   corev1.TaintEffectNoSchedule,
    }}

    return nodeSelector, tolerations
}
```

**Fallback:** Se o pool preferido nao tem nos disponiveis, o build deve ir para o outro pool (nao ficar pending). Implementar com `preferredDuringSchedulingIgnoredDuringExecution` em vez de `requiredDuring...`:

```go
// Affinity preferencial (soft), nao obrigatoria (hard)
affinity := &corev1.Affinity{
    NodeAffinity: &corev1.NodeAffinity{
        PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{{
            Weight: 100,
            Preference: corev1.NodeSelectorTerm{
                MatchExpressions: []corev1.NodeSelectorRequirement{{
                    Key:      "loopzero.io/build-pool",
                    Operator: corev1.NodeSelectorOpIn,
                    Values:   []string{pool},
                }},
            },
        }},
    },
}
```

#### 3.3 — Admission gate por pool

Atualizar `BuildGate` para considerar capacidade por pool:

```go
type ActiveBuildsSnapshot struct {
    Total       int
    ByTenant    map[string]int
    ByPool      map[string]int  // NEW: "fast" → 8, "heavy" → 3
}

// Rejeitar se o pool especifico esta cheio, nao so o cell total
func (g *BuildGate) Check(ctx, cellID, tenantID, scoreClass string) (*Result, error) {
    snapshot := g.querier.GetSnapshot(ctx, cellID)

    pool := "fast"
    if scoreClass == "heavy" {
        pool = "heavy"
    }

    poolCap := g.cfg.MaxConcurrentPerPool[pool]
    if snapshot.ByPool[pool] >= poolCap {
        return &Result{Decision: RejectPoolAtCapacity, RetryAfter: calcRetry(...)}, nil
    }

    // ... existing tenant check ...
}
```

#### 3.4 — Testes (Phase 3)

| Teste | Tipo | O que valida |
|---|---|---|
| `TestNodePoolCreation_Pulumi` | Infra unit | 2 pools criados com taints e labels corretos |
| `TestWorkflowScheduling_FastPool` | E2E | Build fast vai para no com label fast |
| `TestWorkflowScheduling_HeavyPool` | E2E | Build heavy vai para no com label heavy |
| `TestWorkflowScheduling_Fallback` | E2E | Se pool fast cheio, build vai para heavy (nao pending) |
| `TestAdmissionGate_PoolCapacity` | Unit | Gate rejeita quando pool especifico esta cheio |
| `TestCapacityL2_WithPools` | Capacity | 30 builds concorrentes distribuidos corretamente entre pools |

#### Phase 3 — Criterio de conclusao

- [ ] 2 node pools criados no build cluster via Pulumi
- [ ] Workflows roteados para pool correto baseado no score_class
- [ ] Fallback funciona: build nunca fica pending por falta de pool
- [ ] Admission gate respeita capacidade por pool
- [ ] Capacity test L2 passa com distribuicao correta
- [ ] Custo de infra documentado (delta de custo dos nos adicionais)

---

## Alternativa futura: Kueue

Se as 3 phases nao forem suficientes (cenario: >500 builds/dia, multiplos build clusters), considerar **Kueue** (kubernetes-sigs/kueue):

| Feature | Argo priority (Phase 2) | Node pools (Phase 3) | Kueue |
|---|---|---|---|
| Ordenacao de fila | Sim | Sim | Sim + fair sharing |
| Preemption configuravel | Limitada | Nao | Sim (por policy) |
| Multi-tenant fairness | Manual (per-tenant semaphore) | Manual | Nativo (weighted share) |
| Multi-cluster dispatch | Nao | Nao | Sim (MultiKueue) |
| Quota borrowing | Nao | Nao | Sim (Cohorts) |
| Complexidade adicional | Baixa | Media | Alta (novo componente) |

**Recomendacao:** Avaliar Kueue apenas quando Phase 3 nao for suficiente. O overhead operacional de mais um componente no build cluster so se justifica em escala de centenas de builds/dia.

## Riscos

| Risco | Impacto | Mitigacao |
|---|---|---|
| Score incorreto classifica build rapido como heavy | Build rapido espera mais | Rolling window de 30 dias amortece outliers. Score recalculado a cada 1h. |
| Preemption acidental evicta build em andamento | Build perde 20+ min de progresso | `preemptionPolicy: Never` em todas as PriorityClasses |
| Phase 3 node pools ociosos (heavy pool vazio) | Custo de infra desperdicado | Auto-scale com minNodes=1. Fallback permite usar pool vazio para outros builds |
| Builds novos (sem historico) ficam em classe errada | Primeiro build de um projeto espera mais | Default = "default" (prioridade 50, meio termo). Perfil atualizado apos primeiro build |
| Complexidade no admission gate com pools | Bug pode bloquear todos os builds | Feature flag per-pool. Admission gate tem fail-open (padrao LoopZero) |

## Sequenciamento

```
Phase 1 (imediato)                Phase 2 (apos 1 semana dados)     Phase 3 (condicional)
├─ Migration                      ├─ PriorityClasses                ├─ Node pools Pulumi
├─ CalcBuildScore()               ├─ Workflow priority injection    ├─ Tolerations/affinity
├─ Profile reconciler             ├─ Metricas de eficacia           ├─ Admission per-pool
├─ Metricas Prometheus            └─ Capacity test L2               ├─ Fallback scheduling
├─ API endpoint                                                     └─ Capacity test L2+
└─ Grafana dashboard
        │
        ▼
   1 semana coletando dados
   Validar distribuicao (espera-se ~80/20 fast/heavy)
        │
        ▼
   Phase 2 (se distribuicao confirma bimodalidade)
        │
        ▼
   Monitorar 2 semanas
   Trigger Phase 3: p95 queue wait fast > 30s por 7 dias
```

## Referencias

- [arXiv:2504.18705 — Queueing LoopZerory for CI/CD Optimization](https://arxiv.org/abs/2504.18705)
- [Argo Workflows — Priority fields](https://github.com/argoproj/argo-workflows/blob/main/docs/fields.md)
- [Kubernetes — Pod Priority and Preemption](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-priority-preemption/)
- [Kueue — Kubernetes Job Queueing](https://kueue.sigs.k8s.io/docs/overview/)
- [Codefresh — Custom K8s Scheduler for CI](https://codefresh.io/blog/custom-k8s-scheduler-continuous-integration/)
- [Uber — Halving Go Monorepo CI Build Time](https://www.uber.com/blog/how-we-halved-go-monorepo-ci-build-time/)
- [Flipkart Flow — How Flipkart Scales CI/CD for 3000+ Services](https://www.youtube.com/watch?v=7jZ-8TZwxDI)
