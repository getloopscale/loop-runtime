# Plano: KEDA — Scale-to-Zero para Apps de Tenant

**Data:** 2026-04-24
**Status:** DRAFT — aguardando revisão
**Classificação:** T2 (altera contratos: loopzero.yaml, manifest generation, CLI output, plan enforcement)
**Fase:** Pós-launch
**Referência:** `docs/landscape-adoption-opportunities.md` item #6

---

## Problema

Apps de tenant no LoopZero mantêm pelo menos 1 pod rodando 24/7, mesmo sem tráfego. Para o Starter tier (free), isso significa custo fixo por tenant para a plataforma. Em PaaS concorrentes (Render, Railway, Fly.io), scale-to-zero é feature standard para tiers gratuitos.

**Impacto financeiro (estimativa):**
- 100 tenants Starter × 1 pod × ~$5/mês/pod = $500/mês de custo idle
- Com scale-to-zero: $0 quando idle, custo proporcional ao uso real

---

## Estado Atual (baseline)

### O que existe e funciona

| Camada | Arquivo | Estado |
|--------|---------|--------|
| **CLI `loopzero scale`** | `cli/src/commands/scale.ts` | ✅ Completo. Aceita `--replicas`, `--min/--max/--cpu`. Mutual exclusion validada. |
| **CLI registration** | `cli/src/register/scale.ts` | ✅ Registrado. Chama `POST /api/v1/projects/:name/scale`. |
| **loopzero.yaml parser** | `cli/src/adapters/loopzero-yaml.ts:114-131` | ✅ Parseia `autoscaling: { min, max, targetCPU }`. |
| **loopzero.yaml validator** | `cli/src/adapters/validate-config.ts:196-233` | ✅ Valida ranges. **`min >= 1` hardcoded (linha 201)**. |
| **Manifest: HPA** | `api/internal/manifests/renderer_deployment.go:156-206` | ✅ Gera `autoscaling/v2` HPA. CPU-only. |
| **Manifest: helper** | `api/internal/manifests/renderer_helpers.go:60-66` | ✅ `maybeHPA()` chamado em server/worker/frontend render paths. |
| **Manifest: types** | `api/internal/manifests/renderer_types.go:247-280` | ✅ Structs `k8sHPA`, `k8sHPASpec`, etc. |
| **Manifest: Deployment** | `api/internal/manifests/renderer_deployment.go:99-107` | ✅ Replicas = `MinReplicas` quando autoscaling configurado. **`minR < 1` → `minR = 1` (hardcoded)**. |
| **Plan enforcement** | `pkg/enforcement/limiter.go:114` | ✅ `CheckValue("max_replicas_per_service", N)`. Starter=2, Pro=5, Team=10. |
| **Tests** | `api/internal/manifests/renderer_scaling_test.go` (331 lines) | ✅ 9 testes cobrindo HPA, defaults, labels, doc count. |

### O que NÃO existe

| Gap | Impacto |
|-----|---------|
| **API endpoint `/scale`** | CLI bate em 404 → "Scaling is not yet available on this server." |
| **KEDA operator** | Nenhum. Nem Helm chart, nem CRD, nem ScaledObject. |
| **ScaledObject type** | `renderer_types.go` só tem `k8sHPA`. |
| **`min: 0` validation** | `validate-config.ts:201` rejeita `min < 1`. `renderer_deployment.go:101` força `min >= 1`. |
| **Status com replicas** | `loopzero status` não retorna contagem de replicas nem estado de scaling. |
| **Cold start UX** | Nenhuma indicação de que app está em scale-to-zero. |

---

## Decisão: KEDA vs Alternativas

### Opções avaliadas

| Opção | Scale-to-zero | Custom metrics | Cold start | Complexidade |
|-------|:---:|:---:|:---:|:---:|
| **HPA nativo** | ❌ min >= 1 | ❌ CPU/Memory only | N/A | Baixa |
| **KEDA** | ✅ min = 0 | ✅ Prometheus, HTTP, Cron | ~5-10s via HTTP interceptor | Média |
| **Knative Serving** | ✅ min = 0 | ✅ Concurrency-based | <1s (queue proxy) | Alta (requer Knative stack) |
| **Custom controller** | ✅ | ✅ | Customizável | Alta (manutenção) |

### Decisão: KEDA

- **CNCF Graduated** — maturidade e longevidade garantidas
- **Gera HPA internamente** — não conflita com o design atual, substitui o HPA que já geramos
- **Footprint leve** — 1 Deployment + metrics adapter (~100MB RAM)
- **ScaledObject é GitOps-friendly** — CRD gerenciado via ArgoCD como qualquer outro recurso
- **Go SDK não necessário** — interação é 100% via CRD YAML (manifest generation)

### Decisão: KEDA HTTP Add-on para cold start

Quando um app está em 0 replicas, o primeiro request HTTP precisa de um interceptor que:
1. Segura o request
2. Escala para 1 pod
3. Espera o pod ficar ready
4. Proxia o request para o pod

O **KEDA HTTP Add-on** faz exatamente isso. Alternativa: implementar interceptor custom no Traefik middleware — rejeitado por ser reinventar a roda (princípio §9 CLAUDE.md).

---

## Arquitetura

### Fluxo: Deploy com scale-to-zero habilitado

