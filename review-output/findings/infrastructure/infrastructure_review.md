# Infrastructure Completeness Audit

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero
**Reviewer:** infrastructure-reviewer
**Scope:** Cluster management, integrations, tools, and Control Plane

---

## Executive Summary

- **Overall infrastructure health:** NEEDS ATTENTION
- **Total findings:** 13 (critical: 1, high: 5, medium: 4, low: 3)
- **Key concern:** Taskfile Helm installs lack version pinning, creating a local/production divergence risk on every `task setup` invocation. Combined with missing network policies on build and workload clusters, the platform has material gaps between its local dev parity claim and actual behavior.

---

## Artifacts Discovered

- **Dockerfiles:** Not reviewed (scoped to infra only)
- **Helm charts:** 13 (argocd, argo-workflows, cert-manager, external-secrets, gitea, loki, minio, monitoring, network-policies, otel, postgresql, promtail, traefik)
- **Docker Compose files:** 0
- **Terraform files:** 0
- **Pulumi IaC files:** 1 program (bootstrap.ts + infra.ts + expose.ts)
- **k3d cluster configs:** 3 (mgmt, build, workload)
- **GitHub Actions workflows:** 4 (ci.yml, build-images.yml, infra-deploy.yml, infra-preview.yml)
- **Taskfile tasks reviewed:** setup, clusters:create, infra:install:mgmt, infra:install:build, infra:install:workload, crds:install, db:migrate

---

## 1. Helm Chart Version Pinning Assessment

### Summary

All 13 Helm charts define pinned image tags within their values.yaml files (no `:latest` tag found). However, there is a significant split between the Taskfile-based local install path and the Pulumi-based production path:

| Path | Version Pinning | Status |
|------|----------------|--------|
| `infra/pulumi/src/components/bootstrap.ts` | `HELM_VERSIONS` const with all 10 charts pinned | PASS |
| `Taskfile.yml` (infra:install:*) | No `--version` flag on any `helm upgrade --install` | FAIL |

**Charts with verified pinned versions (Pulumi path):**
- cert-manager: v1.14.5
- traefik: 28.3.0
- argocd: 7.3.11
- gitea: 10.6.0
- minio: 5.4.0
- argo-workflows: 1.0.5
- prometheus: 25.27.0
- grafana: 8.5.2
- loki: 6.24.0
- promtail: 6.16.6
- external-secrets: 0.14.3 (hardcoded in bootstrap.ts, not in HELM_VERSIONS)

**Image tags in values.yaml:**
- ArgoCD: `v2.13.3` — pinned
- Gitea: `1.22.6` — pinned
- OTel Collector: `0.96.0` — pinned

**Critical violation:**
- `infra/pulumi/src/components/bootstrap.ts:1242`: `image: "bitnami/kubectl:latest"` — the Gitea bootstrap Job uses an unpinned image.

---

## 2. k3d vs Production Parity Assessment

### Kubernetes Version

| Cluster | k3d (local) | Production (DO) |
|---------|------------|-----------------|
| mgmt | Unpinned (k3d default = latest k3s) | 1.32.13-do.1 |
| build | Unpinned | 1.32.13-do.1 |
| workload | Unpinned | 1.32.13-do.1 |

**Finding:** k3d cluster configs do not specify a k3s image version. Running `task setup` installs the latest available k3s, which may or may not be 1.32.x. This violates CLAUDE.md Rule 10 ("Local equals production").

### Node Configuration

| Property | k3d (local) | Production (DO) |
|----------|------------|-----------------|
| mgmt nodes | 1 server + 2 agents | 2–3 nodes (auto-scale) |
| build nodes | 1 server + 2 agents | 2–3 nodes (auto-scale) |
| workload nodes | 1 server + 2 agents | 2–3 nodes (auto-scale) |

The node count is roughly aligned (3 total vs 2–3). k3d uses a single server+agents model vs DOKS managed control plane — this is an acceptable and expected difference.

### Storage

- Local: k3d uses ephemeral volumes (no persistent storage between cluster recreations)
- Production: DO block storage (50Gi primary + replica for PostgreSQL, 100Gi distributed MinIO)

