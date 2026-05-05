# Charts SOTA Gates

## Como Ler

Cada gate abaixo define:

- `Objetivo` — intenção macro do gate
- `Critério de aprovação` — assertions verificáveis (idealmente automatizáveis)
- `Evidência esperada` — onde a evidência mora hoje OU onde deve passar a morar
- `Status atual` — MET / PARTIAL / NOT_MET com referência a arquivos
- `Notas LoopZero` — contexto específico do projeto

Em uma PaaS, Helm chart é parte do contrato de entrega da plataforma. Então a régua correta inclui segurança, upgrade, GitOps e previsibilidade operacional.

Regra transversal de validação:

- `helm lint --strict`, `helm template`, `kubeconform --strict`, `helm-unittest` e `ct install` são complementares — nenhum substitui o outro.
- Para charts críticos de plataforma, assinatura cosign + verificação obrigatórios.
- Drift entre `infra/helm/` e `charts/` precisa ser detectado por automação, não confiado a revisão humana.

---

## Gate CH-G01: Clear Chart Role

**Objetivo**

Garantir que cada chart tenha responsabilidade clara: library, app, CRD, umbrella, monitoring ou policy.

**Critério de aprovação**

- O papel do chart é explícito em `Chart.yaml` (`type: library` para shared, `type: application` para deployable).
- O chart não mistura responsabilidades incompatíveis (ex: library com Deployment, app sem Pod-bearing resource).
- Subcharts e dependências refletem composição real da plataforma.
- Cada chart consegue ser explicado em 1 frase de `description` (Chart.yaml).

**Evidência esperada**

- `Chart.yaml` e estrutura do chart coerentes com seu papel.
- Documentação no docs/technical/charts/architecture.md mapeando cada chart.

**Status atual**

PARTIAL — `loopzero-controller` e `loopzero-tenant-controller` aparecem em `loopzero-platform/Chart.lock` mas não têm `Chart.yaml` (apenas `charts/loopzero-common-0.2.0.tgz` vendorado). Violam o gate. Quick-win #1 do roadmap.

**Notas LoopZero**

- `loopzero-common`, `loopzero-crds`, `loopzero-platform`, `loopzero-monitoring` e `loopzero-admission-policies` já ilustram papéis distintos.
- Após split de operadores (ADR-0023), os 4 operator charts (`loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`) substituem o monolito anterior.

---

## Gate CH-G02: Safe Defaults

**Objetivo**

Fazer o caminho padrão de instalação ser seguro e operável.

**Critério de aprovação**

- `securityContext` segue PSA `restricted` por default: `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`, `seccompProfile.type: RuntimeDefault`.
- `requests` e `limits` definidos quando aplicável (deployments com workload real, não CRD-only charts).
- Probes (`livenessProbe`, `readinessProbe`, `startupProbe`) configuradas — startup é obrigatório para componentes com cold start > 5s.
- `serviceAccount.automountServiceAccountToken: false` exceto para componentes que consomem K8s API.
- `priorityClassName` definido para componentes platform-critical (operators, API).

**Evidência esperada**

- `charts/loopzero-common/templates/_helpers.tpl` define `loopzero-common.podSecurityContext` (linhas 117-125), `loopzero-common.containerSecurityContext` (linhas 130-140), `loopzero-common.startupProbe` (linhas 195-204), `loopzero-common.priorityClassName` (linhas 180-184).
- Todo deployment chart usa `{{- include "loopzero-common.containerSecurityContext" . | nindent 12 }}`.
- `charts/loopzero-api/tests/security-context_test.yaml` valida 6 invariantes em CI.

**Status atual**

MET — `loopzero-common` hardcoda PSA-restricted. Verificado em todos os 5 deployment templates (api + 4 operators). helm-unittest valida em 5/13 charts.

**Notas LoopZero**

- `loopzero-common` já é base importante desse gate.
- Helpers PSA-restricted são intencionais (não configuráveis via values) para impedir downgrade silencioso.

