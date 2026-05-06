# Plan: Velero Backup & Disaster Recovery

> **Version 1.0** — Implementar Velero como backend de backup/DR para recursos Kubernetes (CRDs, Secrets, ArgoCD state, Gitea PVCs), habilitando restore runbook validado, alertas de freshness, e medição de Recovery Time Objective (RTO). Fecha o gap "validated incident runbooks" bloqueando maturity 4.2 e é pré-requisito para publicação de SLA 99.9%.

## Context

### O que existe hoje

- **PostgreSQL:** DO Managed PG tem backup diário automático + PITR. **Coberto.**
- **Kubernetes resources (CRDs, Secrets, ConfigMaps):** **Zero backup.** Se o etcd do mgmt cluster corromper, todos os Tenant/Application/Environment/ClusterCredential CRs são perdidos permanentemente. São 10 CRDs (`loopzero.io/v1`) que representam o estado declarativo de toda a plataforma.
- **Gitea repositories:** Armazenados em PVCs (NFS RWX em prod, local-path em k3d). Nenhum backup além do que o provedor de PVC oferece. Alert `LoopZeroGiteaBackupStale` está **ORPHAN** (metric `loopzero_gitea_last_backup_timestamp` nunca é emitida — `prometheusrule-critical.yaml:67`).
- **ArgoCD state:** Apps, AppProjects, repo Secrets armazenados em K8s namespace `argocd`. Sem backup.
- **Secrets:** `loopzero-system` namespace contém cluster kubeconfigs, signing keys, registry pull secrets. Sem backup.

### Evidência

- `docs/maturity-assessment.md` — DR gaps bloqueiam maturity 4.2
- `docs/strategy/resilience-roadmap.md` — P1 Velero é prioridade após reordenação (resiliência antes de chaos)
- `prometheusrule-critical.yaml:67` — `LoopZeroGiteaBackupStale` alert marcado ORPHAN
- Nenhum arquivo `infra/helm/velero/` existe
- Nenhum bucket `*-velero-*` em Pulumi L1

### Padrões existentes que este plano segue

- **Helm chart wrapper:** mesmo padrão de `infra/helm/chaos-mesh/` (Chart.yaml com dependency upstream)
- **Helmfile L2:** integração em `infra/helmfile/mgmt.helmfile.yaml.gotmpl` via `needs:` ordering
- **Pulumi L1:** novo bucket via `DigitalOceanStorageProvider.createObjectStorage()` em `infra/pulumi/src/components/infra.ts`
- **PrometheusRule:** alertas em `infra/helm/monitoring/manifests/` com prefixo `LoopZero`
- **Runbooks:** formato de `docs/runbooks/pulumi-state-restore.md` (audiência, steps, troubleshooting)

## Objective

**Done =** Velero operacional no mgmt cluster com backups scheduled de CRDs + ArgoCD + Gitea + Secrets, alertas de freshness no Prometheus, e runbook de restore validado via DR drill que prova RTO < 30 minutos.

Metas específicas e mensuráveis:

1. Velero server instalado no mgmt cluster com DO Spaces (S3) como backend
2. 5 Schedule CRDs cobrindo todos os escopos críticos (CRDs, ArgoCD, Gitea, Secrets, full cluster)
3. Alert `LoopZeroVeleroBackupStale` substituindo o ORPHAN `LoopZeroGiteaBackupStale`
4. Runbook `docs/runbooks/disaster-recovery-restore.md` com procedimento step-by-step
5. DR drill script validando restore + verificação de CRDs restaurados
6. CI workflow `velero-backup-check.yml` rodando diariamente

## ADRs

### D1 — Velero apenas no mgmt cluster

**Decision:** Instalar Velero server somente no management cluster, não nos clusters build/workload/runtime-2.

**Rationale:** O mgmt cluster é o único que contém estado crítico irrecuperável (CRDs, ArgoCD, Gitea, Secrets). Build cluster é stateless (Argo Workflows são efêmeros). Workload clusters são declarativos — estado vive no Gitea (GitOps source of truth) e pode ser re-synced pelo ArgoCD. Backup cross-cluster adiciona complexidade operacional sem valor proporcional (viola KISS).

**Consequences:** Se um workload cluster for destruído, o ArgoCD re-synca tudo automaticamente (já validado pelo cell-2-activation-plan). Se Gitea for perdida E o mgmt cluster morrer, o Velero restore recupera ambos do mesmo backup. Build cluster destruído = zero data loss (builds são reproducible).

### D2 — DO Spaces como backend (S3-compatible)

**Decision:** Usar DigitalOcean Spaces como `BackupStorageLocation`, bucket dedicado `loopzero-{project}-{region}-velero-backups` com versioning habilitado.

**Rationale:** Toda a infraestrutura de storage já usa DO Spaces (5 buckets existentes em Pulumi). O padrão `DigitalOceanStorageProvider.createObjectStorage()` já está implementado. Velero suporta qualquer backend S3-compatible nativamente via plugin `velero-plugin-for-aws`. Criar bucket separado (não reusar `artifacts`) para isolamento de lifecycle policies e IAM (backup bucket = read-heavy, write-scheduled; artifacts bucket = write-heavy, read-heavy).

**Consequences:** Requer 1 novo bucket em Pulumi L1. IAM scoped ao mesmo `LOOPZERO_SPACES_KEY/SECRET` (DO Spaces usa API keys globais por conta, não per-bucket IAM). Lifecycle policy de 30 dias no bucket impede acúmulo de backups antigos.

### D3 — Backup scopes e retention

**Decision:** 5 schedules com retenção diferenciada:

| Scope | Frequência | Retenção | Justificativa |
|-------|-----------|----------|---------------|
| CRDs (10 tipos `loopzero.io/v1`) | 6h | 30 dias | Estado mais crítico, muda frequentemente |
| ArgoCD (`argocd` namespace) | 6h | 30 dias | Apps + AppProjects + repo Secrets |
| Gitea (`loopzero-system` namespace, PVCs) | 24h | 14 dias | PVCs são grandes; daily é suficiente |
| Secrets (`loopzero-system` namespace, apenas Secrets) | 6h | 30 dias | Signing keys, kubeconfigs |
| Full cluster snapshot | 7 dias | 7 dias | Segurança adicional; semanal por custo |