```
loopzero deploy (scaling.min: 0)
  → CLI valida loopzero.yaml (min: 0 permitido)
  → API gera manifests:
     - Deployment (replicas: 0)
     - ScaledObject (minReplicaCount: 0, maxReplicaCount: plan cap)
     - HTTPScaledObject (interceptor para cold start)
     - NÃO gera HPA (KEDA gera internamente)
  → gitops push → ArgoCD sync
  → KEDA escala para 0 quando idle

Request chega com 0 pods:
  → Traefik → KEDA HTTP interceptor
  → KEDA escala Deployment para 1
  → Pod sobe (startup probe: até 150s)
  → Request proxiado para o pod
  → Latência adicional: ~5-10s (cold start)

App idle por cooldownPeriod:
  → KEDA escala para 0
  → Zero custo
```

### Fluxo: Deploy com autoscaling (min >= 1, sem mudança)

```
loopzero deploy (autoscaling.min: 2, max: 10)
  → Mesmo fluxo de hoje
  → Gera HPA nativo (autoscaling/v2)
  → KEDA NÃO envolvido (backward compatible)
```

### Regra de decisão no manifest renderer

```
if app.Autoscaling == nil:
    → Deployment com replicas fixas (1 ou app.Replicas)
    → Sem HPA, sem ScaledObject

if app.Autoscaling.MinReplicas >= 1:
    → Deployment com replicas = MinReplicas
    → HPA nativo (autoscaling/v2) — comportamento atual, zero mudança

if app.Autoscaling.MinReplicas == 0:
    → Deployment com replicas = 0
    → ScaledObject (KEDA) com minReplicaCount: 0
    → HTTPScaledObject (KEDA HTTP Add-on) para cold start
    → NÃO gera HPA (KEDA gera internamente)
```

**Princípio:** backward compatibility total. O KEDA só entra quando `min: 0` é explicitamente configurado. Apps existentes com `min >= 1` continuam usando HPA nativo.

---

## Tasks

### Fase A: Infraestrutura KEDA (3 tasks)

#### A1. Helm chart: KEDA operator

**Tipo:** T1
**Diretório:** `infra/helm/keda/`
**Cluster:** workload (onde os apps de tenant rodam)

```
infra/helm/keda/
├── Chart.yaml
├── values.yaml             # dev (k3d)
├── values.production.yaml  # DO
```

**values.yaml (dev):**

```yaml
# KEDA — Event-driven autoscaler
# Workload cluster only — escala apps de tenant

operator:
  replicaCount: 1
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits: { cpu: 500m, memory: 256Mi }

metricsServer:
  replicaCount: 1
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits: { cpu: 200m, memory: 128Mi }

webhooks:
  enabled: true
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits: { cpu: 200m, memory: 128Mi }

# Prometheus scaler config — usa Prometheus do monitoring stack
prometheus:
  metricServer: http://kube-prometheus-stack-prometheus.monitoring.svc:9090
```

**values.production.yaml (delta):**

```yaml
operator:
  replicaCount: 2  # HA
  resources:
    requests: { cpu: 200m, memory: 256Mi }
    limits: { cpu: 1000m, memory: 512Mi }

metricsServer:
  replicaCount: 2  # HA

podDisruptionBudget:
  operator:
    minAvailable: 1
  metricServer:
    minAvailable: 1
```

**Critérios de aceite:**
- [ ] `helm template` renderiza sem erro
- [ ] `kubeconform --strict` valida os manifests
- [ ] KEDA operator pod running em k3d workload
- [ ] CRDs registrados: `kubectl get crd scaledobjects.keda.sh httpscaledobjects.http.keda.sh`

---

#### A2. Helm chart: KEDA HTTP Add-on

**Tipo:** T1
**Diretório:** `infra/helm/keda-http-addon/`
**Cluster:** workload

O HTTP Add-on é um componente separado do KEDA core. Instala:
- **Interceptor proxy** — recebe requests quando pods = 0
- **Operator** — cria HTTPScaledObject CRDs
- **External scaler** — conecta ao KEDA core

```yaml
# values.yaml
interceptor:
  replicas: 1
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits: { cpu: 200m, memory: 128Mi }

operator:
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits: { cpu: 200m, memory: 128Mi }

scaler:
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits: { cpu: 200m, memory: 128Mi }
```

**Critérios de aceite:**
- [ ] Interceptor pod running
- [ ] CRD `httpscaledobjects.http.keda.sh` registrado
- [ ] Interceptor acessível via Service interno

---

#### A3. Integração Pulumi + ArgoCD

**Tipo:** T1
**Arquivo:** Adicionar deploy do KEDA no bootstrap do workload cluster

**Sequência de bootstrap (workload):**
1. (existente) Traefik
2. (existente) cert-manager
3. (existente) NetworkPolicies
4. (existente) Admission Policies (VAPs)
5. **(novo) KEDA operator**
6. **(novo) KEDA HTTP Add-on**

**Critérios de aceite:**
- [ ] `pulumi up` deploya KEDA automaticamente no workload cluster
- [ ] KEDA operator + HTTP Add-on rodam após bootstrap
- [ ] ArgoCD managed (sync automático)

---

### Fase B: Manifest Generation (4 tasks)

#### B1. Tipos: ScaledObject + HTTPScaledObject

**Tipo:** T1
**Arquivo:** `api/internal/manifests/renderer_types.go`

Adicionar structs para os CRDs do KEDA:

