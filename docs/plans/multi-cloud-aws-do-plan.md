# Multi-Cloud Installation Plan — AWS + DigitalOcean

**Criado:** 2026-04-18
**Meeting:** MEET-2026-04-18-054
**Objetivo:** Instalacao full multi-cluster com todas as features em AWS e DigitalOcean
**Status:** Phase 0 — Not Started

---

## TL;DR

```
Phase 0:  Fix Alertmanager /dev/null (SRE VETO)              1-2 dias
Phase 1:  Abstrair lock-ins DO (prerequisito)                 1-2 semanas
Phase 2:  AWS Pulumi provider (8 arquivos, ~500 LOC)          2-3 semanas
Phase 3:  Umbrella Helm chart + CLI platform commands          2-3 semanas
Phase 4:  E2E validation AWS + DO multi-cluster               1-2 semanas
Phase 5:  Documentacao + runbooks cloud-agnosticos             1 semana
```

**Total estimado:** 8-12 semanas (1 engineer focado)
**Modelo:** Multi-cluster (3 clusters: mgmt, build, runtime) em cada cloud com todas as features.

---

## Arquitetura Target

```
                    ┌─────────────────────────┐
                    │     loopzero CLI            │
                    │  loopzero platform install  │
                    │  loopzero platform doctor   │
                    │  loopzero deploy            │
                    └────────┬────────────────┘
                             │
              ┌──────────────┴──────────────┐
              │                             │
    ┌─────────▼──────────┐      ┌──────────▼──────────┐
    │   DigitalOcean      │      │   AWS                │
    │                     │      │                      │
    │  mgmt (DOKS)        │      │  mgmt (EKS)          │
    │   ├ loopzero-api        │      │   ├ loopzero-api          │
    │   ├ loopzero-controller │      │   ├ loopzero-controller   │
    │   ├ ArgoCD          │      │   ├ ArgoCD            │
    │   ├ Gitea           │      │   ├ Gitea             │
    │   ├ PostgreSQL (DO) │      │   ├ PostgreSQL (RDS)  │
    │   └ Prometheus      │      │   └ Prometheus        │
    │                     │      │                      │
    │  build (DOKS)       │      │  build (EKS)          │
    │   └ Argo Workflows  │      │   └ Argo Workflows    │
    │     + BuildKit      │      │     + BuildKit        │
    │                     │      │                      │
    │  runtime (DOKS)     │      │  runtime (EKS)        │
    │   └ user apps       │      │   └ user apps         │
    │   └ Traefik         │      │   └ Traefik/ALB       │
    │                     │      │                      │
    │  Storage: DO Spaces │      │  Storage: S3           │
    │  Registry: Harbor   │      │  Registry: ECR/Harbor  │
    │  DNS: DO DNS/CF     │      │  DNS: Route53/CF       │
    └─────────────────────┘      └────────────────────────┘
```

Cada instalacao e independente. PG local por cloud (sem cross-cloud PG).
Cell architecture: cada cloud e 1+ cells. Placement engine distribui tenants.

---

## Phase 0 — Fix Alertmanager (SRE VETO)

**Gate para tudo:** Alertas devem chegar a humanos antes de qualquer multi-cloud.

### P0-01: Wire Alertmanager a receiver real

**Arquivo:** `infra/helm/monitoring/values.yaml` L152-160

**Antes:**
```yaml
receivers:
  - name: default
    webhook_configs:
      - url: 'http://localhost:9093/dev-null'
  - name: critical
    webhook_configs:
      - url: 'http://localhost:9093/dev-null'
```

**Depois:**
```yaml
receivers:
  - name: default
    slack_configs:
      - api_url: '{{ .Values.alertmanager.slackWebhookURL }}'
        channel: '#loopzero-alerts'
        send_resolved: true
  - name: critical
    slack_configs:
      - api_url: '{{ .Values.alertmanager.slackCriticalWebhookURL }}'
        channel: '#loopzero-critical'
        send_resolved: true
    # PagerDuty para producao:
    # pagerduty_configs:
    #   - service_key: '{{ .Values.alertmanager.pagerdutyKey }}'
```

