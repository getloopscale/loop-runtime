// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Schema + structure tests for loopruntime-drill (PROD-IDENTICAL 4-CLUSTER TOPOLOGY).
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

describe("loopruntime-drill — Pulumi.yaml", () => {
  const pulumiYaml = yaml.load(
    fs.readFileSync(path.join(ROOT, "Pulumi.yaml"), "utf-8"),
  ) as Record<string, unknown>;

  it("declares stack name + main entry", () => {
    expect(pulumiYaml.name).toBe("loopruntime-drill");
    expect(pulumiYaml.runtime).toBeDefined();
  });
});

describe("loopruntime-drill — config schema (4-cluster topology)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("scoped under namespace `loopruntime-drill` (NOT loopruntime-platform/loopruntime-infra)", () => {
    expect(src).toContain('new pulumi.Config("loopruntime-drill")');
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

describe("loopruntime-drill — 4-cluster topology (D1)", () => {
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

describe("loopruntime-drill — VPC (D5 + EC-06)", () => {
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

describe("loopruntime-drill — Managed PG mgmt-tier (D4)", () => {
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

describe("loopruntime-drill — Managed Valkey/Redis (Bug #15 LIVE fix)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("provisions DatabaseCluster drill-redis with valkey 8", () => {
    expect(src).toMatch(/new digitalocean\.DatabaseCluster\("drill-redis"/);
    expect(src).toMatch(/drill-redis[\s\S]+?engine:\s*"valkey"/);
    expect(src).toMatch(/drill-redis[\s\S]+?version:\s*"8"/);
  });

  it("Valkey uses non-prod tier (db-amd-1vcpu-2gb single-node)", () => {
    expect(src).toMatch(/drill-redis[\s\S]+?size:\s*"db-amd-1vcpu-2gb"/);
    expect(src).toMatch(/drill-redis[\s\S]+?nodeCount:\s*1/);
  });

  it("Valkey attached to drill VPC (private networking)", () => {
    expect(src).toMatch(/drill-redis[\s\S]+?privateNetworkUuid:\s*drillVpc\.id/);
  });

  it("Valkey firewall allows all 4 DOKS clusters", () => {
    const fwBlock = src.match(/DatabaseFirewall\("drill-redis-firewall"[\s\S]+?\}\);/)?.[0] ?? "";
    expect(fwBlock).toMatch(/clusterId:\s*drillRedis\.id/);
    expect(fwBlock).toMatch(/type:\s*"k8s",\s*value:\s*mgmtCluster\.id/);
    expect(fwBlock).toMatch(/type:\s*"k8s",\s*value:\s*buildCluster\.id/);
    expect(fwBlock).toMatch(/type:\s*"k8s",\s*value:\s*runtimeCluster\.id/);
    expect(fwBlock).toMatch(/type:\s*"k8s",\s*value:\s*runtimeCluster2\.id/);
  });

  it("exports redisHost (privateHost), redisPort, redisPassword, redisUri", () => {
    expect(src).toMatch(/^export const redisHost\s*=\s*drillRedis\.privateHost/m);
    expect(src).toMatch(/^export const redisPort\s*=\s*drillRedis\.port/m);
    expect(src).toMatch(/^export const redisPassword\s*=\s*drillRedis\.password/m);
    expect(src).toMatch(/^export const redisUri\s*=\s*drillRedis\.uri/m);
  });

  it("documents Bug #15 root cause + ADR-0016 prod parity in comment", () => {
    expect(src).toMatch(/Bug #15 LIVE/);
    expect(src).toMatch(/ADR-0016/);
  });
});

describe("loopruntime-drill — Spaces buckets (D8)", () => {
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
    expect(src).toMatch(/loopruntime-drill-builds-\$\{region\}/);
    expect(src).toMatch(/loopruntime-drill-artifacts-\$\{region\}/);
    expect(src).toMatch(/loopruntime-drill-static-\$\{region\}/);
  });
});

describe("loopruntime-drill — CF Zone (EC-03 fix)", () => {
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
    // domain comes from config and Pulumi.drill.yaml asserts drill.getloopscale.com
  });
});

describe("loopruntime-drill — Tags + expires-at (T4.1 watchdog support)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("computes expires-at ISO timestamp", () => {
    expect(src).toContain("expiresAtIso");
    expect(src).toMatch(/new Date\(Date\.now\(\) \+ expiresInHours \* 3600 \* 1000\)/);
  });

  it("tags resources with loopruntime-drill base + expires-at variant", () => {
    expect(src).toMatch(/"loopruntime-drill"/);
    expect(src).toMatch(/loopruntime-drill-expires-at-/);
  });

  it("tag values sanitized for DO tag rules (lowercase alphanumeric + dashes)", () => {
    expect(src).toMatch(/toLowerCase/);
    expect(src).toMatch(/replace\(\/\[\^a-z0-9-\]\/gi/);
  });
});

