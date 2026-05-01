# Infra SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo`
- `Critério de aprovação`
- `Evidência esperada`
- `Notas LoopZero`

Em uma PaaS, infra SOTA não é só "terraform aplica" ou "helm install funciona". A régua correta é reprodutibilidade, recuperação, segurança operacional e coerência entre planos.

Regra transversal de validação:

- validação estrutural, smoke e integração são necessários;
- para infra crítica, isso não basta sem parity validation, runbooks exercitados e, quando aplicável, evidência em ambiente real ou dogfood operacional.

## Gate INF-G01: Layer Separation Integrity

`Objetivo`

Garantir que L1, L2 e L3 tenham responsabilidades claras e sem acoplamento confuso.

`Critério de aprovação`

- L1 cria fundação cloud e contratos, não instala recursos de day-2 no cluster.
- L2 bootstrapa componentes base do cluster sem assumir que ArgoCD já governa tudo.
- L3 entrega componentes contínuos via GitOps.
- Dependências entre camadas são explícitas e unidirecionais.

`Evidência esperada`

- pastas e stacks separadas;
- docs e scripts respeitando a ordem de bootstrap;
- ausência de recursos Kubernetes de day-2 escondidos em Pulumi L1.

`Notas LoopZero`

- esse gate é central porque o LoopZero já adota bootstrap em camadas.

## Gate INF-G02: Reproducible Environment Creation

`Objetivo`

Permitir recriação confiável de ambiente a partir do repositório.

`Critério de aprovação`

- um ambiente novo pode ser criado com código, config e segredos previstos;
- passos manuais são mínimos, explícitos e documentados;
- dependências externas necessárias são conhecidas antes do rollout.

`Evidência esperada`

- scripts/bootstrap claros;
- stacks Pulumi com config definida;
- Helmfile/Argo bootstrap documentados.

`Notas LoopZero`

- em uma PaaS multi-cluster, "recriar do zero" é um teste arquitetural, não só operacional.

## Gate INF-G03: State Discipline

`Objetivo`

Controlar estado de IaC e de bootstrap sem corrupção, drift invisível ou perda de rastreabilidade.

`Critério de aprovação`

- cada stack tem backend de estado definido;
- repair/cleanup/restore têm procedimento explícito;
- drift de Pulumi ou bootstrap não fica invisível;
- state é tratada como ativo operacional crítico.

`Evidência esperada`

- documentação/runbooks de state repair;
- scripts de archive/orphan cleanup;
- alerts e práticas de drift detection.

`Notas LoopZero`

- a presença de runbooks de state cleanup/repair já indica que esse domínio é real e importante.

## Gate INF-G04: Idempotent Bootstrap

`Objetivo`

Garantir que bootstrap e wiring possam ser reexecutados sem destruir convergência.

`Critério de aprovação`

- scripts e helmfiles são reentrantes;
- secret generation e wiring são seguros em rerun;
- convergência posterior não depende da ordem acidental de uma primeira execução.

`Evidência esperada`

- scripts com comportamento idempotente;
- helmfile sync previsível;
- testes ou matrizes de regressão de bootstrap.

`Notas LoopZero`

- isso é particularmente importante para local, recuperação e expansão de cluster/cell.

## Gate INF-G05: Local/CI/Prod Semantic Parity

`Objetivo`

Evitar que local seja um simulacro de produto e produção outro sistema.

`Critério de aprovação`

- local preserva os mesmos planos conceituais;
- CI valida o mesmo modelo declarativo usado em produção;
- diferenças de escala ou provider não mudam o fluxo operacional fundamental.

`Evidência esperada`

- três clusters locais;
- parity tests;
- runbooks/matrizes de validação de bootstrap e deploy.

`Notas LoopZero`

- este gate é um dos que mais diferenciam o LoopZero de setups simplificados demais.

## Gate INF-G06: Validation Before Trust

`Objetivo`

Tornar validação estrutural e operacional parte obrigatória do ciclo de infra.

`Critério de aprovação`

- manifests passam schema validation;
- charts e helmfiles passam validação automatizada;
- bootstrap tem smoke checks;
- componentes críticos têm checagem de saúde explícita.

`Evidência esperada`

- `kubeconform`;
- scripts de validation/health-check;
- smoke-test chart e runbooks de validação.

`Notas LoopZero`

- em uma PaaS, infra sem validation gate vira origem recorrente de regressão de produto.

## Gate INF-G07: GitOps Root Of Truth For Day-2

`Objetivo`

Fazer de Git o source of truth para operação contínua do cluster.

`Critério de aprovação`

- recursos de day-2 entram por ArgoCD/Application manifests;
- App-of-Apps ou estrutura equivalente tem raiz explícita;
- mudanças manuais no cluster são detectáveis e corrigíveis.

`Evidência esperada`

- `infra/argocd-bootstrap/root-app.yaml`;
- apps `mgmt-*`, `build-*`, `runtime-*`;
- runbooks/alerts de drift.

`Notas LoopZero`

- esse gate não elimina bootstrap inicial fora do Argo; ele delimita onde o Argo passa a governar.

## Gate INF-G08: Secure Defaults

`Objetivo`

Impor defaults seguros na fundação da plataforma.

`Critério de aprovação`

