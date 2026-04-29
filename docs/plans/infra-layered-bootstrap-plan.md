# Plan: Infrastructure Layered Bootstrap

> **Version 1.3** — Refatorar a infraestrutura de provisioning do LoopZero de um unico stack Pulumi (cloud infra + K8s workloads) para uma arquitetura de 3 camadas separadas: L1 Cloud Infra (Pulumi), L2 K8s Bootstrap (Helmfile), L3 Platform Config (Operators/GitOps). Isso elimina o problema de zombie state no destroy, resolve 35 dos 44 problemas documentados na retrospectiva Sprint 8, e habilita self-hosting confiavel para clientes. O resultado eh um `loopzero platform provision` que funciona end-to-end sem intervencao manual.
>
> **v1.1 changes:** Reconciliado com estado real do repositorio. T0.1 removida (ja commitada em e4a55c13). T0.3 corrigida (Loki/Promtail/Pulumi.dev.yaml ja commitados — restam apenas runtimeMaxNodes bump e CLAUDE.md). ADR D9 adicionada para cloudflared Deployment (CF Tunnel cria K8s resources dentro do L1). T1.1/T1.3 atualizadas para manter @pulumi/kubernetes ate cloudflared migrar. T1.4 nova para migracao do wiring object. T2.3 corrigida (admission-policies nao eh Helm chart). Gate intermediario L3-50 adicionado antes de L3-100. Helmfile adicionado aos prerequisites. CHANGELOG.md atualizado com 18 entries B1-B18 (CHG-2026-04-28-L3-STABILIZATION) que estavam ausentes.
>
> **v1.2 changes:** ADR D10 (loop back obrigatorio). Phase 8 nova com 3 tasks: T8.1 regression test suite (9 testes novos para B1-B18 sem protecao), T8.2 loop back checklist (44/44 problemas com protecao permanente documentada, checkpoint bloqueante por phase), T8.3 post-mortem loop (L3 coverage dos 44 problemas, test variants L3Churn e L3NoisyNeighbor). Coverage matrix expandida com colunas "Protecao Permanente" e "Recorre?". Global DoD atualizado: 44/44 protecao obrigatoria, zero "SIM" na matriz, loop back por phase.
>
> **v1.3 changes:** ADR D11 (um unico comando de instalacao). `loopzero platform provision` ELIMINADO — nao existe mais. `loopzero platform install` eh o UNICO comando, com `--provider` opcional para provisionar infra. D2 reescrito: Helmfile eh o UNICO mecanismo de chart install (prerequisites.ts deletado). D6 atualizado: profile `standalone` para self-hosting single-cluster. Phase 3 reescrita: de "criar provision" para "convergir install" — T3.1 refatora platformInstall para helmfile, T3.2 converge upgrade/uninstall. Zero duplicacao de versoes, ordering, ou secret generation entre self-hosting e LoopZero Cloud.

## Context

### O Que Existe Hoje

O Pulumi gerencia **~200 recursos num unico stack** (`loopzero-infra`):
- **15-17** recursos DigitalOcean (VPC, 4 DOKS, PG, Spaces, Registry)
- **6** recursos Cloudflare (Tunnel, Worker, KV, DNS)
- **26** Helm releases across 4 clusters
- **150+** objetos K8s (Secrets, ConfigMaps, RBAC, HPAs, IngressRoutes, NetworkPolicies)

Codigo em `infra/pulumi/src/` — 6795 linhas, 35+ arquivos TypeScript.

### O Que Esta Quebrado

A retrospectiva Sprint 8 (`docs/retrospectives/2026-04-28-sprint8-l3-session-deep-dive.md`) documentou:
- **25h de sessao**, 17h em sleep/espera
- **76 workarounds manuais** (`kubectl patch`, `doctl update`, `pulumi state delete`)
- **10 versoes de imagem** para debugar
- **0 apps respondendo HTTP 200** ao final

Analise sistematica dos runbooks identificou **44 problemas** em 10 categorias:

| Categoria | Qtd | Exemplo |
|-----------|-----|---------|
| DAY0_MISSING | 9 | ClusterImagePolicy nao criada automaticamente (B16) |
| ORDERING | 8 | ArgoCD SA sem RBAC cluster-scoped (B7) |
| CAPACITY | 7 | Loki saturado a 5000 streams (B5) |
| CONFIG_DRIFT | 5 | Node pool sizes nao persistidos em IaC |
| STATE_DRIFT | 4 | buildkit-registry-config Secret oscilando (B12) |
| TEARDOWN | 3 | CRDs com finalizers travam destroy 5+ min |
| IDEMPOTENCY | 3 | repo-server cache stale apos bootstrap (B17) |
| SECRET_MANAGEMENT | 2 | GITEA_TOKEN com trailing newline (B4) |
| SELF_HOSTING | 2 | adopt-or-create hardcoded para DO APIs |
| GOVERNANCE | 1 | Zero ADRs para T2/T3 changes |

### Root Cause

