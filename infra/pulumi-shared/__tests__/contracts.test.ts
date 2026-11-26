// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Contract tests for pulumi-shared/stack-outputs.ts.
//
// Plan 2026-05-15 T2.2 (audit M05 gap "2 stacks sem testes"): pulumi-shared
// is the SINGLE SOURCE OF TRUTH for cross-stack output types. Any drift
// here cascades to loopruntime-infra + loopruntime-platform mid-`pulumi up`. These tests
// lock the type surface as a compile-time + structure-time contract.
//
// What we test (and why):
//   - Required fields present (drop = breaking change)
//   - Type compatibility (Record<string, ...> vs Map distinction matters
//     because consumers iterate via Object.keys())
//   - Deprecated singleton outputs preserved (legacy CI scripts read them)
//   - Secret-marker comments preserved (operator audit relies on these)

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";
import type {
  ClusterRegistryEntry,
  ClusterRegistry,
  InfraStackOutputs,
  PlatformStackOutputs,
} from "../stack-outputs";

const SRC = path.resolve(__dirname, "../stack-outputs.ts");

describe("ClusterRegistryEntry", () => {
  it("compiles with all required fields", () => {
    const entry: ClusterRegistryEntry = {
      clusterId: "cluster-1",
      clusterName: "loopruntime-core",
      role: "mgmt",
      region: "sfo3",
      kubeconfig: "<secret>",
      endpoint: "https://1.2.3.4:6443",
      labels: { tier: "control" },
    };
    expect(entry.role).toBe("mgmt");
  });

  it("optional cellId may be omitted", () => {
    const entry: ClusterRegistryEntry = {
      clusterId: "cluster-2",
      clusterName: "loopruntime-build",
      role: "build",
      region: "sfo3",
      kubeconfig: "<secret>",
      endpoint: "https://1.2.3.5:6443",
      labels: {},
    };
    // cellId is optional — this object compiles without it
    expect(entry.clusterId).toBe("cluster-2");
  });

  it("role is restricted to mgmt | build | runtime (compile-time union)", () => {
    // The following @ts-expect-error pragma encodes the contract:
    // mistyping role catches at compile-time.
    // @ts-expect-error — invalid role must fail compilation
    const _bad: ClusterRegistryEntry["role"] = "controller";
    void _bad;
    // Valid roles
    const mgmt: ClusterRegistryEntry["role"] = "mgmt";
    const build: ClusterRegistryEntry["role"] = "build";
    const runtime: ClusterRegistryEntry["role"] = "runtime";
    expect([mgmt, build, runtime]).toHaveLength(3);
  });
});

describe("ClusterRegistry", () => {
  it("is a string-keyed map of ClusterRegistryEntry", () => {
    const registry: ClusterRegistry = {
      "cluster-1": {
        clusterId: "cluster-1",
        clusterName: "loopruntime-core",
        role: "mgmt",
        region: "sfo3",
        kubeconfig: "",
        endpoint: "",
        labels: {},
      },
    };
    expect(Object.keys(registry)).toHaveLength(1);
  });
});

describe("InfraStackOutputs — public schema", () => {
  it("compiles with all 21 required fields", () => {
    const outputs: InfraStackOutputs = {
      clusterRegistry: {},
      mgmtClusterName: "loopruntime-core",
      mgmtClusterEndpoint: "",
      mgmtKubeconfig: "",
      buildClusterName: "loopruntime-build",
      buildClusterEndpoint: "",
      buildKubeconfig: "",
      runtimeClusterName: "loopruntime-runtime",
      runtimeClusterEndpoint: "",
      runtimeKubeconfig: "",
      dbHost: "",
      dbPort: 5432,
      dbUri: "",
      dbPassword: "",
      bucketName: "",
      bucketEndpoint: "",
      buildsBucketName: "",
      logsBucketName: "",
      staticBucketName: "",
      staticBucketEndpoint: "",
      spacesEndpoint: "",
      s3Endpoint: "",
      registryEndpoint: "",
      registryName: "",
    };
    // Spot-check the multi-cluster primary contract
    expect(outputs.clusterRegistry).toEqual({});
    // Spot-check legacy singleton outputs (deprecated but preserved)
    expect(outputs.mgmtClusterName).toBe("loopruntime-core");
  });
});

describe("PlatformStackOutputs — public schema", () => {
  it("compiles with all required fields + optional CF fields", () => {
    const outputs: PlatformStackOutputs = {
      mgmtIngressEndpoint: "",
      runtimeIngressEndpoint: "",
      lokiEndpoint: "",
      argoMetricsEndpoint: "",
      giteaReady: true,
      argoCdReady: true,
      prometheusReady: true,
      grafanaReady: true,
      mgmtDnsConfigured: true,
      runtimeDnsConfigured: true,
      // CF optionals omitted — they are properly optional
    };
    expect(outputs.giteaReady).toBe(true);
    // CF optionals can be set explicitly
    const withCF: PlatformStackOutputs = {
      ...outputs,
      cfTunnelId: "tunnel-1",
      cfKvNamespaceId: "kv-1",
      cfWorkerName: "loopruntime-static",
    };
    expect(withCF.cfTunnelId).toBe("tunnel-1");
  });
});

describe("stack-outputs.ts — source invariants (cross-stack contract)", () => {
  const src = fs.readFileSync(SRC, "utf-8");

  it("documents secret fields inline (// secret or // pulumi.secret)", () => {
    // The audit trail relies on these comments — auditors visually scan
    // for /secret/ to enumerate sensitive fields. Removing them silently
    // weakens the audit.
    const secretLines = src.split("\n").filter((l) => /\/\/.*\bsecret\b/i.test(l));
    // 5 secret fields per current schema: kubeconfig + 3 cluster kcs + dbUri/Password
    expect(secretLines.length).toBeGreaterThanOrEqual(5);
  });

  it("declares the file as SINGLE SOURCE OF TRUTH (header comment lock)", () => {
    expect(src).toContain("SINGLE SOURCE OF TRUTH");
    expect(src).toContain("All projects import from here");
  });

  it("declares migration policy for field removals", () => {
    expect(src).toContain("Field additions are backwards-compatible");
    expect(src).toContain("coordinated migration");
  });
});