```go
// ScaledObject — KEDA autoscaler (replaces HPA when min = 0)
type kedaScaledObject struct {
    APIVersion string               `json:"apiVersion"` // keda.sh/v1alpha1
    Kind       string               `json:"kind"`       // ScaledObject
    Metadata   k8sMetadata          `json:"metadata"`
    Spec       kedaScaledObjectSpec  `json:"spec"`
}

type kedaScaledObjectSpec struct {
    ScaleTargetRef    kedaScaleTargetRef `json:"scaleTargetRef"`
    MinReplicaCount   int                `json:"minReplicaCount"`            // 0 for scale-to-zero
    MaxReplicaCount   int                `json:"maxReplicaCount"`
    CooldownPeriod    int                `json:"cooldownPeriod,omitempty"`   // seconds before scale-to-zero (default: 300)
    PollingInterval   int                `json:"pollingInterval,omitempty"`  // seconds (default: 30)
    Triggers          []kedaTrigger      `json:"triggers"`
}

type kedaScaleTargetRef struct {
    Name string `json:"name"`
    Kind string `json:"kind,omitempty"` // default: Deployment
}

type kedaTrigger struct {
    Type     string            `json:"type"`     // "cpu", "prometheus", etc.
    Metadata map[string]string `json:"metadata"`
}

// HTTPScaledObject — KEDA HTTP Add-on (interceptor for cold start)
type kedaHTTPScaledObject struct {
    APIVersion string                    `json:"apiVersion"` // http.keda.sh/v1alpha1
    Kind       string                    `json:"kind"`       // HTTPScaledObject
    Metadata   k8sMetadata               `json:"metadata"`
    Spec       kedaHTTPScaledObjectSpec   `json:"spec"`
}

type kedaHTTPScaledObjectSpec struct {
    Hosts            []string                   `json:"hosts"`
    TargetPendingRequests int                   `json:"targetPendingRequests,omitempty"` // default: 100
    ScaleTargetRef   kedaHTTPScaleTargetRef     `json:"scaleTargetRef"`
    Replicas         *kedaHTTPReplicas          `json:"replicas,omitempty"`
    ScaledownPeriod  int                        `json:"scaledownPeriod,omitempty"` // seconds
}

type kedaHTTPScaleTargetRef struct {
    Name    string `json:"name"`
    Service string `json:"service"`
    Port    int    `json:"port"`
}

type kedaHTTPReplicas struct {
    Min int `json:"min"`
    Max int `json:"max"`
}
```

**Critérios de aceite:**
- [ ] Structs definidas
- [ ] `marshalResource()` serializa corretamente (verificar com teste)
- [ ] YAML gerado passa `kubeconform --strict` com CRD schemas do KEDA

---

#### B2. Builder: `buildScaledObject` + `buildHTTPScaledObject`

**Tipo:** T2 (nova lógica de manifest generation)
**Arquivo:** `api/internal/manifests/renderer_deployment.go`

Adicionar duas funções abaixo de `buildHPA()`:

```go
// buildScaledObject creates a KEDA ScaledObject for scale-to-zero apps.
// Only called when app.Autoscaling.MinReplicas == 0 and KEDA is available.
func buildScaledObject(ri RenderInput, app AppManifestInput) kedaScaledObject {
    as := app.Autoscaling
    maxR := as.MaxReplicas
    if maxR < 1 {
        maxR = 3
    }
    targetCPU := as.TargetCPU
    if targetCPU < 1 {
        targetCPU = 80
    }
    cooldown := as.CooldownPeriod
    if cooldown < 1 {
        cooldown = 300 // 5 minutes default
    }

    return kedaScaledObject{
        APIVersion: "keda.sh/v1alpha1",
        Kind:       "ScaledObject",
        Metadata: k8sMetadata{
            Name:      app.Name + "-scaledobject",
            Namespace: namespace(ri.TenantID, ri.Environment),
            Labels:    commonLabels(ri, app),
        },
        Spec: kedaScaledObjectSpec{
            ScaleTargetRef: kedaScaleTargetRef{Name: app.Name},
            MinReplicaCount: 0,
            MaxReplicaCount: maxR,
            CooldownPeriod:  cooldown,
            PollingInterval:  30,
            Triggers: []kedaTrigger{
                {
                    Type: "cpu",
                    Metadata: map[string]string{
                        "type":  "Utilization",
                        "value": fmt.Sprintf("%d", targetCPU),
                    },
                },
            },
        },
    }
}

// buildHTTPScaledObject creates a KEDA HTTP Add-on interceptor for cold start.
// Only called for server/frontend apps with scale-to-zero (workers don't receive HTTP traffic).
func buildHTTPScaledObject(ri RenderInput, app AppManifestInput) kedaHTTPScaledObject {
    hostname := appHostname(ri.TenantID, ri.ProjectID, app.Name, ri.Environment, ri.BaseDomain)
    maxR := app.Autoscaling.MaxReplicas
    if maxR < 1 {
        maxR = 3
    }
    port := app.Port
    if port == 0 {
        port = 3000
    }

    return kedaHTTPScaledObject{
        APIVersion: "http.keda.sh/v1alpha1",
        Kind:       "HTTPScaledObject",
        Metadata: k8sMetadata{
            Name:      app.Name + "-httpso",
            Namespace: namespace(ri.TenantID, ri.Environment),
            Labels:    commonLabels(ri, app),
        },
        Spec: kedaHTTPScaledObjectSpec{
            Hosts:                []string{hostname},
            TargetPendingRequests: 100,
            ScaleTargetRef: kedaHTTPScaleTargetRef{
                Name:    app.Name,
                Service: app.Name,
                Port:    port,
            },
            Replicas: &kedaHTTPReplicas{Min: 0, Max: maxR},
            ScaledownPeriod: 300,
        },
    }
}
```

