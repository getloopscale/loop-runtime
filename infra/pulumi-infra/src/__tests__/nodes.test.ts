// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";
import { resolveNodeSize, resolveDbSize } from "../providers/digitalocean/nodes";

describe("resolveNodeSize", () => {
  it("maps small to s-2vcpu-4gb", () => {
    expect(resolveNodeSize("small")).toBe("s-2vcpu-4gb");
  });

  it("maps medium to s-4vcpu-8gb-amd", () => {
    expect(resolveNodeSize("medium")).toBe("s-4vcpu-8gb-amd");
  });

  it("maps large to s-8vcpu-16gb", () => {
    expect(resolveNodeSize("large")).toBe("s-8vcpu-16gb");
  });

  it("maps xlarge to s-16vcpu-32gb", () => {
    expect(resolveNodeSize("xlarge")).toBe("s-16vcpu-32gb");
  });
});

describe("resolveDbSize", () => {
  it("maps small to db-s-1vcpu-2gb", () => {
    expect(resolveDbSize("small")).toBe("db-s-1vcpu-2gb");
  });

  it("maps medium to db-s-2vcpu-4gb", () => {
    expect(resolveDbSize("medium")).toBe("db-s-2vcpu-4gb");
  });

  it("maps large to db-s-4vcpu-8gb", () => {
    expect(resolveDbSize("large")).toBe("db-s-4vcpu-8gb");
  });

  it("maps xlarge to db-s-8vcpu-16gb", () => {
    expect(resolveDbSize("xlarge")).toBe("db-s-8vcpu-16gb");
  });
});
