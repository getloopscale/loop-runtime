# Module 05 — Infrastructure as Code (`infra/pulumi*`, `infra/cloudflare/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.7 (1M context), code-validated
**Confidence:** 88% (Claude-only, IaC drift detection mais difícil sem `pulumi preview` LIVE)
**Maturity:** **3.0 / 4.0**

## TL;DR

Arquitetura 3-camadas (Pulumi L1 → Helmfile L2 → ArgoCD L3) funcional, com healing scripts H1-H12 test-pinned e topologia multi-cluster dinâmica. Bloqueia 3.5+ por **`pulumi-control-plane` STUB com 16 funções TODO** declaradas em `index.ts:16` (loopzero-api/Tenant Controller intencional não-deployado via Pulumi), 2 dos 5 projetos Pulumi com **0 testes**, e worker.mjs com S3 endpoint + base domain hardcoded.

---

## 1. Inventário Real

**5 projetos Pulumi** (CLAUDE.md mencionava só 1):

| Projeto | LOC | Files | Tests | Stacks | Propósito |
|---|---|---|---|---|---|
| `pulumi/` | 5,764 | 56 | **24** | dev, prod, staging | L1 cloud primitives (DO + CF) |
| `pulumi-infra/` | 2,814 | 37 | 6 | dev, homolog | Multi-cluster topology dinâmico |
| `pulumi-platform/` | 4,434 | 23 | **0** | dev only | L2 platform services on K8s (32 Helm releases) |
| `pulumi-control-plane/` | 1,735 | 12 | **0** | dev only | **STUB** — 16 funções TODO |
| `pulumi-shared/` | 104 | 1 | 0 | — | Contratos TypeScript (interfaces) |

**Outros:**
- `infra/cloudflare/worker.mjs` — 110 LOC, multi-tenant routing via CF KV
- `infra/k3d/` — 4 cluster configs (mgmt, build, runtime-2, workload), 242 LOC YAML
- `infra/helmfile/` — 4 templates (mgmt, build, runtime, standalone) × 4 environments
- `infra/argocd-bootstrap/` — root-app.yaml + appproject.yaml (PR-driven sync)
- `infra/scripts/` — 46 scripts (3 bootstrap, 4 healing, 6 recovery, 5 validation, 8 build, 7 drift, 13 outros)
- `infra/sloth/` — 7 SLO definitions Prometheus (api-availability, build-pipeline, deploy-latency, deploy-success, domain-provisioning, rollback-success, tenant-availability)

**Providers usados:** DigitalOcean (VPC + 4× DOKS + PG16 + Spaces + Registry + Valkey Harbor HA), Cloudflare (Tunnel + Worker + KV + DNS), AWS (S3 backend reference, stub).

---

## 2. Arquitetura Observada

**Layered Bootstrap (CHG-2026-04-28-LAYERED-BOOTSTRAP):**

```
L1 Pulumi (infra-only)        L2 Helmfile (32 charts)       L3 ArgoCD (Day-2 GitOps)
─────────────────────         ───────────────────────       ──────────────────────
pulumi up                  →  helmfile sync             →   root-app.yaml syncs
  DO VPC + DOKS×4             cert-manager → Traefik         ArgoCD apps from Gitea
  PG16 + Spaces + DOCR        ArgoCD → Gitea → MinIO         PR-driven (no auto-sync)
  CF Tunnel + Worker + KV     Prometheus + Loki + Tempo
                              Ory Kratos + Hydra + Redis
```

**StackReference chain (zero ciclos):**
- `pulumi-platform/src/index.ts:39-41` consome `loopzero-infra` outputs (mgmtKubeconfig)
- `pulumi-control-plane/src/infra-ref.ts:52-53` consome `loopzero-infra` + `loopzero-platform`

**State backend:** `s3://loopzero-infra-pulumi-state` (DigitalOcean S3-compatible, `s3Endpoint` exportado em `pulumi-infra/src/index.ts:223`).

