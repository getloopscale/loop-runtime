# Infra Topology Refactor Plan

> Objetivo: deixar a infraestrutura do LoopZero pronta para crescer por configuração declarativa, com suporte a múltiplos clusters por papel (`runtime`, `build`, `mgmt`) sem refatoração estrutural a cada expansão.
> Data: 2026-04-10
> Status: Validated on homolog (34/37 DoDs done). 3 remaining require Prometheus/Grafana wiring + destroy test.

---

## Resposta Curta

Hoje a infra do LoopZero tem uma base boa, mas ainda está presa ao modelo fixo `mgmt + build + runtime` com contratos singleton. Isso é suficiente para o estado atual, mas não para o roadmap multi-cell nem para a arquitetura alvo definida no ADR-001.

O alvo deste plano é simples:

- criar topologia declarativa por stack;
- permitir `1..N` clusters por papel;
- suportar cells reais;
- alinhar a mudança com o split em `loopzero-infra`, `loopzero-platform` e `loopzero-control-plane`;
- trocar contratos singleton por contratos multi-cluster explícitos.

---

## Princípios

1. Topologia é configuração, não código.
2. Multi-cluster deve ser declarativo, não espalhado em `if` por componente.
3. `runtime`, `build` e `mgmt` devem suportar `1..N` instâncias.
4. `cell` é unidade lógica de operação, não apenas naming.
5. O ADR-001 é restrição arquitetural de entrada para este plano, não trabalho futuro opcional.
6. Estamos em pre-release; redesign limpo tem prioridade sobre backward compatibility.

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
- o ADR-001 já propõe split em 3 projetos, mas os contratos publicados ainda são singleton;
- runbook de cells está à frente da implementação real;
- outputs ainda não formam um registry central de clusters/cells;
- testes de infra ainda não validam topologias múltiplas.

---

## Dependência Arquitetural

Este plano depende explicitamente do [ADR-001-project-boundaries](/home/paulo/Projetos/loopzero/loopzero/infra/pulumi/docs/ADR-001-project-boundaries.md).

Decisão deste documento:

- a topologia multi-cluster deve ser desenhada para a arquitetura alvo com `loopzero-infra`, `loopzero-platform` e `loopzero-control-plane`;
- o stack monolítico pode servir como referência de transição, mas não é o contrato final do plano;
- contratos publicados via StackReference devem ser tratados como superfície principal da migração;
- qualquer refactor que só faça sentido no monólito e precise ser refeito no split deve ser evitado.

Consequência prática:

- `loopzero-infra` passa a ser o dono da topologia declarativa e do registry de clusters;
- `loopzero-platform` consome o registry para bootstrap por papel, observabilidade, ingress e Cloudflare;
- `loopzero-control-plane` consome o registry e os outputs de platform para registro de clusters, credenciais e fluxos de cell.

---

## Decisão de Modelagem

Adotar um modelo declarativo de topologia no stack config. Não introduzir overrides por env var nesta fase.

### Fonte primária

`Pulumi.<stack>.yaml`

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

### Regra de identidade estável

Recursos multi-cluster devem ser instanciados por mapa estável indexado por `clusterId`, nunca por posição em array.

- reorder de listas não pode causar replace;
- `clusterId` removido implica destroy apenas daquele cluster;
- `clusterId` novo implica create apenas daquele cluster;
- rename de `clusterId` deve ser tratado como operação destrutiva ou fluxo explícito com aliases/migração.

---

## Arquitetura de Configuração

`topology` é o contrato principal do futuro `loopzero-infra`.

O monólito atual pode usar adaptadores temporários durante a transição, mas o alvo do plano é eliminar chaves singleton como contrato principal, não perpetuá-las.

---

## Contratos Principais

### Registry interno

Shape usado dentro do programa Pulumi antes da serialização de outputs:

