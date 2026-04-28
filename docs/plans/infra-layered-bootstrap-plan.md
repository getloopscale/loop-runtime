# Plan: Infrastructure Layered Bootstrap

> **Version 1.0** — Refatorar a infraestrutura de provisioning do LoopZero de um unico stack Pulumi (cloud infra + K8s workloads) para uma arquitetura de 3 camadas separadas: L1 Cloud Infra (Pulumi), L2 K8s Bootstrap (Helmfile), L3 Platform Config (Operators/GitOps). Isso elimina o problema de zombie state no destroy, resolve 35 dos 44 problemas documentados na retrospectiva Sprint 8, e habilita self-hosting confiavel para clientes. O resultado eh um `loopzero platform provision` que funciona end-to-end sem intervencao manual.

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

**Done** significa: `pulumi destroy && pulumi up && helmfile sync` em sequencia produz uma plataforma 100% funcional sem nenhuma intervencao manual, validada com L3 100-tenant deploy no DigitalOcean.

Metas especificas:
1. `pulumi destroy` completa em <5min sem state surgery (zero K8s resources no state)
2. `helmfile sync` instala 26 releases com ordering correto e health gates entre fases
3. 35 dos 44 problemas documentados eliminados (80%)
4. `loopzero platform provision` como CLI wrapper end-to-end com preflight + progress + smoke test
5. Self-hosting viavel: mesmo Helmfile funciona em qualquer K8s cluster

## ADRs

### D1 — Manter Pulumi para L1, nao migrar para Terraform/OpenTofu

**Decision:** Pulumi continua como ferramenta de IaC para recursos cloud (L1). Nao migrar para Terraform, OpenTofu, ou CAPI.

**Rationale:** O problema de zombie state eh identico em Terraform (K8s provider perde endpoint quando cluster eh destruido). Migrar reescreveria 6795 linhas TS→HCL sem ganho na confiabilidade. O time ja conhece Pulumi. O provider factory pattern (`contracts/`) permite trocar cloud providers sem trocar ferramenta.

**Consequences:** Pulumi permanece. A melhoria vem da separacao de camadas, nao da troca de ferramenta. K8s resources sao REMOVIDOS do Pulumi state.

### D2 — Helmfile como orquestrador L2 (nao ArgoCD App of Apps direto)

**Decision:** Usar Helmfile para Day-0 bootstrap (instalacao inicial dos 26 Helm charts). ArgoCD App of Apps assume Day-2 (drift detection, upgrades).

**Rationale:** ArgoCD precisa estar instalado antes de poder gerenciar a si mesmo (chicken-and-egg). Helmfile resolve o bootstrap com `needs:` para ordering e `helmfile sync` idempotente. Kubefirst usa exatamente essa combinacao. ArgoCD App of Apps eh melhor para manter estado continuous do que para bootstrap from scratch.

**Consequences:** Dois mecanismos de deploy: Helmfile (Day-0) e ArgoCD (Day-2). O bootstrap repo Git serve ambos. `helmfile destroy` + `helmfile sync` eh o recovery path.

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

### D6 — Helmfile por cluster (nao monolitico)

**Decision:** Um helmfile por cluster-role: `mgmt.helmfile.yaml`, `build.helmfile.yaml`, `runtime.helmfile.yaml`. Nao um helmfile unico.

**Rationale:** Cada cluster tem kubeconfig diferente, releases diferentes, e pode ser rebuilt independentemente. Um helmfile monolitico exige selectors complexos. Helmfile per-cluster espelha a realidade operacional.

**Consequences:** 3 helmfiles a manter (4 com runtime-2). Mais claro, mais simples de debugar. Self-hosting single-cluster: usa apenas `mgmt.helmfile.yaml` com profile `standalone`.

### D7 — Secrets gerados pelo Helmfile via hooks (nao Pulumi RandomPassword)

**Decision:** Secrets (tokens, passwords, keypairs) sao gerados por Helmfile pre-install hooks usando `openssl rand` / `cosign generate-key-pair`. Nao mais `@pulumi/random.RandomPassword`.

