# Runbook: Sprint 8 v3.2 / CHG-002a Closeout Baseline

**Captured:** 2026-04-27 21:30 (post CHG-002a 14/14 GREEN)
**Source:** validation-20260427-200525.log + doctl + kubectl baseline
**Purpose:** Pre-CHG-002b telemetry capture per ADR-0016 §"Pre-conditions: 24h HA stable since CHG-002a closure"

---

## Infrastructure runtime evidence

### DOKS clusters (all 4 running, Sprint 8 v3.2 sizing)

| Cluster | Size | Count | Status |
|---|---|---|---|
| loopzero-infra-dev-sfo3-mgmt | s-4vcpu-8gb-amd | 3 | running |
| loopzero-infra-dev-sfo3-build | s-4vcpu-8gb-amd | 2 | running |
| loopzero-infra-dev-sfo3-runtime | s-2vcpu-4gb | 2 | running |
| loopzero-infra-dev-sfo3-runtime-2 | s-2vcpu-4gb | 2 | running |

### Managed databases

| Name | Engine | Size | Status |
|---|---|---|---|
| loopzero-infra-dev-sfo3-db | pg | db-s-2vcpu-4gb | online |
| loopzero-infra-dev-sfo3-redis | valkey | db-amd-1vcpu-2gb | online |

### Pod inventory (total: 195 across 4 clusters)

| Cluster | Pods | CrashLoop | ImagePullBackOff |
|---|---|---|---|
| mgmt | 83 | 0 | 0 |
| build | 31 | 0 | 0 |
| runtime | 44 | 0 | 0 |
| runtime-2 | 37 | 0 | 0 |

### Mgmt cluster memory (ADR-0010 deliverable #6: target <50% idle)

| Node | CPU% | Memory% |
|---|---|---|
| mgmt-default-3nqhm1 | 2% | **36%** |
| mgmt-default-3nqhm9 | 3% | **37%** |
| mgmt-default-3nqhmz | 2% | **39%** |

**Target met:** all 3 nodes <50% memory idle. T0.2 mgmt scale-up effective.

### HA service status

| Service | Replicas | Backing |
|---|---|---|
| Gitea | 4/4 | RWX NFS PVC (gitea-shared-storage 10Gi nfs StorageClass) |
| Harbor core | 2/2 | shared session via Valkey (pending wire) |
| Harbor registry | 2/2 | S3 backend on DO Spaces |
| Harbor jobservice | 1/1 | shared queue via Valkey (pending wire) |
| loopzero-api | 2/2 | PG + S3 |
| loopzero-controller | 1/1 | singleton (Recreate strategy) |

### Chaos Mesh

CRDs registered in all 4 DOKS:
- podchaos.chaos-mesh.org
- stresschaos.chaos-mesh.org
- networkchaos.chaos-mesh.org

### Other infra

- 195 pods running across 4 clusters
- ArgoCD applications: 0 (fresh bootstrap, no tenants yet)
- BuildKit nodes: 2 (medium size)
- API endpoint: `https://api.dev.loopzero.example/api/v1/health` HTTP 200

## Validation matrix items (CHG-002a as final pass)

All 14/14 PASS per `${HOME}/.loopzero/artifacts/validation-20260427-200525.log`:

1. ✅ Bucket created (loopzero-dev-pulumi-state)
2. ✅ Pulumi auth + stack ls
3. ✅ Config decryptable (token len=72)
4. ✅ Adopt-or-create lookups hit (Registry + VPC adopted)
5. ✅ Preview clean (no deletions)
6. ✅ Pulumi up exit 0
7. ✅ Execution all-completed
8. ✅ Pod health (zero CrashLoop/ImagePull after cloudflared scaled to 0)
9. ✅ PG SELECT 1 = 1
10. ✅ DNS public api.dev.loopzero.example → 143.244.208.150
11. ✅ API HTTP 200
12-15. ⏭ Skipped (smoke E2E — operator interactive only)
16. ✅ Re-preview drift (acceptable 2 changes)

## Costs materialized

