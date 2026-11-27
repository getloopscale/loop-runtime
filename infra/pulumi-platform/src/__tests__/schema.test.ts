// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Schema + structure tests for loop-runtime-platform. Audit gap M05 #9
// (active-execution-plan §6.E.2): pulumi-platform shipped 4434 LOC with
// zero .test.ts. These tests lock the public surface so refactors that
// drop required fields, rename service classes, or break the index
// orchestration sequence fail loud at CI instead of at `pulumi up`.

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";
import * as yaml from "js-yaml";

const SRC_DIR = path.resolve(__dirname, "..");
const PLATFORM_DIR = path.resolve(__dirname, "../..");

// ---------------------------------------------------------------------------
// config.ts — PlatformConfig schema must include all required + optional fields.
// ---------------------------------------------------------------------------

describe("config.ts — PlatformConfig schema", () => {
  const src = fs.readFileSync(path.join(SRC_DIR, "config.ts"), "utf-8");

  it("declares all required (non-optional) fields", () => {
    // Required fields per config.ts:34-44 — environment, domain, region, dnsZone
    // are config.require; spacesAccessKeyId + spacesSecretAccessKey are
    // requireSecret. A regression that drops require() for any of these
    // would let a deploy proceed with undefined values.
    const required = [
      'config.require("environment")',
      'config.require("domain")',
      'config.require("region")',
      'config.requireSecret("spacesAccessKeyId")',
      'config.requireSecret("spacesSecretAccessKey")',
    ];
    for (const r of required) {
      expect(src).toContain(r);
    }
  });

  it("provides optional fields via getSecret / get with undefined fallback", () => {
    // Optional fields must use getSecret(...) ?? undefined or get(...) ?? undefined
    // pattern. Drift to require() would force users to set values that aren't needed.
    const optionalGetters = [
      "config.getSecret(\"giteaAdminPassword\") ?? undefined",
      "config.getSecret(\"minioRootPassword\") ?? undefined",
      "config.getSecret(\"cfApiToken\") ?? undefined",
      "config.get(\"cfAccountId\") ?? undefined",
      "config.get(\"cfZoneId\") ?? undefined",
    ];
    for (const o of optionalGetters) {
      expect(src).toContain(o);
    }
  });

  it("dnsZone defaults to domain when not explicitly set", () => {
    // config.ts:41 — `dnsZone: config.get("dnsZone") ?? config.require("domain")`.
    // This default lets dev stacks reuse the domain when the dns zone matches it.
    // Regression: forcing require() for dnsZone would break stacks that haven't
    // set it explicitly.
    expect(src).toMatch(/dnsZone:\s*config\.get\("dnsZone"\)\s*\?\?\s*config\.require\("domain"\)/);
  });
});

// ---------------------------------------------------------------------------
// services/ — each file MUST export the expected ComponentResource class.
// The index.ts imports these by name; a rename without updating index breaks
// pulumi up with an unhelpful "X is not a constructor" error.
// ---------------------------------------------------------------------------

describe("services/ — ComponentResource class exports", () => {
  const expected: Record<string, string[]> = {
    "argocd.ts": ["ArgoCD"],
    "cert-manager.ts": ["CertManager"],
    "gitea.ts": ["Gitea"],
    "grafana.ts": ["Grafana"],
    "loki.ts": ["Loki"],
    "prometheus.ts": ["Prometheus"],
    "tempo.ts": ["Tempo", "OtelCollector"],
    "traefik.ts": ["Traefik"],
  };

  for (const [file, classes] of Object.entries(expected)) {
    it(`${file} exports [${classes.join(", ")}]`, () => {
      const src = fs.readFileSync(path.join(SRC_DIR, "services", file), "utf-8");
      for (const cls of classes) {
        // `export class X extends pulumi.ComponentResource` is the canonical pattern.
        expect(src).toMatch(new RegExp(`export class ${cls} extends pulumi\\.ComponentResource`));
      }
    });
  }
});

