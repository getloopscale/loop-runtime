# Plan: Infra Recreate + Multi-Cell Fix — Clean Deploy to 10k

**Data:** 2026-04-21
**Status:** PENDENTE APROVAÇÃO
**Sessão origem:** c838843c-739f-4a25-b205-ea0ba26b75d1

## Context

Fizemos destroy total da infra homolog (3 clusters + PG). State Pulumi zerado (0 resources). Objetivo: recriar com **2 runtime clusters** (multi-cell real) e chegar a 10k app runs.

A sessão anterior revelou **17 problemas sistêmicos**. Este plano corrige TODOS antes do `pulumi up`.

**Estado DO atual:** Zero clusters, zero PG, zero droplets. DOCR registry existe (com imagens). VPC existe (orphan). 5 Spaces buckets existem (orphans).

### Lições Aprendidas (17 problemas da sessão anterior)

| # | Problema | Root Cause | Categoria |
|---|---------|-----------|-----------|
| 1 | Pulumi state backend 403 | `SPACES_ACCESS_KEY_ID` vs `AWS_ACCESS_KEY_ID` | Config |
| 2 | K8s version `do.2` deprecated | Hardcoded version slug | Drift |
| 3 | Lockfile blocking Dockerfile builds | Platform overriding user's build contract | Design |
| 4 | Java CrashLoop (`/tmp` read-only) | `readOnlyRootFilesystem` sem emptyDir | Manifests |
| 5 | tenant-controller wrong image | `set image` apontou para `loopzero-api` | Ops manual |
| 6 | MaxTenantsPerCell=500 hardcoded | Placement engine bloqueou provisioning | Config |
| 7 | `MAX_CONCURRENT_BUILDS` wrong env name | Config reads `_PER_CELL` suffix | Naming |
| 8 | ArgoCD CPU amplification (91 apps) | Apps sem manifests em ComparisonError loop | ArgoCD |
| 9 | `RELEASE_PENDING_TIMEOUT < ARGOCD_SYNC_TIMEOUT` | 2m < 5m = releases falham antes do sync | Config |
| 10 | 185 Pulumi replacements (drift) | `kubectl set image/env` manual | Ops manual |
| 11 | Orphan droplets após cluster delete | Autoscaled nodes não cleanup | DO bug |
| 12 | Stress test `export -f` falha | bash array export + xargs incompatível | Script |
| 13 | Gitea saturada (701m CPU) | 10 repo-servers × git fetch simultâneo | Scale |
| 14 | Build throughput bottleneck (250/hr) | 50 concurrent × 12min/build | Capacity |
| 15 | `DEPLOY_SERVICE_AUTH_FAILED` ArgoCD | Token invalidado após pod restart | Auth |
| 16 | runtime-2 kubeconfig missing em Secrets | Adicionamos cluster sem wiring completo | BLOCKER |
| 17 | `pulumi state delete --force` não deleta DO | State cleanup ≠ resource cleanup | Ops |

### Já corrigidos na sessão anterior (commitados)

