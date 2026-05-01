# Charts SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo`
- `Critério de aprovação`
- `Evidência esperada`
- `Notas LoopZero`

Em uma PaaS, Helm chart é parte do contrato de entrega da plataforma. Então a régua correta inclui segurança, upgrade, GitOps e previsibilidade operacional.

Regra transversal de validação:

- `helm lint`, render e validação estrutural são necessários;
- para charts críticos de plataforma, isso ainda não basta sem instalação real ou parity validation suficiente para provar upgrade, observabilidade e comportamento operacional.

## Gate CH-G01: Clear Chart Role

`Objetivo`

Garantir que cada chart tenha responsabilidade clara: library, app, CRD, umbrella, monitoring ou policy.

`Critério de aprovação`

- o papel do chart é explícito;
- o chart não mistura responsabilidades incompatíveis;
- subcharts e dependências refletem composição real da plataforma.

`Evidência esperada`

- `Chart.yaml` e estrutura do chart coerentes com seu papel;
- docs e values alinhados à responsabilidade.

`Notas LoopZero`

- `loopzero-common`, `loopzero-crds`, `loopzero-platform`, `loopzero-monitoring` e `loopzero-admission-policies` já ilustram papéis distintos.

## Gate CH-G02: Safe Defaults

`Objetivo`

Fazer o caminho padrão de instalação ser seguro e operável.

`Critério de aprovação`

- securityContext seguro por default;
- requests/limits definidos quando aplicável;
- probes configuradas de forma razoável;
- serviceAccount e automount seguem princípio do menor privilégio.

`Evidência esperada`

- helpers da library chart;
- templates de deployment consistentes;
- values com defaults explícitos.

`Notas LoopZero`

- `loopzero-common` já é base importante desse gate.

## Gate CH-G03: Stable Values Contract

`Objetivo`

Tratar `values.yaml` como API pública operacional do chart.

`Critério de aprovação`

- chaves de values são previsíveis e documentáveis;
- renomeações e remoções são tratadas como breaking changes operacionais;
- nested/flat balance prioriza clareza do usuário do chart.

`Evidência esperada`

- values organizados;
- ausência de comportamento oculto difícil de descobrir;
- perfis/overrides por ambiente coerentes.

`Notas LoopZero`

- esse gate é central porque ArgoCD, operadores humanos e perfis de ambiente dependem dessa API.

## Gate CH-G04: No Product Logic In Templates

`Objetivo`

Impedir que templates Helm virem camada de regra de negócio.

`Critério de aprovação`

- templates expressam packaging e wiring operacional;
- decisões de produto não ficam escondidas em if/else de Helm;
- lógica complexa é resolvida no componente, não no chart.

`Evidência esperada`

- templates relativamente declarativos;
- ausência de branching excessivo que substitua comportamento do app/operator.

`Notas LoopZero`

- chart deve empacotar, não governar semântica do sistema.

## Gate CH-G05: Upgrade-Safe Packaging

`Objetivo`

Reduzir risco em upgrades de plataforma e componentes.

`Critério de aprovação`

- `version` e `appVersion` têm semântica deliberada;
- mudanças de template e dependency bump são controladas;
- rollout preserva compatibilidade de values sempre que possível.

`Evidência esperada`

- `Chart.lock` e dependências pinadas;
- versionamento consistente;
- cuidado especial com CRDs e policies.

`Notas LoopZero`

- em uma PaaS, upgrade ruim de chart pode indisponibilizar múltiplos componentes de uma vez.

## Gate CH-G06: CRD Lifecycle Separation

`Objetivo`

Tratar CRDs como artefatos com lifecycle próprio.

`Critério de aprovação`

- CRDs ficam em chart dedicado ou estratégia equivalente explícita;
- update e instalação de CRDs não são acoplados implicitamente ao restante da plataforma;
- charts consumidores assumem que a API declarativa existe, não a recriam silenciosamente.

`Evidência esperada`

- `loopzero-crds` separado;
- templates dedicados às CRDs;
- uso deliberado no umbrella/platform flow.

`Notas LoopZero`

- esse gate já é uma força clara do repositório.

## Gate CH-G07: Library Chart Consistency

`Objetivo`

Usar o library chart para convergir padrões, sem forçar abstrações ruins.

`Critério de aprovação`

- helpers cobrem padrões genuinamente compartilhados;
- charts consumidores adotam os helpers onde faz sentido;
- o library chart não cresce como framework opaco.

`Evidência esperada`

- `_helpers.tpl` útil e legível;
- charts dependentes com uso consistente;
- ausência de helper genérico demais sem benefício claro.

`Notas LoopZero`

