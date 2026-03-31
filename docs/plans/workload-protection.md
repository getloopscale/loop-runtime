# Plano: Workload Protection — Defesa contra imagens quebradas

> **Objetivo:** Proteger o cluster contra apps com imagens quebradas/unhealthy consumindo recursos sem entregar valor.
>
> **Escopo:** 12 items em 5 camadas de defesa (Admissao, Rollout, Analise, Runtime, Recuperacao).
>
> **Classificacao:** T3 (toca manifests, operators, Helm, Pulumi, API, CLI, pipeline)
>
> **Status:** APPROVED_WITH_CONDITIONS — MEET-2026-03-30-021
>
> **Comite:** Principal Architect, SRE, Security, K8s Controller, Distributed Systems, Verification Architect

---

## Contexto

### Problema

Quando um usuario faz `loopzero deploy` com uma imagem quebrada (crash loop, OOM, deadlock), o Deployment fica consumindo recursos do cluster (CPU, memoria, IPs, pod slots do ResourceQuota) sem servir trafico. Em ambiente multi-tenant, isso degrada a experiencia de TODOS os tenants no cluster.

### O que ja existe

| Mecanismo | Status | Localizacao |
|-----------|--------|-------------|
| Readiness probe (HTTP /health) | Parcial — sem timeouts, sem thresholds, sem startup probe | `api/internal/manifests/renderer.go:534-543` |
| Liveness probe (HTTP /health) | Parcial — mesmos gaps | `api/internal/manifests/renderer.go:539-543` |
| ResourceQuota por namespace | OK | `operators/internal/controller/environment_controller.go:436-503` |
| LimitRange por namespace | OK — defaults 100m/128Mi requests, 1CPU/1Gi limits | `operators/internal/controller/environment_controller.go:476-501` |
| NetworkPolicy deny-all | OK | `operators/internal/controller/environment_controller.go:296-433` |
| Circuit breaker ArgoCD | OK — 3 failures abre, 2 successes fecha | `api/internal/services/pg_circuit_breaker.go` |
| Circuit breaker Argo Workflows | OK — mesmo pattern | `api/internal/services/workflow_circuit_breaker.go` |
| `progressDeadlineSeconds` | AUSENTE — default K8s 600s (10 min) | `api/internal/manifests/renderer.go:579-587` |
| `minReadySeconds` | AUSENTE — default 0 | `api/internal/manifests/renderer.go:579-587` |
| Startup probe | AUSENTE | `api/internal/manifests/renderer.go` |
| PDB para apps de tenant | AUSENTE | `operators/internal/controller/application_controller.go` |
| Alerta CrashLoopBackOff | AUSENTE | `infra/helm/monitoring/manifests/` |
| Kyverno (policy engine) | NAO INSTALADO | — |
| Argo Rollouts (progressive delivery) | NAO INSTALADO | — |
| Image signing (cosign) | NAO IMPLEMENTADO | Sprint 6 planejado |

### O que sera adicionado

5 camadas de defesa, 12 items, 4 fases de implementacao.

```
Fase 1: Manifests + Alerting     (nativo K8s, zero deps novas)
Fase 2: Policy Engine            (Kyverno)
Fase 3: Deploy Intelligence      (API circuit breaker + Pod Failure Policy)
Fase 4: Progressive Delivery     (Argo Rollouts + cosign + Reaper)
```

---

## Condicoes obrigatorias

### BLOCKING Fase 1

**W1 — Zero regressao nos testes existentes:**
`cd api && go test ./...` passa com todos os novos campos nos manifests.

**W2 — Manifests gerados validam com kubeconform:**
`task validate:manifests` passa. Todos os novos campos (startup probe, PDB, strategy) devem ser validos.

**W3 — Backward compatibility do loopzero.yaml:**
Projetos existentes sem campo `healthCheck` devem continuar funcionando com defaults sensatos. NENHUMA mudanca no loopzero.yaml e obrigatoria — novos campos sao opcionais.

### BLOCKING Fase 2

**W4 — Kyverno nao bloqueia workloads existentes:**
Policies devem ser `Audit` por 48h antes de mudar para `Enforce`. Nenhum deploy existente pode quebrar.

**W5 — Kyverno nao impacta performance da API:**
Webhook timeout max 5s. Failure policy `Ignore` (nao `Fail`) durante rollout inicial.

### BLOCKING Fase 3

**W6 — Deploy circuit breaker nao bloqueia rollback:**
`loopzero rollback` NUNCA deve ser bloqueado pelo circuit breaker. Se a release atual esta unhealthy, rollback e a SOLUCAO, nao o problema.

**W7 — Pod Failure Policy nao quebra retry existente:**
Argo Workflows `retryStrategy` (limit 2, OnFailure) continua funcionando. Pod Failure Policy so adiciona `FailJob` para exit codes irrecuperaveis.

### BLOCKING Fase 4

**W8 — Argo Rollouts nao conflita com ArgoCD:**
ArgoCD gerencia o Rollout resource (nao Deployment). Conversao Deployment→Rollout deve ser transparente para o usuario.

**W9 — cosign nao bloqueia builds em dev:**
Image signing obrigatorio apenas em prod. Dev/staging permitem unsigned para velocidade de iteracao.

