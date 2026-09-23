// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { describe, it, expect } from "vitest";
import * as pulumi from "@pulumi/pulumi";
import { secretFromEnvOrConfig } from "../config/stack";

// loadStackConfig resolved nine credentials (Stripe, session, service auth,
// Resend, GitHub and Google OAuth) with nine copies of one branch: the
// environment variable wins when set, the Pulumi config secret otherwise. That
// rule is one helper now, and these specs are its three paths — plus the one
// the copies got wrong: a value taken from the environment was wrapped with
// pulumi.output(), not pulumi.secret(), so it reached the stack state as
// PLAINTEXT while the same value set through `pulumi config set --secret` was
// encrypted.

function unwrap<T>(o: pulumi.Output<T>): Promise<T> {
  return new Promise((resolve) => o.apply((v) => { resolve(v); return v; }));
}

describe("secretFromEnvOrConfig", () => {
  it("takes the environment variable when it is set", async () => {
    const got = secretFromEnvOrConfig("LOOPZERO_X", "x", {
      env: { LOOPZERO_X: "from-env" },
      getSecret: () => pulumi.secret("from-config"),
    });
    expect(got).toBeDefined();
    expect(await unwrap(got!)).toBe("from-env");
  });

  it("marks a value taken from the environment as a secret", async () => {
    const got = secretFromEnvOrConfig("LOOPZERO_X", "x", {
      env: { LOOPZERO_X: "from-env" },
      getSecret: () => undefined,
    });
    expect(await pulumi.isSecret(got!)).toBe(true);
  });

  it("falls back to the config secret when the environment variable is unset or empty", async () => {
    for (const env of [{}, { LOOPZERO_X: "" }]) {
      const got = secretFromEnvOrConfig("LOOPZERO_X", "x", {
        env,
        getSecret: (key) => (key === "x" ? pulumi.secret("from-config") : undefined),
      });
      expect(await unwrap(got!)).toBe("from-config");
    }
  });

  it("is undefined when neither source is set", () => {
    const got = secretFromEnvOrConfig("LOOPZERO_X", "x", { env: {}, getSecret: () => undefined });
    expect(got).toBeUndefined();
  });
});
