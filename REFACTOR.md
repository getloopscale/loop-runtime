Abaixo está o **plano de refatoração total**, com foco em uma **arquitetura de controllers completa, K8s-native, nível Google**, para transformar o LoopZero de uma plataforma “que funciona com bootstrap imperativo” para uma plataforma **reconciliada, idempotente, observável e operável em produção**. O plano parte diretamente dos problemas documentados: bootstrap imperativo fora do Pulumi, execução baseada em existência de recurso em vez de saúde, uso de `port-forward` no pipeline, drift de naming entre API e controller, destroy com fallback nuclear, e forte dependência de scripts operacionais.  

---

# 1. Objetivo da refatoração

## Meta principal

Substituir a lógica operacional distribuída entre:

* Pulumi
* GitHub Actions
* scripts de bootstrap
* API
* TenantController

por um modelo com:

* **CRDs explícitos**
* **controllers especializados**
* **reconciliação contínua**
* **estado declarativo**
* **health/status real**
* **operações idempotentes e auditáveis**

## Resultado esperado

Ao final, o sistema deve operar assim:

1. Pulumi cria apenas a infraestrutura base e instala os control planes.
2. Um conjunto mínimo de CRs descreve o estado desejado da plataforma.
3. Controllers reconciliam tudo:

   * bootstrap do Gitea
   * registro de repositórios no ArgoCD
   * seed inicial
   * tenants
   * ambientes
   * apps
   * DNS/TLS
   * build orchestration
4. O pipeline deixa de “fazer a plataforma funcionar” e passa apenas a:

   * aplicar infraestrutura
   * publicar imagens
   * aplicar CRs iniciais
5. Toda a plataforma passa a ser controlada por reconciliação, não por scripts lineares.

---

# 2. Princípios arquiteturais obrigatórios

## 2.1 Declarativo por padrão

Nada crítico deve depender de shell script sequencial como fonte de verdade.

## 2.2 Controllers pequenos e especializados

Evitar um “mega operator” com múltiplas responsabilidades. O problema atual do bootstrap mostra por que isso degrada rápido. 

## 2.3 Estado observado separado do estado desejado

Cada CRD precisa diferenciar claramente:

* `spec` = intenção
* `status` = realidade observada

## 2.4 Idempotência total

Toda reconciliação precisa poder rodar N vezes sem efeitos colaterais incorretos.

## 2.5 Reconciliation over scripting

Se algo só funciona quando “rodamos novamente o pipeline”, a arquitetura ainda está errada.

## 2.6 Health real, não existência de recurso

O problema do `execution` atual é que ele tende a marcar “completed” com base em outputs e criação de recurso, sem validar saúde funcional real. 

## 2.7 Single source of truth

Naming, repo names, namespaces, hostnames e bindings precisam sair de uma biblioteca central compartilhada. O bug entre `loopzero-tenant-*` e `*-gitops` é sintoma direto da ausência disso. 

---

# 3. Arquitetura alvo

## 3.1 Camadas

### Camada 1 — Infra Foundation

Responsável apenas por recursos base:

* VPC
* clusters
* DB
* buckets
* registry
* ingress base
* cert-manager
* ArgoCD
* Gitea
* observabilidade
* instalação dos operadores

Aqui continua Pulumi.

### Camada 2 — Platform Control Plane

Responsável por reconciliar a própria plataforma:

* bootstrap de serviços
* credenciais internas
* bindings entre Gitea e ArgoCD
* tenants
* ambientes
* quotas
* GitOps repos
* projetos

Aqui entram CRDs + controllers.

### Camada 3 — Delivery Plane

Responsável por builds, releases e deploys:

* source uploads
* build requests
* workflow de build
* release activation
* GitOps sync
* health verification

Parte pode continuar em API, mas precisa expor CRDs/eventos mais explícitos.

### Camada 4 — Runtime Plane

Responsável por workloads do usuário:

* namespaces tenant/env
* apps
* ingress
* certs
* quotas
* network policies
* status de app

---

# 4. CRDs que devem existir

A seguir está o conjunto mínimo para uma arquitetura realmente reconciliada.

## 4.1 PlatformBootstrap

Representa o bootstrap global da plataforma.

### Responsabilidade

Descrever o estado desejado do control plane:

* Gitea admin/token/bootstrap
* ArgoCD auth material
* secrets operacionais
* registro de repositórios base
* seed inicial da plataforma
* health gates iniciais