describe("loopruntime-drill — Outputs schema v2 (L2-unlock plan D1 named exports)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  // ==========================================================================
  // L2-unlock plan T1.2 TDD specs:
  //   test_named_exports_present()       — assert all 16 required exports
  //   test_no_legacy_outputs_object()    — assert NO `export const outputs = {`
  //   test_kubeconfig_exports_are_Output_T() — assert exports reference Output<T>
  // ==========================================================================

  it("does NOT use object-literal `export const outputs = {` (v1 legacy)", () => {
    // The legacy v1 schema (`export const outputs = { mgmtCluster: {...}, ... }`)
    // is BANNED per L2-unlock D1. Caught Finding #3 (drill 2026-05-16):
    // object-literal nested Output<T> serialized to "null" string.
    //
    // Match must be on real `export const outputs = {` declaration, NOT on
    // commented-out documentation line (// export const outputs = {...).
    const realDeclaration = src.split("\n").some((line) => {
      const trimmed = line.trimStart();
      return /^export const outputs\s*=\s*\{/.test(trimmed);
    });
    expect(realDeclaration).toBe(false);
  });

  it("declares 4 named exports per cluster (id + name + endpoint + kubeconfig)", () => {
    const clusters = ["mgmtCluster", "buildCluster", "runtimeCluster", "runtimeCluster2"];
    const suffixes = ["Id", "Name", "Endpoint", "Kubeconfig"];
    for (const c of clusters) {
      for (const s of suffixes) {
        const exportName = `${c}${s}`;
        const re = new RegExp(`^export const ${exportName}\\s*=`, "m");
        expect(src).toMatch(re);
      }
    }
  });

  it("kubeconfig exports reference Pulumi cluster.kubeConfigs[0].rawConfig (Output<T>)", () => {
    // Verify the value side of `export const X = Y` references real Pulumi
    // resource. If exports point to literals/null, fix would not work.
    const clusters = [
      ["mgmtClusterKubeconfig", "mgmtCluster"],
      ["buildClusterKubeconfig", "buildCluster"],
      ["runtimeClusterKubeconfig", "runtimeCluster"],
      ["runtimeCluster2Kubeconfig", "runtimeCluster2"],
    ];
    for (const [exportName, resourceVar] of clusters) {
      const re = new RegExp(
        `export const ${exportName}\\s*=\\s*${resourceVar}\\.kubeConfigs\\[0\\]\\.rawConfig`,
      );
      expect(src).toMatch(re);
    }
  });

  it("declares 4 PG named exports (uri, host, port, database)", () => {
    expect(src).toMatch(/^export const pgUri\s*=\s*drillPG\.uri/m);
    expect(src).toMatch(/^export const pgHost\s*=\s*drillPG\.host/m);
    expect(src).toMatch(/^export const pgPort\s*=\s*drillPG\.port/m);
    expect(src).toMatch(/^export const pgDatabase\s*=\s*drillPG\.database/m);
  });

  it("declares 3 bucket named exports (builds, artifacts, static)", () => {
    expect(src).toMatch(/^export const bucketBuilds\s*=\s*drillBuildsBucket\.name/m);
    expect(src).toMatch(/^export const bucketArtifacts\s*=\s*drillArtifactsBucket\.name/m);
    expect(src).toMatch(/^export const bucketStatic\s*=\s*drillStaticBucket\.name/m);
  });

  it("declares VPC named exports (id, ipRange)", () => {
    expect(src).toMatch(/^export const vpcId\s*=\s*drillVpc\.id/m);
    expect(src).toMatch(/^export const vpcIpRange\s*=\s*"10\.40\.0\.0\/16"/m);
  });

  it("declares CF Zone named exports (id, nameservers)", () => {
    expect(src).toMatch(/^export const zoneId\s*=\s*drillZone\.id/m);
    expect(src).toMatch(/^export const zoneNameservers\s*=\s*drillZone\.nameServers/m);
  });

  it("declares DO project named export", () => {
    expect(src).toMatch(/^export const doProjectId\s*=\s*drillDOProject\.id/m);
  });

  it("retains `meta` object export for watchdog + cost-report compatibility", () => {
    // meta is intentionally kept as object — drill-watchdog.sh + drill-cost-
    // report.sh parse it as a single output. Future migration to flat
    // `metaDomain`/`metaExpiresAtIso` is documented TODO but not in scope here.
    expect(src).toMatch(/^export const meta\s*=\s*\{/m);
  });

  it("meta block declares drill purpose + cell mapping + teardown policy", () => {
    expect(src).toContain('purpose: "DR drill prod-identical');
    expect(src).toMatch(/teardownPolicy:/);
    expect(src).toMatch(/cellMapping:/);
    expect(src).toMatch(/"cell-0":\s*"runtimeCluster"/);
    expect(src).toMatch(/"cell-1":\s*"runtimeCluster2"/);
  });

  it("documents D1 rationale + RCA reference in source comments", () => {
    expect(src).toMatch(/FLAT NAMED EXPORTS/);
    expect(src).toMatch(/finding-2026-05-16-pulumi-outputs-rca/);
  });

  it("does NOT export legacy singular `cluster` / `clusterKubeconfig`", () => {
    // v0 legacy keys would resurrect Finding #3 collision.
    expect(src).not.toMatch(/^export const cluster\s*=/m);
    expect(src).not.toMatch(/^export const clusterKubeconfig\s*=/m);
    expect(src).not.toMatch(/^export const bucket\s*=/m); // legacy singular bucket
  });

  it("declares exactly 37 top-level named exports (33 prior + 4 redis via Bug #15)", () => {
    // Sanity bound — count helps catch silent additions/removals that drift schema.
    // Real exports only (not commented-out documentation).
    const realExports = src.split("\n").filter((line) => {
      const trimmed = line.trimStart();
      return /^export const \w+\s*=/.test(trimmed) && !trimmed.startsWith("//");
    });
    // 4 clusters × 4 fields = 16 cluster exports
    // + 4 PG exports (uri, host, port, database)
    // + 4 Redis exports (host, port, password, uri) — Bug #15 LIVE fix
    // + 3 bucket exports (builds, artifacts, static)
    // + 2 VPC exports (id, ipRange)
    // + 2 zone exports (id, nameservers)
    // + 1 doProjectId
    // + 1 meta
    // = 33 v2 named exports + 4 DSN (Phase 2 T1.3) = 37 total
    expect(realExports.length).toBe(37);
  });
});

// ============================================================================
// Phase 2 plan T1.1 + T1.2 + T1.3 + T1.4 — per-app DBs + users + DSN exports
// ============================================================================

describe("loopruntime-drill — Per-app DatabaseDb resources (Phase 2 T1.1)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("provisions exactly 4 DatabaseDb resources", () => {
    const matches = src.match(/new digitalocean\.DatabaseDb\(/g) ?? [];
    expect(matches.length).toBe(4);
  });

  it("declares 4 DBs with kratos/hydra/gitea/loopruntime names", () => {
    expect(src).toMatch(/new digitalocean\.DatabaseDb\("drill-pg-kratos"/);
    expect(src).toMatch(/new digitalocean\.DatabaseDb\("drill-pg-hydra"/);
    expect(src).toMatch(/new digitalocean\.DatabaseDb\("drill-pg-gitea"/);
    expect(src).toMatch(/new digitalocean\.DatabaseDb\("drill-pg-loopruntime"/);
  });

  it("each DatabaseDb references drillPG.id for clusterId", () => {
    const dbBlocks = src.match(/new digitalocean\.DatabaseDb\([^)]+\{[^}]+\}/g) ?? [];
    expect(dbBlocks.length).toBe(4);
    for (const block of dbBlocks) {
      expect(block).toMatch(/clusterId:\s*drillPG\.id/);
    }
  });

  it("database names exactly: kratos, hydra, gitea, loopruntime", () => {
    expect(src).toMatch(/name:\s*"kratos"/);
    expect(src).toMatch(/name:\s*"hydra"/);
    expect(src).toMatch(/name:\s*"gitea"/);
    expect(src).toMatch(/name:\s*"loopruntime"/);
  });
});

describe("loopruntime-drill — Per-app DatabaseUser resources (Phase 2 T1.2)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("provisions exactly 4 DatabaseUser resources", () => {
    const matches = src.match(/new digitalocean\.DatabaseUser\(/g) ?? [];
    expect(matches.length).toBe(4);
  });

  it("declares 4 Users with kratos/hydra/gitea/loopruntime-api Pulumi resource names", () => {
    expect(src).toMatch(/new digitalocean\.DatabaseUser\("drill-pg-kratos-user"/);
    expect(src).toMatch(/new digitalocean\.DatabaseUser\("drill-pg-hydra-user"/);
    expect(src).toMatch(/new digitalocean\.DatabaseUser\("drill-pg-gitea-user"/);
    expect(src).toMatch(/new digitalocean\.DatabaseUser\("drill-pg-loopruntime-api-user"/);
  });

  it("user names match pulumi-infra: kratos, hydra, gitea, loopruntime_api", () => {
    const userBlocks = src.match(/new digitalocean\.DatabaseUser\([^)]+\{[^}]+\}/g) ?? [];
    const userNames = userBlocks
      .map((b) => b.match(/name:\s*"([^"]+)"/)?.[1])
      .filter(Boolean)
      .sort();
    expect(userNames).toEqual(["gitea", "hydra", "kratos", "loopruntime_api"]);
  });

  it("each DatabaseUser references drillPG.id for clusterId", () => {
    const userBlocks = src.match(/new digitalocean\.DatabaseUser\([^)]+\{[^}]+\}/g) ?? [];
    expect(userBlocks.length).toBe(4);
    for (const block of userBlocks) {
      expect(block).toMatch(/clusterId:\s*drillPG\.id/);
    }
  });
});

describe("loopruntime-drill — DatabaseFirewall (Phase 2 Bug #11 LIVE fix)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("declares 2 DatabaseFirewall (drill-pg-firewall + drill-redis-firewall via Bug #15)", () => {
    const matches = src.match(/new digitalocean\.DatabaseFirewall\(/g) ?? [];
    expect(matches.length).toBe(2);
    expect(src).toMatch(/new digitalocean\.DatabaseFirewall\("drill-pg-firewall"/);
    expect(src).toMatch(/new digitalocean\.DatabaseFirewall\("drill-redis-firewall"/);
  });

  it("firewall references drillPG.id for clusterId", () => {
    expect(src).toMatch(/DatabaseFirewall\("drill-pg-firewall"[\s\S]*?clusterId:\s*drillPG\.id/);
  });

  it("firewall allows all 4 DOKS clusters (mgmt + build + runtime + runtime-2)", () => {
    const firewallBlock = src.match(/DatabaseFirewall\("drill-pg-firewall"[\s\S]+?\}\);/)?.[0] ?? "";
    expect(firewallBlock).toMatch(/type:\s*"k8s",\s*value:\s*mgmtCluster\.id/);
    expect(firewallBlock).toMatch(/type:\s*"k8s",\s*value:\s*buildCluster\.id/);
    expect(firewallBlock).toMatch(/type:\s*"k8s",\s*value:\s*runtimeCluster\.id/);
    expect(firewallBlock).toMatch(/type:\s*"k8s",\s*value:\s*runtimeCluster2\.id/);
  });

  it("documents Bug #11 LIVE root cause in source comment", () => {
    expect(src).toMatch(/Bug #11[\s\S]*?context deadline exceeded/);
  });
});

describe("loopruntime-drill — Per-service DSN named exports (Phase 2 T1.3)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("declares 4 DSN exports (kratosDsn, hydraDsn, giteaDsn, loopruntimeApiDsn)", () => {
    expect(src).toMatch(/^export const kratosDsn\s*=/m);
    expect(src).toMatch(/^export const hydraDsn\s*=/m);
    expect(src).toMatch(/^export const giteaDsn\s*=/m);
    expect(src).toMatch(/^export const loopruntimeApiDsn\s*=/m);
  });

  it("DSN exports use pulumi.interpolate for Output<T> handling", () => {
    expect(src).toMatch(/export const kratosDsn\s*=\s*pulumi\.interpolate/);
    expect(src).toMatch(/export const hydraDsn\s*=\s*pulumi\.interpolate/);
    expect(src).toMatch(/export const giteaDsn\s*=\s*pulumi\.interpolate/);
    expect(src).toMatch(/export const loopruntimeApiDsn\s*=\s*pulumi\.interpolate/);
  });

  it("DSN format: postgres://USER:PASS@HOST:PORT/DB?sslmode=require", () => {
    // EC-02 from parent plan: sslmode=require for managed PG
    // Bug #12 fix: USER = drillPG.user (doadmin), per-app DB still preserves isolation
    const dsnPattern = /pulumi\.interpolate`postgres:\/\/\$\{drillPG\.user\}:\$\{drillPG\.password\}@\$\{drillPG\.host\}:\$\{drillPG\.port\}\/\$\{[^}]+\.name\}\?sslmode=require`/;
    const matches = src.match(new RegExp(dsnPattern.source, "g")) ?? [];
    expect(matches.length).toBe(4);
  });

  it("each DSN uses doadmin auth (Bug #12 fix — schema public permission)", () => {
    // DO Managed PG 16 restricts CREATE on public schema for per-app users.
    // Mirrors prod precedent: pulumi-platform/src/index.ts:112-115 (Gitea uses doadmin).
    // Per-app DB names preserved for logical isolation.
    expect(src).toMatch(/kratosDsn[\s\S]*?\$\{drillPG\.user\}[\s\S]*?\/\$\{drillPgKratos\.name\}/);
    expect(src).toMatch(/hydraDsn[\s\S]*?\$\{drillPG\.user\}[\s\S]*?\/\$\{drillPgHydra\.name\}/);
    expect(src).toMatch(/giteaDsn[\s\S]*?\$\{drillPG\.user\}[\s\S]*?\/\$\{drillPgGitea\.name\}/);
    expect(src).toMatch(/loopruntimeApiDsn[\s\S]*?\$\{drillPG\.user\}[\s\S]*?\/\$\{drillPgLoopRuntime\.name\}/);
  });

  it("documents Bug #12 root cause in source comment", () => {
    expect(src).toMatch(/Bug #12 LIVE FIX/);
    expect(src).toMatch(/permission denied for schema/);
    expect(src).toMatch(/Mirrors prod precedent/);
  });

  it("documents T1.3.bis --show-secrets warning in source comment", () => {
    expect(src).toMatch(/CRITICAL.*--show-secrets/);
  });
});

describe("loopruntime-drill — Isolation invariants (D7 + MEET-ASYNC-AMENDMENT-2026-05-16-001)", () => {
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

describe("loopruntime-drill — Pulumi.drill.yaml config (T1.1)", () => {
  const drillYamlPath = path.join(ROOT, "Pulumi.drill.yaml");
  const drillYaml = yaml.load(fs.readFileSync(drillYamlPath, "utf-8")) as Record<string, unknown>;
  const conf = drillYaml.config as Record<string, unknown>;

  it("declares all 4-cluster sizing config keys", () => {
    expect(conf["loopruntime-drill:mgmtK8sVersion"]).toBeDefined();
    expect(conf["loopruntime-drill:mgmtNodeSize"]).toBeDefined();
    expect(conf["loopruntime-drill:mgmtNodeCount"]).toBeDefined();
    expect(conf["loopruntime-drill:buildNodeSize"]).toBeDefined();
    expect(conf["loopruntime-drill:buildNodeCount"]).toBeDefined();
    expect(conf["loopruntime-drill:runtimeNodeSize"]).toBeDefined();
    expect(conf["loopruntime-drill:runtimeNodeCount"]).toBeDefined();
  });

  it("declares expiresInHours config key", () => {
    expect(conf["loopruntime-drill:expiresInHours"]).toBeDefined();
  });

  it("defaults mgmt to medium tier (s-4vcpu-8gb-amd)", () => {
    expect(conf["loopruntime-drill:mgmtNodeSize"]).toBe("s-4vcpu-8gb-amd");
  });

  it("defaults runtime to small tier (s-2vcpu-4gb)", () => {
    expect(conf["loopruntime-drill:runtimeNodeSize"]).toBe("s-2vcpu-4gb");
  });

  it("CF account ID is encrypted secret", () => {
    const cfId = conf["loopruntime-drill:cfAccountId"] as Record<string, unknown>;
    expect(cfId).toBeDefined();
    expect(cfId.secure).toBeDefined();
  });
});
