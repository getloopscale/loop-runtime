// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";
import { resolveProvider } from "../components/infra";
import { InfraError } from "../core/errors";
import { CloudProvider } from "../core/types";

// This stack is AS-IS legacy retired at AM10; AWS goes to Terraform in
// ../loopruntime-infra-live (CLAUDE.md, infrastructure section). The factory used to
// return an AwsProviderFactory stub whose every method threw, so a stack
// configured with `provider: aws` got past config and failed resource by
// resource. The only provider this stack will ever have is DigitalOcean, and
// anything else is a configuration error at the one place provider names map
// to implementations.
describe("resolveProvider", () => {
  it("builds the DigitalOcean factory", () => {
    const factory = resolveProvider("digitalocean");
    expect(typeof factory.createKubernetesCluster).toBe("function");
  });

  it("rejects a provider this legacy stack does not implement with INVALID_CONFIG", () => {
    const unsupported = "aws" as unknown as CloudProvider;
    let caught: unknown;
    try {
      resolveProvider(unsupported);
    } catch (e) {
      caught = e;
    }
    expect(caught).toBeInstanceOf(InfraError);
    expect((caught as InfraError).code).toBe("INVALID_CONFIG");
    expect((caught as InfraError).message).toContain("aws");
  });
});