**Rationale:** CRDs e ArgoCD mudam a cada deploy (frequente), justificando 6h. Gitea PVCs são volumosos — backup diário equilibra RPO vs storage cost. Full cluster é safety net semanal. 30 dias de retenção cobre qualquer incidente descoberto tardiamente.

**Consequences:** Custo de storage estimado: ~$2-5/mês em DO Spaces (CRDs são pequenos, PVCs de Gitea ~1GB). Retention policy no Velero auto-prune evita acúmulo.

### D4 — Sem backup de PVs com restic/kopia (V1)

**Decision:** V1 usa apenas backup de recursos Kubernetes (metadata). Backup de volumes (PV data) via restic/kopia é excluído do escopo inicial.

**Rationale:** Os PVCs críticos são Gitea (repos) e MinIO (artifacts). Gitea repos são pequenos (~100MB total em 50 tenants). MinIO artifacts são imutáveis e reproduzíveis (rebuild from source). O overhead de restic DaemonSet + CPU/memory por node + snapshot latency não se justifica para V1 (YAGNI). O Velero `--default-volumes-to-fs-backup=false` desabilita volume backup globalmente.

**Consequences:** Se um PV corromper, os dados do volume são perdidos. Para Gitea isso é mitigável (repos podem ser re-pushed). Para MinIO é irrelevante (artifacts são rebuild outputs). V2 pode adicionar restic se 50+ tenants criarem PVCs grandes.

### D5 — Local dev: Velero com MinIO backend

**Decision:** Em ambiente k3d (standalone/standalone-multi), Velero usa MinIO já existente como backend S3 em vez de DO Spaces.

**Rationale:** k3d não tem acesso a DO Spaces (sem credenciais cloud). MinIO já está instalado no mgmt cluster (`minio.loopzero-system.svc:9000`). Velero suporta qualquer endpoint S3. Isso mantém Rule 10 (local = produção, mesmo Helm chart, diferentes values).

**Consequences:** Values override: `configuration.backupStorageLocation.config.s3Url=http://minio.loopzero-system.svc:9000` para standalone. Bucket `velero-backups` DEVE ser pré-criado no MinIO — adicionar ao MinIO Helm values `buckets: [{name: velero-backups, policy: none, purge: false}]` em `infra/helm/minio/values.yaml` (EC-4: sem este bucket, Velero reporta BSL status=Unavailable em standalone).

### D6 — Encryption at rest delegada ao provider

**Decision:** Não implementar encryption at rest no Velero. Delegar ao DO Spaces (que encripta por padrão).

**Rationale:** DO Spaces usa AES-256 server-side encryption por padrão em todos os objetos. Adicionar Velero-level encryption (via `--features=EnableCSI` + KMS) adiciona complexidade de key management sem valor incremental quando o provider já encripta. O único risco é acesso ao bucket — mitigado por IAM + bucket ACL private.

**Consequences:** Se migrar para provider sem encryption-at-rest nativa, será necessário habilitar Velero encryption. Documentar como pre-condition em ADR.

## Dependency Graph

```
Phase 0 (Pulumi L1: bucket) ──▶ Phase 1 (Helm + Helmfile: Velero server)
                                        │
                                        ▼
                                Phase 2 (Schedules: 5 backup scopes)
                                        │
                                        ▼
                                Phase 3 (Monitoring: alerts + ServiceMonitor)
                                        │
                                        ▼
                                Phase 4 (Runbook + DR drill + CI)
```

Todas as phases são sequenciais — cada uma depende da anterior estar funcional. Phase 0 cria o bucket; Phase 1 instala Velero apontando para o bucket; Phase 2 cria schedules que dependem do Velero estar running; Phase 3 monitora os schedules; Phase 4 valida o fluxo completo.

---

## Phase 0: Pulumi L1 — Bucket de Backup

**Objective:** Criar bucket DO Spaces dedicado para backups Velero, seguindo o padrão existente dos 5 buckets em `infra.ts`.

### T0.1 — Criar bucket `velero-backups` em Pulumi

#### Objective
Adicionar um sexto bucket DO Spaces dedicado a backups Velero com versioning habilitado.

#### Evidence
- `infra/pulumi/src/components/infra.ts:212-276` — 5 buckets existentes seguem padrão `bucketName({...suffix})` + `factory.createObjectStorage()`
- `infra/pulumi/src/core/naming.ts` — `bucketName()` gera nome `loopzero-{project}-{region}-{suffix}`
- Sem bucket de backup existente — zero recovery capability para K8s resources

#### Files to edit
```
infra/pulumi/src/components/infra.ts — adicionar veleroBackupsBucket + export no InfraOutputs
```

#### Deep file dependency analysis
- **`infra.ts`** — Hoje cria 5 buckets + CDN + registry. Mudança: adicionar 6o bucket `velero-backups` com `versioning: true`. Downstream: `InfraOutputs` interface ganha `veleroBackupsBucketName`. Consumers: Helmfile env values (para passar endpoint ao Velero chart).

#### Deep Dives
- **Naming:** `bucketName({project, env, region, suffix: "velero-backups"})` produz `loopzero-{project}-{region}-velero-backups` (ex: `loopzero-infra-sfo3-velero-backups`)
- **Versioning:** `true` — protege contra delete acidental de backups (S3 versioning mantém todas as versões)
- **ACL:** `private` (padrão do provider) — nenhum acesso público
- **forceDestroy:** `true` para dev, `false` para prod (proteger bucket de backup contra destroy acidental)

#### Tasks
1. Adicionar campo `veleroBackupsBucketName` à interface `InfraOutputs`
2. Criar bucket via `factory.createObjectStorage({...suffix: "velero-backups", versioning: true})`
3. Adicionar `protect: isProd` no bucket para prevenir destroy em produção
4. Exportar `veleroBackupsBucketName` no return de `createInfra()`

#### TDD
```
RED:     test_velero_bucket_created() — verifica que o stack cria um SpacesBucket com nome *-velero-backups e versioning enabled
RED:     test_velero_bucket_in_outputs() — verifica que InfraOutputs inclui veleroBackupsBucketName
RED:     test_velero_bucket_protected_in_prod() — verifica protect=true quando environment=prod
GREEN:   Implementar createObjectStorage + InfraOutputs field
REFACTOR: Nenhum esperado
VERIFY:  cd infra/pulumi && npx vitest run src/__tests__/infra.test.ts
```

