# Sprint 1 — Roadmap Executável

> **Princípio organizador:** prova de convergência operacional primeiro, ergonomia depois.
>
> O sprint só está completo quando existe um fluxo vertical executável:
> `CLI → API → persistência → Tenant CR → TenantController reconcilia → objetos observáveis → status consistente`
>
> **Weeks:** 3-5 · **Tasks:** 24 · **Fases:** 3

---

## Decisão arquitetural: modelo de autenticação

**Spec §16 define explicitamente:** LoopZero v1 usa GitHub OAuth token diretamente. Não existe sessão LoopZero, não existe token LoopZero-issued, não existe camada de abstração entre GitHub e a API.

```
Fluxo:
  1. CLI obtém GitHub OAuth token (browser flow ou --token)
  2. CLI armazena token localmente em ~/.loopzero/config.json (0600)
  3. CLI envia token em Authorization: Bearer {token} para API
  4. API valida token chamando GitHub API (GET /user) com cache de 5 min
  5. API resolve user → tenant binding via DB (tabela tenant_members)
```

**Implicações:**
- Não existe login no lado da API — apenas validação de token GitHub
- Revogação = revogar o token no GitHub
- Scope de tenant é resolvido pela API consultando `tenant_members`, não pelo token
- Build credentials são separadas (ephemeral, 1h TTL, geradas pela API por build)
- GitHub token scope mínimo: `read:user` + `read:org` (leitura de perfil e orgs)
- Rate limiting: API cacheia validação GitHub por 5 min para evitar throttling

---

## Decisão arquitetural: seleção de tenant (multi-tenant auth)

**Problema:** Um usuário pode pertencer a múltiplos tenants (normal em SaaS B2B). Sem definição explícita, o middleware injetaria um tenant arbitrário, causando operações no tenant errado.

**Decisão para v1: Modelo C — seleção implícita quando único, explícita quando múltiplo.**

```
Cenário 1: usuário pertence a 1 tenant
  → API resolve automaticamente
  → CLI mostra: "Authenticated as lucas@acme.dev (team: acme)"
  → Nenhuma ação adicional necessária

Cenário 2: usuário pertence a N tenants
  → API retorna 409 com lista de tenants disponíveis se X-LoopZero-Tenant header ausente
  → CLI mostra:
      "You belong to multiple teams:
        1. acme
        2. bigcorp
      Run: loopzero login --team acme"
  → CLI armazena team selecionado no config.json
  → Toda request subsequente envia header X-LoopZero-Tenant: {selected-team}

Cenário 3: usuário não pertence a nenhum tenant
  → API retorna 403 "No team found. Ask a team admin to invite you."
  → CLI mostra mensagem com next action
```

**Implementação no middleware:**
```go
func resolveTeant(ctx context.Context, userID string, requestedTenant string) (string, error) {
    memberships := db.GetTenantMemberships(userID)

    switch len(memberships) {
    case 0:
        return "", LoopZeroError{Code: "NO_TENANT", Status: 403}
    case 1:
        return memberships[0].TenantID, nil
    default:
        if requestedTenant == "" {
            return "", LoopZeroError{Code: "TENANT_REQUIRED", Status: 409,
                Details: memberships} // retorna lista para CLI
        }
        // Verificar que requestedTenant está na lista
        for _, m := range memberships {
            if m.TenantID == requestedTenant {
                return m.TenantID, nil
            }
        }
        return "", LoopZeroError{Code: "TENANT_NOT_FOUND", Status: 403}
    }
}
```

**No CLI (config.json):**
```json
{
  "token": "gho_xxxx",
  "email": "lucas@acme.dev",
  "team": "acme"
}
```

- `loopzero login` → se 1 tenant, seta team automaticamente
- `loopzero login` → se N tenants, pede seleção (ou `--team` flag)
- `loopzero login --team bigcorp` → seta team explicitamente
- Toda request CLI envia `X-LoopZero-Tenant: {team}` se presente no config

---

## Decisão arquitetural: naming convention

**Padrão Go para IDs:** `tenantID`, `projectID`, `buildID` (camelCase com acrônimo maiúsculo, per Go conventions).

- Em código Go: `tenantID` (nunca `tenantId`, `tenant_id`, ou `TENANT_ID`)
- Em YAML/JSON: `tenantId` (camelCase, per Kubernetes convention)
- Em SQL: `tenant_id` (snake_case, per PostgreSQL convention)
- Em env vars: `TENANT_ID` (SCREAMING_SNAKE, per Unix convention)
- Em namespace names: `{tenantID}-{env}` (lowercase, per RFC 1123)

Cada contexto usa a convenção do seu ecossistema. Nenhuma mistura dentro do mesmo contexto.

---

## Decisão arquitetural: namespace ownership

**Problema:** Kubernetes não permite cross-namespace owner references. O Tenant CR é namespaced (e.g., `loopzero-system`), mas os namespaces que ele cria são cluster-scoped. Setar ownerReference em um namespace apontando para um resource em outro namespace é silenciosamente ignorado pelo GC.

**Solução: Finalizer pattern com verify-then-remove.**

Namespace deletion NÃO é síncrona. `client.Delete()` apenas marca o namespace para deletion — ele pode ficar `Terminating` indefinidamente (finalizers de outros recursos, CRDs órfãos, webhooks). Se o finalizer do Tenant for removido imediatamente após emitir `Delete()`, o controle sobre o cleanup é perdido.

```go
// No primeiro reconcile:
controllerutil.AddFinalizer(tenant, "loopzero.io/tenant-cleanup")

// No reconcile, se tenant.DeletionTimestamp != nil:
//   Fase 1: EMITIR deletes (idempotente)
//   1. Listar namespaces com label loopzero.io/tenant={tenantID}
//   2. Para cada namespace existente: client.Delete() (ignorar NotFound)
//   3. Chamar GitRepoProvisioner.ArchiveRepo() (soft delete)
//
//   Fase 2: VERIFICAR que recursos realmente sumiram
//   4. Listar namespaces com label loopzero.io/tenant={tenantID} novamente
//   5. Se lista NÃO vazia → requeue (não remover finalizer)
//   6. Se lista VAZIA → remover finalizer → Kubernetes completa a deletion
controllerutil.RemoveFinalizer(tenant, "loopzero.io/tenant-cleanup")
```

**Regra:** o finalizer só é removido quando os namespaces **não forem mais encontrados** pelo label selector. O `Delete()` é apenas intenção, não conclusão.

Todos os recursos criados pelo controller recebem label `loopzero.io/tenant={tenantID}` para lookup eficiente.

---

## Decisão arquitetural: egress network model

**Problema:** `0.0.0.0/0 except metadata` permite egress para internet inteira. Em multi-tenant, isso é risco: crypto mining, network scanning, abuse.

**Decisão para Sprint 1:** egress restrito (modelo B — allow-list).

```yaml
# Workload namespaces: egress permitido para:
- DNS (kube-system:53)
- Intra-namespace (pods do mesmo tenant)
- API server (para service discovery)
# Internet: BLOQUEADO por default

# Se o tenant precisa de egress externo:
# → Será configurável via Tenant.Spec.EgressPolicy em sprint futuro
# → Egress gateway (Istio/Cilium) para controle granular
```

**IMPORTANTE — limitação funcional v1, não apenas detalhe de segurança:**

Muitos apps reais precisam de egress externo: Stripe, GitHub APIs, SMTP, webhooks, bancos externos, observabilidade third-party. Com internet bloqueada, esses apps vão falhar silenciosamente após deploy.

Isso é **aceitável para v1** desde que seja explícito:
- Documentar no README: "v1 não suporta egress externo por default"
- `loopzero deploy` deve avisar se `loopzero.yaml` declarar integrações externas (sprint futuro)
- Sprint 3+ implementará `Tenant.Spec.EgressPolicy` com allow-list configurável por tenant
- Egress gateway (Istio/Cilium) será avaliado quando egress configurável entrar no roadmap

Tenants que precisam de internet terão configuração explícita. Até lá, o bloqueio é a escolha correta para segurança multi-tenant.

---

## Decisão arquitetural: PriorityClass — global por plano, fora do controller

**Problema 1:** PriorityClass por plan tier como runtime causa preemption entre tenants = violação de SLA.
**Problema 2:** PriorityClass é cluster-scoped e compartilhada por plano. Criar no TenantController e tentar cleanup por tenant é inconsistente — deletar quando um tenant starter sai remove a classe para TODOS os starters.

**Decisão:** PriorityClasses são **infra global de cluster**, não lifecycle de tenant.

```yaml
# Criadas uma vez no bootstrap do cluster (task setup ou Helm chart)
# NÃO criadas pelo TenantController
# NÃO deletadas quando um tenant é removido

apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: loopzero-build-priority-starter
value: 100
preemptionPolicy: Never
globalDefault: false
---
kind: PriorityClass
metadata:
  name: loopzero-build-priority-growth
value: 200
preemptionPolicy: Never
---
kind: PriorityClass
metadata:
  name: loopzero-build-priority-enterprise
value: 300
preemptionPolicy: Never
```

**O que muda no TenantController:**
- Controller NÃO cria PriorityClass
- Controller NÃO deleta PriorityClass no cleanup
- Builds futuros (Sprint 2+) referenciam a PriorityClass pelo nome baseado no plan tier do Tenant
- `preemptionPolicy: Never` garante que builds enterprise NÃO evictam workloads
- Runtime workloads de todos os tenants usam a mesma prioridade (isolamento via ResourceQuota)

