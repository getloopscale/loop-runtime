// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Schema + structure tests for loopzero-drill (PROD-IDENTICAL 4-CLUSTER TOPOLOGY).
//
// Plan 2026-05-16 T1.1 + T1.1.bis + T1.2 + T1.3 + edge cases EC-01..EC-10.
// CTO directive 2026-05-16: "drill IGUAL prod 100% fiel".
//
// These tests lock the new public surface so:
//   - refactors that drop the 4-cluster structure fail loud (D1)
//   - sizing drift from prod tier specs is caught (D2)
//   - VPC CIDR overlap with prod/homolog is caught (D5 + EC-06)
//   - PG tier regression to mini is caught (D4)
//   - CF Zone protect=true regression is caught (EC-03)
//   - outputs schema break is caught (EC-02 — also enforced via grep audit)
//   - expires-at safety ceiling is enforced (EC-01)

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";
import * as yaml from "js-yaml";

const SRC = path.resolve(__dirname, "..");
const ROOT = path.resolve(__dirname, "../..");
const INDEX = path.join(SRC, "index.ts");

describe("loopzero-drill — Pulumi.yaml", () => {
  const pulumiYaml = yaml.load(
    fs.readFileSync(path.join(ROOT, "Pulumi.yaml"), "utf-8"),
  ) as Record<string, unknown>;

  it("declares stack name + main entry", () => {
    expect(pulumiYaml.name).toBe("loopzero-drill");
    expect(pulumiYaml.runtime).toBeDefined();
  });
});

describe("loopzero-drill — config schema (4-cluster topology)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("scoped under namespace `loopzero-drill` (NOT loopzero-platform/loopzero-infra)", () => {
    expect(src).toContain('new pulumi.Config("loopzero-drill")');
  });

  it("requires stack-wide config keys (D1 — 4-cluster mirroring prod)", () => {
    const required = [
      'config.require("domain")',
      'config.require("doProject")',
      'config.require("region")',
      'config.require("mgmtK8sVersion")',
    ];
    for (const r of required) {
      expect(src).toContain(r);
    }
  });

  it("declares per-cluster sizing config (D2 — prod tier specs)", () => {
    // mgmt cluster sizing
    expect(src).toMatch(/mgmtNodeSize.*s-4vcpu-8gb-amd/);
    expect(src).toMatch(/mgmtNodeCount.*3/);
    // build + runtime small tier
    expect(src).toMatch(/buildNodeSize.*s-2vcpu-4gb/);
    expect(src).toMatch(/runtimeNodeSize.*s-2vcpu-4gb/);
  });

  it("CF account ID is a secret (never plaintext)", () => {
    expect(src).toContain('config.requireSecret("cfAccountId")');
  });

  it("expiresInHours has default + safety ceiling (T3.4.bis EC-01)", () => {
    expect(src).toMatch(/expiresInHours.*\?\?\s*8/); // default 8h
    expect(src).toMatch(/expiresInHours\s*>\s*48/); // 48h ceiling check
    expect(src).toMatch(/expiresInHours\s*<\s*1/); // 1h minimum check
  });
});

describe("loopzero-drill — 4-cluster topology (D1)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("provisions 4 KubernetesCluster resources (mgmt + build + runtime + runtime-2)", () => {
    const matches = src.match(/new digitalocean\.KubernetesCluster\(/g) ?? [];
    expect(matches.length).toBe(4);
  });

  it("each cluster declared with prod-identical Pulumi resource names", () => {
    expect(src).toContain('new digitalocean.KubernetesCluster("drill-mgmt"');
    expect(src).toContain('new digitalocean.KubernetesCluster("drill-build"');
    expect(src).toContain('new digitalocean.KubernetesCluster("drill-runtime"');
    expect(src).toContain('new digitalocean.KubernetesCluster("drill-runtime-2"');
  });

  it("mgmt cluster uses medium tier per D2", () => {
    // mgmtNodeSize default in code is s-4vcpu-8gb-amd
    expect(src).toMatch(/mgmtNodeSize\s*=\s*config\.get.*\?\?\s*"s-4vcpu-8gb-amd"/);
  });

  it("build + runtime clusters use small tier per D2", () => {
    expect(src).toMatch(/buildNodeSize\s*=\s*config\.get.*\?\?\s*"s-2vcpu-4gb"/);
    expect(src).toMatch(/runtimeNodeSize\s*=\s*config\.get.*\?\?\s*"s-2vcpu-4gb"/);
  });

  it("all 4 clusters reference shared VPC (D5)", () => {
    // Count vpcUuid references = should be 4 (one per cluster) + 1 (PG)
    const matches = src.match(/vpcUuid:\s*drillVpc\.id|privateNetworkUuid:\s*drillVpc\.id/g) ?? [];
    expect(matches.length).toBeGreaterThanOrEqual(5);
  });

  it("autoUpgrade: false (mirror prod posture)", () => {
    const matches = src.match(/autoUpgrade:\s*false/g) ?? [];
    expect(matches.length).toBe(4); // one per cluster
  });
});