---

## Gate CH-G03: Stable Values Contract

**Objetivo**

Tratar `values.yaml` como API pública operacional do chart, validada por `values.schema.json`.

**Critério de aprovação**

- Todo chart `type: application` tem `values.schema.json` (Draft-07 ou Draft-2020-12).
- Schema valida ao menos: `image.repository` (regex container ref), `image.digest` (regex `^sha256:[a-f0-9]{64}$`), `replicaCount` (integer ≥ 0), `service.port` (1-65535), `service.type` (enum), `resources.limits|requests` (string|number).
- `additionalProperties: true` no top-level (compat-safe Helm 3.18.5) mas considerar `false` em sub-objetos onde typo é grave.
- `helm lint --strict` é executado em CI e bloqueia merge.
- Renomeações e remoções de chave são tratadas como breaking changes operacionais (bump major em `version`).

**Evidência esperada**

- 12/12 charts deployáveis têm `values.schema.json` (`charts/loopzero-{api,cloud,crds,build-cwts,admission-policies,preview-operator,platform-operator,monitoring,tenant-operator,app-operator,dashboard,platform}/values.schema.json`).
- `.github/workflows/helm-charts.yml` step "Lint all charts (helm lint --strict, INF-G17)" linha 51-65.
- Exemplo de schema rico: `charts/loopzero-api/values.schema.json` com regex para image, digest, port range.

**Status atual**

