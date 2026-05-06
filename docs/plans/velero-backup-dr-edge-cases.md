# Edge Case Review — velero-backup-dr

Data: 2026-05-06
Plano: docs/plans/velero-backup-dr-plan.md
Tasks analisadas: 11 (T0.1, T0.2, T1.1, T1.2, T1.3, T2.1, T3.1, T3.2, T4.1, T4.2, T4.3)
Edge cases encontrados: 7 (MUST FIX: 3, SHOULD TEST: 2, DOCUMENT: 2)

## MUST FIX

### EC-1: CRDs backup namespace selector `tenant-*` não existe — namespaces reais são `loopzero-tn-*`

- **Task afetada:** T2.1
- **Família:** Resource
- **Cenário:** O plano define `includedNamespaces: [loopzero-system, tenant-*]` para o schedule `loopzero-crds-backup`. Mas os tenant namespaces no LoopZero seguem o padrão `loopzero-tn-{slug}` (via `pkg/naming/naming.go:66 TenantNamespace()`). Resultado: CRDs que vivem em tenant namespaces (LoopZeroApplication, Environment, ExposedRoute, PreviewEnvironment, GitRepositoryBinding) NUNCA serão backupeados porque `tenant-*` não casa com nenhum namespace real.
- **Impacto:** Perda silenciosa de CRDs em tenant namespaces — exatamente o estado mais crítico da plataforma. O backup parece funcionar (status=Completed) mas os recursos mais importantes não estão incluídos.
- **Fix sugerido:** Trocar `includedNamespaces: [loopzero-system, tenant-*]` por `includedNamespaces: [loopzero-system, loopzero-tn-*]` no schedule `loopzero-crds-backup`. Adicionalmente, verificar se Velero suporta wildcard em `includedNamespaces` — se não, usar `includedResources` com os 10 CRD types sem filtro de namespace (backup cross-namespace por resource type).

### EC-2: Velero `includedNamespaces` não suporta glob/wildcard — apenas nomes exatos

- **Task afetada:** T2.1
- **Família:** Boundary
- **Cenário:** Velero `Schedule.spec.includedNamespaces` aceita apenas nomes exatos de namespaces, não padrões glob como `loopzero-tn-*`. A API do Velero não documenta suporte a wildcards neste campo. Resultado: o backup de CRDs ficaria limitado a `loopzero-system` apenas, perdendo todos os CRDs em tenant namespaces.
- **Impacto:** Mesmo com EC-1 corrigido para `loopzero-tn-*`, o wildcard não funciona. Backup silenciosamente incompleto.
- **Fix sugerido:** Para o schedule `loopzero-crds-backup`, remover `includedNamespaces` e usar APENAS `includedResources` com os 10 CRD types (`tenants.loopzero.io`, `environments.loopzero.io`, etc.). Velero faz backup cross-namespace quando resource types são especificados sem filtro de namespace. Isso captura todos os CRDs independente de em qual namespace vivem. Adicionar `includedNamespaces: ['*']` explicitamente se necessário pelo chart.

### EC-3: Namespace `velero-system` pode não existir quando `generate-secrets.sh` roda

- **Task afetada:** T1.3
- **Família:** Timing
- **Cenário:** O script `generate-secrets.sh` roda ANTES do `helmfile sync` (ver comentário no helmfile: "Called by the CLI orchestrator BEFORE this helmfile runs"). O `createNamespace: true` do Helmfile só cria `velero-system` quando o release `velero` é processado. Se `generate-secrets.sh` tentar criar o Secret em `velero-system` antes do namespace existir, o `kubectl apply` falha.
- **Impacto:** Bootstrap falha no primeiro install (fresh cluster). Re-runs funcionam porque o namespace já existe.
- **Fix sugerido:** O script já tem `ensure_namespace()` que faz `kubectl create namespace --dry-run=client | kubectl apply -f -`. Basta chamar `ensure_namespace velero-system` antes de `apply_secret velero-s3-credentials velero-system ...`. Verificar que o plano T1.3 inclui esta chamada.

## SHOULD TEST

### EC-4: MinIO bucket `velero-backups` não existe automaticamente em standalone

