# Roadmap para Maturidade de 100.000 Pods

**Data:** 2026-04-10  
**Objetivo:** consolidar um roadmap único para levar o LoopZero do estado atual a um nível de maturidade compatível com operação em escala de 100.000 pods  
**Escopo:** implementação do sistema, operação, validação progressiva e critérios objetivos de saída por fase

## Resumo Executivo

Sim, existe um caminho claro.

O roadmap implícito já aparece em:

- [docs/meetings/adr/0008-cell-based-architecture.md](/home/paulo/Projetos/loopzero/loopzero/docs/meetings/adr/0008-cell-based-architecture.md)
- [docs/plans/load-test-plan.md](/home/paulo/Projetos/loopzero/loopzero/docs/plans/load-test-plan.md)
- [docs/runbooks/api-scaling.md](/home/paulo/Projetos/loopzero/loopzero/docs/runbooks/api-scaling.md)
- [docs/runbooks/cell-operations.md](/home/paulo/Projetos/loopzero/loopzero/docs/runbooks/cell-operations.md)

O que faltava era consolidar isso em um plano único de execução e maturidade.

## Princípios do Roadmap

Este roadmap assume quatro princípios:

1. `Scale-out` vence `scale-up`.
2. `Cells` são a unidade principal de escala e isolamento.
3. O LoopZero já possui separação de planos por cluster, mas ainda precisa de desacoplamento interno suficiente do control plane.
4. Nenhuma fase é considerada concluída apenas por implementação; todas exigem validação operacional e evidência de carga.

## Escala de Maturidade

### Níveis

- `M0`: arquitetura e bases parciais
- `M1`: control plane preparado para escalar localmente
- `M2`: single-cell robusta
- `M3`: multi-cell funcional
- `M4`: multi-cell validada sob carga alta
- `M5`: maturidade compatível com trajetória para 100.000 pods

### Leitura

O LoopZero hoje está aproximadamente entre `M0` e `M1`.

## Visão Geral das Fases

| Fase | Nome | Resultado principal |
|---|---|---|
| 0 | Consolidação da base | fechar lacunas óbvias do control plane atual |
| 1 | Desacoplamento interno do control plane | API deixa de carregar o núcleo operacional singleton |
| 2 | Single-cell robusta | uma célula é operável, mensurável e testável com limites explícitos |
| 3 | Multi-cell real | células deixam de ser abstração parcial e viram capacidade operacional |
| 4 | Escala progressiva validada | sistema prova comportamento sob centenas a milhares de tenants |
| 5 | Preparação para 100.000 pods | governança, capacity model e readiness real para expansão agressiva |

## Fase 0 — Consolidação da Base

### Objetivo

Fechar lacunas fundamentais do estado atual para que as fases seguintes não sejam construídas sobre premissas frágeis.

### Escopo

- consolidar observabilidade básica;
- revisar limites atuais do sistema;
- tornar explícitos os gargalos conhecidos;
- eliminar ambiguidades de arquitetura entre o que já existe e o que ainda é roadmap.

### Itens principais

1. Consolidar documentação única de arquitetura atual.
2. Consolidar documentação de limites correntes por componente.
3. Fechar lacunas de métricas essenciais em API, ArgoCD, Argo Workflows e banco.
4. Definir baseline operacional por cluster:
   - management
   - build
   - runtime
5. Publicar capacity budget inicial por componente.

### Dependências

- nenhuma dependência estrutural relevante; é a base para as próximas fases.

### Critérios de aceite

- existe uma visão única e atualizada do estado da plataforma;
- todos os gargalos conhecidos estão documentados com evidência;
- os limites correntes do sistema não dependem mais de inferência informal;
- dashboards e métricas mínimas para API, ArgoCD, Argo Workflows e PostgreSQL estão acessíveis.

### DoD

- documento de arquitetura atual revisado;
- documento de capacidade atual por componente revisado;
- alertas mínimos definidos;
- baseline de carga e saúde registrada;
- backlog técnico de escala priorizado e aceito pela equipe.

### Complexidade

- `2 / 5`

## Fase 1 — Desacoplamento Interno do Control Plane

### Objetivo

Separar o núcleo operacional do LoopZero do processo HTTP da API.

### Contexto

O LoopZero já tem separação de planos por cluster, mas ainda concentra loops centrais dentro da API, com leader election e gargalos singleton.

### Escopo

- extrair loops singleton da API;
- tornar a API predominantemente stateless;
- permitir escala horizontal real da camada HTTP;
- separar claramente:
  - API
  - reconciliadores
  - reapers
  - convergence workers

### Itens principais