---

## Fase 1 — Manifests robustos + Alerting (nativo K8s)

> **Dependencias:** nenhuma. Tudo nativo do Kubernetes.
> **Impacto:** `api/internal/manifests/`, `infra/helm/monitoring/`, `operators/`
> **Estimativa:** Items 1.1-1.6 podem ser implementados em paralelo.

### 1.1 — Startup probe nos manifests gerados

**Arquivo:** `api/internal/manifests/renderer.go`

**Situacao atual:** Sem startup probe. Apps com cold start lento (JVM, Python ML) recebem liveness kill antes de inicializar.

**Mudanca:**

Adicionar campo `StartupProbe` na struct `k8sContainer`:
```go
type k8sContainer struct {
    // ... campos existentes ...
    StartupProbe   *k8sProbe `json:"startupProbe,omitempty"`
}
```

Adicionar campo na struct `k8sProbe` para thresholds:
```go
type k8sProbe struct {
    HTTPGet             k8sHTTPGet `json:"httpGet"`
    InitialDelaySeconds int        `json:"initialDelaySeconds"`
    PeriodSeconds       int        `json:"periodSeconds"`
    TimeoutSeconds      int        `json:"timeoutSeconds,omitempty"`
    SuccessThreshold    int        `json:"successThreshold,omitempty"`
    FailureThreshold    int        `json:"failureThreshold,omitempty"`
}
```

Configurar na funcao `buildDeployment()` (para apps com `port > 0`):
```go
container.StartupProbe = &k8sProbe{
    HTTPGet:          k8sHTTPGet{Path: "/health", Port: port},
    PeriodSeconds:    5,
    TimeoutSeconds:   3,
    FailureThreshold: 30, // 30 * 5s = 150s max startup
}
```

Melhorar probes existentes:
```go
container.ReadinessProbe = &k8sProbe{
    HTTPGet:             k8sHTTPGet{Path: "/health", Port: port},
    InitialDelaySeconds: 0,  // startup probe protege
    PeriodSeconds:       10,
    TimeoutSeconds:      3,
    SuccessThreshold:    1,
    FailureThreshold:    3,  // 3 * 10s = 30s antes de remover do Service
}
container.LivenessProbe = &k8sProbe{
    HTTPGet:             k8sHTTPGet{Path: "/health", Port: port},
    InitialDelaySeconds: 0,  // startup probe protege
    PeriodSeconds:       15,
    TimeoutSeconds:      3,
    SuccessThreshold:    1,
    FailureThreshold:    5,  // 5 * 15s = 75s — mais tolerante que readiness
}
```

**Rationale:** Startup probe com `failureThreshold * periodSeconds = 150s` cobre JVM, Python ML models, e frameworks pesados. Enquanto startup probe roda, liveness e readiness ficam desabilitadas (K8s nativo). Apos startup, liveness e readiness assumem com thresholds separados: readiness mais agressivo (remove do Service rapido), liveness mais tolerante (evita restart desnecessario).

**Testes:**
- `TestBuildDeployment_ServerApp_HasStartupProbe` — server apps com porta recebem startup probe
- `TestBuildDeployment_WorkerApp_NoProbes` — workers sem porta continuam sem probes
- `TestBuildDeployment_ProbeThresholds` — valores de threshold corretos
- `TestBuildDeployment_ProbeTimeout` — timeoutSeconds presente nos 3 probes

**Validacao:** `task validate:manifests` passa com os novos campos.

### 1.2 — progressDeadlineSeconds nos Deployments

**Arquivo:** `api/internal/manifests/renderer.go`

**Situacao atual:** Struct `k8sDeploymentSpec` nao tem campo `ProgressDeadlineSeconds`. Default K8s = 600s (10 min). Deploy quebrado fica 10 minutos consumindo recursos antes de ser marcado Failed.

**Mudanca:**

Adicionar campo na struct:
```go
type k8sDeploymentSpec struct {
    Replicas                int                 `json:"replicas"`
    ProgressDeadlineSeconds int                 `json:"progressDeadlineSeconds,omitempty"`
    MinReadySeconds         int                 `json:"minReadySeconds,omitempty"`
    Strategy                *k8sDeployStrategy  `json:"strategy,omitempty"`
    Selector                k8sLabelSelector    `json:"selector"`
    Template                k8sPodTemplateSpec  `json:"template"`
}
```

Configurar em `buildDeployment()`:
```go
Spec: k8sDeploymentSpec{
    Replicas:                1,
    ProgressDeadlineSeconds: 180,  // 3 min — fail fast
    MinReadySeconds:         10,   // pod deve ficar Ready 10s antes de Available
    // ...
}
```

**Rationale:** 180s e suficiente para startup (ate 150s via startup probe) + margem. Se o Deployment nao faz progresso em 3 minutos, esta quebrado. O default de 600s e inaceitavel para PaaS — usuario espera feedback rapido.

**Testes:**
- `TestBuildDeployment_HasProgressDeadline` — valor 180 presente
- `TestBuildDeployment_HasMinReadySeconds` — valor 10 presente

### 1.3 — Rolling update strategy nos Deployments

