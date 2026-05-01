# Infra Research Basis

## Objetivo

Este documento registra a base de pesquisa usada para definir os gates SOTA da pasta `infra/`.

Ele combina:

- documentação oficial das ferramentas centrais de IaC/GitOps/Kubernetes;
- evidências do código e dos runbooks atuais do LoopZero;
- referências de PaaS que tratam bootstrap e operação como parte do produto.

## Fontes Externas Principais

### 1. Pulumi IaC e State

Fontes:

- https://www.pulumi.com/docs/iac/
- https://www.pulumi.com/docs/reference/state/

Pontos relevantes:

- stacks são a unidade de estado e configuração;
- state é parte essencial do controle de recursos;
- backend e gestão de state precisam ser deliberados.

Gates fundamentados:

- `INF-G01`
- `INF-G02`
- `INF-G03`
- `INF-G15`

Por que importa no LoopZero:

- o LoopZero já usa múltiplos projetos/stacks Pulumi e depende fortemente de contratos entre eles.

### 2. Helmfile Best Practices

Fonte:

- https://helmfile.readthedocs.io/en/stable/writing-helmfile/

Pontos relevantes:

- layering e environments são mecanismos explícitos para DRY e promoção controlada;
- Helmfile serve bem como orquestrador declarativo de bootstrap.

Gates fundamentados:

- `INF-G01`
- `INF-G04`
- `INF-G15`

### 3. Argo CD Cluster Bootstrapping / App of Apps

Fonte:

- https://argo-cd.readthedocs.io/en/release-3.3/operator-manual/cluster-bootstrapping/

Pontos relevantes:

- App-of-Apps é padrão reconhecido para bootstrap de muitos apps em cluster novo;
- GitOps de day-2 precisa de raiz declarativa explícita.

Gates fundamentados:

- `INF-G01`
- `INF-G07`
- `INF-G13`

### 4. Kubernetes Probes

Fonte:

- https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/

Pontos relevantes:

- startup, readiness e liveness têm semânticas diferentes;
- probes incorretas degradam disponibilidade e podem causar starvation.

Gates fundamentados:

- `INF-G08`
- `INF-G10`

### 5. Kubernetes Resource Management / Security Context

Fontes:

- https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
- https://kubernetes.io/docs/tasks/configure-pod-container/security-context/

Pontos relevantes:

- resources e security context são parte da segurança e da previsibilidade operacional;
- defaults ausentes amplificam risco de noisy neighbor e runtime insecurity.

Gates fundamentados:

- `INF-G08`
- `INF-G14`

## Evidências Internas Do LoopZero

### Modelo em camadas

Pastas:

- `infra/pulumi*`
- `infra/helmfile`
- `infra/helm`
- `infra/argocd-bootstrap`

O que sustentam:

- separação real entre fundação cloud, bootstrap de cluster e GitOps day-2;
- evidência concreta de layered bootstrap.

### App-of-Apps e day-2 declarativo

Arquivos:

- `infra/argocd-bootstrap/root-app.yaml`
- `infra/argocd-bootstrap/apps/*`

O que sustentam:

- raiz declarativa explícita;
- separação por planos e clusters.

### Contracts e state de Pulumi

Arquivos:

- `infra/pulumi/README.md`
- `infra/pulumi-shared/stack-outputs.ts`
- `infra/pulumi/docs/ADR-001-project-boundaries.md`

O que sustentam:

- boundary entre stacks;
- contratos entre outputs e consumidores;
- importância explícita de state e boundaries.

### Bootstrap e wiring operacional

Arquivos:

- `infra/helmfile/scripts/*`
- `infra/scripts/bootstrap-cluster.sh`
- `infra/scripts/bootstrap-platform.sh`
- `infra/scripts/health-check.sh`

O que sustentam:

- wiring multi-cluster;
- bootstrap repetível;
- checagem operacional pós-bootstrap.

### Validation e conformance

Pastas/arquivos:

- `infra/kubeconform-schemas/*`
- `infra/helm/monitoring/validate-rules.py`
- `infra/argocd-bootstrap/validate-sync-waves.py`

O que sustentam:

- validação estrutural e operacional como prática nativa da infra.

### Recovery e repair

Arquivos/runbooks:

- `infra/scripts/break-glass.sh`
- `infra/scripts/pre-destroy-cleanup.sh`
- `infra/scripts/pulumi-orphan-rm.sh`
- `docs/runbooks/pulumi-state-cleanup.md`
- `docs/runbooks/pulumi-state-repair.md`
- `docs/runbooks/pulumi-state-restore.md`

O que sustentam:

- estado e recuperação são domínios explícitos;
- a equipe já reconhece repair como parte do sistema.

### Observabilidade e operação

Pastas/arquivos:

- `infra/helm/monitoring/*`
- `infra/helm/promtail/*`
- `infra/helm/otel/*`
- `docs/runbooks/alerts/*`
- `docs/runbooks/observability.md`

O que sustentam:

- alerting e observabilidade são parte da fundação, não adição tardia.

### Paridade, chaos, load e capacidade

Pastas:

- `infra/tests/parity`
- `infra/tests/chaos`
- `infra/tests/load`
- `infra/tests/capacity`
- `infra/tests/e2e`

O que sustentam:

- validação sob condições reais de operação;
- preocupação explícita com resiliência e escala.

## Conclusão

Os gates em [sota-gates.md](./sota-gates.md) existem porque:

1. São coerentes com as ferramentas oficiais que sustentam a infra do LoopZero.
2. São necessários para uma PaaS multi-cluster com bootstrap em camadas.
3. Já encontram evidências reais no código e nos runbooks do projeto.