### Exemplo conceitual

```yaml
apiVersion: platform.loopzero.io/v1alpha1
kind: PlatformBootstrap
metadata:
  name: main
spec:
  gitea:
    enabled: true
    adminUserSecretRef: gitea-admin
    createOrg: loopzero
  argocd:
    enabled: true
    registerDefaultRepos: true
  database:
    seedSystemRecords: true
  secrets:
    createPlatformTokens: true
status:
  phase: Ready
  conditions: []
```

---

## 4.2 GitRepositoryBinding

Vincula um repositório Git a um consumer interno.

### Responsabilidade

Eliminar a lógica manual de criar `repo-creds`, `repository` secrets e restart de ArgoCD. Hoje isso está no pipeline.  

### Uso

* registrar repo no ArgoCD
* garantir credenciais
* forçar refresh seguro quando necessário
* manter consistência de URL/nome

---

## 4.3 Tenant

Você já tem esse CRD. Ele deve continuar, mas precisa ser afinado.

### Responsabilidade refinada

* identidade do tenant
* limites
* ambientes
* políticas default
* GitOps settings
* preview settings

Hoje ele já aciona namespace, repo, quotas, network policies, app project etc. 

O problema é que ele ainda está cercado por bootstrap e naming inconsistentes. 

---

## 4.4 Environment

Representa um ambiente de tenant de forma explícita.

### Por que criar

Hoje environment está embutido dentro do Tenant. Isso dificulta:

* lifecycle isolado
* rollout por ambiente
* health isolado
* suspender/destruir um ambiente sem tocar no tenant inteiro

### Exemplo

```yaml
apiVersion: platform.loopzero.io/v1alpha1
kind: Environment
metadata:
  name: tn-loopzero-production
spec:
  tenantRef: tn-loopzero
  clusterRef: runtime
  quotaProfile: starter
  preview:
    enabled: true
    ttlHours: 24
status:
  phase: Ready
```

---

## 4.5 Project

Representa um projeto/app lógico do tenant.

### Responsabilidade

Separar projeto de ambiente. Isso evita misturar identidade do software com deployment target.

---

## 4.6 Build

Representa um pedido de build reconciliável.

### Problema atual

Hoje o fluxo é muito API-driven:

* upload
* create build
* polling workflow
* release activation 

### Melhor modelo

A API pode continuar iniciando, mas o estado deve ser materializado como um CR `Build`.

---

## 4.7 Release

Representa uma release promovível.

### Responsabilidade

* referência a build
* referência a project/environment
* imagens
* manifests
* git commit GitOps
* sync health
* activation state

---

## 4.8 Application

Representa a app do tenant no runtime.

### Responsabilidade

* source of truth do hostname
* service mapping
* desired replicas
* ingress
* tls
* health summary

Isso reduz a fragilidade atual de geração indireta de hostname e manifests. O bug do hostname excessivamente profundo prova a necessidade de centralizar essa regra. 

---

## 4.9 DNSRecord ou ExposedRoute

Representa exposição pública desejada.

### Por que vale a pena

Hoje o `expose.ts` resolve parte do DNS da plataforma, mas a exposição de apps depende da renderização de manifests e wildcard. 

Ter um recurso explícito permite:

* health de resolução DNS
* checagem de propagação
* binding claro com TLS

---

# 5. Controllers necessários

## 5.1 PlatformBootstrapController

### Missão

Substituir o bootstrap imperativo do pipeline.

### Responsabilidades

* validar presença de Gitea e ArgoCD
* gerar/validar credenciais necessárias
* criar secrets internos da plataforma
* registrar repos default
* semear registros iniciais no banco
* publicar `status.conditions`

### Não deve fazer

* build de apps
* deploy de tenant apps
* destroy de infra

### Conditions mínimas

* `GiteaReady`
* `ArgoCDReady`
* `PlatformSecretsReady`
* `DatabaseSeeded`
* `BootstrapComplete`

---

## 5.2 GitRepositoryBindingController

### Missão

Gerenciar integração Git <-> ArgoCD de forma declarativa.

### Responsabilidades

* validar token/credencial
* criar Secret `repository`
* criar fallback `repo-creds` se necessário
* acionar refresh seguro/reload sem restart bruto sempre
* atualizar status de reachability

