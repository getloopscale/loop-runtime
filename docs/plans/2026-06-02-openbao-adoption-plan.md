# Plan: OpenBao Adoption — Platform Secrets Posture Uplift

> **Version 1.3** (2026-06-02 F1 engineering closure) — All 10 F1 tasks (T1.1-T1.10) implemented + committed in 10 sequential commits (d5184331 → b879e885). Engineering-side complete; operator-time gates remain (LIVE k3d validation, AWS prod KMS provision, prod helmfile sync, Shamir ceremony, root token revoke). Full status em wiki canonical plan §Amendments v1.2→v1.3.
>
> **Version 1.2** (2026-06-02 post second `/grill-me` F1 execution session) — 6 operational decisions ratified specifying HOW F1 tasks execute (k3d-first paralelo; AWS bootstrap long-lived key + us-east-1; image+storage+Renovate bundle; NetworkPolicy egress single permissive LoopZero T8.5b pattern; cert Issuer reuse self-signed-issuer; coverage tiered 95% pkg/vault + 90% rest). T1-T2 scope refinement — zero DoD/ADR changes. Full record em [`.claude/knowledge-base/meetings/MEET-ASYNC-AMENDMENT-2026-06-02-001-openbao-f1-execution.md`](../../.claude/knowledge-base/meetings/MEET-ASYNC-AMENDMENT-2026-06-02-001-openbao-f1-execution.md). F1 fully specified; ready to begin.
>
> **Version 1.1** (2026-06-02 post `/edge-case-plan`) — Edge case review found 22 edge cases (12 MUST FIX + 6 SHOULD TEST + 4 DOCUMENT) incorporated. **Full amendments table + code snippets em [`.claude/knowledge-base/plans/2026-06-02-openbao-adoption-plan.md` § Amendments](../../.claude/knowledge-base/plans/2026-06-02-openbao-adoption-plan.md#amendments)** (skill-compliant wiki version is canonical). See [`.claude/knowledge-base/plans/2026-06-02-openbao-adoption-edge-review.md`](../../.claude/knowledge-base/plans/2026-06-02-openbao-adoption-edge-review.md) for full rationale per item.
>
> **Version 1.0** (2026-06-02) — Ratified via sponsor-direct-approval (`MEET-2026-06-02-vault-adoption-reversal.md`). Implements the 16 architectural decisions consolidated in `ADR-2026-06-02-openbao-adoption.md`. Closes 7 🔴 bugs surfaced by deep review infra Categoria A 2026-06-02 (loopzero-api SA cluster-wide secrets RBAC, master encryption key blast radius, plaintext fallback default, hardcoded Ory defaults, Pulumi.prod.yaml empty post-GHA-removal, cosign without rotation, GitHub App PEM as env var, scoped kubeconfigs legacy SA tokens). Replaces ADR-2026-05-20-D6 partially (SOPS+age retained for at-rest-in-git E1; OpenBao takes E2-E5).

## Context

### What exists today

- 7 🔴 bugs em platform secrets posture (deep review 2026-06-02). Most severe: `charts/loopzero-api/templates/clusterrole.yaml:35-37` gives loopzero-api SA `secrets * cluster-wide` capability, combined with `LOOPZERO_SECRET_ENCRYPTION_KEY` living as plain K8s Secret in `loopzero-system` namespace → comprometer 1 pod loopzero-api decifra TODOS tenant secrets B2 (Categoria B2 do framework dos 5 eixos).
- ESO 0.14.3 deployed mas SEM `ClusterSecretStore` configurado (grep `infra/helm/external-secrets/` retorna apenas `cf-origin-ca-externalsecret.yaml` consuming a SecretStore that probably doesn't exist).
- AES-256-GCM encryption em `secrets.encrypted_value` (PG) com chave em RAM do processo loopzero-api (`api/internal/db/secret_store.go:35-72`). Sem envelope encryption, sem KMS, sem per-tenant DEK.
- `charts/loopzero-api/values.yaml:56` empty `encryptionKey: ""` default; template `charts/loopzero-api/templates/secret.yaml:37` `{{- if .Values.secrets.encryptionKey }}` guard skipa emissão da key se vazia → loopzero-api cai no caminho "encryptionKey is nil → plaintext (local dev only)" silently in prod.
- Ory Kratos cookie/cipher (`infra/helm/ory-kratos/values.yaml.gotmpl:154-158`) e Hydra system/cookie (`:96-98`) hardcoded como `"loopzero-dev-*"` literals. Comentário diz "production overrides via Pulumi" mas `Pulumi.prod.yaml` está vazio (workflow `infra-deploy.yml` deletado 2026-05-23).
- cosign keypair gerado uma vez por `generate-build-secrets.sh:66-90` e preservado idempotente. Sem rotação automática. Drill workflow deletado 2026-05-23.
- GitHub App PEM lido como env var (`api/cmd/setup_github.go:63 GITHUB_APP_PRIVATE_KEY`); PEM completo vaza por `/proc/<pid>/environ`.
- `scoped-kubeconfig.sh:117-128` cria legacy long-lived SA-Secret-token (deprecated K8s ≥1.24). Apenas H13 para `cell-default` usa TokenRequest API moderna.

### What's broken

A combinação destes bugs estabelece um **cenário de compromisso em cascata**:
1. Atacante compromete um loopzero-api pod (vulnerabilidade no código + container escape OR supply chain).
2. SA do loopzero-api tem `secrets *` cluster-wide → atacante lê `loopzero-system/loopzero-api-secrets` Secret.
3. Esse Secret contém `LOOPZERO_SECRET_ENCRYPTION_KEY` em base64.
4. Atacante usa esse key para descifrar TODAS as linhas em PG `secrets` table (AES-256-GCM, single master).
5. Tenants Acme, MegaCorp, IndieCo todos vazam simultaneamente.

Esse é o cenário que OpenBao adoption fecha em F2-F5.

## Objective

**Done = zero 🔴 bugs from deep review infra Categoria A. OpenBao HA in prod with 16 ratified decisions implemented. Migration of existing secrets completed. AES code path deleted.**

Specific measurable goals (each gated by quality check):

1. loopzero-api SA Role split: NO ClusterRole grants `secrets *` cluster-wide. Replaced by namespace-scoped Role + minimal cluster-scoped CRDs read.
2. `LOOPZERO_SECRET_ENCRYPTION_KEY` env var DELETED from loopzero-api Deployment. Vault transit per-tenant keys handle all encryption.
3. `GITHUB_APP_PRIVATE_KEY` env var DELETED from loopzero-api Deployment. Vault KV + projected file.
4. cosign passphrase removed from K8s Secret `loopzero-build/cosign`. Lives in Vault KV `secret/data/build/cosign-passphrase`.
5. Ory cookie/cipher/system secrets in Vault KV; helm template uses `required` gate; ESO injects values at runtime.
6. `Pulumi.prod.yaml` documentation updated; Pulumi Vault provider used for IaC secrets.
7. Scoped kubeconfigs use Vault kubernetes engine (dynamic SA tokens TTL 24h) — NO legacy long-lived SA-Secret-token in any cell.
8. Audit log retention 90d Loki + 1y S3 archive operational; PrometheusRule alerts firing on test scenarios.
9. Vault outage scenarios validated: 1-node down (no impact), 2-node down (read-only), all-down (cluster restore from snapshot ~15min RTO).
10. Quarterly Shamir break-glass drill scheduled + first drill completed.

## ADRs

This plan operates under 2 ADRs ratified 2026-06-02:

- **ADR-2026-06-02-D6-REVOKE** — rescinds ADR-2026-05-20-D6 (partial: SOPS retained for E1).
- **ADR-2026-06-02-OPENBAO-ADOPTION** — consolidates 16 architectural decisions Q1-Q16 from grill session.

Any deviation during execution requires new MEET-ASYNC-AMENDMENT record per `.claude/rules/pre-release-governance.md` § Plan Amendment Policy.

## Phases

### F0 — Governance (DONE 2026-06-02)

| Task | Deliverable | DoD |
|---|---|---|
| F0.1 | `MEET-2026-06-02-vault-adoption-reversal.md` | committed to `.claude/knowledge-base/meetings/` |
| F0.2 | `ADR-2026-06-02-D6-revoke.md` | committed to `.claude/knowledge-base/decisions/` |
| F0.3 | `ADR-2026-06-02-openbao-adoption.md` | committed to `.claude/knowledge-base/decisions/` |
| F0.4 | This plan v1.0 | committed to `docs/plans/` |
| F0.5 | `index.md` + `log.md` updated | follows auto-docs.md rule 6 |

### F1 — Deploy OpenBao HA (1 week)

| Task | Deliverable | DoD |
|---|---|---|
| F1.1 | Pulumi resource AWS KMS key | `infra/pulumi/src/aws-kms.ts` — 1 symmetric key + IAM user `openbao-unseal` with policy `kms:Decrypt` only |
| F1.2 | Pulumi resource DO Spaces bucket `loopzero-openbao-backups` em NYC3 | `infra/pulumi/src/do-spaces-backups.ts` |
| F1.3 | `infra/helm/bank-vaults-operator/` chart | Chart.yaml + values.yaml + (depends on upstream Banzai chart); resource limits per Q4 |
| F1.4 | `infra/helm/openbao/` chart skeleton | BaoConfiguration CRD + Service + cert-manager Certificate + NetworkPolicy + PrometheusRule + ServiceMonitor |
| F1.5 | `values-standalone.yaml` (k3d dev-mode) per Q9 | `replicas:1, storage:inmemory, devMode:true, rootToken:"loopzero-dev-vault-root"` |
| F1.6 | `values-prod.yaml` (HA + AWS KMS unseal) per Q4 | `replicas:3, storage:raft, devMode:false, unseal.type:awskms` |
| F1.7 | Helmfile `mgmt.helmfile.yaml.gotmpl` releases | bank-vaults-operator (needs cert-manager) + openbao (needs bank-vaults-operator) |
| F1.8 | OpenBao init em k3d local | gera 5 Shamir shards (operator captures offline, distributes per Q13); root token revoked após bootstrap |
| F1.9 | Quality gate `task quality:openbao-prod-safe` | grep ensures values-prod.yaml não tem dev literals OR rootToken |
| F1.10 | LIVE k3d: deploy + smoke test | `bao status` 200; auto-unseal test via `kubectl rollout restart sts/openbao` + observe re-unseal via simulated KMS endpoint |
| F1.11 | LIVE prod (DOKS): Helmfile sync + init + Shamir distribution | sponsor + 1 backup person execute Shamir distribution per Q13 |

### F2 — kv-v2 engine + 3 critical platform secrets migrated (1 week)

| Task | Deliverable | DoD |
|---|---|---|
| F2.1 | Enable kv-v2 engine via BaoConfig | `path: secret/, version: v2` |
| F2.2 | Migrate GITHUB_APP_PRIVATE_KEY → `secret/data/platform/github-app` | loopzero-api reads via projected file mount (DEL env var); LIVE test webhook flow |
| F2.3 | Migrate cosign.password → `secret/data/build/cosign-passphrase` | Argo Workflows reads via ESO ExternalSecret; LIVE build pipeline test |
| F2.4 | Migrate LOOPZERO_SECRET_ENCRYPTION_KEY → `secret/data/platform/legacy-aes-key` | Per Q10: legacy key VIVES no Vault KV durante coexistência (NOT em env var); deletado dia 60 |
| F2.5 | loopzero-api refactor reading order | startup hook fetches paths from Vault; existing `secret_store.go` continua usando AES-256-GCM via key vinda do Vault (não env var) — preparação para F3 |
| F2.6 | Cache + circuit breaker per Q11 | `pkg/vault/cached_client.go` + `pkg/circuitbreaker` integration + 5 métricas Prometheus |
| F2.7 | Quality gate `task quality:no-env-vault-secrets` | grep ensures GITHUB_APP_PRIVATE_KEY/LOOPZERO_SECRET_ENCRYPTION_KEY/cosign.password not in helm values/yaml/sh as plain env |

### F3 — transit engine + per-tenant DEK (1 week)

| Task | Deliverable | DoD |
|---|---|---|
| F3.1 | Enable transit engine via BaoConfig | `path: transit/` |
| F3.2 | Master transit key `transit/keys/master` | created via Job; used for platform-level envelope |
| F3.3 | `tenant-controller` reconcile creates `transit/keys/tenant-<id>` | on tenant provisioning; idempotent; before Tenant CR ACTIVE |
| F3.4 | `secret_store.go` refactor read path | detect prefix `vault:v1:` → transit decrypt; raw bytes → legacy AES |
| F3.5 | `secret_store.go` refactor write path | always transit encrypt with `tenant-<id>` key |
| F3.6 | LIVE test: `loopzero secret set` → PG contains `vault:v1:` ciphertext | journey test cleanroom |
| F3.7 | LIVE test: crypto-shredding tenant delete → ciphertexts unreadable | `bao delete transit/keys/tenant-test` post-soft-delete; existing reads return error |
| F3.8 | Quality gate `task quality:vault-prefix-format` | new writes always produce `vault:v1:` prefix |

### F4 — ESO ClusterSecretStore webhook backend + loopzero-api broker (3 days)

| Task | Deliverable | DoD |
|---|---|---|
| F4.1 | New endpoint `POST /internal/v1/secret-fetch/{tenant_id}/{secret_name}` em loopzero-api | Returns plaintext JSON; auth via cross-cluster TokenReview |
| F4.2 | Middleware `pkg/middleware/cross_cluster_token_review.go` | reusa `pkg/cellmgr` ClusterCredential; validates SA token via cell K8s API server |
| F4.3 | Rate limit per cell via `pkg/enforcement` | 100 req/sec per cell default |
| F4.4 | audit_log PG event-type `secret.read.broker` | persists tenant_id + cell_id + eso_sa + timestamp |
| F4.5 | ClusterSecretStore type=webhook em cada cell | template em `infra/helm/external-secrets/templates/cluster-secret-store-broker.yaml` |
| F4.6 | TLS cert pinning entre ESO↔loopzero-api | cert-manager Certificate + SPKI pin em ESO values |
| F4.7 | LIVE test: tenant secret flow E2E | CLI `loopzero secret set` → PG → ESO refresh → K8s Secret no namespace tenant → pod envFrom |

### F5 — K8s Auth Method + policies templated + ClusterRole split (1 week)

| Task | Deliverable | DoD |
|---|---|---|
| F5.1 | Enable `auth/k8s-mgmt/` via BaoConfig | bound to mgmt cluster K8s API |
| F5.2 | Enable `auth/k8s-build/` | bound to build cluster |
| F5.3 | Enable `auth/k8s-cell-default/` | bound to cell-default workload cluster |
| F5.4 | Enable `auth/k8s-cell-1/` + `auth/k8s-cell-2/` | bound to runtime workload clusters |
| F5.5 | Role `loopzero-api` em auth/k8s-mgmt | policies: `loopzero-api-platform` + `loopzero-api-tenant-broker`; TTL 1h |
| F5.6 | Role `loopzero-tenant-operator` em auth/k8s-mgmt | policy templated por `tenant_id` metadata |
| F5.7 | Role `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator` em auth/k8s-mgmt | least-priv per controller |
| F5.8 | Role `argo-workflows` em auth/k8s-build | reads `secret/data/build/*`; PKI sign cert (F-late) |
| F5.9 | Role `external-secrets` em auth/k8s-cell-<X>` | reads tenant secrets via broker validate path |
| F5.10 | Policies em BaoConfig | full HCL with identity templating |
| F5.11 | loopzero-api Deployment: projected SA token | audience=`vault.loopzero.io`, REDACTED_SECRET_043, mountPath=`/var/run/secrets/vault` |
| F5.12 | loopzero-api SDK refactor: K8s auth login | replaces token env var pattern; renew before expiry |
| F5.13 | ClusterRole `loopzero-api` SPLIT | namespaced Role para secrets/configmaps em tenant namespaces; cluster Role apenas para CRDs loopzero.io + Argo Workflows + Namespaces create |
| F5.14 | LIVE test: loopzero-api startup com Vault auth K8s | login success → token cached → policy enforcement validated |
| F5.15 | Quality gate `task quality:cluster-role-no-secrets-star` | greps `charts/loopzero-api/templates/clusterrole.yaml` ensures NO `apiGroups: [""], resources: ["secrets"]` |

### F6 — Database engine (dynamic PG creds) (3 days)

| Task | Deliverable | DoD |
|---|---|---|
| F6.1 | Enable `database/` engine via BaoConfig | path: database/ |
| F6.2 | Configure connection `database/config/postgres-loopzero-api` | DOKS managed PG endpoint + bootstrap admin |
| F6.3 | Role `database/roles/loopzero-api` | creation_statement: CREATE ROLE with grants; TTL 1h max 24h |
| F6.4 | loopzero-api fetches dynamic creds at startup | refresh ~30min |
| F6.5 | `loopzero-dev-password` static password DELETED from `generate-secrets.sh` | k3d standalone now uses Vault database engine OR test fixture |
| F6.6 | LIVE test: loopzero-api uses dynamic creds | PG audit log shows user `v-k8s-loopzero-api-*` |

### F7 — Kubernetes engine (dynamic SA tokens) (1 week)

| Task | Deliverable | DoD |
|---|---|---|
| F7.1 | Enable `kubernetes/` engine | path: kubernetes/; configured per cluster |
| F7.2 | Role para emit SA tokens em `loopzero-system/loopzero-runtime` per cell | TTL 24h, audience=`https://kubernetes.default.svc` |
| F7.3 | Refactor `scoped-kubeconfig.sh` para invoke Vault engine | OR refactor operators to fetch token via Vault SDK at reconcile |
| F7.4 | DELETE legacy `${SA_NAME}-token` Secret type=service-account-token | from scoped-kubeconfig.sh |
| F7.5 | LIVE test: cell access via Vault-issued SA token | reconcile loop succeeds; token rotates after 24h without manual intervention |
| F7.6 | Quality gate `task quality:no-legacy-sa-token-secret` | greps Helmfile + scripts ensure no `type: kubernetes.io/service-account-token` |

### F8 — Audit log streaming + alerts (3 days)

| Task | Deliverable | DoD |
|---|---|---|
| F8.1 | Promtail sidecar em StatefulSet OpenBao | tail `/vault/audit/audit.log`; ship para Loki |
| F8.2 | Loki retention 90d para namespace `vault-system` | Loki config update |
| F8.3 | Grafana dashboard "OpenBao Audit" provisioned | top accessors, policy denials, auth failures, anomaly heatmap |
| F8.4 | PrometheusRule alerts P1 | `policy_denial > 0 sustained 5min`, `auth_failure_rate > 5/min`, `audit_sink_write_failed`, `archive_job_not_succeeded_36h` |
| F8.5 | Cron Job archive Loki → DO Spaces mensal | `loopzero-audit-archive/openbao/YYYY-MM.json.gz` 1y retention |
| F8.6 | LIVE alert test: trigger policy denial → alert fires | manual test or chaos scenario |

### F9 — Pulumi Vault provider para IaC secrets (3 days)

| Task | Deliverable | DoD |
|---|---|---|
| F9.1 | Adicionar `@pulumi/vault` package | `infra/pulumi/package.json` |
| F9.2 | Migrar `Pulumi.dev.yaml` secrets para Vault KV | spaces creds, DO token, CF token, gitea pwd → `secret/data/platform/*` |
| F9.3 | Migrar `Pulumi.prod.yaml` (vazio post-GHA) para Vault | mesmo padrão; runbook `docs/operations/prod-pulumi-bootstrap.md` |
| F9.4 | Pulumi resources read from Vault | `vault.generic.getSecret({path: ...})` |
| F9.5 | DELETE inline `secure: v1:...` entries from Pulumi.dev.yaml | post-migration |
| F9.6 | Quality gate `task quality:no-pulumi-inline-secrets` | greps Pulumi.*.yaml ensures only public config (no `secure:` lines) |

### F10 — Day-30 cron migration + AES code deletion day-60 (calendar gated)

| Task | Deliverable | DoD |
|---|---|---|
| F10.1 | `task migrate:secrets-to-vault` job | idempotente, batch 100/iter, scheduled K8s CronJob OR manual run |
| F10.2 | Quality gate `task quality:no-aes-remaining` | enforced day-60; CI fails if `SELECT COUNT(*) FROM secrets WHERE encrypted_value NOT LIKE 'vault:v1:%' > 0` |
| F10.3 | DELETE AES code path from `secret_store.go` | day-60; `decryptAES`, `encryptAES`, key handling fields removed |
| F10.4 | DELETE `secret/data/platform/legacy-aes-key` from Vault | crypto-shred via `bao delete` |
| F10.5 | DELETE legacy AES tests | symmetric with #3 |

### F11 — PKI engine + mTLS (DEFERRED to Sprint 12+ separate project)

Not in scope for this plan. Documented for future reference.

## Edge cases (Pass 1 — to be reviewed via `/edge-case-plan`)

(Placeholder — formal edge case review pending. Initial preview:)

- **EC-1:** Vault outage durante F2 migration → cache serve stale per Q11; quality gate `task quality:no-stale-deploy` blocks new deploys if stale_read > 0 sustained.
- **EC-2:** AWS KMS unreachable mid-restart → OpenBao pods recém-criados não unselao; existentes continuam. Manual unseal via Shamir documented.
- **EC-3:** Bank-Vaults operator crash loop → BaoConfig drift não corrige; manual operator restart procedure em runbook.
- **EC-4:** SA token expiry race em loopzero-api startup → SDK retry com backoff (cobertura em testes pkg/vault).
- **EC-5:** Per-tenant transit key creation falha durante tenant provisioning → Tenant CR não vai a ACTIVE; reconcile retry.
- **EC-6:** ESO refresh interval too aggressive → rate limit por cell trips; ESO retorna stale; alert.
- **EC-7:** cert-manager rotação cert + SPKI pin update timing → window onde loopzero-api falha auth. Mitigação: gradual rotation com helm chart bump aligned.

## Risk register

| Risk | Likelihood | Impact | Mitigation | Owner |
|---|---|---|---|---|
| Vault outage = no new deploys | Low | Medium | Cache 5min + serve-stale per Q11 | F2/F8 |
| AWS KMS account compromise | Very low | Critical | IAM `kms:Decrypt` only + Shamir break-glass | F1/Q13 |
| Shamir shards inacessíveis | Very low | Critical | 5/3 quorum + 5 storages heterogêneos + quarterly drill | Q13 |
| OpenBao Raft data corruption | Low | Critical | Hourly snapshot + restore runbook + chaos drill | F12/Q12 |
| Bank-Vaults abandoned | Medium | Medium | Apache 2.0 fork possible; BaoConfig portable to HCL | Q7 |
| F3 master key migration breaks reads | Medium | High | Lazy migration preserves AES until day 60 + quality gate | F3/Q10 |
| Per-tenant transit key creation race | Low | Medium | Tenant CR não vai a ACTIVE sem key | F3.3 |
| Vault outage during F1 prod cutover | Low | High | Cutover em janela de baixo tráfego + rollback procedure | F1 |

## Acceptance criteria (final)

- [ ] F0-F10 all tasks DoD met
- [ ] Deep review re-run shows 0 🔴 bugs em platform-secrets posture
- [ ] LIVE prod: 30 dias de operação sem incidente Vault-related
- [ ] First quarterly Shamir drill executed and documented
- [ ] CLAUDE.md "Known Systemic Patterns" updated with OpenBao-related entries
- [ ] CLAUDE.md "Common Mistakes" updated com 5+ new entries específicos OpenBao
- [ ] Audit log Grafana dashboard live + 3 PrometheusRules firing on test scenarios
- [ ] Maturity audit re-baseline (post deletion AES day 60): expect +0.10 to +0.20 movement in security dimension

## References

- [[MEET-2026-06-02-vault-adoption-reversal]]
- [[2026-06-02-D6-revoke]]
- [[2026-06-02-openbao-adoption]] (full 16-decision MADR)
- Deep review infra Categoria A 2026-06-02 (chat transcript)
- ADR-0015 Layered Bootstrap (preserved)
- Bank-Vaults v1.21+ release notes (OpenBao native support)
- OpenBao docs: https://openbao.org/docs/
- Pinterest "Multi-tenant secrets at scale" KubeCon NA 2022
- Stripe Engineering blog 2023 (cache + circuit breaker pattern)
- Adobe "Vault Production Lessons" 2023 (cert pinning minimum viable)
- Cloudflare "Encryption at Cloudflare" 2023 (Shamir distribution pattern)
- Vercel Engineering blog 2024 (broker pattern for tenant secrets)