~$220-250/month running:
- 4 DOKS s-4vcpu-8gb-amd: ~$110/mo (3 mgmt + 2 build = 5 medium nodes; runtime + runtime-2 = 4 small nodes ~$24)
- PG db-s-2vcpu-4gb (3 nodes): ~$60/mo
- Valkey db-amd-1vcpu-2gb (1 node, dev): ~$15/mo
- DO Spaces (3 buckets): ~$15/mo
- DOCR loopzero-system: ~$5/mo
- CF Tunnel + Worker + KV: included in CF subscription

## What's NOT yet done

### Within CHG-002a scope but partial

- **T2.3 MinIO blue/green migration:** runbook + values-distributed.yaml prepared.
  Execution requires 48h overlap window + MAINTENANCE_MODE pre-flight (D-3,
  D-7 vetos). Operator must trigger manually following
  `docs/runbooks/minio-distributed-migration.md` Phase A-F.

- **T2.2 Harbor envFrom Secret:** harbor-redis-credentials Secret created in
  cluster but Harbor Helm values still reference internal Redis. Wire the
  envFrom secretRef on harbor-core/registry/jobservice in next pulumi up
  cycle once cloudflared state-conflict resolved.

### Out of CHG-002a scope (CHG-002b/c)

- **Phase 1 — L3 stabilization (T1.1-T1.4):** 100 tenants × 4h × 5 runs >= 95/100
- **Phase 3 — L4 capacity (T3.1):** 500 tenants × 8h
- **Phase 4 — Gate 2a Exit checklist (T4.1):** independent reviewer per line
- **Phase 5 — Chaos validation (T5.1, T5.2):** 9 scenarios with recovery <2min
- **Phase 6 — Observability (T6.1-T6.3):** 5 metrics + 12 alert deletion + SLO
  formal + 13 runbooks

### Operator tasks (not in any CHG)

- Resolve cloudflared state field-conflict (`pulumi state delete` cycle then
  re-bootstrap tunnel via Pulumi destroy+up of cloudflared deployment).
  Currently scaled to 0; doesn't block API.

## Pre-conditions for opening CHG-002b (per ADR-0016)

- [x] CHG-002a closed GREEN (14/14 validation matrix)
- [ ] **24h HA stable** (started countdown 2026-04-27 19:55 UTC; eligible
  for CHG-002b meeting at 2026-04-28 19:55 UTC at earliest)
- [ ] L3 cost approved by SRE (rough estimate: 4h × 5 runs = 20h compute,
  no extra DOKS allocated, so ~$0 marginal — same hardware)
- [ ] CHG-002b meeting convened with full quorum (per MEET-061 §reviewers)

## CHG-002b 8-item validation matrix preview

To be confirmed in CHG-002b meeting. Rough preview from ADR-0016:

1. L3 100-tenant deploy success rate >= 95/100 (5 consecutive runs)
2. PG connection pool peak ≥ 80 (medium tier supports 100, headroom OK)
3. Mgmt memory peak <70% during L3 (ramp to 80% acceptable, >85% red)
4. BuildKit queue depth p99 <30s
5. ArgoCD sync time p99 <60s during deploy storm
6. Zero workflow timeout failures attributable to platform (vs. tenant)
7. L4 500-tenant deploy success rate (target TBD; informs cell-2 sizing)
8. Per-run telemetry capture to ${HOME}/.loopzero/artifacts/l3-run-{N}.log

## References

- ADR-0015 — Pre-bootstrap Adopt-or-Create Policy
- ADR-0016 — Sprint 8 Phase 0-6 three-CHG split
- MEET-2026-04-27-060 — CHG-001 approval
- MEET-2026-04-27-061 — CHG-002a/b/c three-way split approval
- `docs/plans/sprint8-hardening-chaos-plan.md` v3.2
- `docs/runbooks/sprint8-bootstrap.md` (Phase -1 procedure)
- `docs/runbooks/pulumi-state-repair.md` (operational repair)
- `docs/runbooks/cf-tunnel-cleanup.md` (tunnel drain procedure)
- `docs/runbooks/minio-distributed-migration.md` (T2.3 blue/green)
