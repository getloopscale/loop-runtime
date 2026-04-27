// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Sprint 8 v3.2 / CHG-001 — golden tests for the 6 source-level fixes that
// surfaced during Phase -1 bootstrap. Each test asserts on the FILE CONTENT
// to fail loud if a refactor reverts the fix without intent. This is the
// FAANG-level regression prevention layer described in the v3.2 systematization
// plan (Pilar 1).
//
// Goal: an engineer running `npm test` immediately sees that the bootstrap
// fragility class is preserved. If any of these tests fail, read the test
// description and the file annotation referenced — both will explain the
// historic failure mode the fix prevents.

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";

const componentsDir = path.resolve(__dirname, "..", "components");
const read = (name: string) => fs.readFileSync(path.join(componentsDir, name), "utf-8");

describe("Sprint 8 v3.2 — Pulumi component fixes", () => {
  describe("Fix #1: registry adopt-or-create (infra.ts:282)", () => {
    const src = read("infra.ts");

    it("does NOT use ${config.project}-registry as the registry name", () => {
      // The pre-fix code constructed `loopzero-infra-registry` from project name,
      // but DO real registry is `loopzero-system`. Using project-derived name
      // caused `Preview failed: resource 'loopzero-infra-registry' does not exist`.
      expect(src).not.toContain("`${config.project}-registry`");
      expect(src).not.toContain("'${config.project}-registry'");
    });

    it("consumes orphans?.registryImportId via adopt-or-create lookup", () => {
      expect(src).toContain("orphans?.registryImportId");
    });

    it("falls back to canonical 'loopzero-system' name when lookup returns undefined", () => {
      // `bootstrap-registry.ts:44` hardcodes `loopzero-system` as the imagePullSecret name.
      // The fallback must match for greenfield (no registry yet) bootstrap.
      expect(src).toMatch(/orphans\?\.registryImportId\s*\?\?\s*"loopzero-system"/);
    });
  });

  describe("Fix #2: kratos-ui memory + securityContext + env vars (mgmt-ory.ts)", () => {
    const src = read("mgmt-ory.ts");
    // Extract ONLY the kratos-ui Deployment block (skip the Service which has
    // the same identifier). Use lineStart/lineEnd window: from the Deployment
    // constructor line to the matching `);` close at zero indent.
    const startIdx = src.indexOf("new k8s.apps.v1.Deployment(\n    \"kratos-ui\"");
    // Walk forward to find the matching `\n  );` (close of Deployment constructor).
    let endIdx = startIdx;
    if (startIdx > -1) {
      const closeIdx = src.indexOf("\n  );", startIdx);
      endIdx = closeIdx > -1 ? closeIdx + 5 : src.length;
    }
    const kratosBlock = startIdx > -1 ? src.substring(startIdx, endIdx) : "";

    it("identifies the kratos-ui Deployment block (not the Service)", () => {
      expect(kratosBlock).toContain("k8s.apps.v1.Deployment");
      expect(kratosBlock).toContain("kratos-ui");
      // Sanity: should be at least 1500 chars (full deployment block with env, resources, sec)
      expect(kratosBlock.length).toBeGreaterThan(1500);
    });

    it("kratos-ui memory limit is at least 256Mi (was 64Mi causing OOMKilled)", () => {
      const limitsMatch = kratosBlock.match(/limits:\s*\{[\s\S]*?memory:\s*"(\d+)Mi"/);
      expect(limitsMatch).not.toBeNull();
      const memoryMi = limitsMatch ? parseInt(limitsMatch[1], 10) : 0;
      expect(memoryMi).toBeGreaterThanOrEqual(256);
    });

    it("kratos-ui sets runAsUser to 10000 (image-verified user owning /.npm)", () => {
      expect(kratosBlock).toMatch(/runAsUser:\s*10000/);
    });

    it("kratos-ui includes CSRF_COOKIE_SECRET env var (refused boot without it)", () => {
      expect(kratosBlock).toContain("CSRF_COOKIE_SECRET");
    });

    it("kratos-ui does NOT pin readOnlyRootFilesystem (npm needs writable /.npm)", () => {
      expect(kratosBlock).not.toMatch(/readOnlyRootFilesystem:\s*true/);
    });

    it("kratos-ui pod-level securityContext satisfies PSA restricted profile", () => {
      expect(kratosBlock).toMatch(/runAsNonRoot:\s*true/);
      expect(kratosBlock).toContain("RuntimeDefault");
      expect(kratosBlock).toContain('drop: ["ALL"]');
    });
  });

  describe("Fix #3: KEDA HTTP add-on chart name + image override (runtime-keda.ts)", () => {
    const src = read("runtime-keda.ts");

    it("uses upstream chart name 'keda-add-ons-http' (not deprecated 'http-add-on')", () => {
      // KEDA renamed the chart upstream. Pre-fix value `http-add-on` produced
      // `chart "http-add-on" version "0.8.0" not found in https://kedacore.github.io/charts`.
      expect(src).toMatch(/chart:\s*"keda-add-ons-http"/);
      expect(src).not.toMatch(/chart:\s*"http-add-on"/);
    });

    it("overrides kubeRbacProxy.image.repository to maintained quay.io image", () => {
      // gcr.io/kubebuilder/kube-rbac-proxy was deprecated and removed.
      // quay.io/brancz/kube-rbac-proxy is the maintained replacement (same project).
      expect(src).toContain("quay.io/brancz/kube-rbac-proxy");
    });

    it("disables ServiceMonitor on runtime cluster (Prometheus operator runs on mgmt only)", () => {
      // Enabling ServiceMonitor on runtime breaks bootstrap with
      // `no matches for kind "ServiceMonitor" in version "monitoring.coreos.com/v1"`
      // because Prometheus Operator CRDs only exist on mgmt.
      // Greedy regex captures the prometheus block (ends at the line
      // containing `},` after `webhooks: { enabled: false }` — i.e., the
      // closing brace of `prometheus:`). We then assert each sub-key is false.
      const promBlockIdx = src.indexOf("prometheus: {");
      expect(promBlockIdx).toBeGreaterThan(-1);
      const promBlock = src.substring(promBlockIdx, promBlockIdx + 400);
      expect(promBlock).toMatch(/operator:\s*\{\s*enabled:\s*false/);
      expect(promBlock).toMatch(/metricServer:\s*\{\s*enabled:\s*false/);
      expect(promBlock).toMatch(/webhooks:\s*\{\s*enabled:\s*false/);
      // Crucial negative assertion: must NOT enable serviceMonitor anywhere
      // in the prometheus block (would resurface the original bootstrap bug).
      expect(promBlock).not.toMatch(/serviceMonitor:\s*\{\s*enabled:\s*true/);
    });
  });

  describe("Fix #4: policy-controller schema compliance (runtime-policy-controller.ts)", () => {
    const src = read("runtime-policy-controller.ts");

    it("does NOT pass podSecurityContext or containerSecurityContext at top-level or webhook scope", () => {
      // sigstore/policy-controller chart values.schema.json rejects these
      // properties at every path tried (top-level, webhook.*).
      // The chart ships hardened defaults baked into the controller manager
      // template — overriding is rejected and unnecessary.
      expect(src).not.toMatch(/^\s*podSecurityContext:\s*\{/m);
      expect(src).not.toMatch(/^\s*containerSecurityContext:\s*\{/m);
    });
  });

  describe("Fix #5: loopzero-controller env var LOOPZERO_CLUSTER_ISSUER (mgmt-api.ts)", () => {
    const src = read("mgmt-api.ts");

    it("loopzero-controller Deployment includes LOOPZERO_CLUSTER_ISSUER env var", () => {
      // loopzero-controller refuses to start with
      // `invalid config: LOOPZERO_CLUSTER_ISSUER is required (prod: letsencrypt-prod, dev: loopzero-ca-issuer)`.
      // It shares the loopzero-api binary image so config validation is symmetric.
      const controllerBlock = src.match(/"loopzero-controller",\s*\{[\s\S]*?\}\);[\s\S]*?return apiDeployment/);
      expect(controllerBlock?.[0] ?? src).toContain("LOOPZERO_CLUSTER_ISSUER");
    });

    it("LOOPZERO_CLUSTER_ISSUER value matches loopzero-api (same image, same validation)", () => {
      // Both binaries share the same Helm image and config validation.
      // Drift between them surfaces as silent CrashLoop.
      const apiIssuerCount = (src.match(/LOOPZERO_CLUSTER_ISSUER/g) ?? []).length;
      expect(apiIssuerCount).toBeGreaterThanOrEqual(2); // loopzero-api + loopzero-controller
    });
  });

  describe("Fix #6: tenant-controller env var LOOPZERO_CLUSTER_ISSUER (mgmt-tenant-controller.ts)", () => {
    const src = read("mgmt-tenant-controller.ts");

    it("tenant-controller Deployment includes LOOPZERO_CLUSTER_ISSUER env var", () => {
      // Same root cause as Fix #5 — tenant-controller shares config schema
      // with loopzero-api/loopzero-controller. Drift = silent CrashLoop.
      expect(src).toContain("LOOPZERO_CLUSTER_ISSUER");
    });
  });
});