**Arquivo:** `api/internal/manifests/renderer.go`

**Situacao atual:** Sem strategy definida. K8s default = RollingUpdate com 25% maxUnavailable / 25% maxSurge. Com 1 replica, isso pode causar downtime (mata a unica replica antes da nova estar ready).

**Mudanca:**

Nova struct:
```go
type k8sDeployStrategy struct {
    Type          string              `json:"type"`
    RollingUpdate *k8sRollingUpdate   `json:"rollingUpdate,omitempty"`
}

type k8sRollingUpdate struct {
    MaxUnavailable string `json:"maxUnavailable"`
    MaxSurge       string `json:"maxSurge"`
}
```

Configurar em `buildDeployment()`:
```go
Strategy: &k8sDeployStrategy{
    Type: "RollingUpdate",
    RollingUpdate: &k8sRollingUpdate{
        MaxUnavailable: "0",  // NUNCA mata pod healthy antes do novo estar ready
        MaxSurge:       "1",  // cria 1 pod novo, valida, depois mata o antigo
    },
},
```

**Rationale:** `maxUnavailable: 0` + `maxSurge: 1` garante zero-downtime para apps com 1 replica. O pod antigo so morre depois que o novo passa readiness probe. Isso e CRITICO para PaaS — deploy nao pode causar downtime.

**Testes:**
- `TestBuildDeployment_ZeroDowntimeStrategy` — maxUnavailable=0, maxSurge=1

### 1.4 — PodDisruptionBudget para apps de tenant

**Arquivo:** `api/internal/manifests/renderer.go`

**Situacao atual:** Apps de tenant nao tem PDB. Node drain pode matar todos os pods de um tenant simultaneamente.

**Mudanca:**

Nova struct:
```go
type k8sPDB struct {
    APIVersion string      `json:"apiVersion"`
    Kind       string      `json:"kind"`
    Metadata   k8sMetadata `json:"metadata"`
    Spec       k8sPDBSpec  `json:"spec"`
}

type k8sPDBSpec struct {
    MinAvailable              int               `json:"minAvailable,omitempty"`
    MaxUnavailable            int               `json:"maxUnavailable,omitempty"`
    UnhealthyPodEvictionPolicy string           `json:"unhealthyPodEvictionPolicy,omitempty"`
    Selector                   k8sLabelSelector `json:"selector"`
}
```

Gerar PDB para apps server (com porta):
```go
func buildPDB(ri RenderInput, app AppManifestInput) k8sPDB {
    return k8sPDB{
        APIVersion: "policy/v1",
        Kind:       "PodDisruptionBudget",
        Metadata: k8sMetadata{
            Name:      app.Name + "-pdb",
            Namespace: ns,
            Labels:    labels,
        },
        Spec: k8sPDBSpec{
            MaxUnavailable:             1,
            UnhealthyPodEvictionPolicy: "AlwaysAllow",
            Selector: k8sLabelSelector{MatchLabels: selectorLabels},
        },
    }
}
```

**Rationale:** `maxUnavailable: 1` com 1 replica permite node drain (senao o drain fica stuck). `unhealthyPodEvictionPolicy: AlwaysAllow` (stable desde K8s 1.26) permite evictar pods em CrashLoopBackOff sem violar PDB — essencial para multi-tenant onde pod quebrado de um tenant nao pode bloquear manutencao do cluster.

**Testes:**
- `TestBuildPDB_ServerApp_Created` — server apps recebem PDB
- `TestBuildPDB_WorkerApp_NoPDB` — workers com 1 replica nao recebem PDB (ou recebem, depende da discussao)
- `TestBuildPDB_UnhealthyEvictionPolicy` — policy = AlwaysAllow

**Nota:** PDB faz parte dos manifests pushados para Gitea (incluso no `manifests.yaml`).

### 1.5 — Alertas de CrashLoopBackOff e imagens quebradas

**Arquivo:** `infra/helm/monitoring/manifests/prometheusrule-warning.yaml`

**Situacao atual:** 12 alertas warning mas NENHUM para CrashLoopBackOff, ImagePullBackOff, ou replica mismatch.

**Mudanca:**

Adicionar 4 regras ao grupo `loopzero.warning`:

```yaml
# Pod stuck em CrashLoopBackOff por mais de 10 minutos
- alert: LoopZeroAppCrashLoop
  expr: |
    max by (namespace, pod) (
      kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff"}
    ) * on(namespace) group_left()
    kube_namespace_labels{label_loopzero_io_managed_by="environment-controller"}
    > 0
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "App pod {{ $labels.pod }} in {{ $labels.namespace }} stuck in CrashLoopBackOff"
    description: "Pod has been crash-looping for 10+ minutes. Resources consumed without serving traffic."
    runbook: "Check app logs via `loopzero logs`. If image is broken, rollback via `loopzero rollback`."

# Pod nao consegue puxar imagem
- alert: LoopZeroAppImagePullFailed
  expr: |
    max by (namespace, pod) (
      kube_pod_container_status_waiting_reason{reason="ImagePullBackOff"}
      + kube_pod_container_status_waiting_reason{reason="ErrImagePull"}
    ) * on(namespace) group_left()
    kube_namespace_labels{label_loopzero_io_managed_by="environment-controller"}
    > 0
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "App pod {{ $labels.pod }} in {{ $labels.namespace }} cannot pull image"
    description: "Image pull failing for 5+ minutes. Possible causes: wrong image tag, registry auth, network."
    runbook: "Verify image exists in registry. Check registry credentials. Redeploy with correct image."

# Deployment com replicas desejadas != replicas prontas
- alert: LoopZeroAppReplicaMismatch
  expr: |
    (
      kube_deployment_spec_replicas
      - kube_deployment_status_replicas_ready
    ) * on(namespace) group_left()
    kube_namespace_labels{label_loopzero_io_managed_by="environment-controller"}
    > 0
  for: 15m
  labels:
    severity: warning
  annotations:
    summary: "Deployment {{ $labels.deployment }} in {{ $labels.namespace }} has unavailable replicas"
    description: "Desired replicas != ready replicas for 15+ minutes. App may be partially down."
    runbook: "Check pod status. Common causes: resource pressure, crash loop, image issues."

# Pod restarts excessivos (mais de 10 em 30 minutos)
- alert: LoopZeroAppHighRestartRate
  expr: |
    increase(
      kube_pod_container_status_restarts_total[30m]
    ) * on(namespace) group_left()
    kube_namespace_labels{label_loopzero_io_managed_by="environment-controller"}
    > 10
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "App pod {{ $labels.pod }} in {{ $labels.namespace }} restarting excessively"
    description: "Pod has restarted 10+ times in 30 minutes. Likely unstable or misconfigured."
    runbook: "Check app logs. Consider rollback if latest deploy caused the instability."
```

**Rationale:** Alertas filtrados por label `loopzero.io/managed-by=environment-controller` para nao misturar com pods de plataforma (que tem alertas proprios). Quatro alertas cobrem os cenarios mais comuns: crash loop, image pull failure, replica mismatch, e restart rate alto.

**Testes:**
- `task validate:manifests` — YAML valido
- Manual: verificar que alertas aparecem no Alertmanager apos deploy no DO

### 1.6 — Graceful shutdown com preStop hook

**Arquivo:** `api/internal/manifests/renderer.go`

**Situacao atual:** Sem `terminationGracePeriodSeconds` nem preStop hook. Pods recebem SIGTERM e tem 30s (default K8s) para encerrar.

**Mudanca:**

Adicionar campo na struct `k8sPodSpec`:
```go
type k8sPodSpec struct {
    Containers                    []k8sContainer `json:"containers"`
    TerminationGracePeriodSeconds int            `json:"terminationGracePeriodSeconds,omitempty"`
}
```

Configurar 30s (explicito, mesmo que seja o default, para clareza):
```go
Spec: k8sPodSpec{
    Containers:                    []k8sContainer{container},
    TerminationGracePeriodSeconds: 30,
}
```

**Rationale:** Explicitar o grace period torna o contrato claro e auditavel. 30s e suficiente para a maioria das apps. Nao adicionamos preStop hook customizavel neste momento (YAGNI) — o default do K8s (SIGTERM + grace period) cobre o caso geral.

**Testes:**
- `TestBuildDeployment_HasTerminationGracePeriod` — valor 30 presente

---

## Fase 2 — Policy Engine (Kyverno)

> **Dependencias:** Fase 1 completa. Helm chart novo, Pulumi update.
> **Impacto:** `infra/helm/kyverno/`, `infra/pulumi/`, cluster mgmt + workload
> **CNCF Status:** Incubating. Escolhido sobre OPA Gatekeeper por ser YAML-nativo e ter `verifyImages` built-in.

### 2.1 — Instalar Kyverno via Helm + Pulumi

**Novos arquivos:**
- `infra/helm/kyverno/values.yaml`
- `infra/helm/kyverno/Chart.yaml`
- `infra/helm/kyverno/policies/` (diretorio de policies)

**Pulumi:** Adicionar `installKyverno()` em `mgmt-bootstrap.ts` e `workload-bootstrap.ts`.

**Chart version:** Kyverno 3.x (ultima stable, verificar no momento da implementacao).

**Configuracao critica:**
```yaml
# values.yaml
admissionController:
  replicas: 2                          # HA
  resources:
    requests: { cpu: 100m, memory: 256Mi }
    limits:   { cpu: 500m, memory: 512Mi }

webhookConfiguration:
  failurePolicy: Ignore                # NAO bloqueia deploys se Kyverno cair
  timeoutSeconds: 5                    # Fail fast

backgroundController:
  resources:
    requests: { cpu: 50m, memory: 128Mi }
    limits:   { cpu: 200m, memory: 256Mi }
```

**Instalar em:** Cluster mgmt (protege plataforma) + cluster workload (protege apps de tenant).

**Testes:**
- Pulumi preview sem erros
- `kubectl get clusterpolicy` lista policies
- Deploy existente nao e bloqueado

### 2.2 — Policies de admissao

**Arquivo:** `infra/helm/kyverno/policies/`