**Critérios de aceite:**
- [ ] `buildScaledObject()` gera YAML válido com `minReplicaCount: 0`
- [ ] `buildHTTPScaledObject()` usa o hostname correto da app
- [ ] Custom domains incluídos nos `hosts` do HTTPScaledObject
- [ ] Workers: ScaledObject SIM, HTTPScaledObject NÃO (workers não recebem HTTP)

---

#### B3. Render path: decisão HPA vs ScaledObject

**Tipo:** T2 (muda lógica de render existente)
**Arquivos:** `renderer_helpers.go`, `renderer_deployment.go`

**Mudança em `renderer_helpers.go`:**

```go
// maybeAutoscaler appends HPA or ScaledObject depending on min replicas.
func maybeAutoscaler(docs []string, ri RenderInput, app AppManifestInput) []string {
    if app.Autoscaling == nil {
        return docs
    }
    if app.Autoscaling.MinReplicas == 0 {
        // KEDA ScaledObject (scale-to-zero)
        docs = append(docs, marshalResource(buildScaledObject(ri, app)))
        // HTTP interceptor only for apps that receive traffic (server/frontend)
        if app.Type == "server" || app.Type == "frontend" {
            docs = append(docs, marshalResource(buildHTTPScaledObject(ri, app)))
        }
        return docs
    }
    // HPA nativo (min >= 1) — comportamento atual, zero mudança
    docs = append(docs, marshalResource(buildHPA(ri, app)))
    return docs
}
```

Renomear `maybeHPA` → `maybeAutoscaler` em todos os call sites:
- `renderServerApp()` (linha 86)
- `renderWorkerApp()` (linha 95)
- `renderFrontendApp()` (linha 118)

**Mudança em `renderer_deployment.go:99-107`:**

```go
// Remover o guard `if minR < 1 { minR = 1 }` quando Autoscaling.MinReplicas == 0.
// Permitir replicas = 0 no Deployment (KEDA gerencia).
if app.Autoscaling != nil {
    minR := app.Autoscaling.MinReplicas
    // KEDA scale-to-zero: allow 0 replicas in Deployment
    // HPA mode: enforce min >= 1
    if minR < 0 {
        minR = 0
    }
    if minR < 1 && app.Autoscaling.MinReplicas != 0 {
        minR = 1
    }
    replicas = minR
}
```

**Critérios de aceite:**
- [ ] `Autoscaling.Min = 0` → Deployment com `replicas: 0` + ScaledObject + HTTPScaledObject
- [ ] `Autoscaling.Min = 2` → Deployment com `replicas: 2` + HPA (sem mudança)
- [ ] `Autoscaling = nil` → Deployment com replicas fixas (sem mudança)
- [ ] Worker com `min: 0` → ScaledObject SIM, HTTPScaledObject NÃO
- [ ] Contagem de documentos YAML correta para cada caso

---

#### B4. AutoscalingConfig: campo CooldownPeriod

**Tipo:** T1
**Arquivo:** `api/internal/manifests/renderer.go:42-46`

Adicionar campo `CooldownPeriod` ao struct:

```go
type AutoscalingConfig struct {
    MinReplicas    int // Minimum replicas (default: 1, 0 = scale-to-zero via KEDA)
    MaxReplicas    int // Maximum replicas (default: 3)
    TargetCPU      int // Target CPU utilization percentage (default: 80)
    CooldownPeriod int // Seconds before scale-to-zero (default: 300, only when min = 0)
}
```

**Critérios de aceite:**
- [ ] Campo existe no struct
- [ ] Zero value (0) usa default de 300s no builder

---

### Fase C: CLI + loopzero.yaml (4 tasks)

#### C1. Validação: permitir `min: 0`

**Tipo:** T2 (muda contrato de validação)
**Arquivo:** `cli/src/adapters/validate-config.ts:201`

**Mudança:**

```typescript
// ANTES:
if (min !== undefined && (typeof min !== 'number' || !Number.isInteger(min) || min < 1)) {

// DEPOIS:
if (min !== undefined && (typeof min !== 'number' || !Number.isInteger(min) || min < 0)) {
```

