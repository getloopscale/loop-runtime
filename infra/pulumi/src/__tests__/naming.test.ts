// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";
import { resourceName, bucketName } from "../core/naming";

describe("resourceName", () => {
  it("generates name with project-env-region format", () => {
    const name = resourceName({ project: "loopruntime-infra", env: "dev", region: "nyc3" });
    expect(name).toBe("loopruntime-infra-dev-nyc3");
  });

  it("appends suffix when provided", () => {
    const name = resourceName({
      project: "loopruntime-infra",
      env: "prod",
      region: "sfo1",
      suffix: "mgmt",
    });
    expect(name).toBe("loopruntime-infra-prod-sfo1-mgmt");
  });

  it("omits suffix when undefined", () => {
    const name = resourceName({
      project: "loopruntime-infra",
      env: "staging",
      region: "ams3",
      suffix: undefined,
    });
    expect(name).toBe("loopruntime-infra-staging-ams3");
  });
});

describe("bucketName", () => {
  it("generates lowercase bucket-safe name", () => {
    const name = bucketName({
      project: "loopruntime-infra",
      env: "dev",
      region: "nyc3",
      suffix: "artifacts",
    });
    expect(name).toBe("loopruntime-infra-dev-nyc3-artifacts");
  });

  it("replaces underscores with hyphens", () => {
    const name = bucketName({
      project: "loopruntime_infra",
      env: "dev",
      region: "nyc3",
      suffix: "build_cache",
    });
    expect(name).toBe("loopruntime-infra-dev-nyc3-build-cache");
  });

  it("converts to lowercase", () => {
    const name = bucketName({
      project: "LoopRuntime-Infra",
      env: "dev",
      region: "NYC3",
    });
    expect(name).toBe("loopruntime-infra-dev-nyc3");
  });
});