**Policy 1 — Registry permitido:**
```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: loopzero-restrict-image-registries
spec:
  validationFailureAction: Audit  # Audit 48h, depois Enforce
  rules:
    - name: validate-registry
      match:
        resources:
          kinds: [Pod]
          namespaceSelector:
            matchLabels:
              loopzero.io/managed-by: environment-controller
      validate:
        message: "Images must come from the LoopZero container registry."
        pattern:
          spec:
            containers:
              - image: "registry.digitalocean.com/loopzero-infra-registry/*"
```

**Policy 2 — Proibir tag :latest:**
```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: loopzero-disallow-latest-tag
spec:
  validationFailureAction: Audit
  rules:
    - name: disallow-latest
      match:
        resources:
          kinds: [Pod]
          namespaceSelector:
            matchLabels:
              loopzero.io/managed-by: environment-controller
      validate:
        message: "Image tag ':latest' is not allowed. Use a specific version or digest."
        pattern:
          spec:
            containers:
              - image: "!*:latest"
```

**Policy 3 — Exigir resource limits:**
```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: loopzero-require-resource-limits
spec:
  validationFailureAction: Audit
  rules:
    - name: require-limits
      match:
        resources:
          kinds: [Pod]
          namespaceSelector:
            matchLabels:
              loopzero.io/managed-by: environment-controller
      validate:
        message: "Containers must have resource limits defined."
        pattern:
          spec:
            containers:
              - resources:
                  limits:
                    memory: "?*"
                    cpu: "?*"
```

**Rollout plan:**
1. Deploy com `validationFailureAction: Audit` (48h)
2. Verificar policy reports: `kubectl get policyreport -A`
3. Se zero violations reais → mudar para `Enforce`
4. Se violations → corrigir manifests primeiro, depois Enforce

**Testes:**
- Deploy com imagem de registry errado → Audit event (nao bloqueio)
- Deploy com `:latest` → Audit event
- Deploy sem limits → Audit event
- Apos flip para Enforce: deploy com imagem correta → permitido
- Apos flip para Enforce: deploy com `:latest` → bloqueado (403)

### 2.3 — Kyverno no Taskfile

**Arquivo:** `Taskfile.yml`

```yaml
kyverno:status:
  desc: Check Kyverno health and policy reports
  cmds:
    - kubectl get clusterpolicy -o wide
    - kubectl get policyreport -A --no-headers | head -20
```

---

## Fase 3 — Deploy Intelligence (API + Pipeline)

> **Dependencias:** Fase 1 completa. Fase 2 recomendada mas nao bloqueante.
> **Impacto:** `api/internal/services/`, `api/internal/routes/`, `cli/`, `infra/helm/argo-workflows/`

### 3.1 — Deploy circuit breaker baseado em health do app

**Arquivos:** `api/internal/services/build_orchestrator.go`, `api/internal/routes/builds.go`

**Situacao atual:** O BuildOrchestrator verifica concurrent builds via advisory lock, mas NAO verifica se a release anterior esta healthy. Usuario pode fazer deploy em cima de deploy quebrado infinitamente.

**Mudanca:**

Antes de aceitar um novo build, verificar se a release ACTIVE no environment esta healthy:

```go
// Dentro do withEnvironmentLock, apos concurrent build check:
activeRelease, err := s.releaseStore.GetActiveByEnv(ctx, tx, tenantID, projectID, environment)
if err == nil && activeRelease != nil {
    health := s.checkReleaseHealth(ctx, activeRelease)
    if health == HealthCrashLoop || health == HealthImagePullFailed {
        // Permitir deploy apenas se --force flag presente
        if !req.Force {
            return nil, loopzeroerrrors.NewDeployBlockedError(
                "Current deployment is unhealthy (%s). Use --force to deploy anyway, or `loopzero rollback` to revert.",
                health,
            )
        }
        // Log warning mas permite com --force
        s.logger.Warn("deploying over unhealthy release",
            "release_id", activeRelease.ID,
            "health", health,
            "force", true,
        )
    }
}
```

**checkReleaseHealth:** Query Prometheus ou checar pod status via K8s API no workload cluster.

**CLI:** Adicionar flag `--force` ao `loopzero deploy`:
```typescript
.option('--force', 'Deploy even if current deployment is unhealthy')
```

**IMPORTANTE:** `loopzero rollback` NUNCA e bloqueado por este circuit breaker. Rollback e a solucao para unhealthy, nao algo a ser bloqueado.

**Testes:**
- `TestBuildOrchestrator_BlocksDeployWhenPreviousUnhealthy` — retorna 409 sem --force
- `TestBuildOrchestrator_AllowsDeployWithForce` — aceita com --force
- `TestBuildOrchestrator_RollbackNeverBlocked` — rollback ignora health check
- `TestBuildOrchestrator_HealthyReleaseAllowsDeploy` — happy path nao afetado

### 3.2 — Pod Failure Policy no Argo Workflows

**Arquivo:** `api/internal/argo/workflow_builder.go`

**Situacao atual:** Workflows usam `retryStrategy: { limit: 2, retryPolicy: OnFailure }`. Qualquer falha causa retry, incluindo erros irrecuperaveis (OOM, config error).

**Mudanca:**

