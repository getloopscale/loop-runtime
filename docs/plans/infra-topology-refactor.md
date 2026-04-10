# Infra Topology Refactor Plan

> Objetivo: deixar a infraestrutura do LoopZero pronta para crescer por configuração declarativa, com suporte a múltiplos clusters por papel (`runtime`, `build`, `mgmt`) sem refatoração estrutural a cada expansão.
> Data: 2026-04-10
> Status: Proposed

---

## Resposta Curta

Hoje a infra do LoopZero tem uma base boa, mas ainda está presa ao modelo fixo `mgmt + build + runtime` em stack única. Isso é suficiente para o estado atual, mas não para o roadmap multi-cell.

O alvo deste plano é simples:

- criar topologia declarativa por stack;
- permitir `1..N` clusters por papel;
- suportar cells reais;
- manter compatibilidade com o stack atual;
- usar env vars apenas como override operacional, nunca como fonte primária da topologia.

---

## Princípios

1. Topologia é configuração, não código.
2. Multi-cluster deve ser declarativo, não espalhado em `if` por componente.
3. `runtime`, `build` e `mgmt` devem suportar `1..N` instâncias.
4. `cell` é unidade lógica de operação, não apenas naming.
5. Compatibilidade retroativa é obrigatória.
6. Env vars são override de CI e ambientes efêmeros, não contrato principal.

---

## Estado Atual

### O que já existe

- separação física entre `mgmt`, `build` e `runtime` no Pulumi;
- bootstrap e observabilidade já razoavelmente estruturados;
- dashboards e runbooks já apontando para operação por cell;
- componente `cell.ts` com interface inicial;
- base documental forte para capacidade, cells e load validation.

### O que ainda bloqueia

- o stack principal ainda assume exatamente 1 cluster por papel;
- `cell.ts` ainda é scaffold e retorna placeholders;
- o stack atual não carrega nem consome `loopzero:cells`;
- runbook de cells está à frente da implementação real;
- outputs ainda não formam um registry central de clusters/cells;
- testes de infra ainda não validam topologias múltiplas.

---

## Decisão de Modelagem

Não usar “quantidade de clusters” como única variável solta de ambiente.

Em vez disso, adotar um modelo declarativo de topologia no stack config, com env var opcional para override.

### Fonte primária

`Pulumi.<stack>.yaml`

### Fonte secundária

Env vars para CI, preview stacks e experimentos controlados:

- `LOOPZERO_TOPOLOGY_JSON`
- `LOOPZERO_RUNTIME_CLUSTER_COUNT`
- `LOOPZERO_BUILD_CLUSTER_COUNT`
- `LOOPZERO_MGMT_CLUSTER_COUNT`

Essas env vars só podem sobrescrever a topologia declarada. Não devem virar a forma canônica de operar produção.

### Regra de materialização dos overrides

Overrides por contagem só são válidos quando a stack já tiver metadados suficientes para expandir a topologia sem inferência ambígua.

- `LOOPZERO_TOPOLOGY_JSON` pode substituir a topologia inteira em ambientes efêmeros, desde que passe pela mesma validação do stack config;
- `LOOPZERO_RUNTIME_CLUSTER_COUNT` só pode replicar um template de runtime já definido, preservando `region`, sizing, labels e uma regra determinística de geração de `cellId`/`clusterId`;
- `LOOPZERO_BUILD_CLUSTER_COUNT` e `LOOPZERO_MGMT_CLUSTER_COUNT` seguem a mesma regra: apenas expandem templates declarados, nunca inventam metadados ausentes;
- se o override exigir inferir `cellId`, `region`, naming ou sizing sem template explícito, o loader deve falhar.

---

## Topologia Alvo

Exemplo conceitual:

```yaml
config:
  loopzero-infra:topology:
    controlPlane:
      mgmt:
        clusters:
          - id: mgmt-default
            region: sfo3
            nodeClass: small
            minNodes: 3
            maxNodes: 3

    executionPlanes:
      build:
        strategy: shared
        clusters:
          - id: build-default
            region: sfo3
            nodeClass: medium
            minNodes: 2
            maxNodes: 5

      runtime:
        clusters:
          - id: runtime-default
            cellId: cell-default
            region: sfo3
            nodeClass: small
            minNodes: 2
            maxNodes: 3
          - id: runtime-sfo3-02
            cellId: cell-sfo3-02
            region: sfo3
            nodeClass: medium
            minNodes: 2
            maxNodes: 4
```

