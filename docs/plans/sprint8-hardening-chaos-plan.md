# Plan: Sprint 8 — Phase 2a: Destravar Escala 50k Apps

> **Version 3.2** — O objetivo do LoopZero e suportar 50k apps em producao via cell architecture (5k apps/cell x 10 cells). O bloqueador hoje e Phase 2a (ADR-0010): 7 prerequisitos obrigatorios antes de ativar cell-2. Sprint 8 ataca TODOS os 7 deliverables de Phase 2a, executa os chaos tests como Gate 2a exige, e estabiliza L3 (100 tenants >= 95/100) que e o bloqueador #1. Ao final, Phase 2a deve estar completa ou com gap documentado e path claro, desbloqueando Phase 2b (cell-2 activation) na Sprint 9.
>
> **v3.2 changelog (2026-04-27):** Sprint 8 v3.1 foi formalmente revisado em **MEET-2026-04-27-060** (9 reviewers, T3 governance) e RECEBEU APROVACAO COM CONDICOES via **Option B**: split em **CHG-001 (Phase -1 Bootstrap, este)** + **CHG-002 (Phase 0-6 Hardening, futuro novo meeting)**. Big-bang Option A foi rejeitada unanimemente como epic-CHG anti-pattern. Decisao registrada em **ADR-0015 (Pre-bootstrap Adopt-or-Create Policy)**. Mandatory operational hardening incorporado:
> - Secrets/state em `${HOME}/.loopzero/backups/` chmod 600 (NAO `/tmp` — Security veto absoluto)
> - Per-cluster kubeconfigs em `${HOME}/.kube/loopzero-*.yaml` chmod 600 (NAO aggregar em `~/.kube/config` — Security veto absoluto)
> - Pre-flight `doctl account get` quota check (>= 12 droplet headroom — SRE veto absoluto)
> - `pulumi up` foreground only, operator presente (NAO background — SRE veto absoluto)
> - Runbook `docs/runbooks/sprint8-bootstrap.md` publicado ANTES de T-1.3 (SRE veto absoluto)
> - Registry name fix via `lookupExistingRegistry` em `core/adopt-or-create.ts` (NAO renomear DOCR vivo — Security + Production Design veto absoluto)
>
> Validacao agora referencia matriz formal de 16 itens em MEET-2026-04-27-060 §Stage 2. **CHG-001 esta restrito a 5 arquivos** (este plano + ADR-0015 + runbook + adopt-or-create.ts + infra.ts). Phase 0-6 deferred para CHG-002.
>
> **v3.1 (substituido):** Corrigiu entendimento da esteira de infra apos leitura de `infra/pulumi/src/index.ts`. **`pulumi up` e o unico comando de bootstrap do LoopZero Cloud** — provisiona infra DO (4 DOKS) + CF + bootstrapa todos os Helm releases + DNS. `loopzero platform install` e para customers self-hosting, nao para LoopZero Cloud team.
>
> **v3.0 (substituido):** Tinha adicionado Phase -1 com `loopzero platform install` separado — incorreto.

## Context

### O objetivo: 50k apps

O LoopZero e desenhado para escalar a 50k apps via cell architecture (ADR-0008):
- **1 cell = 1 workload cluster** com ~5k apps (dentro do limite K8s de 150k pods)
- **10 cells** = 50k apps, escalamento linear
- Control plane centralizado: API + PG + Gitea + Argo Workflows + ArgoCD (central ate Phase 3)
- Routing via PostgreSQL (`tenants.cell_id`), nao CF KV (ADR-0010 veto)

### O que bloqueia: Phase 2a (ADR-0010)

ADR-0010 define 7 prerequisitos obrigatorios antes de provisionar cell-2:

| # | Deliverable | Status Atual | Gap |
|---|---|---|---|
| 1 | **L3 10x consecutivo >= 95/100** | Melhor: 75/100 | **BLOQUEADOR PRINCIPAL** — 20 pontos abaixo |
| 2 | **PG max_connections >= 100** | `dbNodeClass: small` (db-s-1vcpu-2gb) | Config change em Pulumi |
| 3 | **Gitea HA** (2 replicas + shared storage + 200 concurrent clones) | Parcial: Redis done, replicas=1 | Requer NFS/S3 git storage + multi-replica |
| 4 | **Harbor HA** (2+ core + Redis HA + S3 shared) | core=1, registry=1, Redis interno | Config changes + Redis HA |
| 5 | **MinIO distributed** | Plano referenciado mas NAO encontrado | Requer plano + implementacao |
| 6 | **mgmt cluster memory < 50%** | 74% idle | Scale nodes: small→medium ou +nodes |
| 7 | **tenant-health Grafana dashboard** | Nao existe | Novo dashboard |

**Gate 2a Exit:** Todos os 7 + chaos test (kill each shared service, recovery <2min) + 5 consecutive L3 >= 98/100.

### O que existe e nunca foi executado

**Load tests (L1-L5):** Harness real completo em `infra/tests/load/`. L1-L3 rodados, L4/L5 nunca.
**Chaos tests (9 cenarios):** 388 LOC + 9 manifests Chaos Mesh em `infra/tests/chaos/`. Nunca executados contra DO.
**Capacity tests (10 suites):** 2616 LOC em `infra/tests/capacity/`. Validados.

### Infraestrutura atual (Pulumi.dev.yaml)

```
mgmtNodeClass: small     ← precisa medium (memoria)
mgmtMinNodes: 3          ← fixo (sem auto-scale)
mgmtMaxNodes: 3          ← precisa 5 (headroom)
dbNodeClass: small        ← precisa medium (connections)
runtimeNodeClass: small   ← OK per-cell
buildNodeClass: medium    ← OK
Harbor core: 1 replica    ← precisa >= 2
Gitea: 1 replica          ← precisa >= 2
```

### Estado REAL verificado em 2026-04-27 (descobertas)

```
$ doctl kubernetes cluster list   → VAZIO (zero DOKS clusters provisionados)
$ doctl databases list             → VAZIO (zero PG managed)
$ doctl registry get               → loopzero-system (sfo3) — registry preservado
$ doctl vpcs list                  → loopzero-infra-dev-sfo3-net (10.20.0.0/16) + loopzero-infra-homolog-nyc3-net (10.30.0.0/16)
$ doctl compute droplet list       → loopzero-code-vm + loopzero-e2e-runner (regression)
$ DO Spaces buckets (sfo3)        → loopzero-builds, loopzero-static-assets, loopzero-homolog-pulumi-state
                                     (NAO existe loopzero-dev-pulumi-state — CLAUDE.md tinha nome stale)
$ pulumi backend                   → s3://loopzero-homolog-pulumi-state existe, contem stack `homolog`
$ pulumi stack homolog             → 41 outputs (alegam infra ativa) MAS apenas 7 resources managed
                                     (4 providers + VPC homolog + registry + Stack root)
                                     STATE ZUMBI: outputs apontam para clusters/DB/Helm
                                     que foram destruidos no DO mas nao limpos do state.
```

**Conclusao:** o ambiente DO foi destruido (ou nunca materializado em plenitude). Pulumi state esta corrompido/zumbi. Estrategia: **criar stack `dev` novo em sfo3** (bucket `loopzero-dev-pulumi-state` precisa ser criado primeiro), usando `Pulumi.dev.yaml` ja presente no repo. **Para executar Phase 0-6, e obrigatorio passar antes pela Phase -1 (Bootstrap)** que provisiona TUDO via `pulumi up` (esteira unica do LoopZero Cloud).

## Objective

**Done = Phase 2a Gate Exit atingido, desbloqueando cell-2 (Phase 2b) na Sprint 9.**