Adicionar `podFailurePolicy` nos templates de build step:
```yaml
podFailurePolicy:
  rules:
    # OOM Kill — nao adianta retry, vai falhar de novo
    - action: FailJob
      onExitCodes:
        containerName: main
        values: [137]  # SIGKILL (OOM)
    # Config error — arquivo invalido, nao e transient
    - action: FailJob
      onExitCodes:
        containerName: main
        values: [1]    # generic error (Dockerfile syntax, missing file)
    # Infrastructure disruption — retry e seguro
    - action: Ignore
      onPodConditions:
        - type: DisruptionTarget
```

**Nota:** Isso e implementado no Go code que gera o workflow YAML, nao no CWT.

**Rationale:** OOM (exit 137) com retry so queima 2x os recursos sem chance de sucesso. Exit code 1 em build geralmente indica erro de config (Dockerfile invalido, dependencia faltando). DisruptionTarget indica preemption/eviction — retry e seguro pois o build pode ter sido interrompido sem culpa do usuario.

**Testes:**
- `TestWorkflowBuilder_PodFailurePolicy_OOM` — exit 137 gera FailJob
- `TestWorkflowBuilder_PodFailurePolicy_ConfigError` — exit 1 gera FailJob
- `TestWorkflowBuilder_PodFailurePolicy_Disruption` — DisruptionTarget gera Ignore
- `TestWorkflowBuilder_RetryStrategy_Preserved` — retry limit 2 continua para outros exit codes

**Nota sobre compatibilidade:** Pod Failure Policy para Jobs esta GA desde K8s 1.31. Verificar se Argo Workflows suporta via campo no WorkflowSpec/template. Se Argo Workflows nao suportar `podFailurePolicy` diretamente, usar `activeDeadlineSeconds` no step como fallback (ja usado no `cwt-cache-save`).

---

## Fase 4 — Progressive Delivery + Supply Chain (medio prazo)

> **Dependencias:** Fases 1-3 completas. Requer infra adicional.
> **Impacto:** `infra/helm/argo-rollouts/`, `infra/pulumi/`, `api/internal/manifests/`, build pipeline
> **Nota:** Esta fase requer design doc proprio (MEET separado) para cada item.

### 4.1 — Argo Rollouts com canary + AnalysisTemplate

**Novos arquivos:**
- `infra/helm/argo-rollouts/values.yaml`
- `infra/helm/argo-rollouts/Chart.yaml`
- `infra/helm/argo-rollouts/templates/analysis-template.yaml`

**Mudanca conceitual:** Manifests gerados pela API mudam de `kind: Deployment` para `kind: Rollout` (CRD do Argo Rollouts). O Rollout e gerenciado pelo ArgoCD normalmente (ArgoCD sabe lidar com Rollouts).

**AnalysisTemplate:**
```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: loopzero-canary-analysis
spec:
  metrics:
    - name: success-rate
      interval: 30s
      successCondition: result[0] >= 0.95
      failureLimit: 3
      provider:
        prometheus:
          address: http://prometheus-server.monitoring:80
          query: |
            sum(rate(traefik_service_requests_total{
              code=~"2..",
              service=~"{{ args.service }}@kubernetescrd"
            }[2m])) /
            sum(rate(traefik_service_requests_total{
              service=~"{{ args.service }}@kubernetescrd"
            }[2m]))
    - name: latency-p99
      interval: 30s
      successCondition: result[0] < 5
      failureLimit: 3
      provider:
        prometheus:
          address: http://prometheus-server.monitoring:80
          query: |
            histogram_quantile(0.99,
              sum(rate(traefik_service_request_duration_seconds_bucket{
                service=~"{{ args.service }}@kubernetescrd"
              }[2m])) by (le)
            )
```

**Rollout spec gerado:**
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  strategy:
    canary:
      canaryService: app-canary
      stableService: app-stable
      trafficRouting:
        traefik:
          weightedTraefikServiceName: app-weighted
      steps:
        - setWeight: 20
        - pause: { duration: 60s }
        - analysis:
            templates:
              - templateName: loopzero-canary-analysis
            args:
              - name: service
                value: "{{ .ObjectMeta.Name }}"
        - setWeight: 50
        - pause: { duration: 60s }
        - setWeight: 100
      rollbackWindow:
        revisions: 2
```

**Impacto no fluxo:**
- `renderer.go` gera `Rollout` ao inves de `Deployment` quando Argo Rollouts esta disponivel
- ArgoCD sync continua normal (ArgoCD suporta Rollout CRD nativamente)
- Rollback: `loopzero rollback` pode usar `kubectl argo rollouts undo` via API

**Pre-requisitos para design doc:**
- Como detectar se Argo Rollouts esta instalado no cluster (feature detection)
- Como o Traefik traffic splitting funciona com Rollouts
- Impacto em apps com 1 replica (canary com 1 replica = 100% ou 0%)
- Metricas de Traefik vs metricas do app (qual usar na analysis)

### 4.2 — Image signing com cosign + Kyverno verifyImages

**Dependencia:** Sprint 6 (Security + Supply Chain) + Kyverno instalado (Fase 2).

**Mudancas no build pipeline:**
1. Apos BuildKit push image → cosign sign (keyless via Fulcio/OIDC)
2. Signature armazenada junto a imagem no registry
3. Kyverno policy `verifyImages` valida assinatura na admissao

**Kyverno policy:**
```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: loopzero-verify-image-signatures
spec:
  validationFailureAction: Enforce  # Apenas em prod
  rules:
    - name: verify-cosign-signature
      match:
        resources:
          kinds: [Pod]
          namespaceSelector:
            matchLabels:
              loopzero.io/managed-by: environment-controller
      verifyImages:
        - imageReferences:
            - "registry.digitalocean.com/loopzero-infra-registry/*"
          attestors:
            - entries:
                - keyless:
                    issuer: "https://token.actions.githubusercontent.com"
                    subject: "https://github.com/getloopscale/*"
