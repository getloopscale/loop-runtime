// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// T1.2 mitigation #1 of MEET-2026-05-12 — unit tests for the Pulumi-owned
// cosign key component. Pure helpers (computeGenerationMarker, render
// ExternalSecretYaml) are tested directly; the Pulumi createCosignKey
// constructor is tested via a config-injection harness because Pulumi
// runtime config (`new pulumi.Config()`) reads from a thread-local
// stack — we set the env vars and let the unit-test runtime resolve.

import { describe, it, expect, beforeEach } from "vitest";
import * as pulumi from "@pulumi/pulumi";

import {
  computeGenerationMarker,
  renderExternalSecretYaml,
  createCosignKey,
} from "../components/cosign-key";

// Pulumi config is read at module-load time from the runtime singleton.
// Unit tests use `pulumi.runtime.setAllConfig` (a public test API) to
// seed values BEFORE the component reads them. The "test-project" name
// must match what `new pulumi.Config()` derives — defaults to "project"
// unless PULUMI_NODEJS_PROJECT is set.
const PROJECT = "project"; // matches Pulumi default in test runtime

function setPulumiConfig(values: Record<string, string>): void {
  const obj: Record<string, string> = {};
  for (const [k, v] of Object.entries(values)) {
    obj[`${PROJECT}:${k}`] = v;
  }
  pulumi.runtime.setAllConfig(obj, /* secretKeys */ Object.keys(obj));
}

function clearPulumiConfig(): void {
  pulumi.runtime.setAllConfig({}, []);
}

// Sample PEM used for deterministic marker tests. Not a real key — the
// helper only computes SHA256 prefix, so any string works.
const SAMPLE_PEM = "REDACTED_SECRET_002";

// -----------------------------------------------------------------------------
// computeGenerationMarker — pure helper
// -----------------------------------------------------------------------------

describe("computeGenerationMarker", () => {
  it("returns a 12-character hex prefix", () => {
    const marker = computeGenerationMarker(SAMPLE_PEM);
    expect(marker).toHaveLength(12);
    expect(marker).toMatch(/^[0-9a-f]+$/);
  });

  it("is deterministic for the same input", () => {
    const a = computeGenerationMarker(SAMPLE_PEM);
    const b = computeGenerationMarker(SAMPLE_PEM);
    expect(a).toBe(b);
  });

  it("differs for different inputs (rotation signal)", () => {
    const a = computeGenerationMarker(SAMPLE_PEM);
    const b = computeGenerationMarker(SAMPLE_PEM + "X");
    expect(a).not.toBe(b);
  });

  it("handles empty input without throwing", () => {
    const marker = computeGenerationMarker("");
    expect(marker).toHaveLength(12);
  });
});

// -----------------------------------------------------------------------------
// renderExternalSecretYaml — pure helper
// -----------------------------------------------------------------------------

describe("renderExternalSecretYaml", () => {
  it("renders valid ExternalSecret v1beta1 manifest", () => {
    const yaml = renderExternalSecretYaml({
      stackName: "production",
      namespace: "argo",
      secretName: "cosign-signing-key",
      storeName: "loop-runtime-pulumi-secrets",
      refreshInterval: "5m",
      generationMarker: "abc123def456",
    });

    expect(yaml).toContain("apiVersion: external-secrets.io/v1beta1");
    expect(yaml).toContain("kind: ExternalSecret");
    expect(yaml).toContain("name: cosign-signing-key");
    expect(yaml).toContain("namespace: argo");
    expect(yaml).toContain("refreshInterval: 5m");
    expect(yaml).toContain("name: loop-runtime-pulumi-secrets");
    expect(yaml).toContain("kind: ClusterSecretStore");
  });

  it("includes the generation marker as annotation (audit correlation)", () => {
    const yaml = renderExternalSecretYaml({
      stackName: "drill",
      namespace: "argo",
      secretName: "cosign-signing-key",
      storeName: "loop-runtime-pulumi-secrets",
      refreshInterval: "10m",
      generationMarker: "deadbeef0042",
    });
    expect(yaml).toContain("runtime.getloopscale.com/generation-marker: deadbeef0042");
    expect(yaml).toContain("runtime.getloopscale.com/stack: drill");
  });

  it("emits both cosign.key and cosign.password data keys (required by build pipeline)", () => {
    const yaml = renderExternalSecretYaml({
      stackName: "x",
      namespace: "argo",
      secretName: "cosign-signing-key",
      storeName: "loop-runtime-pulumi-secrets",
      refreshInterval: "5m",
      generationMarker: "x",
    });
    expect(yaml).toContain("secretKey: cosign.key");
    expect(yaml).toContain("remoteRef:\n        key: cosignKeyPem");
    expect(yaml).toContain("secretKey: cosign.password");
    expect(yaml).toContain("remoteRef:\n        key: cosignKeyPassword");
  });

  it("sets creationPolicy: Owner (ESO manages full lifecycle of Secret)", () => {
    const yaml = renderExternalSecretYaml({
      stackName: "x",
      namespace: "argo",
      secretName: "cosign-signing-key",
      storeName: "loop-runtime-pulumi-secrets",
      refreshInterval: "5m",
      generationMarker: "x",
    });
    expect(yaml).toContain("creationPolicy: Owner");
  });
});