### Conditions

* `CredentialsResolved`
* `RepositoryRegistered`
* `RepositoryReachable`
* `ConsumerHealthy`

---

## 5.3 TenantController v2

### Missão

Continuar sendo o reconciliador principal de tenancy, mas com responsabilidades reduzidas e mais explícitas.

### Responsabilidades

* garantir namespaces corretos
* quotas
* limit ranges
* service accounts
* network policies
* app project base
* repo provision request

### O que deve sair dele

* naming inventado localmente
* decisões de bootstrap global
* patches manuais como mecanismo operacional normal

O documento mostra condition-gated reconciliation que impede correção de recursos já marcados como prontos. Isso precisa ser refeito. 

### Mudança obrigatória

Trocar “skip se condition=True” por:

* diff-based reconciliation
* hash/version-based reconciliation
* observedGeneration respeitado corretamente

---

## 5.4 EnvironmentController

### Missão

Gerenciar ambientes de forma isolada.

### Responsabilidades

* namespace por env
* cluster binding
* defaults de quota/policy
* preview lifecycle
* destruction segura do env

---

## 5.5 ProjectController

### Missão

Gerenciar identidade e configurações do projeto.

### Responsabilidades

* repo source metadata
* build policy
* deploy strategy
* allowed environments
* naming canonical da app

---

## 5.6 BuildController

### Missão

Transformar um `Build` spec em workflow real.

### Responsabilidades

* criar Workflow do Argo
* acompanhar status
* coletar outputs
* produzir artifacts de build
* atualizar status
* gerar evento para Release

### Conditions

* `WorkflowSubmitted`
* `SourcesResolved`
* `BuildSucceeded`
* `ArtifactsPublished`

---

## 5.7 ReleaseController

### Missão

Transformar uma build em release ativa.

### Responsabilidades

* gerar manifests
* fazer commit no repo GitOps correto
* registrar metadata da release
* acionar sync do ArgoCD
* aguardar health real
* promover `READY -> ACTIVE`

Esse fluxo hoje está espalhado entre API e ArgoCD. 

---

## 5.8 ApplicationController

### Missão

Garantir que uma app do tenant esteja exposta e saudável.

### Responsabilidades

* construir hostname via lib canônica
* service discovery
* cert requests
* ingress
* app health
* rollout summary

### Conditions

* `ServiceResolved`
* `IngressReady`
* `CertificateIssued`
* `RouteReachable`
* `ApplicationHealthy`

---

## 5.9 DNSController ou ExposureController

### Missão

Garantir que DNS e exposição estejam operacionais.

### Responsabilidades

* criar records
* validar resolução
* publicar IP/hostname efetivo
* detectar drift

---

# 6. Refatoração do estado e health

## 6.1 Matar o `execution` atual como fonte primária de verdade

O `execution` pode continuar existindo como resumo, mas não pode mais ser o principal indicador de readiness, porque ele ainda está acoplado demais à criação de recursos. 

## 6.2 Novo modelo de status

Cada controller deve publicar:

* `phase`
* `conditions`
* `observedGeneration`
* `lastSuccessfulReconcileTime`
* `lastTransitionTime`
* `reason`
* `message`

## 6.3 Health aggregator

Criar um **PlatformHealthAggregator** que consolida:

* bootstrap
* Git providers
* ArgoCD reachability
* clusters
* TLS
* DNS
* tenant health
* build plane
* runtime plane

### Saída desejada

```json
{
  "platform": "Degraded",
  "bootstrap": "Healthy",
  "gitops": "Healthy",
  "runtime": "Degraded",
  "dns": "Healthy",
  "tls": "Healthy",
  "lastUpdatedAt": "..."
}
```

---

# 7. Biblioteca central de naming e contracts

## 7.1 Problema que precisa morrer

Bug de naming entre API e controller. 

## 7.2 Ação obrigatória

Criar uma biblioteca única, por exemplo:

* `libs/naming`
* `libs/contracts`
* `libs/identity`

## 7.3 Tudo deve sair dela

* tenant ID
* repo names
* namespace names
* hostnames
* certificate secret names
* ArgoCD app names
* labels/annotations
* bucket paths
* release IDs

## 7.4 Regra dura

Nenhum controller, API ou CLI pode montar string estrutural via `fmt.Sprintf` solto fora dessa biblioteca.

---