Metas mensuraveis:
1. L3 >= 95/100 em 5 execucoes consecutivas (Gate #1 de ADR-0010)
2. PG bumped para `dbNodeClass: medium` (max_connections >= 100)
3. Gitea HA: 2 replicas + shared storage + 200 concurrent clones validado
4. Harbor HA: 2+ core replicas + Redis externo (DO managed)
5. MinIO distributed: plano criado + implementado
6. mgmt cluster: `mgmtNodeClass: medium`, memory idle < 50%
7. tenant-health Grafana dashboard criado e funcional
8. 9/9 chaos tests passam contra DO (shared service kill + recovery <2min)
9. L4 (500 tenants) executado com resultados documentados
10. Observability gaps fechados (alerts orfas, runbooks, SLO formal)

## ADRs

### D1 — L3 stability e o bloqueador critico (nao chaos, nao observability)
- **Decision:** L3 >= 95/100 e a prioridade #1 da sprint. Todo o resto e secundario.
- **Rationale:** ADR-0010 Gate #1. Sem L3 estavel, os outros 6 deliverables sao irrelevantes — nao passamos no gate. O gap e 20+ pontos (75→95), indicando problemas sistemicos que precisam de diagnostico + fix iterativo.
- **Consequences:** Se L3 nao estabilizar, Sprint 8 falha. Outros deliverables continuam em paralelo mas nao compensam L3.

### D2 — Infrastructure scale-up via Pulumi config (nao refactoring)
- **Decision:** PG bump, mgmt memory, Harbor HA, e Gitea replicas sao config changes em Pulumi, nao redesign.
- **Rationale:** Pulumi.dev.yaml ja suporta todos os parametros necessarios. `dbNodeClass: medium` e um valor em YAML. `mgmtNodeClass: medium` idem. Harbor `core.replicas: 2` e Helm value. Nao ha codigo novo — apenas escalar o que ja existe.
- **Consequences:** Custo mensal aumenta (~$100-200/mo adicional para nodes medium + PG medium). Tradeoff aceito: investimento em infraestrutura para desbloquear 50k.

### D3 — Gitea HA via NFS (nao S3 git storage)
- **Decision:** Usar DigitalOcean Volumes (NFS) como shared storage para Gitea replicas, nao S3 para git data.
- **Rationale:** Gitea Community Edition nao suporta S3 como backend para repositorios git (apenas para LFS, attachments, avatars — ja configurado). Git operations requerem filesystem POSIX. DO Volumes com ReadWriteMany (via NFS) e a opcao viavel. Alternativa: Gitea Cluster (Enterprise only, rejeitado por custo).
- **Consequences:** Requer DO Volume com NFS access mode. Performance de git clone depende de latencia NFS. Validar com load test de 200 concurrent clones.

### D4 — MinIO distributed via erasure coding (4 nodes)
- **Decision:** Migrar MinIO de single-instance para distributed mode com 4 drives (2 nodes x 2 drives) usando erasure coding.
- **Rationale:** MinIO single-instance e SPOF para build artifacts. Com 100+ tenants, perda de MinIO = perda de todos os bundles. Erasure coding (EC:2) sobrevive a perda de 2 drives. 4 drives e o minimo para distributed mode.
- **Consequences:** Requer 4 PVCs (DO Volumes). Migration path: criar novo cluster MinIO distributed, copiar dados, trocar endpoint. Downtime aceitavel em dev (~30min).

### D5 — Chaos tests servem ao Gate 2a (nao sao objetivo isolado)
- **Decision:** Chaos tests sao executados como parte do Gate 2a exit, nao como exercicio academico.
- **Rationale:** Gate 2a exige: "chaos test proving each shared service can be killed independently with recovery <2min". Os 9 cenarios existentes cobrem exatamente isso (API, controller, PG, ArgoCD, Gitea, build node, simultaneous crash). Executar e validar — nao escrever mais.
- **Consequences:** Se algum cenario falha, o fix e prioridade (bloqueia Gate 2a).

### D6 — L4 (500 tenants) como validation, nao exploration
- **Decision:** L4 e executado como validacao de capacidade da celula unica, informando dimensionamento de cell-2.
- **Rationale:** 5k apps/cell e o target por ADR-0008. L4 (500 tenants) valida ~10% da capacidade target. Se L4 falha, o design de 5k/cell precisa ser revisado antes de provisionar cell-2. L4 nao e gate formal, mas resultado informa dimensionamento.
- **Consequences:** Se L4 < 90% success: revisar capacity assumptions antes de Phase 2b.

### D7 — SLO formal como contrato de escala
- **Decision:** Documentar SLOs em `docs/slo.md` focados em metricas de escala, nao apenas uptime.
- **Rationale:** 50k apps requer SLOs de escala: deploy throughput, build queue latency, sync convergence time. SLOs de uptime sao necessarios mas insuficientes para validar que o sistema escala.
- **Consequences:** SLOs de escala informam decisoes de cell sizing e Gate 2b.

### D8 — Observability hardening suporta operacao multi-cell
- **Decision:** Resolver alerts orfas, criar runbooks, e deploy Promtail no build cluster como pre-requisito para operar 2+ cells.
- **Rationale:** Multi-cell sem observability cross-cluster e operacionalmente inviavel. Gate 2b exige: "per-cell Prometheus with remote-write" e "cell:degraded Alertmanager aggregation". Sprint 8 prepara a base.
- **Consequences:** Observability work e investimento para multi-cell, nao overhead de compliance.

## Esteira de Infra (Como o Pulumi Faz Tudo)

Antes de descrever as phases, e fundamental entender que **`pulumi up --stack dev` e o unico comando de bootstrap**. Ele e composto internamente por 3 phases (em `infra/pulumi/src/index.ts`):

### Pulumi Phase 1 — Base Infrastructure (`createInfra` em `infra.ts`)

Cria recursos primitivos no DigitalOcean:

| Recurso | Nome (dev) | Detalhes |
|---|---|---|
| VPC | `loopzero-infra-dev-sfo3-net` | 10.20.0.0/16, sfo3 |
| DOKS mgmt | `loopzero-infra-dev-sfo3-mgmt` | `mgmtNodeClass`, min/max nodes do Pulumi.dev.yaml |
| DOKS build | `loopzero-infra-dev-sfo3-build` | `buildNodeClass` (medium), 2-5 nodes |
| DOKS runtime-1 (cell-1) | `loopzero-infra-dev-sfo3-runtime` | `runtimeNodeClass`, 2-3 nodes |
| **DOKS runtime-2 (cell-2)** | `loopzero-infra-dev-sfo3-runtime-2` | **JA E PROVISIONADO** — confirmado em `infra.ts:160-175`. Cell-2 nao precisa de "novo cluster" em Phase 2b, apenas ativacao via DB. |
| PG managed | `loopzero-infra-dev-sfo3-db` | `dbNodeClass`, com `trustedK8sClusters` para os 4 DOKS |
| DO Spaces (S3) | `loopzero-static-assets`, `loopzero-builds`, etc | Buckets de artifacts |
| DO Container Registry | `loopzero-system` (sfo3) | Ja preservado |

### Pulumi Phase 1.5 — Cloudflare Resources

Quando `cfAccountId + cfApiToken + cfZoneId` estao configurados (sao em dev):
- **CF Tunnel:** trafego runtime via edge, zero IPs publicos
- **CF Worker + KV:** static frontend delivery (Sprint 10)
- **adopt-or-create logic** em `core/adopt-or-create.ts` permite re-`pulumi up` apos destroy parcial sem conflitos

### Pulumi Phase 2 — Bootstrap Clusters (`bootstrapCluster` em `bootstrap.ts`)

Aplica Helm charts e CRDs em cada cluster via Pulumi K8s provider:

**Mgmt cluster** (em `mgmt-bootstrap.ts`, ordem):
```
namespace -> cert-manager -> ArgoCD -> Traefik -> Gitea -> MinIO ->
  Prometheus -> Grafana -> LoopZero API -> TenantController -> TLS
```

**Build cluster** (em `build-bootstrap.ts`):
```
Argo Workflows -> CWTs -> Loki + Promtail
```

**Runtime cluster (cell-1)** (em `runtime-bootstrap.ts`):
```
namespace -> cert-manager -> Traefik -> ESO -> KEDA -> Promtail -> Policy Controller
```

**Runtime cluster 2 (cell-2)** (em `runtime-bootstrap.ts:bootstrapRuntimeCluster2`):
```
mesma stack do runtime-1 — provisionada mas pode ficar em "stand-by" via tenants.cell_id
```

### Pulumi Phase 3 — Expose Platform (`exposePlatform` em `expose.ts`)

- DNS records do mgmt (CF DNS): `api.dev.loopzero.example`, `git.dev.loopzero.example`, `grafana.dev.loopzero.example`
- DNS runtime: `*.dev.loopzero.example` (wildcard CF Tunnel)

### Implicacoes para Sprint 8

1. **Nao existe "loopzero platform install" no fluxo LoopZero Cloud.** O comando `loopzero platform install` (em `cli/src/commands/platform.ts`) e para customers self-hosting (ver `cli/src/installer/profiles/example-values.yaml` cabecalho "LoopZero Platform — Customer Values"). Para nos, **o equivalente e `pulumi up`**.

2. **Cell-2 ja e provisionado fisicamente pelo `pulumi up`** — `runtimeCluster2` em `infra.ts:164` cria o cluster. O que falta para "ativar cell-2" (Phase 2b) e roteamento via PG: `UPDATE tenants SET cell_id = 2 WHERE ...` + ClusterCredential apontando para o cluster.

3. **Idempotencia by design:** `adopt-or-create` em `core/adopt-or-create.ts` detecta orphans (CF tunnel, KV, DNS, VPC) e gera import IDs. `pulumi destroy && pulumi up` funciona como bootstrap inicial OU como recuperacao apos partial-destroy.

4. **Mudancas Sprint 8 (T0.1, T0.2, T2.x) sao TODAS edits em `Pulumi.dev.yaml` ou Helm values consumidos por `bootstrapCluster`.** Nenhuma nova IaC e necessaria — a esteira existente suporta todos os ajustes.

## Dependency Graph

```
Phase -1 (Bootstrap) ──▶ Phase 0 (Infra Scale-Up) ──▶ Phase 1 (L3 Stabilization) ──▶ Phase 3 (L4)
   │ pulumi up                  │                            │                              │
   │ loopzero platform install      │                            │                              ▼
   │                            ▼                            ▼                      Phase 4 (Gate 2a Exit)
   ▼                    Phase 2 (Shared Service HA)
Phase 0 prereqs           ├─ Gitea HA
                          ├─ Harbor HA
                          ├─ MinIO Distributed
                          └─ mgmt memory
                                                     ▼
                                              Phase 5 (Chaos Gate)
                                                     │
                                                     ▼
                                              Phase 4 (Gate 2a Exit)

Phase 6 (Observability) ── paralelo com Phases 0-5 ──
```

- **Phase -1** (Bootstrap) e **PRE-REQUISITO ABSOLUTO**: provisiona infra DO + instala plataforma. Sem isso, todas as outras phases sao impossiveis (nao ha cluster onde aplicar).
- **Phase 0** (infra config) desbloqueia **tudo** (L3 precisa de PG medium para nao saturar)
- **Phase 1** (L3) e o **bloqueador critico** — sem L3 >= 95/100, Gate 2a falha
- **Phase 2** (HA) roda em paralelo com Phase 1 — Gitea/Harbor/MinIO nao dependem de L3
- **Phase 3** (L4) depende de Phase 1 (L3 estavel primeiro)
- **Phase 5** (chaos) depende de Phases 0+2 (shared services HA antes de testar kill/recovery)
- **Phase 4** (Gate 2a Exit) e o checkpoint final — requer Phases 1+2+5 completas
- **Phase 6** (observability) e paralela e suporta todos os outros phases

---

## Phase -1: Bootstrap (Provisionar do Zero via Pulumi)

**Objective:** Materializar todo o ambiente DO via `pulumi up --stack dev`. Esse unico comando faz **infra base + CF + bootstrap Helm de todos os clusters + DNS** (vide secao "Esteira de Infra"). Pre-requisito absoluto — sem isso, Phases 0-6 nao tem onde rodar.

**Justificativa:** descoberta 2026-04-27: `doctl kubernetes cluster list` retorna vazio, `doctl databases list` vazio. CLAUDE.md alegacao "3 DOKS rodando" e stale. Necessario re-bootstrap completo antes de aplicar mudancas Sprint 8.

**Important:** Esta phase **nao usa `loopzero platform install`** — esse comando e para customers self-hosting LoopZero na infra deles. Para o time LoopZero Cloud (este repo), `pulumi up` e o equivalente e ja faz tudo internamente via `bootstrapCluster()` em `infra/pulumi/src/index.ts:100`.

### T-1.0a — Abrir `/change` Sprint 8 (incluir infra.ts + Helm values + Pulumi.dev.yaml no escopo)

#### Objective
Abrir formalmente o change approval para Sprint 8 cobrindo todos os arquivos T2 que serao tocados durante Phase -1 a Phase 6, incluindo o fix critico em `infra/pulumi/src/components/infra.ts:282` (registry name `loopzero-infra-registry` → `loopzero-system`).

#### Evidence
Tentativa de edit em `infra.ts:282` foi bloqueada pelo `pre-tool-use` hook em `.claude/hooks/governance-gate.py`. Mensagem: "Aprovacao: Implementação do plano docs/plans/preview-environments-plan.md (Sprint 5 — Preview Environments)". O escopo atual nao inclui `infra/pulumi/src/components/infra.ts`. Sprint 8 e novo, portanto novo `/change` necessario.

#### Files to edit
```
NENHUM — apenas processo de aprovacao
```

#### Tasks
1. Executar `/change` com descricao do Sprint 8 v3.1
2. Listar todos os arquivos a serem tocados (incluindo Phase -1 a Phase 6):
   - `infra/pulumi/src/components/infra.ts` (registry name fix `loopzero-system`)
   - `infra/pulumi/Pulumi.dev.yaml` (T0.1 dbNodeClass medium, T0.2 mgmtNodeClass medium)
   - `infra/helm/gitea/values.yaml`, `values-ha.yaml` (T2.1)
   - `infra/helm/harbor/values.yaml` (T2.2)
   - `infra/helm/minio/values.yaml`, `values-distributed.yaml` (T2.3)
   - `infra/helm/monitoring/dashboards/tenant-health.json` (T2.4)
   - `infra/helm/monitoring/manifests/prometheusrule-*.yaml` (T6.1)
   - `api/internal/metrics/metrics.go` (T6.1: 5 novas metricas)
   - `infra/pulumi/src/components/mgmt-gitea.ts` (T2.1: NFS PVC)
   - `infra/pulumi/src/components/harbor-bootstrap.ts` (T2.2: Redis externo)
   - `docs/slo.md` (T6.2 NEW)
   - `docs/runbooks/alerts/*.md` (T6.2 novos runbooks)
   - `docs/plans/sprint8-hardening-chaos-plan.md` (este plano)
3. Aguardar meeting (Meeting Orchestrator + k8s-controller-engineer + platform-architect + security-engineer)
4. Aguardar ADR record + validation plan
5. APPROVED → escopo expandido permite edits em todos os arquivos listados

#### Acceptance Criteria
- [ ] `/change` retorna APPROVED
- [ ] ADR Sprint 8 v3.1 registrado em `docs/meetings/adr/`
- [ ] Validation plan registrado
- [ ] Hook governance-gate.py para de bloquear edits no escopo

#### DoD
- [ ] Sprint 8 oficialmente aprovado para implementacao
- [ ] Escopo claro do que pode ser editado por implementation agents

---

### T-1.0b — Criar bucket de state (loopzero-dev-pulumi-state) + inicializar stack dev

#### Objective
Criar o bucket DO Spaces que sera o backend de state pulumi para stack `dev`, e inicializar o stack vazio. Sem isso, `pulumi login` falha com "NoSuchBucket".

#### Evidence
- `pulumi login s3://loopzero-dev-pulumi-state?endpoint=sfo3.digitaloceanspaces.com` retorna `NoSuchBucket` (bucket nao existe).
- DO Spaces sfo3 ja tem `loopzero-builds`, `loopzero-static-assets`, `loopzero-homolog-pulumi-state` (homolog) — falta o bucket dev.
- Bucket DO Spaces custa $5/mo + $0.02/GB transferido.

#### Files to edit
```
NENHUM — apenas operacoes em DO Spaces
```

#### Tasks
1. Criar bucket via boto3 (single-shot script):
   ```bash
   eval "$(grep -E '^[A-Z][A-Z_0-9]*=' /home/paulo/loopzero/.env | sed 's/^/export /')"
   python3 -c "
   import boto3, os
   s3 = boto3.client('s3', region_name='us-east-1',
       endpoint_url='https://sfo3.digitaloceanspaces.com',
       aws_access_key_id=os.environ['SPACES_ACCESS_KEY_ID'],
       aws_secret_access_key=os.environ['SPACES_SECRET_ACCESS_KEY'])
   s3.create_bucket(Bucket='loopzero-dev-pulumi-state')
   print('Bucket criado: loopzero-dev-pulumi-state')
   "
   ```
2. Login pulumi: `pulumi login 's3://loopzero-dev-pulumi-state?endpoint=sfo3.digitaloceanspaces.com&region=us-east-1'`
3. Inicializar stack: `cd infra/pulumi && pulumi stack init dev` (vai pedir passphrase — usar `PULUMI_CONFIG_PASSPHRASE` do .env)
4. Selecionar: `pulumi stack select dev`
5. Confirmar config carregada: `pulumi config --stack dev` lista todos os secrets/values do `Pulumi.dev.yaml`

#### TDD
```
RED:     pulumi login s3://loopzero-dev-pulumi-state ... — NoSuchBucket
GREEN:   create-bucket via boto3
VERIFY:  pulumi stack ls retorna stack `dev`
```

#### Acceptance Criteria
- [ ] Bucket `loopzero-dev-pulumi-state` criado em sfo3
- [ ] `pulumi login` exit 0 contra novo bucket
- [ ] Stack `dev` inicializado, vazio
- [ ] `pulumi config --stack dev` exibe valores do Pulumi.dev.yaml (nao vazio)

#### DoD
- [ ] Backend pulumi pronto para receber state do stack dev

---

### T-1.1 — Instalar pulumi CLI + carregar credenciais

#### Objective
Tornar `pulumi` executavel e injetar todos os secrets necessarios no environment shell.

#### Evidence
`pulumi` nao esta no PATH default. `.env` contem 5 secrets necessarios para o stack:
- `DIGITALOCEAN_TOKEN` — provider DO (DOKS, PG, Spaces, Registry)
- `SPACES_ACCESS_KEY_ID` + `SPACES_SECRET_ACCESS_KEY` — backend de state pulumi (`s3://loopzero-dev-pulumi-state`)
- `PULUMI_CONFIG_PASSPHRASE` — decifra os 5 `secure:` em `Pulumi.dev.yaml`
- `CLOUDFLARE_API_TOKEN` — provider CF (Tunnel, Worker, KV, DNS records)

#### Files to edit
```
NENHUM — apenas operacoes shell
```

#### Tasks
1. Instalar pulumi: `curl -fsSL https://get.pulumi.com | sh`
2. Adicionar ao PATH desta sessao: `export PATH="$HOME/.pulumi/bin:$PATH"`
3. Validar: `pulumi version` retorna >= 3.232
4. Carregar `.env` no shell SEM expor variaveis no prompt:
   ```bash
   # NOTA: source direto pode falhar em chaves com espacos (ex: "username =val").
   # Use grep dos KEY=VALUE legitimos antes de eval:
   eval "$(grep -E '^[A-Z_]+=' /home/paulo/loopzero/.env | sed 's/^/export /')"
   ```
5. Configurar AWS aliases para Spaces (backend pulumi):
   ```bash
   export AWS_ACCESS_KEY_ID="$SPACES_ACCESS_KEY_ID"
   export AWS_SECRET_ACCESS_KEY="$SPACES_SECRET_ACCESS_KEY"
   export AWS_REGION=us-east-1
   ```
6. Login no backend S3-compatible:
   ```bash
   pulumi login 's3://loopzero-dev-pulumi-state?endpoint=sfo3.digitaloceanspaces.com&region=us-east-1'
   ```
7. Selecionar stack: `cd infra/pulumi && pulumi stack select dev`
8. Validar acesso aos secrets do stack: `pulumi config get --stack dev digitaloceanToken` (deve decifrar com PULUMI_CONFIG_PASSPHRASE e retornar valor)

#### Acceptance Criteria
- [ ] `pulumi version` >= 3.232
- [ ] `pulumi whoami` retorna identidade S3-backed (e.g. `paulohenriquevn` ou similar)
- [ ] `pulumi stack ls` lista stack `dev`
- [ ] `pulumi config get --stack dev region` retorna `sfo3`
- [ ] `pulumi config get --stack dev digitaloceanToken` decifra (nao retorna `[secret]` literal)

#### DoD
- [ ] Pulumi instalado, autenticado, secrets decifraveis
- [ ] Shell tem todas as env vars necessarias (DO_TOKEN, AWS_*, PULUMI_CONFIG_PASSPHRASE, CLOUDFLARE_API_TOKEN)

---

### T-1.2 — Pre-flight: pulumi preview + sanity checks

#### Objective
Antes de fazer `pulumi up`, validar via `preview` que o plano de criacao bate com expectativa (sem surprises de drift, sem secrets faltando, sem orphans nao-detectados).

#### Evidence
`pulumi up` from-scratch sobre stack ja-vivido pode falhar com "resource X already exists" se nao detectarmos orphans. `core/adopt-or-create.ts` ja faz isso para CF + VPC; precisamos confirmar que orphans atuais (registry `loopzero-system`, VPC `loopzero-infra-dev-sfo3-net`) sao adotados, nao recriados.

#### Files to edit
```
NENHUM
```

#### Tasks
1. `cd infra/pulumi && pulumi refresh --stack dev --yes` — sincroniza state com realidade DO (detecta o que ja existe)
2. `pulumi preview --stack dev 2>&1 | tee /tmp/sprint8-preview.log`
3. Inspecionar log: contar `+ create`, `~ update`, `- delete`, `= same`
4. Esperado:
   - **VPC** `loopzero-infra-dev-sfo3-net` → `same` (existe, sera adopted)
   - **Registry** `loopzero-system` → `same` ou `update`
   - **DOKS** mgmt/build/runtime/runtime-2 → todos `+ create`
   - **PG** `loopzero-infra-dev-sfo3-db` → `+ create`
   - **CF Tunnel/Worker/KV** → `+ create` ou `same` se orphans detectados
   - **Helm releases** (~30+) → `+ create` (dependem dos clusters)
5. Se algo aparecer como `- delete`: PARAR e investigar (pode ser drift legitimo ou orphan que nao foi adoptado)
6. Custo estimado: documentar baseado em `pulumi preview` (nodes + PG + Spaces + Registry)

#### Acceptance Criteria
- [ ] `pulumi preview` exit 0 (warnings OK, errors = stop)
- [ ] Zero `- delete` em recursos protegidos (`protect: isProd` em `infra.ts`)
- [ ] Plano inclui 4 DOKS (mgmt + build + runtime + runtime-2), 1 PG, 5+ Spaces, 1 CF Tunnel, ~30 Helm releases
- [ ] Custo mensal estimado documentado (~$170-250 baseline)

#### DoD
- [ ] Preview executado e revisado
- [ ] Plano coerente com expectativa
- [ ] Decision point: prosseguir para `pulumi up`?

---

### T-1.3 — pulumi up: provisionar TUDO (infra + CF + bootstrap clusters + DNS)

#### Objective
Executar `pulumi up --stack dev --yes` que materializa **simultaneamente** infra base + CF resources + bootstrap dos 4 clusters (Helm charts) + DNS — todas as 3 phases internas do Pulumi em uma unica execucao.

#### Evidence
`infra/pulumi/src/index.ts` sequencia automaticamente: createInfra (Phase 1) -> createTunnel + createStaticDelivery (Phase 1.5) -> bootstrapCluster (Phase 2) -> exposePlatform (Phase 3). Pulumi resolve dependencies via `apply()` chains automaticamente. Tempo total: ~45-90min (DOKS create ~10min cada x 4 + Helm ~3min cada x ~30 releases + DNS propagation).

#### Files to edit
```
NENHUM — apenas execucao
```

#### Tasks
1. Backup state pre-up: `pulumi stack export --stack dev > /tmp/state-pre-bootstrap.json`
2. Executar com log: `pulumi up --stack dev --yes 2>&1 | tee /tmp/sprint8-up.log`
3. **Long-running:** ~45-90min. Nao interromper.
4. Em caso de falha em meio do up:
   - `pulumi refresh && pulumi up` (idempotente, retoma do estado parcial)
   - Se algum DOKS falhar de criar: checar quota DO (`doctl account get` mostra droplet limit)
5. Apos sucesso: salvar kubeconfigs dos 4 DOKS:
   ```bash
   for ID in $(doctl kubernetes cluster list --format ID --no-header); do
     doctl kubernetes cluster kubeconfig save "$ID"
   done
   kubectl config get-contexts | grep do-sfo3
   ```
6. Salvar outputs criticos: `pulumi stack output --stack dev --json > /tmp/stack-outputs.json` (contem dbHost, mgmtKubeconfig, etc — todos secrets ficam em `pulumi.secret()`)

#### TDD
```
RED:     doctl kubernetes cluster list — vazio
         doctl databases list — vazio
GREEN:   pulumi up --stack dev --yes
VERIFY:  doctl kubernetes cluster list — 4 entries (mgmt, build, runtime, runtime-2)
         doctl databases list — 1 entry
         kubectl --context do-sfo3-loopzero-infra-dev-sfo3-mgmt get pods -A | grep -c Running > 30
```

#### Acceptance Criteria
- [ ] 4 DOKS clusters Running (mgmt + build + runtime + runtime-2)
- [ ] 1 PG managed online
- [ ] CF Tunnel + Worker + KV ativos
- [ ] DO Spaces buckets + Registry binding ativos
- [ ] Helm releases instalados (validar via `helm list -A --kube-context <each-cluster>`):
  - mgmt: cert-manager, ArgoCD, Traefik, Gitea, MinIO, Prometheus, Grafana, loopzero-api, tenant-controller (>=9 releases)
  - build: argo-workflows, Loki, Promtail (>=3 releases)
  - runtime/runtime-2: cert-manager, Traefik, ESO, KEDA, Promtail (>=5 releases cada)
- [ ] CRDs LoopZero instalados em mgmt: `kubectl get crd | grep loopzero` >= 10
- [ ] DNS resolvendo: `dig api.dev.loopzero.example +short` retorna IP CF
- [ ] `pulumi up` exit 0
- [ ] Zero drift apos: re-rodar `pulumi preview` mostra "no changes"

#### DoD
- [ ] Esteira completa funcional em DO
- [ ] kubeconfigs dos 4 clusters disponiveis em `~/.kube/config`
- [ ] `kubectl get pods -A` em cada cluster: zero CrashLoopBackOff em pods esperados

---

### T-1.4 — Validacao pos-bootstrap: stack outputs + execution state

#### Objective
Confirmar que o `execution` output do Pulumi (em `index.ts:106-138`) reporta TRUE para todos os 14+ componentes de health.

#### Evidence
`buildExecutionState` em `infra/pulumi/src/core/execution.ts` emite um output estruturado com 14 componentes (network, mgmtCluster, buildCluster, runtimeCluster, runtimeCluster2, database, storage, registry, mgmtIngress, mgmtCertManager, mgmtArgocd, mgmtGitea, mgmtPrometheus, mgmtGrafana, mgmtLoopZeroApi, mgmtTenantController, mgmtTls, buildArgoWorkflows, runtimeIngress, mgmtDns, runtimeDns). Cada um deve ser true.

#### Files to edit
```
NENHUM
```

#### Tasks
1. `pulumi stack output --stack dev execution --json | jq` — inspecionar todos os componentes
2. Validar que TODOS retornam `true`. Qualquer `false` indica componente que falhou silenciosamente.
3. Health checks por cluster:
   ```bash
   # mgmt
   kubectl --context <mgmt-ctx> get pods -n loopzero-system -l app=loopzero-api
   kubectl --context <mgmt-ctx> -n argocd get applications  # vazia inicialmente, OK
   # build
   kubectl --context <build-ctx> get pods -n argo
   # runtime
   kubectl --context <runtime-ctx> get pods -n cert-manager
   # runtime-2 (cell-2)
   kubectl --context <runtime-2-ctx> get pods -A | grep -c Running
   ```
4. Endpoint health check: `curl -fsS https://api.dev.loopzero.example/healthz` retorna 200

#### Acceptance Criteria
- [ ] `execution` output: todos os 14+ componentes `true`
- [ ] `kubectl get pods -A` por cluster: zero `CrashLoopBackOff` ou `ImagePullBackOff`
- [ ] `https://api.dev.loopzero.example/healthz` retorna 200
- [ ] `https://grafana.dev.loopzero.example` carrega (login pode pedir)
- [ ] PG conectivel: `psql "$dbUri" -c "SELECT 1"` retorna 1

#### DoD
- [ ] Stack 100% healthy via execution output
- [ ] Endpoints publicos respondem
- [ ] Smoke check em cada cluster passa

---

### T-1.5 — Smoke E2E: deploy + rollback contra DO bootstrapped

#### Objective
Validar que o deploy E2E happy path funciona contra DO recem-bootstrapped — confirmacao final de que o ambiente esta pronto para Phase 0.

#### Evidence
Sem smoke test, qualquer falha em Phase 0/1 sera ambigua: bug do Sprint 8 ou bootstrap incompleto? Memoria do agente registra que ambiente "mirrors production" e que E2E foi validado em droplet 165.227.121.20 — agora validamos contra DOKS real.

#### Files to edit
```
NENHUM
```

#### Tasks
1. CLI setup: `cd /home/paulo/loopzero/cli && npm run build && npm link` (ou usar `node dist/index.js`)
2. `loopzero login --api https://api.dev.loopzero.example` (interativo: cria conta inicial via Kratos)
3. Smoke project:
   ```bash
   mkdir -p /tmp/smoke && cd /tmp/smoke
   git init -q
   echo "console.log('hello sprint 8')" > index.js
   echo '{"name":"smoke","version":"0.0.1","scripts":{"start":"node index.js"}}' > package.json
   git add . && git commit -q -m "init"
   loopzero init --framework node
   ```
4. Deploy: `loopzero deploy --env staging --json | tee /tmp/smoke-deploy.json`
5. Validar URL: `curl -fsS $(jq -r .url /tmp/smoke-deploy.json)` retorna 200
6. Rollback (se houver release anterior): `loopzero rollback --env staging --json`
7. Logs: `loopzero logs --env staging --tail 10`

#### Acceptance Criteria
- [ ] `loopzero login` exit 0
- [ ] `loopzero deploy` exit 0, `release_state: ACTIVE` em <5min
- [ ] App URL responde HTTP 200 com body esperado
- [ ] `loopzero logs` retorna linhas reais (nao vazio, nao timeout)
- [ ] Sem internal terms (Rule 3) na saida CLI: `grep -iE "argocd|gitea|crd" /tmp/smoke-deploy.json` retorna vazio

#### DoD
- [ ] E2E happy path passa contra DO real
- [ ] Maturity baseline (3.6) confirmada para Sprint 8 prosseguir

---

## Phase 0: Infrastructure Scale-Up

**Objective:** Escalar infraestrutura DO para suportar Phase 2a workload (100-500 tenants + HA services).

### T0.1 — PG bump: dbNodeClass small → medium

#### Objective
Escalar PostgreSQL para `db-s-2vcpu-4gb` (max_connections >= 100) para eliminar bottleneck de connection pool.

#### Evidence
ADR-0010 deliverable #2: "PG bump — max_connections >= 100 (db-s-2vcpu-4gb minimum)". Pulumi.dev.yaml atual: `dbNodeClass: small` = db-s-1vcpu-2gb (~25 connections). MEET-051 analise: "PG connection pool at 72% idle saturation". Com 100 tenants (L3), cada deploy usa 1 advisory lock connection + 1 query connection = 200 connections nos picos.

#### Files to edit
```
infra/pulumi/Pulumi.dev.yaml — dbNodeClass: small → medium
```

#### Deep file dependency analysis
- `Pulumi.dev.yaml` (49 LOC): stack config. Unica mudanca: linha 24 `dbNodeClass: small` → `medium`.
- `infra/pulumi/src/config/stack.ts`: le `dbNodeClass` como `NodeClass` type. `medium` ja e valor valido.
- `infra/pulumi/src/components/infra.ts`: usa `config.dbNodeClass` para provisionar DO managed PG. Size mapping: `small` = db-s-1vcpu-2gb, `medium` = db-s-2vcpu-4gb.
- Downstream: zero code changes. PG managed faz resize com failover automatico (~5min downtime).

#### Tasks
1. Alterar `Pulumi.dev.yaml` linha 24: `loopzero-infra:dbNodeClass: medium`
2. `cd infra/pulumi && npx pulumi preview --stack dev` — confirmar que unica mudanca e PG resize
3. `cd infra/pulumi && npx pulumi up --stack dev` — aplicar (esperar failover ~5min)
4. Validar: `SELECT setting FROM pg_settings WHERE name = 'max_connections'` retorna >= 100

#### TDD
```
RED:     max_connections < 100 (estado atual)
GREEN:   pulumi up aplica resize
REFACTOR: None
VERIFY:  psql -c "SHOW max_connections" retorna >= 100
```

#### Acceptance Criteria
- [ ] `dbNodeClass: medium` no Pulumi.dev.yaml
- [ ] PG resize concluido sem data loss
- [ ] max_connections >= 100 confirmado
- [ ] Aplicacao reconecta automaticamente apos failover
- [ ] `pulumi preview` mostra zero drift apos apply

#### DoD
- [ ] PG medium ativo
- [ ] max_connections >= 100
- [ ] API healthy apos resize

---

### T0.2 — mgmt cluster: small → medium + auto-scale

#### Objective
Escalar mgmt cluster para reduzir memory idle de 74% para < 50% e habilitar auto-scale para headroom.

#### Evidence
ADR-0010 deliverable #6: "mgmt cluster memory headroom — below 50% idle". Atual: `mgmtNodeClass: small` (2vCPU/4GB per node x 3 nodes = 12GB total). Com HA replicas de Gitea, Harbor, ArgoCD, Prometheus, Loki: ~8.9GB usado = 74% saturacao. Com `medium` (4vCPU/8GB per node x 3 = 24GB total), mesmo workload = ~37% saturacao.

#### Files to edit
```
infra/pulumi/Pulumi.dev.yaml — mgmtNodeClass: medium, mgmtMaxNodes: 5
```

#### Deep file dependency analysis
- `Pulumi.dev.yaml`: linhas 10-12. `mgmtNodeClass: small` → `medium`, `mgmtMaxNodes: 3` → `5`.
- `infra/pulumi/src/components/infra.ts`: usa `config.mgmtNodeClass` e `config.mgmtMaxNodes` para DOKS cluster spec. Auto-scale habilitado quando `maxNodes > minNodes`.
- Downstream: DOKS faz rolling upgrade dos nodes (um por vez, ~15min total). Pods re-schedulam automaticamente.

#### Tasks
1. Alterar Pulumi.dev.yaml: `mgmtNodeClass: medium`, `mgmtMaxNodes: "5"`
2. `pulumi preview --stack dev` — confirmar node pool resize
3. `pulumi up --stack dev` — aplicar rolling upgrade
4. Monitorar: `kubectl top nodes` — confirmar memory < 50%
5. Validar: todos os pods healthy apos rolling upgrade

#### TDD
```
RED:     kubectl top nodes mostra > 50% memory usage
GREEN:   pulumi up com medium nodes
REFACTOR: None
VERIFY:  kubectl top nodes mostra < 50% memory em todos os nodes
```

#### Acceptance Criteria
- [ ] `mgmtNodeClass: medium` no Pulumi.dev.yaml
- [ ] `mgmtMaxNodes: 5` (auto-scale habilitado)
- [ ] Memory idle < 50% apos upgrade
- [ ] Zero downtime durante rolling upgrade (pods re-scheduled)
- [ ] Todos os pods running apos upgrade

#### DoD
- [ ] mgmt cluster com nodes medium
- [ ] Memory < 50% confirmado
- [ ] Auto-scale funcional (min=3, max=5)

---

### T0.3 — Instalar Chaos Mesh nos clusters DO

#### Objective
Instalar Chaos Mesh 2.7.0 nos 3 clusters DOKS para habilitar chaos tests do Gate 2a.

#### Evidence
Gate 2a exige: "chaos test proving each shared service can be killed independently with recovery <2min". Chaos tests em `infra/tests/chaos/` requerem Chaos Mesh CRDs. 9 manifests ja existem.

#### Files to edit
```
infra/helm/chaos-mesh/values.yaml — (NEW) Helm values
infra/helm/chaos-mesh/Chart.yaml — (NEW) Chart dependency
Taskfile.yml — task chaos:install
```

#### Deep file dependency analysis
- `Taskfile.yml` (1695 LOC): adicionar tasks `chaos:install` e `chaos:uninstall`.
- `infra/tests/chaos/manifests/`: 9 YAML files que referenciam PodChaos, StressChaos, NetworkChaos CRDs.
- Chaos Mesh namespace: `chaos-mesh` (isolado de workloads).

#### Tasks
1. Criar `infra/helm/chaos-mesh/Chart.yaml` com Chaos Mesh 2.7.0 dependency
2. Criar `infra/helm/chaos-mesh/values.yaml`: containerd runtime, dashboard disabled, non-root
3. Adicionar `task chaos:install` no Taskfile.yml
4. Executar `task chaos:install` nos 3 clusters
5. Validar: `kubectl get crd podchaos.chaos-mesh.org` retorna Found

#### TDD
```
RED:     kubectl get crd podchaos.chaos-mesh.org — NotFound
GREEN:   task chaos:install
VERIFY:  3 CRDs registrados em cada cluster
```

#### Acceptance Criteria
- [ ] Chaos Mesh 2.7.0 instalado nos 3 clusters
- [ ] CRDs PodChaos, StressChaos, NetworkChaos registrados
- [ ] `task chaos:install` idempotente
- [ ] Helm chart versao pinned

#### DoD
- [ ] 3 clusters com Chaos Mesh
- [ ] `task chaos:install` / `task chaos:uninstall` funcionais

---

## Phase 1: L3 Stabilization (BLOQUEADOR CRITICO)

**Objective:** Atingir L3 >= 95/100 em 5 execucoes consecutivas. Sem isso, Phase 2a falha.

### T1.1 — Executar L3 diagnostico + categorizar falhas

#### Objective
Executar L3 contra DO (apos PG bump de T0.1) e categorizar cada falha por root cause.

#### Evidence
Melhor L3: 75/100. Categorias provaveis (ADR-0010 analise): shared service saturation (Gitea concurrent clones), PG pool exhaustion (antes do bump), BuildKit concurrency ceiling (4/node x 2 = 8 concurrent, L3 usa 10), ArgoCD sync timeout, app verification timeout.

#### Files to edit
```
infra/tests/load/real_deploy_test.go — ajustes de timeout/concurrency se necessario
docs/plans/sprint8-hardening-chaos-plan.md — registrar diagnostico inline
```

#### Deep file dependency analysis
- `real_deploy_test.go`: `TestRealDeploy_L3_100Tenants` com 100 tenants, concurrency 10, timeout 4h. Cada deploy: provision → upload → submit → poll → verify HTTP 200.
- `real_deploy_harness.go`: `RealDeploy()` retorna `FullDeployResult` com stages timing + error categorization.
- Dependencia: T0.1 (PG medium) deve estar completo — PG small satura com 100 concurrent locks.

#### Tasks
1. Confirmar T0.1 completo (PG medium ativo)
2. Executar L3: `LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l3 LOOPZERO_API_URL=https://api.dev.loopzero.example LOOPZERO_APP_DOMAIN=dev.loopzero.example LOOPZERO_SERVICE_AUTH=<token> go test ./infra/tests/load/ -run L3 -v -timeout 4h 2>&1 | tee l3-diag-run1.log`
3. Para cada falha: categorizar como `build_timeout | verify_timeout | admission_reject | upload_fail | pg_timeout | argocd_sync_fail | unknown`
4. Coletar metricas Prometheus durante L3: `loopzero_postgres_connections_active`, `loopzero_builds_active`, `loopzero_build_queue_depth`, `loopzero_api_request_duration_seconds`
5. Identificar top 3 root causes
6. Documentar com dados

#### TDD
```
RED:     L3 < 95/100
GREEN:   Fixes nos tasks T1.2-T1.4
VERIFY:  L3 >= 95/100 apos fixes
```

#### Acceptance Criteria
- [ ] L3 executado com logs completos
- [ ] Cada falha categorizada
- [ ] Top 3 root causes com dados quantitativos
- [ ] Metricas Prometheus coletadas durante execucao

#### DoD
- [ ] Diagnostico completo com dados
- [ ] Action items para cada root cause

---

### T1.2 — Fix L3 root cause #1

#### Objective
Corrigir o root cause mais frequente de L3 failures.

#### Evidence
Dependente de T1.1. Area provavel: BuildKit concurrency ceiling (10 concurrent vs 8 max → queuing → timeout). Fix provavel: aumentar `buildMaxNodes` de 2 para 3 (12 concurrent slots) ou ajustar workflow concurrency.

#### Files to edit
```
TBD — dependente de T1.1
Provavel: infra/pulumi/Pulumi.dev.yaml (buildMinNodes/buildMaxNodes)
      ou: api/internal/argo/ (workflow concurrency/timeout config)
```

#### Tasks
1. Escrever teste que reproduz root cause
2. Implementar fix (config ou code)
3. Re-executar L3
4. Documentar melhoria

#### TDD
```
RED:     Test que reproduz root cause #1
GREEN:   Fix minimo
VERIFY:  L3 re-run mostra melhoria
```

#### Acceptance Criteria
- [ ] Root cause #1 tem fix implementado
- [ ] L3 success rate melhora (medido)

#### DoD
- [ ] Fix aplicado
- [ ] L3 re-executado com melhoria documentada

---

### T1.3 — Fix L3 root cause #2

#### Objective
Corrigir o segundo root cause mais frequente.

#### Evidence
Dependente de T1.1. Area provavel: ArgoCD sync timeout ou app verification timeout.

#### Files to edit
```
TBD — dependente de T1.1
```

#### Tasks
1. Escrever teste que reproduz root cause
2. Implementar fix
3. Re-executar L3
4. Documentar melhoria cumulativa

#### TDD
```
RED:     Test que reproduz root cause #2
GREEN:   Fix minimo
VERIFY:  L3 re-run com melhoria cumulativa
```

#### Acceptance Criteria
- [ ] Root cause #2 corrigido
- [ ] L3 success rate cumulativamente melhor

#### DoD
- [ ] Fix aplicado e testado

---

### T1.4 — Fix L3 root cause #3 + 5x validation runs

#### Objective
Corrigir terceiro root cause e executar 5 L3 consecutivos >= 95/100 (Gate #1 + Gate 2a exit criteria).

#### Evidence
Gate 2a Exit: "5 consecutive L3 >= 98/100". Gate #1: "10x >= 95/100". Sprint 8 target: 5x >= 95/100 (progresso para ambos os gates).

#### Files to edit
```
TBD + docs/runbooks/l3-validation-results.md — (NEW)
```

#### Tasks
1. Implementar fix root cause #3 (se necessario)
2. Executar L3 run #1 — documentar resultado
3. Se >= 95/100: executar run #2, senao: voltar ao diagnostico
4. Repetir ate 5 runs consecutivos >= 95/100
5. Documentar todos os resultados com metricas

#### TDD
```
RED:     L3 < 95/100
GREEN:   Fixes acumulados
VERIFY:  5 L3 consecutivos >= 95/100
```

#### Acceptance Criteria
- [ ] 5 L3 consecutivos >= 95/100 (ou melhor resultado com gap documentado)
- [ ] Nenhum data loss durante L3
- [ ] Recovery times dentro dos SLOs

#### DoD
- [ ] 5 runs documentados com resultados e metricas
- [ ] Progresso para Gate #1 de ADR-0010 registrado

---

## Phase 2: Shared Service HA (Paralelo com Phase 1)

**Objective:** Entregar os 4 deliverables de HA de ADR-0010: Gitea, Harbor, MinIO, tenant-health dashboard.

### T2.1 — Gitea HA: 2 replicas + NFS shared storage

#### Objective
Escalar Gitea para 2 replicas com shared storage via DO Volumes (NFS).

#### Evidence
ADR-0010 deliverable #3: "Gitea HA — shared storage + 2 replicas + load test 200 concurrent clones". Gitea atual: 1 replica, PVC local. Redis para sessions/cache ja configurado (`values-ha.yaml`). Git repos estao no PVC — replicar exige shared filesystem.

#### Files to edit
```
infra/helm/gitea/values.yaml — replicas: 2, persistence via NFS
infra/helm/gitea/values-ha.yaml — ajustes para multi-replica
infra/pulumi/src/components/mgmt-gitea.ts — provisionar DO Volume com NFS
infra/tests/capacity/gitea_push_test.go — adicionar test 200 concurrent clones
```

#### Deep file dependency analysis
- `values.yaml`: Gitea Helm chart config. `replicas: 1` → `2`. `persistence.storageClass` precisa mudar para NFS-capable.
- `values-ha.yaml`: overlay com Redis config. Ja configurado para sessions/cache/queues.
- `mgmt-gitea.ts`: Pulumi component. Precisa criar DO Volume e configurar como PVC compartilhado.
- `gitea_push_test.go` (413 LOC): testa push latency. Adicionar cenario de 200 concurrent `git clone`.
- Downstream: ArgoCD le repos do Gitea. Com 2 replicas: load balancing via ClusterIP Service.

#### Deep Dives
- **NFS no DigitalOcean:** DO Volumes suportam ReadWriteOnce (RWO), nao ReadWriteMany (RWX). Para RWX: usar NFS provisioner (nfs-subdir-external-provisioner) montando um DO Volume como backend. Alternativa: Longhorn. Complexidade: media.
- **Gitea multi-replica caveats:** Gitea 1.22+ suporta multi-replica com shared filesystem. `indexer` precisa de Bleve on shared disk ou Elasticsearch externo. Preferir Bleve on NFS (simples).
- **Load test 200 concurrent clones:** Fork do `gitea_push_test.go` com 200 goroutines fazendo `git clone` paralelo. Medir: p50, p95, p99 latency + failure rate.

#### Tasks
1. Instalar NFS provisioner no mgmt cluster: `helm install nfs-provisioner nfs-subdir-external-provisioner/nfs-subdir-external-provisioner --set nfs.path=/data --set nfs.server=<volume-ip>`
2. Criar DO Volume para Gitea data (50GB, SFO3)
3. Configurar PVC com StorageClass NFS
4. Alterar `values.yaml`: `replicas: 2`, persistence apontando para NFS PVC
5. Deploy: `helm upgrade gitea ...`
6. Validar: ambos pods running, git clone funciona em ambos
7. Adicionar test de 200 concurrent clones em `gitea_push_test.go`
8. Executar test: confirmar p95 < 5s para clone e 0% failure rate

#### TDD
```
RED:     TestGiteaConcurrentClones_200() — 200 goroutines clone simultaneo, falha se > 5% error
RED:     TestGiteaReplicas() — kubectl get pods -l app=gitea retorna 2 Running
GREEN:   Deploy Gitea HA
REFACTOR: Tune NFS mount options se latency alta
VERIFY:  LOOPZERO_CAPACITY=1 go test ./infra/tests/capacity/ -run Gitea -v
```

#### Acceptance Criteria
- [ ] 2 replicas Gitea running
- [ ] Shared storage funcional (clone em replica A, push em replica B)
- [ ] 200 concurrent clones: p95 < 5s, failure rate < 1%
- [ ] ArgoCD continua sincronizando sem erro
- [ ] Failover: kill 1 pod, service continua respondendo

#### DoD
- [ ] Gitea HA operacional
- [ ] Load test 200 concurrent clones passando
- [ ] Failover validado

---

### T2.2 — Harbor HA: 2+ core replicas + Redis externo

#### Objective
Escalar Harbor para 2+ core/registry replicas com Redis externo (DO managed).

#### Evidence
ADR-0010 deliverable #4: "Harbor HA — 2+ core replicas + Redis HA + S3 shared". Atual: core=1, registry=1, Redis interno single-instance. S3 backend ja configurado (DO Spaces). PG externo ja configurado.

#### Files to edit
```
infra/helm/harbor/values.yaml — core.replicas: 2, registry.replicas: 2, Redis externo
infra/pulumi/src/components/harbor-bootstrap.ts — configurar Redis managed
```

#### Deep file dependency analysis
- `values.yaml` (Harbor): `core.replicas: 1` → `2`, `registry.replicas: 1` → `2`. Redis config: mudar de `internal` para `external` apontando para DO managed Redis.
- `harbor-bootstrap.ts`: Pulumi component que cria DB, robot accounts. Adicionar Redis connection string.
- Downstream: TenantController cria Harbor robot accounts. Multi-replica: load balanced via ClusterIP.

#### Tasks
1. Alterar `values.yaml`: `core.replicas: 2`, `registry.replicas: 2`
2. Configurar Redis externo (DO managed Redis, mesmo usado por Gitea)
3. `helm upgrade harbor ...`
4. Validar: core e registry com 2 pods cada
5. Testar: push + pull image funciona
6. Testar: kill 1 core pod, operations continuam

#### TDD
```
RED:     TestHarborCoreReplicas() — kubectl get pods retorna 2 core + 2 registry Running
RED:     TestHarborFailover() — kill 1 core pod, push/pull continua funcionando
GREEN:   Deploy Harbor HA
VERIFY:  docker push + docker pull funciona apos deploy
```

#### Acceptance Criteria
- [ ] core: 2 replicas running
- [ ] registry: 2 replicas running
- [ ] Redis externo (DO managed) configurado
- [ ] Push/pull image funciona
- [ ] Failover: kill 1 pod, operacoes continuam

#### DoD
- [ ] Harbor HA operacional
- [ ] Failover validado

---

### T2.3 — MinIO distributed: 4 drives com erasure coding

#### Objective
Migrar MinIO de single-instance para distributed mode com erasure coding.

#### Evidence
ADR-0010 deliverable #5: "MinIO distributed migration — per existing docs/plans/minio-distributed-migration.md". Plano referenciado mas NAO encontrado no codebase. MinIO single-instance e SPOF: perda de pod = perda de build artifacts.

#### Files to edit
```
infra/helm/minio/values.yaml — mode: distributed, replicas: 4
infra/helm/minio/values-distributed.yaml — (NEW) overlay para distributed
docs/plans/minio-distributed-migration.md — (NEW) plano de migracao (referenciado por ADR-0010)
```

#### Deep file dependency analysis
- `infra/helm/minio/values.yaml`: MinIO Helm chart. Mudar de `mode: standalone` para `mode: distributed` com `replicas: 4` e `drivesPerNode: 1`.
- Downstream: build pipeline (`infra/helm/loopzero-build-cwts/`) referencia MinIO endpoint. Se endpoint muda: atualizar.
- `loopzero-api`: le MinIO credentials de Secret. Endpoint configurado via env var.

#### Deep Dives
- **Erasure coding:** MinIO distributed com 4 drives usa EC:2 (2 parity, 2 data). Sobrevive a perda de 2 drives.
- **Migration path:** 1) Deploy novo MinIO distributed ao lado do existente, 2) `mc mirror` dados do old para new, 3) Trocar endpoint no API + build workflows, 4) Deletar old MinIO.
- **DO Volumes:** 4 PVCs de 50GB cada (200GB total). Custo: ~$40/mo.