### Networking

- Local: Docker bridge networking for cross-cluster communication, static IPs updated by `infra/scripts/update-cross-cluster-ips.sh`
- Production: DO VPC private networking, Cloudflare Tunnel for external traffic

The networking model difference is documented and expected.

---

## 3. Taskfile Idempotency Assessment

| Task | Idempotent | Mechanism |
|------|-----------|-----------|
| `clusters:create` | YES | `grep -q '"name":"loopzero-mgmt"' \|\| k3d cluster create` |
| `registry:create` | YES | `grep -q '"name":"k3d-loopzero-registry"' \|\| k3d registry create` |
| `helm:repos` | YES | `helm repo add ... \|\| true` |
| `infra:install:mgmt` | YES | `helm upgrade --install` (idempotent) |
| `infra:install:build` | YES | `helm upgrade --install` + `kubectl apply` |
| `infra:install:workload` | YES | `helm upgrade --install` + `kubectl apply` |
| `crds:install` | YES | `kubectl apply -f` (idempotent) |
| `db:migrate` | PARTIAL | SQL uses `IF NOT EXISTS` but no migration state tracking |
| `tokens:provision` | YES | Creates tokens only if missing |
| `dev:seed` | YES | `ON CONFLICT DO NOTHING` |

**Key concern:** `db:migrate` runs all .up.sql files unconditionally without tracking applied migrations (no golang-migrate schema_migrations table). The current migrations use `CREATE TABLE IF NOT EXISTS` which is safe, but this pattern will fail silently on migrations with non-idempotent DDL (indexes, constraints, column additions). This is a time-bomb as the schema evolves.

---

## 4. GitHub Actions Completeness Assessment

### SHA Pinning

All 49 action references across all 4 workflows are SHA-pinned. No floating version tags found. This is excellent.

### State Backend

- Pulumi state: `s3://loopzero-infra-pulumi-state?endpoint=nyc3.digitaloceanspaces.com` — properly externalized to DO Spaces
- No Pulumi Cloud dependency — correct for air-gapped or self-hosted scenarios

### 3-Phase Deploy Completeness

| Step | Name | Status |
|------|------|--------|
| Phase 1 | Provision infrastructure (pulumi up with skipAwait) | Present |
| Phase 2 | Build + push container images (docker build + push) | Present |
| Phase 3 | Convergence deploy (pulumi up, images now available) | Present |
| Post | Bootstrap platform (credentials, seed data) | Present |
| Post | Capture execution state | Present |

The 3-phase pipeline is complete. The pre-destroy cleanup (finalizer removal, cloud resource deletion) is comprehensive and handles all edge cases (orphaned tunnels, CDN endpoints, stuck VPCs).

### Destroy Path Safety

The destroy path is thorough: removes k8s finalizers, scales down cloudflared, deletes CF resources, then handles Pulumi state cleanup. The nuclear state clear option (keeping only the Stack resource) is documented as a last resort. This is acceptable.

### Potential Issues

- Step 2 in infra-deploy.yml builds images with a hardcoded `:dev` tag regardless of the stack. On a prod deploy via workflow_dispatch, this would push `:dev` to the production registry instead of `:latest`. The `build-images.yml` workflow correctly handles `:latest` vs `:dev` based on branch, but the embedded Step 2 in infra-deploy.yml does not.

---

## 5. Network Policies Assessment

| Cluster | Namespace | Network Policy | Status |
|---------|-----------|---------------|--------|
| mgmt | loopzero-system | `default-deny-ingress` + per-component rules | PRESENT |
| mgmt | argocd | None defined | MISSING |
| build | argo | None defined | MISSING |
| workload | platform-system | None defined | MISSING |
| workload | user app namespaces | None defined | MISSING |

The `loopzero-system.yaml` policy is well-designed: default deny ingress + explicit per-component egress policies for loopzero-api, tenant-controller, PostgreSQL, Gitea, and MinIO. The Gitea ingress correctly allows ArgoCD repo-server cross-namespace access.