# 8. Refatoração do pipeline CI/CD

## 8.1 O que sai do pipeline

Remover do GitHub Actions:

* port-forward para Gitea
* port-forward para ArgoCD
* geração manual de JWT
* criação manual de secrets operacionais
* restart manual de componentes
* apply manual de bootstrap lógico
* lógica procedural de seed como fonte principal

Tudo isso hoje está documentado como parte essencial do bootstrap. 

## 8.2 O que fica no pipeline

### Step A — Validate

* lint
* tests
* typecheck

### Step B — Infra Apply

* pulumi up infra foundation

### Step C — Images Publish

* build/push de imagens da plataforma

### Step D — CR Apply

* aplicar ou atualizar `PlatformBootstrap`
* aplicar configs centrais
* opcionalmente aplicar `Tenant` bootstrap

### Step E — Verify

* esperar conditions via kubectl/api
* falhar se health gates não passarem

## 8.3 Sem `port-forward`

Qualquer integração interna deve ocorrer:

* por DNS interno de service
* por Jobs no cluster
* por client in-cluster

---

# 9. Refatoração do destroy/deprovisioning

## 9.1 Estado atual

O fluxo atual depende de limpeza agressiva, remoção de finalizers, deleção manual via API e fallback nuclear no state. 

## 9.2 Novo princípio

Destroy deve ser **reconciliado**, não “forçado”.

## 9.3 Introduzir deleção por fases

### Fase 1 — Logical deprovision

* marcar tenant/environment/project para deleção
* controllers removem apps, routes, certs, repos bindings

### Fase 2 — Resource cleanup

* controllers garantem namespace vazio
* removem AppProjects, Applications, quotas, secrets associados

### Fase 3 — Infra cleanup

* Pulumi remove recursos base se ambiente inteiro for desprovisionado

## 9.4 Finalizers corretos

Cada CRD relevante precisa de finalizers específicos, com responsabilidades limitadas.

### Exemplo

* `Tenant` finalizer: garantir remoção de envs/projetos
* `Environment` finalizer: garantir remoção de namespace e app bindings
* `GitRepositoryBinding` finalizer: remover registration no consumer

## 9.5 Eliminar nuclear fallback como operação normal

Pode existir runbook de desastre extremo, mas não pode ser parte do desenho padrão.

---

# 10. Segurança e credenciais

## 10.1 Problema atual

Tokens de Gitea e ArgoCD ainda são tratados no pipeline de forma excessivamente operacional.  

## 10.2 Modelo alvo

### Credenciais de plataforma

Gerenciadas por controller de bootstrap.

### Tokens de integração

* curtos
* rotacionáveis
* referenciados por SecretRef
* nunca trafegados como passo central da lógica no GitHub Actions

## 10.3 Melhorias obrigatórias

* suporte a rotação
* status de validade
* distinção entre credential desired e effective credential
* eventos claros de expiração/reconciliação

---

# 11. Observabilidade nível Google

## 11.1 Logs estruturados por reconcile

Todo reconcile precisa logar:

* controller
* kind
* namespace/name
* generation
* reconcile_id
* result
* duration_ms
* retries
* changed_resources_count

## 11.2 Métricas obrigatórias

* reconcile_total
* reconcile_errors_total
* reconcile_duration_seconds
* condition_transitions_total
* finalizer_duration_seconds
* drift_detected_total

## 11.3 Eventos K8s úteis

Publicar events claros:

* `BootstrapReady`
* `RepositoryRegistered`
* `TenantEnvironmentProvisioned`
* `ReleaseActivated`
* `RouteCertificateIssued`

---

# 12. Roadmap de execução da refatoração

## Fase 0 — Foundation de refatoração

Objetivo: preparar base sem quebrar o sistema

### Entregas

* biblioteca central de naming/contracts
* padrão de conditions/status
* SDK interno para controllers
* health model comum
* observabilidade base

### DoD

* nenhum novo código estrutural gera naming fora da lib central
* todos os novos controllers usam o mesmo pacote de status/conditions
* métricas e logs padrão disponíveis

---

## Fase 1 — Platform Bootstrap Controller

Objetivo: matar bootstrap imperativo

### Entregas

* CRD `PlatformBootstrap`
* controller correspondente
* seed idempotente
* secrets operacionais gerenciados pelo controller
* validação de readiness de Gitea e ArgoCD
* pipeline reduzido para aplicar CR

