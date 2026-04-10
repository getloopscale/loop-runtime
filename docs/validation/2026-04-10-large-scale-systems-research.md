# Pesquisa: Sistemas Reais em Grande Escala com Kubernetes, Argo CD e Argo Workflows

**Data:** 2026-04-10  
**Objetivo:** reunir evidências públicas de equipes e plataformas que já operaram em escala muito alta, incluindo referências relevantes para metas na ordem de 100.000 pods

## Resumo Executivo

Sim, existem sistemas reais e equipes que já operaram nessa ordem de grandeza ou acima dela.

O padrão mais recorrente não é:

- um único sistema monolítico de controle;
- uma única instância pequena de GitOps;
- apenas aumento de CPU e memória.

O padrão que aparece repetidamente é:

- `cell-based architecture` ou outras formas de particionamento forte;
- `scale-out` em vez de apenas `scale-up`;
- `sharding` de controladores;
- separação clara entre `control plane` e `workload plane`;
- forte tuning do Kubernetes API, banco, caches, workers e concorrência;
- capacidade por unidade operacional explicitamente conhecida e testada.

## 1. Kubernetes em Grande Escala

### Kubernetes upstream

A documentação oficial do Kubernetes afirma que, em `v1.35`, o sistema foi projetado para acomodar clusters com:

- até `5.000 nodes`;
- até `110 pods` por node;
- até `150.000 total pods`;
- até `300.000 total containers`.

Isso é importante porque define uma referência oficial de limite de projeto, não apenas benchmark informal.

Fonte:

- [Kubernetes: Considerations for large clusters](https://kubernetes.io/docs/setup/best-practices/cluster-large/)

### OpenAI

A OpenAI publicou em **18 de janeiro de 2021** o artigo sobre escalar Kubernetes para `7.500 nodes`.

Esse caso é importante porque mostra uma operação real em larga escala e destaca gargalos concretos de:

- networking;
- API server;
- etcd;
- volume de IPs e rotas.

Fonte:

- [OpenAI: Scaling Kubernetes to 7,500 nodes](https://openai.com/index/scaling-kubernetes-to-7500-nodes/)

### AWS EKS

A AWS publicou em **16 de julho de 2025** um artigo anunciando suporte do Amazon EKS a `100.000 nodes` por cluster para workloads ultra-scale de AI/ML.

O post menciona explicitamente clientes como:

- Anthropic;
- Amazon Nova.

O valor dessa referência está menos em “todo mundo precisa de 100k nodes” e mais em mostrar que, em hyperscale, a evolução do control plane exige mudanças profundas em:

- etcd;
- distribuição de carga;
- throughput do plano de controle.

Fonte:

- [Amazon EKS enables ultra-scale AI/ML workloads with support for 100k nodes per cluster](https://aws.amazon.com/blogs/containers/amazon-eks-enables-ultra-scale-ai-ml-workloads-with-support-for-100k-nodes-per-cluster/)

### Google GKE

O Google publicou em **2025** um artigo sobre como construiu um cluster GKE de `130.000 nodes`.

O texto também menciona um benchmark com `65.000 Pods` para preencher metade da capacidade do cluster.

Fonte:

- [How we built a 130,000-node GKE cluster](https://cloud.google.com/blog/products/containers-kubernetes/how-we-built-a-130000-node-gke-cluster)

### Google / Bayer Crop Science

O Google também publicou em **2020** o aumento do suporte de GKE para `15.000 nodes`, com referência ao uso pela Bayer Crop Science.

Fonte:

- [Google Kubernetes Engine clusters can have up to 15,000 nodes](https://cloud.google.com/blog/products/containers-kubernetes/google-kubernetes-engine-clusters-can-have-up-to-15000-nodes)

## 2. Argo CD em Grande Escala

### AWS + comunidade Argo

A AWS publicou em **2023** testes de escalabilidade do Argo CD Application Controller no Amazon EKS com:

- `10.000 aplicações`;
- `97 clusters`;
- sharding do controller.

O material é importante porque mostra que, mesmo bem antes de 50k apps, tuning e sharding já passam a ser centrais.

Fonte:

- [Argo CD application controller scalability testing on Amazon EKS](https://aws.amazon.com/blogs/opensource/argo-cd-application-controller-scalability-testing-on-amazon-eks/)

### CNOE

A CNOE publicou benchmark de Argo CD mostrando testes até:

- `50.000 aplicações`;
- `500 workload clusters`.

Os resultados mostram que o desempenho muda fortemente com:

- `QPS/Burst QPS`;
- número de `status processors` e `operation processors`;
- quantidade de shards;
- quantidade de nós dedicados ao control plane do Argo CD.

O material também mostra que:

- `15k apps` já colocavam um único node `m5.2xlarge` em comportamento insalubre;
- com `3 nodes m5.2xlarge`, o teste de `50k apps` conseguiu sincronizar em cerca de `22 min`;
- aumentar shards de `3` para `9` reduziu um experimento de sync de `75 min` para `21 min`.

Fonte:

- [CNOE: Argo CD Benchmarking - Pushing the Limits and Sharding Deep Dive](https://cnoe.io/blog/argo-cd-application-scalability)

### Documentação oficial do Argo CD

A documentação oficial de HA do Argo CD traz recomendações explícitas para ambientes grandes:

- aumentar `status-processors` e `operation-processors`;
- aumentar `parallelismlimit` no repo-server;
- usar sharding do application-controller;
- monitorar API server e cache;
- tratar monorepos com cuidado por impacto no repo-server.

Fonte:

- [Argo CD High Availability Manual](https://argo-cd.readthedocs.io/en/stable/operator-manual/high_availability/)

## 3. Argo Workflows em Grande Escala

### Documentação oficial do Argo Workflows

A documentação oficial possui uma página dedicada chamada `Running At Massive Scale`.

Ela afirma explicitamente que o Argo Workflows pode lidar com:

- `thousands of workflows per day`;
- workflows com `tens of thousands of nodes`;
- `hundreds of thousands of smaller workflows daily`.

Mas a mesma página deixa claro que isso depende de otimização forte de:

- Kubernetes API;
- executor;
- paralelismo;
- rate limit de criação de pod;
- limpeza rápida de dados;
- banco para offloading.

Fonte:

- [Argo Workflows: Running At Massive Scale](https://argo-workflows.readthedocs.io/en/latest/running-at-massive-scale/)

### CNOE

A CNOE publicou em **4 de junho de 2024** resultados de benchmark de escalabilidade de Argo Workflows no Amazon EKS.

O material traz números concretos como:

- até `2.100 workflows por minuto`;
- fila de reconciliação com `5.000 workflows`;
- cluster com `50 worker nodes`.

As conclusões reforçam a necessidade de tuning explícito de:

- `workflow-workers`;
- `pod-cleanup-workers`;
- `QPS/Burst`;
- sharding por namespace;
- compressão de payload;
- otimização de labels e do tráfego ao Kubernetes API.

Fonte:

- [CNOE: Running Argo Workflows at Scale](https://cnoe.io/blog/argo-workflow-scalability)

### Alibaba Cloud

A Alibaba Cloud publicou um caso de uso de Argo Workflows em cenário de grande escala envolvendo:

- `tens of thousands of pods`;
- `hundreds of thousands of CPU cores`;
- execução elástica/serverless.

Esse caso é útil como evidência adicional de que Argo Workflows aparece em ambientes realmente grandes, mas sempre com forte apoio da plataforma subjacente.

Fonte:

- [Large-Scale Distributed Workflow Scheduling Based on Cloud-Native Argo Workflows](https://www.alibabacloud.com/blog/600924)

## 4. Padrão Arquitetural Repetido: Cells

### AWS Well-Architected

A documentação oficial da AWS para `cell-based architecture` é especialmente relevante porque explica, em termos operacionais, por que serviços grandes migram para esse modelo.

Benefícios destacados:

- `scale-out over scale-up`;
- menor `scope of impact`;
- capacidade conhecida por célula;
- maior testabilidade;
- melhor MTTR;
- deployments e rollbacks com menor blast radius.

Fonte:

- [AWS Well-Architected: Why use a cell-based architecture?](https://docs.aws.amazon.com/wellarchitected/latest/reducing-scope-of-impact-with-cell-based-architecture/why-to-use-a-cell-based-architecture.html)

### Fly.io

O Fly.io documenta explicitamente `cell-based architecture` como blueprint de sistema distribuído.

Essa referência é útil porque mostra que o padrão não é apenas teoria acadêmica ou recomendação cloud-first; ele é usado como modelo operacional real em plataformas que precisam crescer mantendo isolamento.

Fonte:

- [Fly.io: Cell-based architecture](https://fly.io/docs/blueprints/cell-based/)

### Salesforce

A documentação da Salesforce mostra a noção de `Hyperforce Cell (fka Pod)` como unidade relevante de limitação de blast radius.

Isso reforça novamente a repetição do mesmo padrão em plataformas grandes.

Fonte:

- [Salesforce Architect: Platform Transformation](https://architect.salesforce.com/docs/architect/fundamentals/guide/platform-transformation.html)

## 5. Padrões Reais Observados na Pesquisa

Os casos acima convergem para alguns padrões claros.

### 1. Escala alta raramente depende de um único cluster simples

Mesmo quando existe um cluster gigantesco, o sistema em volta costuma ser fortemente particionado:

- por células;
- por shards;
- por namespaces;
- por regiões;
- por classes de workload.

### 2. O limite operacional é quase sempre o control plane

Em grande escala, o primeiro problema raramente é apenas “rodar pods”.

Os gargalos reais aparecem em:

- Kubernetes API;
- etcd;
- controladores;
- GitOps controllers;
- rate de reconciliação;
- fan-out de eventos;
- caches e filas.

### 3. Argo CD escala, mas com sharding e compute explícito

Os benchmarks mostram que Argo CD chega muito longe, mas não “de graça”.

Escalar exige:

- shards;
- nós dedicados;
- tuning de processors;
- tuning de QPS/Burst;
- observabilidade do API server.

### 4. Argo Workflows escala, mas com limites e GC agressivo

Os materiais de Argo Workflows reforçam:

- limitação de paralelismo;
- pod GC;
- workflow GC;
- executor adequado;
- redução da pressão sobre Kubernetes API;
- controle cuidadoso da taxa de criação de pods.

### 5. Cell-based architecture aparece repetidamente

O padrão de célula reaparece em:

- AWS;
- Fly.io;
- Salesforce;
- e implicitamente em benchmarks que precisam particionar carga.

Isso sugere fortemente que, para plataformas tipo PaaS, cell-based não é sofisticação desnecessária. É o padrão operacional natural quando o volume cresce.

## 6. Implicações para o LoopZero

A principal implicação é:

> a meta de 100.000 pods é plausível, mas os casos reais mostram que ela exige fechamento arquitetural e operacional, não apenas aumento de infraestrutura.

### O que a pesquisa valida

A pesquisa valida que:

- metas nessa ordem de grandeza são reais;
- Kubernetes, Argo CD e Argo Workflows podem participar dessa escala;
- o caminho adotado pelo LoopZero em direção a `cells` está alinhado com o padrão do mercado.

### O que a pesquisa também invalida

A pesquisa invalida a ideia de que:

- basta aumentar os clusters na DigitalOcean;
- basta aumentar réplicas da API;
- basta dar mais CPU ao Argo CD ou ao banco.

Os casos reais mostram que o salto de escala vem quando o sistema aceita explicitamente:

- particionamento forte;
- limites por célula;
- control plane separado;
- tuning agressivo;
- testes de capacidade baseados em evidência.

### Leitura prática para o LoopZero

Para o LoopZero, isso significa:

1. `cell-based architecture` não é exagero; é o caminho esperado;
2. a implementação multi-cell precisa sair do nível parcial/scaffold e virar operação real;
3. a API/control plane precisa perder gargalos singleton;
4. Argo CD e Argo Workflows devem ser tratados como componentes escaláveis com tuning e sharding, não como caixas-pretas;
5. o dimensionamento da DigitalOcean só vira a principal conversa depois que a implementação de escala estiver fechada, inclusive no desacoplamento interno do control plane.

## Conclusão Final

Existem, sim, precedentes claros de equipes e plataformas que chegaram nessa ordem de grandeza.

Mas os precedentes mostram quase sempre a mesma coisa:

- grandes volumes exigem arquitetura de `scale-out`;
- `cells` ou unidades equivalentes de isolamento são comuns;
- `Argo CD` e `Argo Workflows` escalam, mas não em modo default;
- o sucesso depende tanto da implementação do sistema quanto do tamanho da infraestrutura.

Para o LoopZero, a conclusão mais importante da pesquisa é:

> o objetivo é tecnicamente viável e tem precedentes reais, mas atingir esse nível exigirá concluir a arquitetura de escala do sistema, e não apenas ampliar a infraestrutura.