#### Acceptance Criteria
- [ ] Bucket `loopzero-{project}-{region}-velero-backups` criado com versioning=true
- [ ] `InfraOutputs.veleroBackupsBucketName` exportado
- [ ] Bucket protegido em prod (`protect: true`)
- [ ] `npx vitest run` green (todos os testes existentes + novos)
- [ ] `npx pulumi preview --stack dev` mostra o bucket como recurso novo

#### DoD (Definition of Done)
- [ ] Todos os tasks completados e validados
- [ ] Todos os testes passing
- [ ] Zero warnings no vitest
- [ ] `npx pulumi preview` clean

### T0.2 — Expor bucket endpoint nos Helmfile environment values

#### Objective
Disponibilizar o nome do bucket e endpoint S3 como variáveis para o Helmfile consumir na Phase 1.

#### Evidence
- `infra/helmfile/environments/dev.yaml` — contém referências a buckets existentes (builds, logs, static-assets)
- O Velero chart precisa de `bucket`, `region`, e `s3Url` para configurar o `BackupStorageLocation`

#### Files to edit
```
infra/helmfile/environments/dev.yaml — adicionar veleroBackupsBucket + spacesEndpoint
infra/helmfile/environments/prod.yaml — idem
infra/helmfile/environments/standalone.yaml — usar MinIO local (D5)
infra/helmfile/environments/standalone-multi.yaml — usar MinIO local (D5)
```

#### Deep file dependency analysis
- **`dev.yaml` / `prod.yaml`** — Hoje contêm variáveis de cluster, DB, e storage. Mudança: adicionar `velero.bucket`, `velero.s3Url`, `velero.region`. Downstream: Helmfile template em Phase 1 consome via `.Values.velero.*`.
- **`standalone*.yaml`** — Usam MinIO. Mudança: apontar `velero.s3Url` para `http://minio.loopzero-system.svc:9000`, `velero.bucket` para `velero-backups`.

#### Tasks
1. Adicionar bloco `velero:` em `dev.yaml` com `bucket`, `s3Url`, `region` referenciando Pulumi outputs
2. Replicar em `prod.yaml`
3. Em `standalone.yaml` e `standalone-multi.yaml`, apontar para MinIO local

#### TDD
```
RED:     test_dev_yaml_has_velero_config() — valida que dev.yaml contém velero.bucket, velero.s3Url, velero.region
RED:     test_standalone_yaml_velero_uses_minio() — valida que standalone.yaml velero.s3Url aponta para minio.loopzero-system.svc
GREEN:   Editar os 4 arquivos YAML
REFACTOR: Nenhum esperado
VERIFY:  helmfile -f infra/helmfile/mgmt.helmfile.yaml -e standalone lint 2>&1 | grep -i error (deve ser vazio)
```

#### Acceptance Criteria
- [ ] `dev.yaml` e `prod.yaml` referenciam bucket DO Spaces
- [ ] `standalone*.yaml` referenciam MinIO local
- [ ] `helmfile lint` sem erros para todos os environments

#### DoD (Definition of Done)
- [ ] YAML válido em todos os 4 environments
- [ ] `helmfile lint` green
- [ ] Nenhuma regressão em releases existentes

---

## Phase 1: Helm Chart + Helmfile — Velero Server

**Objective:** Instalar Velero server no mgmt cluster via wrapper chart + Helmfile integration, com `BackupStorageLocation` apontando para o bucket criado na Phase 0.

### T1.1 — Criar Helm chart wrapper `infra/helm/velero/`

#### Objective
Chart wrapper no padrão Chaos Mesh (dependency upstream) para Velero.

#### Evidence
- `infra/helm/chaos-mesh/Chart.yaml` — padrão wrapper: `dependencies: [{name: chaos-mesh, version: 2.7.0, repository: ...}]`
- Velero Helm chart: `vmware-tanzu/velero` no repo `https://vmware-tanzu.github.io/helm-charts`
- Versão estável atual: 7.2.1 (Velero 1.14.x)

#### Files to edit
```
infra/helm/velero/Chart.yaml (NEW) — wrapper chart com dependency upstream
infra/helm/velero/values.yaml (NEW) — values hardened (PSA restricted, resource limits, DO Spaces config)
infra/helm/velero/.helmignore (NEW) — ignore padrão
```

#### Deep file dependency analysis
- **`Chart.yaml`** (NEW) — Define dependency `velero` do repo vmware-tanzu. Padrão idêntico ao chaos-mesh wrapper. Downstream: Helmfile referencia `chart: ../helm/velero`.
- **`values.yaml`** (NEW) — Configura: `initContainers` com plugin AWS (DO Spaces é S3-compatible), `BackupStorageLocation` com bucket/region/s3Url, resource limits, PSA restricted compliance (runAsNonRoot, seccompProfile, drop ALL caps). Downstream: Helmfile pode override valores via `set:`.

#### Deep Dives
- **Plugin:** Velero usa plugins para interagir com cloud providers. `velero-plugin-for-aws` v1.10.x suporta qualquer S3-compatible endpoint (DO Spaces, MinIO). O plugin é um `initContainer` que copia binários para `/target`.
- **BackupStorageLocation:** Recurso CRD do Velero que define onde backups são armazenados. Criado pelo chart via `configuration.backupStorageLocation`.
- **PSA Restricted:** Seguir padrão chaos-mesh: `runAsNonRoot: true`, `runAsUser: 65532`, `seccompProfile: RuntimeDefault`, `allowPrivilegeEscalation: false`, `drop: ["ALL"]`.
- **Node-agent (restic/kopia):** DESABILITADO per D4. `deployNodeAgent: false`.
- **CRD management:** Velero chart instala seus próprios CRDs. Lifecycle independente (não precisa ir para `charts/loopzero-crds`).