**Rationale:** `RandomPassword` no Pulumi state eh perdido no destroy. Secrets devem ser gerados uma vez e persistidos em K8s Secrets (que morrem com o cluster — aceitavel para dev; prod usa ESO/Vault). Para prod, secrets vem de config externo.

**Consequences:** Dev: secrets auto-gerados, efemeros. Prod: secrets injetados via values overlay. Sem dependencia de Pulumi state para credenciais.

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
Phase 0 (Commit Fixes) ──▶ Phase 1 (Pulumi L1-only) ──▶ Phase 3 (Orchestrator CLI)
                                     │                           │
                                     ▼                           ▼
                            Phase 2 (Helmfile L2) ──────▶ Phase 4 (Health Gates)
                                                                 │
Phase 5 (Preflight) ◀── pode iniciar em paralelo ──────────────▶│
                                                                 │
                                                                 ▼
                                                         Phase 6 (ArgoCD App of Apps)
                                                                 │
                                                                 ▼
                                                         Phase 7 (Validacao DO)
```

- **Phases 0 → 1 → 2:** Sequenciais (1 depende de 0, 2 depende de 1)
- **Phase 5 (Preflight):** Pode iniciar em paralelo com Phase 1-2
- **Phase 3 (Orchestrator):** Depende de Phase 1 + 2
- **Phase 4 (Health Gates):** Depende de Phase 2 + 3
- **Phase 6 (App of Apps):** Depende de Phase 2 + 4
- **Phase 7 (Validacao):** Depende de TODAS as fases anteriores

---

## Phase 0: Commit Pending Fixes

**Objective:** Persistir todos os fixes B12-B18 da sessao Sprint 8 que estao em working tree mas nao commitados. Atualizar CLAUDE.md e Pulumi.dev.yaml com a realidade.

### T0.1 — Commit B12-B15 (Registry Auth Merge)

#### Objective
Commitar os fixes de host-scoped merge do `buildkit-registry-config` Secret que resolvem a oscilacao DOCR/Harbor.

#### Evidence
- B12-B15 documentados em `docs/runbooks/sprint8-l3-stabilization.md` (linhas 107-145)
- Retrospectiva: "arquivos modificados mas unstaged no destroy" (F6)
- Sem commit, proximo bootstrap reintroduz bugs B12-B15

#### Files to edit
```
operators/internal/controller/registry_auth_merge.go — NEW helper + 7 unit tests (ja criado, nao commitado)
operators/internal/controller/tenant_harbor.go — refatorado para usar mergeAuthsByHost
operators/internal/controller/tenant_build.go — refatorado para usar mergeAuthsByHost
operators/internal/controller/constants/labels.go — AnnotationHarborTokenExpiresAt novo
```

#### Deep file dependency analysis
- `registry_auth_merge.go`: Novo arquivo. Exporta `MergeAuthsByHost(existing, incoming)`. Sem downstream.
- `tenant_harbor.go`: `ensureHarborRegistrySecret()` agora chama `MergeAuthsByHost` em vez de delete-and-recreate. Downstream: `tenant_controller.go` reconcile loop.
- `tenant_build.go`: `ensureBuildkitRegistrySecret()` agora chama `MergeAuthsByHost`. Downstream: mesmo reconcile loop.
- `constants/labels.go`: Nova constante `AnnotationHarborTokenExpiresAt`. Downstream: `tenant_harbor.go`, `tenant_build.go`.

#### Deep Dives
- **Convergence proof**: `MergeAuthsByHost` eh idempotente sob interleaving — testes cobrem `[A,B,A,B]` e `[B,A,B,A]` orderings.
- **Type migration**: Secrets antigos com Type `kubernetes.io/dockerconfigjson` sao convertidos para `Opaque` com pre-flight delete.

#### Tasks
1. `git add` dos 4 arquivos modificados
2. `git commit` com mensagem descritiva referenciando B12-B15
3. Verificar testes passam: `cd operators && go test ./internal/controller/... -run MergeAuth`

#### TDD
```
RED:     TestMergeAuthsByHost_DOCROnly — ja existe, ja passa
RED:     TestMergeAuthsByHost_HarborOnly — ja existe, ja passa
RED:     TestMergeAuthsByHost_BothHosts — ja existe, ja passa
RED:     TestMergeAuthsByHost_Interleaving — convergence proof
GREEN:   Codigo ja implementado (apenas commit pendente)
REFACTOR: None expected
VERIFY:  cd operators && go test ./internal/controller/... -run MergeAuth -v
```

#### Acceptance Criteria
- [ ] 4 arquivos commitados na branch develop
- [ ] Testes MergeAuth passam
- [ ] `git diff` limpo para os 4 arquivos

#### DoD
- [ ] Commit criado com referencia B12-B15
- [ ] Zero arquivos unstaged dos fixes

---

### T0.2 — Commit B16 Fix (ClusterImagePolicy)

#### Objective
Persistir a ClusterImagePolicy `loopzero-images-warn` no IaC para que bootstraps futuros nao bloqueiem Deployments.

#### Evidence
- B16: `policy-controller deny-by-default blocks all tenant Deployments` (runbook L3 stabilization)
- Sem policy no IaC, TODA runtime bootstrap bloqueia 100% dos pods

#### Files to edit
```
infra/pulumi/src/components/runtime-policy-controller.ts — adicionar ClusterImagePolicy resource
infra/helm/admission-policies/ — avaliar se policy deve ser Helm values vs Pulumi resource
```

#### Deep file dependency analysis
- `runtime-policy-controller.ts`: Instala policy-controller Helm chart. DEVE tambem aplicar `ClusterImagePolicy/loopzero-images-warn` como K8s resource pos-install.
- `admission-policies/`: Diretorio Helm chart existente — pode conter a policy como template. Verificar se ja tem conteudo.

#### Deep Dives
- A policy usa `mode: warn` + `static.action: pass` como temporary fallback
- Cobre `registry.digitalocean.com/loopzero-system/**` e `harbor.dev.loopzero.example/**`
- Futuro Sprint 9: substituir por cosign keyless verification com Fulcio

#### Tasks
1. Verificar se `infra/helm/admission-policies/` ja tem templates para ClusterImagePolicy
2. Se sim: garantir que o Helm values inclui a policy warn-mode
3. Se nao: adicionar o resource no `runtime-policy-controller.ts`
4. Commit

#### TDD
```
RED:     Verificar via kubeconform que o manifest ClusterImagePolicy eh valido
GREEN:   Adicionar resource/template
REFACTOR: None expected
VERIFY:  task validate:manifests (se aplicavel ao CRD)
```

#### Acceptance Criteria
- [ ] ClusterImagePolicy presente no IaC (Helm ou Pulumi)
- [ ] Proximo `pulumi up` ou `helmfile sync` cria a policy automaticamente
- [ ] Nao requer `kubectl apply` manual pos-bootstrap

#### DoD
- [ ] Commit criado referenciando B16
- [ ] Policy validada contra schema

---

### T0.3 — Atualizar Pulumi.dev.yaml e CLAUDE.md

#### Objective
Persistir no codigo as configuracoes que foram alteradas manualmente durante L3 (node pools, Loki limits, tenant-controller concurrency) e corrigir status incorreto no CLAUDE.md.

#### Evidence
- CONFIG_DRIFT: 5 problemas documentados (node pools, Loki streams, Promtail labels, build autoscaler, runtime sizing)
- CLAUDE.md: claims "Audit mode active" mas policy-controller estava em deny-default (B16)

#### Files to edit
```
infra/pulumi/Pulumi.dev.yaml — runtimeMaxNodes: 8, buildMaxNodes: 5, Loki stream config
infra/helm/loki/values.yaml — max_global_streams_per_user: 100000
infra/helm/promtail/values.yaml — drop high-cardinality labels (step, pod)
CLAUDE.md — corrigir status Sprint 6 security posture
```

#### Deep file dependency analysis
- `Pulumi.dev.yaml`: Lido por `config/stack.ts:loadStackConfig()`. Mudancas em node counts afetam `infra.ts` cluster creation.
- `loki/values.yaml`: Usado pelo Helm release via `build-loki.ts`. Sobrevive a Helm upgrades.
- `promtail/values.yaml`: Usado pelo Helm release via `build-loki.ts` e `runtime-monitoring.ts`.
- `CLAUDE.md`: Lido por todos os agentes. Status incorreto causa decisoes erradas.

#### Deep Dives
- `runtimeMaxNodes: 8` + `--auto-scale --min 4 --max 12` para suportar L3 burst (50 vCPU)
- `buildMaxNodes: 5` para pre-warm antes de L3 (30 concurrent workflows)
- Loki `max_global_streams_per_user: 100000` (era 5000, L3 gera ~450 streams/ciclo)
- Promtail: drop `step` e `pod` labels reduz cardinality 10x

#### Tasks
1. Editar `Pulumi.dev.yaml` com novos node pool limits
2. Editar `infra/helm/loki/values.yaml` com stream limits
3. Editar `infra/helm/promtail/values.yaml` com pipeline stages drop
4. Editar `CLAUDE.md` corrigindo status do policy-controller
5. Commit

#### TDD
```
RED:     N/A (config changes, not code)
GREEN:   N/A
REFACTOR: N/A
VERIFY:  Validar YAML syntax: `python3 -c "import yaml; yaml.safe_load(open('infra/pulumi/Pulumi.dev.yaml'))"`
```

#### Acceptance Criteria
- [ ] `Pulumi.dev.yaml` reflete sizing operacional real
- [ ] Loki values suportam L3 sem 429
- [ ] Promtail labels de alta cardinalidade removidos
- [ ] CLAUDE.md status do Sprint 6 corrigido

#### DoD
- [ ] Commit criado
- [ ] YAML valido em todos os arquivos

---

## Phase 1: Refatorar Pulumi para L1-Only

**Objective:** Remover TODOS os recursos K8s do Pulumi state. Pulumi passa a gerenciar SOMENTE cloud resources (VPC, DOKS, PG, Spaces, Registry, CF Tunnel). Resultado: `pulumi destroy` completa em <5 min sem state surgery.

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
- **Outputs preservados**: `networkId`, `mgmtClusterName`, `mgmtKubeconfig`, `buildKubeconfig`, `runtimeKubeconfig`, `runtime2Kubeconfig`, `dbHost`, `dbUri`, `dbPassword`, `bucketName`, `bucketEndpoint`, `cdnEndpoint`, `registryEndpoint`, `cfTunnelId`, `cfKvNamespaceId`
- **Outputs removidos**: Todos os `*Ready` booleans (pertenciam ao bootstrap), `ingressEndpoint` (pertence ao Helmfile), wiring objects
- **Invariante**: `pulumi up` + `pulumi destroy` NAO toca em nenhum recurso K8s (zero providers `@pulumi/kubernetes`)

#### Tasks
1. Criar branch `feat/infra-layered-bootstrap`
2. Copiar `index.ts` atual para `index.ts.bak` (referencia)
3. Remover import e chamada de `bootstrapCluster`
4. Remover import e chamada de `exposePlatform`
5. Remover import de `@pulumi/kubernetes` (se nao usado por infra)
6. Atualizar exports para SOMENTE outputs L1
7. Testar: `cd infra/pulumi && npx pulumi preview --stack dev` (deve mostrar SOMENTE recursos DO/CF)

#### TDD
```
RED:     pulumi_preview_no_k8s_resources — preview nao deve conter nenhum resource kubernetes:*
GREEN:   Remover Phase 2 + 3 do index.ts
REFACTOR: Limpar imports nao utilizados
VERIFY:  npx pulumi preview --stack dev 2>&1 | grep -c 'kubernetes:' (expected: 0)
```

#### Acceptance Criteria
- [ ] `pulumi preview` mostra ZERO recursos `kubernetes:*`
- [ ] `pulumi preview` mostra ZERO recursos `helm:*`
- [ ] Outputs incluem kubeconfigs, DB connection, storage endpoints
- [ ] `@pulumi/kubernetes` pode ser removido do package.json (se nao usado em L1)

#### DoD
- [ ] Phase 2 + 3 removidos do index.ts
- [ ] Preview limpo (somente DO + CF resources)
- [ ] Nao quebra `pulumi up` existente (preview confirma)

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
Remover a dependencia `@pulumi/kubernetes` se nao for mais necessaria apos a extracao do Phase 2.

#### Evidence
- `package.json` lista `"@pulumi/kubernetes": "4.28.0"` como dependencia
- Se nenhum codigo L1 usa K8s provider, manter a dependencia eh dead weight e risco de usar acidentalmente

#### Files to edit
```
infra/pulumi/package.json — remover @pulumi/kubernetes se nao usado
infra/pulumi/src/components/bootstrap-shared.ts — verificar se funcoes compartilhadas usam K8s provider
infra/pulumi/src/components/infra.ts — verificar (nao deve usar K8s)
```

#### Deep file dependency analysis
- `package.json`: Define dependencias do projeto Pulumi. Remover K8s provider impede uso acidental.
- `bootstrap-shared.ts`: Exporta `HELM_VERSIONS`, `parseKubeconfig`, etc. Funcoes de parsing nao precisam do provider K8s. Mas `createPlatformNamespace`, `installCertManager`, etc. usam — essas funcoes migram para Helmfile.
- `infra.ts`: Usa SOMENTE providers DO/CF. NAO usa K8s.

#### Deep Dives
- **Shared code migration**: `HELM_VERSIONS` e `parseKubeconfig` sao uteis para Helmfile e CLI. Extrair para `infra/shared/versions.ts` ou duplicar em Helmfile config.
- **Se alguma funcao L1 ainda precisar K8s** (e.g., adopt-or-create via kubectl): manter a dependencia mas isolar com lint rule

#### Tasks
1. `grep -r "pulumi/kubernetes" infra/pulumi/src/` para encontrar todos os usos
2. Se zero usos em L1: remover de package.json + `npm install`
3. Se alguns usos: avaliar se podem ser substituidos (e.g., `pulumi command` em vez de K8s provider)
4. Mover `HELM_VERSIONS` para local acessivel pelo Helmfile

#### TDD
```
RED:     N/A (dependency removal)
GREEN:   npm install sem erros
REFACTOR: None
VERIFY:  cd infra/pulumi && npm install && npx pulumi preview --stack dev
```

#### Acceptance Criteria
- [ ] `@pulumi/kubernetes` removido do package.json (ou justificativa documentada para manter)
- [ ] `npm install` sem erros
- [ ] `pulumi preview` funciona sem K8s provider

#### DoD
- [ ] Dependencia removida ou justificada
- [ ] Build limpo

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
| 1 | harbor | cert-manager, traefik | Registry com TLS |
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
| 1 | cluster-issuer | cert-manager |
| 1 | rbac | (none) |

**ClusterImagePolicy** (resolve B16):
- Incluida como release `admission-policies` (Helm chart em `infra/helm/admission-policies/`)
- `mode: warn`, `static.action: pass` para audit mode
- Criada automaticamente — zero `kubectl apply` manual

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

## Phase 3: Orchestrator CLI (`loopzero platform provision`)

**Objective:** Criar o comando `loopzero platform provision` que orquestra L1 (Pulumi) + L2 (Helmfile) + wiring + smoke test como um fluxo unico end-to-end.

### T3.1 — Definir interface e command registration

#### Objective
Criar o comando `loopzero platform provision` no CLI com flags para provider, environment, domain, e kubeconfig outputs.

#### Evidence
- `loopzero platform install` ja existe (self-hosting Helm-based), mas nao cobre provisioning de infra
- `loopzero platform provision` (documentado como P3 gap no CLI) eh para LoopZero Cloud + self-hosting com infra managed

#### Files to edit
```
cli/src/commands/platform.ts — adicionar platformProvision function
cli/src/register/platform.ts — registrar comando `provision`
cli/src/installer/types.ts — adicionar ProvisionConfig type
```

#### Deep file dependency analysis
- `platform.ts`: Ja tem `platformInstall`, `platformStatus`, `platformUpgrade`, `platformUninstall`. Adicionar `platformProvision`.
- `register/platform.ts`: Commander.js registration. Adicionar `.command('provision')` com options.
- `types.ts`: Adicionar `ProvisionConfig` com campos para provider, region, environment, domain.

#### Deep Dives

**Interface:**
```
loopzero platform provision \
  --provider digitalocean \
  --environment dev \
  --domain dev.loopzero.example \
  --config ./platform-config.yaml \
  [--skip-infra]     # skip Pulumi, use existing clusters
  [--skip-bootstrap] # skip Helmfile, use existing releases
  [--dry-run]        # preview only
```

**ProvisionConfig (YAML):**
```yaml
provider: digitalocean
environment: dev
domain: dev.loopzero.example
region: sfo3
clusters:
  mgmt: { nodeClass: medium, minNodes: 3, maxNodes: 5 }
  build: { nodeClass: medium, minNodes: 2, maxNodes: 5 }
  runtime: { nodeClass: small, minNodes: 2, maxNodes: 8 }
database: { nodeClass: medium, nodeCount: 3 }
cloudflare:
  accountId: <from-env>
  zoneId: <from-env>
secrets:
  # Can reference env vars: ${DO_TOKEN}
  digitaloceanToken: ${DO_TOKEN}
  cfApiToken: ${CF_API_TOKEN}
```

#### Tasks
1. Definir `ProvisionConfig` type em `types.ts`
2. Implementar `platformProvision` skeleton em `platform.ts`
3. Registrar comando em `register/platform.ts`
4. Implementar 6 phases: preflight → infra → bootstrap-build → bootstrap-mgmt → bootstrap-runtime → smoke
5. Adicionar testes unitarios

#### TDD
```
RED:     test_provision_command_exists — `loopzero platform provision --help` retorna exit 0
RED:     test_provision_requires_config — sem --config retorna exit 1
RED:     test_provision_dry_run — --dry-run nao executa nada, apenas printa plan
GREEN:   Implementar command registration + skeleton
REFACTOR: Extrair phase execution para funcoes separadas
VERIFY:  cd cli && npm test -- --grep "platform provision"
```

#### Acceptance Criteria
- [ ] `loopzero platform provision --help` funciona
- [ ] `--config` obrigatorio
- [ ] `--dry-run` mostra plan sem executar
- [ ] `--skip-infra` e `--skip-bootstrap` funcionam
- [ ] Exit codes corretos (0 success, 1 usage, 3 provision failure)

#### DoD
- [ ] Comando registrado e funcional
- [ ] Testes unitarios passam
- [ ] `--help` documenta todas as flags

---

### T3.2 — Implementar orquestracao das 6 fases

#### Objective
Implementar a logica que executa Pulumi (L1), Helmfile (L2), wiring, e smoke test em sequencia com progress reporting.

#### Evidence
- `platform.ts:platformInstall` ja tem o pattern de 10 steps com progress reporting — reutilizar
- Cada fase deve ter timeout, retry, e error reporting claro

#### Files to edit
```
cli/src/installer/provision.ts — NEW: provisioning orchestrator
cli/src/installer/pulumi-runner.ts — NEW: wrapper para pulumi up/destroy
cli/src/installer/helmfile-runner.ts — NEW: wrapper para helmfile sync/destroy
```

#### Deep file dependency analysis
- `provision.ts`: Novo orchestrator. Chama `pulumi-runner.ts` → `helmfile-runner.ts` → wiring scripts → smoke test.
- `pulumi-runner.ts`: Executa `pulumi up --stack <env> --yes --skip-preview` com output streaming.
- `helmfile-runner.ts`: Executa `helmfile -f <file> -e <env> sync` com kubeconfig injection.

#### Deep Dives

**6-phase flow:**
```
Phase 0: Preflight   → validatePrereqs() + validateConfig()
Phase 1: Infra       → pulumiUp(config) → outputs (kubeconfigs, endpoints)
Phase 2: Bootstrap   → helmfileSync("build", kubeconfig_build)
                     → helmfileSync("mgmt", kubeconfig_mgmt, {dbHost, ...})
                     → helmfileSync("runtime", kubeconfig_runtime) × N cells
Phase 3: Wire        → wireScript(kubeconfigs, outputs)
Phase 4: DNS         → external-dns ou manual instructions
Phase 5: Smoke test  → createTestTenant() → deployTestApp() → verifyHTTP200()
```

**Output streaming**: Cada subprocess (pulumi, helmfile) tem stdout/stderr streamed para o CLI com prefixo de fase.

**Error recovery**: Se Phase 2 falha, `--skip-infra` permite re-run sem re-provisionar L1.

#### Tasks
1. Criar `provision.ts` com 6-phase orchestrator
2. Criar `pulumi-runner.ts` com `pulumiUp()`, `pulumiDestroy()`, `pulumiOutput()`
3. Criar `helmfile-runner.ts` com `helmfileSync()`, `helmfileDestroy()`, `helmfileDiff()`
4. Integrar com `platformProvision` em `platform.ts`
5. Adicionar testes

#### TDD
```
RED:     test_provision_phase_ordering — phases executam em ordem correta
RED:     test_provision_skip_infra — --skip-infra pula Phase 1
RED:     test_provision_error_stops — falha em Phase 1 nao executa Phase 2
GREEN:   Implementar orchestrator
REFACTOR: Extrair phase interface para reuso
VERIFY:  cd cli && npm test -- --grep "provision"
```

#### Acceptance Criteria
- [ ] 6 fases executam em sequencia
- [ ] `--skip-infra` e `--skip-bootstrap` funcionam
- [ ] Erro em qualquer fase para a execucao com mensagem clara
- [ ] Progress reporting com duracao por fase
- [ ] Outputs da L1 passados como env vars para L2

#### DoD
- [ ] Orchestrator funcional
- [ ] Testes passam
- [ ] Error paths cobertos

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
infra/argocd-bootstrap/apps/cert-manager.yaml — NEW: example
infra/argocd-bootstrap/apps/traefik.yaml — NEW: example
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

**Procedimento:**
1. `pulumi destroy --stack dev --yes` (deve completar em <5 min)
2. `loopzero platform provision --config platform-dev.yaml` (deve completar sem manual steps)
3. Verificar health: `loopzero platform status` → all green
4. Executar L3: `LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l3-realistic go test ./infra/tests/load/ -run L3Realistic -v -timeout 4h`
5. Resultado: >= 95/100 tenants com app respondendo HTTP 200

**Metricas de sucesso:**
- Destroy time: <5 min (era 60+ min com state surgery)
- Provision time: <30 min (era 90+ min com manual steps)
- Manual interventions: 0 (era 76)
- L3 success rate: >= 95% (era 0%)

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

## Coverage Matrix

| # | Problema (dos 44) | Task(s) | Resolucao |
|---|---|---|---|
| 1 | DAY0: ClusterImagePolicy nao criada (B16) | T0.2, T2.3 | Policy incluida como Helmfile release |
| 2 | DAY0: ArgoCD repo-server cache stale (B17) | T4.1 | Health check detecta + restart automatico |
| 3 | DAY0: Runtime pre-warm manual | T0.3, T2.3 | Node pool sizes persistidos em config |
| 4 | DAY0: Promtail ausente no runtime-2 | T2.3 | Helmfile runtime reutilizado para todas as cells |
| 5 | DAY0: Promtail ausente no mgmt | T2.1 | Promtail adicionado ao mgmt helmfile |
| 6 | DAY0: CLAUDE.md status stale | T0.3 | Atualizado |
| 7 | DAY0: B12-B15 nao commitados | T0.1 | Commitados |
| 8 | DAY0: gitops repo archived nao detectado | T2.4 | Wiring script verifica e unarchive |
| 9 | DAY0: gitea_pusher error wrapping | — | Nao coberto (bug de codigo, nao infra) |
| 10 | ORDERING: cert-manager antes de Traefik | T2.1-T2.3 | `needs:` explicito no Helmfile |
| 11 | ORDERING: ArgoCD SA sem RBAC (B7) | T2.4 | Wiring script cria RBAC correto |
| 12 | ORDERING: caData raw PEM (B6) | T2.4 | Script de encoding correto |
| 13 | ORDERING: tenant-controller MaxConcurrent (B3) | T0.3 | Persistido em Pulumi.dev.yaml |
| 14 | ORDERING: policy-controller sem policy (B16) | T2.3 | Helmfile release |
| 15 | ORDERING: monorepo workspace build (B8-B9) | — | Ja fixado em codigo (commit pendente) |
| 16 | ORDERING: L3 harness hardcoded (B10) | — | Ja fixado em codigo |
| 17 | ORDERING: source git push race (B-C) | — | Low severity, aceito |
| 18 | CAPACITY: Loki 5000 streams (B5) | T0.3 | Values persistidos |
| 19 | CAPACITY: Runtime sizing L3 (B18) | T0.3 | Node pool max persistido |
| 20 | CAPACITY: Build autoscaler lento (B-D) | T0.3 | Build max nodes persistido |
| 21 | CAPACITY: DOCR/Docker Hub QPS (B-E) | — | Self-recovers, aceito |
| 22 | CAPACITY: Gitea concurrent clones | — | Nao testado (L3 coverage) |
| 23 | CAPACITY: MinIO distributed migration | — | Fora de escopo (CHG-002c) |
| 24 | CAPACITY: Loki Promtail NotReady | T0.3 | Stream limits corrigidos |
| 25 | CONFIG_DRIFT: Node pool sizes | T0.3 | Persistido em Pulumi.dev.yaml |
| 26 | CONFIG_DRIFT: Loki stream limits | T0.3 | Persistido em values.yaml |
| 27 | CONFIG_DRIFT: Build autoscaler | T0.3 | Persistido em Pulumi.dev.yaml |
| 28 | CONFIG_DRIFT: Promtail labels | T0.3 | Persistido em values.yaml |
| 29 | CONFIG_DRIFT: runtime-2 Promtail | T2.3 | Helmfile runtime reutilizado |
| 30 | STATE_DRIFT: cosign placeholder (B1) | T2.2 | Keypair gerado automaticamente |
| 31 | STATE_DRIFT: buildkit Secret oscillation (B12) | T0.1 | Fix commitado + Helmfile nao cria conflito |
| 32 | STATE_DRIFT: CDN orphan collision | T1.1 | CDN permanece no Pulumi L1 (nao muda) |
| 33 | STATE_DRIFT: Pulumi destroy 404 refresh | T1.1, T1.2 | Zero K8s no state → destroy limpo |
| 34 | TEARDOWN: CRDs com finalizers | T1.1 | Zero CRDs no Pulumi state |
| 35 | TEARDOWN: K8s provider retry infinito | T1.1, T1.3 | Zero K8s provider no Pulumi |
| 36 | TEARDOWN: Destroy 3 rounds + surgery | T1.1, T1.2 | Destroy em <5 min |
| 37 | SECRET: GITEA_TOKEN trailing newline (B4) | T2.4 | Wiring script usa printf sem newline |
| 38 | SECRET: cosign placeholder (B1) | T2.2 | Auto-generated keypair |
| 39 | IDEMPOTENCY: Harbor robot check (B13) | T0.1 | Fix commitado |
| 40 | IDEMPOTENCY: DeleteRobot endpoint (B14) | T0.1 | Fix commitado |
| 41 | IDEMPOTENCY: repo-server cache (B17) | T4.1 | Health check + restart |
| 42 | SELF_HOSTING: adopt-or-create hardcoded DO | T3.1 | Provider abstraction mantida |
| 43 | SELF_HOSTING: loopzero platform install != provision | T3.1 | Dois comandos claros |
| 44 | GOVERNANCE: Zero ADRs | ADRs D1-D8 | 8 ADRs neste plano |

**Coverage: 38/44 problemas cobertos (86%).** Os 6 nao cobertos sao: bugs de codigo ja fixados (B8-B10, gitea_pusher), low-severity aceitos (QPS limits, git push race), ou fora de escopo (MinIO migration).

## Global Definition of Done

- [ ] All phases (0-7) completed
- [ ] `pulumi destroy` < 5 min, zero state surgery
- [ ] `helmfile sync` instala 26 releases com ordering correto
- [ ] `loopzero platform provision` funcional end-to-end
- [ ] Zero intervencoes manuais no bootstrap
- [ ] Health checks passam apos cada fase
- [ ] Preflight checks bloqueiam provisioning com erros claros
- [ ] ArgoCD App of Apps gerencia Day-2 drift detection
- [ ] L3 100-tenant deploy >= 95% success rate no DigitalOcean
- [ ] 38/44 problemas da retrospectiva Sprint 8 eliminados
- [ ] CHANGELOG.md atualizado com todas as mudancas
- [ ] Todos os testes passam (CLI + operators + API)
- [ ] `helmfile lint` passa para os 3 helmfiles
- [ ] `task validate:manifests` passa