### Regras do modelo

- `id` é único globalmente.
- `id` é chave imutável de identidade do recurso no Pulumi; rename deve ser tratado como migração explícita, não como edição trivial.
- `cellId` é obrigatório para cluster de runtime cell-scoped.
- cluster de `build` pode ser `shared` ou `dedicated`.
- `mgmt` pode começar com 1 instância, mas o modelo já aceita mais.
- ausência de `topology` ativa modo compatível com o stack atual.

### Regra de identidade estável

Recursos multi-cluster devem ser instanciados por mapa estável indexado por `clusterId`, nunca por posição em array.

- reorder de listas não pode causar replace;
- `clusterId` removido implica destroy apenas daquele cluster;
- `clusterId` novo implica create apenas daquele cluster;
- rename de `clusterId` deve ser tratado como operação destrutiva ou fluxo explícito com aliases/migração.

---

## Arquitetura de Configuração

### Fase 1: Compatibilidade

Preservar as chaves atuais:

- `mgmtNodeClass`
- `mgmtMinNodes`
- `runtimeNodeClass`
- `runtimeMinNodes`
- `buildNodeClass`
- `buildMinNodes`

Se `topology` não existir, o parser monta uma topologia padrão a partir dessas chaves.

### Fase 2: Canonicalização

Depois da migração, `topology` vira o contrato principal. As chaves antigas continuam por um período de transição, mas apenas para backward compatibility.

---

## Componentes a Refatorar

### 1. Config loader

Arquivo alvo: [stack.ts](/home/paulo/Projetos/loopzero/loopzero/infra/pulumi/src/config/stack.ts)

Mudanças:

- adicionar tipos `TopologyConfig`, `ClusterSpec`, `CellSpec`;
- carregar `loopzero-infra:topology`;
- validar integridade estrutural;
- aplicar overrides por env var;
- gerar modo compatível quando `topology` não existir.

### 2. Provisionador genérico de clusters

Novo objetivo:

- eliminar a suposição “um cluster fixo por papel”;
- criar clusters em loop por coleção declarada;
- produzir outputs estáveis por instância.

Escopo:

- `createMgmtClusters(topology)`
- `createBuildClusters(topology)`
- `createRuntimeClusters(topology)`

### 3. Registry central de clusters

Criar uma estrutura única de outputs com:

- `clusterId`
- `role`
- `cellId`
- `region`
- `kubeconfig`
- `endpoint`
- `tags`
- `observability labels`

Esse registry deve virar a base para routing, placement, ClusterCredential e observabilidade.

### 4. Componente de Cell real

Arquivo alvo: [cell.ts](/home/paulo/Projetos/loopzero/loopzero/infra/pulumi/src/components/cell.ts)

Deve deixar de ser scaffold e passar a:

- provisionar ou referenciar runtime cluster real;
- delegar bootstrap de addons de runtime para a camada comum orientada a papel;
- anexar policies, observabilidade e metadados específicos da cell;
- publicar outputs reais;
- registrar a cell no control plane via `ClusterCredential`;
- integrar com registry central.

Responsabilidade explícita:

- `createCell()` é a unidade lógica que associa `cellId` a um runtime cluster, publica outputs da cell e registra a cell no control plane;
- o bootstrap de addons compartilhados de runtime continua em pipeline comum por papel para evitar duplicação de ArgoCD, Traefik e cert-manager.

### 5. Bootstrap por papel

Os componentes de bootstrap devem ser explicitamente orientados a papel:

- `mgmt`: API, control plane, Grafana, Prometheus central, Gitea;
- `build`: Argo Workflows, logs e componentes de build;
- `runtime`: ingress, ArgoCD de célula, cert-manager, agentes e isolamento.

Nada deve depender implicitamente de “o único runtime cluster”.

Responsabilidade explícita:

- bootstrap por papel é o dono da instalação de addons compartilhados do cluster;
- `cell.ts` complementa o bootstrap com registro da cell, metadados lógicos e integrações específicas de cell;
- nenhum addon deve ser instalado tanto em `createCell()` quanto no bootstrap por papel.

