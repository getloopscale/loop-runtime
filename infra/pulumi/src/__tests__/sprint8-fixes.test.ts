// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Sprint 8 v3.2 / CHG-001 — golden tests for the 6 source-level fixes that
// surfaced during Phase -1 bootstrap.
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP / T1.1 — these fixes migrated:
//   - Pulumi `mgmt-ory.ts`, `runtime-keda.ts`, `runtime-policy-controller.ts`,
//     `mgmt-api.ts`, `mgmt-tenant-controller.ts` were DELETED in T1.1.
//   - Their fixes now live in:
//       * Helmfile `cell.helmfile.yaml` (KEDA chart name, policy-controller install)
//       * Helm chart values (kratos-ui memory, loopruntime-controller env)
//
// Only Fix #1 (registry adopt-or-create) remains in Pulumi (`infra.ts`) because
// the registry is L1 cloud (DOCR), not L2 Kubernetes.

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";

const componentsDir = path.resolve(__dirname, "..", "components");
const helmfileDir = path.resolve(__dirname, "..", "..", "..", "helmfile");

function read(relativePath: string, baseDir: string = componentsDir): string {
  return fs.readFileSync(path.join(baseDir, relativePath), "utf-8");
}

function readIfExists(relativePath: string, baseDir: string = componentsDir): string | null {
  const fp = path.join(baseDir, relativePath);
  return fs.existsSync(fp) ? fs.readFileSync(fp, "utf-8") : null;
}

describe("Sprint 8 v3.2 — fixes preserved across L1/L2 split", () => {
  describe("Fix #1: registry adopt-or-create (infra.ts) — L1 (still in Pulumi)", () => {
    const src = read("infra.ts");

    it("does NOT use ${config.project}-registry as the registry name", () => {
      expect(src).not.toContain("`${config.project}-registry`");
      expect(src).not.toContain("'${config.project}-registry'");
    });

    it("consumes orphans?.registryImportId via adopt-or-create lookup", () => {
      expect(src).toContain("orphans?.registryImportId");
    });

    it("falls back to canonical 'loopruntime-system' name when lookup returns undefined", () => {
      expect(src).toMatch(/orphans\?\.registryImportId\s*\?\?\s*"loopruntime-system"/);
    });
  });

  describe("Fix #2: KEDA HTTP add-on chart name — migrated to cell.helmfile.yaml", () => {
    const helmfile = readIfExists("cell.helmfile.yaml.gotmpl", helmfileDir);

    it("cell.helmfile.yaml.gotmpl exists (T2.3 deliverable)", () => {
      expect(helmfile).not.toBeNull();
    });

    it("uses upstream chart name 'keda-add-ons-http' (not deprecated 'http-add-on')", () => {
      expect(helmfile).toMatch(/chart:\s*kedacore\/keda-add-ons-http/);
      expect(helmfile).not.toMatch(/chart:\s*kedacore\/http-add-on/);
    });
  });

  describe("Fix #3: policy-controller helm chart — cell.helmfile.yaml", () => {
    const helmfile = readIfExists("cell.helmfile.yaml.gotmpl", helmfileDir);

    it("policy-controller release present in runtime helmfile", () => {
      expect(helmfile).toMatch(/name:\s*policy-controller/);
      expect(helmfile).toMatch(/chart:\s*sigstore\/policy-controller/);
    });

    it("admission-policies release `needs:` policy-controller (B16)", () => {
      // The order is enforced via Helmfile needs:. Without this, deploys are
      // rejected by the deny-by-default webhook.
      expect(helmfile).toMatch(/name:\s*admission-policies[\s\S]*?needs:[\s\S]*?cosign-system\/policy-controller/);
    });
  });

  describe("Fix #4: cloudflared K8s objects migrated from tunnel.ts to Helmfile (ADR D9)", () => {
    const tunnel = read(path.join("..", "providers", "cloudflare", "tunnel.ts"));
    const helmfile = readIfExists("cell.helmfile.yaml.gotmpl", helmfileDir);

    it("tunnel.ts no longer imports @pulumi/kubernetes", () => {
      expect(tunnel).not.toMatch(/import.*\*.*from\s+["']@pulumi\/kubernetes["']/);
    });

    it("tunnel.ts no longer creates k8s.core.v1.Secret or k8s.apps.v1.Deployment", () => {
      expect(tunnel).not.toMatch(/k8s\.core\.v1\.Secret/);
      expect(tunnel).not.toMatch(/k8s\.apps\.v1\.Deployment/);
    });

    it("tunnel.ts exports tunnelToken for L2 consumption", () => {
      expect(tunnel).toMatch(/tunnelToken:\s*pulumi\.Output<string>/);
    });

    it("runtime helmfile has cloudflared release", () => {
      expect(helmfile).toMatch(/name:\s*cloudflared/);
    });
  });

  describe("Fix #5: ClusterImagePolicy as Helm chart (B16) — admission-policies", () => {
    const chartFile = path.resolve(__dirname, "..", "..", "..", "..", "charts", "loopruntime-admission-policies", "Chart.yaml");
    const valuesFile = path.resolve(__dirname, "..", "..", "..", "..", "charts", "loopruntime-admission-policies", "values.yaml");
    const cipTemplate = path.resolve(__dirname, "..", "..", "..", "..", "charts", "loopruntime-admission-policies", "templates", "cluster-image-policy.yaml");

    it("Chart.yaml exists for admission-policies", () => {
      expect(fs.existsSync(chartFile)).toBe(true);
    });

    it("values.yaml has clusterImagePolicy.enabled: true (default)", () => {
      const v = fs.readFileSync(valuesFile, "utf-8");
      expect(v).toMatch(/clusterImagePolicy:\s*\n\s*enabled:\s*true/);
    });

    it("template renders ClusterImagePolicy with mode 'warn'", () => {
      const t = fs.readFileSync(cipTemplate, "utf-8");
      expect(t).toContain("kind: ClusterImagePolicy");
      expect(t).toMatch(/\.Values\.clusterImagePolicy\.mode/);
    });
  });

  describe("Fix #6: @pulumi/kubernetes removed from package.json (T1.3)", () => {
    const pkg = JSON.parse(fs.readFileSync(path.resolve(__dirname, "..", "..", "package.json"), "utf-8"));

    it("@pulumi/kubernetes is NOT a runtime dependency", () => {
      expect(pkg.dependencies).not.toHaveProperty("@pulumi/kubernetes");
    });

    it("@pulumi/kubernetes is NOT a devDependency either", () => {
      expect(pkg.devDependencies ?? {}).not.toHaveProperty("@pulumi/kubernetes");
    });
  });

  describe("Fix #7: zero @pulumi/kubernetes imports in source (T1.1 cleanup)", () => {
    function* walk(dir: string): Generator<string> {
      for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        const fp = path.join(dir, entry.name);
        if (entry.isDirectory()) {
          if (entry.name === "node_modules" || entry.name === "__tests__") continue;
          yield* walk(fp);
        } else if (entry.name.endsWith(".ts")) {
          yield fp;
        }
      }
    }

    const srcRoot = path.resolve(__dirname, "..");
    const offenders: string[] = [];
    for (const fp of walk(srcRoot)) {
      const content = fs.readFileSync(fp, "utf-8");
      if (/@pulumi\/kubernetes/.test(content)) {
        offenders.push(path.relative(srcRoot, fp));
      }
    }

    it("no source file imports @pulumi/kubernetes", () => {
      expect(offenders).toEqual([]);
    });
  });
});
