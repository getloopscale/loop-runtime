// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// =============================================================================
// LOOPZERO DR DRILL — Pulumi Project (PROD-IDENTICAL 4-CLUSTER TOPOLOGY)
// =============================================================================
//
// Plan: 2026-05-16-drill-prod-identical-multicluster-plan.md (v1.1)
// MEET-ASYNC-AMENDMENT-2026-05-16-001 (pending ratification)
//
// Per CTO directive 2026-05-16: "nosso sistema NUNCA vai trabalhar com single
// cell, remova essa posibilidade, e vamos alterar o drill-test para ser
// IGUAL prod e ele deve fazer os teste reais 100% fiel a produção."
//
// This stack mirrors `infra/pulumi/src/components/infra.ts:139-186` 4-cluster
// topology (mgmt + build + runtime + runtime-2), enabling:
//   - Multi-cell routing real (cell-default+cell-1 → runtime, cell-2 → runtime-2)
//   - Build cluster isolation real
//   - Cross-cluster ArgoCD sync exercised
//   - Stage 2 cell2-soaks 24h soak feasible (via expiresInHours param)
//   - DR drill against multi-cluster topology
//
// ADRs (per plan v1.1):
//   D1: 4-cluster topology mirror prod
//   D2: Sizing prod tier specs (mgmt=medium s-4vcpu-8gb-amd, runtime/build=small)
//   D3: Cell mapping cell-default+cell-1 → runtimeCluster, cell-2 → runtimeCluster2
//   D4: Managed PG mgmt-tier (db-s-2vcpu-4gb) ephemeral per-run
//   D5: Shared VPC loopzero-drill-vpc-sfo3 (10.40.0.0/16) — disjoint from prod 10.20/homolog 10.30
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