#### Tasks
1. Criar `docs/plans/minio-distributed-migration.md` (plano referenciado por ADR-0010)
2. Criar `infra/helm/minio/values-distributed.yaml` com mode distributed, 4 replicas
3. Deploy novo MinIO distributed no namespace `minio-distributed`
4. `mc mirror` dados do MinIO antigo para novo
5. Atualizar endpoint no loopzero-api e build CWTs
6. Validar: upload + download de build artifacts funciona
7. Deletar MinIO antigo apos validacao
8. Validar: kill 1 pod MinIO, operacoes continuam (erasure coding)

#### TDD
```
RED:     TestMinIODistributedFailover() — kill 1/4 pods, mc cp funciona
RED:     TestMinIODistributedUploadDownload() — build artifact upload + download OK
GREEN:   Deploy MinIO distributed + migrate data
VERIFY:  Build pipeline funciona end-to-end com novo MinIO
```

#### Acceptance Criteria
- [ ] MinIO distributed com 4 replicas
- [ ] Erasure coding ativo (EC:2)
- [ ] Dados migrados do antigo sem perda
- [ ] Build pipeline funciona com novo endpoint
- [ ] Failover: kill 1 pod, operacoes continuam
- [ ] MinIO antigo removido

#### DoD
- [ ] MinIO distributed operacional
- [ ] `docs/plans/minio-distributed-migration.md` criado
- [ ] Failover validado
- [ ] Zero data loss na migracao