**Criterios de aceite:**
- [ ] Receivers configurados com Slack webhook URL via Helm values
- [ ] PagerDuty como opcao documentada para producao
- [ ] URLs parametrizadas (nao hardcoded)

**DoD:**
- Trigger alerta sintetico → recebido no Slack em <2 min
- `helm upgrade` nao quebra alertas existentes

### P0-02: Validar alertas disparam

- [ ] Trigger `LoopZeroCellDiscoveryEmpty` sintetico → confirmar recebimento
- [ ] Trigger `LoopZeroPostgresDown` sintetico → confirmar recebimento
- [ ] Trigger `LoopZeroArgoCDCircuitOpen` sintetico → confirmar recebimento

**DoD:** 3 alertas distintos recebidos por humano.

---

## Phase 1 — Abstrair Lock-ins DO

**Objetivo:** Codigo e Helm charts 100% cloud-agnosticos. Zero referencia hardcoded a DO.

### P1-01: Registry URL parametrizado

**Arquivos:**
- `infra/helm/argo-workflows/values.yaml` L5 → `{{ .Values.global.registryURL }}`
- `infra/helm/argo-workflows/templates/cwt-clone.yaml` L29 → usa value
- `infra/helm/admission-policies/manifests/vap-restrict-registries.yaml` L30 → parametrizar

**Criterios de aceite:**
- [ ] `registry.digitalocean.com` nao aparece hardcoded em nenhum Helm chart
- [ ] `global.registryURL` e o unico ponto de configuracao
- [ ] Admission policy usa valor do Helm value

**DoD:** `grep -r "registry.digitalocean.com" infra/helm/` retorna 0 resultados (exceto comments)

### P1-02: S3/Storage endpoint parametrizado

**Arquivos:**
- `infra/helm/gitea/values-ha.yaml` L74 → `{{ .Values.global.storageEndpoint }}`
- CWT templates que usam `mc` → parametrizar endpoint

**Criterios de aceite:**
- [ ] `sfo3.digitaloceanspaces.com` nao aparece hardcoded
- [ ] Storage endpoint configuravel via Helm values

**DoD:** `grep -r "digitaloceanspaces" infra/helm/` retorna 0 resultados

### P1-03: Registry credential rotation pluggavel

**Arquivo:** `operators/internal/constants/timeouts.go` L94-122, `operators/cmd/main.go` L259-286

**O que mudar:**
- Criar interface `RegistryCredentialProvider` com `RefreshToken(ctx) (string, time.Duration, error)`
- Implementacoes: `DORegistryProvider`, `HarborRegistryProvider`, `StaticDockerConfigProvider`, `ECRRegistryProvider`
- Selecionar provider via env var `LOOPZERO_REGISTRY_PROVIDER` (do/harbor/static/ecr)
- Remover `DORegistryCredentialsURL` hardcoded

**Criterios de aceite:**
- [ ] `DORegistryCredentialsURL` nao existe mais como constante publica
- [ ] Registry provider selecionado via config
- [ ] ECR provider placeholder (token refresh via AWS SDK)

**DoD:**
- `go build ./operators/...` passa
- Harbor provider funciona (ja parcialmente implementado)
- DO provider funciona (backward compatible)

### P1-04: CWT usa S3 CLI generico (nao `mc`)

**Arquivo:** `infra/helm/argo-workflows/templates/cwt-clone.yaml` e outros CWTs

**O que mudar:**
- Substituir `mc` (MinIO client) por `aws s3` CLI com `--endpoint-url`
- `aws s3` CLI funciona com qualquer S3-compatible (DO Spaces, MinIO, AWS S3)
- Ou: manter `mc` mas configurar endpoint via env var (mc tambem e S3-compatible)