#### Tasks
1. Criar `infra/helm/velero/Chart.yaml` com dependency `velero` v7.2.1 do repo vmware-tanzu
2. Criar `infra/helm/velero/values.yaml` com:
   - `velero.initContainers` com plugin AWS v1.10.1
   - `velero.configuration.backupStorageLocation` com provider=aws, bucket/region/s3Url (templated via Helmfile)
   - `velero.deployNodeAgent: false` (D4)
   - `velero.resources` com requests/limits (cpu: 100m/500m, memory: 128Mi/512Mi)
   - `velero.podSecurityContext` + `velero.securityContext` PSA restricted
   - `velero.metrics.serviceMonitor.enabled: false` (Prometheus Operator CRDs podem não estar instaladas — habilitamos na Phase 3 via manifests separados)
   - `velero.schedules: {}` (schedules criados na Phase 2 via values overlay)
3. Criar `infra/helm/velero/.helmignore`
4. Executar `helm dependency update infra/helm/velero/`

#### TDD
```
RED:     test_velero_chart_renders() — helm template infra/helm/velero/ deve produzir Deployment velero sem erros
RED:     test_velero_psa_restricted() — rendered Deployment deve ter securityContext com runAsNonRoot + drop ALL
RED:     test_velero_no_node_agent() — rendered output NÃO deve conter DaemonSet (node-agent disabled)
RED:     test_velero_bsl_configured() — rendered BackupStorageLocation deve ter provider=aws
GREEN:   Criar os 3 arquivos
REFACTOR: Nenhum esperado
VERIFY:  helm template test infra/helm/velero/ --set velero.configuration.backupStorageLocation.bucket=test-bucket --set velero.configuration.backupStorageLocation.config.region=sfo3 | kubeconform --strict
```

#### Acceptance Criteria
- [ ] `helm template` renderiza sem erros
- [ ] `kubeconform --strict` passa em todos os manifests renderizados
- [ ] Nenhum DaemonSet (node-agent) no output
- [ ] SecurityContext PSA-restricted em todos os pods
- [ ] Resource limits definidos

#### DoD (Definition of Done)
- [ ] Chart renderiza corretamente
- [ ] `kubeconform --strict` green
- [ ] `helm dependency update` baixa chart vmware-tanzu/velero

### T1.2 — Integrar Velero no Helmfile mgmt

#### Objective
Adicionar release `velero` no `mgmt.helmfile.yaml.gotmpl` com ordering correto.

