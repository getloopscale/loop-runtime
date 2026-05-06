# Plan: Argo Rollouts — Progressive Delivery

> **Version 1.0** — Substituir deploys all-at-once (`Deployment`) por progressive delivery via Argo Rollouts (`Rollout` com canary). Um deploy ruim nunca afeta 100% do tráfego. A canary analysis automática via Prometheus decide promote ou abort sem intervenção humana. Estratégias graduadas por plano (Starter=1 step, Pro=3, Team=4, Enterprise=custom). Fecha o gap "deploy resilience at scale" e é pré-requisito para 50+ tenants sem risco.

## Context

### O que existe hoje

- **Manifest generator** (`api/internal/manifests/renderer_deployment.go`): gera `apps/v1 Deployment` com `RollingUpdate` strategy (`maxUnavailable:0, maxSurge:1`). Todos os pods são substituídos simultaneamente.
- **Service:** 1 Service por app (`{projectID}-{appName}`) apontando diretamente para pods.
- **Ingress:** Traefik `IngressRoute` aponta para o Service (ou KEDA interceptor em scale-to-zero).
- **HPA/KEDA:** `ScaledObject.scaleTargetRef` referencia Deployment por nome (sem kind/apiVersion explícitos no KEDA, com `Kind: Deployment` no HPA).
- **ArgoCD Application:** Criada por `operators/internal/argocd/real_provisioner.go:163 buildApplicationObject()`. Usa unstructured client. **Atualmente SEM `ignoreDifferences`** no código Go (apenas no template Helm `tenant-app-template.yaml:40`).
- **Rollback:** Reativo (~3s) via state machine `ROLLED_BACK`. Requer detecção humana ou alerta.
- **SSE events:** `build.queued → build.started → build.completed → deploy.verifying → deploy.completed`. Nenhum evento intermediário para canary progress.

### Evidência

- `docs/strategy/resilience-roadmap.md` — P2 Argo Rollouts, estimated maturity +0.3
- `renderer_deployment.go:133` — `Strategy.Type: "RollingUpdate"` (all-at-once)
- `real_provisioner.go:163` — `buildApplicationObject()` sem `ignoreDifferences`
- `renderer_deployment.go:194` — HPA `ScaleTargetRef.Kind: "Deployment"`
- `renderer_deployment.go:245` — KEDA `ScaleTargetRef` sem Kind (defaults to Deployment)
- Zero traffic-splitting capability
- Zero métricas de deployment confidence

## Objective

**Done =** Todos os deploys de tenant apps usam `Rollout` (canary) com análise automática via Prometheus. A CLI mostra canary progress em tempo real. ArgoCD não detecta drift nos weight fields. KEDA funciona com Rollout. Rollback proativo (auto-abort em bad deploy) validado E2E.

Metas mensuráveis:

1. Argo Rollouts controller instalado em todos os workload clusters
2. Manifest generator produz `Rollout` + 2 Services (stable/canary) + `TraefikService` + `AnalysisTemplate`
3. Estratégia canary graduada por plano (Starter/Pro/Team/Enterprise)
4. ArgoCD ignora `spec.replicas` em Rollouts e weight em TraefikService
5. KEDA ScaledObject e HPA referenciam `Rollout` (kind + apiVersion)
6. CLI mostra canary % e analysis status em `loopzero status` e durante `loopzero deploy`
7. SSE events incluem `rollout.step` intermediários
8. E2E test valida: deploy → canary 20% → promote → 100% + abort em bad deploy

## ADRs

### D1 — Canary (não Blue-Green)

**Decision:** Usar estratégia canary com traffic weighting via Traefik, não blue-green.

**Rationale:** Blue-green requer o dobro de pods (duplica custo). Canary usa os mesmos pods com traffic splitting progressivo. Traefik já suporta weighted routing via `TraefikService`. Para Starter (2 replicas), canary 50% = 1 pod novo, 1 pod antigo — equivalente a blue-green sem custo extra. Argo Rollouts suporta Traefik como traffic manager nativo.

**Consequences:** Requer 2 Services (stable + canary) e 1 TraefikService por app. ArgoCD deve ignorar weight changes. Argo Rollouts controla o peso via TraefikService API.

### D2 — Estratégia por plano (não por app)

**Decision:** A estratégia canary é determinada pelo plano do tenant (Starter/Pro/Team/Enterprise), não configurável por app individual.

**Rationale:** Simplifica UX (nenhuma config necessária para 99% dos users). Plan caps já existem em `pkg/enforcement/limiter.go`. Enterprise é o único tier com custom steps via `loopzero.yaml`. Starter com 2 replicas não tem granularidade para canary multi-step — 50/100 é o máximo útil.

**Consequences:** Novo campo `Plan` no `RenderInput` (passado pelo `BuildOrchestrator`). O manifest generator seleciona steps baseado no plan. Enterprise lê custom steps de `loopzero.yaml`.

### D3 — AnalysisTemplate por namespace (não global)

**Decision:** Cada environment namespace recebe seu próprio `AnalysisTemplate` gerado pelo manifest renderer, não um template global em `loopzero-system`.

**Rationale:** O Prometheus query precisa do service name correto (`{{args.service}}`). Templates globais requerem RBAC cross-namespace. Templates por namespace são auto-contidos e limpos quando o environment é deletado (ownership via namespace). Segue o padrão existente: todos os manifests vivem no namespace do environment.

**Consequences:** Template duplicado em cada namespace — aceitável, são ~20 linhas YAML. O manifest renderer adiciona o AnalysisTemplate na lista de docs renderizados.

### D4 — Rollout para TODOS os tipos de app (server + frontend + worker)

**Decision:** Todas as apps que hoje geram Deployment passam a gerar Rollout. Workers incluídos.

**Rationale:** Workers (sem traffic) usam Rollout com análise baseada apenas em pod health (sem Prometheus metrics). Manter uniformidade simplifica o código — um único `buildRollout()` substitui `buildDeployment()`. Se worker apps mantivessem Deployment, precisaríamos de dois caminhos no renderer, KEDA config diferenciado, e ArgoCD ignoreDifferences com condições.

**Consequences:** Workers usam `strategy.canary.steps` sem `setWeight` (só `pause` + analysis). A análise para workers verifica apenas pod health/restart count, não traffic metrics.

### D5 — TraefikService (não Traefik plugin/middleware)

**Decision:** Usar `TraefikService` (CRD nativo do Traefik) para weighted routing entre stable e canary Services, com Argo Rollouts como traffic manager.

**Rationale:** Argo Rollouts tem integração nativa com Traefik via `TraefikService`. O Rollouts controller modifica os pesos automaticamente durante canary steps. Alternativas (Istio VirtualService, Nginx Ingress annotations) requerem service mesh ou ingress controller diferente — não se aplica.

