# Operators SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo`
- `Critério de aprovação`
- `Evidência esperada`
- `Notas LoopZero`

Todos os gates estão escritos no contexto de uma PaaS Kubernetes. Isso muda a régua: não basta o controller "funcionar". Ele precisa sustentar abstração de produto, isolamento e operação confiável.

Regra transversal de validação:

- unit tests e envtest são necessários;
- para controllers críticos, envtest ainda não basta sem prova em cluster real ou production-parity, porque isolamento, finalizers, cache, RBAC e multi-cluster behavior falham justamente no mundo real.

## Gate OP-G01: Reconcile Idempotence

`Objetivo`

Garantir que múltiplos reconciles levem ao mesmo estado sem side effects redundantes ou corrupção.

`Critério de aprovação`

- O reconcile pode rodar repetidamente com o mesmo spec sem quebrar recursos.
- Reexecução após update/status event não cria drift.
- A implementação usa `CreateOrUpdate`, `Ensure*` ou equivalente sempre que aplicável.

`Evidência esperada`

- reconcile sem dependência de ordem implícita;
- ausência de flags locais para "já fiz";
- testes de rerun/reconcile repetido.

`Notas LoopZero`

- O `TenantReconciler` já declara execução incondicional e idempotente dos passos.
- Esse padrão precisa ser preservado em todos os domínios.

## Gate OP-G02: Crash-Safe Progression

`Objetivo`

Permitir retomada segura após crash, restart do pod ou failover de leader.

`Critério de aprovação`

- O progresso relevante é persistido em status, child resources ou ambos.
- O reconcile retoma do estado observado, não de memória local.
- O controller não assume que todos os passos de uma fase ocorreram no mesmo loop.

`Evidência esperada`

- status persistido;
- state machine ou conditions refletindo progresso;
- testes cobrindo retomada parcial.

`Notas LoopZero`

- `PlatformBootstrap` e `PreviewEnvironment` já mostram progressão por fase.

## Gate OP-G03: Status As Contract

`Objetivo`

Fazer do status a superfície confiável para API, CLI e operação.

`Critério de aprovação`

- Recursos com status usam subresource `/status`.
- Conditions têm `type`, `status`, `reason`, `message` e `observedGeneration`.
- Existe condição sumária (`Ready` ou equivalente) quando o recurso representa processo duradouro.
- O status permite distinguir progresso, sucesso, bloqueio e falha.

`Evidência esperada`

- helpers centrais para `SetCondition`;
- CRDs com subresource status;
- testes validando conditions.

`Notas LoopZero`

- `operators/internal/conditions/conditions.go` já força `ObservedGeneration`.
- Para uma PaaS, isso é crítico porque CLI e API dependem de status legível.

## Gate OP-G04: Freshness Via ObservedGeneration

`Objetivo`

Evitar leituras stale após mudanças em `spec`.

`Critério de aprovação`

- Conditions relevantes carregam `observedGeneration`.
- Leitores não tratam uma condition antiga como verdade atual.
- Gate de "all ready" considera a geração observada ou atualiza status adequadamente.

`Evidência esperada`

- helpers como `IsConditionTrueForGeneration`;
- checagens explícitas de freshness.

`Notas LoopZero`

- O helper já existe. O gate é garantir uso consistente, especialmente em preview e bootstrap.

## Gate OP-G05: Finalizer Discipline

`Objetivo`

Usar finalizers apenas quando existe cleanup assíncrono real e controlável.

`Critério de aprovação`

- Finalizer só é adicionado quando o recurso realmente possui responsabilidade externa a limpar.
- Deleção vira fluxo observável e idempotente.
- Ordem entre finalizers não é assumida.
- Remoção do finalizer ocorre apenas após cleanup satisfatório ou decisão explícita de abandono seguro.

`Evidência esperada`

- blocos de `DeletionTimestamp` claros;
- cleanup separado por função;
- testes de delete e retry.

`Notas LoopZero`

- `Tenant`, `PreviewEnvironment` e `PlatformBootstrap` já usam finalizers e são recursos de maior risco operacional.

## Gate OP-G06: Separation Of Spec, Status And External State

`Objetivo`

Evitar CRDs que misturam intenção do usuário com observação ou cache de recurso externo.

`Critério de aprovação`

- `spec` contém somente intenção e parâmetros declarativos.
- `status` contém somente observação, fase e conditions.
- IDs/snapshots externos em status só existem quando necessários para recovery, auditoria ou correlacionamento.

`Evidência esperada`

- CRDs legíveis e com fronteira clara;
- ausência de espelhamento desnecessário de objetos downstream em status.

`Notas LoopZero`

- Em uma PaaS, CRD ruim vira contrato ruim para todo o produto.

## Gate OP-G07: PaaS Abstraction Integrity

`Objetivo`

Garantir que os operators sirvam ao modelo de produto, e não vazem semântica interna do cluster.

`Critério de aprovação`

- CRDs representam conceitos de produto, como tenant, environment, app e preview.
- Recursos downstream são detalhe de implementação.
- O operator não força o usuário a modelar Deployments, Services ou policies diretamente para usar o produto.

`Evidência esperada`

- domínio das CRDs alinhado ao modelo LoopZero;
- lógica tradutora dentro dos controllers.

`Notas LoopZero`

- Esse gate diferencia um operator de plataforma de um simples "manifest generator".

## Gate OP-G08: Isolation By Default

`Objetivo`

Fazer de isolamento um default do sistema, não uma convenção opcional.

`Critério de aprovação`

- Tenant, environment e preview criam namespaces, quotas e policies previsíveis.
- Escopo de cache restringe visão ao namespace correto quando aplicável.
- Recursos de platform system ficam isolados do runtime normal.

