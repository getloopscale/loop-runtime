// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// =============================================================================
// LOOPRUNTIME DR DRILL — Pulumi Project (PROD-IDENTICAL 4-CLUSTER TOPOLOGY)
// =============================================================================
//
// Plan: 2026-05-16-drill-prod-identical-multicluster-plan.md (v1.1)
// MEET-ASYNC-AMENDMENT-2026-05-16-001 (pending ratification)
//
// Per CTO directive 2026-05-16 (English: "our system will NEVER work with a single
// cell, remove that possibility, and we will change the drill-test to be the SAME as
// prod, and it must run the real tests 100% faithful to production"), verbatim:
// "nosso sistema NUNCA vai trabalhar com single cell, remova essa posibilidade,"  // english-only: verbatim CTO directive, kept as evidence
// "e vamos alterar o drill-test para ser IGUAL prod e ele deve fazer os"  // english-only: verbatim CTO directive, kept as evidence
// "teste reais 100% fiel a produção."  // english-only: verbatim CTO directive, kept as evidence
//
// This stack mirrors `infra/pulumi/src/components/infra.ts:139-186` 4-cluster
// topology (mgmt + build + runtime + runtime-2), enabling:
//   - Multi-cell routing real (cell-0 → runtime, cell-1 → runtime-2)
//   - Build cluster isolation real
//   - Cross-cluster ArgoCD sync exercised
//   - Stage 2 cell2-soaks 24h soak feasible (via expiresInHours param)
//   - DR drill against multi-cluster topology
//
// ADRs (per plan v1.1):
//   D1: 4-cluster topology mirror prod
//   D2: Sizing prod tier specs (mgmt=medium s-4vcpu-8gb-amd, runtime/build=small)
//   D3: Cell mapping cell-0 → runtimeCluster, cell-1 → runtimeCluster2
//   D4: Managed PG mgmt-tier (db-s-2vcpu-4gb) ephemeral per-run
//   D5: Shared VPC loopruntime-drill-vpc-sfo3 (10.40.0.0/16) — disjoint from prod 10.20/homolog 10.30
//   D6: Auto-destroy watchdog parameterizable via expiresInHours (default 8h, max 48h)
//   D7: Drill credentials full isolation (DRILL_DO_TOKEN distinct from prod)
//   EC-03 fix: CF Zone protect: false (ephemeral by design; isolation via account+token D7)
//
// Lifecycle:
//   1. Operator authorizes per MEET-ASYNC-AMENDMENT-2026-05-16-001
//   2. infra/pulumi-drill && pulumi up                    (~25min provision 4 clusters)
//   3. helmfile -e drill sync                              (~20-30min platform bootstrap)
//   4. drill-mint-jwt.sh → JWT for L-series tests
//   5. Run drill scenarios (L0-L4 + Stage 1/2)
//   6. infra/pulumi-drill && pulumi destroy                (~10min mandatory teardown)
//   7. drill-teardown-validator.sh (doctl fallback)        (T4.1.bis)

import * as pulumi from "@pulumi/pulumi";
import * as digitalocean from "@pulumi/digitalocean";
import * as cloudflare from "@pulumi/cloudflare";
// Plan v1.2 T3.1 — Component Resources (incremental Pulumi componentization per ADR D1).
import { LoopRuntimeSpacesBucket } from "./components/loopruntime-spaces-bucket";
import { LoopRuntimeVpc } from "./components/loopruntime-vpc";
import { LoopRuntimeCFZone } from "./components/loopruntime-cf-zone";
import { LoopRuntimeDOProject } from "./components/loopruntime-do-project";
import { LoopRuntimeManagedDb } from "./components/loopruntime-managed-db";
import { LoopRuntimeDOKSCluster } from "./components/loopruntime-doks-cluster";

const config = new pulumi.Config("loopruntime-drill");

