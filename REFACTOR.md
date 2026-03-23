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

# 1.1 Matriz de Responsabilidades

> Cada tópico deste plano tem um **Responsável** (organiza, planeja, delega para agentes de implementação) e um **Revisor** (monitora DoDs, padrões, edge cases, boas práticas com rigor FAANG).

## Coordenação Geral

| Papel | Agente | Escopo |
|-------|--------|--------|
| **Coordenador da Refatoração** | `refactor-coordinator` | Sequencia fases, delega, verifica DoDs entre fases |
| **Autoridade Arquitetural** | `principal-architect` | Veta boundaries incorretos, aprova CRDs, garante coerência |

## CRDs (§4)

| CRD | Responsável | Revisor | Notas |
|-----|-------------|---------|-------|
| §4.1 PlatformBootstrap | `operator-engineer` | `k8s-controller-engineer` + `platform-security-engineer` | Envolve credenciais — Security tem veto |
| §4.2 GitRepositoryBinding | `operator-engineer` | `gitops-engineer` + `platform-security-engineer` | Source of truth de repos — GitOps revisor principal |
| §4.3 Tenant | `operator-engineer` | `k8s-controller-engineer` + `distributed-systems-engineer` | Refinar CRD existente, não reescrever |
| §4.4 Environment | `operator-engineer` | `k8s-controller-engineer` + `sre-engineer` | CRD novo — lifecycle isolado de tenant |
| §4.5 Project | `operator-engineer` | `api-domain-architect` + `k8s-controller-engineer` | Boundary API/controller é crítico |
| §4.6 Build | `operator-engineer` | `gitops-engineer` + `distributed-systems-engineer` | State machine spec §5 — convergência |
| §4.7 Release | `operator-engineer` | `gitops-engineer` + `sre-engineer` | GitOps push + health real |
| §4.8 Application | `operator-engineer` | `sre-engineer` + `dx-engineer` | Health real + UX de status |
| §4.9 DNSRecord/ExposedRoute | `operator-engineer` | `sre-engineer` + `dx-engineer` | Reachability real, não assumida |

## Controllers (§5)

| Controller | Responsável | Revisor | Meeting Obrigatória |
|-----------|-------------|---------|---------------------|
| §5.1 PlatformBootstrapController | `operator-engineer` | `platform-security-engineer` + `k8s-controller-engineer` | Sim — Security + Architecture Base |
| §5.2 GitRepositoryBindingController | `operator-engineer` | `gitops-engineer` + `platform-security-engineer` | Sim — Delivery/GitOps + Security |
| §5.3 TenantController v2 | `operator-engineer` | `k8s-controller-engineer` + `distributed-systems-engineer` | Sim — Architecture Base |
| §5.4 EnvironmentController | `operator-engineer` | `k8s-controller-engineer` + `sre-engineer` | Sim — Production & Operability |
| §5.5 ProjectController | `operator-engineer` | `api-domain-architect` + `k8s-controller-engineer` | Sim — Architecture Base |
| §5.6 BuildController | `operator-engineer` | `gitops-engineer` + `distributed-systems-engineer` | Sim — Delivery/GitOps |
| §5.7 ReleaseController | `operator-engineer` | `gitops-engineer` + `sre-engineer` | Sim — Delivery/GitOps + Production |
| §5.8 ApplicationController | `operator-engineer` | `sre-engineer` + `dx-engineer` | Sim — Production & Operability |
| §5.9 DNSController/ExposureController | `operator-engineer` | `sre-engineer` + `dx-engineer` | Sim — Production & Operability |

## Cross-Cutting (§6-§11)

| Tópico | Responsável | Revisor | Agente SDK |
|--------|-------------|---------|------------|
| §6 Estado e health model | `platform-sdk-engineer` | `k8s-controller-engineer` + `sre-engineer` | Cria `pkg/health`, `pkg/conditions` |
| §7 Naming e contracts | `platform-sdk-engineer` | `principal-architect` + `verification-architect` | Cria `pkg/naming`, `pkg/contracts` |
| §8 Pipeline CI/CD | `infra-engineer` | `pipeline-reviewer` (Helena Duarte) + `sre-engineer` | — |
| §9 Destroy/deprovisioning | `operator-engineer` | `sre-engineer` + `platform-security-engineer` | Finalizers + lifecycle |
| §10 Segurança e credenciais | `platform-security-engineer` | `principal-architect` + `sre-engineer` | Threat model obrigatório |
| §11 Observabilidade | `sre-engineer` | `verification-architect` + `principal-architect` | Métricas + logs + eventos |