**Gaps:**
1. **ArgoCD namespace** (mgmt cluster): No network policy. ArgoCD has admin access to all cluster resources — it should have restricted ingress (only from loopzero-api on port 8080) and restricted egress (to Gitea, runtime cluster API server).
2. **Argo namespace** (build cluster): No network policy. Build workflow pods should only egress to MinIO (9000), container registry (443), and DNS. An escaped build container could pivot to the cluster API.
3. **Workload cluster**: No network policy for any namespace. User app namespaces have unrestricted east-west traffic by default.

---

## 6. Missing Infrastructure Assessment

### PostgreSQL Backup

| Item | Status | Notes |
|------|--------|-------|
| Local dev backup | Not configured | Local k3d PG is ephemeral |
| Production backup | DO managed automatic backups | 7-day retention, automatic failover |
| WAL archiving (PITR) | Not configured | values.production.yaml documents it but it is never applied |
| Cross-region backup | Not configured | Single-region risk |
| Backup monitoring | Alert defined but metric source unclear | DO managed DB doesn't emit pg_last_backup_timestamp |

### Gitea Backup

| Item | Status | Notes |
|------|--------|-------|
| Backup CronJob | Does not exist | Alert fires on non-existent metric |
| Backup monitoring | Alert defined, no backing job | loopzero_gitea_last_backup_timestamp never emitted |

Gitea is the most critical missing backup: it stores the authoritative desired-state git repos for all tenants. If the Gitea PV is lost, all GitOps state is gone.

### Secret Rotation

External Secrets Operator (ESO) is deployed with a ClusterSecretStore pointing to the mgmt cluster. ESO's default behavior syncs secrets on creation and on a polling interval (configured per ExternalSecret CR). No automated secret rotation is implemented — secrets are manually created and never rotated. The ESO infrastructure is present but rotation workflows are absent.

### Health Check Completeness

All platform components have liveness and readiness probes configured (ArgoCD, Argo Workflows, cert-manager, Gitea, MinIO, PostgreSQL, Traefik, OTel, monitoring, Grafana). The promtail DaemonSet does not have explicit probes in values.yaml (relies on chart defaults).

---

## Findings (Detailed)

### INFRA-C-001: Taskfile helm installs missing --version flag
- **Severity:** high
- **Area:** taskfile / local dev
- **Location:** Taskfile.yml:665, 681, 687, 695, 712, 732, 741, 754, 760, 773, 779
- **Description:** All 9 `helm upgrade --install` calls in infra:install:* tasks lack `--version` flags. The Taskfile installs whatever the Helm repo serves at time of execution, which may differ from what Pulumi bootstrap.ts deploys in production.
- **Evidence:** No `--version` flag on any helm install line. Compare: bootstrap.ts has `const HELM_VERSIONS = { certManager: "v1.14.5", traefik: "28.3.0", ... }`.
- **Recommendation:** Add `--version` matching HELM_VERSIONS to all Taskfile helm install commands.

### INFRA-C-002: bitnami/kubectl:latest in Gitea bootstrap Job
- **Severity:** critical
- **Area:** pulumi / kubernetes
- **Location:** infra/pulumi/src/components/bootstrap.ts:1242
- **Description:** The Gitea bootstrap Kubernetes Job uses `image: "bitnami/kubectl:latest"`. This is the only `:latest` tag found in the entire infrastructure definition and represents a reproducibility and security risk.
- **Evidence:** `bootstrap.ts:1242: image: "bitnami/kubectl:latest"`
- **Recommendation:** Pin to a specific version, e.g. `bitnami/kubectl:1.32.0`, matching the cluster's Kubernetes version.

### INFRA-C-003: k3d clusters have no pinned Kubernetes version
- **Severity:** high
- **Area:** k3d / local dev
- **Location:** infra/k3d/mgmt-cluster.yaml, build-cluster.yaml, workload-cluster.yaml
- **Description:** k3d cluster configs do not specify a k3s image. k3d defaults to the latest available k3s release, which may be a different minor version than production's 1.32.13.
- **Evidence:** All three k3d YAML files have no `image:` field. Production uses `1.32.13-do.1`.
- **Recommendation:** Add `image: rancher/k3s:v1.32.12-k3s1` (or appropriate patch version) to each k3d config.