**O que muda no Sprint 0/infra:**
- PriorityClasses devem ser criadas como parte do `task setup` ou incluídas no Helm chart da infra

---

## Decisão arquitetural: GitOps repo — por tenant, não por projeto

**Spec §7.3.1 define:** o TenantController cria UM repo GitOps por tenant (`loopzero-tenant-{tenantID}`).

**Esclarecimento:** O repo é do **tenant**, não do projeto. Múltiplos projetos do mesmo tenant compartilham o mesmo repo GitOps, organizados por diretório:

```
loopzero-tenant-acme/
├── my-saas/
│   ├── production/
│   │   ├── api/deployment.yaml
│   │   └── web/deployment.yaml
│   └── staging/
│       ├── api/deployment.yaml
│       └── web/deployment.yaml
└── other-project/
    └── production/
        └── service/deployment.yaml
```

**Implicações:**
- TenantController é responsável por criar o repo (lifecycle de tenant)
- Build pipeline (Sprint 2+) é responsável por escrever manifests dentro do repo (lifecycle de deploy)
- Isolamento entre projetos: ArgoCD Application por `{project}/{environment}` aponta para subdiretório
- Blast radius de commit errado: limitado ao tenant (não cross-tenant)
- Locks por projeto: já resolvido pelo `pg_advisory_xact_lock(tenant:project:env)` na API

**Por que não por projeto:**
- Um repo por projeto multiplicaria repos por 10x+ (1000 tenants × 5 projetos = 5000 repos)
- Gitea performance degrada com milhares de repos
- ArgoCD Application management fica mais complexo
- Repo por tenant é o padrão de mercado (Flux, ArgoCD docs, Crossplane)

---

## Decisão arquitetural: limite de tenants por cluster

**Limite inicial documentado:**

```
maxTenantsPerCluster: 500
```

**Justificativa:** com 500 tenants × 2 environments = ~1500 namespaces (incluindo build), dentro do range confortável do Kubernetes. Acima disso, sharding de clusters é necessário.

**Implicações para Sprint 1:**
- Não precisa de sharding agora
- Controller deve ter metrics (reconcile duration, queue depth) para monitorar crescimento
- Quando atingir ~300 tenants, planejar Sprint para cluster sharding

---

## Índice

