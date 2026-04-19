# Multi-Cell as Default — Roadmap Executavel

**Criado:** 2026-04-18
**Meeting:** MEET-2026-04-18-053
**Status:** Phase A+D(code) COMPLETE. Phase B pending (infra). 14/21 tasks done.
**Owner:** Platform Engineer
**Sponsor:** Paulo (founder)
**Ultima atualizacao:** 2026-04-18 (Phase A + D-01/D-02/D-03/D-04/D-05 implemented)

---

## TL;DR

O sistema operava em single-cell mode com 90% do codigo multi-cell pronto mas 0% ativado.
Este roadmap ativa multi-cell como default em 4 fases, corrigindo 9 blockers encontrados
pelo board tecnico (Principal Architect, K8s Controller, Distributed Systems, SRE).

```
Phase A:  Fix 9 blockers no codigo (zero mudanca de comportamento)   ✅ DONE
Phase B:  Pulumi cria ClusterCredential CR para runtime atual        ⏳ NEXT
Phase C:  Flip LOOPZERO_DYNAMIC_ROUTER=true como default                 ⏳ Blocked by B
Phase D:  Hardening (TOCTOU, Secret watch, orphan detection)         🟡 Partial (D-03, D-05 done)
```

---

## Status

| Task | Fase | Status | Blocker | Implementacao |
|------|------|--------|---------|---------------|
| A-01 | A | :white_check_mark: | B1+B9 | `k8s_cell_store.go`: `extractCellConfig` le `spec.cellID` via unstructured, popula `APIServer`, `SecretRefName/Namespace`. Removidos `_ =` assignments. CellID vazio retorna false. |
| A-02 | A | :white_check_mark: | B2 | `clustercredential_controller.go`: Deletada `extractCredentialsFromKubeconfig` (string split naive). Substituida por `clientcmd.RESTConfigFromKubeConfig`. Fail-fast se token+cert vazios. |
| A-03 | A | :white_check_mark: | B5 | `dynamic_router.go`: `refresh()` recusa substituir map N>0 com lista vazia. Log CRITICAL + erro retornado. Lista menor que atual substitui normalmente. |
| A-04 | A | :white_check_mark: | B3 | `health.go`: `CellDiscoveryChecker` interface + `WithCellDiscovery()`. DeepHealth retorna 503 quando DynamicRouter + 0 cells. Implementado em DynamicRouter, StaticRouter, PassthroughRouter. |
| A-05 | A | :white_check_mark: | B4 | `prometheusrule-critical.yaml`: Alerta `LoopZeroCellDiscoveryEmpty` (critical, for: 60s, com runbook link). |
| A-06 | A | :white_check_mark: | B6 | `builds.go`: `CellResolver` interface substitui `cellID string` hardcoded. `WithAdmission` recebe resolver. Handler resolve cell_id per-request via `GetTenantCellID()`. `main.go` passa `cellStore` como resolver. |
| A-07 | A | :white_check_mark: | B7 | `build_reconcile_loop.go`: `CellLister` interface + `WithCellLister()`. `reconcile()` itera sobre todas cells do lister. Fallback para `cfg.CellID` se lister falha. DynamicRouter implementa `ListCellIDs()`. |
| A-08 | A | :white_check_mark: | — | `config.go`: `SingleCell bool` (`LOOPZERO_SINGLE_CELL=true`). `main.go`: overrides DynamicRouter, forca StaticRouter, loga WARNING. |
| A-09 | A | :white_check_mark: | B4 | `metrics.go`: `CellDiscoveryCount` gauge. `dynamic_router.go`: emitida em cada refresh com `len(r.cells)`. |
| B-01 | B | :black_square_button: | B8 | Requer `pulumi up` contra DO |
| B-02 | B | :black_square_button: | — | Requer kubectl contra DO mgmt cluster |
| C-01 | C | :black_square_button: | Phase B | Flip `DynamicRouter: os.Getenv("LOOPZERO_DYNAMIC_ROUTER") != "false"` |
| C-02 | C | :black_square_button: | C-01 | Staging validation |
| C-03 | C | :black_square_button: | C-02 | Rollback test |
| C-04 | C | :black_square_button: | C-03 | Prod deploy + 24h validation |
| D-01 | D | :white_check_mark: | I1 | `AssignAndExecute` roda pick+INSERT na mesma TX. `TxFromContext` + `PgxTxAdapter` propagam tx. Bootstrap handler usa `AssignAndExecute`. |
| D-02 | D | :white_check_mark: | I2 | `Watches(&corev1.Secret{}, ...)` com `findCredentialsForSecret` mapFunc. Kubeconfig rotation triggers immediate reconcile. |
| D-03 | D | :white_check_mark: | I3 | `clustercredential_types.go`: `ObservedGeneration int64` no status. Controller seta `cr.Status.ObservedGeneration = cr.Generation` antes de status update. |
| D-04 | D | :white_check_mark: | I5 | `OrphanDetector` background loop (60s). Compara cells do router vs cell_ids no PG. Emite `loopzero_orphaned_tenants` gauge. |
| D-05 | D | :white_check_mark: | I6 | Runbooks criados: `docs/runbooks/alerts/LoopZeroCellDiscoveryEmpty.md` + `docs/runbooks/kubeconfig-rotation.md`. |
| D-06 | D | :black_square_button: | — | Remover StaticRouter fallback. So apos 1+ semana de Phase C validada. |