### INFRA-C-004: Build and workload clusters have no network policies
- **Severity:** high
- **Area:** kubernetes / networking
- **Location:** infra/helm/network-policies/ (missing build-cluster.yaml, workload-cluster.yaml)
- **Description:** Only loopzero-system (mgmt) has network policies. The build cluster argo namespace and workload cluster are completely open to east-west traffic.
- **Evidence:** `find infra/helm/network-policies/ -type f` returns only `loopzero-system.yaml`.
- **Recommendation:** Create network policies for the argo namespace (build cluster) and workload cluster namespaces.

### INFRA-C-005: Gitea backup CronJob does not exist
- **Severity:** high
- **Area:** backup / stateful services
- **Location:** infra/helm/monitoring/manifests/prometheusrule-critical.yaml:65
- **Description:** A critical alert references `loopzero_gitea_last_backup_timestamp` but no CronJob exists to generate this metric or perform the backup. Gitea stores all GitOps state.
- **Evidence:** No CronJob found for Gitea backup. Alert expression references non-existent metric.
- **Recommendation:** Implement a Gitea backup CronJob using the Gitea API or git bundle approach.

### INFRA-C-006: Production PostgreSQL backup configuration is not applied
- **Severity:** high
- **Area:** backup / production
- **Location:** infra/helm/postgresql/values.production.yaml
- **Description:** values.production.yaml documents backup configuration but is never referenced in Pulumi or workflows. Production uses DO managed database without verified WAL archiving.
- **Evidence:** bootstrap.ts has no reference to values.production.yaml. The PrometheusRule monitors pg_last_backup_timestamp which DO managed DB does not emit.
- **Recommendation:** Configure DO managed database backup settings explicitly in Pulumi. Update monitoring to use DO API-based backup verification.

### INFRA-C-007: Alertmanager receivers point to dev-null webhook
- **Severity:** medium
- **Area:** observability / alerting
- **Location:** infra/helm/monitoring/values.yaml:143
- **Description:** Both default and critical alert receivers send to `http://localhost:9093/dev-null`, meaning all alerts are silently dropped including critical production alerts.
- **Evidence:** `monitoring/values.yaml:143-150: url: 'http://localhost:9093/dev-null'` for both receivers.
- **Recommendation:** Configure real webhook endpoints in a production values overlay.

### INFRA-C-008: db:migrate task has no migration state tracking
- **Severity:** medium
- **Area:** taskfile / database
- **Location:** Taskfile.yml:847
- **Description:** `db:migrate` executes all .up.sql files unconditionally without tracking applied migrations. While current migrations use `IF NOT EXISTS`, this pattern will break as the schema evolves with non-idempotent DDL.
- **Evidence:** Task runs `for f in $(ls api/internal/db/migrations/*.up.sql | sort); do psql < "$f"; done` with no state tracking.
- **Recommendation:** Use golang-migrate CLI: `migrate -database <dsn> -path api/internal/db/migrations up`.

### INFRA-C-009: PostgreSQL PDB allows maxUnavailable: 1 on single-replica
- **Severity:** medium
- **Area:** kubernetes / availability
- **Location:** infra/helm/postgresql/templates/pdb.yaml:11
- **Description:** maxUnavailable: 1 on a single-replica PostgreSQL deployment provides no protection during node drains — the only pod can be evicted.
- **Evidence:** `pdb.yaml:11: maxUnavailable: 1`. `values.yaml`: no `architecture: replication` setting.
- **Recommendation:** Change to `minAvailable: 1` for the dev single-replica case.

### INFRA-C-010: Promtail and ESO values.yaml missing securityContext
- **Severity:** medium
- **Area:** kubernetes / security
- **Location:** infra/helm/promtail/values.yaml, infra/helm/external-secrets/values.yaml
- **Description:** These two charts do not configure `runAsNonRoot: true`. All other 11 charts explicitly set this field.
- **Evidence:** `grep runAsNonRoot infra/helm/promtail/values.yaml`: no output. Same for external-secrets.
- **Recommendation:** Add `securityContext.runAsNonRoot: true` to both values files.