---

## Estratégia para Múltiplos Clusters de Build

O plano precisa suportar duas estratégias:

### `shared-build`

Padrão inicial.

- 1 ou poucos clusters de build compartilhados por várias cells;
- menor custo operacional;
- mais simples para sair do estado atual.

### `cell-build`

Modo avançado.

- cada cell tem seu próprio build cluster;
- melhor isolamento;
- maior custo e complexidade operacional.

### Decisão inicial

Implementar primeiro `shared-build`, mas modelar desde já a topologia para aceitar `cell-build` sem refactor estrutural.

---

## Estratégia para Env Vars

Env vars devem existir, mas com responsabilidade limitada.

### Permitido

- ajustar número de clusters em ambiente efêmero;
- testar uma topologia sem editar stack file;
- pipelines de benchmark ou smoke infra.

### Não permitido

- definir sozinho o layout de produção;
- carregar metadados incompletos que a stack config deveria conter;
- criar topologia implícita sem validação.

Exemplo prático:

- é aceitável aumentar `LOOPZERO_BUILD_CLUSTER_COUNT=2` quando já existir template canônico para build no stack;
- não é aceitável usar apenas `LOOPZERO_RUNTIME_CLUSTER_COUNT=2` se isso exigir inventar `cellId`, `region` ou sizing do segundo runtime.

### Regra prática

Se a mudança altera a forma persistente como produção opera, ela deve estar no stack config.

---

## Fases de Execução

## Fase 0: Documento e contrato

### Entregas

- documento de topologia alvo;
- schema TypeScript para `topology`;
- decisão escrita sobre `shared-build` vs `cell-build`;
- tabela de compatibilidade com config legada.

### DoD

- [ ] `topology` definida em documento e código
- [ ] regras de validação documentadas
- [ ] fallback para config atual especificado

---

## Fase 1: Config loader e modo compatível

### Entregas

- parser de `topology` em `loadStackConfig()`;
- fallback automático para o layout fixo atual;
- suporte opcional a `LOOPZERO_TOPOLOGY_JSON`.

### DoD

- [ ] stack atual continua funcionando sem `topology`
- [ ] stack com `topology` válida carrega corretamente
- [ ] erro explícito para ids duplicados, roles inválidos e `cellId` ausente onde obrigatório

---

## Fase 2: Provisionador multi-cluster

### Entregas

- extração da criação de clusters para componente genérico;
- `mgmt`, `build` e `runtime` passando a suportar coleções;
- outputs indexados por `clusterId`.

### DoD

- [ ] `pulumi preview` com 2 runtimes cria dois clusters distintos
- [ ] `pulumi preview` com 2 builds cria dois clusters distintos
- [ ] stack simples continua criando um cluster por papel
- [ ] resources são materializados por `clusterId` estável, sem depender da ordem da lista

---

## Fase 2.5: Migração dos contratos singleton

### Entregas

- adaptar `createInfra()` para retornar coleções/registry além de compat shims;
- migrar `bootstrap`, `expose`, execution state e stack outputs para consumir registry central;
- manter outputs legados enquanto existir modo compatível.

### DoD

- [ ] programa principal não assume mais `mgmtCluster`, `buildCluster` e `runtimeCluster` como únicos clusters globais
- [ ] consumers internos usam registry/coleções em vez de contratos singleton
- [ ] outputs legados continuam disponíveis no modo compatível sem duplicar fonte de verdade

---

## Fase 3: Registry central

### Entregas

- outputs centralizados de clusters;
- shape estável para consumo interno;
- labels/tags padronizados.

### Labels mínimas

- `loopzero.io/role`
- `loopzero.io/cluster-id`
- `loopzero.io/cell-id`
- `loopzero.io/environment`
- `loopzero.io/topology-version`

### DoD

- [ ] todo cluster provisionado aparece no registry
- [ ] outputs permitem discovery sem parsing manual de nomes

---

## Fase 4: Cell real

### Entregas

- `cell.ts` funcional;
- runtime cluster associado a `cellId`;
- ArgoCD, ingress e observabilidade por cell;
- `ClusterCredential` gerado no management cluster.

### DoD