**Consequences:** IngressRoute passa a apontar para TraefikService (não diretamente para Service). ArgoCD deve ignorar `/spec/weighted/services/*/weight` no TraefikService.

### D6 — Prometheus address cross-cluster

**Decision:** `AnalysisTemplate` usa Prometheus address resolvível do workload cluster, não `http://prometheus.monitoring:9090` (que é mgmt-cluster-local).

**Rationale:** Prometheus roda no mgmt cluster. Workload clusters não resolvem `prometheus.monitoring.svc`. Em standalone-multi, usa `crossCluster.prometheusApi` (ex: `http://host.k3d.internal:9090`). Em prod, usa o endpoint via CF Tunnel ou service mesh. A address é injetada via variável no manifest.

**Consequences:** Novo campo `PrometheusAddress` no `RenderInput`. Populado pelo `BuildOrchestrator` a partir da config.

### D7 — Enterprise custom steps via loopzero.yaml (V2 — fora do escopo inicial)

**Decision:** V1 implementa apenas Starter/Pro/Team strategies fixas. Enterprise custom steps são V2.

**Rationale:** YAGNI — zero clientes Enterprise hoje. O schema `loopzero.yaml` para custom canary steps requer design review separado (T3). V1 cobre 100% dos tiers ativos. Enterprise pode usar Team strategy como fallback.

**Consequences:** Enterprise usa Team strategy em V1. V2 adiciona `canary.steps` no `loopzero.yaml` com meeting T3.

### D8 — Manter Deployment como fallback condicional (feature flag)

**Decision:** Manter capacidade de gerar `Deployment` (legacy) via feature flag `LOOPZERO_PROGRESSIVE_DELIVERY=false` durante rollout. Padrão: `true` (Rollout).

**Rationale:** Rollout é breaking change no manifest (ArgoCD vê delete Deployment + create Rollout). Se algo falhar em prod, precisamos de rollback rápido. Feature flag permite desabilitar sem revert de código. Remove após 30 dias de estabilidade.

**Consequences:** `renderServerApp()` e peers verificam flag. Ambos caminhos têm testes. Flag vive em env var da API (não do CLI — transparente para o developer).

**EC-4 Migration Strategy:** A flag NÃO deve ser flipada globalmente de uma vez. Migração por tenant: (1) habilitar progressive delivery para novos tenants primeiro, (2) migrar tenants existentes um a um via `loopzero admin tenants set-progressive-delivery --tenant X`. Argo Rollouts pode adotar pods existentes via matching `selector.matchLabels` (ambos usam `app: {resName}`), minimizando downtime para ~5s (tempo de criar ReplicaSet). Documentar procedimento de migração no runbook. A annotation `argocd.argoproj.io/sync-wave: "-1"` no Rollout garante criação antes do delete do Deployment.

## Dependency Graph

```
Phase 0 (Infra: Argo Rollouts controller) ──▶ Phase 1 (Manifests: Rollout + Services)
                                                        │
                                              ┌─────────┴──────────┐
                                              ▼                    ▼
                                    Phase 2 (ArgoCD config)   Phase 3 (KEDA compat)
                                              │                    │
                                              └─────────┬──────────┘
                                                        ▼
                                              Phase 4 (CLI + SSE)
                                                        │
                                                        ▼
                                              Phase 5 (E2E + Dogfood)
```

- Phase 0 → Phase 1: controller precisa existir antes de aplicar Rollout manifests
- Phase 2 e Phase 3: paralelas (independentes — ArgoCD config e KEDA são sistemas separados)
- Phase 4: depende de Phase 1+2+3 (CLI mostra dados que dependem de Rollout + ArgoCD + KEDA)
- Phase 5: valida tudo end-to-end

---

## Phase 0: Infra — Argo Rollouts Controller

**Objective:** Instalar Argo Rollouts controller em todos os workload clusters via Helm chart wrapper + Helmfile.

### T0.1 — Criar Helm chart wrapper `infra/helm/argo-rollouts/`

#### Objective
Chart wrapper para Argo Rollouts no padrão chaos-mesh/velero.

#### Evidence
- Argo Rollouts Helm chart: `argo/argo-rollouts` no repo `https://argoproj.github.io/argo-helm` (já registrado no Helmfile mgmt)
- Versão estável: 2.38.1 (Argo Rollouts 1.7.x)
- Repo `argo` já existe no `mgmt.helmfile.yaml.gotmpl:52`

#### Files to edit
```
infra/helm/argo-rollouts/Chart.yaml (NEW) — wrapper chart
infra/helm/argo-rollouts/values.yaml (NEW) — values hardened (PSA restricted, Traefik traffic manager plugin)
infra/helm/argo-rollouts/.helmignore (NEW)
```

#### Deep file dependency analysis
- **`Chart.yaml`** (NEW) — Dependency `argo-rollouts` do repo argo. Downstream: Helmfile runtime referencia `chart: ../helm/argo-rollouts`.
- **`values.yaml`** (NEW) — Configura: `controller.trafficRouterPlugins.traefik` (registra Traefik como traffic manager), resource limits, PSA restricted. Downstream: Argo Rollouts controller sabe usar TraefikService para traffic splitting.

#### Deep Dives
- **Traffic Router Plugin:** Argo Rollouts precisa de configuração explícita para usar Traefik como traffic manager. No `values.yaml`:
  ```yaml
  controller:
    trafficRouterPlugins:
      traefik: {}  # Ativa o plugin nativo Traefik
  ```
- **Dashboard:** DESABILITADO (mesma política que Chaos Mesh — attack surface desnecessária).
- **CRDs:** Argo Rollouts chart instala seus próprios CRDs (`Rollout`, `AnalysisTemplate`, `AnalysisRun`, `Experiment`).

#### Tasks
1. Criar `infra/helm/argo-rollouts/Chart.yaml` com dependency `argo-rollouts` v2.38.1
2. Criar `infra/helm/argo-rollouts/values.yaml` com Traefik plugin, PSA restricted, resource limits, dashboard disabled
3. Criar `.helmignore`
4. `helm dependency update infra/helm/argo-rollouts/`

#### TDD
```
RED:     test_rollouts_chart_renders() — helm template produz Deployment do controller sem erros
RED:     test_rollouts_psa_restricted() — securityContext tem runAsNonRoot + drop ALL
RED:     test_rollouts_no_dashboard() — output NÃO contém Dashboard Deployment
GREEN:   Criar os 3 arquivos
REFACTOR: Nenhum esperado
VERIFY:  helm template test infra/helm/argo-rollouts/ | kubeconform --strict
```