- ✅ K8s version → `1.33.9-do.3` (Fix #2)
- ✅ Lockfile → warning, não error (Fix #3)
- ✅ Java /tmp → emptyDir volume (Fix #4)
- ✅ MaxTenantsPerCell → configurável, default 5000 (Fix #6)
- ✅ ArgoCD skip-reconcile annotation (Fix #8)
- ✅ ArgoCD workqueue tuning (Fix #9 parcial)
- ✅ RELEASE_PENDING_TIMEOUT → 6m (Fix #9)
- ✅ tenant-controller-kubeconfig com runtime-2 (Fix #16 parcial)
- ✅ Runtime-2 cluster adicionado ao infra.ts + bootstrap (código Pulumi)
- ✅ ClusterCredential CR para runtime-nyc3-02

---

## Pre-Flight: Recursos Órfãos no DO

| Recurso | Existe na DO? | No Pulumi State? | Conflito no Up? | Ação |
|---------|:---:|:---:|:---:|------|
| VPC `loopzero-infra-homolog-nyc3-net` | SIM | NÃO | NÃO (adopt-or-create detecta) | Nenhuma |
| 5 Spaces buckets | SIM | NÃO | **SIM** (nome duplicado) | **Deletar** |
| DOCR `loopzero-infra-registry` | SIM | NÃO | **SIM** (1 per account) | **Import** após up |
| CDN `static-cdn` | Possível | NÃO | Possível | Verificar |

---

## Fase 1 — Fixes no Código (ANTES do pulumi up)

### Fix 1: Deletar Spaces Buckets Órfãos (BLOCKER-INFRA)
**Tipo:** Ação manual
**O que:** 5 buckets existem no DO mas não no state. Pulumi vai tentar criar com mesmo nome → 409 conflict.
```bash
aws s3 rb s3://loopzero-infra-homolog-nyc3-artifacts --force --endpoint-url https://nyc3.digitaloceanspaces.com
aws s3 rb s3://loopzero-infra-homolog-nyc3-builds --force --endpoint-url https://nyc3.digitaloceanspaces.com
aws s3 rb s3://loopzero-infra-homolog-nyc3-logs --force --endpoint-url https://nyc3.digitaloceanspaces.com
aws s3 rb s3://loopzero-infra-homolog-nyc3-static-assets --force --endpoint-url https://nyc3.digitaloceanspaces.com
aws s3 rb s3://loopzero-infra-homolog-nyc3-container-images --force --endpoint-url https://nyc3.digitaloceanspaces.com
```

### Fix 2: loopzero-api-kubeconfig missing runtime-2 (BLOCKER-CODE)
**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts:2126-2139`
**O que:** Criar `runtime2ScopedKubeconfig` via `createScopedKubeconfig("api-runtime-2", infra.runtimeCluster2, ...)` com mesmas RBAC rules do runtime-1. Adicionar ao Secret.
**Impacto sem fix:** API não consegue operar no runtime-2 (auth failure).

### Fix 3: tenant-controller-kubeconfig missing runtime-2 (BLOCKER-CODE)
**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts:2839-2841`
**Status:** ✅ JÁ FEITO na sessão anterior (commitado em `aa4a9b0e`).

### Fix 4: Wire CellLister no BuildReconcileLoop (BLOCKER-CODE)
**Arquivo:** `api/cmd/main.go:447-462` + `api/cmd/controller/main.go`
**O que:** Chamar `reconcileLoop.WithCellLister(cellRouter)` após criação do loop.
**Impacto sem fix:** Builds em cell-runtime-2 ficam stuck em RUNNING forever (loop só verifica cell-default).

### Fix 5: Wire SyncClientFactory em vez de SyncClient hardcoded (BLOCKER-CODE)
**Arquivo:** `api/cmd/main.go:335-352` + `api/cmd/controller/main.go`
**O que:** O `SyncClientFactory` já existe (`api/internal/argocd/sync_client_factory.go`, testado). Substituir:
```go
// ANTES: 1 client hardcoded
rawSyncClient := argocd.NewSyncClient(argocd.SyncClientConfig{BaseURL: cfg.ArgocdURL, ...})

// DEPOIS: factory que resolve per-cell
syncFactory := argocd.NewSyncClientFactory(cellRouter, func(cellCfg *placement.CellConfig) (argocd.SyncClientInterface, error) {
    return argocd.NewSyncClient(argocd.SyncClientConfig{
        BaseURL:  cellCfg.ArgocdURL,
        Token:    cellCfg.ArgocdToken,
        Insecure: cfg.ArgocdInsecure,
    }), nil
})
```
**Impacto sem fix:** Release sync no cell-runtime-2 usa ArgoCD do cell-default → FAIL.

### Fix 6: Wire CellDiscoveryChecker no health handler (BLOCKER-CODE)
**Arquivo:** `api/internal/server/server.go` (onde health handler é construído)
**O que:** Chamar `healthHandler.WithCellDiscovery(cellRouter)`.
**Impacto sem fix:** `/health/deep` retorna 200 mesmo com 0 cells (K8s readiness probe não detecta degradação).

### Fix 7: Verificar imagens Docker existem com tag `homolog` (WARNING)
**Ação manual:**
```bash
doctl registry repository list-tags loopzero-api --format Tag | grep homolog      # ✅ JÁ TAGGED
doctl registry repository list-tags tenant-controller --format Tag | grep homolog  # ✅ EXISTE
doctl registry repository list-tags loopzero-cloud --format Tag | grep homolog    # VERIFICAR
doctl registry repository list-tags loopzero-dashboard --format Tag | grep homolog # VERIFICAR
doctl registry repository list-tags loopzero-packs-runner --format Tag | grep homolog # VERIFICAR
doctl registry repository list-tags loopzero-clone-runner --format Tag | grep homolog # VERIFICAR
```
Se faltarem, tagear da versão existente.

### Fix 8: DOCR Registry Import (WARNING)
**O que:** DOCR é 1 per account DO. Pulumi tenta criar → conflito. Após primeiro erro, fazer `pulumi import`.
**Alternativa:** Proteger o registry existente antes do up.

---

## Fase 2 — Build + Push Imagem

Após fixes 4/5/6, rebuild a imagem API:
```bash
cd /home/paulo/Projetos/loopzero/loopzero
docker build -f api/Dockerfile -t registry.digitalocean.com/loopzero-infra-registry/loopzero-api:homolog .
docker push registry.digitalocean.com/loopzero-infra-registry/loopzero-api:homolog
```
**NOTA:** tenant-controller usa imagem SEPARADA (`tenant-controller:homolog`) — NÃO tocar.

---

## Fase 3 — Cleanup Órfãos + Pulumi Up

```bash
# 1. Deletar buckets órfãos (Fix 1)
# 2. Exportar env vars
eval "$(grep -E '^(DIGITALOCEAN_TOKEN|SPACES_ACCESS_KEY_ID|SPACES_SECRET_ACCESS_KEY|PULUMI_CONFIG_PASSPHRASE|CLOUDFLARE_API_TOKEN)=' .env | sed 's/^/export /')"
export AWS_ACCESS_KEY_ID=$SPACES_ACCESS_KEY_ID
export AWS_SECRET_ACCESS_KEY=$SPACES_SECRET_ACCESS_KEY

# 3. Pulumi up
pulumi up --stack homolog --yes
```

**Provisiona (~15-20 min):**
- 4 DOKS clusters: mgmt, build, runtime-1, runtime-2
- 1 PG managed (com 5 DBs: loopzero, gitea, harbor, kratos, hydra)
- Helm: ArgoCD, Gitea, Traefik (×3), cert-manager (×3), Harbor, monitoring
- 2 ClusterCredentials: `runtime-nyc3-01` (cell-default), `runtime-nyc3-02` (cell-runtime-2)
- Bootstrap job: cria ArgoCD token, configura secrets

---

## Fase 4 — Validação Multi-Cell

```bash
# 1. ClusterCredentials Connected
kubectl get clustercredential -n platform-system
# Espera: 2 CRs, ambos Connected=true

# 2. DynamicRouter descobre 2 cells
kubectl logs deploy/loopzero-api -n platform-system | grep "cell list refreshed"
# Espera: count: 2

# 3. Health deep reports cells
curl -s https://api.homolog.loopzero.example/api/v1/health/deep | jq .
# Espera: "cells": "2 discovered"

# 4. Provisionar 10 tenants → distribuídos entre 2 cells
# 5. 1 build end-to-end → app live
```

---

## Fase 5 — Stress Test 10k

Após validação:
```bash
# 1. Provisionar 5000 tenants (50 concurrency)
# 2. Submeter builds em waves de 200 (worker v2 com retry longo)
# 3. Monitorar: pipeline throughput, autoscaler, runtime pods
```

---

## Effort Estimate

| Fix | Complexidade | Tempo |
|-----|:---:|:---:|
| Fix 1: Delete buckets | Manual | 2 min |
| Fix 2: API kubeconfig runtime-2 | ~15 linhas Pulumi | 10 min |
| Fix 3: Controller kubeconfig | Já feito | 0 min |
| Fix 4: Wire CellLister | 3 linhas Go | 3 min |
| Fix 5: Wire SyncClientFactory | ~25 linhas Go | 15 min |
| Fix 6: Wire CellDiscovery | 2 linhas Go | 2 min |
| Fix 7: Verificar imagens | Manual | 5 min |
| Fix 8: Registry import | Condicional | 5 min |
| Build + push | Docker | 5 min |
| Pulumi up | Infra | 15-20 min |
| Validação | Manual | 10 min |
| **TOTAL** | | **~75 min** |

---

## Arquivos a Modificar

| Arquivo | Fix | Mudança |
|---------|:---:|---------|
| `infra/pulumi/src/components/mgmt-bootstrap.ts` | 2 | runtime-2 scoped kubeconfig + Secret entry |
| `api/cmd/main.go` | 4,5 | `WithCellLister` + SyncClientFactory wiring |
| `api/cmd/controller/main.go` | 4,5 | Mesmo wiring do controller |
| `api/internal/server/server.go` | 6 | `WithCellDiscovery(cellRouter)` |

---

## Riscos Conhecidos

| Risco | Severidade | Mitigation |
|-------|:---:|-----------|
| DOCR registry conflict | MEDIUM | Import manual se falhar |
| Bootstrap job timeout (>5min) | LOW | Monitorar logs, retry manual |
| Cosign key placeholder | MEDIUM | Verificar após up, recriar se necessário |
| DB migration role permissions | LOW | Init container usa admin URL |
| Gitea cold start (sem cache) | LOW | Primeiro deploy mais lento |

---

## Gaps Revisados (análise SRE Guardian)

### Gap 7: PG Migrations — quem roda? → ✅ RESOLVIDO
**Status:** Já coberto pelo código existente.
**Como:** Init container `db-migrate` no loopzero-api Deployment (`mgmt-bootstrap.ts:2325-2343`) roda `migrate -path=/migrations -database "$DATABASE_URL" up` ANTES do container principal. O binary `migrate` está embutido na imagem API (Dockerfile multi-stage). Funciona do zero — cria todas as tabelas.

### Gap 10: ArgoCD sharding para 500+ apps → ✅ JÁ CONFIGURADO
**Status:** Sharding já habilitado no `infra/helm/argocd/values.yaml:31-38`.
```yaml
controller:
  replicas: 3
  env:
    - name: ARGOCD_CONTROLLER_REPLICAS
      value: "3"
```
3 replicas com hash-based sharding distribui Applications entre pods. Combinado com workqueue tuning (maxdelay 300s, backoff 2x), suporta 5k+ apps sem saturar.

### Gap 12: CF Tunnel ID mudou → ✅ COBERTO PELO CÓDIGO
**Status:** O módulo `providers/cloudflare/tunnel.ts` cria um NOVO tunnel e atualiza o DNS record automaticamente. O wildcard `*.homolog.loopzero.example` aponta para o novo tunnel via CNAME. DNS propagation ~1-5 min (CF edge é rápido). Não requer ação manual.

### Gap 13: Bootstrap job TLS — root cause → ✅ NÃO SE APLICA
**Status:** Os repo-creds são criados via Pulumi DIRETAMENTE (`mgmt-bootstrap.ts:108-141`), usando `giteaAdminPassword` (default `"loopzero-gitea-dev-2026"` para homolog). O bootstrap job cria o ArgoCD API token e wires secrets — mas os repo-creds NÃO dependem dele. Se o job falhar, ArgoCD ainda pode clonar do Gitea via repo-creds.

### Gap 14: Repo-creds placeholder → ✅ NÃO SE APLICA
**Status:** Repo-creds password é `giteaAdminPassword ?? "loopzero-gitea-dev-2026"` (`mgmt-bootstrap.ts:111-124`). NÃO é placeholder. Foi corrigido em sessões anteriores quando migramos de bootstrap job para Pulumi-nativo.

### Gap 15: removeSkipReconcile — quem chama? → ✅ INTEGRADO
**Status:** `TriggerSync()` chama `removeSkipReconcile()` automaticamente (`sync_client.go:118-120`). Todo sync trigger (build completion, release convergence, rollback) remove a annotation. Apps novas começam pausadas (skip-reconcile=true), e são ativadas no primeiro deploy real.

### Gap 16: Autoscale no pulumi up → ✅ CONFIGURADO
**Status:** `Pulumi.homolog.yaml` define:
```yaml
runtimeMinNodes: "4"
runtimeMaxNodes: "50"
buildMinNodes: "4"
buildMaxNodes: "20"
```
`infra.ts` passa `autoScale: config.runtimeMaxNodes !== undefined` → true.
DO DOKS cria node pool com `auto_scale=true, min_nodes, max_nodes`. Validar pós-deploy com:
```bash
doctl kubernetes cluster node-pool list <cluster> --format AutoScale,MinNodes,MaxNodes
```

---

## Regras para NÃO Repetir Erros

1. **NUNCA usar `kubectl set image/env`** — todas as mudanças via Pulumi ou rebuild+push da tag correta
2. **NUNCA `pulumi state delete --force`** sem deletar o recurso real primeiro
3. **tenant-controller ≠ loopzero-api** — imagens diferentes, NUNCA apontar uma para a outra
4. **Env vars:** sempre verificar o nome exato no config.go antes de setar
5. **Pulumi backend:** sempre exportar `AWS_ACCESS_KEY_ID=$SPACES_ACCESS_KEY_ID` antes de qualquer comando Pulumi
6. **Após destroy:** SEMPRE verificar droplets/volumes/LBs órfãos com doctl
7. **K8s version slugs:** DO depreca rapidamente — verificar `doctl kubernetes options versions` antes de up
