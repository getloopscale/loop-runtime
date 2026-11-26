// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";
import { standardTags, tagsToStringArray } from "../core/tags";

describe("standardTags", () => {
  it("returns required labels with managedBy pulumi", () => {
    const tags = standardTags("loop-runtime-infra", "dev");
    expect(tags).toEqual({
      project: "loop-runtime-infra",
      environment: "dev",
      managedBy: "pulumi",
    });
  });

  it("sets managedBy to pulumi for all environments", () => {
    expect(standardTags("x", "prod").managedBy).toBe("pulumi");
    expect(standardTags("x", "staging").managedBy).toBe("pulumi");
  });
});

describe("tagsToStringArray", () => {
  it("converts base labels to key:value string array", () => {
    const arr = tagsToStringArray({
      project: "loop-runtime-infra",
      environment: "dev",
      managedBy: "pulumi",
    });
    expect(arr).toEqual([
      "project:loop-runtime-infra",
      "env:dev",
      "managed-by:pulumi",
    ]);
  });

  it("includes owner when present", () => {
    const arr = tagsToStringArray({
      project: "loop-runtime",
      environment: "prod",
      managedBy: "pulumi",
      owner: "platform-team",
    });
    expect(arr).toContain("owner:platform-team");
  });

  it("includes costCenter when present", () => {
    const arr = tagsToStringArray({
      project: "loop-runtime",
      environment: "prod",
      managedBy: "pulumi",
      costCenter: "eng-123",
    });
    expect(arr).toContain("cost-center:eng-123");
  });

  it("excludes optional fields when absent", () => {
    const arr = tagsToStringArray({
      project: "loop-runtime",
      environment: "dev",
      managedBy: "pulumi",
    });
    expect(arr).toHaveLength(3);
    expect(arr.some((t) => t.startsWith("owner:"))).toBe(false);
    expect(arr.some((t) => t.startsWith("cost-center:"))).toBe(false);
  });
});