// -----------------------------------------------------------------------------
// createCosignKey — integrates with Pulumi config
// -----------------------------------------------------------------------------

describe("createCosignKey", () => {
  beforeEach(() => {
    clearPulumiConfig();
  });

  it("requires cosignKeyPem in stack config (throws when missing)", () => {
    setPulumiConfig({}); // empty
    expect(() => createCosignKey({ stackName: "test" })).toThrow();
  });

  it("requires cosignKeyPassword in stack config (throws when missing)", () => {
    setPulumiConfig({ cosignKeyPem: SAMPLE_PEM }); // password missing
    expect(() => createCosignKey({ stackName: "test" })).toThrow();
  });

  it("emits all 5 outputs when config is complete", async () => {
    setPulumiConfig({
      cosignKeyPem: SAMPLE_PEM,
      cosignKeyPassword: "test-password-123",
    });

    const outputs = createCosignKey({ stackName: "test" });

    // Verify all outputs are present.
    expect(outputs.keyPem).toBeDefined();
    expect(outputs.keyPassword).toBeDefined();
    expect(outputs.keyGenerationMarker).toBeDefined();
    expect(outputs.externalSecretManifest).toBeDefined();
    expect(outputs.urn).toBeDefined();

    // URN bears stack name.
    const urn = await unwrapOutput(outputs.urn);
    expect(urn).toContain("test");
    expect(urn).toContain("CosignKey");
  });

  it("computes generation marker matching the helper output", async () => {
    setPulumiConfig({
      cosignKeyPem: SAMPLE_PEM,
      cosignKeyPassword: "p",
    });
    const outputs = createCosignKey({ stackName: "test" });
    const marker = await unwrapOutput(outputs.keyGenerationMarker);
    expect(marker).toBe(computeGenerationMarker(SAMPLE_PEM));
  });

  it("renders manifest referencing the namespace + secret-store defaults", async () => {
    setPulumiConfig({
      cosignKeyPem: SAMPLE_PEM,
      cosignKeyPassword: "p",
    });
    const outputs = createCosignKey({ stackName: "test" });
    const manifest = await unwrapOutput(outputs.externalSecretManifest);
    expect(manifest).toContain("namespace: argo");
    expect(manifest).toContain("name: loop-runtime-pulumi-secrets");
    expect(manifest).toContain("name: cosign-signing-key");
  });

  it("honors namespace override", async () => {
    setPulumiConfig({
      cosignKeyPem: SAMPLE_PEM,
      cosignKeyPassword: "p",
    });
    const outputs = createCosignKey({
      stackName: "test",
      namespace: "argo-staging",
    });
    const manifest = await unwrapOutput(outputs.externalSecretManifest);
    expect(manifest).toContain("namespace: argo-staging");
  });

  it("honors secretName override (rotation drill stack uses cosign-drill)", async () => {
    setPulumiConfig({
      cosignKeyPem: SAMPLE_PEM,
      cosignKeyPassword: "p",
    });
    const outputs = createCosignKey({
      stackName: "drill",
      secretName: "cosign-drill",
    });
    const manifest = await unwrapOutput(outputs.externalSecretManifest);
    expect(manifest).toContain("name: cosign-drill");
  });
});

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

/** Resolve a pulumi.Output<T> to its value in unit tests. */
function unwrapOutput<T>(output: pulumi.Output<T>): Promise<T> {
  return new Promise((resolve) => {
    output.apply((value) => {
      resolve(value);
      return value;
    });
  });
}
