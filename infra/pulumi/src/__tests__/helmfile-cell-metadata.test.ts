// CHG-2026-04-30-INFRA-NORMALIZE / T5.2 (helmfile-native path, audit Gap 3)
//
// Translates plan T5.2 acceptance into vitest. Original T5.2 was
// [N/A]-superseded (iter 4) because the implementation went helmfile-
// native instead of Pulumi.dev.yaml config keys. But the helmfile-
// native artifacts (runtimeCellId + runtime2CellId in
// infra/helmfile/environments/{dev,prod}.yaml) had NO test pinning
// them — if someone renames the keys or deletes cell-2 wiring, routing
// breaks silently in the next install.
//
// This test pins the helmfile-native cell metadata invariants:
//   - dev + prod environments declare runtimeCellId + runtime2CellId
//   - cell IDs match `^cell-[a-z0-9-]+$` regex (matches ClusterCredential
//     CRD validation per migration 000006_cells)
//   - dynamicRouter: true (Audit T0.1 Gap 4 fix)
//
// Catches drift like 'someone deleted runtime2CellId during cleanup'
// or 'cell-id format changed to underscore'. Cell metadata is now
// canonical via helmfile (not Pulumi); test enforces this.

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const ENV_DIR = resolve(REPO_ROOT, "infra", "helmfile", "environments");

const CELL_ID_RE = /^cell-[a-z0-9-]+$/;

// Extract a `key: value` pair from helmfile env YAML — values are simple
// strings or booleans (no nested structures). Returns undefined if key
// is absent.
function extract(text: string, key: string): string | undefined {
  const re = new RegExp(`^${key}:\\s*([^\\s#]+)`, "m");
  const m = text.match(re);
  return m?.[1]?.trim();
}

function loadEnv(name: string): string {
  const path = resolve(ENV_DIR, `${name}.yaml`);
  if (!existsSync(path)) return "";
  return readFileSync(path, "utf-8");
}

const TARGET_ENVS = ["dev", "prod"] as const;

describe("helmfile cell metadata invariants (T5.2 helmfile-native)", () => {
  for (const env of TARGET_ENVS) {
    describe(`environments/${env}.yaml`, () => {
      const text = loadEnv(env);

      it("file exists", () => {
        expect(text.length, `${env}.yaml must be non-empty`).toBeGreaterThan(0);
      });

      it("declares runtimeCellId (canonical cell for runtime cluster)", () => {
        const v = extract(text, "runtimeCellId");
        expect(v, "runtimeCellId must be set").toBeTruthy();
        expect(v).toMatch(CELL_ID_RE);
      });

      it("declares runtime2CellId (canonical cell for runtime-2 cluster)", () => {
        const v = extract(text, "runtime2CellId");
        expect(v, "runtime2CellId must be set").toBeTruthy();
        expect(v).toMatch(CELL_ID_RE);
      });

      it("runtimeCellId !== runtime2CellId (distinct cells)", () => {
        const c1 = extract(text, "runtimeCellId");
        const c2 = extract(text, "runtime2CellId");
        expect(c1).not.toBe(c2);
      });

      it("declares dynamicRouter: true (Audit T0.1 Gap 4)", () => {
        const v = extract(text, "dynamicRouter");
        expect(v, "dynamicRouter must be set").toBeTruthy();
        expect(v).toBe("true");
      });

      it("documents cells SQL migration ref (000006) in inline comments", () => {
        // The audit fix trail must remain visible in the helmfile so
        // future readers know cell metadata pairs with the cells table.
        expect(text).toMatch(/migration\s+000006|cells\s+(SQL\s+)?table|cells.*seeded/i);
      });
    });
  }

  it("dev and prod use the SAME canonical cell IDs (cell-1, cell-2)", () => {
    // Plan invariant: cell IDs are stable across environments — only
    // K8s context differs. Catches drift like 'someone renamed cell-1
    // to cell-prod-1 in prod.yaml only'.
    const dev = loadEnv("dev");
    const prod = loadEnv("prod");
    expect(extract(dev, "runtimeCellId")).toBe(extract(prod, "runtimeCellId"));
    expect(extract(dev, "runtime2CellId")).toBe(extract(prod, "runtime2CellId"));
  });
});