const config = new pulumi.Config("loopzero-drill");

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
    `loopzero-drill:expiresInHours=${expiresInHours} exceeds 48h safety ceiling. ` +
      `Drill is ephemeral; if you need longer, use prod canary stage instead.`,
  );
}
if (expiresInHours < 1) {
  throw new Error(
    `loopzero-drill:expiresInHours=${expiresInHours} below 1h minimum. ` +
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
  "loopzero-drill",
  `loopzero-drill-expires-at-${expiresAtTagSafe}`,
];

// =============================================================================
// 1. CF Zone (separate from production loopzero.example)
// =============================================================================
// EC-03 fix: NO protect flag on this resource. Previously the protect option
// blocked `pulumi destroy --stack drill` (validated 2026-05-16 — commit
// 71d6c460 left zone orphan). Drill is ephemeral by design; isolation
// guarantees come from D7 (separate account + scoped token).

const drillZone = new cloudflare.Zone(
  "drill-zone",
  {
    zone: domain,
    accountId: config.requireSecret("cfAccountId"),
    type: "full",
  },
  // Intentionally no resource options — drill zone is ephemeral.
);

// =============================================================================
// 2. DO Project (cost tracking + token scope isolation per D7)
// =============================================================================

const drillDOProject = new digitalocean.Project("drill-project", {
  name: doProject,
  description: `LoopZero drill prod-identical stack — expires ${expiresAtIso} — destroyed after run`,
  purpose: "Service or API",
  environment: "Staging",
  resources: [],
});

// =============================================================================
// 3. VPC (D5 — shared across 4 clusters, prod-identical pattern)
// =============================================================================

const drillVpc = new digitalocean.Vpc("drill-vpc", {
  name: `${doProject}-vpc-${region}`,
  region,
  ipRange: "10.40.0.0/16", // disjoint from prod 10.20.0.0/16 + homolog 10.30.0.0/16
  description: `LoopZero drill VPC (4 clusters share). Expires ${expiresAtIso}.`,
});

// =============================================================================
// 4. DOKS clusters (4 — mgmt + build + runtime + runtime-2) — D1
// =============================================================================
//
// Topology mirrors prod components/infra.ts:139-186.
// Cell mapping per D3:
//   cell-default + cell-1 → runtimeCluster
//   cell-2              → runtimeCluster2

const mgmtCluster = new digitalocean.KubernetesCluster("drill-mgmt", {
  name: `${doProject}-mgmt-cluster`,
  region,
  version: mgmtK8sVersion,
  vpcUuid: drillVpc.id,
  autoUpgrade: false, // mirror prod posture
  tags: drillTags,
  nodePool: {
    name: "drill-mgmt-pool",
    size: mgmtNodeSize,
    nodeCount: mgmtNodeCount,
    tags: drillTags,
  },
});

const buildCluster = new digitalocean.KubernetesCluster("drill-build", {
  name: `${doProject}-build-cluster`,
  region,
  version: buildK8sVersion,
  vpcUuid: drillVpc.id,
  autoUpgrade: false,
  tags: drillTags,
  nodePool: {
    name: "drill-build-pool",
    size: buildNodeSize,
    nodeCount: buildNodeCount,
    tags: drillTags,
  },
});

const runtimeCluster = new digitalocean.KubernetesCluster("drill-runtime", {
  name: `${doProject}-runtime-cluster`,
  region,
  version: runtimeK8sVersion,
  vpcUuid: drillVpc.id,
  autoUpgrade: false,
  tags: drillTags,
  nodePool: {
    name: "drill-runtime-pool",
    size: runtimeNodeSize,
    nodeCount: runtimeNodeCount,
    tags: drillTags,
  },
});

const runtimeCluster2 = new digitalocean.KubernetesCluster("drill-runtime-2", {
  name: `${doProject}-runtime-2-cluster`,
  region,
  version: runtimeK8sVersion,
  vpcUuid: drillVpc.id,
  autoUpgrade: false,
  tags: drillTags,
  nodePool: {
    name: "drill-runtime-2-pool",
    size: runtimeNodeSize,
    nodeCount: runtimeNodeCount,
    tags: drillTags,
  },
});

// =============================================================================
// 5. DO Spaces buckets (3 — mirroring prod use) — D8
// =============================================================================

const drillBuildsBucket = new digitalocean.SpacesBucket("drill-builds", {
  name: `loopzero-drill-builds-${region}`,
  region,
  forceDestroy: true,
});

const drillArtifactsBucket = new digitalocean.SpacesBucket("drill-artifacts", {
  name: `loopzero-drill-artifacts-${region}`,
  region,
  forceDestroy: true,
});

const drillStaticBucket = new digitalocean.SpacesBucket("drill-static", {
  name: `loopzero-drill-static-${region}`,
  region,
  forceDestroy: true,
});

// =============================================================================
// 6. Managed PostgreSQL (mgmt-tier per D4)
// =============================================================================

const drillPG = new digitalocean.DatabaseCluster("drill-pg", {
  name: `${doProject}-pg`,
  engine: "pg",
  version: "16",
  size: "db-s-2vcpu-4gb", // mgmt-tier per D4 (was db-s-1vcpu-1gb single-cluster legacy)
  region,
  nodeCount: 1,
  privateNetworkUuid: drillVpc.id,
  tags: drillTags,
});

// =============================================================================
// Stack outputs (new schema — cluster map per T1.1.bis EC-02 fix)
// =============================================================================
//
// BREAKING CHANGE vs single-cluster legacy schema:
//   OLD: { cluster, clusterKubeconfig, bucket, pg, ... }
//   NEW: { mgmtCluster: {...}, buildCluster: {...}, runtimeCluster: {...},
//          runtimeCluster2: {...}, buckets: {...}, pg, zone, vpc, meta }
//
// All consumers MUST update atomically (T1.1.bis grep audit gates merge).

export const outputs = {
  // Per-cluster details (consumers use these)
  mgmtCluster: {
    id: mgmtCluster.id,
    name: mgmtCluster.name,
    endpoint: mgmtCluster.endpoint,
    kubeconfig: mgmtCluster.kubeConfigs[0].rawConfig,
  },
  buildCluster: {
    id: buildCluster.id,
    name: buildCluster.name,
    endpoint: buildCluster.endpoint,
    kubeconfig: buildCluster.kubeConfigs[0].rawConfig,
  },
  runtimeCluster: {
    id: runtimeCluster.id,
    name: runtimeCluster.name,
    endpoint: runtimeCluster.endpoint,
    kubeconfig: runtimeCluster.kubeConfigs[0].rawConfig,
  },
  runtimeCluster2: {
    id: runtimeCluster2.id,
    name: runtimeCluster2.name,
    endpoint: runtimeCluster2.endpoint,
    kubeconfig: runtimeCluster2.kubeConfigs[0].rawConfig,
  },

  // Shared infrastructure
  vpc: {
    id: drillVpc.id,
    ipRange: "10.40.0.0/16",
  },
  pg: {
    uri: drillPG.uri,
    host: drillPG.host,
    port: drillPG.port,
    database: drillPG.database,
  },
  buckets: {
    builds: drillBuildsBucket.name,
    artifacts: drillArtifactsBucket.name,
    static: drillStaticBucket.name,
  },
  zone: {
    id: drillZone.id,
    nameservers: drillZone.nameServers,
  },
  doProject: drillDOProject.id,

  // Metadata + expiration (T4.1 watchdog uses these)
  meta: {
    domain,
    expiresInHours,
    expiresAtIso,
    purpose: "DR drill prod-identical — isolated from prod per MEET-ASYNC-AMENDMENT-2026-05-16-001",
    teardownPolicy: "MANDATORY — pulumi destroy + drill-teardown-validator.sh (T4.1.bis)",
    cellMapping: {
      "cell-default": "runtimeCluster",
      "cell-1": "runtimeCluster",
      "cell-2": "runtimeCluster2",
    },
  },
};
