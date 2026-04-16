# Avaliação de Prontidão para 100.000 Pods

**Data:** 2026-04-10  
**Escopo:** capacidade do sistema LoopZero para operar em escala de 100.000 pods  
**Objetivo:** separar limitações de implementação do sistema das limitações de dimensionamento de infraestrutura

## Resposta Executiva

Hoje, o LoopZero **não está pronto** para 100.000 pods em produção.

A conclusão correta não é "a infraestrutura atual é pequena". Isso é verdade, mas é secundário. O ponto principal é:

- a arquitetura do sistema **já aponta para a direção correta** de escala;
- a implementação **ainda não está completa** para suportar essa escala apenas com aumento de capacidade na DigitalOcean;
- existe uma diferença importante entre:
  - `dimensionar a infraestrutura`, e
  - `ter a implementação pronta para escalar quando a infraestrutura crescer`.

## Conclusão Principal

O sistema está:

- **arquiteturalmente orientado para escala**;
- **parcialmente preparado na implementação**;
- **ainda não pronto operacionalmente** para 100.000 pods.

Em outras palavras:

> aumentar nós, CPU, memória, bancos e clusters na DigitalOcean não seria suficiente, hoje, para levar o sistema com segurança a 100.000 pods.

## Evidências Principais

### 1. A própria documentação atual reconhece limites muito abaixo de 100.000 pods

O plano de carga atual registra gargalos operacionais em números ainda pequenos para esse alvo:

- PostgreSQL com limite prático em dezenas de operações concorrentes;
- build concurrency em `10`;
- release convergence em `10`;
- SSE em `200`;
- ArgoCD estimado em `~200 apps` no sizing atual;
- runtime cluster com gargalo na casa de `~40 tenant pods`.

Referência: [docs/plans/load-test-plan.md](/home/paulo/Projetos/loopzero/loopzero/docs/plans/load-test-plan.md)

### 2. A estratégia correta para 50k+ apps / 100k+ pods já existe

O ADR de arquitetura adota explicitamente `cell-based architecture` e reconhece que a escala desejada exige múltiplas células, com isolamento e particionamento:

- cada célula como unidade de execução;
- ArgoCD por célula;
- tenant affinity por célula;
- expansão linear via novas células.

Referência: [docs/meetings/adr/0008-cell-based-architecture.md](/home/paulo/Projetos/loopzero/loopzero/docs/meetings/adr/0008-cell-based-architecture.md)

### 3. A implementação multi-cell ainda não está concluída

O componente de provisionamento de células em Pulumi está explicitamente marcado como scaffold/TODO e retorna placeholders.

Referência: [infra/pulumi/src/components/cell.ts](/home/paulo/Projetos/loopzero/loopzero/infra/pulumi/src/components/cell.ts)

Isso significa que a direção arquitetural existe, mas a peça central que transforma o desenho em capacidade operacional ainda não está pronta.

### 4. A API ainda não escala horizontalmente de forma plena

O LoopZero já possui separação de planos em nível de infraestrutura, com `management`, `build` e `runtime` em clusters distintos.

O runbook de API documenta que a aplicação ainda depende de loops singleton e usa leader election para impedir processamento duplicado:

- `BuildReconcileLoop`
- `BuildReaper`
- `ReleaseReaper`
- `ReleaseConvergenceLoop`
- limpadores e pruners internos

Referência: [docs/runbooks/api-scaling.md](/home/paulo/Projetos/loopzero/loopzero/docs/runbooks/api-scaling.md)

Isso significa que a separação de planos já existe, mas o desacoplamento interno do control plane ainda não está completo. Esse é um gargalo de implementação, não de CPU/RAM.

### 5. O ambiente auditado em produção/dev está pequeno por desenho

O ambiente auditado em 2026-04-08 opera com clusters modestos e capacidade de desenvolvimento/validação, não de hyperscale.

Referência: [docs/LIVE-ENVIRONMENT-AUDIT-2026-04-08.md](/home/paulo/Projetos/loopzero/loopzero/docs/LIVE-ENVIRONMENT-AUDIT-2026-04-08.md)

Isso confirma que o estado atual está muito distante do alvo, mas não é, sozinho, a razão principal do não preparo.

## O Que Já Está Bem Encaminhado

Algumas bases importantes já existem e mostram que o sistema foi pensado para evoluir:

- `cell_id` já faz parte do modelo de tenant;
- separação entre management, build e runtime já existe em nível de cluster;
- abstrações de roteamento por célula já existem;
- gerenciamento de cliente por célula já existe;
- agregação cross-cell já existe em forma inicial;
- controles explícitos de concorrência já existem em partes centrais do sistema.

Referências:

- [operators/api/v1/tenant_types.go](/home/paulo/Projetos/loopzero/loopzero/operators/api/v1/tenant_types.go)
- [api/internal/placement/router.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/placement/router.go)
- [api/internal/cellmgr/client_manager.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/cellmgr/client_manager.go)
- [api/internal/services/cross_cell_aggregator.go](/home/paulo/Projetos/loopzero/loopzero/api/internal/services/cross_cell_aggregator.go)

Isso é relevante porque mostra que o problema não é ausência total de desenho, e sim falta de fechamento da implementação.

## O Que Ainda Bloqueia 100.000 Pods

### Implementação

- multi-cell real ainda incompleto;
- loops centrais da API ainda acoplados ao processo HTTP;
- limites de concorrência centrais ainda baixos por design;
- persistência, locks e throughput de banco ainda não ajustados para esse patamar;
- faltam validações reais progressivas em carga alta.

### Operação

- não existe evidência prática de operação validada em ordens de grandeza próximas;
- runbooks e planos existem, mas parte do caminho ainda é prospectivo;
- a documentação de carga trata níveis altos como trabalho futuro.

## O Que É Só Infra e O Que Não É

### É principalmente infra

- mais nós;
- mais node pools;
- mais memória e CPU;
- autoscaling de clusters;
- tier maior de banco;
- expansão por regiões;
- custo e capacidade na DigitalOcean.

### Não é só infra

- provisionamento real de células;
- separação dos loops singleton da API;
- desacoplamento interno suficiente do control plane para escalar independentemente da API HTTP;
- estratégia operacional de ArgoCD por célula;
- revisão da estratégia de banco e concorrência;
- validação de carga em níveis crescentes.

## Parecer Final

O LoopZero **não está pronto hoje** para 100.000 pods.

O diagnóstico correto é:

1. a infraestrutura atual está subdimensionada para esse alvo;
2. mais importante, a implementação ainda não está completa para que o simples aumento de infraestrutura resolva o problema;
3. a base arquitetural está correta e já existe;
4. o principal trabalho restante é transformar a estratégia multi-cell e a separação já existente entre clusters em um control plane internamente desacoplado, escalável e validado.

## Próximo Passo Recomendado

Antes de discutir sizing final na DigitalOcean, o sistema precisa fechar estes itens de implementação:

1. concluir multi-cell real;
2. extrair loops singleton da API;
3. definir orçamento de capacidade por célula;
4. revisar banco e controle de concorrência;
5. executar testes progressivos de carga com evidência real.

Sem isso, o aumento de infraestrutura melhora folga local, mas não entrega prontidão verdadeira para 100.000 pods.