#### Acceptance Criteria
- [ ] `helm template` renderiza sem erros
- [ ] `kubeconform --strict` passa
- [ ] Dashboard desabilitado
- [ ] PSA restricted em todos os pods
- [ ] Traefik traffic router configurado

#### DoD (Definition of Done)
- [ ] Chart renderiza e valida
- [ ] `helm dependency update` baixa chart argo/argo-rollouts

### T0.2 — Integrar no Helmfile runtime

#### Objective
Adicionar release `argo-rollouts` no `runtime.helmfile.yaml.gotmpl` em todos os workload clusters.

#### Evidence
- `runtime.helmfile.yaml.gotmpl` — gerencia workload clusters. Argo Rollouts deve estar onde os Rollout CRs são criados.
- Repo `argo` NÃO está registrado no runtime helmfile (apenas no mgmt). Precisa adicionar.

#### Files to edit
```
infra/helmfile/runtime.helmfile.yaml.gotmpl — adicionar repo argo + release argo-rollouts
```

#### Deep file dependency analysis
- **`runtime.helmfile.yaml.gotmpl`** — Hoje tem cert-manager, traefik, ESO, KEDA, policy-controller, promtail, cloudflared. Mudança: adicionar repo `argo` e release `argo-rollouts` em Wave 0 (junto com KEDA — ambos são controllers que gerenciam workloads). `needs: [cert-manager/cert-manager]` (Rollouts webhook usa cert-manager para TLS).

#### Tasks
1. Adicionar repo `argo` em `repositories:`
2. Adicionar release `argo-rollouts` em Wave 0 com `needs: [cert-manager/cert-manager]`
3. Namespace: `argo-rollouts`

#### TDD
```
RED:     test_helmfile_lint_with_rollouts() — helmfile -e standalone-multi lint deve passar
GREEN:   Editar runtime.helmfile.yaml.gotmpl
REFACTOR: Nenhum esperado
VERIFY:  helmfile -f infra/helmfile/runtime.helmfile.yaml -e standalone-multi lint
```

#### Acceptance Criteria
- [ ] `helmfile lint` green para todos os environments
- [ ] Release `argo-rollouts` na Wave 0
- [ ] `needs:` inclui cert-manager

#### DoD (Definition of Done)
- [ ] Helmfile lint green
- [ ] Nenhuma regressão em releases existentes

---

## Phase 1: Manifests — Rollout + Services + TraefikService + AnalysisTemplate

**Objective:** Modificar o manifest generator para produzir `Rollout` (em vez de `Deployment`), 2 Services (stable + canary), `TraefikService` para weighted routing, e `AnalysisTemplate` para canary analysis.

### T1.1 — Adicionar tipos Rollout no `renderer_types.go`

#### Objective
Definir structs Go para `Rollout`, `TraefikService`, e `AnalysisTemplate`.

#### Evidence
- `renderer_types.go` — 368 linhas com structs para Deployment, Service, IngressRoute, HPA, KEDA, etc. Todos são structs locais sem imports de client-go.
- Novos tipos necessários: `k8sRollout`, `k8sRolloutSpec`, `k8sCanaryStrategy`, `k8sCanaryStep`, `k8sTraefikTrafficRouting`, `traefikWeightedService`, `analysisTemplate`, `analysisMetric`.

#### Files to edit
```
api/internal/manifests/renderer_types.go — adicionar structs para Rollout, TraefikService, AnalysisTemplate
```

#### Deep file dependency analysis
- **`renderer_types.go`** — Contém todas as structs de manifest. Mudança: adicionar ~120 linhas de novos tipos. Downstream: `renderer_deployment.go` e `renderer_helpers.go` usam esses tipos.

#### Deep Dives

**Structs a criar:**

```go
// Rollout (substitui Deployment)
type k8sRollout struct {
    APIVersion string          `json:"apiVersion"`
    Kind       string          `json:"kind"`
    Metadata   k8sMetadata     `json:"metadata"`
    Spec       k8sRolloutSpec  `json:"spec"`
}

type k8sRolloutSpec struct {
    Replicas                int               `json:"replicas"`
    RevisionHistoryLimit    int               `json:"revisionHistoryLimit,omitempty"`
    Selector                k8sLabelSelector  `json:"selector"`
    Template                k8sPodTemplateSpec `json:"template"`
    Strategy                k8sRolloutStrategy `json:"strategy"`
}

type k8sRolloutStrategy struct {
    Canary *k8sCanaryStrategy `json:"canary,omitempty"`
}

type k8sCanaryStrategy struct {
    Steps                []k8sCanaryStep    `json:"steps"`
    TrafficRouting       *k8sTrafficRouting `json:"trafficRouting,omitempty"`
    Analysis             *k8sRolloutAnalysis `json:"analysis,omitempty"`
    CanaryService        string             `json:"canaryService,omitempty"`
    StableService        string             `json:"stableService,omitempty"`
    MaxUnavailable       int                `json:"maxUnavailable,omitempty"`
    MaxSurge             string             `json:"maxSurge,omitempty"`
}

type k8sCanaryStep struct {
    SetWeight *int              `json:"setWeight,omitempty"`
    Pause     *k8sCanaryPause   `json:"pause,omitempty"`
    Analysis  *k8sStepAnalysis  `json:"analysis,omitempty"`
}

// TraefikService (weighted routing entre stable e canary)
type traefikWeightedService struct {
    APIVersion string                     `json:"apiVersion"`
    Kind       string                     `json:"kind"`
    Metadata   k8sMetadata                `json:"metadata"`
    Spec       traefikWeightedServiceSpec `json:"spec"`
}

// AnalysisTemplate (Prometheus queries para canary validation)
type analysisTemplate struct {
    APIVersion string               `json:"apiVersion"`
    Kind       string               `json:"kind"`
    Metadata   k8sMetadata          `json:"metadata"`
    Spec       analysisTemplateSpec `json:"spec"`
}
```

#### Tasks
1. Adicionar todas as structs acima em `renderer_types.go`
2. Manter JSON tags com `omitempty` onde campos são opcionais
3. Seguir convenção existente (structs privadas, sem imports de client-go)

#### TDD
```
RED:     test_rollout_struct_marshals() — k8sRollout marshal produz YAML válido com apiVersion argoproj.io/v1alpha1
RED:     test_analysis_template_struct_marshals() — analysisTemplate marshal produz YAML válido
GREEN:   Adicionar structs
REFACTOR: Nenhum esperado
VERIFY:  cd api && go test ./internal/manifests/... -run TestTypes
```

#### Acceptance Criteria
- [ ] Structs compilam sem erros
- [ ] Marshal produz YAML válido
- [ ] Nenhuma dependência de client-go adicionada
- [ ] Segue convenção de naming existente