**Criterios de aceite:**
- [ ] CWT funciona com DO Spaces endpoint
- [ ] CWT funciona com AWS S3 endpoint
- [ ] CWT funciona com MinIO endpoint
- [ ] Endpoint configuravel via Helm value

**DoD:** Build pipeline funciona com 3 backends de storage diferentes

### P1-05: Prometheus cross-cluster sem IPs hardcoded

**Arquivo:** `infra/helm/monitoring/values.yaml` L28-41

**O que mudar:**
- Substituir IPs hardcoded por service discovery ou Helm values parametrizados
- `global.buildClusterPrometheusEndpoint`, `global.runtimeClusterPrometheusEndpoint`

**Criterios de aceite:**
- [ ] Zero IPs hardcoded em monitoring values
- [ ] Endpoints configurados via Helm values

### P1-06: Runbooks cloud-agnosticos

**Arquivo:** `docs/runbooks/` (13 runbooks)

**O que mudar:**
- Cada runbook com secoes `{DO}` e `{AWS}` onde comandos diferem
- Remover `doctl` como prerequisito; adicionar alternativa `aws` CLI
- Template: diagnostico generico → remediacao per-cloud

**DoD:** Cada runbook tem instrucoes para DO e AWS

### Phase 1 Gate

```bash
# Zero DO lock-ins em Helm charts
grep -r "registry.digitalocean.com" infra/helm/ | grep -v '#' | wc -l  # must be 0
grep -r "digitaloceanspaces" infra/helm/ | grep -v '#' | wc -l         # must be 0

# Operators build
go build ./operators/... && go vet ./operators/...

# API build
go build ./api/... && go vet ./api/...
```

---

## Phase 2 — AWS Pulumi Provider

**Objetivo:** `pulumi up --stack aws-dev` cria 3 EKS clusters + RDS + S3 + ECR + Route53.

**Referencia:** DO provider tem 427 LOC em 8 arquivos. AWS sera similar com ~600 LOC (EKS e mais complexo que DOKS).

### P2-01: nodes.ts — NodeClass mapping

**Arquivo:** `infra/pulumi/src/providers/aws/nodes.ts`

```typescript
// NodeClass → EC2 instance type
export const nodeClassMap: Record<NodeClass, string> = {
  small:   "t3.medium",    // 2 vCPU, 4 GiB
  medium:  "t3.large",     // 2 vCPU, 8 GiB
  large:   "t3.xlarge",    // 4 vCPU, 16 GiB
  xlarge:  "t3.2xlarge",   // 8 vCPU, 32 GiB
};

// NodeClass → RDS instance type
export const rdsClassMap: Record<NodeClass, string> = {
  small:   "db.t3.micro",
  medium:  "db.t3.small",
  large:   "db.t3.medium",
  xlarge:  "db.t3.large",
};
```

**DoD:** TypeScript compila, mapa cobre todas NodeClass values

### P2-02: network.ts — VPC + Subnets

**Arquivo:** `infra/pulumi/src/providers/aws/network.ts`

Cria: VPC, 3 public subnets, 3 private subnets, IGW, NAT Gateway, route tables.

**Criterios de aceite:**
- [ ] VPC com CIDR configuravel
- [ ] 3 AZs para HA
- [ ] Private subnets para EKS nodes
- [ ] Public subnets para ALB
- [ ] Tags para EKS auto-discovery (`kubernetes.io/cluster/{name}`)

**DoD:** `pulumi preview` mostra recursos corretos

### P2-03: kubernetes.ts — EKS clusters

**Arquivo:** `infra/pulumi/src/providers/aws/kubernetes.ts`

Cria: 3 EKS clusters (mgmt, build, runtime) com managed node groups.

**Criterios de aceite:**
- [ ] EKS cluster com version configuravel
- [ ] Managed node group com autoscaling (min/max do spec)
- [ ] IRSA habilitado (IAM Roles for Service Accounts)
- [ ] kubeconfig output para cada cluster
- [ ] Security group permite comunicacao inter-cluster

