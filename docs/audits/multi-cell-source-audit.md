# Multi-Cell Source Audit (Phase 0 / T0.1)

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 0 T0.1.
> Status: **complete**.
> Date: 2026-04-30.
> Auditor: Claude Code (read-only).

## Executive summary

CLAUDE.md claims **"Cell architecture Phase 0+1+1.5 complete; runtime-2 physically provisioned but not routed; cell-2 activation Sprint 9; Pulumi.dev.yaml multi-cell config missing"**. This audit verifies each clause against source. Findings:

| Claim | Reality | Verdict |
|---|---|---|
| `runtime-2 physically provisioned` | `infra/pulumi/src/components/infra.ts:160-175` provisions `${baseName}-runtime-2` DOKS cluster; `index.ts:121-123` exports `runtime2{ClusterName,ClusterEndpoint,Kubeconfig}` outputs. | **CONFIRMED** |
| `not routed` | No `ClusterCredential` resource with `spec.cellID: cell-2` exists in repo (only the CRD definition + tenant CRD reference). `wire-clusters.sh:40-42` registers runtime-2 cluster with ArgoCD but does NOT create a `ClusterCredential` CR. `setup_background.go:117` instantiates `K8sCellStore` from `loopzero-system` namespace but it will return zero cells if no `ClusterCredential` exists with `status.connected=true`. | **CONFIRMED** |
| `Pulumi.dev.yaml multi-cell config missing` | `Pulumi.dev.yaml` has `runtimeK8sVersion`, `runtimeNodeClass`, `runtimeMinNodes`, `runtimeMaxNodes` — but only one set; runtime-2 reuses these (see `infra.ts:165-170`). No `cell-2`-specific config keys. No `cellId` mapping. | **CONFIRMED** |
| `cells table` schema (per plan v1 ADR D3 task T5.3) | **NOT FOUND**. `000001_initial_schema.up.sql` has `cell_id TEXT NOT NULL DEFAULT 'cell-default'` columns on `tenants`, `builds`, `releases`, `plan_limits` (lines 89, 126, 162, 344) and `populate_cell_id_from_tenant()` trigger (line 59), but **no `cells` table**. Cells are **K8s-native** via `ClusterCredential` CRDs, not SQL. | **REVISES PLAN** |

**Implication for plan v2**: Phase 5 task T5.3 ("Database migration: cells table populated") is **wrong** — there is no SQL `cells` table. The activation path is creating a `ClusterCredential` CR with `spec.cellID: cell-2` and `status.connected: true`. Plan must be amended.

## Reality vs Documented (CLAUDE.md)

### 1. DynamicRouter (`api/internal/placement/dynamic_router.go`)

**168 lines.** Reads cells from a `CellStore` interface, refreshes every N seconds (60s in production per `setup_background.go:118`). Stores `map[cellID]CellConfig` behind `RWMutex`. Three protective behaviours:

- **Stale-data protection** (lines 100-106): refuses to replace non-empty map with empty set (MEET-053 A-03 — K8s API hiccup wouldn't orphan all tenants).
- **Cell-count decrease alerting** (line 108): logs warn if cell list shrinks.
- **Discovery metric** (line 114): `metrics.CellDiscoveryCount.Set(...)` for alerting.

`Resolve(cellID)` returns `*CellConfig` or error. Empty cellID errors. Unknown cellID errors with diagnostic listing known cell count.

**Audit verdict: production-grade.** The router logic is solid; what's missing is the **upstream data**: ClusterCredential CRs.

### 2. K8sCellStore (`api/internal/placement/k8s_cell_store.go`)

**102 lines.** Uses `dynamic.Interface` to list `loopzero.io/v1/clustercredentials` in a configured namespace (`loopzero-system` per `setup_background.go:117`). For each CR:

- Extracts `status.connected` (must be `true`).
- Extracts `spec.cellID` (must be non-empty).
- Extracts `spec.apiServer`.
- Extracts `spec.secretRef.{name,namespace}` (kubeconfig location).
- Extracts `loopzero.io/ingress-domain` label.

If `connected=false` OR `cellID=""`, the CR is **silently skipped**.

**Audit verdict: correct, but couples cell discovery to operator readiness.** A ClusterCredential CR is created by some external actor (tenant-controller? operator? helmfile script?) but **this audit found no caller**. CRDs live in `charts/loopzero-crds/templates/clustercredentials.yaml` and `operators/config/crd/bases/loopzero.io_clustercredentials.yaml`. ClusterCredential **instances** for runtime-2 do not exist anywhere in the repo.

### 3. ClusterCredential CRD spec (`operators/api/v1/clustercredential_types.go`)

```go
type ClusterCredentialSpec struct {
    ClusterName string             // required, MinLength=1
    APIServer   string             // required, MinLength=1, Pattern=^https?://
    SecretRef   SecretReference    // required
    Scope       CredentialScope    // {ArgoCD bool, Operators bool}
    TLS         ClusterTLSConfig   // {Insecure bool}
    CellID      string             // optional, default="cell-default", Pattern=^cell-[a-z0-9-]+$
}

type ClusterCredentialStatus struct {
    Connected          bool
    LastCheckedAt      *metav1.Time
    ObservedGeneration int64
    Conditions         []metav1.Condition
}
```

**Audit verdict: schema supports cell-2 activation.** The path forward:
1. Create a `Secret` in `loopzero-system` containing runtime-2 kubeconfig.
2. Create `ClusterCredential` named e.g. `runtime-2` with `spec.cellID: cell-2`, `spec.apiServer: <runtime-2 endpoint>`, `spec.secretRef: {name: runtime-2-kubeconfig, namespace: loopzero-system}`, `spec.scope.ArgoCD: true`.
3. The ClusterCredential controller reconciles, sets `status.connected: true`.
4. K8sCellStore picks it up next refresh (≤60s).
5. DynamicRouter exposes `cell-2` to callers.

### 4. tenants table cell_id (`000001_initial_schema.up.sql`)

```sql
CREATE TABLE tenants (
    -- ...
    cell_id TEXT NOT NULL DEFAULT 'cell-default',  -- line 89
);

CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
AS $$
BEGIN
    IF NEW.cell_id = 'cell-default' OR NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN NEW.cell_id := 'cell-default'; END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_builds_populate_cell_id BEFORE INSERT ON builds
    FOR EACH ROW EXECUTE FUNCTION populate_cell_id_from_tenant();
CREATE TRIGGER trg_releases_populate_cell_id BEFORE INSERT ON releases
    FOR EACH ROW EXECUTE FUNCTION populate_cell_id_from_tenant();
```

**Audit verdict: cell pinning per tenant is via SQL `tenants.cell_id`.** A tenant with `cell_id='cell-2'` will have its builds and releases inherit `cell-2`. The plan's T5.3 task should be reframed: **no migration needed** — operators just need a way to UPDATE `tenants.cell_id` for canary tenants. This is already supported by the schema.

### 5. wire-clusters.sh (`infra/helmfile/scripts/wire-clusters.sh`)

61 lines. Lines 40-42 already have `LOOPZERO_RUNTIME2_KUBECONFIG` branch:

```bash
if [[ -n "${LOOPZERO_RUNTIME2_KUBECONFIG:-}" ]]; then
  log "wiring runtime-2 cluster..."
  SCOPED=$(KUBECONFIG="$LOOPZERO_RUNTIME2_KUBECONFIG" "$SCRIPT_DIR/lib/scoped-kubeconfig.sh" runtime-2 "$LOOPZERO_RUNTIME2_KUBECONFIG")
  KUBECONFIG="$LOOPZERO_MGMT_KUBECONFIG" "$SCRIPT_DIR/lib/argocd-register.sh" runtime-2 "$SCOPED"
fi
```

**Audit verdict: this registers runtime-2 with ArgoCD but DOES NOT create a `ClusterCredential` CR.** Without that CR, `K8sCellStore.LoadCells()` returns empty for cell-2.

**Gap**: `wire-clusters.sh` (or a sibling script) must create a `ClusterCredential` resource alongside the ArgoCD registration. Plan v2 phase 5 must add this.

### 6. setup_background.go cell router activation

```go
// api/cmd/setup_background.go:113-126
if cfg.DynamicRouter {
    k8sCellStore, k8sErr := placement.NewK8sCellStore(nil, "loopzero-system")
    if k8sErr == nil {
        dynRouter := placement.NewDynamicRouter(k8sCellStore, 60*time.Second, logger)
        cells, loadErr := k8sCellStore.LoadCells(context.Background())
        if loadErr == nil && len(cells) > 0 {
            // start router
        }
        logger.Info("cell router: no ClusterCredentials found, falling back to StaticRouter",
            "load_err", loadErr, "cells_found", len(cells))
    }
}
```

**Audit verdict: graceful fallback to StaticRouter when no CCs found.** Means today (no CC for runtime-2 anywhere) the API runs in single-cell mode silently. Activation requires:
1. Setting `cfg.DynamicRouter = true` (env: `LOOPZERO_DYNAMIC_ROUTER=true`).
2. Ensuring at least one ClusterCredential exists for cell-2.

## Gaps (vs plan v2)

### Gap 1 — Plan v2 T5.3 references a non-existent `cells` table

Plan line 1357-1377:

```
### T5.3 — Database migration: cells table populated
...
api/internal/db/migrations/000003_cells_seed.up.sql — (NEW) INSERT INTO cells
```

**Reality**: there is no `cells` table. The schema uses `tenants.cell_id` for per-tenant pinning. Cells themselves are K8s ClusterCredential CRs.

**Fix**: rewrite T5.3 as **"ClusterCredential bootstrap for cell-2"** — creates the `Secret` + `ClusterCredential` CR for runtime-2 in `loopzero-system`. Migration optional (only if we add a `cells` metadata table for capacity tracking, which is **not in current scope**).

### Gap 2 — `wire-clusters.sh` does not create ClusterCredential CR

`wire-clusters.sh` currently only registers ArgoCD cluster. Activation needs a sibling step to create `ClusterCredential` with `spec.cellID: cell-N`.

**Fix**: add `infra/helmfile/scripts/lib/clustercredential-create.sh` (NEW) called from `wire-clusters.sh` after ArgoCD register, creating one CC per cluster with the right cellID:
- `loopzero-mgmt` → no CC (mgmt is not a workload cell)
- `loopzero-build` → no CC (build is not a workload cell)
- `loopzero-runtime` → CC with `cellID: cell-1`
- `loopzero-runtime-2` → CC with `cellID: cell-2`

### Gap 3 — Pulumi.dev.yaml lacks `cellId` keys

Plan's T5.2 says add `cellId`, `runtime2K8sVersion`, etc. Reality: `runtime2K8sVersion` is implicit (reuses `runtimeK8sVersion`). The needed key is **`cellId`** for cell-2 specifically OR — better — embed cell-2 metadata in helmfile environments since cell metadata is K8s-native.

**Fix**: 2 paths
- (a) add `cellId: cell-2` to `infra/helmfile/environments/dev.yaml` for runtime-2 wiring (cleaner — keeps cell metadata in L2 helmfile, not L1 Pulumi)
- (b) add `cellId` to Pulumi outputs so helmfile can read it from `pulumi stack output`

Recommend (a) — cell-id is ArgoCD/K8s metadata, belongs in Helmfile context, not Pulumi.

### Gap 4 — `LOOPZERO_DYNAMIC_ROUTER` env not in helmfile values

`api` deployment must set `LOOPZERO_DYNAMIC_ROUTER=true` for setup_background.go to pick the dynamic path. Default behaviour today (no env set) = StaticRouter (single-cell).

**Fix**: add to `infra/helm/loopzero-api/values.yaml`:
```yaml
env:
  - name: LOOPZERO_DYNAMIC_ROUTER
    value: "true"
```

Or scoped to dev/prod profiles only (keep standalone single-cell).

### Gap 5 — Test coverage of cell-2 path is zero live

`placement_test.go:656-755` has 8 unit tests of `DynamicRouter`. None exercise cell-2 routing **end-to-end** against a real cluster. CLAUDE.md "Phase 0+1+1.5 complete" overstates — phase 1.5 means "code merged + unit-tested", NOT "live-verified".

**Fix**: T5.6/T5.7 stages 1-2 (synthetic tenant + 10% canary) become **first live exercise** of the cell-2 path. Plan must explicitly mark this; if any failure surfaces, the canary is the first time it shows.

## Recommendations for plan v2 amendment

1. **T5.2** — keep (Pulumi config + outputs) but reduce scope: only emit `runtime2*` outputs already present (`infra.runtimeCluster2.*`); delete the imagined `cellId` Pulumi config key — move to helmfile.
2. **T5.3** — rewrite from "DB migration cells table" to "ClusterCredential bootstrap for cell-2 via wire-clusters.sh extension". New file: `infra/helmfile/scripts/lib/clustercredential-create.sh`.
3. **T5.4** — keep observability (metrics, alerts, dashboards). Add `loopzero_cluster_credential_status{cell, connected}` gauge to surface CC `status.connected`.
4. **T5.5** — `loopzero deploy --cell` must update `tenants.cell_id` (not insert into a `cells` table). Authz check stays.
5. **New T5.x** — set `LOOPZERO_DYNAMIC_ROUTER=true` in dev/prod loopzero-api values; keep standalone unaffected.
6. **T5.6/T5.7 Live tests** — annotate as "first live exercise of multi-cell path". Add explicit pre-flight: confirm `K8sCellStore.LoadCells()` returns ≥1 cell before stage 1.

## Action items (for Phase 5 implementation)

| ID | Task | Status |
|---|---|---|
| A1 | Amend plan v2 T5.3 to remove `cells` table migration | **PENDING** |
| A2 | Design `clustercredential-create.sh` lib script | PENDING |
| A3 | Add `loopzero-runtime-2` ClusterCredential bootstrap to wire-clusters.sh | PENDING |
| A4 | Set `LOOPZERO_DYNAMIC_ROUTER=true` in loopzero-api values for cloud profiles | PENDING |
| A5 | Add `loopzero_cluster_credential_status{cell, connected}` metric | PENDING |
| A6 | Pre-flight check before stage 1 canary: `kubectl get clustercredentials -n loopzero-system -l loopzero.io/cell=cell-2` returns Ready=True | PENDING |

## References

- `docs/plans/infra-normalize-multicell-plan.md` (v2, ADR D3, Phase 0 T0.1, Phase 5)
- `docs/decisions/MEET-INFRA-NORMALIZE-T3-multi-cell.md` (pending — being written by background agent T-1.3)
- `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (committed `8ba6c2f2`)
- `docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` (committed `3b7de128`)
- `api/internal/placement/dynamic_router.go`
- `api/internal/placement/k8s_cell_store.go`
- `api/cmd/setup_background.go`
- `api/internal/db/migrations/000001_initial_schema.up.sql`
- `operators/api/v1/clustercredential_types.go`
- `infra/helmfile/scripts/wire-clusters.sh`
- `infra/pulumi/src/index.ts`
- `infra/pulumi/src/components/infra.ts`
- `CLAUDE.md`