describe("loopzero-drill — VPC (D5 + EC-06)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("provisions a single shared VPC", () => {
    const matches = src.match(/new digitalocean\.Vpc\(/g) ?? [];
    expect(matches.length).toBe(1);
  });

  it("VPC CIDR is 10.40.0.0/16 (disjoint from prod 10.20 + homolog 10.30)", () => {
    expect(src).toContain('ipRange: "10.40.0.0/16"');
    // Negative assertions: no prod/homolog CIDR
    expect(src).not.toMatch(/ipRange:\s*"10\.20\./);
    expect(src).not.toMatch(/ipRange:\s*"10\.30\./);
  });

  it("VPC name includes region for clarity", () => {
    expect(src).toMatch(/name:\s*`\$\{doProject\}-vpc-\$\{region\}`/);
  });
});

describe("loopzero-drill — Managed PG mgmt-tier (D4)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("PG uses mgmt-tier (db-s-2vcpu-4gb) — regression from mini tier", () => {
    expect(src).toContain('size: "db-s-2vcpu-4gb"');
    // Negative: no legacy mini tier
    expect(src).not.toContain('"db-s-1vcpu-1gb"');
  });

  it("PG attached to drill VPC (private networking)", () => {
    expect(src).toMatch(/privateNetworkUuid:\s*drillVpc\.id/);
  });

  it("PG engine + version matches prod", () => {
    expect(src).toContain('engine: "pg"');
    expect(src).toContain('version: "16"');
  });
});

describe("loopzero-drill — Spaces buckets (D8)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("provisions 3 Spaces buckets (builds + artifacts + static)", () => {
    const matches = src.match(/new digitalocean\.SpacesBucket\(/g) ?? [];
    expect(matches.length).toBe(3);
  });

  it("all Spaces buckets have forceDestroy=true (mandatory teardown)", () => {
    const matches = src.match(/forceDestroy:\s*true/g) ?? [];
    expect(matches.length).toBe(3);
  });

  it("bucket names include region suffix", () => {
    expect(src).toMatch(/loopzero-drill-builds-\$\{region\}/);
    expect(src).toMatch(/loopzero-drill-artifacts-\$\{region\}/);
    expect(src).toMatch(/loopzero-drill-static-\$\{region\}/);
  });
});

describe("loopzero-drill — CF Zone (EC-03 fix)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("CF Zone is NOT protected (EC-03: protect:true blocked teardown)", () => {
    // The Zone resource block should NOT include `protect: true`.
    // Match the Zone block specifically (not other resources).
    const zoneBlock = src.match(/new cloudflare\.Zone\([^;]+?\);/s);
    expect(zoneBlock).toBeTruthy();
    if (zoneBlock) {
      expect(zoneBlock[0]).not.toMatch(/protect:\s*true/);
    }
  });

  it("CF Zone type is full (apex zone, not subdomain)", () => {
    expect(src).toContain('type: "full"');
  });

  it("CF Zone uses configured drill domain (not prod)", () => {
    expect(src).toMatch(/zone:\s*domain/);
    // domain comes from config and Pulumi.drill.yaml asserts loopzero-drill.example
  });
});

describe("loopzero-drill — Tags + expires-at (T4.1 watchdog support)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("computes expires-at ISO timestamp", () => {
    expect(src).toContain("expiresAtIso");
    expect(src).toMatch(/new Date\(Date\.now\(\) \+ expiresInHours \* 3600 \* 1000\)/);
  });

  it("tags resources with loopzero-drill base + expires-at variant", () => {
    expect(src).toMatch(/"loopzero-drill"/);
    expect(src).toMatch(/loopzero-drill-expires-at-/);
  });

  it("tag values sanitized for DO tag rules (lowercase alphanumeric + dashes)", () => {
    expect(src).toMatch(/toLowerCase/);
    expect(src).toMatch(/replace\(\/\[\^a-z0-9-\]\/gi/);
  });
});