// --- Stack-wide config ---
const domain = config.require("domain");
const doProject = config.require("doProject");
const region = config.require("region") as digitalocean.Region;
const expiresInHours = config.getNumber("expiresInHours") ?? 8;

// --- Cluster sizing config (mirrors prod Pulumi.dev.yaml keys) ---
const mgmtK8sVersion = config.require("mgmtK8sVersion");
const mgmtNodeSize = config.get("mgmtNodeSize") ?? "s-4vcpu-8gb-amd"; // medium per D2
const mgmtNodeCount = config.getNumber("mgmtNodeCount") ?? 3;
const buildK8sVersion = config.get("buildK8sVersion") ?? mgmtK8sVersion;
const buildNodeSize = config.get("buildNodeSize") ?? "s-2vcpu-4gb"; // small per D2
const buildNodeCount = config.getNumber("buildNodeCount") ?? 2;
const runtimeK8sVersion = config.get("runtimeK8sVersion") ?? mgmtK8sVersion;
const runtimeNodeSize = config.get("runtimeNodeSize") ?? "s-2vcpu-4gb"; // small per D2
const runtimeNodeCount = config.getNumber("runtimeNodeCount") ?? 2;

// --- Safety: 48h ceiling on expires-at (T3.4.bis EC-01 fix) ---
if (expiresInHours > 48) {
  throw new Error(
    `loopruntime-drill:expiresInHours=${expiresInHours} exceeds 48h safety ceiling. ` +
      `Drill is ephemeral; if you need longer, use prod canary stage instead.`,
  );
}
if (expiresInHours < 1) {
  throw new Error(
    `loopruntime-drill:expiresInHours=${expiresInHours} below 1h minimum. ` +
      `Provision time alone is ~25min; <1h cannot complete a smoke run.`,
  );
}

// --- Computed expires-at timestamp (ISO date, used in resource tags) ---
const expiresAtIso = new Date(Date.now() + expiresInHours * 3600 * 1000)
  .toISOString();

// --- Common tags applied to ALL resources (T4.1 watchdog scans by tag) ---
// DO tag rules: lowercase letters, numbers, colons, dashes, underscores. Max 255 chars.
const expiresAtTagSafe = expiresAtIso
  .replace(/[:.]/g, "-")
  .replace(/[^a-z0-9-]/gi, "")
  .toLowerCase()
  .slice(0, 64);
const drillTags = [
  "loopruntime-drill",
  `loopruntime-drill-expires-at-${expiresAtTagSafe}`,
];

// =============================================================================
// 1. CF Zone (separate from production getloopscale.com)
// =============================================================================
// EC-03 fix: NO protect flag on this resource. Previously the protect option
// blocked `pulumi destroy --stack drill` (validated 2026-05-16 — commit
// 71d6c460 left zone orphan). Drill is ephemeral by design; isolation
// guarantees come from D7 (separate account + scoped token).

// Plan v1.2 T3.1 — LoopRuntimeCFZone component (was inline new cloudflare.Zone).
const drillZone = new LoopRuntimeCFZone("drill-zone", {
  domain,
  accountId: config.requireSecret("cfAccountId"),
  type: "full",
});

// =============================================================================
// 2. DO Project (cost tracking + token scope isolation per D7)
// =============================================================================

// Plan v1.2 T3.1 — LoopRuntimeDOProject component (was inline new digitalocean.Project).
const drillDOProject = new LoopRuntimeDOProject("drill-project", {
  projectName: doProject,
  description: `LoopRuntime drill prod-identical stack — expires ${expiresAtIso} — destroyed after run`,
});

// =============================================================================
// 3. VPC (D5 — shared across 4 clusters, prod-identical pattern)
// =============================================================================

// Plan v1.2 T3.1 — LoopRuntimeVpc component (was inline new digitalocean.Vpc).
const drillVpc = new LoopRuntimeVpc("drill-vpc", {
  vpcName: `${doProject}-vpc-${region}`,
  region,
  ipRange: "10.40.0.0/16", // disjoint from prod 10.20.0.0/16 + homolog 10.30.0.0/16
  description: `LoopRuntime drill VPC (4 clusters share). Expires ${expiresAtIso}.`,
});