### Resumo por fase

| Fase | Total | Done | Pendente | Blocker |
|------|:-----:|:----:|:--------:|---------|
| A | 9 | 9 | 0 | — |
| B | 2 | 0 | 2 | Pulumi + DO infra |
| C | 4 | 0 | 4 | Phase B gate |
| D | 6 | 5 | 1 | Phase C gate 1 semana (D-06) |
| **Total** | **21** | **14** | **7** | |

---

## Phase A — Fix Safety Net — COMPLETE :white_check_mark:

**Gate:** `go build ./api/... ./operators/... && go vet ./api/... ./operators/...` — PASSED
**Testes:** `go test ./api/...` (exceto `k8s` que requer cluster) — PASSED
**Testes:** `go test ./operators/...` — PASSED
**Comportamento em producao:** INALTERADO (DynamicRouter ainda opt-in via `LOOPZERO_DYNAMIC_ROUTER=true`)

### Arquivos modificados

| Arquivo | Mudanca |
|---------|---------|
| `api/internal/placement/k8s_cell_store.go` | A-01: `extractCellConfig` reescrito — spec.cellID, APIServer, SecretRef |
| `api/internal/placement/router.go` | A-01: `CellConfig` struct estendido com `APIServer`, `SecretRefName`, `SecretRefNamespace`. A-04: `DiscoveredCellCount()` + `IsDynamic()` em Passthrough/Static. |
| `api/internal/placement/dynamic_router.go` | A-03: stale-data guard em `refresh()`. A-04: `DiscoveredCellCount()` + `IsDynamic()`. A-07: `ListCellIDs()`. A-09: emite `loopzero_cell_discovery_count`. |
| `api/internal/routes/health.go` | A-04: `CellDiscoveryChecker` interface + `WithCellDiscovery()` + cell check em DeepHealth |
| `api/internal/routes/builds.go` | A-06: `CellResolver` interface substitui `cellID string`. `WithAdmission` recebe resolver. Handler resolve per-request. |
| `api/internal/services/build_reconcile_loop.go` | A-07: `CellLister` interface + `WithCellLister()`. `reconcile()` itera sobre todas cells. |
| `api/internal/metrics/metrics.go` | A-09: `CellDiscoveryCount` gauge adicionado |
| `api/internal/config/config.go` | A-08: `SingleCell bool` campo + env var `LOOPZERO_SINGLE_CELL` |
| `api/cmd/main.go` | A-08: `SingleCell` overrides DynamicRouter. A-06: `cellStore` como CellResolver no admission. |
| `operators/internal/controller/clustercredential_controller.go` | A-02: `extractCredentialsFromKubeconfig` deletado, substituido por `clientcmd.RESTConfigFromKubeConfig`. D-03: `ObservedGeneration` setado. |
| `operators/api/v1/clustercredential_types.go` | D-03: `ObservedGeneration int64` no `ClusterCredentialStatus` |
| `infra/helm/monitoring/manifests/prometheusrule-critical.yaml` | A-05: Alerta `LoopZeroCellDiscoveryEmpty` |

