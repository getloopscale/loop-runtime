# Bare-Helm → Helmfile Mapping (T2.1)

Phase 2 of plan `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md` v1.3.

Maps every `helm upgrade --install` in `taskfiles/infra.yml` to its
corresponding declared release in `infra/helmfile/{mgmt,build,runtime}.helmfile.yaml.gotmpl`.

---

## mgmt cluster (infra:install:mgmt)

| Taskfile bare-helm | Helmfile release (mgmt) | EC-01 needs: declaration | EC-02 portable secret |
|---|---|---|---|
| `postgresql bitnami/postgresql v18.5.14` | `postgresql` | none (root) | n/a |
| `gitea gitea-charts/gitea v10.6.0` | `gitea` | `loopzero-system/postgresql + loopzero-system/gitea-redis` | PASSWD via Secret postgresql |
| `minio minio/minio v5.4.0` | `minio` | none | n/a |
| `ory-kratos ory/kratos v0.61.0` | `ory-kratos` | `loopzero-system/postgresql` | DSN via LOOPZERO_DB_PASSWORD env |
| `ory-hydra ory/hydra v0.61.0` | `ory-hydra` | `loopzero-system/postgresql` | DSN via LOOPZERO_DB_PASSWORD env |
| `argocd argo/argo-cd v7.3.11` | `argocd` | (existing wave) | n/a |
| `network-policies infra/helm/network-policies` | (NOT YET in helmfile) | — | n/a — **GAP** |
| `monitoring prometheus-community/kube-prometheus-stack v82.15.1` | `prometheus` | none (Wave -3) | n/a |
| `otel-collector open-telemetry/opentelemetry-collector v0.108.0` | `otel-collector` | `monitoring/prometheus` | n/a |
| `loki grafana/loki v6.24.0` | (BUILD helmfile) | n/a (cross-cluster) | n/a |

## build cluster (infra:install:build)

| Taskfile bare-helm | Helmfile release (build) |
|---|---|
| `argo-workflows argo/argo-workflows v1.0.5` | `argo-workflows` |
| `loopzero-cwts` (helm template + kubectl apply) | `loopzero-build-cwts` |

## workload cluster (infra:install:workload)

| Taskfile bare-helm | Helmfile release (runtime) |
|---|---|
| `cert-manager jetstack/cert-manager v1.14.5` | `cert-manager` |
| `traefik traefik/traefik v28.3.0` | `traefik` |

---

## Gaps identified

1. **network-policies** chart at `infra/helm/network-policies` NOT declared in mgmt helmfile.
   Currently installed via bare `helm upgrade --install` in Taskfile.
   **Action:** add release to mgmt helmfile in T2.3 rewrite.

2. **Cross-cluster loki**: Taskfile installs loki in mgmt cluster (`{{.MGMT_CONTEXT}}`)
   via `helm upgrade`, but loki helmfile release lives in `build.helmfile.yaml.gotmpl`.
   **Decision:** Loki belongs in build cluster per audit M08 #18 (separation of
   concerns: build observability lives with build cluster). Taskfile should
   stop installing loki in mgmt; build helmfile already covers it.

3. **dashboards:install** and **infra:install:loki** are separate Taskfile tasks
   that get called via `task: ...` from `infra:install:monitoring`. They should
   be subsumed into helmfile sync — no separate task needed.

---

## EC-01 compliance check (master-sync ordering)

For T2.3 to use **single master `helmfile sync`** (no `--selector`), the
helmfile MUST declare `needs:` between releases that depend on each other:

| Release | Declared needs: | Required? | Status |
|---|---|---|---|
| postgresql | (root) | n/a | ✓ |
| gitea-redis | none | NO (independent) | ✓ |
| gitea | postgresql + gitea-redis | YES | ✓ verified |
| minio | none | NO | ✓ |
| ory-kratos | (verify) | YES — needs postgresql for DSN | **VERIFY in T2.3** |
| ory-hydra | (verify) | YES — needs postgresql for DSN | **VERIFY in T2.3** |
| argocd | (existing) | YES — needs PostgreSQL CR | ✓ |
| opencost (T7.2) | monitoring/prometheus | YES | ✓ |

T2.3 implementation MUST verify kratos+hydra `needs:` declarations include
`loopzero-system/postgresql`. Without that, master-sync may try to install
kratos before postgres is ready, race-failing `requiredEnv "LOOPZERO_DB_PASSWORD"`.

---

## EC-02 compliance — portable secret extraction

Current Taskfile uses `kubectl ... | base64 -d` extensively. **macOS BSD
`base64` uses `-D` (capital), not `-d`** — Mac contributors fail silently.

**Fix:** Use `kubectl -o go-template='{{.data.X | base64decode}}'` which
returns DECODED bytes directly via kubectl's own template engine. Cross-platform:
no pipe to `base64`, no portability bug.

Lines to fix in Taskfile (T2.3):
- Line 22: `PG_ADMIN_PASS=$(kubectl ... -o jsonpath='{.data.postgres-password}' | base64 -d)`
- Line 55: same pattern
- Multiple other lines (grep `base64 -d` to find all)

---

## Decision: T2.3 architecture

Per EC-01: **master `helmfile sync` per cluster** (no `--selector`):

```yaml
infra:install:mgmt:
  cmds:
    - |
      # T2.3.a (EC-01) master sync resolves order via needs:
      export LOOPZERO_DB_PASSWORD=$(kubectl --context {{.MGMT_CONTEXT}} -n loopzero-system \
        get secret postgresql -o go-template='{{`{{.data.password | base64decode}}`}}')
      helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl \
        -e standalone-multi \
        --kube-context {{.MGMT_CONTEXT}} \
        sync
```

**WAIT** — the postgres Secret doesn't exist BEFORE postgres install.
Chicken-and-egg. Solution: extract LOOPZERO_DB_PASSWORD AFTER postgresql install OR
use generate-secrets.sh first (already does this). Real T2.3 must:

1. Pre-sync: ensure LOOPZERO_DB_PASSWORD env is exported (from local secret OR
   from postgres if existing)
2. Run helmfile master sync (postgres + dependent releases)
3. Post-sync verification: confirm dependent pods reached Ready

`generate-secrets.sh` already exports LOOPZERO_DB_PASSWORD before any helmfile
sync. T2.3 must invoke it FIRST.

---

## Status

- T2.1 (this doc): produced 2026-05-22 iter 6
- T2.2 (EC-02 YAML escape): kratos+hydra `values.yaml.gotmpl` fixed in same commit
- T2.3 (Taskfile rewrite): in progress this iter
- T2.4 (LIVE cleanroom validation): **HARD STOP** operator action
