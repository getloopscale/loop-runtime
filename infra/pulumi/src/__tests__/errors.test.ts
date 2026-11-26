// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, it, expect } from "vitest";
import { InfraError } from "../core/errors";

describe("InfraError", () => {
  it("formats message with error code prefix", () => {
    const err = new InfraError({
      message: "VPC creation failed",
      code: "RESOURCE_CREATION_FAILED",
    });
    expect(err.message).toContain("[RESOURCE_CREATION_FAILED]");
    expect(err.message).toContain("VPC creation failed");
  });

  it("includes context location in message", () => {
    const err = new InfraError({
      message: "unsupported",
      code: "PROVIDER_NOT_IMPLEMENTED",
      context: { provider: "aws", resource: "network", operation: "create" },
    });
    expect(err.message).toContain("(aws/network/create)");
  });

  it("omits location when no context", () => {
    const err = new InfraError({
      message: "bad config",
      code: "INVALID_CONFIG",
    });
    expect(err.message).toBe("[INVALID_CONFIG] bad config");
  });

  it("preserves error code and context as properties", () => {
    const err = new InfraError({
      message: "test",
      code: "BOOTSTRAP_FAILED",
      context: { provider: "digitalocean", resource: "cert-manager" },
    });
    expect(err.code).toBe("BOOTSTRAP_FAILED");
    expect(err.context.provider).toBe("digitalocean");
    expect(err.context.resource).toBe("cert-manager");
  });

  it("preserves cause error", () => {
    const cause = new Error("network timeout");
    const err = new InfraError({
      message: "failed to create cluster",
      code: "RESOURCE_CREATION_FAILED",
      cause,
    });
    expect(err.cause).toBe(cause);
  });

  it("produces structured log with all fields", () => {
    const cause = new Error("timeout");
    const err = new InfraError({
      message: "creation failed",
      code: "RESOURCE_CREATION_FAILED",
      context: { provider: "digitalocean", resource: "kubernetes" },
      cause,
    });

    const log = err.toStructuredLog();
    expect(log.error_code).toBe("RESOURCE_CREATION_FAILED");
    expect(log.provider).toBe("digitalocean");
    expect(log.resource).toBe("kubernetes");
    expect(log.cause).toBe("timeout");
  });

  it("sets name to InfraError", () => {
    const err = new InfraError({
      message: "test",
      code: "INVALID_CONFIG",
    });
    expect(err.name).toBe("InfraError");
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(InfraError);
  });
});
