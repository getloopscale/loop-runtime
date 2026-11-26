// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";
import { platformClusterPlans } from "../components/infra";
import { PlatformStackConfig } from "../config/stack";

// The four-cluster topology used to be four hand-copied
// factory.createKubernetesCluster blocks plus a hand-written four-element
// trustedK8sClusters list. It is one declared table now. These specs pin what
// the table must produce: the cluster NAMES are the Pulumi resource names, so a
// change here renames — and replaces — a live cluster.
const config = {
  mgmtK8sVersion: "1.32.1",
  mgmtNodeClass: "medium",
  mgmtMinNodes: 3,
  mgmtMaxNodes: 5,
  runtimeK8sVersion: "1.32.2",
  runtimeNodeClass: "large",
  runtimeMinNodes: 2,
  runtimeMaxNodes: undefined,
  buildK8sVersion: "1.32.3",
  buildNodeClass: "small",
  buildMinNodes: 2,
  buildMaxNodes: 4,
} as unknown as PlatformStackConfig;

describe("platformClusterPlans", () => {
  // Order matters too: it is the order of the database firewall's trusted rules.
  it("names the four clusters exactly as the stack has always named them, in firewall-rule order", () => {
    const plans = platformClusterPlans(config, "loopruntime-dev-nyc3");
    expect(plans.map((plan) => [plan.output, plan.name])).toEqual([
      ["mgmtCluster", "loopruntime-dev-nyc3-mgmt"],
      ["buildCluster", "loopruntime-dev-nyc3-build"],
      ["runtimeCluster", "loopruntime-dev-nyc3-runtime"],
      ["runtimeCluster2", "loopruntime-dev-nyc3-runtime-2"],
    ]);
  });

  it("sizes the second runtime cell from the runtime settings", () => {
    const [, , runtime, runtime2] = platformClusterPlans(config, "b");
    expect(runtime2).toMatchObject({
      version: runtime.version,
      nodeClass: runtime.nodeClass,
      minNodes: runtime.minNodes,
      maxNodes: runtime.maxNodes,
    });
  });

  it("autoscales exactly the clusters that declare a maximum", () => {
    const auto = platformClusterPlans(config, "b").map((plan) => [plan.output, plan.autoScale]);
    expect(auto).toEqual([
      ["mgmtCluster", true],
      ["buildCluster", true],
      ["runtimeCluster", false],
      ["runtimeCluster2", false],
    ]);
  });
});