`Evidência esperada`

- network policies e quotas reconciliadas;
- cache scoping por objeto/namespace;
- testes de isolamento.

`Notas LoopZero`

- `tenant-operator` e `platform-operator` já usam scoping explícito de cache.

## Gate OP-G09: External Dependency Containment

`Objetivo`

Evitar que Gitea, ArgoCD, Harbor, MinIO ou APIs externas dominem o lifecycle do controller.

`Critério de aprovação`

- Dependências externas são encapsuladas por interfaces/provisioners.
- Falhas transitórias são tratadas com retry/backoff/circuit breaker quando apropriado.
- O controller consegue refletir indisponibilidade sem corromper status ou child resources.

`Evidência esperada`

- provisioners dedicados;
- clients injetáveis;
- circuit breaker onde risco operacional justificar.

`Notas LoopZero`

- O LoopZero já faz isso em partes do tenant/platform operator. O gate formaliza essa exigência.

## Gate OP-G10: Multi-Cluster Correctness

`Objetivo`

Tratar build cluster, runtime cluster e mgmt cluster como boundaries reais.

`Critério de aprovação`

- Cada client aponta explicitamente para o cluster certo.
- Erros de cluster remoto não são confundidos com erro do mgmt cluster.
- Recursos são reconciliados no cluster correto de forma auditável.

`Evidência esperada`

- clientes separados por domínio;
- logs/metrics com contexto de cluster;
- testes de wiring e segurança concorrente.

`Notas LoopZero`

- O LoopZero é uma PaaS multi-plano; esse gate é central, não opcional.

## Gate OP-G11: State Machines For Lifecycles

`Objetivo`

Evitar progressão ad hoc em fluxos multi-etapa.

`Critério de aprovação`

- Recursos com lifecycle explícito usam state machine ou helper equivalente.
- Transições inválidas são detectadas, não absorvidas silenciosamente.
- Fases terminais são claras.

`Evidência esperada`

- uso do `pkg/statemachine`;
- ausência de strings de fase atualizadas arbitrariamente.

`Notas LoopZero`

- `Tenant`, `PreviewEnvironment` e `PlatformBootstrap` já apontam nessa direção.

## Gate OP-G12: Drift Correction Over One-Time Provisioning

`Objetivo`

Garantir que o operator continue sendo controller, não um script de bootstrap disfarçado.

`Critério de aprovação`

- O reconcile corrige drift de child resources mesmo após condição previamente true.
- Passos não são pulados apenas porque "já passaram uma vez".
- Desired state continua sendo derivado do spec atual.

`Evidência esperada`

- reconciles completos e idempotentes;
- comentários/código explicitando correção de drift.

`Notas LoopZero`

- O `TenantReconciler` explicita esse princípio. Deve virar padrão de módulo.

## Gate OP-G13: Wiring Completeness

`Objetivo`

Eliminar a classe "implemented but not wired".

`Critério de aprovação`

- Todo controller exportado e esperado em produção está registrado no binário correto.
- Não há chamadas órfãs para setup inexistente.
- Mudanças na decomposição por binário preservam cobertura de wiring.

`Evidência esperada`

- testes estruturais de wiring;
- `main.go`/binários com setup explícito.

`Notas LoopZero`

- `operators/cmd/registration_test.go` já é evidência importante desse gate.

## Gate OP-G14: Observability Native To Kubernetes

`Objetivo`

Permitir diagnóstico operacional real em produção.

`Critério de aprovação`

- controller expõe metrics, healthz e readyz;
- logs seguem convenções Kubernetes e são estruturados;
- tracing existe para loops críticos e integrações externas;
- debug surfaces como pprof são controladas explicitamente.

`Evidência esperada`

- endpoints de health/ready;
- package de metrics;
- tracing compartilhado;
- guardas de enablement de pprof.

`Notas LoopZero`

- Todos os binários split já mostram esse padrão em grau relevante.

## Gate OP-G15: Test Matrix For Controllers

`Objetivo`

Provar comportamento, não apenas compilar a intenção.

`Critério de aprovação`

- unit tests cobrem helpers e edge cases locais;
- envtest cobre reconcile e status;
- integração cobre invariantes sistêmicas e concorrência;
- recursos com finalizer possuem testes de delete/retry;
- recursos com multi-step lifecycle possuem testes de fase.

`Evidência esperada`

- `operators/internal/controller/*_test.go`
- `operators/operatortest/*`

`Notas LoopZero`

- Para uma PaaS, preview, tenant delete, bootstrap e isolation devem sempre estar entre as áreas mais testadas.

## Gate OP-G16: Upgrade And API Evolution Safety

`Objetivo`

Permitir evolução das CRDs e controllers sem quebrar instalações existentes.

`Critério de aprovação`

- CRDs têm schema claro e versionamento disciplinado.
- Mudanças em status/spec consideram compatibilidade.
- Controllers lidam com objetos existentes de versões anteriores quando aplicável.

`Evidência esperada`

- manifests de CRD gerados e auditáveis;
- documentação de mudança de contrato;
- ausência de dependência em campos transitórios implícitos.

`Notas LoopZero`

- Em uma PaaS, CRD é API pública entre componentes, mesmo que não seja API de usuário final direta.

## Ordem De Prioridade

1. `OP-G01`, `OP-G02`, `OP-G03`, `OP-G05`, `OP-G08`
2. `OP-G04`, `OP-G09`, `OP-G10`, `OP-G11`, `OP-G12`
3. `OP-G13`, `OP-G14`, `OP-G15`, `OP-G16`, `OP-G07`

Justificativa:

- primeiro, segurança de reconcile e isolamento;
- depois, resiliência e modelagem correta;
- por fim, disciplina estrutural, observabilidade e evolução.