- [Fase 1 — Prova do Control Plane](#fase-1--prova-do-control-plane) (sistema converge)
- [Fase 2 — CLI Foundation + Auth](#fase-2--cli-foundation--auth) (ergonomia)
- [Fase 3 — Vertical Slice](#fase-3--vertical-slice) (tudo conectado)
- [Grafo de Dependências](#grafo-de-dependências)
- [Exit Gate](#exit-gate)

---

## Fase 1 — Prova do Control Plane

> **Objetivo:** provar que Tenant CR → reconciliação → objetos Kubernetes observáveis funciona.
> Esta fase é a prioridade absoluta do sprint. Sem ela, CLI e API são fachada.

### Contrato do TenantController

Antes de qualquer task, o contrato está definido aqui. Cada step do reconcile é uma task separada.

```yaml
Input:
  apiVersion: loopzero.io/v1
  kind: Tenant
  spec:
    displayName: "Acme Corp"
    plan: starter | growth | enterprise
    environments:
      - name: production
        cluster: workload
      - name: staging
        cluster: workload
    gitops:
      repoName: "loopzero-tenant-acme"
      branch: "main"
    preview:
      maxEnvironments: 3
      ttlHours: 48
    limits:
      maxCPU: "8"
      maxMemory: "16Gi"
      maxStorage: "50Gi"
      maxBuildConcurrency: 2

Output (objetos criados por reconcile — idempotente):
  1. Gitea repo: {spec.gitops.repoName} com branch {spec.gitops.branch}
  2. Per environment namespace: {tenantID}-{environment} no workload cluster
     Labels: loopzero.io/tenant={tenantID}, loopzero.io/environment={env}
  3. Per namespace ResourceQuota: cpu/memory/storage do spec.limits
  4. Per namespace LimitRange: defaults razoáveis (cpu: 100m/500m, mem: 128Mi/512Mi)
  5. Per namespace ServiceAccount: loopzero-deployer
  6. Per namespace Role: permissões de deploy (variável por plan tier)
  7. Per namespace RoleBinding: loopzero-deployer → Role
  8. Per namespace NetworkPolicy: deny-all + allow-list
     (Traefik ingress, DNS, intra-namespace, API server — internet BLOQUEADA)
  9. Build namespace: build-{tenantID} com ResourceQuota (maxBuildConcurrency)
     Labels: loopzero.io/tenant={tenantID}, loopzero.io/component=build
  NOTA 1: PriorityClasses (loopzero-build-priority-{plan}) são infra global de cluster,
  criadas no bootstrap (task setup), NÃO pelo TenantController. Builds referenciam
  a classe pelo plan tier do tenant. Ver decisão arquitetural acima.

  NOTA 2: Namespaces NÃO usam ownerReference (cross-namespace not allowed).
  Cleanup via finalizer pattern com verify-then-remove (ver decisão arquitetural acima).
  Todos os recursos criados recebem label loopzero.io/tenant={tenantID} para lookup.

Status:
  phase: Provisioning → Active (terminal de sucesso)
  phase: Provisioning → (fica em Provisioning se step falha, com condition explicando)
  gitopsRepo: URL do repo Gitea criado
  observedGeneration: metadata.generation após reconcile completo
  conditions:
    - GitRepoReady: True/False — repo Gitea existe e acessível
    - NamespacesReady: True/False — todos os namespaces existem com quota/limitrange
    - RBACReady: True/False — SA + Role + RoleBinding em todos os namespaces
    - NetworkPolicyReady: True/False — deny-all + allow-list em todos os namespaces
    - BuildNamespaceReady: True/False — build-{tenantID} existe com quota
    - QuotaEnforced: True/False — ResourceQuota aplicado e ativo

Failure behavior:
  - Step falha → condition correspondente = False com reason/message
  - Phase permanece "Provisioning" (NÃO avança para Active)
  - Controller faz requeue com backoff exponencial
  - Próximo reconcile tenta APENAS os steps com condition False (skip os True)
  - Crash mid-reconcile → restart lê conditions existentes → retoma de onde parou
  - Estado parcial é explícito: conditions mostram exatamente o que foi provisionado e o que falta

Idempotency:
  - Cada step usa create-or-update (server-side apply)
  - Reconcile chamado 10x produz mesmo estado final
  - Nenhum recurso duplicado, nenhum ID regenerado
  - Conditions não flickam (LastTransitionTime preservado se status não muda)

Compensação em falha parcial:
  - NÃO faz rollback automático de steps concluídos
  - Requeue garante convergência eventual (ou falha permanente explícita após N retries)
  - Resources órfãos mantêm label loopzero.io/tenant={tenantID} para discovery

Cleanup (deletion) — verify-then-remove pattern:
  - Controller usa finalizer "loopzero.io/tenant-cleanup"
  - Quando Tenant.DeletionTimestamp != nil:
    Fase 1 — EMITIR deletes (idempotente):
      1. Listar namespaces com label loopzero.io/tenant={tenantID} → deletar (ignorar NotFound)
      2. Chamar GitRepoProvisioner.ArchiveRepo() (soft delete)
    Fase 2 — VERIFICAR que recursos sumiram:
      3. Re-listar namespaces com label loopzero.io/tenant={tenantID}
      4. Se lista NÃO vazia → requeue (namespace deletion é async)
      5. Se lista VAZIA → remover finalizer → Kubernetes completa a deletion
  - Controller NÃO deleta PriorityClasses (infra global de cluster)
  - Se cleanup falha: finalizer permanece → Tenant fica em Deleting → requeue
  - NÃO usa ownerReference para namespaces (cross-namespace limitation)
```

---

### Task S1.1: TenantController scaffold + envtest

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.1 — TenantController
**Depende de:** Sprint 0 (Tenant CRD exists)
**Estimativa:** M

#### O que implementar
- Controller `operators/internal/controller/tenant_controller.go`
- Scaffold kubebuilder controller para Tenant CR
- Ginkgo test suite `operators/internal/controller/suite_test.go` + `tenant_controller_test.go`
- Controller registrado no manager com leader election
- Reconcile loop skeleton:
  1. Lê Tenant
  2. Se DeletionTimestamp != nil → executa cleanup (ver S1.7) → remove finalizer → return
  3. Adiciona finalizer `loopzero.io/tenant-cleanup` se não presente
  4. Seta Phase=Provisioning se Phase vazio
  5. Executa steps de provisioning
- Configura envtest com CRDs do operators/config/crd/bases/

#### Critérios de aceite
- [ ] Controller compila e registra no manager
- [ ] Ginkgo test suite roda contra envtest
- [ ] Criar Tenant CR → Reconcile é chamado
- [ ] Tenant sem status → Phase = Provisioning após primeiro reconcile
- [ ] Finalizer `loopzero.io/tenant-cleanup` adicionado automaticamente
- [ ] Leader election configurado
- [ ] `make test` passa

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/tenant_controller_test.go`
- Ginkgo + envtest: criar Tenant CR → verificar Phase = Provisioning
- Ginkgo + envtest: verificar finalizer adicionado
- Testar: controller não panic em Tenant CR com spec vazio

#### Riscos conhecidos
- envtest requer binários de etcd/kube-apiserver — Makefile setup-envtest target

---

### Task S1.2: Reconcile — Namespaces + ResourceQuota + LimitRange

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.1 steps 2-3 · §14 — Multi-tenancy
**Depende de:** S1.1
**Estimativa:** M

#### O que implementar
- Para cada environment em `Tenant.Spec.Environments`:
  1. Criar namespace `{tenantID}-{env}` no cluster (same-cluster em Sprint 1)
  2. Setar labels no namespace:
     ```yaml
     labels:
       loopzero.io/tenant: "{tenantID}"
       loopzero.io/environment: "{env}"
       loopzero.io/managed-by: "tenant-controller"
     ```
  3. **NÃO usar ownerReference** (cross-namespace not allowed — cleanup via finalizer)
  4. Criar ResourceQuota:
     ```yaml
     spec:
       hard:
         requests.cpu: {spec.limits.maxCPU}
         requests.memory: {spec.limits.maxMemory}
         persistentvolumeclaims: "10"
         services.loadbalancers: "0"
     ```
  5. Criar LimitRange:
     ```yaml
     spec:
       limits:
         - type: Container
           default:
             cpu: "500m"
             memory: "512Mi"
           defaultRequest:
             cpu: "100m"
             memory: "128Mi"
     ```
- Namespace naming via RFC 1123 (lowercase, max 63 chars, alphanum + dash)
- Idempotente: create-or-update via controllerutil.CreateOrUpdate
- Atualiza condition `NamespacesReady`

#### Critérios de aceite
- [ ] Namespace `{tenantID}-{env}` criado para cada environment no spec
- [ ] Labels `loopzero.io/tenant` e `loopzero.io/environment` setados
- [ ] **NÃO usa ownerReference** (cleanup via finalizer em S1.7)
- [ ] ResourceQuota aplicado com valores do spec.limits
- [ ] LimitRange aplicado com defaults definidos
- [ ] Idempotente: reconcile 2x → mesmos namespaces, sem duplicação
- [ ] Namespace names RFC 1123 compliant (max 63 chars)
- [ ] Condition `NamespacesReady` = True após sucesso
- [ ] Environment removido do spec → namespace correspondente NÃO é deletado automaticamente (segurança — requer ação manual)

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/tenant_controller_test.go`
- envtest: criar Tenant com 2 envs → verificar 2 namespaces criados
- envtest: verificar ResourceQuota com valores corretos
- envtest: verificar LimitRange com defaults
- envtest: reconcile 2x → count de namespaces permanece 2
- envtest: verificar labels `loopzero.io/tenant` presentes (NÃO ownerReference)

#### Riscos conhecidos
- Sprint 1 usa same-cluster (controller e namespaces no mesmo cluster)
- Multi-cluster (mgmt → workload) será implementado em Sprint 2+

---

### Task S1.3: Reconcile — RBAC (ServiceAccount + Role + RoleBinding)

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.1 step 4 · §16.3 — RBAC
**Depende de:** S1.2
**Estimativa:** M

#### O que implementar
- Para cada namespace criado:
  1. ServiceAccount `loopzero-deployer`
  2. Role com permissões por plan tier:
     ```yaml
     # starter
     rules:
       - apiGroups: ["", "apps"]
         resources: ["deployments", "services", "configmaps"]
         verbs: ["get", "list", "create", "update", "patch"]

     # growth (starter + extras)
       - apiGroups: [""]
         resources: ["secrets"]
         verbs: ["get", "list", "create", "update", "patch"]

     # enterprise (growth + extras)
       - apiGroups: ["networking.k8s.io"]
         resources: ["ingresses", "networkpolicies"]
         verbs: ["get", "list", "create", "update", "patch", "delete"]
     ```
  3. RoleBinding: loopzero-deployer → Role
- Idempotente via controllerutil.CreateOrUpdate
- Atualiza condition `RBACReady`

#### Critérios de aceite
- [ ] ServiceAccount `loopzero-deployer` criado em cada namespace
- [ ] Role com permissões corretas para o plan tier
- [ ] RoleBinding liga ServiceAccount ao Role
- [ ] Plan `starter` → deploy + view only
- [ ] Plan `growth` → starter + secrets
- [ ] Plan `enterprise` → growth + ingress/networkpolicy management
- [ ] Idempotente: re-reconcile não falha
- [ ] Condition `RBACReady` = True

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/tenant_controller_test.go`
- envtest: Tenant plan=starter → verificar Role com permissões básicas
- envtest: Tenant plan=enterprise → verificar Role com permissões expandidas
- envtest: verificar ServiceAccount e RoleBinding existem
- envtest: reconcile 2x → recursos permanecem idênticos

#### Riscos conhecidos
- Nenhum

---

### Task S1.4: Reconcile — NetworkPolicy

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §14.4 — Network isolation
**Depende de:** S1.2
**Estimativa:** M

#### O que implementar
- Para cada namespace de tenant:
  1. **Deny-all** NetworkPolicy (default):
     ```yaml
     spec:
       podSelector: {}
       policyTypes: ["Ingress", "Egress"]
     ```
  2. **Allow ingress** de Traefik:
     ```yaml
     spec:
       podSelector: {}
       ingress:
         - from:
           - namespaceSelector:
               matchLabels:
                 app.kubernetes.io/name: traefik
       policyTypes: ["Ingress"]
     ```
  3. **Allow egress** DNS + intra-namespace + API server (internet BLOQUEADA):
     ```yaml
     spec:
       podSelector: {}
       egress:
         # DNS resolution
         - to:
           - namespaceSelector:
               matchLabels:
                 kubernetes.io/metadata.name: kube-system
           ports:
             - protocol: UDP
               port: 53
             - protocol: TCP
               port: 53
         # Intra-namespace communication
         - to:
           - podSelector: {}
         # Kubernetes API server (service discovery, readiness)
         - to:
           - ipBlock:
               cidr: {API_SERVER_CIDR}/32
           ports:
             - protocol: TCP
               port: 443
       policyTypes: ["Egress"]
     ```
  **NOTA:** Internet egress é BLOQUEADO por default (deny-all + allow-list explícita).
  Tenants que precisam de internet (webhooks, APIs externas) terão
  `Tenant.Spec.EgressPolicy` configurável em sprint futuro.
- Atualiza condition `NetworkPolicyReady`

#### Critérios de aceite
- [ ] NetworkPolicy deny-all criada como default
- [ ] Ingress permitido apenas de namespace do Traefik
- [ ] Egress DNS permitido (kube-system:53)
- [ ] Egress intra-namespace permitido
- [ ] Egress API server permitido (TCP 443)
- [ ] **Internet egress BLOQUEADO** (sem 0.0.0.0/0)
- [ ] Metadata service (169.254.169.254) implicitamente bloqueado pelo deny-all
- [ ] Idempotente
- [ ] Condition `NetworkPolicyReady` = True

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/tenant_controller_test.go`
- envtest: criar Tenant → verificar 3 NetworkPolicies por namespace (deny-all, allow-ingress, allow-egress)
- envtest: verificar deny-all spec
- envtest: verificar allow-list rules (Traefik, DNS, intra-namespace, API server)
- envtest: verificar que NÃO existe regra com cidr 0.0.0.0/0
- envtest: reconcile 2x → mesmas policies

#### Riscos conhecidos
- envtest não avalia enforcement de NetworkPolicy — apenas verifica criação do resource

---

### Task S1.5: Reconcile — Build namespace + QuotaEnforced

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.1 steps 7-8 · §14 — Multi-tenancy
**Depende de:** S1.2
**Estimativa:** M

#### O que implementar
- Criar namespace `build-{tenantID}` com labels (NÃO ownerReference — mesmo motivo):
  ```yaml
  labels:
    loopzero.io/tenant: "{tenantID}"
    loopzero.io/component: "build"
    loopzero.io/managed-by: "tenant-controller"
  ```
- ResourceQuota no build namespace:
  ```yaml
  spec:
    hard:
      requests.cpu: "4"
      requests.memory: "8Gi"
      limits.cpu: "8"
      limits.memory: "16Gi"
      count/workflows.argoproj.io: "{spec.limits.maxBuildConcurrency}"
  ```
- **NÃO cria PriorityClass** — PriorityClasses são infra global de cluster,
  criadas no bootstrap (`task setup` ou Helm chart). Ver decisão arquitetural acima.
  Builds referenciam `loopzero-build-priority-{plan}` pelo plan tier do tenant.
- Atualiza conditions `BuildNamespaceReady` e `QuotaEnforced`
- `QuotaEnforced` = True quando TODOS os ResourceQuotas (workload + build) estão aplicados

#### Critérios de aceite
- [ ] Namespace `build-{tenantID}` criado com labels corretas
- [ ] **NÃO usa ownerReference** (cleanup via finalizer em S1.7)
- [ ] ResourceQuota no build namespace com maxBuildConcurrency
- [ ] **NÃO cria PriorityClass** (infra global — ver decisão arquitetural)
- [ ] Condition `BuildNamespaceReady` = True
- [ ] Condition `QuotaEnforced` = True (todos os quotas aplicados)
- [ ] Idempotente

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/tenant_controller_test.go`
- envtest: verificar build namespace + labels + ResourceQuota
- envtest: verificar que controller NÃO cria PriorityClass
- envtest: verificar QuotaEnforced reflete estado real

#### Riscos conhecidos
- PriorityClasses devem existir no cluster antes do controller rodar (bootstrap dependency)

---

### Task S1.6: Reconcile — Gitea repo provisioning

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.1 step 1
**Depende de:** S1.1
**Estimativa:** M

#### O que implementar
- Interface `GitRepoProvisioner`:
  ```go
  type GitRepoProvisioner interface {
      EnsureRepo(ctx context.Context, name string, branch string) (repoURL string, err error)
      RepoExists(ctx context.Context, name string) (bool, error)
  }
  ```
- Implementação Gitea: `operators/internal/gitea/client.go`
  - Usa Gitea REST API para criar org repo
  - Idempotente: se repo já existe, retorna URL sem erro
- Fake implementation para envtest: `operators/internal/gitea/fake.go`
- Reconcile step:
  1. Chamar `EnsureRepo(spec.gitops.repoName, spec.gitops.branch)`
  2. Setar `status.gitopsRepo = repoURL`
  3. Setar condition `GitRepoReady = True`
- Se Gitea indisponível: condition `GitRepoReady = False`, reason `GiteaUnavailable`, requeue

#### Critérios de aceite
- [ ] Interface `GitRepoProvisioner` definida
- [ ] Fake implementation para testes
- [ ] Gitea client cria repo se não existe
- [ ] Idempotente: segunda chamada retorna URL sem recriar
- [ ] `status.gitopsRepo` preenchido com URL
- [ ] Condition `GitRepoReady` = True após sucesso
- [ ] Gitea indisponível → `GitRepoReady` = False, requeue
- [ ] Usa `pkg/errors` LoopZeroError para erros

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/tenant_controller_test.go`
- envtest com fake GitRepoProvisioner
- Testar: Tenant criado → GitRepoReady = True, gitopsRepo preenchido
- Testar: fake retorna erro → GitRepoReady = False, controller requeue
- Testar: reconcile 2x → fake chamado 1x (idempotente via condition check)

#### Riscos conhecidos
- Gitea real requer URL/credentials do cluster — configurável via env ou Secret

---

### Task S1.7: Reconcile — Phase transition + crash recovery + idempotency

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.1 · §5 — State machines · §20 — Test contracts
**Depende de:** S1.2, S1.3, S1.4, S1.5, S1.6
**Estimativa:** G

#### O que implementar
- **Phase transition:**
  ```go
  // Após cada step, verificar se TODOS os conditions são True
  allReady := allConditionsTrue(tenant.Status.Conditions,
      ConditionGitRepoReady,
      ConditionNamespacesReady,
      ConditionRBACReady,
      ConditionNetworkPolicyReady,
      ConditionBuildNamespaceReady,
      ConditionQuotaEnforced,
  )
  if allReady {
      tenant.Status.Phase = TenantPhaseActive
  }
  tenant.Status.ObservedGeneration = tenant.Generation
  ```
- **Crash recovery:** cada step verifica condition antes de executar
  ```go
  if !isConditionTrue(conditions, ConditionNamespacesReady) {
      if err := r.reconcileNamespaces(ctx, tenant); err != nil {
          return ctrl.Result{}, err
      }
  }
  // Skip if already done — resume from where it stopped
  ```
- **Finalizer cleanup (deletion) — verify-then-remove pattern:**
  ```go
  if !tenant.DeletionTimestamp.IsZero() {
      tenant.Status.Phase = TenantPhaseDeleting

      // FASE 1: EMITIR deletes (idempotente)
      // 1. Delete namespaces by label (ignorar NotFound)
      nsList := &corev1.NamespaceList{}
      client.List(ctx, nsList, client.MatchingLabels{"loopzero.io/tenant": tenant.Name})
      for _, ns := range nsList.Items {
          client.Delete(ctx, &ns, client.PropagationPolicy(metav1.DeletePropagationBackground))
          // Ignore NotFound — already deleted
      }

      // 2. Archive Gitea repo (soft delete, idempotente)
      gitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName)

      // FASE 2: VERIFICAR que recursos realmente sumiram
      // 3. Re-listar namespaces com label loopzero.io/tenant={tenantID}
      remainingNs := &corev1.NamespaceList{}
      client.List(ctx, remainingNs, client.MatchingLabels{"loopzero.io/tenant": tenant.Name})
      if len(remainingNs.Items) > 0 {
          // Namespaces ainda existem (deletion é async) → requeue
          return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
      }

      // 4. Tudo limpo → remover finalizer → K8s completa deletion
      controllerutil.RemoveFinalizer(tenant, "loopzero.io/tenant-cleanup")
      return ctrl.Result{}, client.Update(ctx, tenant)
  }
  ```
  **NOTA:** Controller NÃO deleta PriorityClasses (são infra global de cluster).
  Namespace deletion é async — o finalizer só é removido APÓS verificar que
  todos os namespaces com label `loopzero.io/tenant={tenantID}` sumiram.
- Idempotency validation: reconcile loop completo é seguro para re-execução

#### Critérios de aceite
- [ ] Todos os 6 conditions True → Phase = Active
- [ ] Qualquer condition False → Phase permanece Provisioning
- [ ] ObservedGeneration = metadata.Generation após reconcile
- [ ] Crash após namespace mas antes de RBAC → restart: NamespacesReady=True, pula namespace, faz RBAC
- [ ] Crash após RBAC mas antes de NetworkPolicy → restart retoma de NetworkPolicy
- [ ] Reconcile 10x seguidas → mesmo estado final, nenhum recurso duplicado
- [ ] Spec update (mudança de plan) → re-reconcile atualiza RBAC roles
- [ ] Requeue automático quando qualquer step falha
- [ ] **Delete Tenant → Phase=Deleting → namespaces deleted → verify-then-remove → finalizer removed**
- [ ] Delete NÃO deleta PriorityClasses (infra global)
- [ ] Delete com namespace ainda existindo (async) → finalizer permanece → requeue
- [ ] Delete com todos namespaces gone → finalizer removido → Tenant deletado

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/tenant_controller_test.go`
- envtest: criar Tenant completo → verificar Phase = Active
- envtest: criar Tenant com fake Gitea falhando → Phase = Provisioning, GitRepoReady = False
- envtest: simular crash (early return) após namespace → re-trigger → Phase = Active
- envtest: reconcile 2x → count de namespaces/RBAC/policies idêntico
- envtest: update Tenant plan starter→growth → Role permissions expandem
- envtest: **deletar Tenant → verificar namespaces deletados via label selector**
- envtest: **deletar Tenant → verificar finalizer NÃO removido enquanto namespaces existem (verify-then-remove)**
- envtest: **deletar Tenant → verificar finalizer removido apenas quando namespaces gone**
- envtest: **verificar que controller NÃO tenta deletar PriorityClasses**

#### Riscos conhecidos
- envtest não suporta crash real — simular via early return ou error injection no reconcile
- Namespace deletion pode levar tempo (finalizers no namespace) — use Eventually com timeout

---

### Task S1.8: Integration test — Tenant reconciliation end-to-end (envtest)

**Componente:** operators
**Agent:** test-engineer
**Spec:** §20 — Test contracts · §7.3.1
**Depende de:** S1.7
**Estimativa:** M

#### O que implementar
- Teste de integração completo em `operators/internal/controller/tenant_integration_test.go`
- Este teste é o **proof of convergence** do control plane
- Cenários:

```ginkgo
Describe("TenantController Integration", func() {

  Context("happy path — full provisioning", func() {
    It("should provision all resources and reach Active phase", func() {
      // 1. Apply Tenant CR
      tenant := newTenant("acme", PlanStarter, []string{"production", "staging"})
      Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

      // 2. Wait for reconciliation
      Eventually(func() TenantPhase {
          Expect(k8sClient.Get(ctx, tenantKey, tenant)).To(Succeed())
          return tenant.Status.Phase
      }, timeout, interval).Should(Equal(TenantPhaseActive))

      // 3. Verify ALL expected objects exist
      // Namespaces
      Expect(k8sClient.Get(ctx, nsKey("acme-production"), &corev1.Namespace{})).To(Succeed())
      Expect(k8sClient.Get(ctx, nsKey("acme-staging"), &corev1.Namespace{})).To(Succeed())

      // ResourceQuotas
      var quota corev1.ResourceQuota
      Expect(k8sClient.Get(ctx, quotaKey("acme-production"), &quota)).To(Succeed())
      Expect(quota.Spec.Hard.Cpu().String()).To(Equal("8"))

      // RBAC
      Expect(k8sClient.Get(ctx, saKey("acme-production", "loopzero-deployer"), &corev1.ServiceAccount{})).To(Succeed())

      // NetworkPolicy
      var policies networkingv1.NetworkPolicyList
      Expect(k8sClient.List(ctx, &policies, client.InNamespace("acme-production"))).To(Succeed())
      Expect(len(policies.Items)).To(BeNumerically(">=", 3)) // deny-all, allow-traefik, allow-dns

      // Build namespace
      Expect(k8sClient.Get(ctx, nsKey("build-acme"), &corev1.Namespace{})).To(Succeed())

      // Conditions
      Expect(conditionStatus(tenant, ConditionGitRepoReady)).To(Equal(metav1.ConditionTrue))
      Expect(conditionStatus(tenant, ConditionNamespacesReady)).To(Equal(metav1.ConditionTrue))
      Expect(conditionStatus(tenant, ConditionRBACReady)).To(Equal(metav1.ConditionTrue))
      Expect(conditionStatus(tenant, ConditionNetworkPolicyReady)).To(Equal(metav1.ConditionTrue))
      Expect(conditionStatus(tenant, ConditionBuildNamespaceReady)).To(Equal(metav1.ConditionTrue))
      Expect(conditionStatus(tenant, ConditionQuotaEnforced)).To(Equal(metav1.ConditionTrue))

      // ObservedGeneration
      Expect(tenant.Status.ObservedGeneration).To(Equal(tenant.Generation))

      // GitOps repo
      Expect(tenant.Status.GitOpsRepo).ToNot(BeEmpty())
    })
  })

  Context("partial failure — Gitea unavailable", func() {
    It("should provision K8s resources but report GitRepoReady=False", func() {
      // Inject Gitea failure
      // Verify: Phase=Provisioning, GitRepoReady=False, NamespacesReady=True
      // Verify: all K8s objects still created
    })
  })

  Context("idempotency", func() {
    It("should produce identical state after 3 consecutive reconciles", func() {
      // Create Tenant, wait Active
      // Count resources (namespaces, policies, quotas)
      // Trigger reconcile 2 more times
      // Count resources again — must be identical
    })
  })

  Context("crash recovery", func() {
    It("should resume from partial state and reach Active", func() {
      // Create Tenant with injected error after namespace step
      // Verify: NamespacesReady=True, RBACReady=False, Phase=Provisioning
      // Remove error injection
      // Wait for convergence: Phase=Active, all conditions True
    })
  })

  Context("spec update", func() {
    It("should re-reconcile RBAC when plan changes", func() {
      // Create Tenant plan=starter, wait Active
      // Update plan to growth
      // Verify: Role now includes secrets permissions
    })
  })

  Context("deletion via finalizer (verify-then-remove)", func() {
    It("should delete namespaces by label, verify they're gone, then remove finalizer", func() {
      // Create Tenant, wait Active
      // Verify finalizer present: "loopzero.io/tenant-cleanup"
      // Delete Tenant
      // Wait for Phase=Deleting
      // Verify: namespaces with label loopzero.io/tenant=acme are eventually deleted
      // Verify: controller does NOT attempt to delete PriorityClasses
      // Verify: finalizer removed only AFTER namespaces gone → Tenant object gone
    })
  })

  Context("deletion with partial cleanup failure", func() {
    It("should keep finalizer if cleanup is incomplete", func() {
      // Create Tenant, wait Active
      // Inject namespace deletion failure
      // Delete Tenant
      // Verify: finalizer still present → Tenant stuck in Deleting
      // Remove failure injection → next reconcile completes cleanup
    })
  })
})
```

#### Critérios de aceite
- [ ] Happy path: Tenant CR → Phase=Active com todos os 6 conditions True
- [ ] Todos os objetos Kubernetes verificados (namespace, quota, limitrange, SA, Role, RoleBinding, NetworkPolicy, build namespace)
- [ ] Partial failure: Gitea down → K8s resources created, Phase=Provisioning, condition explica falha
- [ ] Idempotency: 3 reconciles → estado idêntico
- [ ] Crash recovery: estado parcial → convergência eventual
- [ ] Spec update: plan change → RBAC roles atualizados
- [ ] Deletion: verify-then-remove → namespaces deleted by label → verified gone → finalizer removed (no PriorityClass deletion)
- [ ] Deletion partial failure: finalizer preserved → Tenant stuck in Deleting → requeue resolves
- [ ] Teste roda em < 60 segundos via envtest

#### Teste primeiro (TDD)
- Este IS o teste — escrito ANTES de qualquer refactor necessário para passar
- Se algum cenário falha, é bug na implementação dos steps S1.1-S1.7

#### Riscos conhecidos
- Envtest pode ter race conditions com Eventually — usar timeout/interval adequados

---

## Fase 2 — CLI Foundation + Auth

> **Objetivo:** CLI existe, autentica via GitHub, detecta projetos.
> Pode rodar em paralelo com Fase 1 a partir do momento que S1.1 (controller scaffold) começa.

### Task S1.9: CLI project scaffold (Commander + TypeScript)

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §0.2 — CLI stability contract, §25 — CLI reference
**Depende de:** nenhuma
**Estimativa:** M

#### O que implementar
- Estrutura do projeto `cli/` com TypeScript + Commander
- `package.json` com deps: `commander`, `chalk`, `ora`, `open`
- `tsconfig.json` com strict mode
- Entrypoint `cli/src/index.ts` que registra subcomandos
- Script `bin/loopzero` como CLI binary
- Jest configurado em `cli/jest.config.js`
- ESLint configurado

#### Critérios de aceite
- [ ] `npx ts-node cli/src/index.ts --help` exibe lista de comandos
- [ ] `npx ts-node cli/src/index.ts --version` exibe versão semver
- [ ] `npm test` roda Jest com exit 0
- [ ] `npm run build` gera dist/ sem erros
- [ ] Zero vulnerabilidades conhecidas em deps

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/index.test.ts`
- Validar: CLI exporta Commander program com nome "loopzero"
- Validar: `--version` retorna string semver

#### Riscos conhecidos
- Nenhum

---

### Task S1.10: Config e token storage module

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §16.1 — Token storage, §25.1 — Authentication
**Depende de:** S1.9
**Estimativa:** P

#### O que implementar
- Módulo `cli/src/config/store.ts`
- Token em `~/.loopzero/config.json` (ou `$XDG_CONFIG_HOME/loopzero/config.json`)
- Funções: `saveToken(token, email, team)`, `getToken()`, `clearToken()`, `getEmail()`, `getTeam()`
- Permissões: `0600` (owner only)
- **Importante:** token armazenado é o GitHub OAuth token — não há transformação

#### Critérios de aceite
- [ ] `saveToken()` cria diretório `~/.loopzero/` se não existe
- [ ] Arquivo escrito com permissões 0600
- [ ] `getToken()` retorna token ou `null`
- [ ] `clearToken()` remove arquivo
- [ ] Respeita `$XDG_CONFIG_HOME`
- [ ] Nunca loga/printa valor do token

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/config/store.test.ts`
- Usa tmpdir como config dir
- Testar: save → get → clear lifecycle
- Testar: permissões 0600
- Testar: null quando não existe

#### Riscos conhecidos
- Nenhum

---

### Task S1.11: CLI output formatting (human + JSON + CI mode)

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §0.3 — Exit codes + JSON schemas + CI mode
**Depende de:** S1.9
**Estimativa:** P

#### O que implementar
- Módulo `cli/src/output/formatter.ts`
- Funções: `success(msg)`, `error(msg, exitCode)`, `info(msg)`, `json(data)`, `table(headers, rows)`
- CI mode detection: `CI=true` || `GITHUB_ACTIONS` || `GITLAB_CI` || `CIRCLECI` || `JENKINS_URL`
- Flag `--json` global para JSON output
- Sem cores/spinners em CI mode

#### Critérios de aceite
- [ ] `--json` produz JSON válido em stdout
- [ ] `CI=true` desabilita cores e spinners
- [ ] `error()` printa em stderr
- [ ] Nenhum termo interno (Rule 3)
- [ ] Exit codes: apenas 0, 1, 2, 3, 4, 5

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/output/formatter.test.ts`
- Testar cada função human vs JSON
- Testar CI mode detection
- Grep automatizado por termos proibidos

#### Riscos conhecidos
- Nenhum

---

### Task S1.12: API client module

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §0.3 — CLI output contract
**Depende de:** S1.10
**Estimativa:** P

#### O que implementar
- Módulo `cli/src/api/client.ts`
- Injeta `Authorization: Bearer {token}` via `getToken()`
- Injeta `Correlation-ID` (UUID v4)
- `LOOPZERO_API_URL` env (default: `http://localhost:3001`)
- Métodos: `get(path)`, `post(path, body)`, `delete(path)`
- HTTP error → exit code mapping: 401→4, 409→"operation in progress", 422→"invalid request", 5xx→"service unavailable"
- Timeout: 30s, retry: 0

#### Critérios de aceite
- [ ] Bearer token injected
- [ ] Correlation-ID injected (UUID v4)
- [ ] 401 → exit code 4 com "Not authenticated. Run: loopzero login"
- [ ] Nenhum termo interno em mensagens
- [ ] `LOOPZERO_API_URL` env var respeitada

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/api/client.test.ts`
- Testar: Authorization header present
- Testar: Correlation-ID header present
- Testar: HTTP status → exit code mapping
- Testar: mensagens sem termos internos

#### Riscos conhecidos
- Usar `fetch` nativo (Node 20+)

---

### Task S1.13: `loopzero login` — GitHub OAuth flow + `--token`

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.1 — Authentication, §16 — Security model
**Depende de:** S1.10, S1.11
**Estimativa:** G

#### O que implementar
- Comando `cli/src/commands/login.ts`
- **Fluxo interativo (browser):**
  1. HTTP server local em porta aleatória
  2. Abre browser com GitHub OAuth URL
  3. Recebe callback com authorization code
  4. Troca code por GitHub access token
  5. Busca perfil via GitHub API (email, orgs)
  6. `saveToken(token, email, team)`
  7. Fecha HTTP server
- **Fluxo `--token`** (CI/CD):
  1. Valida token via GitHub API (`GET /user`)
  2. Se válido: salva
  3. Se inválido: exit 4
- **Flag `--team`** (multi-tenant):
  - Quando usuário pertence a múltiplos tenants, `--team` seleciona qual tenant usar
  - Sem `--team` + múltiplos tenants → lista tenants disponíveis e pede seleção (interativo)
  - Sem `--team` + múltiplos tenants + CI mode → exit 1 com mensagem listando opções
  - Salva team selecionado em `~/.loopzero/config.json` para uso posterior
  - Ver decisão arquitetural "seleção de tenant" acima
- Output: `✓ Authenticated as {email} (team: {team})`
- JSON: `{ "authenticated": true, "email": "...", "team": "..." }`
- Exit codes: 0 (success), 4 (auth error), 1 (multiple teams, no --team flag in CI)
- **Clarificação:** o token armazenado É o token GitHub. Não há exchange, não há sessão LoopZero.

#### Critérios de aceite
- [ ] Browser flow funciona (OAuth callback processado)
- [ ] `--token` funciona sem browser
- [ ] Token armazenado com permissões 0600
- [ ] Output humano correto
- [ ] `--json` output com campos corretos
- [ ] Exit code 4 em falha de auth
- [ ] Nunca printa/loga token
- [ ] `--token` sem valor → exit 1 (usage error)
- [ ] `--team` seleciona tenant quando múltiplos disponíveis
- [ ] Sem `--team` + 1 tenant → auto-resolve
- [ ] Sem `--team` + N tenants + CI → exit 1 com lista de teams
- [ ] Team selecionado salvo em config

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/login.test.ts`
- Testar: callback handler extrai code
- Testar: token salvo via store
- Testar: output format human + JSON
- Testar: exit 4 em falha
- Testar: --token válido → save + exit 0
- Testar: --token inválido → exit 4
- Testar: --team flag seleciona tenant
- Testar: múltiplos tenants sem --team em CI → exit 1

#### Riscos conhecidos
- GitHub OAuth App precisa de `LOOPZERO_GITHUB_CLIENT_ID` e `LOOPZERO_GITHUB_CLIENT_SECRET`

---

### Task S1.14: `loopzero logout` e `loopzero whoami`

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.1 — Authentication
**Depende de:** S1.10, S1.11
**Estimativa:** P

#### O que implementar
- `cli/src/commands/logout.ts`: `clearToken()` → `✓ Logged out` → exit 0
- `cli/src/commands/whoami.ts`:
  - Autenticado: `Authenticated as: {email}\nTeam: {team}` → exit 0
  - Autenticado + múltiplos tenants: mostra team selecionado + lista de teams disponíveis
  - Não autenticado: `✗ Not authenticated. Run: loopzero login` → exit 4
  - JSON: `{ "authenticated": true/false, "email": "...", "team": "...", "teams": ["..."] }`

#### Critérios de aceite
- [ ] `loopzero logout` remove token, printa confirmação
- [ ] `loopzero whoami` exibe email e team
- [ ] `loopzero whoami` sem token → exit 4
- [ ] `loopzero whoami --json` retorna JSON válido
- [ ] Após logout, whoami retorna exit 4

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/logout.test.ts`
- Arquivo: `cli/tests/unit/commands/whoami.test.ts`

#### Riscos conhecidos
- Nenhum

---

### Task S1.15: Monorepo tool detection

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.2 — Monorepo tool detection
**Depende de:** S1.9
**Estimativa:** M

#### O que implementar
- Módulo `cli/src/detection/monorepo.ts`
- `detectMonorepoTool(rootDir): { tool: string, workspaces: string[] }`
- Detecção por marker files (prioridade):

| Tool | Marker |
|------|--------|
| Turborepo | `turbo.json` |
| Nx | `nx.json` |
| Lerna | `lerna.json` |
| pnpm workspaces | `pnpm-workspace.yaml` |
| npm workspaces | `package.json` com `workspaces` |
| yarn workspaces | `package.json` com `workspaces` + `yarn.lock` |
| Flat | Múltiplos dirs com `package.json` / `go.mod` / `requirements.txt` |

#### Critérios de aceite
- [ ] Detecta cada um dos 7 cenários
- [ ] Resolve glob patterns em workspace configs
- [ ] "flat" quando sem tool mas múltiplos projetos

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/detection/monorepo.test.ts`
- Fixture dirs em tmpdir para cada cenário

#### Riscos conhecidos
- Glob patterns variam entre tools

---

### Task S1.16: Framework detection

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §26.5 — Supported frameworks v1
**Depende de:** S1.9
**Estimativa:** M

#### O que implementar
- Módulo `cli/src/detection/framework.ts`
- `detectFramework(appDir): { framework: string, port: number, buildCommand?: string, startCommand?: string }`
- 10 frameworks: nextjs, vite, remix, nestjs, express, fastify, fastapi, django, gin, node (fallback)
- Prioriza config file sobre dependência

#### Critérios de aceite
- [ ] Detecta todos os 10 frameworks
- [ ] Porta default correta por framework
- [ ] Framework desconhecido → `{ framework: "custom", port: null }`
- [ ] Não falha em diretório vazio

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/detection/framework.test.ts`
- Fixture dirs para cada framework

#### Riscos conhecidos
- FastAPI requer ler conteúdo do arquivo, não só nome

---

## Fase 3 — Vertical Slice

> **Objetivo:** conectar CLI → API → Kubernetes. Esta fase é o proof final do sprint.
> Só pode começar quando Fase 1 (control plane) e Fase 2 (CLI auth) estão completas.

### Task S1.17: API HTTP server scaffold

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2 — loopzero API
**Depende de:** Sprint 0
**Estimativa:** M

#### O que implementar
- Expandir `api/cmd/main.go` com HTTP server (chi router)
- Middleware chain: correlation-id → logging → (auth — wired mas não enforced em health)
- `GET /api/v1/health` → `{ "status": "ok" }`
- `GET /api/v1/health/deep` → verifica DB connection
- Graceful shutdown (SIGTERM/SIGINT)
- Config via env: `PORT` (default: 3001), `DATABASE_URL`
- Usa `pkg/logging` e `pkg/errors`

#### Critérios de aceite
- [ ] `GET /api/v1/health` → 200 `{ "status": "ok" }`
- [ ] `GET /api/v1/health/deep` → 200 (DB up) ou 503 (DB down)
- [ ] Correlation-ID injected e retornado no response header
- [ ] Request logging: method, path, status, duration
- [ ] Graceful shutdown
- [ ] PORT env var

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/health_test.go`
- Testar: health 200
- Testar: deep health com DB interface (up/down)
- Testar: correlation-id no response header

#### Riscos conhecidos
- chi vs net/http: usar chi para middleware ergonômico

---

### Task S1.18: Database migration — users, tenants, projects

**Componente:** api
**Agent:** api-engineer
**Spec:** §9 — Data model
**Depende de:** S1.17
**Estimativa:** M

#### O que implementar
- Migration `api/internal/db/migrations/000002_auth_tenants.up.sql`
- Tabelas (IF NOT EXISTS):
  - `users`: id (github-{username}), email, github_id, display_name, timestamps
  - `tenants`: id (slug), display_name, plan, owner_id → users, timestamps
  - `tenant_members`: (tenant_id, user_id) PK, role, timestamps
  - `projects`: id (proj-{8hex}), tenant_id → tenants, name, created_by → users, UNIQUE(tenant_id, name)
- Indexes para queries frequentes
- Sem DROP statements (forward-only)

#### Critérios de aceite
- [ ] Migration idempotente (IF NOT EXISTS)
- [ ] 4 tabelas criadas com FKs corretas
- [ ] Composite PK em tenant_members
- [ ] UNIQUE em projects (tenant_id, name)
- [ ] Sem DROP
- [ ] Re-runnable

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/migration_002_test.go`
- Testar: tabelas presentes, IF NOT EXISTS, FKs, sem DROP

#### Riscos conhecidos
- Token GitHub NÃO é armazenado no DB (fica local no CLI)

---

### Task S1.19: Auth middleware — GitHub token validation

**Componente:** api
**Agent:** api-engineer
**Spec:** §16 — Security model
**Depende de:** S1.17, S1.18
**Estimativa:** M

#### O que implementar
- `api/internal/middleware/auth.go`
- Extrai `Authorization: Bearer {token}`
- Valida via GitHub API (`GET https://api.github.com/user`)
- Se válido: injeta `UserContext` (user_id, email, github_id) no request context
- **Multi-tenant resolution (Model C):**
  ```go
  // Resolve tenant binding via tenant_members
  memberships := db.GetTenantMemberships(userID)
  switch len(memberships) {
  case 0:
      // Usuário sem tenant → 403 LoopZeroError{Code: "NO_TENANT"}
  case 1:
      // Auto-resolve → injeta tenant_id no context
  default:
      // Múltiplos tenants → lê header X-LoopZero-Tenant
      requestedTenant := r.Header.Get("X-LoopZero-Tenant")
      if requestedTenant == "" {
          // 409 LoopZeroError{Code: "TENANT_REQUIRED", Details: memberships}
      }
      // Valida que requestedTenant está na lista de memberships
      // Se não → 403 LoopZeroError{Code: "TENANT_NOT_FOUND"}
  }
  ```
- Se inválido/ausente: 401 com LoopZeroError
- Se GitHub API down: 503 com LoopZeroError
- Cache de validação: 5 minutos
- Interface `TokenValidator` (DIP)

#### Critérios de aceite
- [ ] Sem header → 401 LoopZeroError
- [ ] Token inválido → 401 LoopZeroError
- [ ] Token válido → UserContext no context (user_id, email, tenant_id, role)
- [ ] Cache: 2o request mesmo token → não chama GitHub
- [ ] GitHub down → 503 LoopZeroError
- [ ] **1 tenant → auto-resolve tenant_id**
- [ ] **N tenants + X-LoopZero-Tenant header → resolve ao tenant solicitado**
- [ ] **N tenants + sem X-LoopZero-Tenant → 409 com lista de tenants**
- [ ] **0 tenants → 403 NO_TENANT**
- [ ] **X-LoopZero-Tenant com tenant que usuário não pertence → 403**

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/middleware/auth_test.go`
- Fake TokenValidator
- Testar: sem header → 401
- Testar: inválido → 401
- Testar: válido + 1 tenant → UserContext com tenant auto-resolvido
- Testar: válido + N tenants + X-LoopZero-Tenant → resolve corretamente
- Testar: válido + N tenants + sem header → 409 TENANT_REQUIRED
- Testar: válido + 0 tenants → 403 NO_TENANT
- Testar: cache hit

#### Riscos conhecidos
- GitHub API rate limiting → cache é essencial

---

### Task S1.20: GET /api/v1/auth/whoami

**Componente:** api
**Agent:** api-engineer
**Spec:** §25.1 — Authentication
**Depende de:** S1.19
**Estimativa:** P

#### O que implementar
- `api/internal/routes/auth.go`
- `GET /api/v1/auth/whoami` → retorna user info do UserContext
- Response: `{ "user_id": "...", "email": "...", "team": "...", "role": "..." }`
- 401 se não autenticado (middleware)

#### Critérios de aceite
- [ ] 200 com user info quando autenticado
- [ ] 401 via middleware
- [ ] Response inclui user_id, email, team, role
- [ ] Correlation-ID no response header

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/routes/auth_test.go`

#### Riscos conhecidos
- Nenhum

---

### Task S1.21: POST /api/v1/projects — cria projeto e Tenant CR

**Componente:** api
**Agent:** api-engineer
**Spec:** §25.2 — Project initialization, §9 — Data model
**Depende de:** S1.18, S1.19
**Estimativa:** G

#### O que implementar
- `api/internal/routes/projects.go`
- `POST /api/v1/projects`:
  1. Valida input (nome RFC 1123)
  2. Verifica unicidade (tenant_id, name) no DB
  3. Gera project ID (`proj-{8hex}`)
  4. Insere na tabela `projects`
  5. **Cria/atualiza Tenant CR no Kubernetes** — este é o trigger do TenantController
  6. Retorna 201 com project info
- Kubernetes client interface:
  ```go
  type TenantCRManager interface {
      EnsureTenant(ctx context.Context, tenantID string, spec TenantSpec) error
  }
  ```
- 409 se projeto já existe (LoopZeroError)
- Idempotency-Key middleware

#### Critérios de aceite
- [ ] 201 com project ID
- [ ] 409 se nome duplicado no tenant
- [ ] Nome RFC 1123 validado (400 se inválido)
- [ ] Project inserido no DB
- [ ] **Tenant CR criado/atualizado no cluster** (bridge API → controller)
- [ ] 401 sem auth
- [ ] Idempotency-Key funciona

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/routes/projects_test.go`
- Testar: criação → 201
- Testar: duplicado → 409
- Testar: nome inválido → 400
- Testar: sem auth → 401
- Testar: TenantCRManager.EnsureTenant chamado com spec correta

#### Riscos conhecidos
- Kubernetes client no API server requer kubeconfig ou in-cluster config
- Para testes: fake TenantCRManager

---

### Task S1.22: `loopzero init` — wizard + loopzero.yaml + API call

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.2, §26 — loopzero.yaml spec
**Depende de:** S1.12, S1.13, S1.15, S1.16, S1.21
**Estimativa:** G

#### O que implementar
- Comando `cli/src/commands/init.ts`
- Fluxo:
  1. Verificar auth (exit 4 se não logado)
  2. Detectar monorepo (S1.15)
  3. Detectar frameworks (S1.16)
  4. Exibir resultado
  5. Wizard interativo (ou `--yes` para defaults)
  6. Pedir project name (default: dirname)
  7. Gerar `loopzero.yaml`
  8. Adicionar `.env.local` ao `.gitignore`
  9. **Chamar `POST /api/v1/projects`** → trigger TenantController
  10. Output: resultado da operação
- Flags: `--force` (sobrescreve), `--yes` (non-interactive)

#### Critérios de aceite
- [ ] Detecta monorepo + frameworks
- [ ] Gera `loopzero.yaml` conforme spec §26
- [ ] `--yes` aceita defaults
- [ ] `--force` sobrescreve
- [ ] Sem `--force`, exit 1 se `loopzero.yaml` já existe
- [ ] `.env.local` adicionado ao `.gitignore`
- [ ] Auth required (exit 4)
- [ ] **Chama API que cria Tenant CR → controller reconcilia** (vertical slice)
- [ ] `loopzero.yaml` não contém campo `tenant`
- [ ] Nenhum termo interno (Rule 3)
- [ ] CI mode: requer `--yes`

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/init.test.ts`
- Testar: geração de loopzero.yaml
- Testar: --force / sem --force
- Testar: .env.local adicionado
- Testar: API chamada com payload correto
- Testar: CI mode sem --yes → exit 1

#### Riscos conhecidos
- Wizard interativo: usar `prompts` package
- Depende de API rodando para o fluxo completo

---

### Task S1.23: Vertical slice integration test

**Componente:** cross
**Agent:** test-engineer
**Spec:** §20 — Test contracts, §24.2 — User onboarding
**Depende de:** S1.8, S1.21, S1.22
**Estimativa:** G

#### O que implementar
- Teste em `infra/tests/e2e/vertical_slice_test.go` (ou script shell)
- Este teste prova que **o sistema inteiro converge quando provocado**:

```bash
# Setup: API running, controller running, clusters up

# 1. Autenticação funciona
loopzero login --token $TEST_TOKEN
RESULT=$(loopzero whoami --json)
[ "$(echo $RESULT | jq -r .authenticated)" = "true" ] || exit 1

# 2. CLI fala com API com correlation_id
CORR_ID=$(curl -s -I -H "Authorization: Bearer $TOKEN" \
    http://localhost:3001/api/v1/auth/whoami | grep -i correlation-id | awk '{print $2}')
[ -n "$CORR_ID" ] || exit 1

# 3. API cria recurso e persiste estado
RESPONSE=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"name":"my-saas","apps":[{"name":"api","framework":"express","port":3000,"path":"."}]}' \
    http://localhost:3001/api/v1/projects)
PROJECT_ID=$(echo $RESPONSE | jq -r .id)
[ -n "$PROJECT_ID" ] || exit 1

# 4. Controller reconcilia (Tenant CR → objetos K8s)
# Wait for Active phase (max 60s)
for i in $(seq 1 12); do
    PHASE=$(kubectl get tenant $TENANT_ID -o jsonpath='{.status.phase}' 2>/dev/null)
    [ "$PHASE" = "Active" ] && break
    sleep 5
done
[ "$PHASE" = "Active" ] || exit 1

# 5. Status observável
CONDITIONS=$(kubectl get tenant $TENANT_ID -o json | jq '.status.conditions')
[ "$(echo $CONDITIONS | jq -r '.[] | select(.type=="NamespacesReady") | .status')" = "True" ] || exit 1
[ "$(echo $CONDITIONS | jq -r '.[] | select(.type=="RBACReady") | .status')" = "True" ] || exit 1
[ "$(echo $CONDITIONS | jq -r '.[] | select(.type=="NetworkPolicyReady") | .status')" = "True" ] || exit 1

# 6. Objetos K8s existem
kubectl get ns ${TENANT_ID}-production || exit 1
kubectl get resourcequota -n ${TENANT_ID}-production || exit 1
kubectl get networkpolicy -n ${TENANT_ID}-production || exit 1
kubectl get ns build-${TENANT_ID} || exit 1

# 7. Erro parcial gera estado explícito
# (Inject Gitea failure, create new tenant, verify Phase=Provisioning, condition=False)
```

#### Critérios de aceite
- [ ] CLI autentica e fala com API
- [ ] Correlation-ID cruza boundary CLI → API
- [ ] API cria project + Tenant CR
- [ ] TenantController reconcilia → Phase=Active
- [ ] Todos os objetos K8s observáveis
- [ ] Conditions refletem estado real
- [ ] Erro parcial → estado explícito e recuperável

#### Teste primeiro (TDD)
- Este IS o teste. É o proof of convergence do sprint.

#### Riscos conhecidos
- Requer todos os componentes running (API, controller, cluster)
- Pode ser lento (60s+ para convergência)

---

### Task S1.24: Atualizar CHANGELOG.md

**Componente:** cross
**Agent:** orchestrator
**Spec:** Keep a Changelog
**Depende de:** S1.23
**Estimativa:** P

#### O que implementar
- Atualizar `CHANGELOG.md` seção `[Unreleased]` com:
  - Added: `loopzero login` (GitHub OAuth + `--token`)
  - Added: `loopzero logout`, `loopzero whoami`
  - Added: `loopzero init` (monorepo detection, framework detection, loopzero.yaml generation)
  - Added: API server with health, auth, project creation endpoints
  - Added: TenantController reconciliation (namespaces, RBAC, NetworkPolicy, build namespace, quotas)
  - Added: Vertical slice: CLI → API → Kubernetes convergence tested

#### Critérios de aceite
- [ ] Entries seguem Keep a Changelog format
- [ ] Cada entry referencia task ou PR

#### Riscos conhecidos
- Nenhum

---

## Grafo de Dependências

```
Sprint 0 (Complete)
    │
    │ ═══════════════ FASE 1: PROVA DO CONTROL PLANE ═══════════════
    │
    ├── S1.1 (Controller scaffold) ──→ S1.6 (Gitea repo) ─────────────────┐
    │       │                                                               │
    │       ├── S1.2 (Namespaces+Quota) ──→ S1.3 (RBAC) ──────────────────┤
    │       │       │                                                       │
    │       │       ├── S1.4 (NetworkPolicy) ──────────────────────────────┤
    │       │       │                                                       │
    │       │       └── S1.5 (Build ns + Quota) ──────────────────────────┤
    │       │                                                               │
    │       └───────────────────────────────────────→ S1.7 (Phase + crash) ┤
    │                                                                       │
    │                                                   S1.8 (Integration) ←┘
    │
    │ ═══════════════ FASE 2: CLI FOUNDATION (paralelo) ═══════════════
    │
    ├── S1.9 (CLI scaffold) ──→ S1.10 (Token store) ──→ S1.12 (API client) ──→ S1.13 (login)
    │       │                       │                                                │
    │       ├── S1.11 (Output fmt) ─┼── S1.14 (logout/whoami)                       │
    │       │                       │                                                │
    │       ├── S1.15 (Monorepo) ───┼────────────────────────────────────────────────┤
    │       │                       │                                                │
    │       └── S1.16 (Framework) ──┼────────────────────────────────────────────────┤
    │                               │                                                │
    │ ═══════════════ FASE 3: VERTICAL SLICE ═══════════════                         │
    │                                                                                │
    ├── S1.17 (API server) ──→ S1.18 (DB migration) ──→ S1.19 (Auth middleware)     │
    │                                                        │                       │
    │                                                        ├── S1.20 (whoami)      │
    │                                                        │                       │
    │                                                        └── S1.21 (POST projects + Tenant CR)
    │                                                                    │            │
    │                                                                    └──→ S1.22 (loopzero init)
    │                                                                              │
    │   S1.8 (Integration) ─────────────────────────────────────→ S1.23 (Vertical slice E2E)
    │                                                                              │
    │                                                                    S1.24 (CHANGELOG)
```

### Ordem de execução (3 weeks)

```
Week 3 (PRIORIDADE: control plane)
  Fase 1:  S1.1 → S1.2 → S1.3      │  S1.6
                    │                │
                    ├── S1.4         │
                    └── S1.5         │
                                     │
  Fase 2 (paralelo):                 │
           S1.9 → S1.10 → S1.11     │
           S1.15, S1.16 (paralelos)  │

Week 4 (integration + API + CLI commands)
  Fase 1:  S1.7 (Phase + crash) → S1.8 (Integration test controller)
  Fase 2:  S1.12 → S1.13 → S1.14
  Fase 3:  S1.17 → S1.18 → S1.19 → S1.20

Week 5 (vertical slice)
  Fase 3:  S1.21 (POST projects + Tenant CR bridge)
           S1.22 (loopzero init — integra tudo)
           S1.23 (Vertical slice E2E)
           S1.24 (CHANGELOG)
```

---

## Exit Gate

> O exit gate é baseado em **fluxo vertical**, não em componentes individuais.
> Cada check prova uma camada da convergência.

### Gate 1: Control plane converge (OBRIGATÓRIO — bloqueia gates seguintes)

```bash
# Controller tests passam contra envtest
cd operators && make test                          # Ginkgo suite passes

# Verificar que integration test cobre todos os cenários
go test ./internal/controller/ -v -run Integration # 8 cenários passam (inclui deletion + partial cleanup)
```

### Gate 2: API responde e autentica

```bash
# Health
curl -sf http://localhost:3001/api/v1/health       # 200 {"status":"ok"}

# Auth
curl -sf -H "Authorization: Bearer $TOKEN" \
     http://localhost:3001/api/v1/auth/whoami       # 200 with user info
curl -s -o /dev/null -w "%{http_code}" \
     http://localhost:3001/api/v1/auth/whoami       # 401

# Correlation-ID cruza boundary
CORR=$(curl -sI -H "Authorization: Bearer $TOKEN" \
    http://localhost:3001/api/v1/auth/whoami | grep -i x-correlation-id)
[ -n "$CORR" ] || echo "FAIL: no correlation-id"

# Multi-tenant resolution (Model C)
# User com 1 tenant → auto-resolve
curl -sf -H "Authorization: Bearer $SINGLE_TENANT_TOKEN" \
     http://localhost:3001/api/v1/auth/whoami | jq -r .team  # Returns tenant name

# User com N tenants sem X-LoopZero-Tenant → 409
curl -s -o /dev/null -w "%{http_code}" \
     -H "Authorization: Bearer $MULTI_TENANT_TOKEN" \
     http://localhost:3001/api/v1/auth/whoami                # 409

# User com N tenants + X-LoopZero-Tenant → resolve
curl -sf -H "Authorization: Bearer $MULTI_TENANT_TOKEN" \
     -H "X-LoopZero-Tenant: acme" \
     http://localhost:3001/api/v1/auth/whoami | jq -r .team  # "acme"
```

### Gate 3: CLI autentica e gera output correto

```bash
# Auth flow
loopzero login --token $TEST_TOKEN                     # Exit 0
loopzero whoami                                        # Shows email + team
loopzero whoami --json | jq .authenticated             # true
loopzero logout                                        # Exit 0
loopzero whoami; echo $?                               # Exit 4

# No internal terms
grep -rn "ArgoCD\|argocd\|GitOps\|gitops-push\|CRD\|tenant_id\|advisory lock\|pg_advisory\|Gitea\|MinIO\|kubebuilder\|envtest\|Kaniko" \
    cli/src/ --include="*.ts" | grep -v test | grep -v ".d.ts"
# Must return 0 results
```

### Gate 4: Vertical slice — a prova final (OBRIGATÓRIO)

```bash
# O fluxo completo:
# 1. Autenticar
loopzero login --token $TOKEN

# 2. Inicializar projeto (cria project na API + Tenant CR no cluster)
cd /tmp/test-project && loopzero init --yes

# 3. Observar convergência do controller
TENANT_ID="acme"  # resolved from token
kubectl get tenant $TENANT_ID -o jsonpath='{.status.phase}'
# Deve ser "Active" dentro de 60 segundos

# 4. Verificar TODOS os objetos provisionados
kubectl get ns ${TENANT_ID}-production             # Namespace existe
kubectl get resourcequota -n ${TENANT_ID}-production  # Quota aplicada
kubectl get sa loopzero-deployer -n ${TENANT_ID}-production  # ServiceAccount existe
kubectl get role -n ${TENANT_ID}-production        # Role existe
kubectl get networkpolicy -n ${TENANT_ID}-production  # NetworkPolicy existe
kubectl get ns build-${TENANT_ID}                  # Build namespace existe

# 5. Verificar conditions do Tenant
for COND in GitRepoReady NamespacesReady RBACReady NetworkPolicyReady BuildNamespaceReady QuotaEnforced; do
    STATUS=$(kubectl get tenant $TENANT_ID -o json | \
        jq -r ".status.conditions[] | select(.type==\"$COND\") | .status")
    [ "$STATUS" = "True" ] || echo "FAIL: $COND is $STATUS"
done

# 6. ObservedGeneration consistente
GEN=$(kubectl get tenant $TENANT_ID -o json | jq '.metadata.generation')
OBS=$(kubectl get tenant $TENANT_ID -o json | jq '.status.observedGeneration')
[ "$GEN" = "$OBS" ] || echo "FAIL: generation mismatch"

# 7. Labels corretas (não ownerReference)
kubectl get ns ${TENANT_ID}-production -o json | \
    jq -r '.metadata.labels["loopzero.io/tenant"]'     # Deve ser $TENANT_ID
kubectl get ns ${TENANT_ID}-production -o json | \
    jq '.metadata.ownerReferences'                  # Deve ser null ou []

# 8. NetworkPolicy egress não permite internet aberta
kubectl get networkpolicy -n ${TENANT_ID}-production -o json | \
    grep -c "0.0.0.0/0"                             # Deve ser 0

# 9. PriorityClass existe como infra global (NÃO criado pelo controller)
kubectl get priorityclass loopzero-build-priority-starter -o json | \
    jq -r '.preemptionPolicy'                       # Deve ser "Never"
# Nota: PriorityClasses são criadas no bootstrap (task setup), não pelo TenantController

# 10. Finalizer presente no Tenant
kubectl get tenant $TENANT_ID -o json | \
    jq -r '.metadata.finalizers[]'                  # Deve conter "loopzero.io/tenant-cleanup"

# 11. Deletion cleanup funciona
kubectl delete tenant $TENANT_ID
# Aguardar cleanup (max 30s)
for i in $(seq 1 6); do
    kubectl get ns ${TENANT_ID}-production 2>/dev/null || break
    sleep 5
done
kubectl get ns ${TENANT_ID}-production 2>/dev/null && echo "FAIL: namespace not cleaned up"
```

### Gate 5: Testes e qualidade

```bash
task test:unit                                     # All unit tests pass
task lint                                          # golangci-lint + eslint
task validate:manifests                            # kubeconform
```