### Arquivos criados

| Arquivo | Conteudo |
|---------|---------|
| `docs/runbooks/alerts/LoopZeroCellDiscoveryEmpty.md` | Runbook: diagnostico, remediacao, rollback, escalacao |
| `docs/runbooks/kubeconfig-rotation.md` | Procedimento de rotacao via Pulumi e manual |
| `docs/meetings/records/MEET-2026-04-18-053.md` | Meeting record com 9 blockers + plano 4 fases |

---

## Phase B — Pulumi cria ClusterCredential — NEXT :arrow_forward:

**Prerequisito:** Phase A gate passed.
**Blocker:** Requer acesso ao ambiente DigitalOcean (`pulumi up --stack dev`).

### B-01: Pulumi cria CR + Secret

**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts`

CR proposto:
```yaml
apiVersion: platform.loopzero.io/v1
kind: ClusterCredential
metadata:
  name: runtime-sfo3-01
  namespace: platform-system
spec:
  clusterName: loopzero-infra-runtime
  apiServer: "<runtime-api-server>"
  secretRef:
    name: runtime-cell-kubeconfig
    namespace: platform-system
  scope: { argocd: true, operators: true }
  tls: { insecure: false }
  cellID: cell-default
```

**Criterios de aceite:**
- [ ] `pulumi preview` mostra criacao do ClusterCredential CR
- [ ] CR referencia Secret existente
- [ ] `cellID: cell-default` (match com tenants existentes no PG)
- [ ] CR criado APOS Secret e APOS CRD installation

**DoD:**
- `pulumi up --stack dev` sem erro
- `kubectl get clustercredentials` mostra CR
- Controller reconcilia → Connected=True

### B-02: Validar Connected=True

**Criterios de aceite:**
- [ ] `status.connected: true`
- [ ] `loopzero_cell_discovery_count` = 1
- [ ] ArgoCD cluster secret criado em ns `argocd`

---

## Phase C — Flip the Switch

**Gate:** 24h em producao sem incidents

### C-01: Default LOOPZERO_DYNAMIC_ROUTER=true

**Arquivo:** `api/internal/config/config.go` L324
```go
// Antes:
DynamicRouter: os.Getenv("LOOPZERO_DYNAMIC_ROUTER") == "true",
// Depois:
DynamicRouter: os.Getenv("LOOPZERO_DYNAMIC_ROUTER") != "false",
```

**Criterios de aceite:**
- [ ] Sem env var → `DynamicRouter = true` (default)
- [ ] `LOOPZERO_DYNAMIC_ROUTER=false` → desativa explicitamente
- [ ] `LOOPZERO_SINGLE_CELL=true` → forca StaticRouter (overrides)

### C-02: Staging validation
- [ ] API log: "DynamicRouter active"
- [ ] health/deep: `"cells": "1 discovered"`
- [ ] `loopzero deploy` end-to-end funciona
- [ ] `loopzero_cell_discovery_count` = 1 em Prometheus

### C-03: Rollback test
- [ ] Deletar CR → alerta dispara <2min
- [ ] `LOOPZERO_SINGLE_CELL=true` → restaura operacao
- [ ] Recriar CR → alerta resolve

### C-04: Producao
- [ ] Deploy sem downtime
- [ ] 1 `loopzero deploy` completo com DynamicRouter
- [ ] 24h sem incidents

---

## Phase D — Harden (1+ semana apos Phase C)

| Task | Status | Descricao | Arquivo | Blocker |
|------|--------|-----------|---------|---------|
| D-01 | :black_square_button: | Fix TOCTOU: INSERT dentro da advisory lock TX | `bootstrap_store.go` + `serializer.go` | PG real para teste integracao |
| D-02 | :black_square_button: | Controller Watch em Secrets referenciados | `clustercredential_controller.go` | envtest |
| D-03 | :white_check_mark: | ObservedGeneration no status | `clustercredential_types.go` | — |
| D-04 | :black_square_button: | Orphaned tenant detection + metrica | Novo: `orphan_detector.go` | — |
| D-05 | :white_check_mark: | Runbooks | `docs/runbooks/` | — |
| D-06 | :black_square_button: | Zero cells = fatal (remover fallback) | `main.go` | 1+ semana Phase C |

---

## Dependencias

```
Phase A ━━━━━━━━━━━━━━━━━━━━━━ COMPLETE ━━━━━━━━━━━━━━━━━━━