// ---------------------------------------------------------------------------
// index.ts — orchestration entry point must import every service that
// services/ exports. A new service added under services/ but not wired into
// index would silently not deploy.
// ---------------------------------------------------------------------------

describe("index.ts — service orchestration imports", () => {
  const src = fs.readFileSync(path.join(SRC_DIR, "index.ts"), "utf-8");
  const expectedImports = [
    'import { CertManager } from "./services/cert-manager"',
    'import { Traefik } from "./services/traefik"',
    'import { ArgoCD } from "./services/argocd"',
    'import { Gitea } from "./services/gitea"',
    'import { Loki } from "./services/loki"',
    'import { Prometheus } from "./services/prometheus"',
    'import { Grafana } from "./services/grafana"',
    'import { Tempo, OtelCollector } from "./services/tempo"',
  ];

  for (const imp of expectedImports) {
    it(`imports ${imp.match(/{ ([^}]+) }/)?.[1]}`, () => {
      expect(src).toContain(imp);
    });
  }

  it("loads config via loadPlatformConfig()", () => {
    expect(src).toContain("loadPlatformConfig()");
  });
});

// ---------------------------------------------------------------------------
// refs.ts — StackReference fail-fast contract. Drift to .getOutput() would
// silently swallow missing exports.
// ---------------------------------------------------------------------------

describe("refs.ts — StackReference fail-fast", () => {
  const src = fs.readFileSync(path.join(SRC_DIR, "refs.ts"), "utf-8");

  it("uses requireOutput() for all infra-side fields", () => {
    // Audit-critical: requireOutput() panics if loop-runtime-infra didn't export the
    // field. getOutput() returns undefined silently. The platform stack MUST
    // fail-fast on missing infra outputs.
    expect(src).not.toMatch(/\.getOutput\(/);
    expect(src).toMatch(/\.requireOutput\(/);
  });

  it("permits infraStackRef override via config", () => {
    // refs.ts:19 — `cfg.get("infraStackRef") ?? \`organization/loop-runtime-infra/${pulumi.getStack()}\``.
    // Allows per-developer stacks (e.g. paulo-dev) to reference a non-canonical infra stack.
    expect(src).toContain('cfg.get("infraStackRef")');
  });

  it("requires the 5 critical infra outputs (kubeconfigs + DB)", () => {
    const required = [
      'requireOutput("mgmtKubeconfig")',
      'requireOutput("buildKubeconfig")',
      'requireOutput("runtimeKubeconfig")',
      'requireOutput("dbHost")',
      'requireOutput("dbUri")',
    ];
    for (const r of required) {
      expect(src).toContain(r);
    }
  });
});

// ---------------------------------------------------------------------------
// Pulumi.dev.yaml — baseline config keys present so dev stack init doesn't
// regress (e.g., someone removes cfAccountId from baseline and dev fails to
// adopt CF zone).
// ---------------------------------------------------------------------------

describe("Pulumi.dev.yaml — dev stack baseline", () => {
  const raw = fs.readFileSync(path.join(PLATFORM_DIR, "Pulumi.dev.yaml"), "utf-8");
  const doc = yaml.load(raw) as { config: Record<string, unknown> };

  it("declares the 4 required environment/domain keys", () => {
    expect(doc.config["loop-runtime-platform:environment"]).toBe("dev");
    expect(doc.config["loop-runtime-platform:domain"]).toBeDefined();
    expect(doc.config["loop-runtime-platform:region"]).toBeDefined();
    expect(doc.config["loop-runtime-platform:dnsZone"]).toBeDefined();
  });

  it("declares Cloudflare account+zone IDs (CF Worker delivery)", () => {
    expect(doc.config["loop-runtime-platform:cfAccountId"]).toBeDefined();
    expect(doc.config["loop-runtime-platform:cfZoneId"]).toBeDefined();
  });

  it("declares the cross-cluster Loki push URL", () => {
    expect(doc.config["loop-runtime-platform:lokiPushUrl"]).toBeDefined();
  });
});