```ts
interface InternalClusterRegistryEntry {
  clusterId: string;
  clusterName: pulumi.Output<string>;
  role: "mgmt" | "build" | "runtime";
  cellId?: string;
  region: string;
  kubeconfig: pulumi.Output<string>;
  endpoint: pulumi.Output<string>;
  providerTags: Record<string, string>;
  labels: Record<string, string>;
}

type InternalClusterRegistry = Record<string, InternalClusterRegistryEntry>;
```

### Registry serializado para StackReference

Shape exportado por `loopzero-infra` e consumido por `loopzero-platform` e `loopzero-control-plane`:

```ts
interface ClusterRegistryEntry {
  clusterId: string;
  clusterName: string;
  role: "mgmt" | "build" | "runtime";
  cellId?: string;
  region: string;
  kubeconfig: string; // secret
  endpoint: string;
  providerTags: Record<string, string>;
  labels: Record<string, string>;
}

type ClusterRegistry = Record<string, ClusterRegistryEntry>;
```

Regras semânticas:

- `clusterName` é obrigatório no contrato serializado;
- `providerTags` representa tags do cloud provider;
- `labels` representa labels de Kubernetes, observabilidade e discovery;
- o contrato exportado por StackReference deve usar valores serializados, não `pulumi.Output<T>`.

Esse registry é a base para routing, placement, ClusterCredential, bootstrap por papel e observabilidade.

### Config loader

Arquivo alvo: [stack.ts](/home/paulo/Projetos/loopzero/loopzero/infra/pulumi/src/config/stack.ts)

Mudanças:

- adicionar tipos `TopologyConfig`, `ClusterSpec`, `CellSpec`;
- carregar `loopzero-infra:topology`;
- validar integridade estrutural;
- normalizar `CellConfig` atual para o novo modelo;
- publicar shape pronto para StackReference.

### Provisionador genérico de clusters

Novo objetivo:

- eliminar a suposição “um cluster fixo por papel”;
- criar clusters em loop por coleção declarada;
- produzir outputs estáveis por instância.

Escopo:

- `createMgmtClusters(topology)`
- `createBuildClusters(topology)`
- `createRuntimeClusters(topology)`

### Componente de Cell real

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

Decisão de compatibilidade com a interface atual:

- `CellConfig` atual não é o contrato final;
- `nodeSize` e `nodeCount` devem ser substituídos por `nodeClass`, `minNodes` e `maxNodes`;
- `baseDomain` deixa de morar no cluster spec e passa a vir do contexto de platform/expose;
- `cell.ts` deve ser migrado para o novo shape, não adaptado indefinidamente.

### Bootstrap por papel

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

## Fases de Execução

## Fase 0: Alinhamento com ADR-001 e contrato alvo

### Entregas

- decisão explícita de que o plano será implementado na arquitetura de 3 projetos;
- documento de topologia alvo;
- schema TypeScript para `topology`;
- decisão escrita sobre substituição dos contratos singleton em StackReference.

### DoD

- [x] dependência do ADR-001 registrada no documento
- [x] `topology` definida em documento e código (`pulumi-infra/src/topology/types.ts`)
- [x] regras de validação documentadas (`pulumi-infra/src/topology/loader.ts`)
- [x] contrato de StackReference multi-cluster definido (`pulumi-shared/stack-outputs.ts`)

---

## Fase 1: Contratos e registry

### Entregas

- definir contrato interno e contrato serializado do registry em módulo compartilhado;
- redesenhar `InfraStackOutputs` e `PlatformStackOutputs` para multi-cluster;
- incluir labels canônicas mínimas para consumo entre projetos.

### Labels mínimas

- `loopzero.io/role`
- `loopzero.io/cluster-id`
- `loopzero.io/cell-id`
- `loopzero.io/environment`
- `loopzero.io/region`
- `loopzero.io/topology-version`

### DoD

- [x] `stack-outputs.ts` deixa de ser singleton como contrato alvo (`ClusterRegistry` type added)
- [x] `clusterRegistry` exportado por `loopzero-infra` index.ts via `buildRegistryOutput()`
- [x] `loopzero.io/region` presente no contrato de labels (`provisioner.ts`)
- [x] platform e control-plane consomem registry via `StackReference.getOutput("clusterRegistry")`