## Fases de Execução (§12)

| Fase | Responsável | Revisor | Meetings Obrigatórias |
|------|-------------|---------|----------------------|
| Fase 0 — Foundation | `refactor-coordinator` + `platform-sdk-engineer` | `principal-architect` + `verification-architect` | Architecture Base, Quality & Verification |
| Fase 1 — PlatformBootstrap | `refactor-coordinator` → `operator-engineer` | `platform-security-engineer` + `k8s-controller-engineer` | Architecture Base, Security, Production |
| Fase 2 — GitOps Binding | `refactor-coordinator` → `operator-engineer` | `gitops-engineer` + `platform-security-engineer` | Delivery/GitOps, Security |
| Fase 3 — Tenant v2 + Env | `refactor-coordinator` → `operator-engineer` | `k8s-controller-engineer` + `distributed-systems-engineer` | Architecture Base, Production, Quality |
| Fase 4 — App + Exposure | `refactor-coordinator` → `operator-engineer` | `sre-engineer` + `dx-engineer` | Production, Delivery/GitOps, Security |
| Fase 5 — Build/Release | `refactor-coordinator` → `operator-engineer` + `api-engineer` | `gitops-engineer` + `verification-architect` | Delivery/GitOps, Architecture Base, Quality |
| Fase 6 — Deprovisioning | `refactor-coordinator` → `operator-engineer` | `sre-engineer` + `platform-security-engineer` | Production, Security, Distributed Systems |

## Regras da Matriz

1. **Responsável** organiza, planeja e delega — nunca implementa sozinho se o escopo é multi-componente
2. **Revisor** monitora DoDs com rigor — veta se padrões não forem seguidos
3. **Meeting obrigatória** = decisão crítica passa pelo Meeting Review Board antes de implementar
4. **Security/SRE como revisor** = veto absoluto se risco não mitigado
5. **`platform-sdk-engineer`** é consultado sempre que naming, conditions ou health model forem tocados
6. **`refactor-coordinator`** verifica DoD da fase anterior antes de autorizar início da próxima

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

> **Resp:** `operator-engineer` | **Rev:** `k8s-controller-engineer` + `platform-security-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `gitops-engineer` + `platform-security-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `k8s-controller-engineer` + `distributed-systems-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `k8s-controller-engineer` + `sre-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `api-domain-architect` + `k8s-controller-engineer`

Representa um projeto/app lógico do tenant.

### Responsabilidade

Separar projeto de ambiente. Isso evita misturar identidade do software com deployment target.

---

## 4.6 Build