---

### T2.4 — tenant-health Grafana dashboard

#### Objective
Criar dashboard unificado: "tenant X em qual cell, com qual status em cada stage".

#### Evidence
ADR-0010 deliverable #7: "Unified tenant-health Grafana dashboard — answer 'tenant X at which cell, with what status at each stage' from one panel".

#### Files to edit
```
infra/helm/monitoring/dashboards/tenant-health.json — (NEW) dashboard
```

#### Deep file dependency analysis
- `infra/helm/monitoring/dashboards/`: 21 dashboards existentes em JSON. ConfigMap montado no Grafana.
- Metricas necessarias: `loopzero_cell_tenant_count{cell_id}`, `loopzero_builds_total{tenant, status}`, `loopzero_deploys_total{tenant, status}`, `loopzero_custom_domains_total{tenant, status}`.
- Downstream: Grafana auto-import via sidecar.

#### Tasks
1. Criar `tenant-health.json` com paineis:
   - Variable: tenant selector (dropdown)
   - Panel 1: Cell assignment (`loopzero_cell_tenant_count`)
   - Panel 2: Deploy status timeline (builds queued/running/succeeded/failed)
   - Panel 3: Release state (pending/active/superseded/rolled_back)
   - Panel 4: App health (pods running/pending/crashloop)
   - Panel 5: Custom domain status