**Worker multi-tenant** (`infra/cloudflare/worker.mjs:24-105`):
- Parse hostname → subdomain
- KV lookup `cell:{tenant}` → proxy para Traefik ingress IP do cell
- Headers: Host + `X-Cell-ID` forward
- Fallback: passthrough para CF Tunnel default origin
- Cache control heurístico: immutable se hash 8+ hex; no-cache para HTML/SPA

**Healing scripts (H1-H12 codificados em `k3d-cross-cluster-heal.sh`):**
- H1: docker network reconnect
- H2: kubeconfig regeneration (CA + IP changes)
- H3: ArgoCD cluster Secret recreation
- H4: /etc/hosts hostAlias para k3d-registry
- H5: SA cluster-admin binding (local only)
- H6: empty registry auth fallback
- H7: MINIO_ENDPOINT → NodePort 32000
- H8: loopzero-api Deployment env patch
- H9: loopzero-build-cwts values patch
- H10: tenant-controller WORKLOAD_CLUSTER_URL patch
- H11: ArgoCD token rotation
- H12: cell-1 ClusterCredential heal (spec.cellID lookup, matrix entry 153)

---

## 3. Contratos Públicos

**InfraStackOutputs (TypeScript interface):**
- `clusterRegistry: ClusterRegistryEntry[]` (canonical multi-cluster contract)
- Singletons (legacy): mgmtKubeconfig, buildKubeconfig, runtimeKubeconfig
- DB scoped per-user: dbGiteaUser/Password, dbKratosUser/Password, dbHydraUser/Password
- Storage: bucketName, buildsBucketName, logsBucketName, staticBucketName + s3Endpoint
- CF: cfTunnelId/Token, cfKvNamespaceId, cfWorkerName

**PlatformStackOutputs:**
- `mgmtIngressEndpoint`, `runtimeIngressEndpoint`, `lokiEndpoint`
- Readiness flags: `giteaReady`, `argoCdReady`, `prometheusReady`, `grafanaReady`

**Pulumi config secrets requeridos:** `digitalocean:token`, `cloudflare:apiToken`, `spacesAccessKeyId`, `spacesSecretAccessKey`, `giteaAdminPassword` (optional).

**Helmfile environments:** dev, prod, standalone, standalone-multi (4 perfis, último em CHG-2026-04-28).

---

## 4. Cobertura de Testes

**24 vitest suites em `pulumi/src/__tests__/`** — destaque:
- `heal-script-H12.test.ts` (19 assertions: spec.cellID detection, cluster-name=workload, trap EXIT, matrix 153 fix)
- `heal-script-H14.test.ts`, `dns-detection.test.ts`, `errors.test.ts`, `naming.test.ts`, `tags.test.ts`, `nodes.test.ts`
- `migrations-forward-only.test.ts`, `operator-clusterrole-subset.test.ts`, `operator-split-invariants.test.ts`
- `helmfile-cell-metadata.test.ts`, `helmfile-environments-parity.test.ts`
- `production-runbooks.test.ts`, `adr-verification-matrices.test.ts`

**6 vitest suites em `pulumi-infra/src/__tests__/`** (dns, errors, naming, nodes, tags, topology).

**0 testes em `pulumi-platform/` e `pulumi-control-plane/`.**

**Shell test harnesses:** `audit-helm-drift.test.sh`, `crd-semantic-diff.test.sh`.