> **Resp:** `operator-engineer` | **Rev:** `gitops-engineer` + `distributed-systems-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `gitops-engineer` + `sre-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `sre-engineer` + `dx-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `sre-engineer` + `dx-engineer`

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

> **Resp:** `operator-engineer` | **Rev:** `platform-security-engineer` + `k8s-controller-engineer`
> **Meeting:** Obrigatória — Comitê Architecture Base + Security & Isolation

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

> **Resp:** `operator-engineer` | **Rev:** `gitops-engineer` + `platform-security-engineer`
> **Meeting:** Obrigatória — Comitê Delivery/GitOps + Security & Isolation

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

> **Resp:** `operator-engineer` | **Rev:** `k8s-controller-engineer` + `distributed-systems-engineer`
> **Meeting:** Obrigatória — Comitê Architecture Base

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

> **Resp:** `operator-engineer` | **Rev:** `k8s-controller-engineer` + `sre-engineer`
> **Meeting:** Obrigatória — Comitê Production & Operability

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

> **Resp:** `operator-engineer` | **Rev:** `api-domain-architect` + `k8s-controller-engineer`
> **Meeting:** Obrigatória — Comitê Architecture Base

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

> **Resp:** `operator-engineer` | **Rev:** `gitops-engineer` + `distributed-systems-engineer`
> **Meeting:** Obrigatória — Comitê Delivery/GitOps

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

> **Resp:** `operator-engineer` | **Rev:** `gitops-engineer` + `sre-engineer`
> **Meeting:** Obrigatória — Comitê Delivery/GitOps + Production & Operability

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

> **Resp:** `operator-engineer` | **Rev:** `sre-engineer` + `dx-engineer`
> **Meeting:** Obrigatória — Comitê Production & Operability

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

> **Resp:** `operator-engineer` | **Rev:** `sre-engineer` + `dx-engineer`
> **Meeting:** Obrigatória — Comitê Production & Operability

### Missão

Garantir que DNS e exposição estejam operacionais.

### Responsabilidades

* criar records
* validar resolução
* publicar IP/hostname efetivo
* detectar drift

---

# 6. Refatoração do estado e health

> **Resp:** `platform-sdk-engineer` | **Rev:** `k8s-controller-engineer` + `sre-engineer`
> Cria `pkg/health` e `pkg/conditions`. Todos os controllers devem usar esses pacotes.

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

> **Resp:** `platform-sdk-engineer` | **Rev:** `principal-architect` + `verification-architect`
> Cria `pkg/naming` e `pkg/contracts`. Nenhum código estrutural fora dessa lib. Testes de edge case obrigatórios.

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

> **Resp:** `infra-engineer` | **Rev:** `pipeline-reviewer` (Helena Duarte) + `sre-engineer`
> Pipeline review obrigatório: determinismo, build/release separation, escala, observabilidade, recovery.

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

> **Resp:** `operator-engineer` | **Rev:** `sre-engineer` + `platform-security-engineer`
> SRE e Security têm veto absoluto. Finalizers devem ter cleanup contract explícito.

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

> **Resp:** `platform-security-engineer` | **Rev:** `principal-architect` + `sre-engineer`
> Threat model obrigatório. Veto absoluto de Security. TTL, rotação e blast radius para toda credencial.

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

> **Resp:** `sre-engineer` | **Rev:** `verification-architect` + `principal-architect`
> Métricas, logs estruturados e eventos K8s obrigatórios por controller. SRE define SLOs.

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

> **Resp:** `refactor-coordinator` + `platform-sdk-engineer`
> **Rev:** `principal-architect` + `verification-architect`
> **Meetings:** Architecture Base, Quality & Verification

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

> **Resp:** `refactor-coordinator` → delega a `operator-engineer`
> **Rev:** `platform-security-engineer` + `k8s-controller-engineer`
> **Meetings:** Architecture Base, Security & Isolation, Production & Operability

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

> **Resp:** `refactor-coordinator` → delega a `operator-engineer`
> **Rev:** `gitops-engineer` + `platform-security-engineer`
> **Meetings:** Delivery/GitOps, Security & Isolation

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

> **Resp:** `refactor-coordinator` → delega a `operator-engineer`
> **Rev:** `k8s-controller-engineer` + `distributed-systems-engineer`
> **Meetings:** Architecture Base, Production & Operability, Quality & Verification

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

> **Resp:** `refactor-coordinator` → delega a `operator-engineer`
> **Rev:** `sre-engineer` + `dx-engineer`
> **Meetings:** Production & Operability, Delivery/GitOps, Security & Isolation

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

> **Resp:** `refactor-coordinator` → delega a `operator-engineer` + `api-engineer`
> **Rev:** `gitops-engineer` + `verification-architect`
> **Meetings:** Delivery/GitOps, Architecture Base, Quality & Verification

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

> **Resp:** `refactor-coordinator` → delega a `operator-engineer`
> **Rev:** `sre-engineer` + `platform-security-engineer`
> **Meetings:** Production & Operability, Security & Isolation, Distributed Systems Review

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

---

# 16. Prompt de Execução

> Use este prompt para iniciar a execução da refatoração com uma LLM usando o sistema de agentes e meetings do LoopZero.

```text
Execute o REFACTOR.md fase a fase (0→6), usando o refactor-coordinator como entry point: para cada fase, (1) leia a seção correspondente do REFACTOR.md e docs/technical.md, (2) convoque /meeting para toda decisão marcada como "Meeting Obrigatória" na matriz de responsabilidades §1.1, (3) delegue implementação ao agente Responsável listado em cada seção, (4) o Revisor listado valida DoDs e veta se padrões não forem seguidos, (5) só avance para a próxima fase quando TODOS os itens do DoD da fase atual estiverem GREEN — comece pela Fase 0 agora.
```

---

# 17. Definition of Done — Refatoração Completa

## 17.1 DoD Global (a refatoração só está DONE quando TODAS forem TRUE)

| # | Critério | Verificação | Agente que valida |
|---|----------|-------------|-------------------|
| G1 | A plataforma sobe sem bootstrap imperativo dependente de `port-forward` | `task dev:up` completa sem port-forward no pipeline | `sre-engineer` |
| G2 | O estado operacional primário vem de `status.conditions`, não de outputs de existência | Grep por `execution` como source of truth → zero ocorrências | `k8s-controller-engineer` |
| G3 | Gitea/ArgoCD bindings são reconciliados declarativamente via CRD | `GitRepositoryBinding` CRD existe e controller reconcilia | `gitops-engineer` |
| G4 | Naming estrutural é centralizado em `pkg/naming` | Grep por `fmt.Sprintf` gerando nomes estruturais fora de `pkg/naming` → zero | `platform-sdk-engineer` |
| G5 | Tenant, Environment, Application, Build e Release têm lifecycle explícito como CRDs | 5 CRDs com spec, status, conditions e observedGeneration | `k8s-controller-engineer` |
| G6 | Delete/deprovision não depende de fallback nuclear | Finalizers com cleanup contracts; deleção testada em e2e | `sre-engineer` + `platform-security-engineer` |
| G7 | O pipeline só provisiona, publica imagens, aplica CRs e verifica saúde | GitHub Actions sem port-forward, sem criação manual de secrets, sem restart | `pipeline-reviewer` |
| G8 | Toda operação crítica é auditável por CR status, logs estruturados e métricas | Métricas reconcile_total, reconcile_errors_total, reconcile_duration_seconds em todos controllers | `sre-engineer` |
| G9 | Nenhum controller tem múltiplas responsabilidades (SRP) | Cada controller reconcilia exatamente um CRD | `principal-architect` |
| G10 | Todo reconcile é idempotente | Teste: rodar reconcile 3x consecutivas → mesmo resultado | `verification-architect` |
| G11 | Conditions são honestas (refletem realidade, não otimismo) | Teste: recurso degradado → condition reflete degradação | `k8s-controller-engineer` |
| G12 | Zero mocks nos testes de controllers | Todos os testes usam envtest, nenhum mock/fake/stub | `verification-architect` |
| G13 | Meetings obrigatórias realizadas para todas as decisões críticas | Meeting records existem em `docs/meetings/records/` para cada controller | `meeting-orchestrator` |
| G14 | ADRs criados para toda divergência Architect vs DistSys | ADRs em `docs/adr/` para decisões com dissent formal | `principal-architect` |

## 17.2 DoD por Fase

### Fase 0 — Foundation

| # | Critério | Teste |
|---|----------|-------|
| F0.1 | `pkg/naming` existe com todas as funções listadas no REFACTOR.md §7.3 | `go test ./pkg/naming/...` passa |
| F0.2 | `pkg/conditions` existe com helpers SetCondition, GetCondition, IsConditionTrue | `go test ./pkg/conditions/...` passa |
| F0.3 | `pkg/health` existe com Phase enum e aggregation | `go test ./pkg/health/...` passa |
| F0.4 | `pkg/contracts` existe com tipos compartilhados | `go test ./pkg/contracts/...` passa |
| F0.5 | Padrão de observabilidade por controller definido (métricas + logs) | Documentado e template disponível |
| F0.6 | Nenhum novo código gera naming fora de `pkg/naming` | Grep confirma |
| F0.7 | Meeting Architecture Base realizada | Record em `docs/meetings/records/` |

### Fase 1 — PlatformBootstrapController

| # | Critério | Teste |
|---|----------|-------|
| F1.1 | CRD `PlatformBootstrap` definido em `operators/api/v1/` | `kubeconform --strict` passa |
| F1.2 | Controller reconcilia: Gitea ready, ArgoCD ready, secrets, seed | Envtest: conditions `GiteaReady`, `ArgoCDReady`, `PlatformSecretsReady`, `DatabaseSeeded`, `BootstrapComplete` |
| F1.3 | Reconcile idempotente (3x sem side effects) | Envtest |
| F1.4 | Pipeline NÃO usa port-forward | Grep no workflow YAML → zero |
| F1.5 | Pipeline NÃO cria secrets manualmente | Grep no workflow YAML → zero |
| F1.6 | Naming vem de `pkg/naming` | Grep no controller → zero `fmt.Sprintf` para nomes estruturais |
| F1.7 | Meetings Security + Architecture Base realizadas | Records existem |

### Fase 2 — GitRepositoryBindingController

| # | Critério | Teste |
|---|----------|-------|
| F2.1 | CRD `GitRepositoryBinding` definido | `kubeconform --strict` passa |
| F2.2 | Controller reconcilia: credentials, repo registration, reachability | Envtest: conditions `CredentialsResolved`, `RepositoryRegistered`, `RepositoryReachable` |
| F2.3 | Finalizer remove registration no consumer ao deletar | Envtest: delete → cleanup |
| F2.4 | Naming de repos usa `pkg/naming` exclusivamente | Grep confirma |
| F2.5 | Nenhum repo é registrado por shell script | Grep no pipeline → zero |
| F2.6 | Meetings Delivery/GitOps + Security realizadas | Records existem |

### Fase 3 — TenantController v2 + EnvironmentController

| # | Critério | Teste |
|---|----------|-------|
| F3.1 | `Environment` CRD definido com lifecycle separado do Tenant | `kubeconform --strict` passa |
| F3.2 | TenantController simplificado: namespaces, quotas, RBAC, network policies | Envtest |
| F3.3 | EnvironmentController: namespace, cluster binding, preview lifecycle | Envtest |
| F3.4 | Reconcile baseado em diff/observedGeneration (não condition-gated) | Envtest: mudança de spec → re-reconcile |
| F3.5 | Patch manual de status NÃO é mecanismo de correção | Grep → zero `status.conditions[].status = ` fora de controller |
| F3.6 | Meetings Architecture Base + Production realizadas | Records existem |

### Fase 4 — Application + Exposure

| # | Critério | Teste |
|---|----------|-------|
| F4.1 | `Application` CRD definido | `kubeconform --strict` passa |
| F4.2 | `ExposedRoute` (ou `DNSRecord`) CRD definido | `kubeconform --strict` passa |
| F4.3 | ApplicationController: hostname via `pkg/naming`, ingress, cert, health | Envtest |
| F4.4 | ExposureController: DNS record, resolução, TLS binding | Envtest |
| F4.5 | Health da app inclui reachability real (não só existência) | Envtest: app down → condition degrades |
| F4.6 | Meetings Production + Security realizadas | Records existem |

### Fase 5 — Build/Release Controllers

| # | Critério | Teste |
|---|----------|-------|
| F5.1 | `Build` CRD definido com state machine spec §5 | `kubeconform --strict` + state machine compliant |
| F5.2 | `Release` CRD definido com state machine spec §5 | `kubeconform --strict` + state machine compliant |
| F5.3 | BuildController: cria Argo Workflow, acompanha status, coleta artifacts | Envtest |
| F5.4 | ReleaseController: gera manifests, commit GitOps, sync ArgoCD, health real | Envtest |
| F5.5 | API delegou estado de build/deploy para CRDs (não é mais única fonte) | API lê status dos CRDs |
| F5.6 | Meetings Delivery/GitOps + Architecture Base realizadas | Records existem |

### Fase 6 — Deprovisioning

| # | Critério | Teste |
|---|----------|-------|
| F6.1 | Finalizers corretos em Tenant, Environment, GitRepositoryBinding, Application | Envtest: delete → cleanup em cascata |
| F6.2 | Deleção por fases: logical → resource cleanup → infra cleanup | Envtest |
| F6.3 | Nuclear fallback NÃO existe no caminho nominal | Grep → zero referências a fallback nuclear no código de produção |
| F6.4 | Delete interrompido → reconcile retoma cleanup corretamente | Envtest: crash mid-delete → resume |
| F6.5 | Meetings Production + Security + Distributed Systems realizadas | Records existem |

## 17.3 Critérios de Rejeição (qualquer um destes BLOQUEIA o DONE)

| # | Bloqueador | Por que é inaceitável |
|---|-----------|----------------------|
| B1 | Qualquer `port-forward` no pipeline | Indica acoplamento imperativo não resolvido |
| B2 | Qualquer naming estrutural via `fmt.Sprintf` fora de `pkg/naming` | Bug de naming vai reaparecer |
| B3 | Qualquer controller com mais de uma responsabilidade | Viola SRP, impossível de testar e operar |
| B4 | Qualquer condition que não reflete realidade observada | Status mentiroso contamina todo o sistema |
| B5 | Qualquer mock/fake/stub em teste de controller | Esconde bugs de integração |
| B6 | Qualquer fase avançada sem DoD da anterior 100% GREEN | Acumula dívida técnica entre fases |
| B7 | Qualquer decisão crítica sem meeting record | Viola governança — decisão sem review cruzado |
| B8 | Veto de Security ou SRE não resolvido | Risco operacional ou de segurança aceito implicitamente |
| B9 | Reconcile não-idempotente em qualquer controller | Viola princípio fundamental — sistema instável |
| B10 | ObservedGeneration incorreto em qualquer controller | Reconciles desnecessários ou perdidos |