2. Deploy via ConfigMap
3. Validar: selecionar tenant, ver status em todos os stages

#### TDD
```
RED:     Dashboard nao existe — Grafana 404
GREEN:   Criar JSON + deploy
VERIFY:  Grafana /d/tenant-health acessivel com dados
```

#### Acceptance Criteria
- [ ] Dashboard acessivel no Grafana
- [ ] Tenant selector funcional (dropdown com todos os tenants)
- [ ] Todas as stages visiveis: cell → build → release → app → domain
- [ ] Funciona com dados reais (nao placeholder)

#### DoD
- [ ] `tenant-health.json` commitado
- [ ] Dashboard funcional no Grafana DO

---

## Phase 3: L4 Validation (500 Tenants)

**Objective:** Validar capacidade da celula unica com 500 tenants. Informar dimensionamento de cell-2.

### T3.1 — Executar L4 e documentar capacidade

#### Objective
Rodar L4 (500 tenants) apos L3 estavel para validar capacity assumptions de ADR-0008 (5k apps/cell).

#### Evidence
L4 nunca executado. ADR-0008 assume 5k apps/cell. L4 (500 tenants) = 10% da capacidade target. Se L4 falha em <90%: capacity model precisa revisao antes de Phase 2b.

#### Files to edit
```
docs/runbooks/l4-capacity-results.md — (NEW) resultados + capacity analysis
```