- `loopzero-common` é ativo arquitetural; também pode virar fonte de acoplamento ruim se crescer sem disciplina.

## Gate CH-G08: Observability By Packaging

`Objetivo`

Fazer com que charts operacionais publiquem sinais mínimos necessários.

`Critério de aprovação`

- components que expõem métricas têm `ServiceMonitor` ou estratégia equivalente;
- health, ports e labels são suficientes para monitoramento;
- monitoring charts são tratados como parte da plataforma.

`Evidência esperada`

- `servicemonitor.yaml`;
- dashboards e `PrometheusRule`;
- labels consistentes.

`Notas LoopZero`

- `loopzero-monitoring` e `ServiceMonitor` nos charts de app/operator já apontam nessa direção.

## Gate CH-G09: Policy Packaging As Contract

`Objetivo`

Empacotar guardrails de plataforma como first-class artifacts.

`Critério de aprovação`

- admission policies são declarativas, versionáveis e revisáveis;
- falhas de policy têm comportamento previsível;
- chart de policy reflete a postura de segurança da plataforma.

`Evidência esperada`

- `loopzero-admission-policies`;
- uso de `ValidatingAdmissionPolicy` e bindings;
- values claros para escopo/enablement.

`Notas LoopZero`

- em uma PaaS multi-tenant, policy chart é parte do produto, não apêndice.

## Gate CH-G10: Umbrella Composition Without Hidden Coupling

`Objetivo`

Permitir composição de plataforma sem esconder dependências perigosas.

`Critério de aprovação`

- o umbrella chart explicita dependências e conditions;
- subcharts podem ser entendidos e operados individualmente;
- profiles de ambiente não substituem entendimento da topologia real.

`Evidência esperada`

- `loopzero-platform/Chart.yaml`;
- perfis em `profiles/*`;
- conditions explícitas por dependency.

`Notas LoopZero`

- umbrella chart útil não pode virar “caixa-preta que instala tudo”.

## Gate CH-G11: Published Charts And Bootstrap Charts Stay Deliberately Aligned

`Objetivo`

Evitar divergência silenciosa entre `charts/` e `infra/helm/`.

`Critério de aprovação`

- diferenças entre published charts e bootstrap charts são intencionais e compreendidas;
- padrões compartilhados relevantes são propagados;
- mudança em um lado dispara revisão do outro quando necessário.

`Evidência esperada`

- documentação dessa dualidade;
- revisão cruzada em mudanças sensíveis de defaults e segurança.

`Notas LoopZero`

- esse gate é específico do LoopZero e muito importante.

## Gate CH-G12: Render And Validation Cleanliness

`Objetivo`

Garantir que o chart seja instalável e validável antes de ir para cluster.

`Critério de aprovação`

- chart renderiza sem erro;
- manifestos resultantes passam validação estrutural;
- dependências estão resolvidas e lockadas.

`Evidência esperada`

- uso de `helm template`, `helm lint`, `kubeconform` ou equivalente no fluxo;
- `Chart.lock` quando há dependências.

`Notas LoopZero`

- esse gate é básico, mas inegociável.

## Gate CH-G13: RBAC Explicitness

`Objetivo`

Tratar permissões como contrato instalável e auditável.

`Critério de aprovação`

- charts que precisam de RBAC o declaram explicitamente;
- service accounts e bindings acompanham o chart;
- permissões não ficam implícitas em instalação manual.

`Evidência esperada`

- `clusterrole.yaml`, `role.yaml`, `serviceaccount.yaml`, bindings;
- defaults mínimos razoáveis.

`Notas LoopZero`

- operators e API exigem atenção alta aqui.

## Gate CH-G14: Environment Profiles Stay Operational, Not Magical

`Objetivo`

Usar profiles/values por ambiente sem esconder acoplamento crítico.

`Critério de aprovação`

- profiles representam variação legítima de ambiente;
- não viram substituto para documentação de topologia;
- nome e escopo dos profiles são claros.

`Evidência esperada`

- `values-prod.yaml`, `values-standalone-multi.yaml`, `profiles/*`;
- revisão de impacto entre ambientes.

`Notas LoopZero`

- perfis são necessários, mas aumentam custo de compatibilidade e teste.

## Ordem De Prioridade

1. `CH-G02`, `CH-G03`, `CH-G05`, `CH-G06`, `CH-G12`
2. `CH-G01`, `CH-G07`, `CH-G08`, `CH-G09`, `CH-G13`
3. `CH-G04`, `CH-G10`, `CH-G11`, `CH-G14`

Justificativa:

- primeiro, defaults seguros, values estáveis, upgrades e validade;
- depois, consistência estrutural e sinais operacionais;
- por fim, composição avançada e governança entre famílias de chart.