- [ ] `createCell()` não retorna placeholders
- [ ] nova cell pode ser provisionada por config
- [ ] `ClusterCredential` aparece conectado
- [ ] observabilidade por cell aparece no Grafana/Prometheus
- [ ] `createCell()` não duplica addons já instalados pela camada de bootstrap por papel

---

## Fase 5: Bootstrap por papel

### Entregas

- bootstrap desacoplado do modelo fixo atual;
- addons instalados conforme papel e não conforme “cluster hardcoded”.

### DoD

- [ ] runtime addons instalados em todos os runtime clusters declarados
- [ ] build addons instalados em todos os build clusters declarados
- [ ] mgmt addons continuam restritos ao control plane
- [ ] ownership entre bootstrap por papel e `cell.ts` está documentado e refletido no código

---

## Fase 6: Testes de topologia

### Entregas

- testes unitários para parser e validação;
- testes de preview/outputs para múltiplas topologias;
- smoke tests de infra para cenários mínimos.

### Casos mínimos

- topologia legada compatível;
- `2 runtime`;
- `2 build`;
- `2 runtime + 1 shared build`;
- `2 runtime + 2 build`;
- erro em config inválida.

### DoD

- [ ] suite cobre casos válidos e inválidos
- [ ] destroy seletivo preserva clusters não-alvo
- [ ] reorder de listas não provoca replace indevido
- [ ] rename de `clusterId` é detectado como operação destrutiva ou migração explícita

---

## Fase 7: Operação e runbooks

### Entregas

- alinhar runbooks com a implementação real;
- documentar add/remove/scale de clusters por config;
- padronizar fluxo de expansão de cell.

### DoD

- [ ] operator consegue adicionar runtime cluster apenas por config + `pulumi up`
- [ ] operator consegue adicionar build cluster apenas por config + `pulumi up`
- [ ] runbook não depende de passos manuais que o stack deveria automatizar

---

## Critérios de Aceitação do Plano

O plano só estará concluído quando:

1. adicionar 2 ou mais clusters `runtime` for apenas mudança declarativa de config;
2. adicionar 2 ou mais clusters `build` for apenas mudança declarativa de config;
3. o stack atual continuar funcionando sem quebra;
4. `cell.ts` estiver operacional;
5. observabilidade multi-cluster estiver conectada ao novo registry;
6. a infra puder crescer sem reescrever o programa principal a cada nova expansão.

---

## Riscos

### Risco 1: refatorar demais cedo

Mitigação:

- manter modo compatível;
- entregar em fases pequenas;
- não tentar resolver tenant routing e placement no mesmo patch.

### Risco 2: topologia ambígua

Mitigação:

- schema rígido;
- validação fail-fast;
- ids e papéis explícitos.

### Risco 3: observabilidade quebrar com múltiplos clusters

Mitigação:

- labels padronizadas desde o início;
- registry como fonte única;
- testes de dashboards e scrape config.

### Risco 4: runbook continuar adiantado demais em relação ao código

Mitigação:

- só considerar runbook “done” quando o fluxo existir por config e `pulumi up`;
- evitar documentação aspiracional em passos críticos.

---

## Fora de Escopo

Este plano prepara a infra. Ele não resolve sozinho:

- loops singleton da API;
- placement/routing do sistema;
- isolamento lógico completo por `cell_id`;
- admission control;
- validação progressiva de carga;
- tenant migration em nível de aplicação.

Esses itens continuam pertencendo ao roadmap principal.

---

## Ordem Recomendada

1. Implementar parser de `topology` com compatibilidade.
2. Extrair provisionador genérico de clusters.
3. Introduzir registry central de clusters.
4. Migrar contratos singleton do programa principal para registry/coleções.
5. Tornar `cell.ts` funcional.
6. Refatorar bootstrap por papel.
7. Fechar observabilidade multi-cluster.
8. Criar testes de topologia.
9. Atualizar runbooks para refletir a implementação real.

---

## Resultado Esperado

Quando este plano estiver concluído, o LoopZero terá uma infraestrutura pronta para crescer por configuração declarativa. Criar 2 ou mais clusters `runtime`, `build` ou outros papéis passará a ser mudança de stack config, com override opcional por env var em CI, sem reestruturar o código da infra a cada expansão.
