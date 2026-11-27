// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// The 2026-08-09 1:1 cell unification renamed the old `default` cell to
// `cell-0` in place. Where the drill listed both `default` and `cell-0`, the
// rename produced `cell-0` TWICE: once in this stack's `meta.cellMapping`
// object literal (a duplicate key TypeScript rejects, TS1117) and once in the
// helmfile drill environment's `cells:` list, which then declared three cells
// for two clusters. One cell per cluster is the contract; these specs hold it.

const ROOT = resolve(__dirname, "..", "..");
const INDEX_TS = resolve(ROOT, "src", "index.ts");
const DRILL_ENV = resolve(ROOT, "..", "helmfile", "environments", "drill.yaml");

describe("drill cell mapping", () => {
  it("maps each cell of the stack's meta output exactly once", () => {
    const src = readFileSync(INDEX_TS, "utf-8");
    const block = src.match(/cellMapping:\s*\{([^}]*)\}/)?.[1] ?? "";
    const cells = [...block.matchAll(/"(cell-\d+)":/g)].map((m) => m[1]);
    expect(cells).toEqual(["cell-0", "cell-1"]);
  });

  it("declares each cell of the helmfile drill environment exactly once", () => {
    const env = readFileSync(DRILL_ENV, "utf-8");
    const block = env.split(/^cells:\s*$/m)[1]?.split(/^\S/m)[0] ?? "";
    const ids = [...block.matchAll(/^\s*-\s*id:\s*(\S+)\s*$/gm)].map((m) => m[1]);
    expect(ids).toEqual(["cell-0", "cell-1"]);
  });
});