#### Tasks
1. Confirmar L3 estavel (T1.4 completo)
2. Executar L4: `LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l4 go test ./infra/tests/load/ -run L4 -v -timeout 8h 2>&1 | tee l4-run1.log`
3. Monitorar: PG connections, memory, CPU, BuildKit queue, ArgoCD app count
4. Documentar: success rate, bottleneck(s), resource peaks
5. Extrapolar: com estes dados, 5k apps/cell e viavel?
6. Se < 90%: documentar revisao de capacity model

#### TDD
```
RED:     L4 nunca executado
GREEN:   Executar L4
VERIFY:  Resultados documentados com extrapolacao para 5k/cell
```

#### Acceptance Criteria
- [ ] L4 executado ate conclusao
- [ ] Success rate documentado (target: >= 90%)
- [ ] Resource peaks por componente documentados
- [ ] Capacity extrapolacao para 5k apps/cell documentada
- [ ] Se < 90%: capacity model revisado

#### DoD
- [ ] `docs/runbooks/l4-capacity-results.md` criado
- [ ] Dados informam dimensionamento de cell-2

---

## Phase 4: Gate 2a Exit Validation

**Objective:** Checkpoint formal: todos os 7 deliverables de Phase 2a completos.

### T4.1 — Gate 2a exit checklist