// =============================================================================
// 4. DOKS clusters (4 — mgmt + build + runtime + runtime-2) — D1
// =============================================================================
//
// Topology mirrors platformClusterPlans() in infra/pulumi/src/components/infra.ts.
// Cell mapping per D3:
//   cell-0 → runtimeCluster
//   cell-1 → runtimeCluster2

// Plan v1.2 T3.1 — LoopRuntimeDOKSCluster component (4 instances replace inline declarations).
const mgmtCluster = new LoopRuntimeDOKSCluster("drill-mgmt", {
  clusterName: `${doProject}-core-cluster`,
  region,
  version: mgmtK8sVersion,
  vpcUuid: drillVpc.id,
  nodePoolName: "drill-mgmt-pool",
  nodeSize: mgmtNodeSize,
  nodeCount: mgmtNodeCount,
  tags: drillTags,
});

const buildCluster = new LoopRuntimeDOKSCluster("drill-build", {
  clusterName: `${doProject}-build-cluster`,
  region,
  version: buildK8sVersion,
  vpcUuid: drillVpc.id,
  nodePoolName: "drill-build-pool",
  nodeSize: buildNodeSize,
  nodeCount: buildNodeCount,
  tags: drillTags,
});

const runtimeCluster = new LoopRuntimeDOKSCluster("drill-runtime", {
  clusterName: `${doProject}-runtime-cluster`,
  region,
  version: runtimeK8sVersion,
  vpcUuid: drillVpc.id,
  nodePoolName: "drill-runtime-pool",
  nodeSize: runtimeNodeSize,
  nodeCount: runtimeNodeCount,
  tags: drillTags,
});

const runtimeCluster2 = new LoopRuntimeDOKSCluster("drill-runtime-2", {
  clusterName: `${doProject}-cell-1-cluster`,
  region,
  version: runtimeK8sVersion,
  vpcUuid: drillVpc.id,
  nodePoolName: "drill-runtime-2-pool",
  nodeSize: runtimeNodeSize,
  nodeCount: runtimeNodeCount,
  tags: drillTags,
});

// =============================================================================
// 5. DO Spaces buckets (3 — mirroring prod use) — D8
// =============================================================================

// Plan v1.2 T3.1 — LoopRuntimeSpacesBucket component (replaces 4 inline SpacesBucket
// declarations). Each instance is 1 component call site. Pattern enables future
// extraction of LoopRuntimeVPC, LoopRuntimeMgmtCluster, LoopRuntimeManagedPG, etc. (incremental
// Pulumi componentize per ADR D1).
const drillBuildsBucket = new LoopRuntimeSpacesBucket("drill-builds", {
  bucketName: `loopruntime-drill-builds-${region}`,
  region,
  forceDestroy: true,
});

const drillArtifactsBucket = new LoopRuntimeSpacesBucket("drill-artifacts", {
  bucketName: `loopruntime-drill-artifacts-${region}`,
  region,
  forceDestroy: true,
});

const drillStaticBucket = new LoopRuntimeSpacesBucket("drill-static", {
  bucketName: `loopruntime-drill-static-${region}`,
  region,
  forceDestroy: true,
});

// Plan v1.2 T6.1 — Velero backup bucket (closes finding-2026-05-20 Gap 1).
// Velero BSL "default" was Unavailable in drill iter 2026-05-20 because
// the bucket `loopruntime-drill-velero-backups` (referenced by
// infra/helmfile/environments/drill.yaml velero.bucket) did not exist.
// Operator action: re-run `pulumi up --stack drill` to provision this
// bucket; after that, Velero BSL transitions to Available within ~60s
// and 5 Schedule CRs apply automatically (Gap 2 closes secondarily).
//
// lifecycleRule: 30-day expiration to keep storage cost bounded
// (drill is iteration env; backups older than 30d have no recovery value).
const drillVeleroBucket = new LoopRuntimeSpacesBucket("drill-velero", {
  bucketName: `loopruntime-drill-velero-backups`,
  region,
  forceDestroy: true,
  acl: "private",
  lifecycleRules: [
    {
      id: "expire-30d",
      enabled: true,
      expiration: { days: 30 },
    },
  ],
});