**DoD:** 3 EKS clusters criados, `kubectl get nodes` funciona em cada um

### P2-04: database.ts — RDS PostgreSQL

**Arquivo:** `infra/pulumi/src/providers/aws/database.ts`

Cria: RDS PostgreSQL com Multi-AZ.

**Criterios de aceite:**
- [ ] PG 16 (match com DO)
- [ ] Multi-AZ para HA
- [ ] `deletion_protection = true`
- [ ] Connection string output
- [ ] Security group permite acesso dos EKS clusters
- [ ] Automated backups habilitados

**DoD:** `psql` conecta ao RDS a partir de pod no EKS mgmt cluster

### P2-05: storage.ts — S3 buckets

**Arquivo:** `infra/pulumi/src/providers/aws/storage.ts`

Cria: S3 buckets para source bundles, build cache, static assets, Pulumi state.

**Criterios de aceite:**
- [ ] Bucket com versioning habilitado
- [ ] IAM policy para IRSA (service accounts nos EKS clusters)
- [ ] CDN output (CloudFront ou S3 endpoint direto)
- [ ] Endpoint output no formato S3 standard

**DoD:** Upload/download funciona a partir de pod no EKS

### P2-06: dns.ts — Route53

**Arquivo:** `infra/pulumi/src/providers/aws/dns.ts`

Cria: Route53 hosted zone + records.

**Criterios de aceite:**
- [ ] Hosted zone para dominio base
- [ ] Wildcard record para apps (*.apps.domain.com)
- [ ] Suporte a CF DNS override (como DO provider)

**DoD:** DNS resolve para o endpoint correto

### P2-07: registry.ts — ECR

**Arquivo:** `infra/pulumi/src/providers/aws/registry.ts`

Cria: ECR repository para container images.

**Criterios de aceite:**
- [ ] ECR repository com lifecycle policy (limitar images)
- [ ] IAM policy para IRSA (push/pull)
- [ ] Registry URL output

**DoD:** `docker push` funciona para o ECR a partir de build cluster

### P2-08: index.ts — Factory wiring

**Arquivo:** `infra/pulumi/src/providers/aws/index.ts`

Substitui o stub com implementacao real wiring P2-01 a P2-07.

**Criterios de aceite:**
- [ ] Todos os metodos de `CloudProviderFactory` implementados
- [ ] `resolveProvider("aws")` retorna `AwsProviderFactory`
- [ ] `pulumi preview --stack aws-dev` mostra todos os recursos

**DoD:**
```bash
cd infra/pulumi
npx pulumi preview --stack aws-dev  # mostra ~30-40 recursos
npx pulumi up --stack aws-dev       # cria tudo
```

### Phase 2 Gate

- [ ] 3 EKS clusters criados e acessiveis
- [ ] RDS PostgreSQL acessivel dos EKS clusters
- [ ] S3 buckets criados e acessiveis
- [ ] ECR repository criado
- [ ] Route53 hosted zone criada
- [ ] `Pulumi.aws-dev.yaml` stack config criado
- [ ] `npx pulumi up --stack aws-dev` completa sem erros

---

## Phase 3 — Umbrella Helm Chart + CLI

**Objetivo:** `loopzero platform install` funciona em ambas as clouds.

### P3-01: Umbrella Helm chart `loopzero-platform`

**Arquivo:** `infra/helm/loopzero-platform/` (novo chart)

Subchart dependencies:
```yaml
dependencies:
  - name: argocd
    version: "7.x"
    repository: "file://../argocd"
  - name: gitea
    version: "10.x"
    repository: "file://../gitea"
  - name: argo-workflows
    version: "0.x"
    repository: "file://../argo-workflows"
  - name: monitoring
    version: "0.x"
    repository: "file://../monitoring"
  - name: traefik
    version: "28.x"
    repository: "file://../traefik"
  - name: cert-manager
    version: "1.x"
    repository: "file://../cert-manager"
  # loopzero-api e loopzero-controller como subcharts tambem
```