#### Objective
Validar formalmente que todos os 7 deliverables de ADR-0010 Phase 2a estao completos.

#### Evidence
ADR-0010 Gate 2a Exit: "all seven done + chaos test proving each shared service can be killed independently with recovery <2min + 5 consecutive L3 >= 98/100".

#### Files to edit
```
docs/meetings/adr/0010-phase2-rescope.md — adicionar secao "Gate 2a Status" com evidencia
docs/maturity-assessment.md — atualizar score
CLAUDE.md — atualizar sprint status
```

#### Tasks
1. Checklist Gate 2a:
   - [ ] L3 5x >= 95/100 (evidencia: logs de T1.4)
   - [ ] PG max_connections >= 100 (evidencia: `SHOW max_connections`)
   - [ ] Gitea HA: 2 replicas + 200 concurrent clones test (evidencia: T2.1 results)
   - [ ] Harbor HA: 2+ core + Redis externo (evidencia: T2.2 results)
   - [ ] MinIO distributed: 4 drives + failover (evidencia: T2.3 results)
   - [ ] mgmt memory < 50% (evidencia: `kubectl top nodes`)
   - [ ] tenant-health dashboard funcional (evidencia: Grafana screenshot)
   - [ ] Chaos: each shared service kill + recovery <2min (evidencia: Phase 5)
2. Documentar cada item com evidencia concreta
3. Se todos passam: Gate 2a PASSED — Sprint 9 pode iniciar Phase 2b
4. Se algum falha: documentar gap e path para Sprint 9

#### Acceptance Criteria
- [ ] Todos os 7 deliverables validados com evidencia
- [ ] Gate 2a status documentado no ADR-0010
- [ ] Maturity assessment atualizado
- [ ] CLAUDE.md sprint status atualizado

#### DoD
- [ ] Gate 2a PASSED (ou gap documentado com path)
- [ ] Phase 2b desbloqueado para Sprint 9

---

## Phase 5: Chaos Gate (Shared Service Kill + Recovery)

**Objective:** Executar chaos tests contra DO validando recovery <2min para cada shared service. Gate 2a requirement.

### T5.1 — Executar 9 chaos cenarios contra DO

#### Objective
Rodar todos os 9 cenarios de chaos contra clusters DO com shared services HA (apos Phase 2).

#### Evidence
Gate 2a: "chaos test proving each shared service can be killed independently with recovery <2min". 9 cenarios em `infra/tests/chaos/chaos_test.go`. Targets: API (120s), controller (60s), PG storm (recovery), PG latency, ArgoCD (120s), Gitea (com HA agora), build node, simultaneous crash (180s), cell partition.

#### Files to edit
```
infra/tests/chaos/chaos_test.go — ajustes de contexto DO se necessario
docs/runbooks/chaos-gate-results.md — (NEW) resultados + evidencia
```

#### Tasks
1. Configurar kubeconfig com contextos DO
2. Executar cenarios 1-9 sequencialmente com logging
3. Para cada cenario: registrar recovery time exato
4. Gate validation: cada shared service (PG, Gitea, ArgoCD, MinIO, controller) recovery < 2min
5. Documentar resultados com timestamps

#### TDD
```
RED:     Chaos cenarios nunca executados em DO
GREEN:   Executar — recovery times dentro do SLO
VERIFY:  LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v -timeout 30m — 9/9 pass
```

#### Acceptance Criteria
- [ ] 9/9 cenarios executados contra DO
- [ ] Cada shared service: recovery < 2min (Gate 2a)
- [ ] API recovery < 120s
- [ ] Controller recovery < 60s
- [ ] Simultaneous crash recovery < 180s
- [ ] Resultados documentados com evidence

#### DoD
- [ ] 9/9 chaos tests passam
- [ ] `docs/runbooks/chaos-gate-results.md` criado
- [ ] Gate 2a chaos requirement satisfeito