// =============================================================================
// 6. Managed PostgreSQL (mgmt-tier per D4)
// =============================================================================

// Plan v1.2 T3.1 — LoopRuntimeManagedDb component (was inline new digitalocean.DatabaseCluster).
const drillPG = new LoopRuntimeManagedDb("drill-pg", {
  clusterName: `${doProject}-pg`,
  engine: "pg",
  version: "16",
  size: "db-amd-2vcpu-4gb", // mgmt-tier per D4 (Bug #26 2026-05-19: AMD variant; sfo3 db-s-2vcpu-4gb capacity exhausted during marathon)
  region,
  nodeCount: 1,
  privateNetworkUuid: drillVpc.id,
  tags: drillTags,
});

// =============================================================================
// 6.1. Per-app DatabaseDb resources (Phase 2 plan T1.1 + D1 — mirror pulumi-infra)
// =============================================================================
//
// Managed PostgreSQL only creates "defaultdb". Platform services need their own
// databases. Mirrors `infra/pulumi-infra/src/providers/digitalocean/database.ts:36-56`.
//
// EC-7 (DOCUMENTED ACCEPTED): `pulumi destroy` deletes these DBs + data. Drill
// is ephemeral by design (D4 ADR + T4.8 mandatory teardown). Zero production
// data — only L0+L1+L2 harness test fixtures. Data loss acceptable.

const drillPgKratos = new digitalocean.DatabaseDb("drill-pg-kratos", {
  clusterId: drillPG.id,
  name: "kratos",
});

const drillPgHydra = new digitalocean.DatabaseDb("drill-pg-hydra", {
  clusterId: drillPG.id,
  name: "hydra",
});

const drillPgGitea = new digitalocean.DatabaseDb("drill-pg-gitea", {
  clusterId: drillPG.id,
  name: "gitea",
});

const drillPgLoopRuntime = new digitalocean.DatabaseDb("drill-pg-loopruntime", {
  clusterId: drillPG.id,
  name: "loopruntime",
});

// =============================================================================
// 6.2. Per-app DatabaseUser resources (Phase 2 plan T1.2 + D1 — mirror pulumi-infra)
// =============================================================================
//
// Each service gets its own user (no shared loopruntime:loopruntime-dev-password). Mirrors
// `infra/pulumi-infra/src/providers/digitalocean/database.ts:90-112`.
//
// EC-5 (SHOULD TEST): DO API accepts 4 paralel User creates without rate limit
// per prod precedence (pulumi-infra creates same 4 users + 4 DBs at scale).

const drillPgKratosUser = new digitalocean.DatabaseUser("drill-pg-kratos-user", {
  clusterId: drillPG.id,
  name: "kratos",
});

const drillPgHydraUser = new digitalocean.DatabaseUser("drill-pg-hydra-user", {
  clusterId: drillPG.id,
  name: "hydra",
});

const drillPgGiteaUser = new digitalocean.DatabaseUser("drill-pg-gitea-user", {
  clusterId: drillPG.id,
  name: "gitea",
});

const drillPgLoopRuntimeApiUser = new digitalocean.DatabaseUser("drill-pg-loopruntime-api-user", {
  clusterId: drillPG.id,
  name: "loopruntime_api",
});

