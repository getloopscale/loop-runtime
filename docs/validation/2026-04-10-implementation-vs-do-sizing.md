# Implementação do Sistema vs Dimensionamento na DigitalOcean

**Data:** 2026-04-10  
**Objetivo:** separar claramente o que já está pronto na implementação, o que ainda está parcial e o que depende principalmente de dimensionamento de infraestrutura

## Resumo Executivo

O ponto central é este:

- `dimensionamento na DO` é majoritariamente problema de capacidade, configuração e custo;
- `implementação do sistema` precisa estar pronta antes para que esse dimensionamento faça efeito;
- hoje, o LoopZero tem bases importantes prontas, mas ainda tem lacunas críticas de implementação para escala extrema.

Importante:

- o LoopZero já tem separação de planos em nível de infraestrutura, com `management`, `build` e `runtime` em clusters distintos;
- o que ainda falta é desacoplamento interno suficiente do control plane para escala alta.

## Matriz de Separação

| Área | Já pronto na implementação | Parcial / incompleto | Só depende de dimensionamento na DO |
|---|---|---|---|
| Modelo de escala | `cell_id` no modelo de tenant e contratos básicos de célula existem em [operators/api/v1/tenant_types.go](/home/paulo/Projetos/loopzero/loopzero/operators/api/v1/tenant_types.go) e [api/internal/placement/router.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/placement/router.go) | Multi-cell real ainda não está fechado; provisionamento de célula está scaffoldado/TODO em [infra/pulumi/src/components/cell.ts](/home/paulo/Projetos/loopzero/loopzero/infra/pulumi/src/components/cell.ts) | Depois que a implementação multi-cell existir, aumentar número de células, nós e regiões é infra |
| API stateless / horizontal | HTTP, auth, rotas e parte do roteamento já funcionam em múltiplas réplicas; a separação mgmt/build/runtime já existe | Loops centrais ainda são singleton com leader election em [docs/runbooks/api-scaling.md](/home/paulo/Projetos/loopzero/loopzero/docs/runbooks/api-scaling.md); isso bloqueia desacoplamento interno e escala horizontal plena do backend | Mais réplicas/pods da API e HPA são infra, mas hoje não resolvem o gargalo principal |
| Controle de concorrência | Há limites explícitos para build, reconcile e SSE em [api/internal/services/build_reconcile_loop.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/services/build_reconcile_loop.go), [api/internal/services/release_convergence_loop.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/services/release_convergence_loop.go) e [api/internal/sse/stream_handler.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/sse/stream_handler.go) | Os limites atuais são baixos por design e precisariam ser redesenhados/testados para escala alta; não é só “aumentar número” | Ajustar valores e dar mais CPU/mem ajuda, mas só depois do redesenho dos pontos centrais |
| Kubernetes por célula | Cliente por célula e abstrações básicas existem em [api/internal/cellmgr/client_manager.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/cellmgr/client_manager.go) | Operação multi-cell de verdade ainda não está consolidada ponta a ponta | Tamanho dos clusters, node pools, autoscaling e distribuição por região são infra |
| ArgoCD / GitOps | Uso de ArgoCD está integrado ao sistema e a arquitetura futura por célula está definida em [docs/meetings/adr/0008-cell-based-architecture.md](/home/paulo/Projetos/loopzero/loopzero/docs/meetings/adr/0008-cell-based-architecture.md) | Sharding, células e isolamento operacional em larga escala ainda são meta arquitetural, não estado validado hoje | Memória, CPU, número de réplicas e tamanho dos clusters ArgoCD são infra |
| Banco / estado | Já existe disciplina de locks, idempotência e estrutura para controle de estado | Pool, advisory locks, throughput e particionamento por célula ainda não estão no ponto para escala extrema; o próprio plano aponta isso em [docs/plans/load-test-plan.md](/home/paulo/Projetos/loopzero/loopzero/docs/plans/load-test-plan.md) | Escalar tier do PostgreSQL, réplicas e conexões é infra, mas não substitui mudanças de desenho |
| Build plane | Pipeline existe, com quotas, workers e isolamento em namespaces | Capacidade atual é deliberadamente pequena; build plane para grande volume exigirá revisão de arquitetura e concorrência além de mais nós | Mais nós no build cluster e autoscaling são infra |
| Observabilidade | Há monitoramento, dashboards e runbooks | Observabilidade federada entre muitas células ainda é parcial/planejada | Retenção, recursos, storage e scrape scale são infra |
| Teste de carga | Existe plano e harness de load test em [infra/tests/load/cell_scale_test.go](/home/paulo/Projetos/loopzero/loopzero/infra/tests/load/cell_scale_test.go) | A validação real em níveis altos ainda não aconteceu; isso é lacuna de evidência do sistema | Ambientes maiores para executar os testes são infra |

## Leitura Correta da Situação

### O que já pode ser considerado base pronta

- direção arquitetural correta;
- abstrações centrais para célula;
- modelo de tenant preparado para célula;
- alguns controles explícitos de concorrência;
- organização operacional com planos, ADRs e runbooks.

### O que ainda não pode ser tratado como pronto

- multi-cell em produção real;
- plano de controle internamente desacoplado e horizontalmente escalável sem gargalo singleton;
- banco pronto para throughput extremo por célula;
- validação de capacidade em grande escala com evidência operacional.

### O que é erro de interpretação

É erro dizer:

> "o sistema já está pronto, falta só aumentar a infra na DO"

O correto é:

> "o sistema tem base arquitetural para escalar, mas ainda faltam implementações críticas para que o aumento de infra funcione com segurança"

## Conclusão

A divisão correta é:

### Já pronto

- base de arquitetura para célula;
- contratos e abstrações iniciais;
- parte do controle operacional.

### Parcial / incompleto

- multi-cell real;
- escala horizontal plena da API;
- estratégia de estado e throughput em grande escala;
- validação real em carga alta.

### Só infra

- mais clusters;
- mais nós;
- mais CPU e memória;
- autoscaling;
- maior tier de banco;
- distribuição regional;
- orçamento e custo na DigitalOcean.

## Parecer Final

Hoje, aumentar a infraestrutura na DigitalOcean **não é suficiente** por si só.

Primeiro, a implementação do sistema precisa fechar os pontos críticos de escala. Depois disso, o dimensionamento da DO passa a ser o principal limitador.