Phase A Gate ──► B-01 ──► B-02         ← NEXT (Pulumi + DO)

Phase B Gate ──► C-01 ──► C-02 ──► C-03 ──► C-04

Phase C Gate (24h) ──► D-01, D-02, D-04 (paralelo)
Phase C Gate (1 sem) ──► D-06
```

---

## Novo no CellConfig (struct changes)

```go
// api/internal/placement/router.go
type CellConfig struct {
    CellID             string // e.g., "cell-default", "cell-sfo3-02"
    KubeConfig         string // path to kubeconfig file (StaticRouter)
    KubeContext        string // context name within kubeconfig
    ArgocdURL          string // ArgoCD server URL
    ArgocdToken        string // ArgoCD auth token
    IngressDomain      string // base domain for ingress
    APIServer          string // K8s API server URL (DynamicRouter)
    SecretRefName      string // kubeconfig Secret name (DynamicRouter)
    SecretRefNamespace string // kubeconfig Secret namespace (DynamicRouter)
}
```

## Novas interfaces

```go
// api/internal/routes/health.go — MEET-053 A-04
type CellDiscoveryChecker interface {
    DiscoveredCellCount() int
    IsDynamic() bool
}

// api/internal/routes/builds.go — MEET-053 A-06
type CellResolver interface {
    GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// api/internal/services/build_reconcile_loop.go — MEET-053 A-07
type CellLister interface {
    ListCellIDs(ctx context.Context) ([]string, error)
}
```

## Novas metricas

| Metrica | Tipo | Descricao |
|---------|------|-----------|
| `loopzero_cell_discovery_count` | Gauge | Cells descobertas pelo DynamicRouter |

## Novos alertas

| Alerta | Severity | Expr | For |
|--------|----------|------|-----|
| `LoopZeroCellDiscoveryEmpty` | critical | `loopzero_cell_discovery_count == 0` | 60s |

## Novos env vars

| Var | Default | Descricao |
|-----|---------|-----------|
| `LOOPZERO_SINGLE_CELL` | `false` | Forca StaticRouter (rollback mechanism) |

---

## Referencias

- **Meeting:** `docs/meetings/records/MEET-2026-04-18-053.md`
- **ADR:** ADR-0008 Cell-Based Architecture
- **Cell Budget:** `docs/architecture/cell-budget.md`
- **Cell Overview:** `docs/plans/cell-architecture-overview.md`
- **MEET-051 Invariants:** PG authority, advisory lock, kubeconfig in Secret, manual sync
- **Runbooks:** `docs/runbooks/alerts/LoopZeroCellDiscoveryEmpty.md`, `docs/runbooks/kubeconfig-rotation.md`