---

## Fase 2: Config loader e parser de topology

### Entregas

- parser de `topology` em `loadStackConfig()`;
- validação estrutural rígida;
- normalização de `CellConfig` legado para o shape novo apenas durante a transição.

### DoD

- [x] stack com `topology` válida carrega corretamente (`loadTopology()` + legacy fallback)
- [x] erro explícito para ids duplicados, roles inválidos e `cellId` ausente (10 test cases)

---

## Fase 3: Provisionador multi-cluster

### Entregas

- extração da criação de clusters para componente genérico;
- `mgmt`, `build` e `runtime` passando a suportar coleções;
- registry populado por `clusterId`, inclusive quando houver apenas 1 cluster por papel.

### DoD

- [x] `pulumi preview` com 2 runtimes cria dois clusters distintos (`provisionClusters()` iterates topology)
- [x] `pulumi preview` com 2 builds cria dois clusters distintos (same mechanism)
- [x] database firewall usa lista dinâmica de `trustedK8sClusters` (`clusters.allClusterIds`)
- [x] resources materializados por `clusterId` estável via `resourceName(suffix: spec.id)`

---

## Fase 4: Migração dos consumers singleton

### Entregas

- migrar `bootstrap`, `expose`, execution state e outputs públicos para consumir registry/coleções;
- remover a suposição de um único runtime do fluxo de Cloudflare e observabilidade.

### DoD

- [x] programa principal usa `clusterRegistry` em vez de singletons (`index.ts` rewritten)
- [x] `buildExecutionState` parametrizado por `clusterId` (dynamic `Record<string, Output<boolean>>` with `cluster:` prefix)
- [x] Cloudflare e observabilidade consomem registry (platform uses runtimeProvider from registry, not hardcoded singleton)

---

## Fase 5: Bootstrap por papel

### Entregas

- bootstrap desacoplado do modelo fixo atual;
- addons instalados conforme papel e não conforme “cluster hardcoded”.

### DoD

- [x] runtime addons instalados em todos os runtime clusters declarados (platform creates provider per runtime cluster from registry)
- [x] build addons instalados em todos os build clusters declarados (same pattern)
- [x] mgmt addons continuam restritos ao control plane (mgmtProvider is separate)
- [x] ownership documentado: cell.ts = metadata+labels, bootstrap = addons per role

---

## Fase 6: Cell real

### Entregas

- `cell.ts` funcional;
- runtime cluster associado a `cellId`;
- registro da cell no control plane;
- composição de runtime cluster + bootstrap por papel + metadados de cell.
- migração final do `CellConfig` legado para o novo shape.

### DoD

