// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// =============================================================================
// LOOPZERO DR DRILL — Pulumi Project (separate from production)
// =============================================================================
//
// Per MEET-2026-05-12 D9 + EC-NEW-5: this project provisions a quarterly
// disaster recovery validation cluster in isolation from production.
//
// Isolation guarantees (all mandatory per SRE veto):
//   1. Separate CF Zone (loopzero-drill.example) — NOT a subdomain of loopzero.example
//   2. Separate DO project (loopzero-drill) — its own API token scope
//   3. Separate Pulumi state backend (s3://loopzero-drill-pulumi-state)
//   4. Separate Cloudflare API token scoped ONLY to loopzero-drill.example
//
// Resources provisioned:
//   - 1 DOKS cluster (loopzero-drill-cluster)
//   - 1 DO Spaces bucket (loopzero-drill-builds)
//   - 1 managed PG16 instance (small)
//   - 1 CF Zone (loopzero-drill.example) with NS records
//
// Lifecycle: provisioned by `dr-drill.sh` orchestration, destroyed at end of
// drill. The "drill destroys drill" pattern validates DR recovery without
// risking prod.

import * as pulumi from "@pulumi/pulumi";
import * as digitalocean from "@pulumi/digitalocean";
import * as cloudflare from "@pulumi/cloudflare";

const config = new pulumi.Config("loopzero-drill");
const domain = config.require("domain");
const doProject = config.require("doProject");
const region = config.require("region");
const cellName = config.require("cellName");
const nodeCount = config.requireNumber("nodeCount");
const k8sVersion = config.require("k8sVersion");

// =============================================================================
// 1. CF Zone (separate from production loopzero.example)
// =============================================================================
// REQUIRES: CF API token scoped ONLY to this zone (operator pre-provisions).
// The zone was registered + delegated by sponsor; Pulumi adopts it.

const drillZone = new cloudflare.Zone(
  "drill-zone",
  {
    // @pulumi/cloudflare v5 renamed `name` -> `zone`. Finding 2026-05-12 iter 13.
    zone: domain,
    accountId: config.requireSecret("cfAccountId"),
    type: "full",
  },
  { protect: true } // Prevent accidental zone deletion mid-drill
);

// =============================================================================
// 2. DO Project (separate scope for cost tracking + token isolation)
// =============================================================================

const drillDOProject = new digitalocean.Project(
  "drill-project",
  {
    name: doProject,
    description: "LoopZero DR drill resources — destroyed quarterly",
    purpose: "Service or API",
    environment: "Staging",
    resources: [],
  }
);

// =============================================================================
// 3. DOKS cluster
// =============================================================================

const drillCluster = new digitalocean.KubernetesCluster(
  "drill-cluster",
  {
    name: `${doProject}-cluster`,
    region: region as digitalocean.Region,
    version: k8sVersion,
    autoUpgrade: false, // Manual upgrade only — drill should mirror prod posture
    nodePool: {
      name: "drill-pool",
      size: "s-2vcpu-4gb",
      nodeCount,
    },
  }
);

// =============================================================================
// 4. DO Spaces bucket (build artifacts during drill)
// =============================================================================

const drillBuildsBucket = new digitalocean.SpacesBucket(
  "drill-builds",
  {
    name: `loopzero-drill-builds-${region}`,
    region: region as digitalocean.Region,
    forceDestroy: true, // Allow teardown during destroy
  }
);

// =============================================================================
// 5. Managed PG16 (small instance for drill)
// =============================================================================

const drillPG = new digitalocean.DatabaseCluster(
  "drill-pg",
  {
    name: `${doProject}-pg`,
    engine: "pg",
    version: "16",
    size: "db-s-1vcpu-1gb",
    region: region as digitalocean.Region,
    nodeCount: 1,
  }
);

// =============================================================================
// Stack outputs (consumed by dr-drill.sh)
// =============================================================================

export const outputs = {
  zone: drillZone.id,
  zoneNameservers: drillZone.nameServers,
  cluster: drillCluster.endpoint,
  clusterKubeconfig: drillCluster.kubeConfigs[0].rawConfig,
  bucket: drillBuildsBucket.name,
  pg: drillPG.uri,
  doProject: drillDOProject.id,
  meta: {
    domain,
    cellName,
    purpose: "DR drill — isolated from prod per MEET-2026-05-12 D9",
    teardownAfter: "drill completion (no persistent workloads)",
  },
};