#### DoD (Definition of Done)
- [ ] `go build ./...` green
- [ ] Testes de marshal passam

### T1.2 — Adicionar `Plan` e `PrometheusAddress` ao `RenderInput`

#### Objective
O manifest generator precisa saber o plano do tenant (para escolher canary strategy) e o endpoint Prometheus (para AnalysisTemplate).

#### Evidence
- D2: estratégia por plano
- D6: Prometheus address cross-cluster
- `renderer.go:72` — `RenderInput` struct precisa de novos campos

#### Files to edit
```
api/internal/manifests/renderer.go — adicionar Plan, PrometheusAddress, ProgressiveDelivery ao RenderInput
api/internal/services/manifest_builder.go — popular os novos campos ao construir RenderInput
api/internal/services/build_orchestrator.go — passar plan + prometheus address ao manifest builder
```

#### Deep file dependency analysis
- **`renderer.go`** — Define `RenderInput`. Mudança: adicionar `Plan string`, `PrometheusAddress string`, `ProgressiveDelivery bool`. Downstream: `renderServerApp()` e peers consultam esses campos.
- **`manifest_builder.go`** — Constrói `RenderInput` a partir de build/release data. Mudança: popular `Plan` do tenant, `PrometheusAddress` da config API, `ProgressiveDelivery` da env var.
- **`build_orchestrator.go`** — Orquestra build→manifest→gitops. Mudança: passar plan info ao manifest builder.

#### Tasks
1. Adicionar campos `Plan`, `PrometheusAddress`, `ProgressiveDelivery` ao `RenderInput`
2. Adicionar feature flag check: `ProgressiveDelivery = os.Getenv("LOOPZERO_PROGRESSIVE_DELIVERY") != "false"` (D8)
3. Popular `Plan` a partir do tenant record no `manifest_builder.go`
4. Popular `PrometheusAddress` da config API

#### TDD
```
RED:     test_render_input_has_plan() — RenderInput com Plan="pro" é aceito pelo Render()
RED:     test_feature_flag_default_true() — sem env var, ProgressiveDelivery é true
RED:     test_feature_flag_false() — LOOPZERO_PROGRESSIVE_DELIVERY=false desabilita Rollout
GREEN:   Editar os 3 arquivos
REFACTOR: Nenhum esperado
VERIFY:  cd api && go test ./internal/manifests/... ./internal/services/...
```

#### Acceptance Criteria
- [ ] `RenderInput` aceita `Plan`, `PrometheusAddress`, `ProgressiveDelivery`
- [ ] Feature flag funciona (default true, false desabilita)
- [ ] `manifest_builder.go` popula corretamente do tenant/config

#### DoD (Definition of Done)
- [ ] Compilação green
- [ ] Testes passing
- [ ] Feature flag documentada

### T1.3 — Implementar `buildRollout()` substituindo `buildDeployment()`

#### Objective
Criar função que gera `Rollout` com canary strategy baseada no plano.

#### Evidence
- `renderer_deployment.go:12 buildDeployment()` — 161 linhas gerando Deployment
- D1: canary com Traefik weighted routing
- D2: steps por plano
- D4: Rollout para todos os tipos (server/frontend/worker)

#### Files to edit
```
api/internal/manifests/renderer_rollout.go (NEW) — buildRollout(), buildCanarySteps(), buildTraefikService(), buildAnalysisTemplate(), buildStableService(), buildCanaryService()
```

#### Deep file dependency analysis
- **`renderer_rollout.go`** (NEW) — Contém toda a lógica de geração de Rollout. Reutiliza: container build logic de `buildDeployment()` (extrair para `buildPodTemplateSpec()` helper). Downstream: `renderServerApp()` chama `buildRollout()` em vez de `buildDeployment()` quando `ProgressiveDelivery=true`.

#### Deep Dives

**Canary steps por plano:**

```go
func canaryStepsForPlan(plan string) []k8sCanaryStep {
    switch plan {
    case "starter":
        return []k8sCanaryStep{
            {SetWeight: intPtr(50)},
            {Pause: &k8sCanaryPause{Duration: "60s"}},
        }
    case "pro":
        return []k8sCanaryStep{
            {SetWeight: intPtr(20)},
            {Pause: &k8sCanaryPause{Duration: "30s"}},
            {Analysis: &k8sStepAnalysis{Templates: []k8sAnalysisTemplateRef{{TemplateName: "loopzero-canary-analysis"}}}},
            {SetWeight: intPtr(50)},
            {Pause: &k8sCanaryPause{Duration: "30s"}},
        }
    case "team":
        return []k8sCanaryStep{
            {SetWeight: intPtr(10)},
            {Pause: &k8sCanaryPause{Duration: "30s"}},
            {Analysis: &k8sStepAnalysis{Templates: []k8sAnalysisTemplateRef{{TemplateName: "loopzero-canary-analysis"}}}},
            {SetWeight: intPtr(30)},
            {Pause: &k8sCanaryPause{Duration: "30s"}},
            {SetWeight: intPtr(60)},
            {Pause: &k8sCanaryPause{Duration: "30s"}},
        }
    default: // enterprise fallback
        return canaryStepsForPlan("team")
    }
}
```

**TraefikService gerada:**

```yaml
apiVersion: traefik.io/v1alpha1
kind: TraefikService
metadata:
  name: {resName}
  namespace: {ns}
spec:
  weighted:
    services:
      - name: {resName}-stable
        port: {port}
        weight: 100
      - name: {resName}-canary
        port: {port}
        weight: 0
```

**2 Services (stable + canary):**

```yaml
# Stable Service
name: {resName}-stable
selector: {app: resName}  # Argo Rollouts gerencia quais pods são stable

# Canary Service
name: {resName}-canary
selector: {app: resName}  # Argo Rollouts gerencia quais pods são canary
```

**AnalysisTemplate (server/frontend apps only — workers não têm traffic):**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: loopzero-canary-analysis
  namespace: {ns}
spec:
  args:
    - name: service-name
  metrics:
    - name: success-rate
      interval: 30s
      successCondition: "result[0] >= 0.95"
      failureCondition: "result[0] < 0.90"
      inconclusive: "isNaN(result[0])"  # EC-3 fix: first deploy has no traffic → NaN → auto-promote
      failureLimit: 3
      consecutiveErrorLimit: 4  # EC-7 fix: Prometheus down → 4 consecutive errors before abort
      provider:
        prometheus:
          address: {prometheusAddress}
          query: |
            sum(rate(traefik_service_requests_total{service=~"{{args.service-name}}.*",code!~"5.."}[2m]))
            / sum(rate(traefik_service_requests_total{service=~"{{args.service-name}}.*"}[2m]))