- **Task afetada:** T0.2, T1.1
- **Família:** Resource
- **Cenário:** Em standalone (k3d), o plano diz que Velero usa MinIO como backend (D5). Mas o bucket `velero-backups` precisa existir no MinIO antes do Velero tentar acessá-lo. Os outros buckets MinIO são criados pelo MinIO chart via `buckets:` no values. Se o bucket não for pré-criado, Velero reporta `BackupStorageLocation` status=Unavailable.
- **Teste sugerido:** `test_standalone_minio_velero_bucket_exists` — após `helmfile sync` em standalone, verificar que o MinIO contém o bucket `velero-backups` (via `mc ls minio/velero-backups` ou equivalente).

### EC-5: DR drill cria Tenant CR `drill-test` — pode triggar TenantController reconcile

- **Task afetada:** T4.2
- **Família:** State
- **Cenário:** O DR drill script cria um Tenant CR `drill-test` para teste de restore. Se os operators estão rodando, o TenantController vai reconciliar esse CR e tentar criar namespace, PG schema, etc. Depois o drill deleta o CR, triggando cleanup. Isso pode criar ruído em logs e métricas, ou falhar se o controller espera campos spec obrigatórios.
- **Teste sugerido:** `test_drill_tenant_has_valid_spec` — verificar que o Tenant CR criado pelo drill tem todos os campos spec obrigatórios (plan, slug, etc.) para não causar crash no controller. Ou usar annotation `loopzero.io/skip-reconcile: "true"` se os controllers respeitam isso.

## DOCUMENT

### EC-6: DO Spaces API keys são globais por conta — sem isolamento per-bucket

- **Task afetada:** T1.3
- **Risco aceito:** DO Spaces usa API keys globais (não per-bucket IAM como AWS S3). O mesmo `LOOPZERO_SPACES_KEY/SECRET` que acessa builds, logs, e artifacts também acessa backups. Se a key vazar, todos os buckets ficam comprometidos, incluindo os backups. Isso é uma limitação do DO Spaces, não do plano. Mitigação real: migrar para IAM granular quando DO suportar (ou usar um provedor S3 que suporte). Para V1, o risco é aceito porque a alternativa (conta DO separada para backups) adiciona complexidade operacional desproporcional.

### EC-7: Backup de Secrets contém dados sensíveis em plaintext no bucket S3

- **Task afetada:** T2.1
- **Risco aceito:** O schedule `loopzero-secrets-backup` faz backup de TODOS os Secrets do namespace `loopzero-system`, que incluem DB passwords, Gitea tokens, signing keys, kubeconfigs. Esses dados ficam no bucket DO Spaces, encriptados at-rest pelo provider (D6), mas acessíveis a qualquer pessoa com a API key. O `generate-secrets.sh` já faz backup local (linha 69: `backup_secrets()`), mas o Velero backup vai para S3 remoto. O risco é aceito porque: (a) DO Spaces encripta at-rest, (b) bucket é private ACL, (c) API key já é necessária para operar a plataforma. Documentar no runbook que restore de backups deve ser tratado como operação sensível com acesso auditado.

## Padrões Sistêmicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | **Sim** | EC-1/EC-2: schedule `loopzero-crds-backup` com namespace selector que não casa com namespaces reais — backup "funciona" mas não captura CRDs |
| Correct code in wrong place | Não | — |
| Project name vs ID | Não | Plano não toca CLI/API |
| ArgoCD notifiers not services | Não | — |
| Single ArgoCD App per tenant | Não | — |
| CF scan imports conflicting apex | Não | — |

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 | 0 | 0 | 0 | 0 |
| T0.2 | 1 | 0 | 1 (EC-4) | 0 |
| T1.1 | 1 | 0 | 1 (EC-4) | 0 |
| T1.2 | 0 | 0 | 0 | 0 |
| T1.3 | 2 | 1 (EC-3) | 0 | 1 (EC-6) |
| T2.1 | 3 | 2 (EC-1, EC-2) | 0 | 1 (EC-7) |
| T3.1 | 0 | 0 | 0 | 0 |
| T3.2 | 0 | 0 | 0 | 0 |
| T4.1 | 0 | 0 | 0 | 0 |
| T4.2 | 1 | 0 | 1 (EC-5) | 0 |
| T4.3 | 0 | 0 | 0 | 0 |

**Veredicto: PLANO PRECISA DE AJUSTE** — 3 MUST FIX items afetam a corretude dos backups (EC-1/EC-2 silenciosamente perdem CRDs críticos) e a robustez do bootstrap (EC-3 falha em fresh install).