**Drift detection:** `audit-helm-drift.sh` (helm diff em pre-deploy gate). Pulumi drift detection deferred para `pulumi preview` manual em CI.

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação |
|---|---|---|
| **`pulumi-control-plane` STUB** — 16 funções TODO declaradas | **HIGH** | `pulumi-control-plane/src/index.ts:16-33` (readiness hardcoded false) |
| **2 projetos Pulumi sem testes** | **HIGH** | `pulumi-platform/`, `pulumi-control-plane/` (0 .test.ts) |
| Worker.mjs hardcoded S3 + domain | MÉDIA | `infra/cloudflare/worker.mjs:1-2` |
| `Pulumi.staging.yaml` stale | MÉDIA | `pulumi/Pulumi.staging.yaml` (sem CI run recente) |
| DO Project + UptimeCheck deferred | BAIXA | `pulumi/src/components/infra.ts` (TODO) |
| Traefik naming não-parametrizado | BAIXA | `pulumi-platform/src/components/bootstrap-shared.ts` (TODO) |
| Sem Pulumi CrossGuard policies | BAIXA | — |
| Sem cost alerts (DO billing) | BAIXA | — |
| Adoption fragility (name collision risk) | BAIXA | `pulumi/src/core/adopt-or-create.ts` (mitigado por env config) |

**Disparidade vs CLAUDE.md:** declara "Pulumi L1-only (zero K8s resources)" mas `pulumi-platform/` tem **32 Helm releases + namespaces + RBAC + Secrets**. Clarification needed em CLAUDE.md.

---

## 6. Avaliação de Maturidade

**Score: 3.0 / 4.0** (confidence 88%)

### Evidências

1. **T1 (commit+test)** — `pulumi/src/__tests__/heal-script-H12.test.ts` (19 assertions matrix 153); `pulumi-shared/src/index.ts` (InfraStackOutputs + PlatformStackOutputs contratos); 24 vitest suites + 2 shell test harnesses.
2. **T2 (LIVE log)** — Cleanroom 12/12 GREEN com bootstrap completo Pulumi L1 → Helmfile L2 → ArgoCD L3 (2026-05-05); cell-2 ativada via Pulumi (2026-05-02); 4 clusters DOKS LIVE em DO.
3. **T3 (CI run)** — `ci.yml`, `infra-validate.yml`, `infra-tests.yml` workflows em `.github/workflows/`.

### Raciocínio

**Sustenta 2.5-3.0:**
- 3-layer separation funcional (L1 Pulumi → L2 Helmfile → L3 ArgoCD)
- StackReferences sem ciclos (unidirecional L1→L2→L3)
- 24 vitest em `pulumi/` (healing scripts test-pinned)
- 7 SLOs Sloth definidos
- Multi-cluster topology dinâmico via `pulumi-infra/src/topology/`
- 46 scripts (bootstrap/healing/recovery/validation) bem distribuídos
- Worker multi-tenant funcional com cell routing

**Bloqueia 3.5+:**
- **`pulumi-control-plane` STUB**: loopzero-api, Tenant Controller, Ory Kratos/Hydra **intencionais mas não-deployados via Pulumi** (16 TODOs)
- **0 testes em 2 projetos Pulumi** (platform + control-plane)
- Worker hardcoded values (deveria injetar via Pulumi)
- `Pulumi.staging.yaml` stale (sem CI run recente)
- Sem Pulumi policy-as-code (CrossGuard)
- Sem cost monitoring/alerts integrados
- Documentação CLAUDE.md desalinhada com realidade (5 projetos vs "L1-only")

### Caminho para 3.5-4.0

1. **Implementar `pulumi-control-plane`** completo (16 funções TODO) — T3 explícito
2. Adicionar test suite para `pulumi-platform/` (mínimo: smoke tests para 32 Helm releases)
3. Adicionar test suite para `pulumi-control-plane/` quando implementado
4. Refatorar worker.mjs para usar Pulumi-injected env vars (eliminar hardcoded)
5. Adicionar `pulumi preview` automático em PR como gate de drift detection
6. Implementar CrossGuard policies (RBAC, networking, cost limits)
7. Atualizar CLAUDE.md para refletir 5 projetos Pulumi (não "L1-only")

---

**Próximas auditorias relacionadas:** Helm Charts (M06), Multi-Cell (M09), CI/CD (M12).