// =============================================================================
// 6.3. DatabaseFirewall (Phase 2 Bug #11 LIVE fix — mirror pulumi/{prod}+pulumi-infra)
// =============================================================================
//
// Without this, DO managed PG defaults to "trusted sources only" and DOKS pods
// time out connecting to port 25060. Caught LIVE 2026-05-17 iter 1 on ory-kratos
// migration job: "Error: context deadline exceeded" after 900s.
//
// Mirrors `infra/pulumi/src/providers/digitalocean/database.ts:75` +
// `infra/pulumi-infra/src/providers/digitalocean/database.ts:62`.
//
// Rules: all 4 DOKS clusters need PG access (mgmt for loopruntime-api/kratos/hydra/gitea
// migrations, build for loopruntime-api API calls into workflow steps, runtime+runtime-2
// for cell-aware operator reconciles that read tenant state).

new digitalocean.DatabaseFirewall("drill-pg-firewall", {
  clusterId: drillPG.id,
  rules: [
    { type: "k8s", value: mgmtCluster.id },
    { type: "k8s", value: buildCluster.id },
    { type: "k8s", value: runtimeCluster.id },
    { type: "k8s", value: runtimeCluster2.id },
  ],
});

// =============================================================================
// 7. Managed Valkey/Redis (Bug #15 LIVE fix)
// =============================================================================
//
// Per ADR-0016 D-6 + CTO Golden Rule #0 ("ambiente local fidedigno ao prod"):
// drill MUST use DO Managed Valkey, NOT in-cluster bitnami/redis. In-cluster
// redis is a STANDALONE/local-k3d pattern. Production uses managed Valkey
// because:
//   - Internal Redis = SPOF (single-replica StatefulSet)
//   - Sessions/queues/cache lost on pod restart
//   - No automated backups
//   - Helm-managed Service naming drift (Bug #15: gitea-redis vs
//     gitea-redis-master Service name mismatch)
//
// Bug #15 LIVE caught 2026-05-17 iter 5: gitea pod CrashLoopBackOff on
// `cache.Init failed: dial tcp: lookup gitea-redis on 10.245.0.10:53: no
// such host`. Root cause: bitnami/redis chart creates Service
// `gitea-redis-master` but gitea config points to `gitea-redis`. Wrong fix
// would be addr=gitea-redis-master:6379 — right fix is replace in-cluster
// redis with managed Valkey (prod parity).
//
// Sizing: drill = non-prod tier (db-amd-1vcpu-2gb single-node) matching
// non-prod prod config. Same engine version (valkey 8) for parity.

// Plan v1.2 T3.1 — LoopRuntimeManagedDb component (Valkey variant).
const drillRedis = new LoopRuntimeManagedDb("drill-redis", {
  clusterName: `${doProject}-redis`,
  engine: "valkey",
  version: "8",
  size: "db-amd-1vcpu-2gb", // non-prod tier
  region,
  nodeCount: 1,
  privateNetworkUuid: drillVpc.id,
  tags: drillTags,
});

// Firewall: same 4 DOKS clusters as drill-pg (mgmt needs Redis for gitea
// cache/session/queue; build/runtime currently don't but
// kept symmetric for future hardening + zero added cost since rules are free).
new digitalocean.DatabaseFirewall("drill-redis-firewall", {
  clusterId: drillRedis.id,
  rules: [
    { type: "k8s", value: mgmtCluster.id },
    { type: "k8s", value: buildCluster.id },
    { type: "k8s", value: runtimeCluster.id },
    { type: "k8s", value: runtimeCluster2.id },
  ],
});