**Global values (o que o usuario configura):**
```yaml
global:
  # Infrastructure endpoints (BYOI)
  database:
    host: ""        # postgres://user:pass@host:5432/loopzero
    secretName: ""  # K8s Secret com connection string
  storage:
    endpoint: ""    # s3://bucket?endpoint=...&region=...
    secretName: ""  # K8s Secret com access key + secret
  registry:
    url: ""         # registry.example.com/loopzero
    secretName: ""  # K8s Secret com docker config
  domain: ""        # deploy.mycompany.com
  provider: ""      # "digitalocean" | "aws" | "generic"

  # Cluster topology
  clusters:
    build:
      context: ""     # kubeconfig context para build cluster
    runtime:
      context: ""     # kubeconfig context para runtime cluster
```

**Criterios de aceite:**
- [ ] `helm install loopzero ./loopzero-platform -f values-aws.yaml` instala todos os componentes
- [ ] `helm install loopzero ./loopzero-platform -f values-do.yaml` instala todos os componentes
- [ ] `helm upgrade` funciona sem downtime
- [ ] CRDs instalados corretamente

**DoD:** Install funcional em DO e AWS com values diferentes

### P3-02: `loopzero platform install` CLI command

**Arquivo:** `cli/src/commands/platform-install.ts` (novo)

```bash
loopzero platform install \
  --kubeconfig ~/.kube/config \
  --context mgmt-cluster \
  --build-context build-cluster \
  --runtime-context runtime-cluster \
  --database "postgres://..." \
  --storage "s3://..." \
  --registry "ecr.amazonaws.com/loopzero" \
  --domain "deploy.mycompany.com"
```

**O que faz:**
1. Valida todos os inputs (conectividade, permissoes, versoes)
2. Gera values.yaml a partir dos inputs
3. Instala CRDs
4. `helm install loopzero-platform` com os values gerados
5. Roda migrations PG
6. Cria ClusterCredential CRs para build e runtime
7. Aguarda todos os pods healthy
8. Imprime credenciais de admin

**Criterios de aceite:**
- [ ] Maximo 8 flags (os valores essenciais)
- [ ] Auto-detecta provider (DO/AWS) pelo kubeconfig context name
- [ ] Valida ANTES de instalar (fail-fast)
- [ ] Zero termos internos no output (Rule 3)
- [ ] Exit code 0 = sucesso, 1 = erro de config, 3 = erro de install

**DoD:**
- Teste: `loopzero platform install` em cluster DO → funciona
- Teste: `loopzero platform install` em cluster AWS → funciona
- Jest tests para parsing de flags e validacao

### P3-03: `loopzero platform doctor` CLI command

**Arquivo:** `cli/src/commands/platform-doctor.ts` (novo)

Checks:
```
✓ Kubernetes API reachable (mgmt cluster)
✓ Kubernetes version >= 1.28
✓ Build cluster reachable
✓ Runtime cluster reachable
✓ PostgreSQL reachable (version 16+)
✓ PostgreSQL advisory locks supported
✓ PostgreSQL max_connections >= 50
✓ S3 storage accessible (PutObject + GetObject)
✓ Container registry accessible (push + pull test)
✓ DNS domain resolves
✓ cert-manager ClusterIssuer ready
✓ Helm v3.14+ installed
✗ ArgoCD not installed (will be installed by loopzero platform install)
```

**Criterios de aceite:**
- [ ] Cada check mostra PASS/FAIL com descricao actionable
- [ ] `--json` output para CI
- [ ] Exit code 0 = all pass, 1 = failures found
- [ ] Roda ANTES do install (pre-flight)

**DoD:** Teste em cluster sem LoopZero → mostra o que falta

### P3-04: `loopzero platform status` CLI command

**Arquivo:** `cli/src/commands/platform-status.ts` (novo)