1. Extrair `BuildReconcileLoop`.
2. Extrair `ReleaseConvergenceLoop`.
3. Extrair `BuildReaper` e `ReleaseReaper`.
4. Definir mecanismo operacional dos workers extraídos:
   - deployment dedicado
   - cronjob, se aplicável
   - controller, se aplicável
5. Garantir compatibilidade com:
   - idempotência
   - locks existentes
   - ordenação esperada das transições
6. Garantir que API em múltiplas réplicas não altere o comportamento do sistema.

### Dependências

- Fase 0 concluída.

### Critérios de aceite

- API pode rodar com múltiplas réplicas sem risco de duplicação de processamento central;
- reconciliadores e reapers podem ser escalados e observados separadamente;
- não existe mais dependência crítica de leader election no binário HTTP para funcionamento principal do sistema;
- throughput da API melhora sob concorrência sem regressão funcional.

### DoD

- loops centrais extraídos do processo HTTP;
- testes de regressão cobrindo duplicação, idempotência e race conditions;
- dashboards separados por componente operacional;
- deploy em ambiente real com múltiplas réplicas da API validado;
- runbook operacional atualizado.

### Complexidade

- `4 / 5`

## Fase 2 — Single-Cell Robusta

### Objetivo

Transformar a célula padrão atual em uma unidade de capacidade realmente conhecida, previsível e testável.

### Escopo

- definir limites máximos de uma célula;
- revisar budgets de:
  - pods
  - aplicações
  - syncs
  - builds
  - conexões
- reforçar observabilidade e isolamento;
- estabelecer tuning explícito de ArgoCD, Argo Workflows e banco para uma única célula madura.

### Itens principais

1. Definir `cell budget` oficial:
   - apps por célula
   - pods por célula
   - tenants por célula
   - builds concorrentes por célula
2. Revisar PostgreSQL:
   - pool
   - PgBouncer, se necessário
   - locks
   - contenção
3. Revisar ArgoCD:
   - processors
   - cache
   - repo-server parallelism
   - sizing de controller
4. Revisar Argo Workflows:
   - workflowWorkers
   - podWorkers
   - parallelism
   - GC
   - quotas
5. Tornar métricas de saturação parte da operação padrão.

### Dependências

- Fase 1 concluída.

### Critérios de aceite

- a equipe consegue responder com dados qual é a capacidade segura de uma célula;
- os principais limites por componente estão instrumentados;
- uma única célula não depende mais de tuning implícito ou acidental;
- há testes de carga progressiva mostrando estabilidade até o limite alvo da célula.

### DoD

- documento oficial de capacidade por célula publicado;
- dashboards de saturação por célula publicados;
- tuning de ArgoCD e Argo Workflows versionado;
- plano de reação a saturação documentado;
- teste de carga da célula executado com relatório arquivado.

### Complexidade

- `4 / 5`

## Fase 3 — Multi-Cell Real

### Objetivo

Sair do estado atual de base parcial e tornar multi-cell uma capacidade operacional real.

### Escopo

- provisionamento real de células;
- roteamento por célula;
- credenciais e integração por célula;
- operação e visibilidade por célula;
- onboarding de tenants em células específicas.

### Itens principais

1. Implementar provisionamento real em:
   - [infra/pulumi/src/components/cell.ts](/home/paulo/Projetos/loopzero/loopzero/infra/pulumi/src/components/cell.ts)
2. Tornar `cell_id` efetivamente operacional ponta a ponta.
3. Integrar `ClusterCredential` e discovery real de células.
4. Garantir que componentes dependentes resolvam célula corretamente:
   - Kubernetes clients
   - ArgoCD clients
   - status aggregation
5. Definir operação mínima para:
   - provisionar célula
   - verificar saúde
   - esvaziar célula
   - descomissionar célula

### Dependências

- Fase 2 concluída.

### Critérios de aceite

- é possível criar uma segunda célula real;
- novos tenants podem ser alocados corretamente;
- operações de deploy respeitam a célula atribuída;
- falhas em uma célula não comprometem o funcionamento básico das demais;
- existe visão operacional por célula.

### DoD

- pelo menos duas células reais operando;
- deploy de tenants em células diferentes validado;
- runbooks de provisionamento e falha testados;
- observabilidade por célula ativa;
- teste de blast radius executado.

### Complexidade

- `5 / 5`

## Fase 4 — Escala Progressiva Validada

### Objetivo

Substituir hipóteses por evidência em carga progressiva.

### Escopo

- executar testes progressivos em degraus;
- validar estabilidade;
- identificar limites reais por célula e do plano de controle;
- corrigir gargalos encontrados.

### Degraus sugeridos

1. `10 tenants`
2. `50 tenants`
3. `100 tenants`
4. `500 tenants`
5. `1.000 tenants`
6. cenário multi-cell