Adicionar validação contextual:
```typescript
// min: 0 só permitido para server e frontend (não worker)
if (min === 0 && appType === 'worker') {
    errors.push({
        field: `apps.${name}.autoscaling.min`,
        message: `App '${name}': autoscaling.min = 0 (scale-to-zero) is not supported for workers. Workers don't receive HTTP traffic for wake-up. Use min >= 1.`,
    });
}
```

**Espera.** Workers PODEM ter scale-to-zero no KEDA — via trigger de queue ou Prometheus metric. Mas o LoopZero não gera `HTTPScaledObject` para workers (task B3). O ScaledObject com trigger CPU funciona: quando não há CPU load, KEDA escala para 0. Para escalar de volta, precisa de um trigger externo (não HTTP).

**Decisão:** Permitir `min: 0` para workers se houver trigger explícito. Na v1, rejeitar `min: 0` para workers (YAGNI — sem trigger de queue configurável ainda).

**Critérios de aceite:**
- [ ] `min: 0` para server → aceito
- [ ] `min: 0` para frontend → aceito
- [ ] `min: 0` para worker → rejeitado com mensagem clara
- [ ] `min: -1` → rejeitado
- [ ] `min: 1` → aceito (sem mudança)
- [ ] Testes unitários existentes continuam passando

---

#### C2. Parser: campo `cooldown`

**Tipo:** T1
**Arquivo:** `cli/src/adapters/loopzero-yaml.ts`

Adicionar `cooldown` ao `LoopZeroYamlAutoscaling`:

```typescript
export interface LoopZeroYamlAutoscaling {
    min?: number;
    max?: number;
    targetCPU?: number;
    cooldown?: number;  // seconds before scale-to-zero (default: 300)
}
```

Parsear no bloco existente (linha 117-131):

```typescript
const cooldown = coerce(asCfg.cooldown);
autoscaling = {
    ...(min !== undefined ? { min } : {}),
    ...(max !== undefined ? { max } : {}),
    ...(targetCPU !== undefined ? { targetCPU } : {}),
    ...(cooldown !== undefined ? { cooldown } : {}),
};
```

Validação em `validate-config.ts`:

```typescript
if (as.cooldown !== undefined) {
    if (typeof as.cooldown !== 'number' || !Number.isInteger(as.cooldown) || as.cooldown < 30 || as.cooldown > 3600) {
        errors.push({
            field: `apps.${name}.autoscaling.cooldown`,
            message: `App '${name}': autoscaling.cooldown must be between 30 and 3600 seconds (got ${JSON.stringify(as.cooldown)}).`,
        });
    }
}
```

Adicionar `cooldown` ao `knownFields` set em `validate-config.ts` (dentro do autoscaling block, não no top level).

**Critérios de aceite:**
- [ ] `cooldown: 600` parseado corretamente
- [ ] `cooldown: 10` rejeitado (< 30)
- [ ] `cooldown: 5000` rejeitado (> 3600)
- [ ] `cooldown` ausente → sem erro (default no server-side)

---

#### C3. `loopzero scale`: suporte a `--min 0`

**Tipo:** T1
**Arquivo:** `cli/src/commands/scale.ts:108`

**Mudança:**

```typescript
// ANTES:
if (opts.min !== undefined && opts.min < 0) {

// DEPOIS (já correto — aceita 0):
if (opts.min !== undefined && opts.min < 0) {
```

O código já aceita `min: 0` no CLI (`opts.min < 0` rejeita negativos, não zero). Nenhuma mudança necessária na CLI scale command.

Mas adicionar output visual para scale-to-zero:

```typescript
if (response.autoscaling && response.autoscaling.min === 0) {
    this.deps.writeTo(`  Scale-to-zero: enabled (cooldown: ${response.autoscaling.cooldown ?? 300}s)`);
    this.deps.writeTo('  Note: First request after idle will take ~5-10s (cold start).');
}
```

**Critérios de aceite:**
- [ ] `loopzero scale --app api --min 0 --max 5` → aceito
- [ ] Output mostra "Scale-to-zero: enabled" com nota de cold start
- [ ] `--json` inclui informação de scale-to-zero

---

#### C4. `loopzero status`: mostrar estado de scaling

**Tipo:** T2 (muda contrato de status output)
**Arquivo:** `cli/src/commands/status.ts`

**Mudança na `StatusApiResponse`:**

```typescript
interface StatusApiResponse {
    // ... campos existentes ...
    scaling?: {
        replicas: number;          // replicas atuais (0 quando scaled-to-zero)
        min: number;
        max: number;
        scaled_to_zero: boolean;   // true quando min=0 e replicas=0
    };
}
```

**Mudança no display:**

```typescript
if (status.scaling) {
    if (status.scaling.scaled_to_zero) {
        this.deps.writeTo(`Scaling:      sleeping (0/${status.scaling.max} replicas)`);
        this.deps.writeTo(`              First request wakes up in ~5-10s`);
    } else {
        this.deps.writeTo(`Scaling:      ${status.scaling.replicas}/${status.scaling.max} replicas`);
    }
}
```

**Output exemplo:**

```
$ loopzero status
  Environment: production
  Last deploy:  bld_abc123 — SUCCEEDED
  Release:      rel_def456 (active since 14:30)
  Scaling:      sleeping (0/5 replicas)
                First request wakes up in ~5-10s
  URL:          https://acme-myapp-api-production.dev.loopzero.example
```

**Critérios de aceite:**
- [ ] `loopzero status` mostra "sleeping" quando replicas = 0 e scaled_to_zero = true
- [ ] `loopzero status` mostra contagem de replicas quando rodando normalmente
- [ ] `--json` inclui campo `scaling` com todos os campos
- [ ] Sem termos internos ("KEDA", "ScaledObject", "HTTPScaledObject" NUNCA aparecem)

---

### Fase D: API Backend (3 tasks)

#### D1. Endpoint `POST /api/v1/projects/:name/scale`

**Tipo:** T2 (novo endpoint mutante)
**Arquivo:** `api/internal/routes/scale.go` (novo)

```go
// POST /api/v1/projects/:name/scale
// Body: { "environment": "production", "app": "api", "min": 0, "max": 5, "cpu": 80 }
// ou: { "environment": "production", "app": "api", "replicas": 3 }
//
// Persiste config de scaling no DB (environment_targets ou nova tabela).
// NÃO aplica imediatamente — toma efeito no próximo deploy.
// Responde 200 com config salva.
//
// Regras:
// - withEnvironmentLock (regra §6)
// - Idempotency-Key (regra §4)
// - CheckValue("max_replicas_per_service", max) — plan enforcement
// - min: 0 só se KEDA habilitado no cluster (feature gate)
```

**Critérios de aceite:**
- [ ] `POST` com `min: 0, max: 5` → 200 com config salva
- [ ] `POST` com `replicas: 3` → 200 com config salva
- [ ] `POST` com `replicas + min` → 400 (mutual exclusion)
- [ ] `POST` com `max: 20` em Starter tier → 403 (plan cap = 2)
- [ ] `POST` com `min: 0` quando KEDA não habilitado → 400 com mensagem
- [ ] `withEnvironmentLock` aplicado
- [ ] `Idempotency-Key` header respeitado

---

#### D2. Status API: campo `scaling`

**Tipo:** T2 (muda contrato de response)
**Arquivo:** `api/internal/routes/status.go`

Adicionar campo `scaling` ao response do `GET /api/v1/projects/:name/status`:

```go
type statusResponse struct {
    // ... campos existentes ...
    Scaling *scalingStatus `json:"scaling,omitempty"`
}

type scalingStatus struct {
    Replicas     int  `json:"replicas"`        // replicas atuais (do Deployment)
    Min          int  `json:"min"`
    Max          int  `json:"max"`
    ScaledToZero bool `json:"scaled_to_zero"`  // replicas == 0 && min == 0
}
```

Para obter replicas atuais: query ao workload cluster via Kubernetes API (ou cache):
```go
deployment, err := workloadClient.AppsV1().Deployments(ns).Get(ctx, appName, metav1.GetOptions{})
currentReplicas := int(*deployment.Spec.Replicas)
```

**Critérios de aceite:**
- [ ] Status inclui `scaling` quando autoscaling configurado
- [ ] `scaled_to_zero: true` quando replicas = 0 e min = 0
- [ ] `scaled_to_zero: false` quando replicas = 0 e min > 0 (crash, não scale-to-zero)

---

#### D3. Feature gate: KEDA enabled

**Tipo:** T1
**Arquivo:** `api/internal/config/`

```go
// Config field:
KEDAEnabled bool `env:"LOOPZERO_KEDA_ENABLED" default:"false"`
```

Quando `LOOPZERO_KEDA_ENABLED=false`:
- `min: 0` rejeitado no `/scale` endpoint (400)
- Manifest renderer nunca gera ScaledObject (guard clause)
- Backward compatible: zero impacto em clusters sem KEDA

Quando `LOOPZERO_KEDA_ENABLED=true`:
- `min: 0` permitido
- Manifest renderer gera ScaledObject/HTTPScaledObject

**Critérios de aceite:**
- [ ] Feature gate OFF → `min: 0` rejeitado com mensagem "Scale-to-zero requires KEDA. Contact platform admin."
- [ ] Feature gate ON → `min: 0` aceito

---

### Fase E: Plan Enforcement (2 tasks)

#### E1. Novo metric: `min_replicas_per_service`

**Tipo:** T2 (muda tabela plan_limits)
**Arquivo:** Migration + `pkg/enforcement/limiter.go`

**Migration:**

```sql
-- Permitir min: 0 para todos os plans (scale-to-zero é feature, não restriction)
INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit) VALUES
    ('starter', 'min_replicas_per_service', 0, 0),
    ('pro', 'min_replicas_per_service', 0, 0),
    ('team', 'min_replicas_per_service', 0, 0);
-- Enterprise: NULL (unlimited) — já handled pelo nil limiter
```

**Espera.** `min_replicas` não é um limit a ser enforced — é um valor permitido. O enforcement real é `max_replicas_per_service` (que já existe). A questão é: Starter tier pode ter `min: 0`? Sim — é o principal benefício (custo zero).

**Decisão:** NÃO criar novo metric. `min: 0` é permitido para todos os plans. O enforcement existente (`max_replicas_per_service`) já cobre o max.

**Risco:** E se quisermos cobrar por scale-to-zero como feature premium? → Usar feature gate (`LOOPZERO_KEDA_ENABLED` por cluster, não por plan). Quando/se virar premium, adicionar metric.

**Critérios de aceite:**
- [ ] Starter com `min: 0, max: 2` → permitido
- [ ] Starter com `min: 0, max: 5` → rejeitado (max cap = 2)
- [ ] Pro com `min: 0, max: 5` → permitido

---

#### E2. Validação server-side: `min: 0` + KEDA gate

**Tipo:** T1
**Arquivo:** `api/internal/services/build_orchestrator.go` (ou scale handler)

```go
if autoscaling.MinReplicas == 0 && !cfg.KEDAEnabled {
    return LoopZeroError{
        Code:    "SCALE_TO_ZERO_UNAVAILABLE",
        Message: "Scale-to-zero is not available on this cluster.",
        Hint:    "Use min >= 1 for autoscaling, or contact support to enable scale-to-zero.",
    }
}
```

**Critérios de aceite:**
- [ ] KEDA OFF + `min: 0` → `SCALE_TO_ZERO_UNAVAILABLE` error
- [ ] KEDA ON + `min: 0` → aceito
- [ ] Error tem hint actionable

---

### Fase F: Traefik Integration (1 task)

#### F1. Traefik → KEDA HTTP interceptor routing

**Tipo:** T2 (muda fluxo de routing)

Quando app tem scale-to-zero, o Traefik IngressRoute precisa apontar para o **KEDA HTTP interceptor Service** em vez do Service da app diretamente.

```
Sem KEDA:    Traefik → app Service → app Pod
Com KEDA:    Traefik → KEDA interceptor → app Service → app Pod (quando acordado)
```

O KEDA HTTP Add-on cria automaticamente um `Service` interceptor no namespace da app. O `HTTPScaledObject` configura o routing.

**Alternativa:** O KEDA HTTP Add-on pode operar como Traefik middleware. Avaliar na implementação se é mais limpo que mudar o IngressRoute target.

**Mudança no manifest renderer:**

```go
// Em buildIngressResources(), quando scale-to-zero:
if app.Autoscaling != nil && app.Autoscaling.MinReplicas == 0 {
    // Route to KEDA HTTP Add-on interceptor instead of app service
    // The interceptor handles wake-up and proxying
    service.Name = fmt.Sprintf("keda-add-ons-http-interceptor-proxy")
    service.Namespace = "keda"  // interceptor namespace
    // Port stays the same — interceptor proxies to the real service
}
```

**Nota:** A implementação exata depende de como o KEDA HTTP Add-on é configurado. O `HTTPScaledObject` pode criar automaticamente os routes necessários. Validar em k3d antes de definir a mudança final.

**Critérios de aceite:**
- [ ] App com 0 replicas → request via Traefik → interceptor → pod sobe → response
- [ ] App com 1+ replicas → request via Traefik → direto ao pod (sem interceptor overhead)
- [ ] Cold start < 15s para app Node.js simples
- [ ] Health check probes não triggam wake-up falso

---

### Fase G: Testes (3 tasks)

#### G1. Testes unitários: manifest generation

**Tipo:** T1
**Arquivo:** `api/internal/manifests/renderer_scaling_test.go`

```go
// Novos testes (adicionar aos 9 existentes):

func TestRenderDeployment_ScaleToZero_GeneratesScaledObject(t *testing.T)
// min: 0 → ScaledObject gerado, HPA NÃO gerado

func TestRenderDeployment_ScaleToZero_GeneratesHTTPScaledObject(t *testing.T)
// server + min: 0 → HTTPScaledObject gerado com hostname correto

func TestRenderDeployment_ScaleToZero_WorkerNoHTTPScaledObject(t *testing.T)
// worker + min: 0 → ScaledObject SIM, HTTPScaledObject NÃO

func TestRenderDeployment_ScaleToZero_DeploymentReplicasZero(t *testing.T)
// min: 0 → Deployment.spec.replicas = 0

func TestRenderDeployment_ScaleToZero_DocCount(t *testing.T)
// server + min: 0 → Deployment + PDB + Service + Cert + IngressRoute + ScaledObject + HTTPScaledObject = 7

func TestRenderDeployment_MinOne_StillGeneratesHPA(t *testing.T)
// min: 1 → HPA gerado (backward compatible, zero mudança)

func TestRenderDeployment_ScaleToZero_CooldownDefault(t *testing.T)
// cooldown: 0 → default 300 no ScaledObject

func TestRenderDeployment_ScaleToZero_CooldownCustom(t *testing.T)
// cooldown: 600 → 600 no ScaledObject

func TestRenderDeployment_ScaleToZero_CustomDomains(t *testing.T)
// HTTPScaledObject.hosts inclui custom domains
```

**Critérios de aceite:**
- [ ] 9 testes novos passando
- [ ] 9 testes existentes passando (zero regressão)

---

#### G2. Testes unitários: CLI validation

**Tipo:** T1
**Arquivo:** `cli/tests/unit/`

```typescript
// validate-config:
// - min: 0 para server → aceito
// - min: 0 para worker → rejeitado
// - cooldown: 30 → aceito
// - cooldown: 10 → rejeitado

// loopzero-yaml parser:
// - autoscaling com cooldown parseado
// - autoscaling sem cooldown → undefined

// scale command:
// - --min 0 --max 5 → aceito
// - output mostra "Scale-to-zero: enabled"
// - --json inclui scale-to-zero info
```

**Critérios de aceite:**
- [ ] Testes passando
- [ ] Testes existentes sem regressão

---

#### G3. Teste de integração: cold start end-to-end

**Tipo:** T2
**Arquivo:** `infra/tests/e2e/` (ou teste manual documentado)

```
1. Deploy app com min: 0
2. Verificar: 0 pods rodando
3. Fazer request HTTP para a URL da app
4. Verificar: pod sobe em < 15s
5. Verificar: response 200
6. Esperar cooldown (300s ou menor para teste)
7. Verificar: pod escala para 0
8. Repetir request → pod sobe novamente
```

**Critérios de aceite:**
- [ ] Cold start < 15s para app Node.js
- [ ] Scale-to-zero após cooldown
- [ ] Re-wake funciona consistentemente

---

## Dependências entre tasks

```
Fase A (infra) ────────────────────────────────────────
  A1 (KEDA chart)
  A2 (HTTP Add-on chart)    depende de A1
  A3 (Pulumi)               depende de A1, A2

Fase B (manifests) ────────────────────────────────────
  B1 (types)
  B4 (CooldownPeriod)
  B2 (builders)             depende de B1, B4
  B3 (render path)          depende de B2

Fase C (CLI) ──────────────────────────────────────────
  C1 (validation min:0)
  C2 (parser cooldown)
  C3 (scale output)         depende de C1
  C4 (status scaling)

Fase D (API) ──────────────────────────────────────────
  D3 (feature gate)
  D1 (scale endpoint)       depende de D3
  D2 (status scaling)       depende de D3

Fase E (enforcement) ──────────────────────────────────
  E1 (decisão: no new metric)
  E2 (server-side gate)     depende de D3

Fase F (routing) ──────────────────────────────────────
  F1 (Traefik→interceptor)  depende de A2, B3

Fase G (testes) ────────────────────────────────────────
  G1 (manifest tests)       depende de B3
  G2 (CLI tests)            depende de C1, C2
  G3 (e2e cold start)       depende de tudo
```

**Ordem recomendada:**

```
1. A1 → A2 → A3           (infra — pode começar independente)
2. B1 → B4 → B2 → B3      (manifests — core da feature)
3. C1 → C2                 (CLI parsing — paralelo com B)
4. D3 → D1 → D2            (API — depende de decisões de B)
5. E2                       (enforcement — rápido)
6. C3 → C4                 (CLI output — depende de D2)
7. F1                       (routing — depende de A2 + B3)
8. G1 → G2 → G3            (testes — acompanha cada fase)
```

---

## Riscos e Mitigações

| Risco | Probabilidade | Impacto | Mitigação |
|-------|:---:|:---:|---------|
| KEDA HTTP Add-on + Traefik IngressRoute conflito de routing | Média | Alto | Testar em k3d antes. Fallback: usar KEDA sem HTTP Add-on (scale-to-zero funciona, mas sem cold start automático — user vê 503 até pod subir) |
| Cold start > 15s para apps pesadas (Java Spring) | Alta | Médio | Documentar cold start times por framework. Sugerir `min: 1` para apps latency-sensitive |
| KEDA gera HPA internamente → conflito com HPA existente | Baixa | Alto | Guard clause: NUNCA gerar HPA + ScaledObject para mesmo Deployment. `maybeAutoscaler()` é exclusivo |
| Scale-to-zero causa alarme falso no monitoring | Média | Baixo | Dashboards filtram apps com `min: 0`. Alert rules excluem namespaces com ScaledObject |
| KEDA CRDs pesados (muitos ScaledObjects) | Baixa | Baixo | 1 ScaledObject + 1 HTTPScaledObject por app. 100 tenants × 2 apps = 400 CRDs — ok para KEDA |
| Feature gate `LOOPZERO_KEDA_ENABLED` esquecido em prod | Baixa | Médio | Config.Validate() log warning se KEDA CRDs existem mas gate está OFF |

---

## loopzero.yaml final (exemplo)

```yaml
# Sem autoscaling (comportamento atual, zero mudança)
project: my-app
apps:
  api:
    path: ./api
    framework: go
    port: 8080

# Com HPA nativo (comportamento atual, zero mudança)
project: my-app
apps:
  api:
    path: ./api
    framework: go
    port: 8080
    autoscaling:
      min: 2
      max: 10
      targetCPU: 75

# Com scale-to-zero (NOVO — requer KEDA)
project: my-app
apps:
  api:
    path: ./api
    framework: go
    port: 8080
    autoscaling:
      min: 0          # scale-to-zero
      max: 5
      targetCPU: 80
      cooldown: 300   # 5 min antes de dormir (default)
```

---

## Changelog entry (quando implementado)

```markdown
### Added
- Scale-to-zero para apps de tenant via KEDA (min: 0 no loopzero.yaml)
- Cold start automático via KEDA HTTP interceptor (~5-10s)
- Campo `cooldown` no loopzero.yaml para configurar tempo antes de scale-to-zero
- Endpoint POST /api/v1/projects/:name/scale
- Campo `scaling` no loopzero status com estado de replicas e scale-to-zero
- Feature gate LOOPZERO_KEDA_ENABLED para habilitar scale-to-zero por cluster
```

---

## Referências

| Item | Localização |
|------|-------------|
| KEDA docs | https://keda.sh/docs/ |
| KEDA HTTP Add-on | https://github.com/kedacore/http-add-on |
| KEDA ScaledObject spec | https://keda.sh/docs/latest/concepts/scaling-deployments/ |
| HPA atual | `api/internal/manifests/renderer_deployment.go:156-206` |
| maybeHPA helper | `api/internal/manifests/renderer_helpers.go:60-66` |
| AutoscalingConfig struct | `api/internal/manifests/renderer.go:40-46` |
| HPA types | `api/internal/manifests/renderer_types.go:247-280` |
| Scaling tests (9) | `api/internal/manifests/renderer_scaling_test.go` |
| CLI scale command | `cli/src/commands/scale.ts` |
| CLI scale registration | `cli/src/register/scale.ts` |
| loopzero.yaml parser | `cli/src/adapters/loopzero-yaml.ts:114-131` |
| Validation (min >= 1) | `cli/src/adapters/validate-config.ts:201` |
| Deployment replicas guard | `api/internal/manifests/renderer_deployment.go:99-107` |
| Plan enforcement | `pkg/enforcement/limiter.go:114` (CheckValue) |
| Status command | `cli/src/commands/status.ts` |
| Spec §0.3 (JSON schemas) | `docs/technical.md` |
| Spec §26 (loopzero.yaml) | `docs/technical.md` |
