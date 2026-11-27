// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";

// Direct import of the parser logic for unit testing (no Pulumi runtime needed)
// We test the validation and parsing logic, not the Pulumi config loading.

import { TopologyConfig, ClusterSpec } from "../topology/types";

// Minimal test parser (mirrors loader.ts logic without Pulumi dependency)
function parseTopology(raw: any): TopologyConfig {
  const parsePlane = (arr: any[], role: "mgmt" | "build" | "runtime"): ClusterSpec[] => {
    if (!arr || !Array.isArray(arr) || arr.length === 0) {
      throw new Error(`${role}: must have at least 1 cluster`);
    }
    return arr.map((entry, idx) => {
      if (!entry.id) throw new Error(`${role}[${idx}]: missing 'id'`);
      if (!entry.region) throw new Error(`${role}[${idx}] (${entry.id}): missing 'region'`);
      if (!entry.nodeClass) throw new Error(`${role}[${idx}] (${entry.id}): missing 'nodeClass'`);
      if (typeof entry.minNodes !== "number" || entry.minNodes < 1) {
        throw new Error(`${role}[${idx}] (${entry.id}): 'minNodes' must be >= 1`);
      }
      if (role === "runtime" && !entry.cellId) {
        throw new Error(`${role}[${idx}] (${entry.id}): runtime cluster requires 'cellId'`);
      }
      return { id: entry.id, role, cellId: entry.cellId, region: entry.region, nodeClass: entry.nodeClass, minNodes: entry.minNodes, maxNodes: entry.maxNodes } as ClusterSpec;
    });
  };

  const validate = (t: TopologyConfig) => {
    const ids = new Set<string>();
    for (const c of [...t.mgmt, ...t.build, ...t.runtime]) {
      if (ids.has(c.id)) throw new Error(`duplicate cluster id: '${c.id}'`);
      ids.add(c.id);
    }
    const cellIds = new Set<string>();
    for (const c of t.runtime) {
      if (c.cellId) {
        if (cellIds.has(c.cellId)) throw new Error(`duplicate cellId: '${c.cellId}'`);
        cellIds.add(c.cellId);
      }
    }
  };

  const mgmt = parsePlane(raw.mgmt, "mgmt");
  const build = parsePlane(raw.build, "build");
  const runtime = parsePlane(raw.runtime, "runtime");
  const t = { mgmt, build, runtime };
  validate(t);
  return t;
}

describe("topology parser", () => {
  it("parses valid single-cluster topology", () => {
    const t = parseTopology({
      mgmt: [{ id: "mgmt-default", region: "sfo3", nodeClass: "small", minNodes: 3 }],
      build: [{ id: "build-default", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [{ id: "runtime-default", cellId: "cell-0", region: "sfo3", nodeClass: "small", minNodes: 2 }],
    });
    expect(t.mgmt).toHaveLength(1);
    expect(t.build).toHaveLength(1);
    expect(t.runtime).toHaveLength(1);
    expect(t.runtime[0].cellId).toBe("cell-0");
  });

  it("parses multi-cluster topology", () => {
    const t = parseTopology({
      mgmt: [{ id: "mgmt-default", region: "sfo3", nodeClass: "small", minNodes: 3 }],
      build: [
        { id: "build-shared", region: "sfo3", nodeClass: "medium", minNodes: 2 },
      ],
      runtime: [
        { id: "runtime-sfo3-01", cellId: "cell-sfo3-01", region: "sfo3", nodeClass: "small", minNodes: 2 },
        { id: "runtime-nyc3-01", cellId: "cell-nyc3-01", region: "nyc3", nodeClass: "medium", minNodes: 2 },
      ],
    });
    expect(t.runtime).toHaveLength(2);
    expect(t.runtime[0].region).toBe("sfo3");
    expect(t.runtime[1].region).toBe("nyc3");
  });

  it("parses 2 build clusters", () => {
    const t = parseTopology({
      mgmt: [{ id: "mgmt-default", region: "sfo3", nodeClass: "small", minNodes: 3 }],
      build: [
        { id: "build-sfo3", region: "sfo3", nodeClass: "medium", minNodes: 2 },
        { id: "build-nyc3", region: "nyc3", nodeClass: "large", minNodes: 3 },
      ],
      runtime: [{ id: "runtime-default", cellId: "cell-0", region: "sfo3", nodeClass: "small", minNodes: 2 }],
    });
    expect(t.build).toHaveLength(2);
  });

  it("rejects duplicate cluster ids", () => {
    expect(() => parseTopology({
      mgmt: [{ id: "shared-id", region: "sfo3", nodeClass: "small", minNodes: 3 }],
      build: [{ id: "shared-id", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [{ id: "runtime-default", cellId: "cell-0", region: "sfo3", nodeClass: "small", minNodes: 2 }],
    })).toThrow("duplicate cluster id");
  });

  it("rejects duplicate cellIds", () => {
    expect(() => parseTopology({
      mgmt: [{ id: "mgmt-default", region: "sfo3", nodeClass: "small", minNodes: 3 }],
      build: [{ id: "build-default", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [
        { id: "rt-1", cellId: "same-cell", region: "sfo3", nodeClass: "small", minNodes: 2 },
        { id: "rt-2", cellId: "same-cell", region: "nyc3", nodeClass: "small", minNodes: 2 },
      ],
    })).toThrow("duplicate cellId");
  });

  it("rejects runtime without cellId", () => {
    expect(() => parseTopology({
      mgmt: [{ id: "mgmt-default", region: "sfo3", nodeClass: "small", minNodes: 3 }],
      build: [{ id: "build-default", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [{ id: "runtime-default", region: "sfo3", nodeClass: "small", minNodes: 2 }],
    })).toThrow("runtime cluster requires 'cellId'");
  });

  it("rejects missing id", () => {
    expect(() => parseTopology({
      mgmt: [{ region: "sfo3", nodeClass: "small", minNodes: 3 }],
      build: [{ id: "build", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [{ id: "rt", cellId: "c", region: "sfo3", nodeClass: "small", minNodes: 2 }],
    })).toThrow("missing 'id'");
  });

  it("rejects minNodes < 1", () => {
    expect(() => parseTopology({
      mgmt: [{ id: "mgmt", region: "sfo3", nodeClass: "small", minNodes: 0 }],
      build: [{ id: "build", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [{ id: "rt", cellId: "c", region: "sfo3", nodeClass: "small", minNodes: 2 }],
    })).toThrow("'minNodes' must be >= 1");
  });

  it("rejects empty plane", () => {
    expect(() => parseTopology({
      mgmt: [],
      build: [{ id: "build", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [{ id: "rt", cellId: "c", region: "sfo3", nodeClass: "small", minNodes: 2 }],
    })).toThrow("must have at least 1 cluster");
  });

  it("preserves maxNodes", () => {
    const t = parseTopology({
      mgmt: [{ id: "mgmt", region: "sfo3", nodeClass: "small", minNodes: 3, maxNodes: 5 }],
      build: [{ id: "build", region: "sfo3", nodeClass: "medium", minNodes: 2 }],
      runtime: [{ id: "rt", cellId: "c", region: "sfo3", nodeClass: "small", minNodes: 2, maxNodes: 10 }],
    });
    expect(t.mgmt[0].maxNodes).toBe(5);
    expect(t.build[0].maxNodes).toBeUndefined();
    expect(t.runtime[0].maxNodes).toBe(10);
  });
});