```

**Regra W9:** Dev/staging usa `validationFailureAction: Audit`. Apenas prod usa `Enforce`.

**Dependencia de infra:** Requer Sigstore (Fulcio, Rekor) ou keyless signing via GitHub Actions OIDC. Alinhado com Sprint 6.

### 4.3 — Custom Reaper para CrashLoopBackOff

**Conceito:** Componente que monitora Deployments em namespaces de tenant. Se TODOS os pods de um Deployment estao em CrashLoopBackOff por mais de 5 minutos, escala para 0 replicas e notifica o usuario.

**Opcoes de implementacao:**
1. **Controller dedicado** (novo controller no operators/) — mais robusto, mais codigo
2. **CronJob com kubectl** — simples mas fragil
3. **Extensao do ApplicationController** — adicionar logica de reaping ao reconcile loop existente

**Recomendacao:** Opcao 3 — estender o ApplicationController. Ja tem acesso ao status do app e pode verificar pod conditions. Adicionar campo `Status.UnhealthySince` na CRD LoopZeroApplication para rastrear tempo em estado unhealthy.

**Logica:**
```go
// No reconcile loop do ApplicationController:
if allPodsInCrashLoop(app) {
    if app.Status.UnhealthySince.IsZero() {
        app.Status.UnhealthySince = metav1.Now()
    }
    unhealthyDuration := time.Since(app.Status.UnhealthySince.Time)
    if unhealthyDuration > 5*time.Minute {
        // Scale to 0 via ArgoCD (nao diretamente, senao ArgoCD restaura)
        // Marcar app como "scaled-down-unhealthy"
        // Enviar evento SSE para CLI/dashboard
    }
} else {
    app.Status.UnhealthySince = nil
}
```

**CUIDADO:** Escalar para 0 diretamente no K8s nao funciona — ArgoCD vai restaurar para 1. A solucao correta e:
1. Atualizar o manifest no Gitea com `replicas: 0`
2. ArgoCD sync → pod removido
3. Proximo `loopzero deploy` restaura `replicas: 1`

**Pre-requisitos para design doc:**
- Como notificar o usuario (SSE event? Email? Slack webhook?)
- Quanto tempo antes de scale-down (5min? Configuravel por tenant?)
- Como restaurar (proximo deploy automaticamente restaura, ou precisa de acao explicita?)
- Interacao com `loopzero rollback` (rollback deve restaurar replicas para 1)

---

## Sequenciamento e dependencias

```
Fase 1 (items 1.1-1.6) ─────────────────────────┐
  Todos podem ser implementados em paralelo       │
  Zero dependencias externas                       │
  Estimativa: 1-2 sessoes de trabalho              │
                                                   ▼
Fase 2 (items 2.1-2.3) ────────────────────────── Fase 3 (items 3.1-3.2)
  Kyverno install + policies                       Deploy circuit breaker + Pod Failure Policy
  Depende: Fase 1 (LimitRange ja injetado)         Depende: Fase 1 (probes configurados)
  Estimativa: 1-2 sessoes                          Estimativa: 1-2 sessoes
                    │                                          │
                    ▼                                          ▼
              Fase 4 (items 4.1-4.3) ──────────────────────────
                Argo Rollouts + cosign + Reaper
                Depende: Fases 2+3 completas
                Requer design doc proprio por item
                Estimativa: 3-5 sessoes (complexo)