### INFRA-C-011: Staging stack config is incomplete
- **Severity:** medium
- **Area:** pulumi / multi-environment
- **Location:** infra/pulumi/Pulumi.staging.yaml
- **Description:** The staging stack lacks build cluster configuration and encrypted credential secrets — it cannot be successfully deployed.
- **Evidence:** Pulumi.staging.yaml has no buildK8sVersion, buildNodeClass, buildMinNodes, buildMaxNodes, or secure: credential blocks.
- **Recommendation:** Complete the staging stack config matching the dev stack structure.

### INFRA-C-012: MinIO credentials Secret on build cluster contains hardcoded dev credentials
- **Severity:** low
- **Area:** kubernetes / secrets
- **Location:** infra/helm/minio/minio-credentials-secret.yaml:21
- **Description:** Build cluster Secret has hardcoded `loopzero-admin` / `loopzero-dev-password` credentials. Comment says to replace for production but no automated path exists.
- **Evidence:** `minio-credentials-secret.yaml:21-22: AWS_ACCESS_KEY_ID: "loopzero-admin", AWS_SECRET_ACCESS_KEY: "loopzero-dev-password"`.
- **Recommendation:** Use an ExternalSecret to sync credentials from the production secrets store.

### INFRA-C-013: No HPA configured for any control plane component
- **Severity:** low
- **Area:** kubernetes / scaling
- **Location:** infra/helm/
- **Description:** No HorizontalPodAutoscaler is defined for loopzero-api, tenant-controller, Traefik, or ArgoCD server.
- **Evidence:** `grep -rn HorizontalPodAutoscaler infra/helm/ --include=*.yaml`: 0 results.
- **Recommendation:** Add HPA for loopzero-api (CPU-based) and Traefik (connections-based) as starting points.

---

## Single Points of Failure Analysis

| Component | SPOF Risk | Mitigation Present |
|-----------|-----------|-------------------|
| Gitea | YES — single replica, no backup CronJob | PDB (minAvailable: 1), not HA |
| PostgreSQL (local) | YES — single replica | PDB (maxUnavailable: 1, flawed — see C-009) |
| PostgreSQL (production) | NO — DO managed with automatic failover | DO 2-node managed cluster |
| MinIO (local) | YES — standalone mode | PDB exists |
| MinIO (production) | NO — distributed mode (4 replicas) | values.production.yaml |
| ArgoCD | PARTIAL — single server replica | PDB (minAvailable: 1) |
| Traefik | NO — 2 replicas, PDB, anti-affinity | Fully HA |
| Loki | YES — single binary, 1 replica | No PDB |
| cert-manager | PARTIAL | No PDB |

---

## Recommendations (Prioritized)

1. **[Critical]** Pin `bitnami/kubectl:latest` to a versioned image in `bootstrap.ts:1242`. This is the only `:latest` tag in the entire codebase and violates the foundational reproducibility principle.

2. **[High]** Add `--version` to all Taskfile helm installs. The Taskfile is the primary developer workflow and currently pulls floating chart versions on every `task setup`.

3. **[High]** Create Gitea backup CronJob. Gitea is the sole source of desired state for all tenant deployments. Its loss means unrecoverable GitOps state for every tenant.

4. **[High]** Create network policies for the build cluster (argo namespace) and workload cluster. The build cluster is a particularly high risk — malicious build scripts have no network isolation from the cluster control plane.

5. **[High]** Pin Kubernetes version in k3d cluster configs using the `image:` field to match production's 1.32.13.

6. **[Medium]** Replace `db:migrate` implementation with golang-migrate CLI to track applied migrations. As the schema grows, the current approach becomes increasingly risky.

7. **[Medium]** Fix PostgreSQL PDB: change `maxUnavailable: 1` to `minAvailable: 1` for the single-replica dev configuration.

8. **[Medium]** Configure real Alertmanager webhook receivers in a production overlay. Currently all alerts are silently dropped.

9. **[Medium]** Complete the staging Pulumi stack configuration to enable staging deployments.

10. **[Low]** Add `runAsNonRoot: true` to promtail and external-secrets values.yaml.