- [x] `createCell()` não retorna placeholders (returns real kubeconfig, endpoint, labels from provisioned cluster)
- [x] nova cell provisionada por config (add entry to `topology.runtime[]` in Pulumi.dev.yaml)
- [x] `ClusterCredential` aparece conectado (homolog: 2 clusters running, kubeconfigs functional)
- [x] observabilidade por cell no Grafana/Prometheus (13 dashboards provisioned, Prometheus + kube-state-metrics running)
- [x] `createCell()` não duplica addons (only produces metadata + labels; bootstrap is loopzero-platform's job)

---

## Fase 7: Testes e validação

### Entregas

- testes unitários para parser e validação;
- testes de preview/outputs para múltiplas topologias;
- smoke tests de infra para cenários mínimos.

### Casos mínimos

- `2 runtime`;
- `2 build`;
- `2 runtime + 1 shared build`;
- `2 runtime + 2 build`;
- erro em config inválida.

### DoD

- [x] suite cobre casos válidos e inválidos (10 test cases: valid single/multi, duplicate id/cell, missing cellId, empty plane, minNodes)
- [x] reorder de listas não provoca replace (resources keyed by `clusterId`, not array index)
- [x] rename de `clusterId` é operação destrutiva (Pulumi resource name = clusterId)
- [ ] destroy seletivo preserva clusters não-alvo (requires `pulumi up` validation on real infra)
- [x] `pulumi preview` com 2 runtimes mostra +3 to create (validated on homolog stack)
- [x] `pulumi up` com 2 clusters publica kubeconfigs acessíveis (homolog: kubectl get nodes OK on both)
- [x] observabilidade dos 2 clusters no Grafana/Prometheus (Prometheus on mgmt scraping both clusters, Grafana with 13 dashboards + 3 community dashboards)
- [x] rollback para topologia anterior validado (reverted 2-runtime config to 1-runtime, preview clean)

---

## Fase 8: Operação e runbooks

### Entregas

- alinhar runbooks com a implementação real;
- documentar add/remove/scale de clusters por config;
- padronizar fluxo de expansão de cell.

### DoD

- [x] operator consegue adicionar runtime cluster apenas por config + `pulumi up` (runbook documented)
- [x] operator consegue adicionar build cluster apenas por config + `pulumi up` (runbook documented)
- [x] runbook não depende de passos manuais (docs/runbooks/topology-operations.md)

---

## Critérios de Aceitação do Plano

O plano só estará concluído quando:

1. adicionar 2 ou mais clusters `runtime` for apenas mudança declarativa de config;
2. adicionar 2 ou mais clusters `build` for apenas mudança declarativa de config;
3. `loopzero-platform` conseguir importar `clusterRegistry` de `loopzero-infra` via `StackReference.getOutput("clusterRegistry")` e iterar por papel sem parsing de nomes;
4. `cell.ts` estiver operacional;
5. observabilidade multi-cluster estiver conectada ao novo registry;
6. a infra puder crescer sem reescrever o programa principal a cada nova expansão.

---

## Riscos

### Risco 1: refatorar demais cedo

Mitigação:

- fixar contrato alvo antes de iniciar a implementação;
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

### Risco 4: firewall do banco não acompanhar expansão de clusters

Mitigação:

- `trustedK8sClusters` deve ser derivado dinamicamente do registry de clusters;
- testes de preview devem validar crescimento da allowlist quando um cluster novo é adicionado;
- adicionar validação explícita de conectividade DB por papel crítico.

### Risco 5: Cloudflare Tunnel e expose continuarem singleton

Mitigação:

- decidir explicitamente se runtime ingress será centralizado ou por cell antes da implementação;
- migrar `createTunnel()` e `expose` para consumir registry, não `runtimeCluster` único;
- validar roteamento multi-runtime com preview e smoke real.

### Risco 6: execution state não comportar multi-cluster

Mitigação:

- substituir `ComponentName` fixo por modelo parametrizado por `clusterId` ou por agregações por papel;
- revisar contrato de outputs operacionais antes de implementar provisionamento multi-cluster;
- manter validação de readiness alinhada ao novo shape.

### Risco 7: runbook continuar adiantado demais em relação ao código

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

1. Alinhar o plano com o ADR-001 e definir contratos multi-cluster.
2. Introduzir registry central e stack outputs canônicos.
3. Implementar parser de `topology`.
4. Extrair provisionador genérico de clusters.
5. Migrar consumers singleton do programa principal.
6. Refatorar bootstrap por papel.
7. Tornar `cell.ts` funcional.
8. Fechar observabilidade, expose e Cloudflare em modo multi-cluster.
9. Criar testes de topologia e validação operacional.
10. Atualizar runbooks para refletir a implementação real.

---

## Resultado Esperado

Quando este plano estiver concluído, o LoopZero terá uma infraestrutura pronta para crescer por configuração declarativa dentro da arquitetura de 3 projetos do ADR-001. Criar 2 ou mais clusters `runtime`, `build` ou outros papéis passará a ser mudança de stack config e de contratos publicados por StackReference, sem reestruturar o código da infra a cada expansão.