---

### T5.2 — Fix chaos test failures

#### Objective
Corrigir quaisquer falhas dos chaos tests. Bloqueia Gate 2a.

#### Evidence
Historicamente, codigo nao testado em producao tem alta taxa de defeito (padrao "implemented but not wired"). Provavelmente 1-3 cenarios precisam ajuste.

#### Files to edit
```
TBD — dependente de T5.1
```

#### Tasks
1. Analisar logs de cenarios que falharam
2. Identificar root cause (infra config vs code bug vs timeout)
3. Escrever teste de regressao
4. Implementar fix
5. Re-executar cenario

#### Acceptance Criteria
- [ ] Todos os cenarios que falharam agora passam
- [ ] Cada fix tem teste de regressao

#### DoD
- [ ] 9/9 chaos tests passam (re-run completo)

---

## Phase 6: Observability (Paralelo)

**Objective:** Fechar gaps de observability que suportam operacao multi-cell.

### T6.1 — Resolver 17 alerts orfas

#### Objective
Eliminar alerts que referenciam metricas inexistentes (falsa confianca).

#### Evidence
17 PrometheusRules referenciam metricas nunca emitidas. 5 criticos, 12 warning/registry.

#### Files to edit
```
api/internal/metrics/metrics.go — adicionar 5 metricas criticas
infra/helm/monitoring/manifests/prometheusrule-*.yaml — deletar 12 orfas
```

#### Tasks
1. Implementar 5 metricas criticas: `loopzero_gitea_last_backup_timestamp`, `loopzero_build_egress_connectivity`, `loopzero_git_mirror_status`, `loopzero_git_desired_state_divergence`, `loopzero_break_glass_sessions_active`
2. Deletar 12 alerts orfas (warning/registry/compound cujas features nao existem)
3. Validar: `task validate:manifests` passa

#### TDD
```
RED:     TestOrphanAlertsRemoved() — zero alerts referencing non-emitted metrics
GREEN:   Implementar metricas + deletar orfas
VERIFY:  go test ./api/internal/metrics/... + kubeconform
```

#### Acceptance Criteria
- [ ] 5 novas metricas emitidas
- [ ] 12 alerts orfas deletadas
- [ ] Zero alerts referenciando metricas inexistentes

#### DoD
- [ ] 100% dos alerts ativos tem metricas reais

---

### T6.2 — Criar SLO formal + 13 runbooks faltantes

#### Objective
Documentar SLOs de escala em `docs/slo.md` e criar 13 runbooks faltantes.

#### Evidence
SLOs fragmentados em 9 YAML files. 13 alerts ativos sem runbook. Multi-cell precisa de SLOs formais para dimensionamento.

#### Files to edit
```
docs/slo.md — (NEW) SLOs formais com metricas de escala
docs/runbooks/alerts/ — 13 novos runbooks
```

#### Tasks
1. Criar `docs/slo.md` com SLOs de escala: deploy throughput >= 10/min, build p95 < 5min, cell recovery < 2min
2. Criar 13 runbooks para alerts sem documentacao
3. Validar: todos os `runbook_url` apontam para arquivos existentes

#### Acceptance Criteria
- [ ] `docs/slo.md` com SLOs de escala (nao apenas uptime)
- [ ] 13 runbooks criados
- [ ] Zero dead links em runbook_url

#### DoD
- [ ] SLO formal commitado
- [ ] 47/47 alerts ativos tem runbooks

---

### T6.3 — Validar Promtail no build cluster (gap claim era stale)

#### Objective
**Correcao do plano v3.1:** o gap "Build cluster sem logs" em ADR-0010 e claim **stale**. Pulumi ja instala Promtail no build cluster via `infra/pulumi/src/components/build-loki.ts:142-146` e no runtime cluster via `runtime-monitoring.ts:42-50`. Taskfile.yml linha 873 tambem tem `helm upgrade --install promtail` para fluxo task-based. Esta task agora e **validacao** que o que esta instalado esta funcional, nao implementacao do zero.

#### Evidence
- `infra/pulumi/src/components/build-loki.ts:142`: `new k8s.helm.v3.Release("promtail", {...chart: "promtail"...})` no build cluster
- `infra/pulumi/src/components/runtime-monitoring.ts:42`: idem para runtime cluster
- `infra/pulumi/src/components/bootstrap-shared.ts:35`: `promtail: "6.16.6"` (versao pinned)
- `infra/helm/promtail/values.yaml`: ja configurado com remote-write para Loki via `clients.url`
- Taskfile.yml linha 873: helm upgrade promtail (instalacao manual fallback)

#### Files to edit
```
NENHUM — apenas validacao
```

#### Tasks
1. Apos `pulumi up` (Phase -1), verificar promtail running:
   ```bash
   kubectl --context <build-ctx> get pods -A | grep promtail
   kubectl --context <runtime-ctx> get pods -A | grep promtail
   kubectl --context <runtime-2-ctx> get pods -A | grep promtail
   ```
2. Validar query no Loki/Grafana:
   ```
   {cluster="build"} | argo workflows logs visiveis
   {cluster="runtime"} | logs de pods runtime visiveis
   ```
3. Se algum cluster nao tiver promtail: investigar se o Helm release falhou no `pulumi up` (checar `pulumi stack output execution`).

#### Acceptance Criteria
- [ ] Promtail Running em build, runtime, runtime-2 (DaemonSet por cluster)
- [ ] Logs queryaveis no Grafana (mgmt) por `{cluster=...}`
- [ ] Versao pinned 6.16.6 em todos os clusters

#### DoD
- [ ] Build/runtime/runtime-2 logs centralizados
- [ ] Plano v3.1 corrige documentacao: gap nao era real, mas validacao em todos os 3 clusters de aplicacao e o trabalho legitimo

---

## Coverage Matrix

| # | Gap / ADR-0010 Requirement | Task(s) | Resolution |
|---|---|---|---|
| 0 | **Bootstrap from scratch** (descoberta 2026-04-27) | T-1.1, T-1.2, T-1.3, T-1.4, T-1.5 | install pulumi + carregar `.env` + `pulumi preview` + `pulumi up` (faz tudo: 4 DOKS + PG + Spaces + Registry + CF + Helm bootstrap dos 4 clusters + DNS) + validacao via `execution` output + smoke E2E. **NAO usa `loopzero platform install`** (esse e para customers self-hosting). |
| 1 | **L3 >= 95/100** (Gate #1, BLOQUEADOR) | T1.1-T1.4 | Diagnostico + fix iterativo + 5x validation |
| 2 | **PG max_connections >= 100** | T0.1 | Pulumi config: dbNodeClass medium |
| 3 | **Gitea HA** (2 replicas + shared storage + 200 clones) | T2.1 | NFS + 2 replicas + load test |
| 4 | **Harbor HA** (2+ core + Redis HA + S3) | T2.2 | 2 replicas + Redis externo DO |
| 5 | **MinIO distributed** | T2.3 | 4 drives erasure coding + migration |
| 6 | **mgmt memory < 50%** | T0.2 | Pulumi config: mgmtNodeClass medium |
| 7 | **tenant-health dashboard** | T2.4 | Novo Grafana dashboard |
| 8 | **Chaos: shared service kill + recovery <2min** (Gate 2a) | T0.3, T5.1, T5.2 | Chaos Mesh + executar 9 cenarios |
| 9 | **L4 capacity validation** (informa cell-2 sizing) | T3.1 | 500 tenants com capacity extrapolacao |
| 10 | **Gate 2a formal exit** | T4.1 | Checklist com evidencia |
| 11 | 17 alerts orfas (falsa confianca) | T6.1 | Implementar 5 + deletar 12 |
| 12 | SLO formal + 13 runbooks | T6.2 | docs/slo.md + runbooks |
| 13 | Build cluster sem logs | T6.3 | Promtail remote-write (NOTA: ja instalado em build-loki.ts:142 — verificar se gap real existe ou e claim stale) |

**Coverage: 14/14 gaps cobertos (100%)** — Phase -1 bootstrap + 7/7 ADR-0010 deliverables + 6 supporting items

## Global Definition of Done

- [ ] **Phase -1: Bootstrap completo**
  - [ ] Pulumi >= 3.232 instalado e autenticado no backend S3 (DO Spaces)
  - [ ] Stack `dev` selecionado e secrets decifraveis via `PULUMI_CONFIG_PASSPHRASE`
  - [ ] `pulumi preview` revisado e plano coerente
  - [ ] `pulumi up --stack dev` exit 0 — 4 DOKS (mgmt+build+runtime+runtime-2) + PG + Spaces + Registry + CF Tunnel + ~30 Helm releases + DNS provisionados
  - [ ] `pulumi stack output execution` retorna todos os 14+ componentes `true`
  - [ ] Smoke test E2E (login + deploy + rollback) passa contra ambiente DO bootstrapped
- [ ] **Gate 2a: 7/7 deliverables de ADR-0010 completos com evidencia**
  - [ ] L3 5x >= 95/100
  - [ ] PG max_connections >= 100
  - [ ] Gitea HA (2 replicas + 200 concurrent clones)
  - [ ] Harbor HA (2+ core + Redis externo)
  - [ ] MinIO distributed (4 drives + erasure coding)
  - [ ] mgmt memory < 50%
  - [ ] tenant-health Grafana dashboard
- [ ] **Gate 2a: chaos tests passam** (9/9 cenarios, recovery <2min)
- [ ] **L4 executado** com capacity extrapolacao documentada
- [ ] **Observability:** 0 alerts orfas, SLO formal, build logs centralizados
- [ ] `task test:unit` passa
- [ ] `task lint` passa
- [ ] `task validate:manifests` passa
- [ ] CHANGELOG.md atualizado
- [ ] Maturity assessment atualizado (target >= 4.0)
- [ ] **Phase 2b desbloqueado** para Sprint 9 (cell-2 activation)