describe("loopzero-drill — Outputs schema (EC-02 BREAKING CHANGE)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("exports a single named `outputs` object", () => {
    expect(src).toMatch(/^export const outputs\s*=/m);
  });

  it("outputs has cluster map keys (NEW schema, post T1.1)", () => {
    const newKeys = [
      "mgmtCluster",
      "buildCluster",
      "runtimeCluster",
      "runtimeCluster2",
      "vpc",
      "pg",
      "buckets",
      "zone",
      "doProject",
      "meta",
    ];
    for (const k of newKeys) {
      expect(src).toMatch(new RegExp(`\\b${k}:`));
    }
  });

  it("outputs does NOT contain legacy singular `cluster` key (EC-02)", () => {
    // Negative test — old singular keys would conflict with new schema
    const outputsBlock = src.match(/export const outputs\s*=\s*\{([\s\S]*?\n\};)/);
    expect(outputsBlock).toBeTruthy();
    if (outputsBlock) {
      const body = outputsBlock[1];
      // Should not have top-level `cluster:` or `clusterKubeconfig:`
      expect(body).not.toMatch(/^\s\scluster:/m);
      expect(body).not.toMatch(/^\s\sclusterKubeconfig:/m);
      expect(body).not.toMatch(/^\s\sbucket:/m); // legacy singular bucket
    }
  });

  it("each cluster output exposes id + name + endpoint + kubeconfig", () => {
    for (const cluster of ["mgmtCluster", "buildCluster", "runtimeCluster", "runtimeCluster2"]) {
      const block = new RegExp(
        `${cluster}:\\s*\\{[^}]*id:[^}]*name:[^}]*endpoint:[^}]*kubeconfig:`,
        "s",
      );
      expect(src).toMatch(block);
    }
  });

  it("meta block declares drill purpose + cell mapping + teardown policy", () => {
    expect(src).toContain('purpose: "DR drill prod-identical');
    expect(src).toMatch(/teardownPolicy:/);
    expect(src).toMatch(/cellMapping:/);
    expect(src).toMatch(/"cell-default":\s*"runtimeCluster"/);
    expect(src).toMatch(/"cell-1":\s*"runtimeCluster"/);
    expect(src).toMatch(/"cell-2":\s*"runtimeCluster2"/);
  });
});

describe("loopzero-drill — Isolation invariants (D7 + MEET-ASYNC-AMENDMENT-2026-05-16-001)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("does NOT import from pulumi-shared (isolation: no cross-project type leak)", () => {
    expect(src).not.toContain("pulumi-shared");
    expect(src).not.toContain("PlatformStackOutputs");
    expect(src).not.toContain("InfraStackOutputs");
  });

  it("references MEET-ASYNC-AMENDMENT-2026-05-16-001 in code comments (governance trail)", () => {
    expect(src).toContain("MEET-ASYNC-AMENDMENT-2026-05-16-001");
  });

  it("documents EC-03 fix in code (CF Zone protect:false rationale)", () => {
    expect(src).toContain("EC-03");
  });
});

describe("loopzero-drill — Pulumi.drill.yaml config (T1.1)", () => {
  const drillYamlPath = path.join(ROOT, "Pulumi.drill.yaml");
  const drillYaml = yaml.load(fs.readFileSync(drillYamlPath, "utf-8")) as Record<string, unknown>;
  const conf = drillYaml.config as Record<string, unknown>;

  it("declares all 4-cluster sizing config keys", () => {
    expect(conf["loopzero-drill:mgmtK8sVersion"]).toBeDefined();
    expect(conf["loopzero-drill:mgmtNodeSize"]).toBeDefined();
    expect(conf["loopzero-drill:mgmtNodeCount"]).toBeDefined();
    expect(conf["loopzero-drill:buildNodeSize"]).toBeDefined();
    expect(conf["loopzero-drill:buildNodeCount"]).toBeDefined();
    expect(conf["loopzero-drill:runtimeNodeSize"]).toBeDefined();
    expect(conf["loopzero-drill:runtimeNodeCount"]).toBeDefined();
  });

  it("declares expiresInHours config key", () => {
    expect(conf["loopzero-drill:expiresInHours"]).toBeDefined();
  });

  it("defaults mgmt to medium tier (s-4vcpu-8gb-amd)", () => {
    expect(conf["loopzero-drill:mgmtNodeSize"]).toBe("s-4vcpu-8gb-amd");
  });

  it("defaults runtime to small tier (s-2vcpu-4gb)", () => {
    expect(conf["loopzero-drill:runtimeNodeSize"]).toBe("s-2vcpu-4gb");
  });

  it("CF account ID is encrypted secret", () => {
    const cfId = conf["loopzero-drill:cfAccountId"] as Record<string, unknown>;
    expect(cfId).toBeDefined();
    expect(cfId.secure).toBeDefined();
  });
});