O problema **NAO** eh o Pulumi — Terraform/OpenTofu tem o mesmo bug de zombie state (pulumi-kubernetes Issue #2517 / terraform-provider-kubernetes equivalent). O problema eh **arquitetural**: um unico stack gerencia cloud infra E workloads K8s. Quando o cluster eh destruido, os 150+ recursos K8s viram fantasmas no state.

### Evidencia da Industria

Pesquisa em 10 plataformas/ferramentas (Rancher, Replicated, Kubefirst, Glasskube, Porter, Radius, Crossplane, CAPI, Talos, k0smotron, Helmfile) confirma: toda plataforma K8s madura separa infra de workloads. A combinacao Pulumi (L1) + Helmfile (L2) + ArgoCD (Day-2) eh o padrao convergente.

### O Que Ja Existe e Funciona

- **`loopzero platform install`** (`cli/src/commands/platform.ts`, 489 linhas) — installer self-hosting para single-cluster K8s via Helm. 10 steps, zero manual. Usa mesmo pinned HELM_VERSIONS.
- **`cli/src/installer/`** — modulos de validacao, prerequisites, bootstrap, components com types bem definidos (`PlatformConfig`, `HelmRelease`, `ValidationResult`).
- **22 Helm charts** em `infra/helm/` com values deterministic.
- **Provider factory pattern** em `infra/pulumi/src/contracts/` — abstractions para DO/AWS/CF plugaveis.
- **Adopt-or-create pattern** em `infra/pulumi/src/core/adopt-or-create.ts` — idempotencia para recursos pre-existentes.

## Objective

**Done** significa: `loopzero platform install -f values.yaml` produz uma plataforma 100% funcional sem nenhuma intervencao manual, tanto para LoopZero Cloud (com `--provider digitalocean`) quanto para self-hosting (sem `--provider`), validado com L3 100-tenant deploy no DigitalOcean.

Metas especificas:
1. **Um unico comando**: `loopzero platform install` eh o unico caminho de instalacao. Nao existe `loopzero platform provision` separado.
2. `pulumi destroy` completa em <5min sem state surgery (zero K8s resources no state)
3. `helmfile sync` eh o unico mecanismo de instalacao de charts (substitui `prerequisites.ts`)
4. 44/44 problemas com protecao permanente contra recorrencia (ADR D10)
5. Self-hosting identico a LoopZero Cloud: mesmo Helmfile, mesmas versoes, mesmos health checks

## ADRs

### D1 — Manter Pulumi para L1, nao migrar para Terraform/OpenTofu

**Decision:** Pulumi continua como ferramenta de IaC para recursos cloud (L1). Nao migrar para Terraform, OpenTofu, ou CAPI.

**Rationale:** O problema de zombie state eh identico em Terraform (K8s provider perde endpoint quando cluster eh destruido). Migrar reescreveria 6795 linhas TS→HCL sem ganho na confiabilidade. O time ja conhece Pulumi. O provider factory pattern (`contracts/`) permite trocar cloud providers sem trocar ferramenta.

**Consequences:** Pulumi permanece. A melhoria vem da separacao de camadas, nao da troca de ferramenta. K8s resources sao REMOVIDOS do Pulumi state.

### D2 — Helmfile como UNICO mecanismo de instalacao de charts

**Decision:** Helmfile eh a unica fonte de verdade para instalacao de Helm charts, tanto para LoopZero Cloud quanto para self-hosting. `cli/src/installer/prerequisites.ts` (com `VERSIONS`, `buildCorePrerequisites`, `buildAuthPrerequisites`) eh SUBSTITUIDO pelo Helmfile. ArgoCD App of Apps assume Day-2 (drift detection, upgrades).

**Rationale:** Ter dois mecanismos (Helm direto via prerequisites.ts + Helmfile) viola DRY na logica mais critica do sistema. Versoes em dois lugares, ordering duplicado, secret generation duplicado, bugs corrigidos num caminho nao chegam no outro. A convergencia elimina toda essa classe de problemas. O `loopzero platform install` existente (489 linhas) eh refatorado para chamar `helmfile sync` em vez de `installOrUpgrade()` por chart. ArgoCD assume Day-2 (drift, upgrades) apos o bootstrap via Helmfile.

**Consequences:** `prerequisites.ts` deletado. `VERSIONS` dict vive somente nos helmfiles. `loopzero platform install` chama helmfile internamente. Self-hosting e LoopZero Cloud usam o mesmo caminho. `helmfile` passa a ser prerequisito obrigatorio (adicionado ao `task doctor`).

### D3 — Preflight checks inspirados no Replicated Troubleshoot

**Decision:** Implementar preflight checks como Phase 0 obrigatoria antes de qualquer provisioning. Framework simples em TypeScript (nao adotar Replicated Troubleshoot diretamente).

**Rationale:** Replicated Troubleshoot eh o padrao da industria para preflight (70 Fortune 100), mas trazer a dependencia completa eh overkill para o caso do LoopZero. O pattern (check → pass/fail/warn → block on fail) eh o que importa. O `cli/src/installer/validator.ts` ja tem a estrutura basica — estender.

**Consequences:** Preflight detecta problemas antes de gastar 15-90 min em provisioning. Mesmos checks servem self-hosting e LoopZero Cloud.

### D4 — Health gates sincrono entre fases (nao async/eventual)

**Decision:** Cada fase do bootstrap bloqueia ate health checks passarem. Nao prosseguir se a fase anterior nao esta healthy.

**Rationale:** A maioria dos 8 problemas de ORDERING vem de componentes instalados antes de suas dependencias estarem ready. Helmfile `needs:` resolve ordering de instalacao, mas nao garante que o chart esta funcional. Health gates adicionam `kubectl wait` + HTTP probes entre fases.

**Consequences:** Bootstrap mais lento (espera por health) mas mais confiavel. Falhas detectadas cedo, nao 30 min depois quando outro componente falha misteriosamente.

### D5 — DNS exposure move para Helmfile (nao Pulumi)

**Decision:** DNS records (Phase 3 atual do Pulumi) movem para Helmfile como external-dns ou post-sync hook. Pulumi fica SOMENTE com cloud resources.

**Rationale:** DNS records dependem de Load Balancer IPs que so existem apos Traefik estar instalado (L2). Manter no Pulumi cria acoplamento cross-layer. external-dns eh o padrao K8s-native para DNS automation.

**Consequences:** Pulumi perde `expose.ts` e providers DNS. Helmfile gerencia DNS como release separada. Self-hosting: cliente configura DNS manualmente ou usa external-dns.

### D6 — Helmfile por cluster-role + profile `standalone` para self-hosting

**Decision:** Um helmfile por cluster-role: `mgmt.helmfile.yaml`, `build.helmfile.yaml`, `runtime.helmfile.yaml`. Para self-hosting single-cluster: `mgmt.helmfile.yaml` com environment `standalone` inclui TODOS os releases (mgmt + build + runtime) num unico cluster. `loopzero platform install` sem `--provider` usa o profile standalone automaticamente.

**Rationale:** Multi-cluster (LoopZero Cloud) e single-cluster (self-hosting) precisam dos mesmos charts, na mesma ordem, com as mesmas versoes. A diferenca eh ONDE rodam (N clusters vs 1). Helmfile environments resolvem isso com overlays — `standalone` coloca tudo no mesmo cluster, `dev`/`prod` distribui por cluster-role. Nenhum codigo diferente entre os dois caminhos.

**Consequences:** 3 helmfiles + 1 profile `standalone`. Self-hosting customer roda `loopzero platform install -f values.yaml` e internamente o CLI faz `helmfile -f mgmt.helmfile.yaml -e standalone sync`. LoopZero Cloud roda `loopzero platform install -f values.yaml --provider digitalocean` que faz Pulumi L1 + helmfile per-cluster. Mesmos charts, mesmas versoes, mesmo ordering.

### D7 — Secrets gerados pelo Helmfile via hooks (nao Pulumi RandomPassword)

**Decision:** Secrets (tokens, passwords, keypairs) sao gerados por Helmfile pre-install hooks usando `openssl rand` / `cosign generate-key-pair`. Nao mais `@pulumi/random.RandomPassword`.

**Rationale:** `RandomPassword` no Pulumi state eh perdido no destroy. Secrets devem ser gerados uma vez e persistidos em K8s Secrets (que morrem com o cluster — aceitavel para dev; prod usa ESO/Vault). Para prod, secrets vem de config externo.

**Consequences:** Dev: secrets auto-gerados, efemeros. Prod: secrets injetados via values overlay. Sem dependencia de Pulumi state para credenciais.

### D9 — Cloudflared Deployment migra do Pulumi para Helmfile (runtime)

**Decision:** O cloudflared Deployment + Secret (atualmente criados por `tunnel.ts` via `@pulumi/kubernetes`) migram para o `runtime.helmfile.yaml`. O Pulumi mantem SOMENTE o Tunnel resource no Cloudflare API (cloud infra legitimo de L1). O CF Worker + KV permanecem no Pulumi (sao recursos CF API, nao K8s).

**Rationale:** `tunnel.ts` cria 2 K8s resources (Secret + Deployment) no runtime cluster via `@pulumi/kubernetes`. Isso viola a premissa central do plano (L1 = zero K8s no state). Se o cluster runtime for destruido, esses 2 resources viram zombies — exatamente o problema que queremos eliminar. A solucao limpa eh: Pulumi cria o Tunnel no CF API (L1), Helmfile deploya o cloudflared pod no runtime (L2). O tunnel token eh passado como valor entre L1→L2 via `pulumi stack output`.

**Consequences:** `@pulumi/kubernetes` so pode ser removido do package.json APOS essa migracao. Ate la, T1.3 fica bloqueada. O Helmfile runtime ganha uma release extra (`cloudflared`). O tunnel token secret eh gerado pelo Pulumi (armazenado no state) e consumido pelo Helmfile via env var.

### D11 — Um unico comando de instalacao, nao dois

**Decision:** `loopzero platform install` eh o UNICO comando de instalacao. `loopzero platform provision` NAO EXISTE. A diferenca entre LoopZero Cloud e self-hosting eh resolvida por um flag `--provider` (opcional) que controla se a infra (clusters, DB, storage) eh provisionada automaticamente via Pulumi ou se o customer ja tem.

**Rationale:** Ter dois comandos (`install` vs `provision`) cria dois caminhos de codigo, dois conjuntos de bugs, dois pontos de manutencao. O produto eh um so — a instalacao deve ser uma so. "Provisionar infra" eh um step OPCIONAL antes de "instalar a plataforma", nao um comando diferente. O customer que traz seu proprio K8s nao precisa saber que o LoopZero Cloud usa Pulumi internamente — para ele, `loopzero platform install` eh tudo.

**Consequences:** `loopzero platform install -f values.yaml` (self-hosting, K8s existente). `loopzero platform install -f values.yaml --provider digitalocean` (LoopZero Cloud, provisiona DOKS+PG+Spaces primeiro). Internamente, `--provider` aciona Pulumi L1 antes do Helmfile L2. Sem `--provider`, assume que o cluster ja existe e vai direto pro Helmfile. `cli/src/installer/prerequisites.ts` eh deletado. `cli/src/commands/platform.ts:platformInstall()` eh a unica orquestracao (com step L1 condicional via `--provider`). _[iter 18 audit fix: implementação real ficou em `commands/platform.ts`, não em um arquivo `provision.ts` separado como originalmente planejado]_

### D10 — Loop back obrigatorio: cada problema requer protecao permanente contra recorrencia

**Decision:** Todo problema dos 44 catalogados DEVE ter uma das tres protecoes permanentes: (a) teste automatizado que falha se o bug voltar, (b) config declarativa que impede a condicao (Helmfile `needs:`, values pinned), ou (c) preflight/health check que detecta em runtime. Problemas sem nenhuma protecao NAO podem ser marcados como resolvidos.

**Rationale:** A sessao Sprint 8 mostrou que fixes pontuais sem verificacao de recorrencia sao ineficazes — 76 workarounds manuais, muitos resolvendo o mesmo problema pela 3a vez. O pattern "fix and forget" eh a root cause da fragilidade. Cada fase do plano DEVE terminar com um loop back: revisitar a lista completa dos 44 problemas e verificar se algum pode reaparecer com as mudancas feitas. O custo de parar 30 min para revisar eh infinitamente menor que o custo de outra sessao de 25h.

**Consequences:** Cada phase ganha um checkpoint de regressao no DoD. Phase 8 (nova) eh dedicada a escrever regression tests para todos os problemas que ainda nao tem protecao automatizada. O Global DoD ganha o criterio "44/44 problemas com protecao documentada".

### D8 — Nao adotar CAPI/CAPDO, Crossplane, Timoni, cdk8s, Dagger, Flux

**Decision:** Nenhuma dessas ferramentas sera adotada nesta iteracao.

**Rationale:**
- **CAPDO**: Nao suporta DOKS managed (provisiona Droplets raw). Ultimo release abril 2024 (2 anos stale). Perderiamos control plane gerenciado.
- **Crossplane**: Chicken-and-egg (precisa K8s para provisionar K8s). Ja rejeitado em MEET-054.
- **Timoni**: Alpha (`v1alpha1`), ecossistema minusculo vs Helm.
- **cdk8s**: Gera YAML, nao gerencia lifecycle. Camada errada.
- **Dagger**: Pipeline runner, nao substitui Argo Workflows (complementar — avaliar Sprint 9+).
- **Flux**: Substituir ArgoCD por Flux eh migracao grande sem ganho proporcional no bootstrap.

**Consequences:** Stack permanece: Pulumi + Helm + ArgoCD + Argo Workflows. Helmfile eh a unica adicao.

## Dependency Graph

```
Phase 0 (Reconciliar) ──▶ Phase 1 (Pulumi L1-only) ──▶ Phase 3 (Convergir CLI install)
   T0.1 admission-policies        T1.1 extract Phase 2        T3.1 refatorar platformInstall
   T0.2 runtimeMaxNodes+CLAUDE    T1.2 clean state            T3.2 convergir upgrade/uninstall
   T0.3 helmfile prereqs           T1.3 remove @pulumi/k8s    │
                                   T1.4 wiring contract        ▼
                                     │                   Phase 4 (Health Gates)
                                     ▼                         │
                            Phase 2 (Helmfile L2) ─────────────┘
                              T2.1 mgmt helmfile
                              T2.2 build helmfile
                              T2.3 runtime helmfile (+cloudflared +admission-policies)
                              T2.4 wiring scripts

Phase 5 (Preflight) ◀── pode iniciar em paralelo com Phase 1-2 ──▶ Phase 4
                                                                      │
                                                                      ▼
                                                              Phase 6 (ArgoCD App of Apps)
                                                                      │
                                                                      ▼
                                                              Phase 7 (Validacao DO)
                                                                Gate 1: L3-50
                                                                Gate 2: L3-100
                                                                      │
                                                                      ▼
                                                              Phase 8 (Regression + Loop Back)
                                                                44/44 protecao permanente
```

- **Phases 0 → 1 → 2:** Sequenciais (1 depende de 0, 2 depende de 1)
- **T1.3** bloqueada por T1.1 (CF Tunnel K8s resources removidos primeiro)
- **T2.3** depende de T0.1 (admission-policies Helm chart criado)
- **Phase 3 (Convergir CLI):** Depende de Phase 1 + 2 (helmfiles e pulumi-runner prontos)
- **Phase 5 (Preflight):** Pode iniciar em paralelo com Phase 1-2
- **Phase 4 (Health Gates):** Depende de Phase 2 + 3
- **Phase 6 (App of Apps):** Depende de Phase 2 + 4
- **Phase 7 (Validacao):** Depende de TODAS as fases anteriores. Gate 1 (L3-50) antes de Gate 2 (L3-100)
- **Phase 8 (Regression):** Depende de Phase 7. Checkpoint final antes de Done

---

## Phase 0: Reconciliar Estado e Pendencias

**Objective:** Verificar o que ja foi commitado, persistir as mudancas restantes, e atualizar documentacao. B12-B15 ja commitados em `e4a55c13`. Loki/Promtail/buildMaxNodes ja commitados. Restam: runtimeMaxNodes bump, ClusterImagePolicy no IaC, CLAUDE.md fix, e adicionar Helmfile aos prerequisites.

### T0.1 — Criar ClusterImagePolicy como Helm chart (B16)

#### Objective
Converter `infra/helm/admission-policies/` de raw manifests para Helm chart instalavel, e incluir a ClusterImagePolicy `loopzero-images-warn` que falta.

#### Evidence
- B16: `policy-controller deny-by-default blocks all tenant Deployments` (runbook L3 stabilization)
- `infra/helm/admission-policies/` contem SOMENTE `manifests/` com 6 VAP YAML files + alerts — NAO eh um Helm chart (falta `Chart.yaml`, `templates/`, `values.yaml`)
- `runtime-policy-controller.ts` cria ClusterImagePolicy via Pulumi K8s resource — precisa migrar para Helm chart para funcionar no Helmfile

#### Files to edit
```
infra/helm/admission-policies/Chart.yaml — NEW: Helm chart metadata
infra/helm/admission-policies/values.yaml — NEW: configurable registries, mode (warn/enforce)
infra/helm/admission-policies/templates/ — NEW: move manifests/ content to templates/
infra/helm/admission-policies/templates/cluster-image-policy.yaml — NEW: ClusterImagePolicy loopzero-images-warn
```

#### Deep file dependency analysis
- `manifests/*.yaml`: 6 existing VAP files + alerts. Move para `templates/` com minimal templating (namespace, registry refs from values).
- `Chart.yaml`: Define chart identity. Sera referenciado por `runtime.helmfile.yaml`.
- `values.yaml`: Configura registries cobertas (`registry.digitalocean.com/loopzero-system/**`, `harbor.{{ .Values.domain }}/**`), mode (`warn`/`enforce`).
- `cluster-image-policy.yaml`: Template para ClusterImagePolicy com `mode: warn` + `static.action: pass`.

#### Deep Dives
- A ClusterImagePolicy CRD vem do policy-controller Helm chart (instalado separadamente)
- Sem a ClusterImagePolicy, o webhook `policy.sigstore.dev` rejeita TODOS os pods com "no matching policies"
- O mode `warn` + `static.action: pass` eh temporary fallback ate cosign keyless verification (Sprint 9)
- Os 6 VAPs existentes (disallow-latest-tag, require-resource-limits, etc.) sao ValidatingAdmissionPolicies K8s nativas, nao CRDs do policy-controller

#### Tasks
1. Criar `Chart.yaml` com `name: admission-policies`, `version: 0.1.0`
2. Criar `values.yaml` com registries e mode configuravel
3. Mover `manifests/*.yaml` para `templates/`, adicionando `{{ .Values }}` refs onde necessario
4. Criar `templates/cluster-image-policy.yaml` com ClusterImagePolicy
5. `helm lint infra/helm/admission-policies/`
6. Commit

#### TDD
```
RED:     helm_lint — `helm lint infra/helm/admission-policies/` deve passar
RED:     template_renders — `helm template admission-policies infra/helm/admission-policies/` deve incluir ClusterImagePolicy
GREEN:   Criar Chart.yaml, values.yaml, mover manifests para templates
REFACTOR: None expected
VERIFY:  helm lint infra/helm/admission-policies/ && helm template admission-policies infra/helm/admission-policies/ | grep -c ClusterImagePolicy
```

#### Acceptance Criteria
- [ ] `infra/helm/admission-policies/` eh um Helm chart valido (Chart.yaml + templates/)
- [ ] `helm template` renderiza ClusterImagePolicy com mode warn
- [ ] 6 VAPs existentes preservados como templates
- [ ] `helm lint` passa

#### DoD
- [ ] Chart criado e lintado
- [ ] ClusterImagePolicy incluida
- [ ] Commit referenciando B16

---

### T0.2 — Atualizar runtimeMaxNodes e CLAUDE.md

#### Objective
Persistir o runtime node pool sizing para L3 e corrigir status incorreto do Sprint 6 security posture no CLAUDE.md.

#### Evidence
- `Pulumi.dev.yaml` tem `runtimeMaxNodes: "3"` — insuficiente para L3 (50 vCPU, precisa 8+ nodes)
- `buildMaxNodes: "5"` ja esta correto (commitado)
- Loki `max_global_streams_per_user: 100000` ja commitado
- Promtail drop labels ja commitado
- CLAUDE.md: claims "Audit mode active" mas policy-controller estava em deny-default (B16)

#### Files to edit
```
infra/pulumi/Pulumi.dev.yaml — runtimeMaxNodes: "3" → "8" (L3 burst capacity)
CLAUDE.md — corrigir status Sprint 6 security posture (deny-default, nao Audit mode)
```

#### Deep file dependency analysis
- `Pulumi.dev.yaml`: Lido por `config/stack.ts:loadStackConfig()`. `runtimeMaxNodes` afeta autoscaler config do DOKS runtime cluster.
- `CLAUDE.md`: Lido por todos os agentes. Status incorreto causa decisoes erradas sobre security posture.

#### Deep Dives
- `runtimeMaxNodes: "8"` com autoscale min=2, max=8 para suportar L3 burst
- CLAUDE.md Sprint 6 status: mudar de "Audit mode active" para "policy-controller installed; warn-mode ClusterImagePolicy required (B16); Deny-mode flip pending Sprint 9 cosign keyless"

#### Tasks
1. Editar `Pulumi.dev.yaml`: `runtimeMaxNodes: "8"`
2. Editar `CLAUDE.md`: corrigir Sprint 6 security posture
3. Commit

#### TDD
```
RED:     N/A (config changes)
GREEN:   N/A
REFACTOR: N/A
VERIFY:  python3 -c "import yaml; yaml.safe_load(open('infra/pulumi/Pulumi.dev.yaml'))"
```

#### Acceptance Criteria
- [ ] `runtimeMaxNodes` = "8" no Pulumi.dev.yaml
- [ ] CLAUDE.md Sprint 6 status corrigido
- [ ] YAML valido

#### DoD
- [ ] Commit criado
- [ ] YAML valido

---

### T0.3 — Adicionar Helmfile aos prerequisites do projeto

#### Objective
Documentar Helmfile como dependencia obrigatoria e adicionar check no `task doctor`.

#### Evidence
- Helmfile (ADR D2) eh o orquestrador L2 — deve estar documentado como prerequisite
- `CLAUDE.md` Prerequisites section nao lista Helmfile
- `task doctor` nao verifica Helmfile

#### Files to edit
```
CLAUDE.md — adicionar helmfile na secao Prerequisites
Taskfile.yml — adicionar check no target doctor
```

#### Deep file dependency analysis
- `CLAUDE.md`: Lido por devs e agentes. Prerequisites section define o que precisa estar instalado.
- `Taskfile.yml`: Define `task doctor` que verifica ambiente. Adicionar `helmfile version` check.

#### Tasks
1. Editar `CLAUDE.md` Prerequisites: adicionar `Helmfile v1.0+`
2. Editar `Taskfile.yml` target `doctor`: adicionar `helmfile version` check
3. Commit

#### TDD
```
RED:     task doctor deve reportar helmfile status
GREEN:   Adicionar check
REFACTOR: None
VERIFY:  task doctor
```

#### Acceptance Criteria
- [ ] `helmfile` listado nos Prerequisites do CLAUDE.md
- [ ] `task doctor` verifica helmfile
- [ ] Commit criado

#### DoD
- [ ] Documentacao atualizada
- [ ] Doctor check funcional

---

## Phase 1: Refatorar Pulumi para L1-Only

**Objective:** Remover a vasta maioria dos recursos K8s do Pulumi state. Pulumi passa a gerenciar cloud resources (VPC, DOKS, PG, Spaces, Registry) + CF API resources (Tunnel config, Worker, KV). Os 2 K8s resources do CF Tunnel (cloudflared Secret + Deployment em `tunnel.ts`) migram para o Helmfile runtime (ADR D9). Resultado: `pulumi destroy` completa em <5 min sem state surgery.

### T1.1 — Extrair Phase 2 do index.ts

#### Objective
Separar o orchestrator Pulumi para que Phase 2 (bootstrap) e Phase 3 (expose) nao sejam mais executados pelo `pulumi up`.

#### Evidence
- `index.ts` linhas 98-170: `bootstrapCluster()` e `exposePlatform()` sao chamados diretamente no stack
- Esses 150+ K8s resources no state causam zombie state no destroy (3 dos 4 problemas STATE_DRIFT)

#### Files to edit
```
infra/pulumi/src/index.ts — remover chamadas a bootstrapCluster() e exposePlatform()
infra/pulumi/src/index.ts — exportar APENAS outputs de infra (kubeconfigs, endpoints, connection strings)
```

#### Deep file dependency analysis
- `index.ts`: Orchestrador principal. Chama `createInfra()` → `bootstrapCluster()` → `exposePlatform()`. Removendo bootstrap e expose, fica: `createInfra()` → exports.
- **Downstream**: CLI `loopzero platform provision` consumira outputs via `pulumi stack output --json`. Helmfile consumira via environment variables.

#### Deep Dives
- **Outputs preservados**: `networkId`, `mgmtClusterName`, `mgmtKubeconfig`, `buildKubeconfig`, `runtimeKubeconfig`, `runtime2Kubeconfig`, `dbHost`, `dbUri`, `dbPassword`, `bucketName`, `bucketEndpoint`, `cdnEndpoint`, `registryEndpoint`, `cfTunnelId`, `cfKvNamespaceId`, `cfTunnelToken` (NOVO — necessario para Helmfile cloudflared)
- **Outputs removidos**: Todos os `*Ready` booleans (pertenciam ao bootstrap), `ingressEndpoint` (pertence ao Helmfile), wiring object (migra para ConfigMap no Helmfile — ver T1.4)
- **CF Tunnel (ADR D9)**: `tunnel.ts` hoje cria Tunnel CF resource + 2 K8s resources (Secret + Deployment via `@pulumi/kubernetes`). Nesta task, remover os 2 K8s resources de `tunnel.ts` e exportar `cfTunnelToken` como output. O cloudflared Deployment migra para `runtime.helmfile.yaml` (T2.3). O Tunnel config no CF API permanece no Pulumi.
- **CF Worker + KV**: Permanecem no Pulumi — sao recursos CF API, nao K8s.
- **Invariante pos-migracao**: `pulumi up` + `pulumi destroy` NAO toca em nenhum recurso K8s

#### Tasks
1. Criar branch `feat/infra-layered-bootstrap`
2. Copiar `index.ts` atual para `index.ts.bak` (referencia)
3. Remover import e chamada de `bootstrapCluster`
4. Remover import e chamada de `exposePlatform`
5. Refatorar `tunnel.ts`: remover `k8s.core.v1.Secret` e `k8s.apps.v1.Deployment` (cloudflared). Manter `cloudflare.Tunnel` + `cloudflare.TunnelConfig`. Exportar `tunnelToken` como output secreto.
6. Remover `k8s.Provider("cf-runtime-k8s", ...)` de `index.ts` (nao mais necessario)
7. Atualizar exports para SOMENTE outputs L1 + tunnel token
8. Testar: `cd infra/pulumi && npx pulumi preview --stack dev`

#### TDD
```
RED:     pulumi_preview_no_k8s_resources — preview nao deve conter nenhum resource kubernetes:*
RED:     tunnel_token_exported — `pulumi stack output cfTunnelToken` retorna valor secreto
GREEN:   Remover Phase 2 + 3 + K8s resources do tunnel.ts
REFACTOR: Limpar imports nao utilizados
VERIFY:  npx pulumi preview --stack dev 2>&1 | grep -c 'kubernetes:' (expected: 0)
```

#### Acceptance Criteria
- [ ] `pulumi preview` mostra ZERO recursos `kubernetes:*`
- [ ] `pulumi preview` mostra ZERO recursos `helm:*`
- [ ] `cfTunnelToken` exportado como output secreto
- [ ] Outputs incluem kubeconfigs, DB connection, storage endpoints
- [ ] CF Tunnel config no CF API preservado
- [ ] CF Worker + KV preservados

#### DoD
- [ ] Phase 2 + 3 + tunnel K8s resources removidos
- [ ] Preview limpo (somente DO + CF API resources)
- [ ] Tunnel token exportado para consumo pelo Helmfile

---

### T1.2 — Limpar State Existente

#### Objective
Remover recursos K8s orphans do Pulumi state atual (dev stack) para que o state reflita SOMENTE L1.

#### Evidence
- State atual tem ~200 resources. Apos T1.1, code gerencia ~23. State precisa ser limpo para nao ter fantasmas.
- TEARDOWN problemas (3) sao causados por K8s resources no state apos cluster destruido

#### Files to edit
```
(nenhum arquivo — operacao no Pulumi state)
```

#### Deep file dependency analysis
- N/A — operacao administrativa no state backend (S3 `loopzero-dev-pulumi-state`)

#### Deep Dives
- **Procedimento**: `pulumi stack export > state-backup.json`, filtrar resources com type `kubernetes:*` e `helm:*`, re-importar state limpo, ou usar `pulumi state delete` iterativo
- **Risco**: Se feito incorretamente, perde tracking de DO resources. Backup OBRIGATORIO.
- **Alternativa**: Se o state estiver muito poluido, criar novo stack `dev-v2` do zero com `pulumi import` dos DO resources existentes

#### Tasks
1. Exportar state: `pulumi stack export --stack dev > state-backup-$(date +%Y%m%d).json`
2. Contar resources K8s: `jq '.deployment.resources[] | select(.type | startswith("kubernetes:")) | .type' state-backup.json | wc -l`
3. Se <50: usar `pulumi state delete` iterativo
4. Se >50: criar script `scripts/clean-pulumi-state.sh` que filtra K8s resources
5. Validar: `pulumi preview` mostra zero pending deletes de K8s resources

#### TDD
```
RED:     pulumi preview mostra pending deletes de recursos K8s
GREEN:   State limpo — preview mostra somente DO/CF resources
REFACTOR: Remover state-backup temporario apos validacao
VERIFY:  npx pulumi preview --stack dev 2>&1 | grep -c 'delete' (expected: 0 para k8s)
```

#### Acceptance Criteria
- [ ] State backup criado antes da limpeza
- [ ] Zero recursos `kubernetes:*` ou `helm:*` no state
- [ ] `pulumi preview` limpo (nenhum delete pendente)
- [ ] Recursos DO existentes preservados no state

#### DoD
- [ ] State limpo
- [ ] Preview confirma somente L1 resources

---

### T1.3 — Remover @pulumi/kubernetes do package.json

#### Objective
Remover a dependencia `@pulumi/kubernetes` apos confirmacao de que ZERO codigo L1 a utiliza.

#### Evidence
- `package.json` lista `"@pulumi/kubernetes": "4.28.0"` como dependencia
- Apos T1.1 (que remove os K8s resources do tunnel.ts), nenhum codigo L1 deve usar `@pulumi/kubernetes`
- **BLOQUEADA por T1.1**: tunnel.ts tinha 4 referencias a `@pulumi/kubernetes`. Essas sao removidas em T1.1. Esta task so pode executar APOS T1.1.

#### Files to edit
```
infra/pulumi/package.json — remover @pulumi/kubernetes
infra/pulumi/src/components/bootstrap-shared.ts — remover funcoes que usam K8s provider (migram para Helmfile)
```

#### Deep file dependency analysis
- `package.json`: Remover K8s provider impede uso acidental. Tambem permite remover ~50MB de node_modules.
- `bootstrap-shared.ts`: Apos T1.1, funcoes como `createPlatformNamespace`, `installCertManager`, `installTraefik` nao sao mais chamadas pelo Pulumi — migram conceitualmente para Helmfile. Manter apenas `HELM_VERSIONS` e `parseKubeconfig` (pure functions, sem provider).

#### Deep Dives
- **Verificacao pre-remocao**: `grep -r "@pulumi/kubernetes\|from.*pulumi.*kubernetes" infra/pulumi/src/` deve retornar ZERO hits apos T1.1
- **Shared code**: `HELM_VERSIONS` duplicado no Helmfile config (YAML). `parseKubeconfig` copiado para CLI se necessario.
- **Risk**: Se alguma funcao L1 esquecida usar K8s, `npm install` quebra. Deteccao imediata — bom.

#### Tasks
1. Confirmar que T1.1 removeu todos os usos de `@pulumi/kubernetes`
2. `grep -r "pulumi/kubernetes" infra/pulumi/src/` — deve retornar 0
3. Remover de `package.json` e rodar `npm install`
4. Limpar `bootstrap-shared.ts` — manter apenas pure functions
5. `npx pulumi preview --stack dev` — deve funcionar

#### TDD
```
RED:     grep retorna 0 hits para @pulumi/kubernetes
GREEN:   Remover dependencia + limpar imports
REFACTOR: Limpar bootstrap-shared.ts
VERIFY:  cd infra/pulumi && npm install && npx pulumi preview --stack dev
```

#### Acceptance Criteria
- [ ] Zero referencias a `@pulumi/kubernetes` no codigo L1
- [ ] `@pulumi/kubernetes` removido do package.json
- [ ] `npm install` sem erros
- [ ] `pulumi preview` funciona

#### DoD
- [ ] Dependencia removida
- [ ] Build limpo
- [ ] Preview funciona

---

### T1.4 — Migrar wiring object para contrato L1→L2

#### Objective
Definir como os outputs do Pulumi L1 (kubeconfigs, DB connection, endpoints) sao passados para o Helmfile L2 e para os componentes da plataforma (loopzero-api, operators).

#### Evidence
- `index.ts:206-243` exporta um `wiring` object consumido por loopzero-api para saber endpoints internos (Gitea URL, DB host, registry, storage, cluster endpoints)
- Apos a separacao L1/L2, os outputs de bootstrap (e.g., `bootstrap.mgmt.ingressEndpoint`) nao existem mais no Pulumi
- Sem essa migracao, loopzero-api perde visibilidade sobre endpoints — deploy falha

#### Files to edit
```
infra/helmfile/scripts/create-wiring-configmap.sh — NEW: gera ConfigMap com topologia da plataforma
infra/helmfile/mgmt.helmfile.yaml — hook pos-sync que cria ConfigMap
cli/src/commands/platform.ts — integrar: pulumiOutput() → env vars → helmfile → wiring ConfigMap (implementação ficou em platform.ts, não provision.ts separado — iter 18 audit correction)
```

#### Deep file dependency analysis
- `wiring` object atual (`index.ts:206-243`): Contem URLs internas (gitea.svc, prometheus.svc), endpoints de infra (DB host/port, registry, storage), e endpoints de bootstrap (ingress IPs).
- **URLs internas** (gitea, prometheus, grafana): Sao estáticas — definidas pela convencao de nomes K8s (e.g., `http://gitea-http.loopzero-system.svc.cluster.local:3000`). Podem ser hardcoded no Helm values do loopzero-api.
- **Endpoints de infra** (DB, registry, storage): Vem do `pulumi stack output`. Passados como env vars para o Helmfile que injeta nos Helm values.
- **Endpoints de bootstrap** (ingress IPs): So conhecidos APOS o Helmfile instalar Traefik. Resolvidos via `kubectl get svc` no wiring script.

#### Deep Dives

**Contrato L1 → L2:**
```bash
# Outputs do Pulumi consumidos pelo Helmfile
export LOOPZERO_DB_HOST=$(pulumi stack output dbHost --stack dev)
export LOOPZERO_DB_URI=$(pulumi stack output dbUri --stack dev --show-secrets)
export LOOPZERO_REGISTRY_ENDPOINT=$(pulumi stack output registryEndpoint --stack dev)
export LOOPZERO_BUCKET_NAME=$(pulumi stack output bucketName --stack dev)
export LOOPZERO_BUCKET_ENDPOINT=$(pulumi stack output bucketEndpoint --stack dev)
export LOOPZERO_CF_KV_NAMESPACE=$(pulumi stack output cfKvNamespaceId --stack dev)
export LOOPZERO_CF_TUNNEL_TOKEN=$(pulumi stack output cfTunnelToken --stack dev --show-secrets)
```

**Wiring ConfigMap** (criado pos-bootstrap):
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: loopzero-platform-topology
  namespace: loopzero-system
data:
  GITEA_INTERNAL_URL: "http://gitea-http.loopzero-system.svc.cluster.local:3000"
  GRAFANA_INTERNAL_URL: "http://grafana.loopzero-system.svc.cluster.local:3000"
  PROMETHEUS_INTERNAL_URL: "http://prometheus-server.loopzero-system.svc.cluster.local:80"
  DB_HOST: "{{ from env }}"
  REGISTRY_ENDPOINT: "{{ from env }}"
  STORAGE_BUCKET: "{{ from env }}"
  STORAGE_ENDPOINT: "{{ from env }}"
  MGMT_INGRESS_IP: "{{ from kubectl get svc }}"
  RUNTIME_INGRESS_IP: "{{ from kubectl get svc }}"
```

**loopzero-api consome via envFrom**: O Deployment do loopzero-api referencia este ConfigMap.

#### Tasks
1. Documentar contrato L1→L2 (quais outputs, quais env vars)
2. Criar `create-wiring-configmap.sh` que gera ConfigMap a partir de env vars + kubectl lookups
3. Integrar como hook pos-sync no mgmt helmfile (apos Traefik estar ready)
4. Atualizar loopzero-api Helm values para consumir ConfigMap via envFrom
5. Testar: ConfigMap criado com valores corretos

#### TDD
```
RED:     test_wiring_configmap_has_db_host — ConfigMap contem DB_HOST nao vazio
RED:     test_wiring_configmap_has_ingress — ConfigMap contem MGMT_INGRESS_IP nao vazio
GREEN:   Implementar script e hook
REFACTOR: None expected
VERIFY:  kubectl get cm loopzero-platform-topology -n loopzero-system -o yaml
```

#### Acceptance Criteria
- [ ] Contrato L1→L2 documentado (outputs, env vars, ConfigMap)
- [ ] ConfigMap criado automaticamente pos-bootstrap
- [ ] loopzero-api consome ConfigMap (envFrom)
- [ ] Zero outputs de bootstrap no Pulumi state
- [ ] Wiring ConfigMap inclui todos os campos do antigo wiring object

#### DoD
- [ ] Script e hook funcionais
- [ ] loopzero-api recebe todas as env vars necessarias
- [ ] Teste confirma ConfigMap correto

---

## Phase 2: Criar Helmfile Layer (L2)

**Objective:** Criar 3 helmfiles (mgmt, build, runtime) que instalam os 26 Helm releases com ordering correto via `needs:`, substituindo todo o Phase 2 do Pulumi.

### T2.1 — Criar mgmt.helmfile.yaml

#### Objective
Definir os 15+ releases do management cluster com dependencias explicitas via `needs:`.

#### Evidence
- `mgmt-bootstrap.ts` (216 linhas) instala 35 componentes no mgmt cluster
- Ordering atual eh implicito via `dependsOn` do Pulumi — precisa ser explicito no Helmfile
- 8 dos 44 problemas (ORDERING) sao causados por falta de ordering correto

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml — NEW: helmfile para management cluster
infra/helmfile/environments/dev.yaml — NEW: values overlay para dev
infra/helmfile/environments/prod.yaml — NEW: values overlay para prod
```

#### Deep file dependency analysis
- `mgmt.helmfile.yaml`: Novo arquivo. Consome values de `infra/helm/*/values.yaml`. Referenceia charts dos repos ja definidos em `cli/src/installer/prerequisites.ts:REPOS`.
- `environments/dev.yaml`: Overrides para dev (replicas menores, self-signed TLS, passwords default).
- `environments/prod.yaml`: Overrides para prod (HA replicas, ACME TLS, secrets de config externo).

#### Deep Dives

**Release ordering (sync waves):**

| Wave | Release | needs: | Rationale |
|------|---------|--------|-----------|
| -3 | cert-manager | (none) | CRDs necessarios antes de qualquer TLS |
| -3 | metrics-server | (none) | HPA precisa de metrics |
| -2 | traefik | cert-manager | Precisa de ClusterIssuer para TLS |
| -2 | nfs-server-provisioner | (none) | Gitea precisa de RWX PVC |
| -1 | gitea-redis | (none) | Gitea session/cache |
| -1 | gitea | nfs-server-provisioner, gitea-redis | Precisa de RWX storage e Redis |
| 0 | argocd | cert-manager | GitOps engine |
| 0 | minio | (none) | Artifact storage |
| 0 | prometheus | (none) | Metrics |
| 0 | grafana | prometheus | Datasource precisa existir |
| 1 | ory-kratos | (database ready) | Identity |
| 1 | ory-hydra | ory-kratos | OAuth2 depende de identity |
| 1 | tempo | (none) | Tracing |
| 1 | otel-collector | tempo | Push endpoint |
| 1 | blackbox-exporter | (none) | Probing |
| 2 | harbor | cert-manager, traefik, minio | Registry com TLS + S3 backend |
| 2 | loopzero-api | argocd, gitea, minio, ory-kratos | Core API |
| 2 | loopzero-controller | loopzero-api | Depende do API |
| 2 | tenant-controller | loopzero-api | Depende do API |
| 2 | loopzero-cloud | ory-hydra, loopzero-api | Dashboard |
| 99 | smoke-test | ALL | Validacao final |

**Secrets como Helmfile hooks:**
```yaml
hooks:
  - events: ["presync"]
    command: "scripts/generate-secrets.sh"
    args: ["{{`{{ .Environment.Name }}`}}", "mgmt"]
```

O script `generate-secrets.sh` cria Secrets K8s necessarios (DB URL, Gitea token, MinIO creds) a partir de environment variables ou `pulumi stack output`.

#### Tasks
1. Criar `infra/helmfile/mgmt.helmfile.yaml` com todos os 15+ releases
2. Definir `needs:` para cada release baseado na tabela de ordering
3. Criar `infra/helmfile/environments/dev.yaml` com overrides dev
4. Criar `infra/helmfile/environments/prod.yaml` com overrides prod
5. Criar `infra/helmfile/scripts/generate-secrets.sh` para secrets pre-install
6. Testar: `helmfile -f infra/helmfile/mgmt.helmfile.yaml -e dev diff` (dry-run)

#### TDD
```
RED:     helmfile_lint — `helmfile lint` deve passar sem erros
RED:     helmfile_diff_no_errors — `helmfile diff` contra cluster vazio nao deve ter erros de template
GREEN:   Criar helmfile com todos os releases e ordering
REFACTOR: Extrair values comuns para `_defaults.yaml`
VERIFY:  helmfile -f infra/helmfile/mgmt.helmfile.yaml lint
```

#### Acceptance Criteria
- [ ] Todos os 15+ Helm releases do mgmt cluster declarados
- [ ] `needs:` define ordering explicito (zero dependencias implicitas)
- [ ] Environment overlays para dev e prod
- [ ] `helmfile lint` passa
- [ ] Versoes pinned identicas a `bootstrap-shared.ts:HELM_VERSIONS`

#### DoD
- [ ] Helmfile criado e lintado
- [ ] Dry-run confirma ordering correto
- [ ] Secrets hook funcional

---

### T2.2 — Criar build.helmfile.yaml

#### Objective
Definir os 3 releases do build cluster com secrets e local-path-provisioner.

#### Evidence
- `build-argo.ts` + `build-loki.ts` + `build-secrets.ts` (total ~400 linhas)
- Build cluster tem peculiaridades: local-path-provisioner (Rancher), cosign keys, NodePort Loki

#### Files to edit
```
infra/helmfile/build.helmfile.yaml — NEW: helmfile para build cluster
infra/helmfile/scripts/generate-build-secrets.sh — NEW: cosign keypair, registry auth, S3 creds
```

#### Deep file dependency analysis
- `build.helmfile.yaml`: 3 releases (argo-workflows, loki, promtail) + local-path-provisioner como manifest.
- `generate-build-secrets.sh`: Gera `cosign-signing-key` Secret (nao mais placeholder!), `buildkit-registry-config`, `minio-credentials`.

#### Deep Dives

**Release ordering:**

| Wave | Release | needs: |
|------|---------|--------|
| -2 | local-path-provisioner | (none) — applied as manifest, not Helm |
| -1 | namespace + RBAC | (none) |
| 0 | argo-workflows | namespace |
| 0 | loki | namespace |
| 0 | promtail | loki |

**Cosign key generation** (resolve problema B1 — placeholder):
```bash
# generate-build-secrets.sh
cosign generate-key-pair --output-key-prefix /tmp/cosign
kubectl create secret generic cosign-signing-key -n argo \
  --from-file=cosign.key=/tmp/cosign.key \
  --from-file=cosign.pub=/tmp/cosign.pub \
  --dry-run=client -o yaml | kubectl apply -f -
```

**Local-path-provisioner** (resolve B2 — DO API rate limit):
- Instalado como raw manifest (nao Helm chart): `kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.31/deploy/local-path-storage.yaml`
- StorageClass `local-path` com `volumeBindingMode: WaitForFirstConsumer`

#### Tasks
1. Criar `infra/helmfile/build.helmfile.yaml`
2. Criar `infra/helmfile/scripts/generate-build-secrets.sh`
3. Incluir local-path-provisioner como `kubectl apply` no hook presync
4. Testar: `helmfile -f infra/helmfile/build.helmfile.yaml lint`

#### TDD
```
RED:     helmfile_lint_build — lint passa
RED:     cosign_key_generation — script gera keypair valido (nao placeholder)
GREEN:   Criar helmfile + scripts
REFACTOR: None expected
VERIFY:  helmfile -f infra/helmfile/build.helmfile.yaml lint
```

#### Acceptance Criteria
- [ ] Argo Workflows, Loki, Promtail declarados
- [ ] Cosign keypair gerado automaticamente (nao placeholder)
- [ ] Local-path-provisioner instalado via hook
- [ ] `helmfile lint` passa

#### DoD
- [ ] Helmfile criado e lintado
- [ ] Secret generation script funcional

---

### T2.3 — Criar runtime.helmfile.yaml

#### Objective
Definir os 8 releases do runtime cluster, reutilizavel para multiplas cells (runtime, runtime-2, ...).

#### Evidence
- `runtime-bootstrap.ts` (246 linhas) instala 8 charts + RBAC + ESO + policy-controller
- Mesmo helmfile sera usado para runtime e runtime-2 (multi-cell via kubeconfig override)
- Resolve B16: ClusterImagePolicy incluida como release

#### Files to edit
```
infra/helmfile/runtime.helmfile.yaml — NEW: helmfile para runtime cluster(s)
```

#### Deep file dependency analysis
- `runtime.helmfile.yaml`: 8 releases reutilizaveis por qualquer cell. Kubeconfig injetado via env var `KUBECONFIG`.
- Downstream: `loopzero platform provision` roda este helmfile N vezes (uma por cell) com kubeconfigs diferentes.

#### Deep Dives

**Release ordering:**

| Wave | Release | needs: |
|------|---------|--------|
| -3 | cert-manager | (none) |
| -2 | traefik | cert-manager |
| -1 | external-secrets | (none) |
| -1 | metrics-server | (none) |
| 0 | keda | (none) |
| 0 | keda-http-addon | keda |
| 0 | kube-state-metrics | (none) |
| 0 | policy-controller | cert-manager |
| 0 | admission-policies | policy-controller |
| 0 | promtail | (none) |
| 0 | cloudflared | (none) |
| 1 | cluster-issuer | cert-manager |
| 1 | rbac | (none) |

**ClusterImagePolicy** (resolve B16):
- Incluida como release `admission-policies` (Helm chart convertido de raw manifests em T0.1)
- `mode: warn`, `static.action: pass` para audit mode
- Criada automaticamente — zero `kubectl apply` manual
- **Prerequisito**: T0.1 deve estar completa (Chart.yaml + templates/ criados)

**Cloudflared** (ADR D9):
- Deployment + Secret migrados do Pulumi `tunnel.ts` (T1.1) para Helmfile release
- Tunnel token injetado via env var `LOOPZERO_CF_TUNNEL_TOKEN` (do `pulumi stack output`)
- Image: `cloudflare/cloudflared:2024.12.2` com probes + non-root security context
- Replicas: 2 (HA)

**Multi-cell reuse:**
```bash
# runtime-1
KUBECONFIG=~/.kube/loopzero-runtime.yaml helmfile -f runtime.helmfile.yaml -e dev sync
# runtime-2
KUBECONFIG=~/.kube/loopzero-runtime-2.yaml helmfile -f runtime.helmfile.yaml -e dev sync
```

#### Tasks
1. Criar `infra/helmfile/runtime.helmfile.yaml`
2. Verificar que `infra/helm/admission-policies/` tem template para ClusterImagePolicy
3. Se nao: criar template basico
4. Testar: `helmfile -f infra/helmfile/runtime.helmfile.yaml lint`

#### TDD
```
RED:     helmfile_lint_runtime — lint passa
RED:     admission_policy_included — ClusterImagePolicy presente no diff
GREEN:   Criar helmfile com admission-policies release
REFACTOR: None expected
VERIFY:  helmfile -f infra/helmfile/runtime.helmfile.yaml lint
```

#### Acceptance Criteria
- [ ] 8 releases declarados com ordering
- [ ] ClusterImagePolicy criada automaticamente (B16 resolved)
- [ ] Reutilizavel para N cells via KUBECONFIG override
- [ ] `helmfile lint` passa

#### DoD
- [ ] Helmfile criado e lintado
- [ ] Multi-cell testado com dois kubeconfigs

---

### T2.4 — Criar scripts de wiring cross-cluster

#### Objective
Automatizar as operacoes que conectam clusters entre si: ArgoCD cluster registration, cross-cluster RBAC, Prometheus scrape targets.

#### Evidence
- `mgmt-network.ts`: registra runtime clusters no ArgoCD via Secret
- `mgmt-api.ts`: cria scoped kubeconfigs para cross-cluster access
- `mgmt-observability.ts`: configura Prometheus federation targets (Loki NodePort, KSM NodePort)
- Esses sao os passos que mais falham manualmente (B6 caData raw PEM, B7 RBAC missing)

#### Files to edit
```
infra/helmfile/scripts/wire-clusters.sh — NEW: cross-cluster wiring script
infra/helmfile/scripts/lib/argocd-register.sh — NEW: ArgoCD cluster Secret creation
infra/helmfile/scripts/lib/scoped-kubeconfig.sh — NEW: RBAC-scoped kubeconfig generator
```

#### Deep file dependency analysis
- `wire-clusters.sh`: Executado como Helmfile hook pos-sync no mgmt helmfile. Consome kubeconfigs de env vars.
- `argocd-register.sh`: Cria ArgoCD cluster Secret com `tlsClientConfig.caData` corretamente base64-encoded (resolve B6).
- `scoped-kubeconfig.sh`: Cria ServiceAccount + ClusterRole + kubeconfig scoped para loopzero-api acessar build/runtime.

#### Deep Dives
- **B6 fix (caData encoding)**: `cat $KUBECONFIG | yq '.clusters[0].cluster.certificate-authority-data'` ja eh base64. ArgoCD cluster Secret espera base64 de base64? NAO — espera base64 do PEM raw. O fix correto: decodificar o CA do kubeconfig, depois re-encodar para o Secret.
- **RBAC ClusterRole**: `loopzero-ctrl-runtime-rbac-admin` com `resources: ["*"]` e `verbs: ["get","list","watch"]` (fix B7).

#### Tasks
1. Criar `wire-clusters.sh` que orquestra os sub-scripts
2. Criar `argocd-register.sh` com encoding correto do caData
3. Criar `scoped-kubeconfig.sh` com RBAC criacao
4. Integrar como Helmfile post-sync hook no mgmt.helmfile.yaml
5. Testar encoding do caData

#### TDD
```
RED:     test_caData_encoding — base64 decode do caData deve comecar com "-----BEGIN CERTIFICATE-----"
RED:     test_rbac_permissions — ClusterRole deve ter resources: ["*"] verbs: ["get","list","watch"]
GREEN:   Implementar scripts
REFACTOR: None expected
VERIFY:  Executar scripts contra kubeconfig de teste
```

#### Acceptance Criteria
- [ ] ArgoCD cluster registration funciona sem erro de caData (B6)
- [ ] RBAC ClusterRole criado com permissoes completas (B7)
- [ ] Scoped kubeconfigs gerados para build + runtime
- [ ] Scripts idempotentes (re-run nao falha)

#### DoD
- [ ] Scripts criados e testados
- [ ] Integrados como hooks no mgmt helmfile

---

## Phase 3: Convergir `loopzero platform install` (ADR D11)

**Objective:** Refatorar `loopzero platform install` para usar Helmfile como unico mecanismo de instalacao. Adicionar `--provider` flag para provisionar infra opcionalmente. Deletar `prerequisites.ts`. Resultado: um unico comando serve LoopZero Cloud E self-hosting.

### T3.1 — Refatorar platformInstall para usar Helmfile

#### Objective
Reescrever `platformInstall()` em `cli/src/commands/platform.ts` para chamar `helmfile sync` em vez de `installOrUpgrade()` por chart. Deletar `cli/src/installer/prerequisites.ts`.

#### Evidence
- `platform.ts:platformInstall` (489 linhas) tem 10 steps que instalam charts via Helm direto
- `prerequisites.ts` (200+ linhas) duplica versoes e ordering que agora vivem nos helmfiles
- Dois caminhos = dois conjuntos de bugs — convergencia elimina a classe inteira

#### Files to edit
```
cli/src/commands/platform.ts — reescrever platformInstall para usar helmfile-runner
cli/src/installer/prerequisites.ts — DELETAR (substituido por helmfiles)
cli/src/installer/helmfile-runner.ts — NEW: wrapper para helmfile sync/destroy/diff
cli/src/installer/types.ts — estender PlatformConfig com campo `provider` opcional
cli/src/register/platform.ts — adicionar --provider flag ao install command
```

#### Deep file dependency analysis
- `platform.ts`: 10 steps atuais (validate → prereqs → registry → DB → CRDs → bootstrap → secrets → install → wait → validate). Novos steps: validate → preflight → [infra L1 se --provider] → helmfile sync → wiring → smoke.
- `prerequisites.ts`: Exporta `buildCorePrerequisites()`, `buildAuthPrerequisites()`, `VERSIONS`. TODOS substituidos por helmfiles. NENHUM consumer restante apos refactor.
- `helmfile-runner.ts`: Wrapper que executa `helmfile -f <file> -e <env> sync` com kubeconfig injection e output streaming. Usado por `platformInstall` e `platformUpgrade`.
- `types.ts`: `PlatformConfig` ganha campo opcional `provider?: 'digitalocean' | 'aws'` e `providerConfig?: { region, clusters, database }`.

#### Deep Dives

**Novo fluxo do `loopzero platform install`:**

```
loopzero platform install -f values.yaml [--provider digitalocean]
  │
  ├── Step 1: Validate (cluster reachable OR --provider set)
  ├── Step 2: Preflight (tokens, quotas, tools — inclui helmfile)
  │
  ├── Step 3: Infrastructure (CONDICIONAL — somente com --provider)
  │     └── pulumiUp(config) → outputs (kubeconfigs, DB host, etc.)
  │
  ├── Step 4: Bootstrap
  │     ├── Se --provider (multi-cluster):
  │     │     helmfileSync("build", kubeconfig_build)
  │     │     helmfileSync("mgmt", kubeconfig_mgmt, env_vars_from_L1)
  │     │     helmfileSync("runtime", kubeconfig_runtime) × N cells
  │     └── Se standalone (single cluster):
  │           helmfileSync("mgmt", current_kubeconfig, env="standalone")
  │
  ├── Step 5: Wire (ArgoCD registration, cross-cluster RBAC, DNS)
  ├── Step 6: Smoke test (create tenant, deploy, verify HTTP 200)
  └── Done: endpoints, credentials, next steps
```

**Backward compatibility:**
- `values.yaml` existente (sem `provider`) continua funcionando — instala no cluster atual
- `--profile minimal|production` continua funcionando — mapeado para helmfile environment overlay
- `loopzero platform status`, `upgrade`, `uninstall` nao mudam

**O que muda para o customer self-hosting:**
- Antes: `helm install` por chart (via prerequisites.ts)
- Depois: `helmfile sync` (via helmfile-runner.ts)
- Customer precisa ter `helmfile` instalado — adicionado no Step 2 preflight
- Se helmfile nao instalado: mensagem clara com link para instalacao

#### Tasks
1. Criar `helmfile-runner.ts` com `helmfileSync()`, `helmfileDiff()`, `helmfileDestroy()`
2. Estender `PlatformConfig` com `provider` e `providerConfig` opcionais
3. Adicionar `--provider` flag no `register/platform.ts`
4. Reescrever `platformInstall()`: 10 steps antigos → 6 steps novos
5. Criar `pulumi-runner.ts` com `pulumiUp()`, `pulumiDestroy()`, `pulumiOutput()` (condicional)
6. Deletar `prerequisites.ts`
7. Atualizar testes existentes de `platformInstall`
8. Adicionar testes novos para fluxo com `--provider`

#### TDD
```
RED:     test_install_uses_helmfile — platformInstall chama helmfileSync (nao installOrUpgrade)
RED:     test_install_standalone — sem --provider usa environment "standalone"
RED:     test_install_with_provider — com --provider digitalocean executa pulumiUp primeiro
RED:     test_install_provider_requires_region — --provider sem region no config retorna exit 1
RED:     test_install_no_prerequisites_ts — import de prerequisites.ts causa compile error
RED:     test_install_dry_run — --dry-run mostra plan sem executar
GREEN:   Implementar refactor
REFACTOR: Extrair phase interface para reuso entre install/upgrade
VERIFY:  cd cli && npm test -- --grep "platform install"
```

#### Acceptance Criteria
- [ ] `loopzero platform install -f values.yaml` funciona (self-hosting, standalone)
- [ ] `loopzero platform install -f values.yaml --provider digitalocean` funciona (LoopZero Cloud)
- [ ] `prerequisites.ts` DELETADO — zero references restantes
- [ ] `VERSIONS` dict NAO existe mais no CLI — somente nos helmfiles
- [ ] `--dry-run` mostra plan sem executar
- [ ] Exit codes corretos (0 success, 1 usage, 3 failure)
- [ ] Testes existentes de platformInstall atualizados e passando
- [ ] Loop back: nenhum dos 44 problemas reintroduzido pelo refactor

#### DoD
- [ ] platformInstall refatorado
- [ ] prerequisites.ts deletado
- [ ] Todos os testes passam
- [ ] `loopzero platform install --help` documenta --provider

---

### T3.2 — Convergir platformUpgrade e platformUninstall

#### Objective
Atualizar `platformUpgrade` e `platformUninstall` para usar helmfile (em vez de Helm direto), mantendo consistencia com o novo `platformInstall`.

#### Evidence
- `platformUpgrade` (linhas 407-447) chama `upgradePlatform()` que usa Helm direto
- `platformUninstall` (linhas 452-458) chama `uninstallPlatform()` que usa Helm direto
- Se install usa helmfile mas upgrade usa Helm direto, versoes podem divergir

#### Files to edit
```
cli/src/commands/platform.ts — refatorar platformUpgrade e platformUninstall
cli/src/installer/components.ts — avaliar se pode ser simplificado/deletado
```

#### Deep file dependency analysis
- `platformUpgrade`: Hoje extrai tokens existentes + chama `upgradePlatform(version, config, tokens)`. Com helmfile: `helmfile -e <env> sync` (idempotente — upgrade eh o mesmo que sync com novos values).
- `platformUninstall`: Hoje chama `uninstallPlatform()`. Com helmfile: `helmfile -e <env> destroy`.
- `components.ts`: Exporta `installPlatform`, `upgradePlatform`, `uninstallPlatform`. Se todo lifecycle usa helmfile, estas funcoes sao substituidas.

#### Deep Dives
- **Upgrade via helmfile**: `helmfile sync` eh idempotente — se chart version muda no helmfile, sync faz upgrade. Se nao muda, no-op. Isso simplifica drasticamente o upgrade: editar version no helmfile + `loopzero platform upgrade`.
- **Uninstall via helmfile**: `helmfile destroy` remove releases em ordem reversa do `needs:` — respeitando dependencias na remocao.
- **Token preservation**: `extractExistingTokens()` continua necessario para upgrades (nao regenerar secrets). Tokens sao lidos do cluster e passados como env vars para helmfile.

#### Tasks
1. Refatorar `platformUpgrade` para usar `helmfileSync` com token extraction
2. Refatorar `platformUninstall` para usar `helmfileDestroy`
3. Avaliar e simplificar `components.ts`
4. Atualizar testes

#### TDD
```
RED:     test_upgrade_uses_helmfile — platformUpgrade chama helmfileSync
RED:     test_upgrade_preserves_tokens — tokens existentes passados como env vars
RED:     test_uninstall_uses_helmfile — platformUninstall chama helmfileDestroy
GREEN:   Implementar refactor
REFACTOR: Deletar funcoes mortas em components.ts
VERIFY:  cd cli && npm test -- --grep "platform upgrade|platform uninstall"
```

#### Acceptance Criteria
- [ ] `loopzero platform upgrade` usa helmfile sync
- [ ] `loopzero platform uninstall` usa helmfile destroy
- [ ] Tokens preservados durante upgrade
- [ ] Zero Helm direto no CLI (tudo via helmfile)

#### DoD
- [ ] Upgrade e uninstall refatorados
- [ ] Testes passam
- [ ] Funcoes mortas removidas

---

## Phase 4: Health Gates

**Objective:** Adicionar validacao de saude entre cada fase do bootstrap. Nao prosseguir se o componente anterior nao esta healthy.

### T4.1 — Health checks por cluster

#### Objective
Implementar health checks que validam se cada cluster/componente esta funcional apos o Helmfile sync.

#### Evidence
- 8 problemas ORDERING causados por componentes instalados antes de dependencias estarem ready
- B17: ArgoCD repo-server cache stale apos bootstrap — health check detectaria
- B18: Runtime pods Pending por falta de CPU — health check detectaria

#### Files to edit
```
infra/helmfile/scripts/health-check.sh — NEW: health validation per cluster
cli/src/installer/health-checks.ts — NEW: programmatic health checks
```

#### Deep file dependency analysis
- `health-check.sh`: Executado como Helmfile post-sync hook. Valida deployment readiness, service endpoints, CRD registration.
- `health-checks.ts`: TypeScript equivalente para o CLI orchestrator. Chama kubectl + HTTP probes.

#### Deep Dives

**Health checks por fase:**

| Fase | Check | Comando | Expected |
|------|-------|---------|----------|
| Post-Infra | Cluster reachable | `kubectl cluster-info` | exit 0 |
| Post-Infra | PG connectable | `pg_isready -h <host>` | exit 0 |
| Post-Build | Argo API ready | `curl -sf http://<build-ip>:2746/api/v1/info` | HTTP 200 |
| Post-Build | Loki ready | `curl -sf http://<build-ip>:3100/ready` | HTTP 200 |
| Post-Mgmt | Gitea API | `curl -sf http://git.<domain>/api/v1/version` | HTTP 200 |
| Post-Mgmt | ArgoCD API | `curl -sf https://argocd.<domain>/api/version` | HTTP 200 |
| Post-Mgmt | LoopZero API | `curl -sf https://api.<domain>/api/v1/health` | HTTP 200 |
| Post-Runtime | Traefik LB | `kubectl get svc traefik -n traefik -o jsonpath='{.status.loadBalancer}'` | IP assigned |
| Post-Runtime | KEDA ready | `kubectl get deploy keda-operator -n keda -o jsonpath='{.status.readyReplicas}'` | >= 1 |
| Post-Wire | ArgoCD sees runtime | `argocd cluster list --grpc-web` | cluster listed |

**Retry logic**: Each check retries 30x com 10s interval (5 min total). Falha apos timeout = abort com mensagem de diagnostico.

#### Tasks
1. Criar `health-check.sh` com funcoes por cluster-role
2. Criar `health-checks.ts` com equivalente programatico
3. Integrar nos Helmfile hooks (post-sync)
4. Integrar no CLI orchestrator (entre fases)
5. Testar contra cluster real

#### TDD
```
RED:     test_health_check_timeout — check que falha retorna exit 1 apos timeout
RED:     test_health_check_passes — check que passa retorna exit 0
RED:     test_health_check_retry — check retry 3x antes de falhar
GREEN:   Implementar checks
REFACTOR: Extrair retry logic para funcao generica
VERIFY:  bash infra/helmfile/scripts/health-check.sh mgmt (contra cluster real)
```

#### Acceptance Criteria
- [ ] Health checks para todos os 4 clusters implementados
- [ ] Retry com timeout configuravel
- [ ] Mensagem de diagnostico clara em falha
- [ ] Integrado tanto no Helmfile (hook) quanto no CLI (programmatic)

#### DoD
- [ ] Health checks funcionais
- [ ] Testes passam
- [ ] Documentacao de cada check

---

## Phase 5: Preflight Checks

**Objective:** Implementar validacao pre-provisioning que detecta problemas antes de gastar 15-90 min em infraestrutura.

### T5.1 — Framework de preflight checks

#### Objective
Criar preflight checks que validam pre-condicoes para provisioning (tokens, quotas, DNS, ferramentas).

#### Evidence
- `cli/src/installer/validator.ts` ja tem `validateCluster()` com 5 checks basicos
- Precisa ser estendido para cobrir: DO API token, CF API token, quota de droplets, helmfile no PATH, DNS zone access

#### Files to edit
```
cli/src/installer/preflight.ts — NEW: extended preflight checks
cli/src/installer/validator.ts — estender com novos checks
```

#### Deep file dependency analysis
- `validator.ts`: Ja exporta `validateCluster()` com interface `ValidationResult`. Estender com novos checks sem quebrar existentes.
- `preflight.ts`: Novo modulo com checks especificos para `provision` (nao `install`). Separa concerns: `install` valida cluster existente, `provision` valida pre-cluster.

#### Deep Dives

**Preflight checks para provision:**

| Check | Comando | Expected | Severity |
|-------|---------|----------|----------|
| DO token valid | `doctl account get` | exit 0 | BLOCK |
| DO droplet quota | `doctl account get -o json \| jq .droplet_limit` | >= 12 | BLOCK |
| CF token valid | `curl -H "Authorization: Bearer $CF_TOKEN" api.cloudflare.com/client/v4/user/tokens/verify` | success=true | BLOCK (if CF enabled) |
| CF zone access | `curl .../zones/$ZONE_ID` | zone exists | BLOCK (if CF enabled) |
| pulumi installed | `pulumi version` | exit 0 | BLOCK |
| helmfile installed | `helmfile version` | exit 0 | BLOCK |
| helm installed | `helm version` | exit 0 | BLOCK |
| kubectl installed | `kubectl version --client` | exit 0 | BLOCK |
| cosign installed | `cosign version` | exit 0 | WARN |
| Existing resources | adopt-or-create lookups | detected | INFO |
| Disk space | `df -h .` | >= 5GB free | WARN |

**Severity levels:**
- BLOCK: Provisioning nao inicia
- WARN: Provisioning inicia com aviso
- INFO: Informacao util

#### Tasks
1. Criar `preflight.ts` com checks para provision
2. Estender `validator.ts` com novos checks para install
3. Integrar na Phase 0 do orchestrator
4. Testes unitarios (mock do execSync para simular falhas)

#### TDD
```
RED:     test_preflight_blocks_on_missing_token — sem DO_TOKEN retorna BLOCK
RED:     test_preflight_warns_on_missing_cosign — sem cosign retorna WARN (nao BLOCK)
RED:     test_preflight_detects_existing_resources — adopt-or-create retorna INFO
GREEN:   Implementar checks
REFACTOR: Extrair check interface para reutilizar entre install e provision
VERIFY:  cd cli && npm test -- --grep "preflight"
```

#### Acceptance Criteria
- [ ] 11 checks implementados
- [ ] BLOCK impede provisioning
- [ ] WARN permite com aviso
- [ ] Output formatado para CLI (checkmarks/X)
- [ ] Reutilizavel entre `install` e `provision`

#### DoD
- [ ] Preflight funcional
- [ ] Testes passam
- [ ] Integrado no orchestrator

---

## Phase 6: ArgoCD App of Apps (Day-2)

**Objective:** Configurar ArgoCD para gerenciar os mesmos Helm charts que o Helmfile instalou, habilitando drift detection e upgrades via GitOps apos o bootstrap inicial.

### T6.1 — Criar bootstrap Git repo structure

#### Objective
Criar a estrutura de diretorio Git que o ArgoCD App of Apps consome para manter o estado dos charts pos-bootstrap.

#### Evidence
- ArgoCD ja esta instalado (mgmt cluster)
- Kubefirst usa exatamente este pattern: Helmfile para Day-0, ArgoCD App of Apps para Day-2
- Hoje, drift eh invisivel (Helm releases nao tem GitOps tracking)

#### Files to edit
```
infra/argocd-bootstrap/ — NEW: root directory for App of Apps
infra/argocd-bootstrap/root-app.yaml — NEW: root Application pointing to apps/
infra/argocd-bootstrap/apps/ — NEW: one Application manifest per Helm release
infra/argocd-bootstrap/apps/mgmt-cert-manager.yaml — NEW: example (implementação real usa cluster-role prefix, e.g., mgmt-X / runtime-X / build-X)
infra/argocd-bootstrap/apps/mgmt-traefik.yaml — NEW: example
... (um por release)
```

#### Deep file dependency analysis
- `root-app.yaml`: ArgoCD Application que aponta para `apps/` directory. Self-referencing (ArgoCD manages itself).
- `apps/*.yaml`: Cada arquivo eh uma ArgoCD Application que referencia o Helm chart + values do release correspondente.

#### Deep Dives

**Sync wave annotations** para ordering (equivalente ao `needs:` do Helmfile):
```yaml
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "-3"  # cert-manager first
```

**Day-0 → Day-2 transition:**
1. Helmfile instala tudo (Day-0)
2. Pos-install, apply `root-app.yaml` no ArgoCD
3. ArgoCD detecta que os releases ja existem (in-sync)
4. A partir desse ponto, ArgoCD monitora drift e pode re-sync
5. Upgrades: PR no Git → ArgoCD sync → Helm upgrade

**Self-managing ArgoCD:**
- ArgoCD Application for ArgoCD itself usa `selfHeal: false` (nao queremos ArgoCD se auto-modificando sem PR)
- Outros apps: `selfHeal: true` + `prune: true` para auto-recovery

#### Tasks
1. Criar `infra/argocd-bootstrap/root-app.yaml`
2. Criar Application manifests para cada release (gerar via script dos helmfiles)
3. Adicionar sync wave annotations
4. Documentar Day-0 → Day-2 transition
5. Integrar como passo final do `loopzero platform provision`

#### TDD
```
RED:     kubeconform_root_app — root-app.yaml valido contra ArgoCD Application schema
RED:     sync_waves_ordered — waves -3 < -2 < -1 < 0 < 1 < 2
GREEN:   Criar manifests
REFACTOR: Script para gerar apps/ a partir dos helmfiles
VERIFY:  task validate:manifests (incluindo argocd-bootstrap/)
```

#### Acceptance Criteria
- [ ] root-app.yaml valido
- [ ] Um Application manifest por Helm release
- [ ] Sync waves corretos
- [ ] Documentacao de Day-0 → Day-2 transition

#### DoD
- [ ] Bootstrap repo criado
- [ ] Manifests validados
- [ ] Integracao com orchestrator

---

## Phase 7: Validacao End-to-End no DigitalOcean

**Objective:** Validar que o fluxo completo (`pulumi up` + `helmfile sync` + wiring + smoke) funciona sem intervencao manual, com 100-tenant L3 deploy.

### T7.1 — Full destroy + rebuild validation

#### Objective
Executar o ciclo completo destroy → provision → L3 deploy no DigitalOcean e documentar resultados.

#### Evidence
- Sessao Sprint 8: 25h, 76 workarounds → meta: <2h, 0 workarounds
- CHG-002b validation matrix requer L3 95/100 success rate

#### Files to edit
```
docs/runbooks/layered-bootstrap-validation.md — NEW: runbook de validacao
```

#### Deep file dependency analysis
- N/A — validacao operacional, nao code change.

#### Deep Dives

**Procedimento (2 gates progressivos):**

**Gate 1 — L3-50 (50 tenants):**
1. `pulumi destroy --stack dev --yes` (deve completar em <5 min)
2. `loopzero platform provision --config platform-dev.yaml` (deve completar sem manual steps)
3. Verificar health: `loopzero platform status` → all green
4. Executar L3-50: `LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l3-realistic LOOPZERO_TENANT_COUNT=50 go test ./infra/tests/load/ -run L3Realistic -v -timeout 2h`
5. Resultado: >= 47/50 tenants (94%) com app respondendo HTTP 200
6. Se FAIL: diagnosticar, ajustar helmfile values, re-run sem re-provisionar L1 (`--skip-infra`)

**Gate 2 — L3-100 (100 tenants):**
7. Escalar runtime pool se necessario: `doctl kubernetes cluster node-pool update ... --count 8`
8. Executar L3-100: `LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l3-realistic LOOPZERO_TENANT_COUNT=100 go test ./infra/tests/load/ -run L3Realistic -v -timeout 4h`
9. Resultado: >= 95/100 tenants com app respondendo HTTP 200

**Metricas de sucesso:**
- Destroy time: <5 min (era 60+ min com state surgery)
- Provision time: <30 min (era 90+ min com manual steps)
- Manual interventions: 0 (era 76)
- L3-50 success rate: >= 94% (gate intermediario)
- L3-100 success rate: >= 95% (gate final, era 0%)

#### Tasks
1. Executar destroy
2. Executar provision
3. Verificar health
4. Executar L3
5. Documentar resultados no runbook

#### TDD
```
RED:     destroy_under_5min — timer do destroy < 300s
RED:     provision_zero_manual — nenhum kubectl/doctl manual durante provision
RED:     l3_95_percent — >= 95/100 tenants com HTTP 200
GREEN:   Execute e documente
REFACTOR: Ajustar helmfile values baseado em findings
VERIFY:  Runbook documentado com evidencia
```

#### Acceptance Criteria
- [ ] Destroy completo em <5 min sem state surgery
- [ ] Provision completo sem intervencao manual
- [ ] `loopzero platform status` mostra all green
- [ ] L3 >= 95/100 success rate
- [ ] Runbook documentado com timestamps e evidencia

#### DoD
- [ ] Validacao executada e documentada
- [ ] Metricas de sucesso atingidas
- [ ] Runbook criado

---

## Phase 8: Regression Tests + Loop Back Final

**Objective:** Garantir que CADA UM dos 44 problemas tem protecao permanente contra recorrencia. Nenhum problema pode ser marcado como "resolvido" sem pelo menos uma das tres protecoes: (a) teste automatizado, (b) config declarativa, ou (c) check de runtime. Esta phase eh o loop back final antes de declarar o plano Done.

### T8.1 — Regression Test Suite para B1-B18

#### Objective
Escrever testes automatizados que **falham se o bug voltar** para cada um dos 18 bugs L3 que hoje nao tem teste de regressao.

#### Evidence
- B1 (cosign placeholder): nenhum teste verifica que o Secret nao contem "PLACEHOLDER"
- B4 (GITEA_TOKEN newline): nenhum teste verifica que o token nao tem `\n` trailing
- B6 (caData encoding): tem regression test (commitado) — OK
- B12 (registry merge): tem convergence proof (7 tests) — OK
- B16 (ClusterImagePolicy): nenhum teste verifica que a policy existe pos-bootstrap
- B17 (repo-server cache): nenhum teste verifica que apps nao estao "(cached)" apos fresh bootstrap

#### Files to edit
```
infra/helmfile/scripts/regression-checks.sh — NEW: script executado pos-bootstrap que valida todos os B1-B18
infra/tests/load/regression_test.go — NEW: Go tests que validam pre-condicoes antes de L3
```

#### Deep file dependency analysis
- `regression-checks.sh`: Executado como step final do `loopzero platform provision`. Consome kubeconfigs do L1 output. Valida 18 condicoes correspondentes aos 18 bugs.
- `regression_test.go`: Go test que pode rodar independentemente. Verifica pre-condicoes do cluster antes do L3 (cosign key real, GITEA_TOKEN limpo, caData correto, RBAC completo, etc.).

#### Deep Dives

**Tabela de protecoes por bug:**

| Bug | Protecao Atual | Protecao Necessaria | Tipo |
|-----|---------------|---------------------|------|
| B1 cosign placeholder | Nenhuma | `regression-checks.sh`: Secret value nao contem "PLACEHOLDER" | (a) teste |
| B2 local-path StorageClass | Nenhuma | `regression-checks.sh`: `kubectl get sc local-path -o jsonpath='{.provisioner}'` == `rancher.io/local-path` | (a) teste |
| B3 MaxConcurrent | Config em Pulumi.dev.yaml | `regression-checks.sh`: env var LOOPZERO_TENANT_MAX_CONCURRENT >= 20 | (b) config + (a) teste |
| B4 GITEA_TOKEN newline | Nenhuma | `regression-checks.sh`: `wc -c` do token decodificado == 40 (sem newline) | (a) teste |
| B5 Loki streams | Config em values.yaml | `regression-checks.sh`: Loki config `max_global_streams_per_user` >= 50000 | (b) config + (a) teste |
| B6 caData encoding | Regression test em Go | JA PROTEGIDO | (a) teste |
| B7 ArgoCD RBAC | Config em runtime-bootstrap.ts | `regression-checks.sh`: ClusterRole tem `resources: ["*"]` | (b) config + (a) teste |
| B8-B9 monorepo workspace | Unit tests em workspace_build_test.go | JA PROTEGIDO | (a) teste |
| B10 L3 harness | Code fix em test harness | JA PROTEGIDO (harness le loopzero.yaml) | (a) teste |
| B11 gitops archived | Nenhuma | `regression-checks.sh`: nenhum repo gitops com `archived: true` | (a) teste |
| B12 registry merge | 7 unit tests convergence proof | JA PROTEGIDO | (a) teste |
| B13 Harbor self-heal | Code fix com semantic check | JA PROTEGIDO (secretHasHostAuth) | (a) teste |
| B14 DeleteRobot endpoint | Code fix com dual lookup | JA PROTEGIDO | (a) teste |
| B15 annotation collision | Code fix com scoped annotations | JA PROTEGIDO | (a) teste |
| B16 ClusterImagePolicy | Nenhuma (T0.1 cria chart) | `regression-checks.sh`: `kubectl get clusterimagepolicy` retorna >= 1 | (a) teste + (b) config |
| B17 repo-server cache | Nenhuma | Health check T4.1 detecta "(cached)" e faz restart | (c) runtime check |
| B18 runtime pool sizing | Config em Pulumi.dev.yaml | `regression-checks.sh`: node pool max >= 8 | (b) config + (a) teste |

**Bugs que precisam de teste NOVO: B1, B2, B3, B4, B5, B7, B11, B16, B18** (9 testes)
**Bugs ja protegidos: B6, B8-B10, B12-B15** (8 testes existentes)
**Bugs protegidos por runtime check: B17** (1 health check)

#### Tasks
1. Criar `regression-checks.sh` com 9 checks para bugs sem protecao
2. Criar `regression_test.go` com Go tests equivalentes para CI
3. Integrar como step final do `loopzero platform provision` (pos-smoke, pre-Done)
4. Validar: rodar script contra cluster real e confirmar 18/18 PASS
5. Documentar cada check com referencia ao bug original

#### TDD
```
RED:     test_b1_cosign_not_placeholder — Secret nao contem "PLACEHOLDER"
RED:     test_b2_local_path_rancher — provisioner == rancher.io/local-path
RED:     test_b3_max_concurrent_20 — env var >= 20
RED:     test_b4_gitea_token_no_newline — wc -c == 40
RED:     test_b5_loki_streams_100k — config >= 50000
RED:     test_b7_rbac_cluster_scoped — ClusterRole resources includes *
RED:     test_b11_no_archived_repos — zero repos com archived=true
RED:     test_b16_cluster_image_policy — >= 1 ClusterImagePolicy exists
RED:     test_b18_runtime_max_nodes — node pool maxNodes >= 8
GREEN:   Implementar checks
REFACTOR: Consolidar em funcao check_or_fail() reutilizavel
VERIFY:  bash regression-checks.sh && go test ./infra/tests/load/ -run Regression
```

#### Acceptance Criteria
- [ ] 18/18 bugs com protecao documentada (teste, config, ou runtime check)
- [ ] 9 testes novos escritos e passando
- [ ] Script integravel no `loopzero platform provision`
- [ ] Cada check referencia o bug original (B1-B18)

#### DoD
- [ ] Regression suite criada e funcional
- [ ] 18/18 protecoes verificadas
- [ ] Integrado no orchestrator

---

### T8.2 — Loop Back Checklist: 44/44 Problemas com Protecao

#### Objective
Revisar TODOS os 44 problemas catalogados e confirmar que cada um tem protecao permanente contra recorrencia. Produzir um documento de protecao que serve como referencia futura.

#### Evidence
- ADR D10: todo problema DEVE ter protecao
- Retrospectiva Sprint 8: pattern "fix and forget" causou 76 workarounds
- Sem esta verificacao, o proximo bootstrap pode reintroduzir problemas que o plano "resolveu"

#### Files to edit
```
docs/runbooks/bootstrap-regression-matrix.md — NEW: matriz 44 problemas × protecao permanente
```

#### Deep file dependency analysis
- Documento standalone. Referencia: catalogo de 44 problemas, regression-checks.sh, helmfile configs, health checks.

#### Deep Dives

**Formato da matriz:**

```markdown
| # | Problema | Protecao | Tipo | Verificacao | Pode Recorrer? |
|---|---------|----------|------|-------------|----------------|
| 1 | ClusterImagePolicy (B16) | Helm chart + helmfile release | config | regression-checks.sh test_b16 | NAO — criado automaticamente |
| 2 | repo-server cache (B17) | Health check T4.1 | runtime | health-check.sh post-mgmt | PARCIAL — detecta mas nao previne |
| ... | ... | ... | ... | ... | ... |
```

**Classificacao "Pode Recorrer?":**
- **NAO** — protecao elimina a possibilidade (config declarativa, teste automatizado)
- **PARCIAL** — protecao detecta mas nao previne (health check, runtime check)
- **SIM** — nenhuma protecao efetiva (INACEITAVEL — deve gerar task de correcao)

**Criterio de saida:** Zero problemas com "SIM". Todos devem ser "NAO" ou "PARCIAL" com justificativa.

**Loop back por fase (retrospectivo):**

Apos CADA phase completada durante a execucao do plano, o executor DEVE revisar a matriz e responder:

> "Alguma mudanca que fiz nesta phase pode reintroduzir algum dos 44 problemas?"

Se SIM:
1. Identificar qual problema
2. Adicionar protecao (teste, config, ou check)
3. Atualizar a matriz
4. So entao prosseguir para a proxima phase

Este checkpoint eh **bloqueante** — nao eh opcional, nao eh "se der tempo". Eh parte do DoD de cada phase.

#### Tasks
1. Criar `bootstrap-regression-matrix.md` com 44 linhas
2. Para cada problema: documentar protecao, tipo, verificacao, e "Pode Recorrer?"
3. Identificar gaps (problemas com "SIM") e criar tasks de correcao
4. Validar: zero problemas com "SIM"

#### TDD
```
RED:     Matriz tem 44 linhas
RED:     Zero problemas com classificacao "SIM"
RED:     Cada problema tem pelo menos uma verificacao (coluna nao vazia)
GREEN:   Preencher matriz + corrigir gaps
REFACTOR: None
VERIFY:  grep -c "SIM" bootstrap-regression-matrix.md (expected: 0)
```

#### Acceptance Criteria
- [ ] 44/44 problemas documentados na matriz
- [ ] Zero problemas com "Pode Recorrer? = SIM"
- [ ] Cada problema tem pelo menos uma protecao (teste, config, ou runtime)
- [ ] Loop back checkpoint documentado como DoD de cada phase

#### DoD
- [ ] Matriz completa e validada
- [ ] Zero gaps
- [ ] Documento publicado em docs/runbooks/

---

### T8.3 — Post-mortem Loop: L3 Coverage dos 44 Problemas

#### Objective
Verificar que o L3 test (100-tenant deploy) exercita CADA cenario dos 44 problemas. Se um problema nao eh exercitado pelo L3, adicionar coverage ou justificar exclusao.

#### Evidence
- L3 eh o gate final do plano — se nao exercita um cenario, o cenario nao esta validado
- Problemas como B11 (gitops archived) e B17 (repo-server cache) so aparecem apos MULTIPLAS iteracoes L3 (tenant delete + recreate cycles)

#### Files to edit
```
infra/tests/load/real_deploy_test.go — estender com cenarios de regressao
docs/runbooks/bootstrap-regression-matrix.md — adicionar coluna "Exercitado por L3?"
```

#### Deep file dependency analysis
- `real_deploy_test.go`: L3 harness. Hoje testa deploy + verify HTTP 200. Precisa de cenarios adicionais: tenant delete + recreate (B11), concurrent deploy (B-C), burst deploy (B18).
- `bootstrap-regression-matrix.md`: Matriz de T8.2 ganha coluna extra.

#### Deep Dives

**Cenarios L3 adicionais necessarios:**

| Bug | Cenario L3 | Implementacao |
|-----|-----------|---------------|
| B11 gitops archived | Delete 10% dos tenants, recreate, deploy novamente | `L3RealisticWithChurn` test variant |
| B17 repo-server cache | Deploy 100 tenants em batch, verificar zero "(cached)" | Check no verify step do L3 |
| B-C git push race | 10 rapid deploys no mesmo tenant | `L3NoisyNeighbor` test variant |
| B18 runtime burst | Medir tempo de scheduling dos ultimos 10 pods | Metrica no verify step |

**Cenarios ja exercitados pelo L3 existente:**
- B1-B7: Pre-condicoes validadas pelo regression-checks.sh (T8.1) antes do L3
- B8-B10: Exercitados porque L3 usa dogfood-monorepo (4 apps workspace)
- B12-B15: Exercitados porque L3 cria tenants com DOCR + Harbor
- B16: Exercitado porque L3 cria Deployments que passam pelo webhook

#### Tasks
1. Adicionar coluna "Exercitado por L3?" na matriz de T8.2
2. Implementar `L3RealisticWithChurn` test variant (delete + recreate 10%)
3. Adicionar check de "(cached)" no verify step do L3
4. Documentar gaps de coverage com justificativa

#### TDD
```
RED:     test_l3_churn_recreate — tenants deletados e recriados deployam com sucesso
RED:     test_l3_no_cached_apps — zero apps com "(cached)" no status
GREEN:   Implementar variants
REFACTOR: Extrair churn logic para funcao reutilizavel
VERIFY:  go test ./infra/tests/load/ -run L3RealisticWithChurn -v -timeout 4h
```

#### Acceptance Criteria
- [ ] Matriz tem coluna "Exercitado por L3?" para todos os 44 problemas
- [ ] >= 38/44 problemas exercitados pelo L3 (os 6 aceitos podem ter "N/A")
- [ ] Test variants implementados para gaps criticos (B11, B17)
- [ ] Zero problemas criticos sem coverage L3

#### DoD
- [ ] Coverage matrix atualizada
- [ ] Test variants implementados
- [ ] L3 full run confirma cobertura

---

## Coverage Matrix

| # | Problema (dos 44) | Task(s) | Resolucao | Protecao Permanente (ADR D10) | Recorre? |
|---|---|---|---|---|---|
| 1 | DAY0: ClusterImagePolicy (B16) | T0.1, T2.3 | Helm chart + Helmfile release | (a) T8.1 test_b16 + (b) helmfile release | NAO |
| 2 | DAY0: repo-server cache (B17) | T4.1 | Health check + restart | (c) health-check.sh post-mgmt | PARCIAL |
| 3 | DAY0: Runtime pre-warm (B18) | T0.2, T2.3 | runtimeMaxNodes persistido | (a) T8.1 test_b18 + (b) Pulumi.dev.yaml | NAO |
| 4 | DAY0: Promtail runtime-2 | T2.3 | Helmfile runtime reutilizado | (b) helmfile per-cell | NAO |
| 5 | DAY0: Promtail mgmt | T2.1 | Promtail no mgmt helmfile | (b) helmfile release | NAO |
| 6 | DAY0: CLAUDE.md stale | T0.2 | Atualizado | (b) review no DoD de cada phase | NAO |
| 7 | DAY0: B12-B15 uncommitted | — | JA COMMITADO e4a55c13 | (a) 7 unit tests convergence proof | NAO |
| 8 | DAY0: gitops archived (B11) | T2.4 | Wiring script unarchive | (a) T8.1 test_b11 + (a) T8.3 L3Churn | NAO |
| 9 | DAY0: gitea_pusher wrapping | — | Bug de codigo | Nenhuma — low priority | N/A |
| 10 | ORDERING: cert-manager→Traefik | T2.1-T2.3 | Helmfile `needs:` | (b) declarativo no helmfile | NAO |
| 11 | ORDERING: ArgoCD RBAC (B7) | T2.4 | Wiring script RBAC | (a) T8.1 test_b7 | NAO |
| 12 | ORDERING: caData PEM (B6) | T2.4 | Script encoding | (a) regression test Go existente | NAO |
| 13 | ORDERING: MaxConcurrent (B3) | T0.2 | Pulumi.dev.yaml | (a) T8.1 test_b3 + (b) config | NAO |
| 14 | ORDERING: policy-controller (B16) | T2.3 | Helmfile release | (a) T8.1 test_b16 | NAO |
| 15 | ORDERING: monorepo (B8-B9) | — | JA COMMITADO | (a) workspace_build_test.go | NAO |
| 16 | ORDERING: L3 harness (B10) | — | JA COMMITADO | (a) harness le loopzero.yaml | NAO |
| 17 | ORDERING: git push race (B-C) | — | Low severity | Nenhuma — aceito | N/A |
| 18 | CAPACITY: Loki streams (B5) | — | JA COMMITADO | (a) T8.1 test_b5 + (b) values.yaml | NAO |
| 19 | CAPACITY: Runtime sizing (B18) | T0.2 | runtimeMaxNodes: 8 | (a) T8.1 test_b18 + (b) config | NAO |
| 20 | CAPACITY: Build autoscaler (B-D) | — | JA COMMITADO | (b) Pulumi.dev.yaml buildMaxNodes | NAO |
| 21 | CAPACITY: DOCR QPS (B-E) | — | Self-recovers | Nenhuma — aceito | N/A |
| 22 | CAPACITY: Gitea clones | — | Nao testado | Nenhuma — L3 coverage gap | PARCIAL |
| 23 | CAPACITY: MinIO migration | — | Fora de escopo | Nenhuma — CHG-002c | N/A |
| 24 | CAPACITY: Loki NotReady | — | JA COMMITADO | (b) values.yaml stream+labels | NAO |
| 25 | CONFIG_DRIFT: Node pools | T0.2 | Persistido | (b) Pulumi.dev.yaml | NAO |
| 26 | CONFIG_DRIFT: Loki streams | — | JA COMMITADO | (b) values.yaml | NAO |
| 27 | CONFIG_DRIFT: Build autoscaler | — | JA COMMITADO | (b) Pulumi.dev.yaml | NAO |
| 28 | CONFIG_DRIFT: Promtail labels | — | JA COMMITADO | (b) values.yaml | NAO |
| 29 | CONFIG_DRIFT: runtime-2 Promtail | T2.3 | Helmfile reutilizado | (b) helmfile per-cell | NAO |
| 30 | STATE_DRIFT: cosign placeholder (B1) | T2.2 | Auto-generated keypair | (a) T8.1 test_b1 | NAO |
| 31 | STATE_DRIFT: Secret oscillation (B12) | — | JA COMMITADO | (a) 7 unit tests + MergeAuthsByHost | NAO |
| 32 | STATE_DRIFT: CDN orphan | T1.1 | CDN no Pulumi L1 | (b) separacao L1/L2 | NAO |
| 33 | STATE_DRIFT: destroy 404 | T1.1, T1.2 | Zero K8s no state | (b) separacao L1/L2 | NAO |
| 34 | TEARDOWN: CRD finalizers | T1.1 | Zero CRDs no Pulumi | (b) separacao L1/L2 | NAO |
| 35 | TEARDOWN: K8s retry infinito | T1.1, T1.3 | Zero K8s provider | (b) separacao L1/L2 | NAO |
| 36 | TEARDOWN: 3 rounds surgery | T1.1, T1.2 | Destroy <5min | (a) T7.1 destroy timer | NAO |
| 37 | SECRET: GITEA_TOKEN newline (B4) | T2.4 | printf sem newline | (a) T8.1 test_b4 | NAO |
| 38 | SECRET: cosign placeholder (B1) | T2.2 | Auto-generated | (a) T8.1 test_b1 | NAO |
| 39 | IDEMPOTENCY: Harbor robot (B13) | — | JA COMMITADO | (a) secretHasHostAuth code | NAO |
| 40 | IDEMPOTENCY: DeleteRobot (B14) | — | JA COMMITADO | (a) findRobotInProject code | NAO |
| 41 | IDEMPOTENCY: repo-server cache (B17) | T4.1 | Health check + restart | (c) health-check.sh | PARCIAL |
| 42 | SELF_HOSTING: adopt-or-create DO | T3.1 | --provider flag + contracts/ | (b) contracts/ pattern + --provider | NAO |
| 43 | SELF_HOSTING: install vs provision | T3.1, D11 | ELIMINADO — unico comando | (b) prerequisites.ts deletado | NAO |
| 44 | GOVERNANCE: Zero ADRs | D1-D11 | 11 ADRs neste plano | (b) ADRs no plano | NAO |

**Coverage: 44/44 problemas com protecao documentada (100%).**
- **NAO recorre:** 36 problemas — protecao elimina recorrencia
- **PARCIAL:** 4 problemas (B17 cache, B22 Gitea clones) — detecta mas nao previne
- **N/A:** 4 problemas — aceitos (low severity) ou fora de escopo

## Global Definition of Done

- [ ] All phases (0-8) completed
- [ ] `pulumi destroy` < 5 min, zero state surgery
- [ ] `helmfile sync` instala 26 releases com ordering correto
- [ ] `loopzero platform provision` funcional end-to-end
- [ ] Zero intervencoes manuais no bootstrap
- [ ] Health checks passam apos cada fase
- [ ] Preflight checks bloqueiam provisioning com erros claros
- [ ] ArgoCD App of Apps gerencia Day-2 drift detection
- [ ] L3-50 gate: >= 94% success rate no DigitalOcean
- [ ] L3-100 gate: >= 95% success rate no DigitalOcean
- [ ] **44/44 problemas com protecao permanente documentada (ADR D10)**
- [ ] **Regression test suite (T8.1): 18/18 bugs B1-B18 com teste ou check**
- [ ] **Loop back matrix (T8.2): zero problemas com "Pode Recorrer? = SIM"**
- [ ] **L3 coverage (T8.3): >= 38/44 problemas exercitados pelo L3**
- [ ] CHANGELOG.md atualizado com todas as mudancas
- [ ] Todos os testes passam (CLI + operators + API)
- [ ] `helmfile lint` passa para os 3 helmfiles
- [ ] `task validate:manifests` passa
- [ ] Loop back checkpoint executado em CADA phase (nao apenas no final)