```

**Fase 1** e a prioridade maxima — 80% do valor com 0% de dependencias novas.

---

## Contrato do endpoint `/health` (F1-C3)

LoopZero gera probes HTTP apontando para `GET /health` na porta do container. O endpoint e responsabilidade do usuario — LoopZero NAO injeta sidecar ou health endpoint automatico.

### Contrato

- **Path:** `/health` (hardcoded, nao configuravel na Fase 1)
- **Method:** `GET`
- **Resposta esperada:** HTTP 200 para healthy, qualquer outro codigo para unhealthy
- **Timeout:** 3 segundos (probe timeout)

### Recomendacoes para o usuario

- O endpoint `/health` DEVE ser leve (< 100ms response time)
- O endpoint `/health` NAO DEVE checar dependencias externas (banco, cache, APIs)
  - Se o banco cai e `/health` retorna 500, o K8s mata TODOS os pods simultaneamente (cascading failure)
  - Checar dependencias e papel do readiness probe em implementacoes futuras com paths separados
- O endpoint `/health` DEVE retornar 200 assim que o processo estiver pronto para receber requests
- Apps sem `/health` implementado terao startup probe falhando e nunca receberao trafico

### Probes gerados

| Probe | Funcao | Threshold | Acao no K8s |
|-------|--------|-----------|-------------|
| Startup | App inicializou? | 30 x 5s = 150s max | Bloqueia liveness/readiness ate sucesso |
| Readiness | Pronto para trafico? | 3 falhas x 10s = 30s | Remove do Service endpoints |
| Liveness | Processo vivo? | 5 falhas x 15s = 75s | Reinicia container |

---

## Checklist de validacao por fase

### Fase 1 — Done quando:
- [x] `cd api && go test ./...` passa (W1) — 25 packages, 0 FAIL
- [ ] `task validate:manifests` passa (W2) — requires k3d cluster
- [x] Manifests gerados contem: startupProbe, readinessProbe (com thresholds), livenessProbe (com thresholds), progressDeadlineSeconds (210s), minReadySeconds (10s), strategy (maxUnavailable:0), PDB (AlwaysAllow), terminationGracePeriodSeconds (30s)
- [x] 6 novos alertas: CrashLoop (5m), ImagePullFailed, ReplicaMismatch (5m), HighRestartRate, DeployStuck, CrashLoopCritical (30m)
- [ ] Deploy existente no DO funciona sem alteracao no loopzero.yaml (W3) — requires DO validation
- [x] CHANGELOG atualizado
- [x] F1-C1: progressDeadlineSeconds = 210s
- [x] F1-C2: Workers recebem PDB
- [x] F1-C3: Contrato /health documentado
- [x] F1-C4: CrashLoop 5m, ReplicaMismatch 5m
- [x] F1-C5: Alerta ProgressDeadlineExceeded (LoopZeroAppDeployStuck)
- [x] F1-C6: Alerta critical CrashLoop 30m (LoopZeroAppCrashLoopCritical)
- [x] F1-C7: 11 testes com parsing estrutural YAML (nao string matching)
- [ ] F1-C8: Integrar progressDeadline + convergence loop (Fase 3)

### Fase 2 — Done quando:
- [ ] Kyverno running nos clusters mgmt + workload
- [ ] 3 policies em modo Audit
- [ ] Zero violations em workloads existentes (W4)
- [ ] `task kyverno:status` funciona
- [ ] 48h de Audit sem problemas → flip para Enforce
- [ ] CHANGELOG atualizado

### Fase 3 — Done quando:
- [ ] `loopzero deploy` com release unhealthy retorna erro sugerindo --force ou rollback
- [ ] `loopzero deploy --force` funciona mesmo com release unhealthy
- [ ] `loopzero rollback` NUNCA bloqueado (W6)
- [ ] Pod Failure Policy: OOM nao causa retry
- [ ] `cd api && go test ./...` passa
- [ ] CHANGELOG atualizado

### Fase 4 — Done quando:
- [ ] Design docs aprovados em reuniao tecnica separada
- [ ] Argo Rollouts instalado e funcional
- [ ] Canary deploy com auto-rollback validado no DO
- [ ] cosign signing no build pipeline (apenas prod enforce)
- [ ] Reaper: app em CrashLoopBackOff > 5min escala para 0
- [ ] CHANGELOG atualizado

---

## Riscos e mitigacoes

| Risco | Probabilidade | Impacto | Mitigacao |
|-------|--------------|---------|-----------|
| Startup probe muito curto mata app JVM | Media | Alto | 150s timeout cobre 99% dos frameworks. Futuro: campo no loopzero.yaml para override |
| Kyverno webhook adiciona latencia | Baixa | Medio | `timeoutSeconds: 5`, `failurePolicy: Ignore`. Monitoring via ServiceMonitor |
| Deploy circuit breaker false positive | Media | Alto | Apenas bloqueia com --force escape hatch. Rollback nunca bloqueado (W6) |
| Argo Rollouts + ArgoCD conflito | Media | Alto | Design doc dedicado. Testar em staging antes de prod |
| Pod Failure Policy nao suportada pelo Argo Workflows | Baixa | Baixo | Fallback: `activeDeadlineSeconds` por step (ja usado em cache-save) |
| CrashLoopBackOff alert noise (too many alerts) | Media | Baixo | Filtro por namespace label. Routing no Alertmanager para canal separado |

---

## Referencias

- [CNCF: Kubernetes Multi-tenancy Best Practices](https://kubernetes.io/docs/concepts/security/multi-tenancy/)
- [KEP-3017: Unhealthy Pod Eviction Policy for PDBs](https://github.com/kubernetes/enhancements/tree/master/keps/sig-apps/3017-pod-healthy-policy-for-pdb)
- [Kyverno: Image Verification](https://kyverno.io/docs/policy-types/cluster-policy/verify-images/sigstore/)
- [Argo Rollouts: Analysis & Progressive Delivery](https://argoproj.github.io/argo-rollouts/features/analysis/)
- [K8s Pod Failure Policy (GA 1.31)](https://kubernetes.io/blog/2024/08/19/kubernetes-1-31-pod-failure-policy-for-jobs-goes-ga/)
- [Sigstore Policy Controller](https://docs.sigstore.dev/policy-controller/overview/)
- LoopZero spec: `docs/technical.md` §12 (Failure modes), §15 (Observability), §16 (Security)
