# ADR-002: Container Registry Migration — Harbor + DO Spaces

## Status

Accepted (2026-04-11)

## Context

DigitalOcean Container Registry (DOCR) Professional tier has a hard cap of 100 GB storage. This is the highest tier offered — no upgrade path exists. At the 100k concurrent applications target (~150 TB storage, ~1M live images, ~450 TB/month pull throughput), DOCR is mathematically infeasible. The registry must be migrated before Sprint 6 (image signing with cosign + syft).

Beyond storage limits, the current setup has two critical security findings:

1. **No tenant isolation on image access.** All images live in a single flat namespace. Any build pod can pull any tenant's images. For a multi-tenant PaaS accepting payment, this is a security requirement today, not a future optimization.
2. **DO API Token used as Docker registry auth.** The `buildkit-registry-config` secret contains the full DO API token. A leaked build pod secret grants full DO account control.

## Decision

**Harbor (minimal deployment, 6-7 pods) with DO Spaces as the S3 storage backend.**

Harbor runs in the mgmt cluster alongside existing infrastructure. It uses the existing managed PostgreSQL instance (separate `harbor_db` database, connection pool capped at 10). Blob storage is offloaded to DO Spaces, keeping Harbor stateless for image data.

### Key architectural choices

- **One Harbor project per tenant.** Provides native RBAC isolation without custom code.
- **Robot accounts with TTL <= 24h.** Build pods get tenant-scoped push/pull tokens. Workload pods get tenant-scoped pull-only tokens. Automatic rotation by TenantController.
- **Minimal deployment only.** Trivy, Notary, Chartmuseum, and Portal are disabled. Harbor serves as a registry with tenant isolation, not a full artifact management platform.
- **Immutable tags per project.** Prevents tag overwrite attacks.
- **Online GC (Harbor 2.9+).** Eliminates the historical stop-the-world GC concern. Scheduled off-peak with monitoring.

## Options Evaluated

| Option | Storage | Registry | Monthly Cost (100k) | Tenant Isolation | Verdict |
|--------|---------|----------|---------------------|------------------|---------|
| DOCR Professional | DO-managed | DO-managed | $0 (included) | None | **Infeasible** — 100 GB hard cap, no upgrade path |
| ECR (AWS) | AWS S3 | AWS ECR | ~$12k (egress) | Native | **Rejected** — cross-cloud egress cost, latency, operational split |
| GCR (Google) | GCS | GCR | Higher than ECR | Native | **Rejected** — worse economics, same cross-cloud problems |
| GHCR (GitHub) | GitHub-managed | GitHub-managed | ~$69k (storage+bandwidth) | Per-org only | **Rejected** — prohibitive cost, insufficient isolation granularity |
| zot + DO Spaces | DO Spaces | zot (2 pods) | ~$750 (Spaces) | None native | **Rejected** — Security VETO: no tenant isolation. Building equivalent = 2-3 sprints custom security code |
| zot + Cloudflare R2 | CF R2 | zot (2 pods) | ~$450 (R2) | None native | **Rejected** — SRE VETO: correlated blast radius with CF Tunnel (CF incident = total platform outage) |
| **Harbor + DO Spaces** | DO Spaces | Harbor (6-7 pods) | ~$750 (Spaces) + ~$50 (compute) | **Native (projects, robot accounts, quotas)** | **Chosen** |
| Harbor + Cloudflare R2 | CF R2 | Harbor (6-7 pods) | ~$450 (R2) + ~$50 (compute) | Native | **Rejected** — SRE VETO: same correlated blast radius as zot + R2 |

## Justification

### Why Harbor over zot

The initial assessment (MEET-047) chose zot for its simplicity (2 pods, no PG dependency). MEET-048 reversed this decision for three reasons:

1. **Tenant isolation is a present requirement, not future.** Platform Security exercised absolute VETO on any solution without per-tenant image access enforcement. Building an auth proxy on zot to achieve equivalent isolation would require 2-3 sprints of custom security-critical code. This directly violates "Don't Reinvent the Wheel" (Principle #9) in a domain where custom authz proxies are where vulnerabilities live.

2. **Harbor operational cost was overestimated.** Minimal deployment is 6-7 pods (core x2, registry x2, jobservice x1, Redis x1), not 15-20. Online GC in Harbor 2.9+ eliminates the stop-the-world concern. Using existing managed PG means no new database infrastructure.

3. **zot engineering cost was underestimated.** Maintaining a custom auth proxy, credential controller, quota enforcement, audit logging, and retention policy indefinitely costs more engineering hours than operating a proven, CNCF-graduated system.

### Why DO Spaces over Cloudflare R2

- **Same-datacenter latency.** Harbor, BuildKit, and Spaces are all in SFO3. No cross-cloud round trips for blob operations.
- **No correlated failure domain.** SRE vetoed R2 because Cloudflare already handles Tunnel (runtime traffic), Workers (static delivery), KV (routing), and Zone Management (custom domains). Adding registry storage to CF means a single CF incident takes down the entire platform.
- **Already in stack.** DO Spaces is used for source bundles, static assets, Loki chunks, and Pulumi state. Operational team is familiar. No new vendor credentials or billing.

## Trade-offs Accepted

1. **PostgreSQL as a failure domain for registry metadata.** DO managed PG has automated failover (seconds) and daily backups with PITR (minutes). This is faster recovery than zot's BoltDB rescan from S3 (hours at 150 TB). Mitigated by: separate database, capped connection pool (10), `pg_stat_activity` monitoring.

2. **6-7 additional pods in mgmt cluster.** The cluster already runs 40+ pods. This is incremental, not transformative. Resource limits are mandatory on all Harbor pods.

3. **Harbor upgrade complexity.** Historical destructive migrations exist. Mitigated by staging-first mandatory policy for every upgrade. Rollback plan: PG snapshot + S3 versioning.

4. **Online GC unproven at 1M images.** PG-based mark phase is O(n) rows. Mitigated by off-peak scheduling, monitoring (duration, blobs deleted, errors), and the ability to pause GC if problems emerge.

## Dissent

Production Design Reviewer maintained that zot remains the better production bet. Their position: "PG irrecoverable failure domain outweighs Harbor multi-tenancy benefits. BoltDB is reconstructible from S3 by design. Eliminating a failure domain is worth more than native multi-tenancy features."

This dissent was not incorporated because Security's absolute VETO on solutions without tenant isolation is binding, and the "build it custom" alternative to achieve equivalent isolation on zot violates Principle #9 for security-critical code.

## Consequences

### Immediate

- Harbor Helm chart added to `infra/helm/harbor/` with minimal deployment and hardened values
- Pulumi `HarborRegistryProvider` implementing `ContainerRegistrySpec`/`ContainerRegistryOutput`
- TenantController extended: create Harbor project + robot account per tenant
- Registry monitoring package deployed before migration (canary push/pull, Prometheus metrics, alerts, Grafana dashboard, `pg_stat_activity`)
- Migration runbook + rollback runbook
- Dual-push during migration (both DOCR and Harbor receive images)
- VAP admission policy updated to allow Harbor registry prefix, migrated from Audit to Deny mode
- Image mirror script (`infra/scripts/registry-mirror/`) copies existing DOCR images to Harbor

### Post-migration (after 30 days dual-operation)

- DOCR decommissioned
- Single registry reference via `REGISTRY_URL` environment variable
- Spegel P2P image cache on workload cluster (before 10k apps, reduces cross-cluster pull traffic)

### Mandatory gates before migration

1. Validate DO Spaces bucket-scoped access keys (if unavailable, create separate DO account)
2. Eliminate DO API Token as registry auth (pre-existing CRITICAL finding)
3. Deploy monitoring package with functional alerts

## Meeting Records

- **MEET-2026-04-11-047:** Initial registry strategy assessment. Chose zot + DO Spaces. Superseded by MEET-048.
- **MEET-2026-04-11-048:** Reassessment. Reversed to Harbor + DO Spaces. Three errors in MEET-047 corrected: tenant isolation treated as present (not future), Harbor operational cost corrected (6-7 pods, not 15-20), zot custom isolation cost acknowledged (2-3 sprints). Security VETO on zot without tenant isolation was binding. SRE lifted Harbor veto based on new evidence. Final vote: 3-1 (Production Design Reviewer dissented).