#### Evidence
- `infra/helmfile/mgmt.helmfile.yaml.gotmpl` — 430 linhas, wave-based ordering via `needs:`
- Velero precisa de: cert-manager (TLS webhooks), namespace `velero-system`
- Velero NÃO precisa de: Gitea, ArgoCD, API (é infraestrutura pura)
- Posição ideal: Wave -1 (stateful core), junto com PostgreSQL/Gitea/MinIO

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml.gotmpl — adicionar release velero + repo vmware-tanzu na seção repositories
```

#### Deep file dependency analysis
- **`mgmt.helmfile.yaml.gotmpl`** — Hoje tem 20+ releases ordenadas por waves. Mudança: adicionar repo `vmware-tanzu` na seção `repositories:` e release `velero` na Wave -1 com `needs: [cert-manager/cert-manager]`. Downstream: `helmfile sync` instala Velero automaticamente.

#### Deep Dives
- **Wave placement:** Wave -1 é onde ficam componentes stateful de infraestrutura (PostgreSQL, Gitea, MinIO). Velero se encaixa aqui — é infraestrutura de backup, não serviço de plataforma.
- **Conditional install:** Habilitado por padrão em todos os environments. Em standalone, usa MinIO backend (D5). Não há razão para desabilitar — backup é sempre desejado.
- **Values templating:** Helmfile injeta `{{ .Values.velero.bucket }}` etc. dos environment YAML files.

#### Tasks
1. Adicionar repo `vmware-tanzu` em `repositories:` no helmfile
2. Adicionar release `velero` em Wave -1:
   ```yaml
   - name: velero
     namespace: velero-system
     chart: ../helm/velero
     needs:
       - cert-manager/cert-manager
     values:
       - ../helm/velero/values.yaml
     set:
       - name: velero.configuration.backupStorageLocation.bucket
         value: {{ .Values.velero.bucket | quote }}
       - name: velero.configuration.backupStorageLocation.config.region
         value: {{ .Values.velero.region | quote }}
       - name: velero.configuration.backupStorageLocation.config.s3Url
         value: {{ .Values.velero.s3Url | quote }}
   ```
3. Criar Secret com DO Spaces credentials (ou referenciar o existente `minio-credentials`)

#### TDD
```
RED:     test_helmfile_lint_with_velero() — helmfile -e standalone lint deve passar com release velero
RED:     test_velero_needs_certmanager() — rendered helmfile output mostra velero com needs cert-manager
GREEN:   Editar mgmt.helmfile.yaml.gotmpl
REFACTOR: Nenhum esperado
VERIFY:  helmfile -f infra/helmfile/mgmt.helmfile.yaml -e standalone-multi lint
```

#### Acceptance Criteria
- [ ] `helmfile lint` green para todos os 4 environments
- [ ] Release `velero` aparece no output de `helmfile list`
- [ ] `needs:` inclui cert-manager
- [ ] Values de bucket/region/s3Url corretamente injetados do environment

#### DoD (Definition of Done)
- [ ] Helmfile lint green
- [ ] Release ordering correto
- [ ] Nenhuma regressão em releases existentes

### T1.3 — Criar Secret para credenciais S3

#### Objective
Velero precisa de credenciais AWS-style (access key + secret key) para acessar DO Spaces. Criar o Secret no helmfile scripts.

#### Evidence
- `infra/helmfile/scripts/generate-secrets.sh` — gera Secrets para DB, Gitea, MinIO, Harbor, registry pull
- Velero espera Secret no formato AWS credentials file: `[default]\naws_access_key_id=...\naws_secret_access_key=...`
- DO Spaces usa os mesmos `LOOPZERO_SPACES_KEY` / `LOOPZERO_SPACES_SECRET` que MinIO e Loki

#### Files to edit
```
infra/helmfile/scripts/generate-secrets.sh — adicionar geração do Secret velero-s3-credentials
```

#### Deep file dependency analysis
- **`generate-secrets.sh`** — Hoje gera 6 Secrets. Mudança: adicionar 7o Secret `velero-s3-credentials` no namespace `velero-system` com key `cloud` contendo AWS credentials file format. Downstream: Velero Deployment monta este Secret via `configuration.backupStorageLocation.credential`.

#### Deep Dives
- **Formato:** Velero espera um arquivo no formato AWS credentials INI:
  ```
  [default]
  aws_access_key_id=<LOOPZERO_SPACES_KEY>
  aws_secret_access_key=<LOOPZERO_SPACES_SECRET>
  ```
- **Namespace:** `velero-system` (criado pelo Helmfile `createNamespace: true`)
- **Standalone:** MinIO usa credenciais locais (`MINIO_ROOT_USER/PASSWORD`). O script detecta o environment e usa as credenciais corretas.

#### Tasks
1. Chamar `ensure_namespace velero-system` ANTES de criar o Secret (EC-3 fix: namespace pode não existir no first install porque `generate-secrets.sh` roda ANTES do `helmfile sync`)
2. Adicionar bloco no `generate-secrets.sh` que cria Secret `velero-s3-credentials` no namespace `velero-system`
3. Formato: `kubectl create secret generic velero-s3-credentials -n velero-system --from-literal=cloud="[default]\naws_access_key_id=${LOOPZERO_SPACES_KEY}\naws_secret_access_key=${LOOPZERO_SPACES_SECRET}" --dry-run=client -o yaml | kubectl apply -f -`
4. Para standalone: usar `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD`

#### TDD
```
RED:     test_generate_secrets_creates_velero_credential() — script gera Secret velero-s3-credentials no namespace velero-system
RED:     test_velero_credential_format() — Secret contém key "cloud" com formato AWS credentials INI
GREEN:   Editar generate-secrets.sh
REFACTOR: Nenhum esperado
VERIFY:  bash infra/helmfile/scripts/generate-secrets.sh standalone-multi mgmt --dry-run 2>&1 | grep velero
```

#### Acceptance Criteria
- [ ] Secret `velero-s3-credentials` criado em `velero-system`
- [ ] Formato AWS credentials INI correto
- [ ] Funciona para dev/prod (DO Spaces) e standalone (MinIO)
- [ ] Idempotente (`apply -f -` não falha em re-run)

#### DoD (Definition of Done)
- [ ] Script gera Secret corretamente
- [ ] Idempotente em re-runs
- [ ] Nenhuma regressão nos outros 6 Secrets

---

## Phase 2: Backup Schedules

**Objective:** Criar 5 Schedule CRDs do Velero para cobrir todos os escopos de backup definidos em D3.

### T2.1 — Criar values overlay com schedules

#### Objective
Definir os 5 Velero Schedule CRDs via values do Helm chart.

#### Evidence
- D3 define 5 scopes: CRDs, ArgoCD, Gitea, Secrets, Full cluster
- Velero Helm chart suporta `schedules:` no values.yaml para criar Schedule CRDs automaticamente
- Cada schedule usa `includedNamespaces`, `includedResources`, e `labelSelector` para escopo

#### Files to edit
```
infra/helm/velero/values-schedules.yaml (NEW) — overlay com 5 schedules
infra/helmfile/mgmt.helmfile.yaml.gotmpl — adicionar values overlay para schedules
```

#### Deep file dependency analysis
- **`values-schedules.yaml`** (NEW) — Define 5 schedules com cron expressions, TTL, e selectors. Downstream: Velero controller cria Schedule CRDs que disparam Backup CRDs periodicamente.
- **`mgmt.helmfile.yaml.gotmpl`** — Adicionar `- ../helm/velero/values-schedules.yaml` na lista de values do release velero.

#### Deep Dives

**Schedule definitions:**

1. **`loopzero-crds-backup`** — `0 */6 * * *` (every 6h)
   - `includedResources: [tenants.loopzero.io, environments.loopzero.io, loopzeroapplications.loopzero.io, exposedroutes.loopzero.io, platformbootstraps.loopzero.io, gitrepositorybindings.loopzero.io, clustercredentials.loopzero.io, previewenvironments.loopzero.io, appsecrets.loopzero.io, reconciliationreports.loopzero.io]`
   - **Sem `includedNamespaces`** — backup cross-namespace por resource type (EC-1/EC-2 fix: Velero não suporta wildcard em `includedNamespaces`, e tenant namespaces seguem padrão `loopzero-tn-{slug}` via `pkg/naming`. Usar apenas `includedResources` captura CRDs em TODOS os namespaces incluindo `loopzero-system` e `loopzero-tn-*`)
   - `ttl: 720h` (30 dias)

2. **`loopzero-argocd-backup`** — `0 */6 * * *` (every 6h)
   - `includedNamespaces: [argocd]`
   - `ttl: 720h` (30 dias)

3. **`loopzero-gitea-backup`** — `0 2 * * *` (daily 02:00 UTC)
   - `includedNamespaces: [loopzero-system]`
   - `labelSelector: {matchLabels: {app: gitea}}`
   - `ttl: 336h` (14 dias)
   - NOTA: sem PV snapshot (D4) — apenas metadata K8s (StatefulSet, Service, ConfigMap, Secret)

4. **`loopzero-secrets-backup`** — `0 */6 * * *` (every 6h)
   - `includedNamespaces: [loopzero-system]`
   - `includedResources: [secrets]`
   - `ttl: 720h` (30 dias)

5. **`loopzero-full-backup`** — `0 3 * * 0` (Sunday 03:00 UTC)
   - `includedNamespaces: [*]` (all namespaces)
   - `excludedNamespaces: [kube-system, kube-public, kube-node-lease]`
   - `ttl: 168h` (7 dias)

**Invariant:** Todas as schedules devem ter `useOwnerReferencesInBackup: false` (Velero default) para que backups sobrevivam ao delete do Schedule.

#### Tasks
1. Criar `infra/helm/velero/values-schedules.yaml` com os 5 schedules
2. Adicionar overlay reference no Helmfile release velero
3. Validar que cada schedule renderiza corretamente

#### TDD
```
RED:     test_5_schedules_rendered() — helm template com values-schedules.yaml produz exatamente 5 Schedule CRDs
RED:     test_crds_schedule_includes_all_10_types() — schedule loopzero-crds-backup inclui todos os 10 CRD types
RED:     test_schedules_have_ttl() — todos os schedules têm ttl definido
RED:     test_full_backup_excludes_system_ns() — loopzero-full-backup exclui kube-system/kube-public/kube-node-lease
GREEN:   Criar values-schedules.yaml + editar Helmfile
REFACTOR: Nenhum esperado
VERIFY:  helm template test infra/helm/velero/ -f infra/helm/velero/values-schedules.yaml --set velero.configuration.backupStorageLocation.bucket=test | kubeconform --strict
```

#### Acceptance Criteria
- [ ] 5 Schedule CRDs renderizados corretamente
- [ ] Cron expressions corretas para cada scope
- [ ] TTL definido em todos os schedules
- [ ] CRDs backup inclui todos os 10 tipos `loopzero.io/v1`
- [ ] Full backup exclui namespaces de sistema
- [ ] `kubeconform --strict` passa

#### DoD (Definition of Done)
- [ ] Todos os schedules renderizados e validados
- [ ] `kubeconform` green
- [ ] Helmfile lint green com overlay adicionado

---

## Phase 3: Monitoring & Alerting

**Objective:** Adicionar alertas de backup freshness ao Prometheus e substituir o alert ORPHAN `LoopZeroGiteaBackupStale`.

### T3.1 — Criar PrometheusRule para Velero

#### Objective
Alertas de backup freshness que disparam quando backups estão atrasados ou falhando.

#### Evidence
- `prometheusrule-critical.yaml:67` — `LoopZeroGiteaBackupStale` é ORPHAN (metric não existe)
- Velero expõe métricas nativamente: `velero_backup_success_total`, `velero_backup_failure_total`, `velero_backup_last_successful_timestamp`
- Convenção de alertas: prefixo `LoopZero`, severity `critical`/`warning`, annotations com `summary`/`description`/`runbook`

#### Files to edit
```
infra/helm/monitoring/manifests/prometheusrule-velero.yaml (NEW) — alertas de backup
infra/helm/monitoring/manifests/prometheusrule-critical.yaml — remover alert ORPHAN LoopZeroGiteaBackupStale
```

#### Deep file dependency analysis
- **`prometheusrule-velero.yaml`** (NEW) — Novo arquivo de alertas seguindo padrão existente. Downstream: Prometheus carrega automaticamente via ServiceMonitor discovery.
- **`prometheusrule-critical.yaml`** — Remover linhas 64-74 (alert ORPHAN `LoopZeroGiteaBackupStale`). O novo alert `LoopZeroVeleroBackupStale` no arquivo novo substitui este.

#### Deep Dives

**Alertas a criar:**

1. **`LoopZeroVeleroBackupStale`** (critical) — `time() - velero_backup_last_successful_timestamp{schedule=~"loopzero-.*"} > 43200` (12h) for 5m
   - Substitui o ORPHAN `LoopZeroGiteaBackupStale`
   - Cobre TODOS os schedules, não apenas Gitea

2. **`LoopZeroVeleroBackupFailing`** (critical) — `increase(velero_backup_failure_total{schedule=~"loopzero-.*"}[6h]) > 0` for 0m
   - Qualquer falha de backup é crítica

3. **`LoopZeroVeleroServerDown`** (critical) — `up{job="velero"} == 0` for 2m
   - Velero server está down

4. **`LoopZeroVeleroBackupPartial`** (warning) — `velero_backup_partial_failure_total > 0` for 0m
   - Backup completou mas com falhas parciais

#### Tasks
1. Criar `infra/helm/monitoring/manifests/prometheusrule-velero.yaml` com os 4 alertas
2. Remover alert ORPHAN `LoopZeroGiteaBackupStale` de `prometheusrule-critical.yaml`
3. Adicionar comentário de cross-reference no critical.yaml apontando para velero.yaml

#### TDD
```
RED:     test_velero_alerts_valid_yaml() — kubeconform valida prometheusrule-velero.yaml
RED:     test_orphan_alert_removed() — prometheusrule-critical.yaml NÃO contém LoopZeroGiteaBackupStale
RED:     test_4_velero_alerts_exist() — prometheusrule-velero.yaml contém exatamente 4 regras de alerta
GREEN:   Criar/editar os arquivos
REFACTOR: Nenhum esperado
VERIFY:  kubeconform --strict infra/helm/monitoring/manifests/prometheusrule-velero.yaml
```

#### Acceptance Criteria
- [ ] 4 alertas criados com nomes e severidades corretos
- [ ] Alert ORPHAN `LoopZeroGiteaBackupStale` removido
- [ ] `kubeconform --strict` passa no novo arquivo
- [ ] Annotations incluem `runbook` apontando para `docs/runbooks/disaster-recovery-restore.md`

#### DoD (Definition of Done)
- [ ] Alertas válidos via kubeconform
- [ ] ORPHAN eliminado
- [ ] Referências a runbook corretas

### T3.2 — Adicionar Velero ao scrape config do Prometheus

#### Objective
Prometheus precisa scrape as métricas do Velero server para que os alertas da T3.1 funcionem.

#### Evidence
- `infra/helm/monitoring/values-prometheus.yaml` — contém `scrape_configs` com jobs para loopzero-api, ArgoCD, Gitea, etc.
- Velero expõe métricas em `:8085/metrics` por padrão
- Prometheus community chart usa `additionalScrapeConfigs` para jobs custom

#### Files to edit
```
infra/helm/monitoring/values-prometheus.yaml — adicionar scrape job para velero
```

#### Deep file dependency analysis
- **`values-prometheus.yaml`** — Hoje tem ~10 scrape jobs. Mudança: adicionar job `velero` com target `velero.velero-system.svc:8085`. Downstream: Prometheus scrape começa e métricas `velero_*` ficam disponíveis para alertas.

#### Tasks
1. Adicionar scrape job no `additionalScrapeConfigs`:
   ```yaml
   - job_name: velero
     static_configs:
       - targets: ['velero.velero-system.svc:8085']
     metrics_path: /metrics
     scrape_interval: 30s
   ```

#### TDD
```
RED:     test_prometheus_scrapes_velero() — values-prometheus.yaml contém job_name: velero
GREEN:   Editar values-prometheus.yaml
REFACTOR: Nenhum esperado
VERIFY:  helm template test infra/helm/monitoring/ -f infra/helm/monitoring/values-prometheus.yaml | grep -A5 'velero'
```

#### Acceptance Criteria
- [ ] Scrape job `velero` configurado
- [ ] Target aponta para `velero.velero-system.svc:8085`
- [ ] `scrape_interval: 30s` (consistente com outros jobs)

#### DoD (Definition of Done)
- [ ] Job adicionado
- [ ] Prometheus config renderiza corretamente

---

## Phase 4: Runbook + DR Drill + CI

**Objective:** Documentar procedimento de restore, criar script de validação de DR drill, e adicionar CI workflow de backup freshness.

### T4.1 — Escrever runbook de disaster recovery

#### Objective
Runbook completo para restaurar o mgmt cluster a partir de backups Velero.

#### Evidence
- `docs/runbooks/pulumi-state-restore.md` — template de referência (audiência, steps, troubleshooting)
- Nenhum runbook de DR K8s existe
- Maturity 5.0 requer "validated incident runbooks"

#### Files to edit
```
docs/runbooks/disaster-recovery-restore.md (NEW) — runbook de DR completo
```

#### Deep file dependency analysis
- **Arquivo novo.** Nenhuma dependência. Downstream: referenciado por alertas (annotation `runbook`) e pelo CI workflow (T4.3).

#### Deep Dives

**Estrutura do runbook:**

1. **Pre-conditions:** Velero CLI instalado, acesso ao DO Spaces, novo cluster provisionado (Pulumi L1)
2. **Step 1:** Instalar Velero no novo cluster (Helmfile)
3. **Step 2:** Verificar `BackupStorageLocation` conecta ao bucket
4. **Step 3:** Listar backups disponíveis (`velero backup get`)
5. **Step 4:** Restore CRDs primeiro (`velero restore create --from-backup loopzero-crds-backup-<latest>`)
6. **Step 5:** Verificar CRDs restaurados (`kubectl get tenants,environments,loopzeroapplications -A`)
7. **Step 6:** Restore ArgoCD (`velero restore create --from-backup loopzero-argocd-backup-<latest>`)
8. **Step 7:** Verificar ArgoCD Apps restaurados
9. **Step 8:** Restore Secrets (`velero restore create --from-backup loopzero-secrets-backup-<latest>`)
10. **Step 9:** Verificar platform health (`task doctor`)
11. **Step 10:** Smoke test (`loopzero deploy` funciona)

**Troubleshooting:**
- "Backup not found" → verificar BSL connectivity
- "Partially failed restore" → verificar conflitos de namespace/CRD version
- "CRDs restored but controllers crash" → reinstalar operators first

#### Tasks
1. Escrever runbook completo seguindo estrutura acima
2. Incluir seção de troubleshooting com 3+ cenários
3. Incluir estimated RTO para cada step
4. Cross-reference alertas e schedules

#### TDD
```
RED:     test_runbook_exists() — arquivo docs/runbooks/disaster-recovery-restore.md existe
RED:     test_runbook_has_10_steps() — runbook contém pelo menos 10 steps numerados
RED:     test_runbook_has_troubleshooting() — runbook contém seção "Troubleshooting"
GREEN:   Escrever o runbook
REFACTOR: Nenhum esperado
VERIFY:  wc -l docs/runbooks/disaster-recovery-restore.md (deve ter >100 linhas)
```

#### Acceptance Criteria
- [ ] Runbook com 10 steps de restore completos
- [ ] Seção de troubleshooting com 3+ cenários
- [ ] RTO estimado por step
- [ ] Cross-references para alertas e schedules
- [ ] Seguir formato do `pulumi-state-restore.md`

#### DoD (Definition of Done)
- [ ] Runbook escrito e revisado
- [ ] Seguir formato de runbooks existentes

### T4.2 — Criar script de DR drill validation

#### Objective
Script automatizado que valida o procedimento de restore simulando um cenário de DR.

#### Evidence
- `infra/scripts/` — contém scripts de validação (bootstrap-smoke.sh, dogfood-cleanroom-loop.sh)
- DR drill precisa ser reproduzível e automatizado para quarterly execution

#### Files to edit
```
infra/scripts/velero-dr-drill.sh (NEW) — script de DR drill
```

#### Deep file dependency analysis
- **Arquivo novo.** Depende de: Velero CLI, kubectl, cluster acessível com Velero instalado. Downstream: CI workflow (T4.3) executa este script. Runbook (T4.1) referencia este script.

#### Deep Dives

**Flow do drill:**
1. Verificar Velero está healthy (`velero backup-location get` status=Available)
2. Criar backup on-demand dos CRDs (`velero backup create drill-crds-$(date +%s) --include-resources=tenants.loopzero.io,...`)
3. Esperar backup completar (`velero backup wait`)
4. Contar recursos antes do "desastre" (`kubectl get tenants -A -o json | jq '.items | length'`)
5. Deletar 1 CRD de teste (`kubectl delete tenant drill-test` — cria antes se não existir)
6. Restore do backup (`velero restore create drill-restore-$(date +%s) --from-backup drill-crds-*`)
7. Verificar CRD restaurado (`kubectl get tenant drill-test`)
8. Cleanup: deletar tenant de teste
9. Report: PASS/FAIL com timestamps

**Invariantes:**
- O drill NUNCA deleta CRDs reais (cria um tenant de teste `drill-test` e só deleta esse)
- O drill é idempotente (pode rodar múltiplas vezes)
- Timeout: 5 minutos total
- EC-5: O Tenant CR `drill-test` DEVE ter spec válido completo (plan, slug, etc.) para não causar crash no TenantController se os operators estiverem rodando. Usar annotation `loopzero.io/drill: "true"` para identificação e cleanup.

#### Tasks
1. Criar script com as 9 etapas
2. Adicionar flag `--dry-run` que simula sem executar
3. Exit code 0 = PASS, 1 = FAIL
4. Output JSON compatível com CI

#### TDD
```
RED:     test_drill_script_exists() — arquivo infra/scripts/velero-dr-drill.sh existe e é executável
RED:     test_drill_dry_run() — script com --dry-run não cria/deleta recursos
GREEN:   Escrever o script
REFACTOR: Nenhum esperado
VERIFY:  bash infra/scripts/velero-dr-drill.sh --dry-run
```

#### Acceptance Criteria
- [ ] Script executa 9 etapas de DR drill
- [ ] Flag `--dry-run` para simulação segura
- [ ] Exit code 0/1 para PASS/FAIL
- [ ] Output JSON com timestamps e status por etapa
- [ ] Nunca deleta CRDs reais (apenas tenant de teste `drill-test`)
- [ ] Timeout de 5 minutos

#### DoD (Definition of Done)
- [ ] Script funcional e testado
- [ ] `--dry-run` implementado
- [ ] Idempotente

### T4.3 — Criar CI workflow `velero-backup-check.yml`

#### Objective
Workflow GitHub Actions que verifica freshness dos backups diariamente.

#### Evidence
- `.github/workflows/charts-drift-cron.yml` — padrão de cron workflow (every 6h, JSON report, issue on failure)
- `.github/workflows/platform-health-7day-watch.yml` — padrão de health check workflow (hourly)

#### Files to edit
```
.github/workflows/velero-backup-check.yml (NEW) — CI workflow
```

#### Deep file dependency analysis
- **Arquivo novo.** Depende de: kubectl access ao mgmt cluster, Velero CLI. Downstream: GitHub Actions executa diariamente. Cria issue on failure.

#### Deep Dives

**Workflow steps:**
1. Checkout + setup kubectl + setup Velero CLI
2. Connect to mgmt cluster (kubeconfig from GitHub secret `MGMT_KUBECONFIG`)
3. Check `velero backup-location get` — status must be "Available"
4. For each schedule: `velero backup get -l velero.io/schedule-name=loopzero-*-backup --sort-by=.status.startTimestamp` — last backup must be < threshold
5. Run DR drill script (T4.2) with `--dry-run` (smoke only, no mutations)
6. Report: create/update GitHub issue if any check fails

**Cron:** `0 8 * * *` (daily 08:00 UTC)

#### Tasks
1. Criar workflow com steps acima
2. Usar `concurrency: {group: velero-check, cancel-in-progress: true}` para evitar runs paralelos
3. Condition: `if: github.repository == 'loopzero/loopzero'` (não rodar em forks)

#### TDD
```
RED:     test_workflow_valid_yaml() — .github/workflows/velero-backup-check.yml é YAML válido
RED:     test_workflow_has_schedule() — workflow contém trigger schedule com cron
GREEN:   Criar workflow
REFACTOR: Nenhum esperado
VERIFY:  python -c "import yaml; yaml.safe_load(open('.github/workflows/velero-backup-check.yml'))"
```

#### Acceptance Criteria
- [ ] Workflow YAML válido
- [ ] Cron trigger diário
- [ ] Verifica BSL connectivity + backup freshness para todos os schedules
- [ ] Cria GitHub issue on failure
- [ ] `concurrency` configurada

#### DoD (Definition of Done)
- [ ] Workflow criado e YAML válido
- [ ] Segue padrão de workflows cron existentes

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Zero backup de CRDs K8s | T0.1, T2.1 (schedule `loopzero-crds-backup`) | Backup every 6h, 30 days retention |
| 2 | Zero backup de ArgoCD state | T2.1 (schedule `loopzero-argocd-backup`) | Backup every 6h, 30 days retention |
| 3 | Zero backup de Gitea metadata | T2.1 (schedule `loopzero-gitea-backup`) | Backup daily, 14 days retention |
| 4 | Zero backup de Secrets/kubeconfigs | T2.1 (schedule `loopzero-secrets-backup`) | Backup every 6h, 30 days retention |
| 5 | Nenhum DR runbook | T4.1 | Step-by-step restore procedure |
| 6 | Alert ORPHAN LoopZeroGiteaBackupStale | T3.1 | Substituído por LoopZeroVeleroBackupStale |
| 7 | SLA recovery time undocumented | T4.1 | RTO < 30 min documented per step |
| 8 | Nenhum CI de backup freshness | T4.3 | Daily workflow checks |
| 9 | Local dev sem backup capability | D5, T0.2 | MinIO backend para standalone |
| 10 | Nenhum DR drill automatizado | T4.2 | Script reproduzível com PASS/FAIL |
| 11 | DO Spaces bucket para backups | T0.1 | Novo bucket via Pulumi L1 |
| 12 | Velero server não instalado | T1.1, T1.2 | Helm chart + Helmfile integration |
| 13 | S3 credentials para Velero | T1.3 | Secret gerado por generate-secrets.sh |
| 14 | Prometheus não scrape Velero | T3.2 | Scrape job adicionado |

**Coverage: 14/14 gaps cobertos (100%)**

## Global Definition of Done

- [ ] Todas as phases (0-4) completadas
- [ ] Todos os testes passing (vitest Pulumi + kubeconform + helm template + shellcheck)
- [ ] Zero lint warnings
- [ ] `task validate:manifests` passa com novos manifests
- [ ] Alert ORPHAN `LoopZeroGiteaBackupStale` removido
- [ ] 5 Schedule CRDs renderizados e validados
- [ ] Runbook escrito e formatado
- [ ] DR drill script funcional
- [ ] CI workflow válido
- [ ] **`/cross-validation velero-backup-dr` PASSED** — zero CRITICO, zero GRAVE
- [ ] **`/dogfood full` PASSED** (ver seção abaixo)

---

## Dogfood Gate (MANDATORY -- NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure (operator must run before dogfood)
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] Velero pods running in `velero-system` namespace
- [ ] `velero backup-location get` shows status=Available
- [ ] At least 1 backup completed successfully for each of the 5 schedules
- [ ] `LoopZeroVeleroBackupStale` alert NOT firing
- [ ] `LoopZeroGiteaBackupStale` alert REMOVED (no longer in PrometheusRule)
- [ ] DR drill script passes (`infra/scripts/velero-dr-drill.sh` exit 0)
- [ ] No regressions in previously passing flows
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.

---

## Governance Note

This plan is a **T2 change** (new Helm chart, Pulumi infrastructure changes, Helmfile modification, new CI workflow). Per pre-release governance rules, it requires meeting approval before implementation. Submit via `/meeting` with scope: infrastructure + operations.
