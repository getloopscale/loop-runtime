# Operators Research Basis

## Objetivo

Este documento registra a base de pesquisa usada para definir os gates SOTA dos operators do LoopZero.

Ele combina:

- documentação oficial do Kubernetes e Kubebuilder;
- convenções oficiais de API/status/finalizers;
- evidências do código atual do módulo `operators/`;
- referências de PaaS construídas sobre Kubernetes.

## Fontes Externas Principais

### 1. Kubernetes Operator Pattern

Fonte:

- https://kubernetes.io/docs/concepts/extend-kubernetes/operator/

Pontos relevantes:

- operators usam CRs + controllers para manter estado desejado;
- o núcleo é o control loop;
- operators existem para automatizar gestão operacional de sistemas complexos.

Gates fundamentados:

- `OP-G01`
- `OP-G02`
- `OP-G07`
- `OP-G12`

Por que importa no LoopZero:

- LoopZero é PaaS; nossos operators encapsulam automação de plataforma, não apenas recursos genéricos do cluster.

### 2. Kubebuilder Good Practices

Fonte:

- https://book.kubebuilder.io/reference/good-practices

Pontos relevantes:

- reconciliation precisa ser idempotente;
- conditions alinham CRDs ao ecossistema Kubernetes;
- logging e observabilidade devem seguir convenções Kubernetes.

Gates fundamentados:

- `OP-G01`
- `OP-G03`
- `OP-G14`

Por que importa no LoopZero:

- CLI, API e operadores dependem de status legível e comportamento previsível do reconcile.

### 3. Kubernetes API Concepts

Fonte:

- https://kubernetes.io/docs/reference/using-api/api-concepts/

Pontos relevantes:

- deleção ocorre em fases, com finalization antes de remoção;
- finalizers não têm ordem garantida;
- clients devem tolerar semântica real de watch e staleness.

Gates fundamentados:

- `OP-G02`
- `OP-G05`
- `OP-G10`

Por que importa no LoopZero:

- finalizers de tenant/preview/bootstrap não podem assumir ordem mágica;
- em multi-cluster, staleness e retry importam muito.

### 4. Gateway API Conditions Guidance

Fonte:

- https://gateway-api.sigs.k8s.io/geps/gep-1364/

Pontos relevantes:

- conditions devem usar `type`, `status`, `reason`, `observedGeneration`;
- é útil ter condição sumária como `Ready`;
- conditions devem refletir estado atual e não histórico difuso.

Gates fundamentados:

- `OP-G03`
- `OP-G04`

Por que importa no LoopZero:

- CRDs de produto precisam ser auditáveis por ferramentas e por humanos.

### 5. Kubebuilder Finalizers

Fonte:

- https://book-v3.book.kubebuilder.io/reference/using-finalizers

Pontos relevantes:

- finalizer é mecanismo de pre-delete assíncrono;
- deleção vira update com `deletionTimestamp`.

Gates fundamentados:

- `OP-G05`

### 6. Kubebuilder Status Subresource

Fontes:

- https://book-v1.book.kubebuilder.io/basics/status_subresource.html
- https://master.book.kubebuilder.io/reference/generating-crd.html

Pontos relevantes:

- recursos com status devem expor `/status`;
- update de status deve ser separado do update do recurso principal.

Gates fundamentados:

- `OP-G03`
- `OP-G16`

## Evidências Internas Do LoopZero

### Split de binários e boundaries

Arquivos:

- `operators/cmd/tenant-operator/main.go`
- `operators/cmd/app-operator/main.go`
- `operators/cmd/preview-operator/main.go`
- `operators/cmd/platform-operator/main.go`

O que sustentam:

- decomposição por domínio;
- leader election habilitada;
- healthz/readyz;
- cache scoping por namespace em domínios sensíveis;
- clients multi-cluster explícitos.

### Conditions com observed generation

Arquivos:

- `operators/internal/conditions/conditions.go`
- `operators/internal/conditions/conditions_test.go`

O que sustentam:

- `ObservedGeneration` como parte obrigatória da atualização de condition;
- helper para freshness por geração.

### Preview lifecycle e cleanup

Arquivos:

- `operators/internal/controller/preview_controller.go`
- `operators/internal/controller/preview_controller_envtest_test.go`
- `operators/internal/controller/preview_controller_unit_test.go`

O que sustentam:

- finalizer;
- lifecycle com phase machine;
- namespace efêmero, quota, policy, URL e gating de workload.

### Tenant orchestration e drift correction

Arquivos:

- `operators/internal/controller/tenant_controller.go`
- `operators/internal/controller/tenant_*`

O que sustentam:

- passos idempotentes;
- correção de drift em vez de one-shot provisioning;
- multi-cluster wiring;
- provisioners externos encapsulados.

### Bootstrap progressivo

Arquivos:

- `operators/internal/controller/platformbootstrap_controller.go`
- `operators/internal/controller/platformbootstrap_*`

O que sustentam:

- state machine;
- retry count;
- transição por fases;
- finalizer e cleanup de recursos próprios.

### Garantia contra “implemented but not wired”

Arquivos:

- `operators/cmd/registration_test.go`

O que sustentam:

- teste estrutural específico para wiring de controllers;
- proteção contra regressão sistêmica importante para módulo grande.

### Concorrência e segurança sistêmica

Arquivos:

- `operators/operatortest/concurrent_safety_test.go`

O que sustentam:

- preocupação explícita com segurança concorrente no módulo.

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) não foram escolhidos por preferência genérica de engenharia. Eles existem porque:

1. São coerentes com as convenções oficiais do ecossistema Kubernetes.
2. São necessários para operadores que sustentam uma PaaS multi-tenant e multi-cluster.
3. Podem ser auditados diretamente no código atual do LoopZero.