### DoD

* GitHub Actions não usa `port-forward`
* GitHub Actions não cria manualmente secrets do bootstrap
* bootstrap completo acontece por reconciliação
* readiness do bootstrap visível via `status.conditions`

---

## Fase 2 — GitOps Binding Controller

Objetivo: tornar Gitea/ArgoCD declarativo

### Entregas

* CRD `GitRepositoryBinding`
* controller de registro de repo
* refresh/reload controlado
* validação de reachability
* correção definitiva de naming

### DoD

* nenhum repo é registrado por shell script
* repo registration é reconciliado
* mismatch de repo naming é impossível por contrato

---

## Fase 3 — TenantController v2 + Environment CRD

Objetivo: separar tenancy de environment lifecycle

### Entregas

* `Environment` CRD
* split claro entre tenant e env
* TenantController simplificado
* EnvironmentController
* reconcile baseado em diff/observedGeneration

### DoD

* patch manual de status não é mais mecanismo normal de correção
* tenant e env possuem phases e conditions próprias
* namespace/RBAC/network policy são reconciliados corretamente mesmo após fixes

---

## Fase 4 — Application + Exposure model

Objetivo: tornar apps, DNS e TLS objetos declarativos de primeira classe

### Entregas

* `Application` CRD
* `ExposedRoute` ou `DNSRecord` CRD
* ApplicationController
* DNS/ExposureController
* health de TLS/DNS/route real

### DoD

* hostname vem da lib canônica
* TLS e route possuem status próprio
* health da app inclui reachability real

---

## Fase 5 — Build/Release controllers

Objetivo: formalizar delivery plane

### Entregas

* `Build` CRD
* `Release` CRD
* BuildController
* ReleaseController
* integração mais explícita com Argo Workflows e ArgoCD

### DoD

* build e release têm phases e conditions próprias
* API deixa de ser a única fonte do estado de build/deploy
* release activation é auditável por CR status

---

## Fase 6 — Deprovisioning correto

Objetivo: eliminar destroy agressivo como modo normal

### Entregas

* finalizers corretos
* delete lifecycle por CR
* cleanup reconciliado
* runbooks de desastre separados do fluxo padrão

### DoD

* sem nuclear fallback no caminho nominal
* tenant/env/app deletion é previsível e observável
* Pulumi destroy fica restrito à camada base

---

# 13. Estrutura de repositório recomendada

## Operators

```text
operators/
  pkg/
    conditions/
    reconcile/
    naming/
    identity/
    events/
    metrics/
    finalizers/
    clients/
  controllers/
    platformbootstrap/
    gitrepositorybinding/
    tenant/
    environment/
    application/
    build/
    release/
    exposure/
  apis/
    platform/v1alpha1/
```

## Shared libs

```text
libs/
  naming/
  contracts/
  health/
  status/
```

## API

A API deve consumir os mesmos contratos, não reinventá-los.

---

# 14. Critérios de arquitetura para aceitar a refatoração

A refatoração só deve ser considerada concluída quando estas afirmações forem verdadeiras:

1. A plataforma sobe sem bootstrap imperativo dependente de `port-forward`. 
2. O estado operacional primário vem de `status.conditions`, não de outputs de existência. 
3. Gitea/ArgoCD bindings são reconciliados declarativamente. 
4. Naming estrutural é centralizado e compartilhado. 
5. Tenant, environment, app, build e release têm lifecycle explícito.
6. Delete/deprovision não depende de fallback nuclear como caminho nominal. 
7. O pipeline só provisiona, publica imagens, aplica CRs e verifica saúde.
8. Toda operação crítica é auditável por CR status, logs e métricas.

---

# 15. Veredito final da refatoração proposta

A mudança correta não é “melhorar o bootstrap”.

A mudança correta é:

> **remover o bootstrap como conceito operacional central**
> e substituí-lo por **controladores reconciliadores de plataforma**

Esse é o salto que separa:

* uma infra avançada com scripts fortes
  de
* uma plataforma verdadeiramente operável em produção

No estado atual, sua base de infra é boa, mas o control plane ainda não está no nível exigido para uma PaaS madura. Os dois documentos deixam isso muito claro: o sistema já aprendeu lições valiosas, mas elas ainda estão materializadas em procedimentos e exceções, não em arquitetura permanente.  