```

**EC-3 fix:** `inconclusive: "isNaN(result[0])"` garante que first deploy (sem traffic baseline) auto-promotes em vez de auto-abort. NaN acontece quando denominador é 0 (zero requests).

**EC-14 note:** O `args.service-name` deve ser `{resName}-canary` (não `{resName}`) para medir apenas tráfego canary. O Traefik metric format é `{namespace}-{serviceName}@kubernetesprovider`.

**Worker apps:** Usam Rollout mas com steps simplificados (pause only, sem traffic analysis):
```go
func workerCanarySteps() []k8sCanaryStep {
    return []k8sCanaryStep{
        {Pause: &k8sCanaryPause{Duration: "30s"}},
        // Argo Rollouts verifica pod health automaticamente
    }
}
```

#### Tasks
1. Extrair `buildPodTemplateSpec()` de `buildDeployment()` (compartilhado entre Deployment e Rollout)
2. Implementar `buildRollout()` — gera Rollout CRD com canary strategy
3. Implementar `canaryStepsForPlan()` — retorna steps baseados no plano
4. Implementar `buildStableService()` e `buildCanaryService()` — 2 Services para traffic splitting
5. Implementar `buildTraefikService()` — TraefikService weighted routing
6. Implementar `buildAnalysisTemplate()` — AnalysisTemplate com Prometheus queries (server/frontend only)
7. Workers: Rollout sem TraefikService/AnalysisTemplate

#### TDD
```
RED:     test_build_rollout_starter() — Rollout com plan=starter tem 2 steps (50%, pause 60s)
RED:     test_build_rollout_pro() — Rollout com plan=pro tem 5 steps (20%, pause, analysis, 50%, pause)
RED:     test_build_rollout_team() — Rollout com plan=team tem 7 steps (10%, 30%, 60%)
RED:     test_stable_canary_services() — 2 Services gerados com sufixo -stable e -canary
RED:     test_traefik_service_weights() — TraefikService inicial: stable=100, canary=0
RED:     test_analysis_template_prometheus() — AnalysisTemplate contém query success-rate
RED:     test_worker_rollout_no_traffic() — Worker Rollout sem TraefikService/AnalysisTemplate
RED:     test_feature_flag_false_generates_deployment() — ProgressiveDelivery=false gera Deployment (legacy)
GREEN:   Implementar as 7 funções
REFACTOR: Extrair buildPodTemplateSpec() de buildDeployment() para reutilização
VERIFY:  cd api && go test ./internal/manifests/... -v
```

#### Acceptance Criteria
- [ ] Rollout renderiza com apiVersion `argoproj.io/v1alpha1`
- [ ] Steps corretos para cada plano
- [ ] 2 Services + 1 TraefikService gerados para server/frontend
- [ ] Workers usam Rollout sem traffic routing
- [ ] Feature flag funciona (false → Deployment)
- [ ] `kubeconform --strict` passa em todos os manifests renderizados

#### DoD (Definition of Done)
- [ ] Todos os testes passing
- [ ] `go test` green
- [ ] `kubeconform --strict` green para rendered output

### T1.4 — Atualizar `renderServerApp()`, `renderWorkerApp()`, `renderFrontendApp()`

#### Objective
Integrar `buildRollout()` nos render paths existentes, substituindo `buildDeployment()`.

#### Evidence
- `renderer_helpers.go:114 renderServerApp()` — hoje chama `buildDeployment()`, `buildService()`, `buildIngressResources()`
- `renderer_helpers.go:136 renderWorkerApp()` — hoje chama `buildDeployment()`, `buildPDB()`
- `renderer_helpers.go:146 renderFrontendApp()` — mesma estrutura que server

#### Files to edit
```
api/internal/manifests/renderer_helpers.go — modificar renderServerApp/renderWorkerApp/renderFrontendApp
api/internal/manifests/renderer_ingress.go — IngressRoute aponta para TraefikService (não Service direto)
```

#### Deep file dependency analysis
- **`renderer_helpers.go`** — Orquestra quais recursos são gerados por tipo de app. Mudança: quando `ProgressiveDelivery=true`, usar `buildRollout()` + `buildStableService()` + `buildCanaryService()` + `buildTraefikService()` + `buildAnalysisTemplate()` em vez de `buildDeployment()` + `buildService()`.
- **`renderer_ingress.go`** — IngressRoute hoje aponta para `{resName}` (Service). Mudança: quando progressive delivery, apontar para `{resName}` (TraefikService — mesmo nome, mas Kind diferente). Traefik resolve TraefikService antes de Service.

#### Tasks
1. Modificar `renderServerApp()`: condicional por `ri.ProgressiveDelivery`
2. Modificar `renderWorkerApp()`: mesma condicional
3. Modificar `renderFrontendApp()`: mesma condicional
4. Modificar `buildIngressResources()`: EC-2 fix — adicionar campo `Kind string json:"kind,omitempty"` ao struct `traefikRouteService` em `renderer_types.go`. Quando progressive delivery ativo, setar `Kind: "TraefikService"` e omitir `Port` (TraefikService define ports internamente). Sem o campo `kind`, Traefik resolve como Kubernetes Service (que não existe mais) → 503.
5. EC-9 fix: custom domains IngressRoutes TAMBÉM devem apontar para TraefikService (mesma lógica condicional)
6. Manter path legacy (Deployment) quando feature flag é false

#### TDD
```
RED:     test_render_server_app_with_rollout() — renderServerApp produz Rollout + 2 Services + TraefikService + AnalysisTemplate + IngressRoute + PDB
RED:     test_render_worker_app_with_rollout() — renderWorkerApp produz Rollout + PDB (sem Services/TraefikService/AnalysisTemplate)
RED:     test_render_frontend_app_with_rollout() — igual server
RED:     test_render_server_app_legacy() — ProgressiveDelivery=false produz Deployment + 1 Service (unchanged)
RED:     test_ingress_points_to_traefik_service() — IngressRoute services apontam para TraefikService
GREEN:   Modificar os 3 render functions + buildIngressResources
REFACTOR: Nenhum esperado
VERIFY:  cd api && go test ./internal/manifests/... -v -count=1
```

#### Acceptance Criteria
- [ ] Server/frontend: Rollout + 2 Services + TraefikService + AnalysisTemplate + IngressRoute
- [ ] Worker: Rollout + PDB (sem networking)
- [ ] Legacy path (Deployment) funciona quando flag=false
- [ ] IngressRoute aponta para TraefikService
- [ ] Nenhuma regressão nos manifests existentes quando flag=false
- [ ] Output é determinístico (sorted keys)

#### DoD (Definition of Done)
- [ ] Todos os testes passing
- [ ] `go test` green
- [ ] `kubeconform --strict` green

---

## Phase 2: ArgoCD Configuration

**Objective:** Configurar ArgoCD para ignorar campos que Argo Rollouts modifica em runtime, evitando drift detection false positives.

### T2.1 — Adicionar `ignoreDifferences` no `buildApplicationObject()`

#### Objective
ArgoCD deve ignorar: replicas em Rollout (KEDA/HPA modifica), weights em TraefikService (Argo Rollouts modifica).

#### Evidence
- `real_provisioner.go:163 buildApplicationObject()` — SEM `ignoreDifferences` hoje
- `tenant-app-template.yaml:40` — template Helm TEM `ignoreDifferences` para Deployment replicas
- Argo Rollouts modifica `TraefikService.spec.weighted.services[].weight` em runtime

#### Files to edit
```
operators/internal/argocd/real_provisioner.go — adicionar ignoreDifferences em buildApplicationObject()
infra/helm/argocd/templates/tenant-app-template.yaml — atualizar template de referência
infra/tests/unit/argocd_app_test.go — atualizar testes
```

#### Deep file dependency analysis
- **`real_provisioner.go`** — Constrói Application CR unstructured. Mudança: adicionar `spec.ignoreDifferences` array com 3 entries (Rollout replicas, Deployment replicas legacy, TraefikService weights).
- **`argocd_app_test.go`** — Testa que Application tem ignoreDifferences. Mudança: atualizar assertions para incluir Rollout e TraefikService.

#### Deep Dives

**ignoreDifferences a adicionar:**
```go
"ignoreDifferences": []interface{}{
    // Rollout replicas — KEDA/HPA modify at runtime
    map[string]interface{}{
        "group": "argoproj.io",
        "kind":  "Rollout",
        "jsonPointers": []interface{}{
            "/spec/replicas",
        },
    },
    // Legacy Deployment replicas (feature flag path)
    map[string]interface{}{
        "group": "apps",
        "kind":  "Deployment",
        "jsonPointers": []interface{}{
            "/spec/replicas",
        },
    },
    // TraefikService weights — Argo Rollouts modifies at runtime
    map[string]interface{}{
        "group": "traefik.io",
        "kind":  "TraefikService",
        "jsonPointers": []interface{}{
            "/spec/weighted/services/0/weight",
            "/spec/weighted/services/1/weight",
        },
    },
    // EC-13: Service selectors — Argo Rollouts injects rollouts-pod-template-hash at runtime
    map[string]interface{}{
        "group": "",
        "kind":  "Service",
        "jsonPointers": []interface{}{
            "/spec/selector/rollouts-pod-template-hash",
        },
    },
},
```

#### Tasks
1. Adicionar `ignoreDifferences` em `buildApplicationObject()` spec
2. Atualizar template de referência `tenant-app-template.yaml`
3. Atualizar testes em `argocd_app_test.go`

#### TDD
```
RED:     test_application_ignores_rollout_replicas() — Application CR ignoreDifferences inclui Rollout/spec/replicas
RED:     test_application_ignores_traefik_weights() — ignoreDifferences inclui TraefikService weights
RED:     test_application_ignores_deployment_replicas() — mantém Deployment (legacy compat)
GREEN:   Editar buildApplicationObject()
REFACTOR: Nenhum esperado
VERIFY:  cd operators && go test ./internal/argocd/... -v && cd ../infra && go test ./tests/unit/... -v
```

#### Acceptance Criteria
- [ ] `ignoreDifferences` com 3 entries (Rollout, Deployment, TraefikService)
- [ ] Template de referência atualizado
- [ ] Testes existentes atualizados + novos passam

#### DoD (Definition of Done)
- [ ] `go test` green para operators + infra tests
- [ ] Application CR renderiza com ignoreDifferences correto

---

## Phase 3: KEDA Compatibility

**Objective:** Atualizar HPA e KEDA ScaledObject para referenciar `Rollout` em vez de `Deployment`.

### T3.1 — Atualizar `buildHPA()` e `buildScaledObject()` para Rollout

#### Objective
HPA e KEDA devem target `Rollout` quando progressive delivery está ativo.

#### Evidence
- `renderer_deployment.go:194` — HPA `ScaleTargetRef.Kind: "Deployment"`, `APIVersion: "apps/v1"`
- `renderer_deployment.go:245` — KEDA `ScaleTargetRef` sem Kind (defaults to Deployment)
- Argo Rollouts suporta HPA nativamente (mesmo comportamento que Deployment)
- KEDA `ScaledObject` precisa de `kind: Rollout` explícito

#### Files to edit
```
api/internal/manifests/renderer_deployment.go — condicional em buildHPA() e buildScaledObject()
```

#### Deep file dependency analysis
- **`renderer_deployment.go`** — HPA e ScaledObject. Mudança: quando `ProgressiveDelivery=true`, `ScaleTargetRef.Kind = "Rollout"`, `APIVersion = "argoproj.io/v1alpha1"`. Para KEDA: adicionar `kind` field no `kedaScaleTargetRef`.

#### Tasks
1. Modificar `buildHPA()`: condicional Kind/APIVersion baseado em ProgressiveDelivery
2. Modificar `buildScaledObject()`: adicionar Kind no ScaleTargetRef
3. Adicionar tipo `kedaScaleTargetRef.Kind` e `kedaScaleTargetRef.APIVersion` se necessário
4. `buildHTTPScaledObject()`: EC-1 fix — `ScaleTargetRef.Service` DEVE apontar para `{resName}-stable` (não `{resName}` que não existe mais). `ScaleTargetRef.Name` aponta para o Rollout.
5. EC-5 fix: quando `isScaleToZero=true` E `ProgressiveDelivery=true`, Rollout usa `replicas: 1` (não 0). KEDA gerencia scale-down APÓS rollout completar. Adicionar `scaledobject.keda.sh/transfer-hpa-ownership: "true"` annotation.

#### TDD
```
RED:     test_hpa_targets_rollout() — HPA ScaleTargetRef com Kind=Rollout quando ProgressiveDelivery=true
RED:     test_keda_targets_rollout() — ScaledObject ScaleTargetRef com Kind=Rollout
RED:     test_hpa_targets_deployment_legacy() — HPA com Kind=Deployment quando ProgressiveDelivery=false
GREEN:   Modificar buildHPA() e buildScaledObject()
REFACTOR: Nenhum esperado
VERIFY:  cd api && go test ./internal/manifests/... -run TestHPA -v && go test ./internal/manifests/... -run TestKEDA -v
```

#### Acceptance Criteria
- [ ] HPA `ScaleTargetRef` aponta para Rollout (progressive) ou Deployment (legacy)
- [ ] KEDA ScaledObject com `kind: Rollout` explícito
- [ ] HTTPScaledObject inalterado (usa Service name)
- [ ] Legacy path funciona

#### DoD (Definition of Done)
- [ ] Testes passing
- [ ] `kubeconform --strict` green

---

## Phase 4: CLI + SSE Events

**Objective:** Mostrar canary progress na CLI durante deploy e no status command.

### T4.1 — Adicionar SSE events de canary progress

#### Objective
API emite eventos intermediários durante canary rollout para a CLI consumir.

#### Evidence
- `sse/stream_handler.go:45` — terminal events: `deploy.completed`, `deploy.sync_failed`, etc.
- Não existe evento intermediário para canary steps
- CLI precisa mostrar: "Canary 20% → analyzing... → Canary 50% → Canary 100%"

#### Files to edit
```
api/internal/services/release_activator.go — emitir rollout.step events
api/internal/sse/stream_handler.go — registrar rollout.step como non-terminal event
```

#### Deep file dependency analysis
- **`release_activator.go`** — Emite `deploy.completed`. Mudança: emitir `rollout.step` com `{weight: 20, status: "analyzing"}` durante convergence polling.
- **`stream_handler.go`** — Define terminal events. Mudança: `rollout.step` NÃO é terminal (stream continua).

#### Deep Dives
- **Polling-based:** O `ReleaseConvergenceLoop` (10s polls) é onde canary status seria checado. Quando detecta que o Rollout está em um step intermediário, emite `rollout.step`.
- **Rollout status:** Via ArgoCD ou kubectl direto — verificar `.status.currentStepIndex` e `.status.phase` do Rollout CR.
- **Sem mudança no terminal event:** `deploy.completed` continua sendo o terminal. `rollout.step` são informativos.

#### Tasks
1. Definir event type `rollout.step` com schema: `{weight: int, step: int, total_steps: int, status: string, analysis_status: string}`
2. Registrar em `stream_handler.go` como non-terminal
3. Emitir durante convergence polling quando Rollout está em step intermediário
4. Emitir `rollout.aborted` quando auto-abort ocorre (terminal — alternativo a `deploy.completed`)

#### TDD
```
RED:     test_rollout_step_event_schema() — evento rollout.step tem campos weight, step, total_steps, status
RED:     test_rollout_step_not_terminal() — stream NÃO fecha em rollout.step
RED:     test_rollout_aborted_is_terminal() — stream FECHA em rollout.aborted
GREEN:   Implementar event emission + stream handler registration
REFACTOR: Nenhum esperado
VERIFY:  cd api && go test ./internal/sse/... ./internal/services/... -v
```

#### Acceptance Criteria
- [ ] Event `rollout.step` emitido com weight/step/status
- [ ] Event `rollout.aborted` emitido em auto-abort
- [ ] Stream não fecha em `rollout.step`
- [ ] `deploy.completed` continua como terminal em rollout success

#### DoD (Definition of Done)
- [ ] Testes passing
- [ ] Event schema documentado

### T4.2 — Atualizar CLI `deploy.ts` para mostrar canary progress

#### Objective
Deploy output mostra canary weight progression em tempo real.

#### Evidence
- `cli/src/commands/deploy.ts` — consome SSE events e mostra status
- Hoje: `build.queued → build.started → build.completed → deploy.verifying → deploy.completed`
- Após: `... → rollout.step(20%) → rollout.step(50%) → deploy.completed(100%)`

#### Files to edit
```
cli/src/commands/deploy.ts — consumir rollout.step events
cli/src/renderers/deploy-renderer.ts (ou equivalente) — renderizar canary progress
```

#### Deep file dependency analysis
- **`deploy.ts`** — SSE consumer. Mudança: handler para `rollout.step` que mostra `Canary: 20% → analyzing...` e para `rollout.aborted` que mostra error message + exit code 3.

#### Tasks
1. Adicionar handler para `rollout.step` event
2. Renderizar progress: `⠋ Canary: 20% (step 1/5) — analyzing...`
3. Handler para `rollout.aborted`: mostrar error, exit code 3 (deploy failure)
4. Rule 3: zero internal terms (não mencionar "Rollout", "Argo Rollouts", "TraefikService")
5. Product language: "Canary deployment", "Progressive rollout", "Auto-verification"

#### TDD
```
RED:     test_deploy_shows_canary_progress() — output contém "20%" quando rollout.step event recebido
RED:     test_deploy_shows_abort() — output contém error quando rollout.aborted event recebido
RED:     test_deploy_exit_code_3_on_abort() — exit code 3 em abort
RED:     test_no_internal_terms_in_canary_output() — output não contém "Rollout", "Argo", "TraefikService"
GREEN:   Implementar handlers
REFACTOR: Nenhum esperado
VERIFY:  cd cli && npm test -- --grep "canary"
```

#### Acceptance Criteria
- [ ] Canary weight mostrado em tempo real
- [ ] Abort mostra erro com exit code 3
- [ ] Zero internal terms (Rule 3)
- [ ] `--json` inclui canary data (new fields, non-breaking)

#### DoD (Definition of Done)
- [ ] CLI testes passing
- [ ] Rule 3 clean

### T4.3 — Atualizar CLI `status.ts` para mostrar rollout state

#### Objective
`loopzero status` mostra canary weight atual e analysis status.

#### Evidence
- `cli/src/commands/status.ts` — mostra sync_status, scaling, last build
- Precisa adicionar: `Rollout: 50% canary (step 3/5, analyzing...)` ou `Rollout: stable (100%)`

#### Files to edit
```
cli/src/commands/status.ts — adicionar rollout info
api/internal/routes/ — endpoint status precisa retornar rollout data (se não existe)
```

#### Deep file dependency analysis
- **`status.ts`** — Consome `StatusApiResponse`. Mudança: novo campo `rollout?` no response com `{phase, current_step, total_steps, canary_weight, analysis_status}`.
- **API status route** — Precisa consultar Rollout CR status no workload cluster e retornar dados.

#### Tasks
1. Adicionar campo `rollout` no StatusApiResponse
2. API: consultar Rollout CR `.status.currentStepIndex`, `.status.phase`, `.status.canaryStatus`
3. CLI: renderizar rollout info no output human-readable
4. `--json`: incluir rollout data (new fields, non-breaking — Rule 5)
5. Product language: "Progressive deploy: 50% (step 3/5)" — não "Rollout"

#### TDD
```
RED:     test_status_shows_rollout_phase() — output contém "Progressive deploy" quando rollout ativo
RED:     test_status_shows_stable_when_done() — output contém "Progressive deploy: complete" quando stable
RED:     test_status_json_has_rollout_field() — --json output inclui rollout object
RED:     test_no_internal_terms() — output não contém "Rollout", "Argo"
GREEN:   Implementar
REFACTOR: Nenhum esperado
VERIFY:  cd cli && npm test -- --grep "status"
```

#### Acceptance Criteria
- [ ] `loopzero status` mostra rollout phase e weight
- [ ] `--json` inclui rollout data (non-breaking)
- [ ] Zero internal terms (Rule 3)

#### DoD (Definition of Done)
- [ ] CLI testes passing
- [ ] Rule 3 clean

---

## Phase 5: E2E Validation

**Objective:** Validar end-to-end: deploy → canary → promote → 100% e deploy → canary → abort em bad deploy.

### T5.1 — E2E test: canary happy path

#### Objective
Teste que valida: deploy → canary 20% → analysis pass → promote → 100%.

#### Files to edit
```
infra/tests/e2e/rollout_e2e_test.go (NEW) — Ginkgo E2E test
```

#### Tasks
1. Deploy app de teste com plan=pro
2. Verificar Rollout CR criado com canary strategy
3. Verificar TraefikService com stable=100, canary=0 inicialmente
4. Esperar rollout progressing → verificar weight changes
5. Esperar rollout completed → verificar stable=100

#### TDD
```
RED:     TestE2ERolloutCanaryHappyPath — Ginkgo E2E
GREEN:   Implementar
VERIFY:  cd infra && go test ./tests/e2e/... -run TestE2ERollout -v -timeout 10m
```

#### Acceptance Criteria
- [ ] Rollout completa com canary analysis
- [ ] TraefikService weights progridem corretamente
- [ ] `deploy.completed` SSE event emitido no final

#### DoD (Definition of Done)
- [ ] E2E test green

### T5.2 — E2E test: canary abort on bad deploy

#### Objective
Teste que valida: deploy bad image → canary 20% → analysis fail → auto-abort.

#### Files to edit
```
infra/tests/e2e/rollout_e2e_test.go — adicionar caso de abort
```

#### Tasks
1. Deploy app com imagem que retorna 500 em /health
2. Verificar canary step atinge analysis
3. Verificar analysis fails (success_rate < 95%)
4. Verificar Rollout auto-aborts
5. Verificar stable pods inalterados (traffic 100% stable)
6. Verificar `rollout.aborted` SSE event

#### TDD
```
RED:     TestE2ERolloutCanaryAbort — Ginkgo E2E
GREEN:   Implementar
VERIFY:  cd infra && go test ./tests/e2e/... -run TestE2ERolloutCanaryAbort -v -timeout 10m
```

#### Acceptance Criteria
- [ ] Bad deploy auto-abort sem afetar stable
- [ ] `rollout.aborted` SSE event emitido
- [ ] Stable pods continuam servindo 100% traffic

#### DoD (Definition of Done)
- [ ] E2E test green

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Deploy all-at-once (Deployment) | T1.3, T1.4 | Rollout com canary strategy |
| 2 | Zero traffic splitting | T1.3 (TraefikService) | Weighted routing stable/canary |
| 3 | Rollback reativo | T1.3 (AnalysisTemplate) | Auto-abort via Prometheus analysis |
| 4 | Sem deployment confidence metric | T1.3 (AnalysisTemplate) | success-rate >= 95% measurable |
| 5 | Estratégia por plano | T1.2, T1.3 | Starter(2 steps)/Pro(5)/Team(7) |
| 6 | Argo Rollouts controller | T0.1, T0.2 | Helm chart + Helmfile runtime |
| 7 | ArgoCD drift on weights | T2.1 | ignoreDifferences em Application |
| 8 | ArgoCD drift on replicas (Rollout) | T2.1 | ignoreDifferences para Rollout |
| 9 | KEDA targets Deployment | T3.1 | HPA/ScaledObject target Rollout |
| 10 | CLI no canary visibility | T4.2, T4.3 | rollout.step SSE + status |
| 11 | SSE sem canary events | T4.1 | rollout.step + rollout.aborted |
| 12 | Feature flag for rollback | D8, T1.2 | LOOPZERO_PROGRESSIVE_DELIVERY=false |
| 13 | Prometheus cross-cluster | D6, T1.2 | PrometheusAddress no RenderInput |
| 14 | E2E canary happy path | T5.1 | Ginkgo E2E test |
| 15 | E2E canary abort | T5.2 | Ginkgo E2E test |
| 16 | Worker apps | D4, T1.3 | Rollout sem traffic routing |

**Coverage: 16/16 gaps cobertos (100%)**

## Global Definition of Done

- [ ] Todas as phases (0-5) completadas
- [ ] Todos os testes passing (Go + TypeScript)
- [ ] Zero lint warnings (golangci-lint + ESLint)
- [ ] `task validate:manifests` passa com novos manifests Rollout
- [ ] `kubeconform --strict` passa para Rollout + TraefikService + AnalysisTemplate
- [ ] Feature flag `LOOPZERO_PROGRESSIVE_DELIVERY=false` produz manifests legacy (Deployment)
- [ ] Zero internal terms em CLI output (Rule 3: "Rollout" → "Progressive deploy")
- [ ] **`/cross-validation argo-rollouts-progressive-delivery` PASSED** — zero CRITICO, zero GRAVE
- [ ] **`/dogfood full` PASSED** (ver seção abaixo)

---

## Dogfood Gate (MANDATORY -- NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure (operator must run before dogfood)
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] Argo Rollouts controller running in workload clusters
- [ ] `loopzero deploy` produces Rollout (not Deployment) manifests
- [ ] Canary rollout completes successfully (20% → 50% → 100% for Pro plan)
- [ ] CLI shows canary progress during deploy
- [ ] `loopzero status` shows rollout phase
- [ ] ArgoCD does NOT show drift on TraefikService weights
- [ ] KEDA scale-to-zero still works with Rollout
- [ ] `loopzero rollback` works (reverts Rollout to previous revision)
- [ ] No regressions in previously passing flows
- [ ] Rule 3 clean: zero internal terms in any CLI output
- [ ] Feature flag `LOOPZERO_PROGRESSIVE_DELIVERY=false` produces Deployment manifests

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.

---

## Governance Note

This plan is a **T3 change** (architecture: manifest generation rewrite, new CRDs on workload clusters, deploy pipeline behavior change, CLI output changes, ArgoCD sync semantics change). Per pre-release governance rules, it requires T3 meeting approval with mandatory ADR before implementation. Submit via `/meeting` with scope: deploy + manifests + CLI + ArgoCD + infra.