```
Platform Status — deploy.mycompany.com

Component          Status    Version    Pods
─────────────────────────────────────────────
API                ✓ Ready   v0.1.0     2/2
Build System       ✓ Ready   v3.5.0     1/1
Deploy Pipeline    ✓ Ready   v2.13.3    3/3
Source Storage     ✓ Ready   v1.22.6    1/1
Database           ✓ Ready   PG 16      —
Object Storage     ✓ Ready   S3         —
Registry           ✓ Ready   Harbor     —
Monitoring         ✓ Ready   v2.54.0    1/1
Certificate Mgmt   ✓ Ready   v1.14.5    1/1

Cells: 1 discovered (cell-default)
Tenants: 12 active
Last deploy: 3 minutes ago (success)
```

**Criterios de aceite:**
- [ ] Zero termos internos (ArgoCD = "Deploy Pipeline", Gitea = "Source Storage")
- [ ] `--json` output
- [ ] Mostra cells e tenants count

### P3-05: `loopzero platform upgrade` CLI command

**Arquivo:** `cli/src/commands/platform-upgrade.ts` (novo)

**O que faz:**
1. Verifica versao atual vs versao target
2. Mostra changelog de mudancas
3. Aplica CRD updates primeiro
4. `helm upgrade` com novos values
5. Roda migrations PG se necessario
6. Valida health apos upgrade
7. Rollback automatico se health falha

**DoD:** Upgrade de v1.0 → v1.1 funcional com rollback testado

---

## Phase 4 — E2E Validation

### P4-01: Full deploy cycle em DO

```bash
# Pulumi cria infra DO
cd infra/pulumi && npx pulumi up --stack do-dev

# Instala LoopZero
loopzero platform install --kubeconfig ... --database ... --storage ... --registry ... --domain ...

# Valida
loopzero platform doctor
loopzero platform status
loopzero deploy  # app live
loopzero rollback
loopzero logs
```

**DoD:** Todo o fluxo funciona end-to-end em DO

### P4-02: Full deploy cycle em AWS

Mesmo fluxo que P4-01 mas com `--stack aws-dev`.

**DoD:** Todo o fluxo funciona end-to-end em AWS

### P4-03: Multi-cell test (DO cell + AWS cell)

- Cell-default em DO
- Cell-aws-01 em AWS
- Tenants distribuidos via PlacementEngine
- `loopzero deploy` funciona em ambas cells

**DoD:** Tenant em cell DO deploya com sucesso. Tenant em cell AWS deploya com sucesso.

### P4-04: Capacity test em AWS

Rodar capacity suite (`run-capacity.sh`) em AWS para comparar com DO baselines.

**DoD:** 5 capacity tests passam em AWS com resultados comparaveis a DO

---

## Phase 5 — Documentacao

### P5-01: Installation guide (DO)

`docs/guides/install-digitalocean.md`

### P5-02: Installation guide (AWS)

`docs/guides/install-aws.md`

### P5-03: Architecture guide (multi-cloud)

`docs/guides/multi-cloud-architecture.md`

### P5-04: Runbooks per-cloud

Atualizar 13 runbooks com secoes DO/AWS

---

## Resumo de Arquivos

### Novos

| Arquivo | Phase | Descricao |
|---------|-------|-----------|
| `infra/pulumi/src/providers/aws/nodes.ts` | P2 | NodeClass → EC2/RDS mapping |
| `infra/pulumi/src/providers/aws/network.ts` | P2 | VPC + subnets |
| `infra/pulumi/src/providers/aws/kubernetes.ts` | P2 | EKS clusters |
| `infra/pulumi/src/providers/aws/database.ts` | P2 | RDS PostgreSQL |
| `infra/pulumi/src/providers/aws/storage.ts` | P2 | S3 buckets |
| `infra/pulumi/src/providers/aws/dns.ts` | P2 | Route53 |
| `infra/pulumi/src/providers/aws/registry.ts` | P2 | ECR |
| `infra/helm/loopzero-platform/` | P3 | Umbrella Helm chart |
| `cli/src/commands/platform-install.ts` | P3 | Install command |
| `cli/src/commands/platform-doctor.ts` | P3 | Pre-flight validation |
| `cli/src/commands/platform-status.ts` | P3 | Platform health |
| `cli/src/commands/platform-upgrade.ts` | P3 | Upgrade with rollback |
| `infra/pulumi/Pulumi.aws-dev.yaml` | P2 | AWS dev stack config |
| `docs/guides/install-aws.md` | P5 | AWS install guide |
| `docs/guides/install-digitalocean.md` | P5 | DO install guide |