- probes, resources e non-root estão presentes quando aplicável;
- políticas de admissão e supply chain são integradas ao bootstrap;
- segredos têm geração e rotação operacional claras;
- serviços de base não nascem em modo permissivo sem razão explícita.

`Evidência esperada`

- charts/values seguros;
- policy-controller e admission policies;
- runbooks de secret rotation.

`Notas LoopZero`

- em uma PaaS, defaults inseguros se multiplicam por todos os tenants e apps.

## Gate INF-G09: Multi-Cluster Wiring Correctness

`Objetivo`

Assegurar que management, build e runtime planes estejam ligados corretamente.

`Critério de aprovação`

- kubeconfigs, cluster secrets e wiring configmaps são gerados e consumidos corretamente;
- falhas de wiring são detectáveis;
- cross-cluster assumptions ficam documentadas e testáveis.

`Evidência esperada`

- scripts como `wire-clusters.sh`;
- health checks cross-cluster;
- parity/runbooks específicos.

`Notas LoopZero`

- este gate é absolutamente crítico para uma PaaS multi-plano.

## Gate INF-G10: Observability First

`Objetivo`

Garantir que a plataforma seja observável desde a fundação, não só depois de incidentes.

`Critério de aprovação`

- métricas, logs, tracing e alerting fazem parte do bootstrap;
- runbooks acompanham alerts principais;
- há visibilidade por plano e por cluster.

`Evidência esperada`

- charts de monitoring, promtail, tempo, otel;
- runbooks em `docs/runbooks/alerts/`;
- alertas multi-cell e de drift.

`Notas LoopZero`

- em infra de PaaS, falta de observabilidade é bug de arquitetura.

## Gate INF-G11: Recovery And Repair Readiness

`Objetivo`

Tratar recuperação como capability nativa da plataforma.

`Critério de aprovação`

- há procedimentos explícitos para restore, cleanup, break-glass e rollback;
- scripts críticos de repair são controlados e documentados;
- o operador não depende de conhecimento tribal para recuperar estado.

`Evidência esperada`

- `infra/scripts/break-glass.sh`;
- runbooks de pulumi state, cleanup, cutover, cluster cleanup;
- alerts com ação operacional definida.

`Notas LoopZero`

- isso é especialmente importante porque o LoopZero já opera componentes stateful e multi-cluster.

## Gate INF-G12: Upgrade And Rollout Safety

`Objetivo`

Permitir upgrades controlados de componentes de plataforma e fundação.

`Critério de aprovação`

- mudanças de versão são pinadas;
- rollout tem ordem clara;
- rollback/cutover é conhecido antes da promoção;
- HA/migração de serviços base segue plano deliberado.

`Evidência esperada`

- values pinados;
- runbooks de cutover/migration;
- bootstrap wave validation.

`Notas LoopZero`

- em uma PaaS, upgrade de Gitea, Harbor, Postgres, Argo e Ory não pode ser aventura manual.

## Gate INF-G13: Drift Visibility And Control

`Objetivo`

Evitar que a plataforma se afaste silenciosamente do estado declarado.

`Critério de aprovação`

- drift de IaC e drift de GitOps têm visibilidade;
- correção de drift é documentada;
- mudanças fora do código geram sinal operacional.

`Evidência esperada`

- alertas de drift;
- scripts de regression check;
- runbooks de repair e parity validation.

`Notas LoopZero`

- drift é inevitável em sistemas vivos; invisibilidade de drift não é aceitável.

## Gate INF-G14: Capacity And Resilience Validation

`Objetivo`

Testar a plataforma nas condições em que PaaS falham de verdade: carga, quota, falha parcial e pressão operacional.

`Critério de aprovação`

- existem testes de load, chaos, parity e capacity;
- cells e quotas têm sinais operacionais;
- componentes críticos têm runbooks de saturação/falha.

`Evidência esperada`

- `infra/tests/load`, `infra/tests/chaos`, `infra/tests/capacity`;
- alertas como quota exceeded e backlog;
- runbooks multi-cell.

`Notas LoopZero`

- esse gate sobe de importância conforme a plataforma amadurece para mais tenants e cells.

## Gate INF-G15: Environment Contracts Are Explicit

`Objetivo`

Reduzir ambiguidade entre dev, prod, standalone e multi-cell.

`Critério de aprovação`

- environments e values por ambiente são nomeados e versionados;
- contratos entre stacks e charts são explícitos;
- não há dependência em env vars mágicas não documentadas.

`Evidência esperada`

- `infra/helmfile/environments/*.yaml`;
- arquivos de values específicos por ambiente;
- docs de boundaries e contracts.

`Notas LoopZero`

- esse gate é chave para evitar degeneração em “prod funciona porque alguém sabe como”.

## Ordem De Prioridade

1. `INF-G01`, `INF-G02`, `INF-G04`, `INF-G05`, `INF-G09`
2. `INF-G03`, `INF-G06`, `INF-G07`, `INF-G08`, `INF-G10`
3. `INF-G11`, `INF-G12`, `INF-G13`, `INF-G14`, `INF-G15`

Justificativa:

- primeiro, separação correta, bootstrap e wiring;
- depois, governança contínua, segurança e observabilidade;
- por fim, repair, rollout e escala.