MET — schemas existem em todos charts deployáveis e CI roda strict. Risco residual: `additionalProperties: true` permite typos passarem em values custom; `loopzero-platform` umbrella não tem schema próprio (Sprint 9 #3).

**Notas LoopZero**

- Esse gate é central porque ArgoCD, operadores humanos e perfis de ambiente dependem dessa API.
- Helm 3.18.5 endureceu validação (`santhosh-tekuri/jsonschema/v6`) — manter `additionalProperties: true` é a escolha defensiva certa para umbrella charts (ver helm/helm#10392).

---

## Gate CH-G04: No Product Logic In Templates

**Objetivo**

Impedir que templates Helm virem camada de regra de negócio.

**Critério de aprovação**

- Templates expressam packaging e wiring operacional (rendering de manifestos K8s).
- Decisões de produto não ficam escondidas em `if/else` profundo de Helm.
- Lógica complexa é resolvida no componente (Go/TypeScript), não no chart.
- Branching em template é proporcional a configuração K8s (existingSecret, autoscaling, networkPolicy enabled), nunca a regra de negócio.

**Evidência esperada**

- Templates relativamente declarativos (≤ 3 níveis de aninhamento de `if`).
- Ausência de cálculos numéricos ou string manipulation além de naming.
- `grep -rn '{{- if and' charts/` retorna casos justificáveis (probe enabled + path defined, etc).

**Status atual**

MET — verificado em `charts/loopzero-tenant-operator/templates/deployment.yaml`: branching limitado a kubeconfig mounts (existingConfigMap), existingSecret, podLabels override. Sem lógica de produto.

**Notas LoopZero**

- Chart deve empacotar, não governar semântica do sistema.
- Operadores e API têm lógica em Go; charts só descrevem como instalar.

---

## Gate CH-G05: Upgrade-Safe Packaging

**Objetivo**

Reduzir risco em upgrades de plataforma e componentes.

**Critério de aprovação**

- `version` (chart) e `appVersion` (componente) têm semântica deliberada (SemVer).
- Mudanças de template e dependency bump são controladas via `Chart.lock`.
- Rollout preserva compatibilidade de values sempre que possível; quebras são major bumps.
- Para charts com hooks ou ordering crítico, `helm.sh/hook-weight` é explícito.
- `helm diff upgrade --detailed-exitcode` é usado como gate em PR para charts críticos.
- CRDs e policies têm separação de lifecycle (CH-G06).

**Evidência esperada**

- `Chart.lock` presente em umbrella e charts com deps (`charts/loopzero-platform/Chart.lock`).
- Pinned versions em deps (linhas 2-40 de Chart.lock — todas exatas, não ranges).
- Histórico de version bumps trackável em git.
- Em produção (Post-Launch): step CI `helm-diff` em PR.

**Status atual**

PARTIAL — Chart.lock existe e versions são pinadas. `helm-diff` está disponível como prerequisite (CLAUDE.md) mas não é usado em CI gate. Sem teste de upgrade matrix (chart-testing `--upgrade`).

**Notas LoopZero**

- Em uma PaaS, upgrade ruim de chart pode indisponibilizar múltiplos componentes de uma vez.
- Sprint 9 #2 do roadmap adiciona `ct install --upgrade` em matriz K8s.

---

## Gate CH-G06: CRD Lifecycle Separation

**Objetivo**

Tratar CRDs como artefatos com lifecycle próprio.

**Critério de aprovação**

- CRDs ficam em chart dedicado (`loopzero-crds`) com `Chart.yaml` separado.
- Update e instalação de CRDs não são acoplados implicitamente ao restante da plataforma.
- Charts consumidores (operators) assumem que a API declarativa existe, não a recriam silenciosamente.
- CI valida que CRDs em `operators/config/crd/bases/` correspondem a `charts/loopzero-crds/templates/`.

**Evidência esperada**

- `charts/loopzero-crds/Chart.yaml` separado.
- 10 templates em `charts/loopzero-crds/templates/` (appsecrets, clustercredentials, environments, exposedroutes, gitrepositorybindings, platformbootstraps, previewenvironments, reconciliationreports, tenants, loopzeroapplications).
- `.github/workflows/helm-charts.yml` job `crd-sync` (linhas 215-247) compara count e nomes.

**Status atual**

PARTIAL — separation MET; drift check existe mas só compara `metadata.name`, não `spec` interno. Mudanças em `spec.versions[].schema` passariam silenciosamente. Quick-win #4 do roadmap fecha.

**Notas LoopZero**

- Esse gate já é uma força clara do repositório.
- Drift detector semântico é o próximo passo.

---

## Gate CH-G07: Library Chart Consistency

**Objetivo**

Usar o library chart para convergir padrões, sem forçar abstrações ruins.

**Critério de aprovação**

- Helpers cobrem padrões genuinamente compartilhados (≥ 2 charts consumidores).
- Charts consumidores adotam os helpers onde faz sentido (ex: deployment chama `loopzero-common.containerSecurityContext`, não reimplementa).
- O library chart não cresce como framework opaco.
- Cada helper é nomeado com prefixo `loopzero-common.` e tem 1 responsabilidade.

**Evidência esperada**

- `charts/loopzero-common/templates/_helpers.tpl` — 16 helpers atuais (name, fullname, labels, operatorLabels, selectorLabels, image, podSecurityContext, containerSecurityContext, serviceAccountName, automountServiceAccountToken, priorityClassName, startupProbe, affinity, topologySpreadConstraints, servicemonitor, pdb, networkpolicy).
- 9 charts deployáveis declaram dependency em `loopzero-common >=0.1.0 <1.0.0`.
- `grep -rn 'include "loopzero-common.' charts/loopzero-{api,cloud,dashboard,*-operator}/templates/` retorna ≥ 5 chamadas por chart.

**Status atual**

PARTIAL — helpers existem e são adotados, mas `loopzero-common` lacks 6 helpers Bitnami-style (`tplvalues.render`, `kubeVersion`, `checksumAnnotations`, `imagePullSecrets`, `secrets.name`, `tmpVolumeMount`). Padrões duplicados em 5 deployments (Sprint 9 #1 do roadmap fecha).

**Notas LoopZero**

- `loopzero-common` é ativo arquitetural; também pode virar fonte de acoplamento ruim se crescer sem disciplina.
- Bitnami common (40+ helpers) é referência mas só portar o que tem caso de uso real.

---

## Gate CH-G08: Observability By Packaging

**Objetivo**

Fazer com que charts operacionais publiquem sinais mínimos necessários.

**Critério de aprovação**

- Componentes que expõem métricas têm `ServiceMonitor` (Prometheus Operator) ou estratégia equivalente, condicional via `metrics.serviceMonitor.enabled`.
- Health, ports e labels são suficientes para monitoramento.
- Monitoring charts (`loopzero-monitoring`) são tratados como parte da plataforma, não opcional.
- `app.kubernetes.io/component` é definido para fan-out queries (ex: `kubectl logs -l app.kubernetes.io/component=loopzero-operator -A`).

**Evidência esperada**

- `charts/loopzero-common/templates/_helpers.tpl` linhas 267-284 (`loopzero-common.servicemonitor`).
- `charts/loopzero-tenant-operator/templates/servicemonitor.yaml` + tests/servicemonitor_test.yaml.
- `charts/loopzero-monitoring/templates/` com PrometheusRules (10 rules) + dashboards.
- `app.kubernetes.io/component: loopzero-operator` consistente nos 4 operadores (helper `loopzero-common.operatorLabels`).

**Status atual**

PARTIAL — ServiceMonitor + PrometheusRules existem; falta unit test em `loopzero-monitoring` (8/13 charts sem unit test). Cobertura de tests para servicemonitor render existe em 4 operadores + loopzero-api.

**Notas LoopZero**

- `loopzero-monitoring` é apenas rules + dashboards, NÃO o stack Prometheus (este vive em outro chart bootstrap). Renomear para `loopzero-monitoring-rules` reduz ambiguidade (anti-pattern #9 do roadmap).

---

## Gate CH-G09: Policy Packaging As Contract

**Objetivo**

Empacotar guardrails de plataforma como first-class artifacts.

**Critério de aprovação**

- Admission policies são declarativas, versionáveis e revisáveis.
- Falhas de policy têm comportamento previsível (warn vs deny via `failurePolicy`).
- Chart de policy reflete a postura de segurança da plataforma published.
- Bootstrap (`infra/helm/admission-policies/`) e published (`charts/loopzero-admission-policies/`) NÃO drift silencioso — diferenças são intencionais e documentadas.

**Evidência esperada**

- `charts/loopzero-admission-policies/templates/` com VAPs (vap-disallow-latest-tag, vap-protect-platform-networkpolicies, vap-require-image-signature, vap-require-resource-limits, vap-restrict-registries, vap-argocd-managed-by).
- ClusterImagePolicy (`cluster-image-policy.yaml`) integrado a Sprint 6 cosign.

**Status atual**

NOT_MET — drift severo: `infra/helm/admission-policies/` tem 11 templates, `charts/loopzero-admission-policies/` tem 6. Faltam no published: `cluster-image-policy.yaml`, `policy-controller-alerts.yaml`, `tenant-namespace-alerts.yaml`, `vap-no-runtime-image-mutations.yaml`, `vap-tenant-namespace.yaml`. Inversão da expectativa (production tem mais policy do que ArgoCD-distributable). Quick-win #2 do roadmap fecha.

**Notas LoopZero**

- Em uma PaaS multi-tenant, policy chart é parte do produto, não apêndice.
- Drift atual é **regressão** vs Sprint 6 ship — published deveria ser superset.

---

## Gate CH-G10: Umbrella Composition Without Hidden Coupling

**Objetivo**

Permitir composição de plataforma sem esconder dependências perigosas.

**Critério de aprovação**

- O umbrella chart explicita dependências e conditions (`<dep>.enabled`).
- Subcharts podem ser entendidos e operados individualmente.
- Profiles de ambiente não substituem entendimento da topologia real.
- Toda dep declarada resolve para chart com `Chart.yaml` válido (gate CH-G15).
- `Chart.lock` é commitado e pinado.

**Evidência esperada**

- `charts/loopzero-platform/Chart.yaml` lista 12 deps (loopzero-common + 11 sub-charts) com `condition: <name>.enabled`.
- `charts/loopzero-platform/values.yaml` define toggles para cada sub-chart.
- `charts/loopzero-platform/profiles/` para variação por ambiente.

**Status atual**

PARTIAL — composição explícita MET; mas 2 deps (`loopzero-controller`, `loopzero-tenant-controller`) são ghost charts sem `Chart.yaml` → CH-G15 violado, CH-G10 PARTIAL. Quick-win #1 fecha.

**Notas LoopZero**

- Umbrella chart útil não pode virar "caixa-preta que instala tudo".
- Pós-split de operadores (ADR-0023), a composição deve ser revisada para refletir 4 binários separados, não monolito.

---

## Gate CH-G11: Published Charts And Bootstrap Charts Stay Deliberately Aligned

**Objetivo**

Evitar divergência silenciosa entre `charts/` e `infra/helm/`.

**Critério de aprovação**

- Diferenças entre published charts e bootstrap charts são intencionais e compreendidas.
- Padrões compartilhados relevantes (security context, resources, labels) são propagados.
- Mudança em um lado dispara revisão do outro quando necessário.
- Existe **automação** que detecta drift, não apenas convenção.
- Para cada drift mantido deliberadamente, existe ADR ou comentário explicando.

**Evidência esperada**

- Script `infra/scripts/audit-helm-drift.sh` (proposto, hoje inexistente).
- CI step `audit-drift` em `.github/workflows/helm-charts.yml`.
- ADRs documentando drifts intencionais (ex: bootstrap minimal vs published full).

**Status atual**

NOT_MET — gate é "paper gate" sem automação. Drift atual: (a) `loopzero-api` published tem 12 templates, bootstrap tem ~3 (intencional mas não documentado); (b) `loopzero-admission-policies` published tem 6, bootstrap tem 11 (regressão); (c) `loopzero-cloud` overlap não inspecionado por ferramenta. Apenas CRDs têm drift detector. Quick-win #3 do roadmap fecha (script + step CI).

**Notas LoopZero**

- Esse gate é específico do LoopZero e muito importante.
- Pré-release é o momento de fechar; pós-launch só piora.

---

## Gate CH-G12: Render And Validation Cleanliness

**Objetivo**

Garantir que o chart seja instalável e validável antes de ir para cluster.

**Critério de aprovação**

- Chart renderiza sem erro (`helm template`).
- Manifestos resultantes passam validação estrutural (`kubeconform --strict`).
- Dependências estão resolvidas e lockadas.
- `helm lint --strict` passa (com schema validation).
- helm-unittest cobre comportamento crítico (security, networking, observability).

**Evidência esperada**

- `.github/workflows/helm-charts.yml` jobs `lint` (helm lint --strict + helm-unittest) e `validate` (helm template + kubeconform).
- Cobertura de helm-unittest: 5/13 charts (api + 4 operators).
- `Chart.lock` em `loopzero-platform`.

**Status atual**

PARTIAL — render + lint + kubeconform MET. helm-unittest cobre 5/13 charts (38%); 8 charts sem unit test (cloud, crds, dashboard, build-cwts, monitoring, admission-policies, platform, common). Sem K8s version matrix (kubeconform 1.31.0 only). Sem chart-testing (ct) install/upgrade. Quick-wins #5 + Sprint 9 #2/#4 fecham.

**Notas LoopZero**

- Esse gate é básico, mas inegociável.
- Próximo nível: ct install + matrix K8s + chartsnap.

---

## Gate CH-G13: RBAC Explicitness

**Objetivo**

Tratar permissões como contrato instalável e auditável.

**Critério de aprovação**

- Charts que precisam de RBAC o declaram explicitamente.
- ServiceAccounts, ClusterRoles, ClusterRoleBindings acompanham o chart.
- Permissões não ficam implícitas em instalação manual.
- Defaults mínimos razoáveis (princípio do menor privilégio).
- `automountServiceAccountToken: false` por padrão; `true` apenas para K8s API consumers.

**Evidência esperada**

- `charts/loopzero-{tenant,app,preview,platform}-operator/templates/clusterrole.yaml` + `clusterrolebinding.yaml` + `serviceaccount.yaml` para cada operador.
- `charts/loopzero-api/templates/clusterrole.yaml` (API client de K8s).
- `loopzero-common.serviceAccountName` (linhas 151-157 de _helpers.tpl) e `loopzero-common.automountServiceAccountToken` (linhas 162-168).

**Status atual**

MET — verificado em 4 operadores + API. Pós-split (ADR-0023), cada operator tem ServiceAccount + ClusterRole próprio com escopo mínimo. `automountServiceAccountToken` controlado via values.

**Notas LoopZero**

- Operators e API exigem atenção alta aqui.
- Sprint 6 + ADR-0023 ratificaram split de RBAC.

---

## Gate CH-G14: Environment Profiles Stay Operational, Not Magical

**Objetivo**

Usar profiles/values por ambiente sem esconder acoplamento crítico.

**Critério de aprovação**

- Profiles representam variação legítima de ambiente (dev/homolog/prod).
- Não viram substituto para documentação de topologia.
- Nome e escopo dos profiles são claros.
- Cada profile tem 1 dimensão clara de variação (ambiente, escala, isolamento).

**Evidência esperada**

- `charts/loopzero-platform/profiles/` com values overrides.
- `values-prod.yaml`, `values-standalone-multi.yaml` ou similar.
- Revisão de impacto entre ambientes documentada em PR.

**Status atual**

PARTIAL — `profiles/` dir existe; volume de variação não inspecionado a fundo. Risco: profile composition não tem schema, typos passam.

**Notas LoopZero**

- Perfis são necessários, mas aumentam custo de compatibilidade e teste.
- Sprint 9 #3 do roadmap (schema do umbrella) reduz risco.

---

## Gate CH-G15: No Ghost Charts In Umbrella (NEW)

**Objetivo**

Garantir que toda dependência declarada em `loopzero-platform/Chart.yaml` resolva para um chart com `Chart.yaml` válido e templates renderizáveis.

**Critério de aprovação**

- Para cada entrada em `loopzero-platform/dependencies`, existe `charts/<name>/Chart.yaml` válido.
- Para cada entrada, `helm template charts/<name>` produz pelo menos um manifesto não-vazio.
- CI step `audit-ghost-charts` falha se algum dep é fantasma.
- `Chart.lock` é regenerado com `helm dependency update` sem warnings de "missing chart".

**Evidência esperada**

- Job `audit-ghost-charts` em `.github/workflows/helm-charts.yml`.
- `loopzero-controller` e `loopzero-tenant-controller` removidos OU reconstruídos com `Chart.yaml`.

**Status atual**

NOT_MET — `loopzero-platform/Chart.lock` linhas 23-25 referenciam `loopzero-controller` que não tem `Chart.yaml`. Mesmo para `loopzero-tenant-controller` (não está no Chart.lock atual mas dir existe vazio). Quick-win #1 do roadmap fecha em 1 commit.

**Notas LoopZero**

- Ghost charts são dead code que sobreviveu ao split de operadores (ADR-0023).
- FAANG reference: Bitnami nunca publica subchart com `Chart.yaml` ausente; é parte do CI de release.

---

## Gate CH-G16: Drift Detector Between Bootstrap And Published (NEW)

**Objetivo**

Operacionalizar CH-G11 com automação. Toda mudança em `infra/helm/<X>/` ou `charts/loopzero-<X>/` que afete overlaps reais ou conceituais emite report obrigatório.

**Critério de aprovação**

- Existe `infra/scripts/audit-helm-drift.sh` que detecta:
  - Templates presentes em um lado mas não no outro.
  - Diferenças de defaults críticos: `securityContext`, `resources`, `priorityClassName`, `image.repository`.
- CI step `audit-drift` emite warning (não-bloqueante inicialmente, bloqueante após Sprint 9 GA).
- Drift detectado vira comment em PR via `actions/github-script`.

**Evidência esperada**

- Script + step CI.
- 1 ADR documentando "diferença intencional" para cada drift que **deve** persistir (e.g., bootstrap `loopzero-api` minimal para dev local vs published full para production).

**Status atual**

NOT_MET — sem ferramenta. Drifts conhecidos não-documentados: loopzero-api (12 vs 3 templates), loopzero-admission-policies (6 vs 11 templates, regressão). Quick-win #3 do roadmap.

**Notas LoopZero**

- Pré-release é o momento de criar disciplina de drift; pós-launch só vira drift permanente.
- FAANG reference: Crossplane usa `helm-drift` em CI para confirmar que provider chart matches operator state.

---

## Gate CH-G17: Library Chart Helper Coverage (NEW)

**Objetivo**

`loopzero-common` deve fornecer helpers para todos os padrões duplicados detectados nos templates atuais, eliminando copy-paste cross-chart.

**Critério de aprovação**

- `_helpers.tpl` define ao menos: `name`, `fullname`, `labels`, `operatorLabels`, `selectorLabels`, `image`, `podSecurityContext`, `containerSecurityContext`, `serviceAccountName`, `automountServiceAccountToken`, `priorityClassName`, `startupProbe`, `affinity`, `topologySpreadConstraints`, `servicemonitor`, `pdb`, `networkpolicy`, **`tplvalues.render`**, **`kubeVersion`**, **`checksumAnnotations`**, **`imagePullSecrets`**, **`secrets.name`**, **`tmpVolumeMount`** (em negrito = novos no Sprint 9).
- Cada helper tem teste em `charts/loopzero-common/tests/<helper>_test.yaml`.
- Nenhum chart cliente reimplementa lógica equivalente em template próprio.

**Evidência esperada**

- 21 helpers documentados em `charts/loopzero-common/README.md` (a criar).
- Suite helm-unittest cobrindo cada helper.
- `grep -rn 'checksum/config:' charts/loopzero-*/templates/` retorna apenas `{{ include "loopzero-common.checksumAnnotations" . }}` (zero reimplementação inline).

**Status atual**

PARTIAL — 16 helpers existem, 5 a 6 padrões duplicados detectados nos 5 deployment templates (checksum, tmpVolumeMount, secrets.name, imagePullSecrets, etc). Sprint 9 #1 do roadmap.

**Notas LoopZero**

- Não inflar `loopzero-common` com helpers especulativos — só portar o que tem ≥ 2 caso de uso real.
- FAANG reference: Bitnami common 2.x tem 40+ helpers; cada chart consumer chama via `include` puro.

---

## Gate CH-G18: Multi-K8s Version Matrix In Chart-Testing (NEW)

**Objetivo**

Charts publicados devem renderizar e instalar em N-1, N, N+1 versões K8s (alinhado com janela de suporte upstream).

**Critério de aprovação**

- `ct.yaml` define `kubernetes-versions: [v1.30.0, v1.31.0, v1.32.0]` (atualizar a cada release K8s).
- CI job `install-test` roda `ct install --config ct.yaml --upgrade` em matriz, falha em qualquer versão.
- `kubeconform --kubernetes-version <version>` é aplicado para cada uma das 3+ versões (hoje 1.31.0 only).
- Versões testadas refletem janela suportada (mínimo: versão atual em DOKS + 1 anterior + 1 futura).

**Evidência esperada**

- Workflow `helm-charts.yml` job `install-test` com `strategy.matrix.k8s-version`.
- Status badge "Charts | K8s 1.30 | 1.31 | 1.32" no README.

**Status atual**

NOT_MET — kubeconform 1.31.0 only; sem ct install. Sprint 9 #2 do roadmap.

**Notas LoopZero**

- DOKS está em 1.32.13 — testar 1.31/1.32/1.33 cobre janela de upgrade DigitalOcean.
- FAANG reference: Elastic helm-charts usa `helpers/matrix.yml`; Bitnami testa em GKE/TKG/AKS simultaneamente.

---

## Gate CH-G19: SBOM Attestation Parity With Container Images (NEW)

**Objetivo**

Cada chart publicado tem SBOM CycloneDX attached como cosign attestation, igual aos container images do Sprint 6.

**Critério de aprovação**

- CI step `attest-charts-sbom` gera `syft <chart>.tgz -o cyclonedx-json` e roda `cosign attest --predicate sbom.json --type cyclonedx <REF>`.
- Attestation é verificável: `cosign verify-attestation --type cyclonedx <REF> --certificate-identity-regexp ...` retorna 0.
- Sprint 9 ClusterImagePolicy aceita ou requer attestation para charts críticos.

**Evidência esperada**

- Step "Generate + attest SBOM" em `helm-charts.yml`.
- 1 chart como amostra com SBOM verificado em DOCR.

**Status atual**

NOT_MET — cosign sign já operacional (linhas 251-291 de helm-charts.yml, MEET-063), SBOM não. Container images têm syft SBOM (Sprint 6). Charts ficam para trás. Sprint 9 #5 do roadmap.

**Notas LoopZero**

- Charts assinados sem SBOM = supply chain incompleto; Sprint 6 estabeleceu o padrão para imagens, charts devem se alinhar.
- FAANG reference: Sigstore project + Bitnami Secure Images shipping com SBOM in-toto.

---

## Ordem De Prioridade

### Bloco 1 — Inegociável para GA (pré-Sprint 9 ship)

1. **CH-G15** (ghost charts) — quick-win #1, 1 commit
2. **CH-G09** (admission-policies drift) — quick-win #2, ~5 commits
3. **CH-G02** (safe defaults) — MET, manter
4. **CH-G03** (values schema) — MET, manter
5. **CH-G06** (CRD lifecycle) — PARTIAL → quick-win #4 (semantic drift)

### Bloco 2 — Sprint 9 (Polish + Launch)

6. **CH-G11 + CH-G16** (drift detector) — quick-win #3, automação
7. **CH-G07 + CH-G17** (library chart coverage) — Sprint 9 #1
8. **CH-G12 + CH-G18** (validation + matrix) — Sprint 9 #2
9. **CH-G05** (upgrade-safe via ct + helm-diff) — Sprint 9 #2
10. **CH-G19** (SBOM attestation) — Sprint 9 #5

### Bloco 3 — Pós-launch (qualidade contínua)

11. **CH-G04** (no product logic) — verificação periódica
12. **CH-G08** (observability) — fechar gap de unit tests em loopzero-monitoring
13. **CH-G10** (umbrella composition) — depende de CH-G15 + CH-G16
14. **CH-G13** (RBAC) — manutenção
15. **CH-G14** (profiles) — schema do umbrella + revisão semestral

### Justificativa

- **Bloco 1**: gates onde violação atual é defeito imediato (ghost charts, drift de policy regressivo) ou fundação inegociável (defaults seguros, schema) — fechar antes de qualquer outra coisa.
- **Bloco 2**: gates que viram Sprint 9 (Polish + Launch) com retorno alto: drift detection, library helpers, matriz de versão, SBOM. Graduam LoopZero de "PaaS funcional" para "PaaS auditável e supply-chain-safe".
- **Bloco 3**: gates de manutenção contínua, cuja falha não bloqueia GA mas degradação acumulada dói pós-launch.