### Modificados

| Arquivo | Phase | Mudanca |
|---------|-------|---------|
| `infra/helm/monitoring/values.yaml` | P0 | Alertmanager receivers reais |
| `infra/helm/argo-workflows/values.yaml` | P1 | Registry URL parametrizado |
| `infra/helm/argo-workflows/templates/cwt-*.yaml` | P1 | Storage endpoint parametrizado |
| `infra/helm/gitea/values-ha.yaml` | P1 | MinIO endpoint parametrizado |
| `infra/helm/admission-policies/manifests/` | P1 | Registry prefix parametrizado |
| `operators/internal/constants/timeouts.go` | P1 | DO registry constants removidos |
| `operators/cmd/main.go` | P1 | Registry provider pluggavel |
| `infra/pulumi/src/providers/aws/index.ts` | P2 | Stub → implementacao real |
| `infra/pulumi/src/components/infra.ts` | P2 | `resolveProvider("aws")` funcional |
| `cli/src/index.ts` | P3 | Platform commands registrados |
| `docs/runbooks/` | P5 | Secoes DO/AWS em cada runbook |

---

## Dependencias

```
P0-01 (Alertmanager) ━━━━━━━━━━━━ VETO GATE ━━━━━━━━━━━━━

P1-01 (registry) ──┐
P1-02 (storage) ───┤
P1-03 (credentials)┼──► Phase 1 Gate (zero DO lock-ins)
P1-04 (CWT S3) ────┤
P1-05 (monitoring) ─┘

Phase 1 Gate ──► P2-01..P2-08 (AWS provider, parallelizable)

P2-08 (factory) ──► Phase 2 Gate (pulumi up aws works)

Phase 1 Gate ──► P3-01 (Helm chart, independent of P2)
Phase 1 Gate ──► P3-02..P3-05 (CLI commands, independent of P2)

Phase 2+3 Gates ──► P4-01..P4-04 (E2E validation)

Phase 4 Gate ──► P5-01..P5-04 (docs)
```

**Nota:** Phase 2 (AWS Pulumi) e Phase 3 (Helm + CLI) podem rodar em paralelo apos Phase 1.

---

## Custos Estimados (AWS dev stack)

| Recurso | Tipo | Custo/mes |
|---------|------|:---------:|
| 3 EKS clusters | Control plane | $225 ($75 × 3) |
| 6 EC2 nodes (2/cluster, t3.medium) | Compute | ~$250 |
| 1 RDS PostgreSQL (db.t3.small) | Database | ~$30 |
| 3 S3 buckets | Storage | ~$5 |
| 1 ECR repository | Registry | ~$5 |
| 1 Route53 hosted zone | DNS | ~$1 |
| NAT Gateway | Networking | ~$45 |
| **Total** | | **~$560/mes** |

Comparacao DO dev stack: ~$180/mes. AWS e ~3x mais caro por causa do EKS control plane pricing ($75/cluster).

---

## Referencias

- **MEET-054:** `docs/meetings/records/MEET-2026-04-18-054.md`
- **ADR-0008:** Cell-Based Architecture
- **DO provider:** `infra/pulumi/src/providers/digitalocean/` (427 LOC, referencia)
- **ISP contracts:** `infra/pulumi/src/contracts/` (6 interfaces)
- **Cell overview:** `docs/plans/cell-architecture-overview.md`
- **Multi-cell roadmap:** `docs/plans/multi-cell-default-roadmap.md`