// =============================================================================
// Stack outputs (FLAT NAMED EXPORTS per L2-unlock plan D1)
// =============================================================================
//
// BREAKING CHANGE vs prior schemas (twice now):
//   v0 single-cluster: { cluster, clusterKubeconfig, bucket, pg, ... }
//   v1 cluster-map:    export const outputs = { mgmtCluster: {...}, ... }
//   v2 NAMED EXPORTS:  export const mgmtClusterId, mgmtClusterKubeconfig, ...
//
// v2 rationale (per L2-unlock plan T1.1 + T1.2 + RCA
// `finding-2026-05-16-pulumi-outputs-rca.md`):
//
//   - Each `export const X = Y` registers ONE output Y under name X. Pulumi
//     state stores per-named-export. No nested object serialization fragility.
//   - `pulumi stack output mgmtClusterKubeconfig` returns the raw kubeconfig
//     directly — no jq nesting traversal needed.
//   - Schema is GREP-AUDITABLE — each export is one line, easy to detect via
//     `check-drill-outputs-consumers.sh` patterns.
//   - Eliminates Hypothesis B (Output<T> resolution failure in object literal).
//   - T1.2.bis state reset belt-and-suspenders eliminates Hypothesis A.
//
// All consumers MUST update atomically (T1.3 grep audit gates merge).

// Per-cluster outputs (mgmt) — D1 named exports per cluster
export const mgmtClusterId = mgmtCluster.id;
export const mgmtClusterName = mgmtCluster.name;
export const mgmtClusterEndpoint = mgmtCluster.endpoint;
export const mgmtClusterKubeconfig = mgmtCluster.kubeConfigs[0].rawConfig;

// Per-cluster outputs (build)
export const buildClusterId = buildCluster.id;
export const buildClusterName = buildCluster.name;
export const buildClusterEndpoint = buildCluster.endpoint;
export const buildClusterKubeconfig = buildCluster.kubeConfigs[0].rawConfig;

// Per-cluster outputs (runtime — hosts cell-0 per D3)
export const runtimeClusterId = runtimeCluster.id;
export const runtimeClusterName = runtimeCluster.name;
export const runtimeClusterEndpoint = runtimeCluster.endpoint;
export const runtimeClusterKubeconfig = runtimeCluster.kubeConfigs[0].rawConfig;

// Per-cluster outputs (runtime-2 — hosts cell-1 per D3)
export const runtimeCluster2Id = runtimeCluster2.id;
export const runtimeCluster2Name = runtimeCluster2.name;
export const runtimeCluster2Endpoint = runtimeCluster2.endpoint;
export const runtimeCluster2Kubeconfig = runtimeCluster2.kubeConfigs[0].rawConfig;

// Shared infrastructure — VPC (D5)
export const vpcId = drillVpc.id;
export const vpcIpRange = "10.40.0.0/16";

// Shared infrastructure — Managed PG (D4 mgmt-tier)
export const pgUri = drillPG.uri;
export const pgHost = drillPG.host;
export const pgPort = drillPG.port;
export const pgDatabase = drillPG.database;

// Shared infrastructure — Managed Valkey/Redis (Bug #15 LIVE fix)
// Used by gitea (cache/session/queue) via drill-helmfile-bootstrap.sh
// exporting GITEA_REDIS_HOST/PORT/PASSWORD env vars.
// privateHost ensures DOKS pods reach Valkey via internal VPC (10.40.0.0/16),
// NOT public internet (would require explicit firewall allow per IP).
export const redisHost = drillRedis.privateHost;
export const redisPort = drillRedis.port;
export const redisPassword = drillRedis.password;
export const redisUri = drillRedis.uri; // full rediss://user:pass@host:port/0 — convenience