### Itens principais

1. Executar o plano de carga já definido.
2. Capturar:
   - latência
   - saturação
   - filas
   - falhas
   - recovery
3. Corrigir gargalos por rodada.
4. Repetir testes após cada ajuste relevante.

### Dependências

- Fase 3 concluída.

### Critérios de aceite

- cada degrau tem relatório formal;
- o sistema degrada graciosamente quando ultrapassa limites;
- os limites observados são coerentes com o capacity budget;
- não há lacunas graves entre arquitetura pretendida e comportamento observado.

### DoD

- relatórios arquivados por nível de carga;
- capacidade segura por célula revisada com dados reais;
- gargalos críticos corrigidos ou explicitamente aceitos;
- critérios de rollback e recovery validados sob carga.

### Complexidade

- `4 / 5`

## Fase 5 — Preparação para 100.000 Pods

### Objetivo

Chegar a um nível de maturidade em que a conversa sobre 100.000 pods deixe de ser especulativa e passe a ser uma questão de expansão planejada.

### Escopo

- capacity model consolidado;
- estratégia de expansão por células;
- readiness para grandes volumes;
- política de governança e operação em escala.

### Itens principais

1. Definir modelo de capacidade:
   - pods por célula
   - apps por célula
   - tenants por célula
   - clusters por região
2. Definir política de expansão:
   - quando abrir nova célula
   - quando reequilibrar tenants
   - quando ampliar bancos e control planes
3. Definir política de falha:
   - blast radius
   - failover
   - recuperação
4. Definir política de canary por célula.
5. Consolidar custo de escala.

### Dependências

- Fase 4 concluída.

### Critérios de aceite

- existe um modelo de capacidade rastreável até 100.000 pods;
- a capacidade máxima de uma célula é conhecida e testada;
- a expansão para novas células é operacionalmente repetível;
- a equipe consegue explicar como crescer sem romper o blast radius planejado;
- a estratégia de scaling da DO passa a ser subordinada ao modelo de célula.

### DoD

- documento oficial de capacity model aprovado;
- política de criação e operação de células aprovada;
- simulação ou piloto multi-cell com capacidade projetada concluído;
- orçamento de crescimento por faixa de escala documentado;
- decisão explícita de readiness ou não-readiness para trajetória de 100.000 pods.

### Complexidade

- `5 / 5`

## Roadmap Consolidado por Prazo

### Curto prazo

- Fase 0
- Fase 1

### Médio prazo

- Fase 2
- Fase 3

### Longo prazo

- Fase 4
- Fase 5

## Dependências Críticas

As dependências com maior risco são:

1. extração dos loops centrais da API;
2. provisionamento real de células;
3. revisão de banco, locks e throughput;
4. tuning de ArgoCD e Argo Workflows com dados reais;
5. disciplina de testes de carga repetíveis.

## Riscos de Execução

| Risco | Impacto | Mitigação |
|---|---|---|
| Tratar escala como problema apenas de infra | Alto | manter separação entre implementação e sizing |
| Pular validação progressiva | Alto | nenhuma fase fecha sem relatório real |
| Multi-cell incompleto virar dependência invisível | Alto | Fase 3 com DoD operacional explícito |
| Banco virar gargalo sistêmico | Alto | revisar pool, locks e arquitetura antes da expansão |
| ArgoCD/Workflows em modo default | Médio | tuning e sharding tratados como trabalho explícito |

## Critério Final de Prontidão

O LoopZero só deve ser considerado realmente maduro para perseguir operação em escala de 100.000 pods quando estes pontos estiverem verdadeiros ao mesmo tempo:

1. API HTTP escala sem carregar o núcleo singleton do sistema.
2. Existe multi-cell real em produção.
3. Cada célula possui orçamento de capacidade conhecido e testado.
4. Banco, ArgoCD e Argo Workflows possuem tuning validado para a capacidade de cada célula.
5. Há evidência de carga progressiva em múltiplos níveis.
6. Falhas ficam contidas dentro do blast radius planejado.
7. A expansão da infraestrutura vira consequência do modelo, e não substituto da implementação.

## Parecer Final

Existe, sim, um roadmap claro.

Ele pode ser resumido assim:

1. fortalecer a base;
2. desacoplar internamente o control plane;
3. tornar uma célula robusta;
4. tornar múltiplas células reais;
5. validar carga progressivamente;
6. só então declarar maturidade para trajetória de 100.000 pods.

Esse roadmap é tecnicamente coerente com:

- a arquitetura já escolhida pelo LoopZero;
- os gargalos já identificados;
- e os padrões observados em sistemas reais de grande escala.