// Per-service DSN exports (Phase 2 plan T1.3 + D2 — pulumi.interpolate for Output<T>)
// CRITICAL: consumers MUST use `--show-secrets` flag when reading these (T1.3.bis EC-1).
// Without --show-secrets, password renders as "[secret]" literal → invalid DSN → service crashloop.
// sslmode=require matches T2.1.bis EC-02 fix from parent plan (managed PG enforces SSL).
//
// Bug #12 LIVE FIX (caught 2026-05-17 iter 2 on ory-kratos migration):
// DO Managed PG 16 restricts schema permissions on `public` — per-app users
// can connect but CANNOT `CREATE TABLE` (ERROR: permission denied for schema
// public, SQLSTATE 42501). Solution: use `doadmin` (cluster admin) for auth,
// keep per-app DB names for logical isolation. Mirrors prod precedent at
// `infra/pulumi-platform/src/index.ts:112-115` (Gitea uses doadmin for exact
// same reason: "DO managed PG15+ restricts schema permissions on the public
// schema — custom users can't CREATE tables without GRANT").
//
// Per-app DatabaseUser resources retained for future GRANT-based hardening
// (when drill becomes long-lived) but not used in DSN.
// T0.4 (plan 2026-05-21-deep-review-remediation): wrap DSN exports in
// pulumi.secret() so the embedded ${drillPG.password} is marked secret in
// stack state output. Without the wrap, `pulumi stack output kratosDsn`
// prints the password in plaintext + the password leaks to the state file
// as a non-secret value. Per ADR D3: any Pulumi output embedding an
// Output<string> of sensitive value MUST be pulumi.secret-wrapped.
export const kratosDsn = pulumi.secret(pulumi.interpolate`postgres://${drillPG.user}:${drillPG.password}@${drillPG.host}:${drillPG.port}/${drillPgKratos.name}?sslmode=require`);
export const hydraDsn = pulumi.secret(pulumi.interpolate`postgres://${drillPG.user}:${drillPG.password}@${drillPG.host}:${drillPG.port}/${drillPgHydra.name}?sslmode=require`);
export const giteaDsn = pulumi.secret(pulumi.interpolate`postgres://${drillPG.user}:${drillPG.password}@${drillPG.host}:${drillPG.port}/${drillPgGitea.name}?sslmode=require`);
export const loopruntimeApiDsn = pulumi.secret(pulumi.interpolate`postgres://${drillPG.user}:${drillPG.password}@${drillPG.host}:${drillPG.port}/${drillPgLoopRuntime.name}?sslmode=require`);

// Shared infrastructure — Spaces buckets (D8)
export const bucketBuilds = drillBuildsBucket.name;
export const bucketArtifacts = drillArtifactsBucket.name;
export const bucketStatic = drillStaticBucket.name;
// Plan v1.2 T6.1 — Velero backup bucket (Gap 1 closure).
export const bucketVeleroBackups = drillVeleroBucket.name;
export const bucketVeleroEndpoint = pulumi.interpolate`https://${region}.digitaloceanspaces.com`;

// Shared infrastructure — CF Zone (EC-03: NOT protected)
export const zoneId = drillZone.id;
export const zoneNameservers = drillZone.nameServers;

// DO Project (cost tracking + token scope per D7)
export const doProjectId = drillDOProject.id;

// Metadata + expiration (T4.1 watchdog uses these via dedicated outputs)
// Note: `meta` retained as object for backward-compat with drill-cost-report.sh
// --auto mode + drill-watchdog.sh tag scanning. Future: migrate to named
// `metaDomain`, `metaExpiresAtIso`, etc. as separate outputs.
export const meta = {
  domain,
  expiresInHours,
  expiresAtIso,
  purpose: "DR drill prod-identical — isolated from prod per MEET-ASYNC-AMENDMENT-2026-05-16-001",
  teardownPolicy: "MANDATORY — pulumi destroy + drill-teardown-validator.sh (T4.1.bis)",
  cellMapping: {
    "cell-0": "runtimeCluster",
    "cell-1": "runtimeCluster2",
  },
};

// Compatibility shims for consumers that haven't migrated yet (T1.3 grep
// audit will flag these for refactor; intentionally kept as separate exports
// so removal can happen in a follow-up PR without rebuilding all 16 named
// exports above).
//
// Removal target: after T1.3 grep audit shows zero legacy consumers in repo
// (operator-handoff doc + workflows + scripts all migrated).
//
// Deliberately commented out — DO NOT re-enable. Kept for documentation of
// what NOT to do (object-literal aggregation triggered the original Finding #3
// bug).
//
// export const outputs = {
//   mgmtCluster: { id: mgmtCluster.id, kubeconfig: ... },  // ← legacy v1 schema
//   ...
// };

