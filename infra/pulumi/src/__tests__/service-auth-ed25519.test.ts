// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { describe, expect, it } from "vitest";
import {
  computeServiceAuthGenerationMarker,
  renderServiceAuthEd25519Manifests,
} from "../components/service-auth-ed25519";

describe("service-auth-ed25519 component", () => {
  describe("computeServiceAuthGenerationMarker", () => {
    it("returns a 12-char deterministic prefix of SHA256(private-key)", () => {
      const fakePriv = "MC4CAQAwBQYDK2VwBCIEILongPrivateKeyBase64Padding=";
      const marker = computeServiceAuthGenerationMarker(fakePriv);
      expect(marker).toHaveLength(12);
      expect(marker).toMatch(/^[a-f0-9]{12}$/);
    });

    it("is deterministic — same key produces same marker", () => {
      const k = "stable-key-bytes";
      expect(computeServiceAuthGenerationMarker(k)).toBe(
        computeServiceAuthGenerationMarker(k),
      );
    });

    it("changes on key rotation — different key produces different marker", () => {
      const a = computeServiceAuthGenerationMarker("key-a");
      const b = computeServiceAuthGenerationMarker("key-b");
      expect(a).not.toBe(b);
    });
  });

  describe("renderServiceAuthEd25519Manifests", () => {
    const baseInput = {
      stackName: "dev",
      cloudNamespace: "loop-runtime-system",
      apiNamespace: "loop-runtime-system",
      privateSecretName: "loop-runtime-service-auth-ed25519",
      publicConfigMapName: "loop-runtime-service-auth-ed25519-pub",
      storeName: "loop-runtime-pulumi-secrets",
      refreshInterval: "5m",
      generationMarker: "abc123def456",
    };

    it("emits a private-key ExternalSecret targeting the cloud namespace", () => {
      const out = renderServiceAuthEd25519Manifests(baseInput);
      expect(out).toContain("kind: ExternalSecret");
      expect(out).toContain("name: loop-runtime-service-auth-ed25519");
      expect(out).toContain(`namespace: ${baseInput.cloudNamespace}`);
      expect(out).toContain("secretKey: private");
      expect(out).toContain("key: serviceAuthEd25519Private");
    });

    it("emits a public-key ExternalSecret targeting the API namespace", () => {
      const out = renderServiceAuthEd25519Manifests(baseInput);
      // The public key is materialized as a Secret too (ESO doesn't
      // write directly to ConfigMaps in v1beta1) and the API reads
      // the Secret content via env var or file-mount.
      expect(out).toContain("name: loop-runtime-service-auth-ed25519-pub");
      expect(out).toContain(`namespace: ${baseInput.apiNamespace}`);
      expect(out).toContain("secretKey: current");
      expect(out).toContain("key: serviceAuthEd25519PublicCurrent");
    });

    it("supports a previous-key entry for the dual-key rotation window", () => {
      const out = renderServiceAuthEd25519Manifests(baseInput);
      // Per RFC: API accepts current + previous public keys during
      // rotation. The manifest must expose BOTH slots; operator
      // populates `serviceAuthEd25519PublicPrevious` config during
      // rotation.
      expect(out).toContain("secretKey: previous");
      expect(out).toContain("key: serviceAuthEd25519PublicPrevious");
    });

    it("annotates with runtime.getloopscale.com/generation-marker so audit-log can correlate rotations", () => {
      const out = renderServiceAuthEd25519Manifests(baseInput);
      expect(out).toContain(
        `runtime.getloopscale.com/generation-marker: ${baseInput.generationMarker}`,
      );
    });

    it("uses the provided refreshInterval (operator-tunable)", () => {
      const customInterval = renderServiceAuthEd25519Manifests({
        ...baseInput,
        refreshInterval: "30s",
      });
      expect(customInterval).toContain("refreshInterval: 30s");
    });

    it("emits exactly two ExternalSecrets — one private (cloud) + one public (API)", () => {
      // Defense in depth: the rendered manifests must be exactly two
      // ExternalSecrets. The private key flows ONLY through the
      // `loop-runtime-service-auth-ed25519` ExternalSecret; the public keys
      // flow ONLY through `loop-runtime-service-auth-ed25519-pub`. Even if
      // the operator configures both ExternalSecrets in the same
      // namespace, the separation is at the K8s Secret name level —
      // RoleBindings on `loop-runtime-service-auth-ed25519` grant access to
      // the cloud SA only; RoleBindings on `-pub` grant API SA only.
      const out = renderServiceAuthEd25519Manifests(baseInput);
      const blocks = out.split("---").filter((b) => b.trim().length > 0);
      expect(blocks).toHaveLength(2);

      // Private block: exposes the `private` data key from
      // serviceAuthEd25519Private — does NOT include current/previous.
      const priv = blocks.find((b) =>
        b.includes(`name: ${baseInput.privateSecretName}\n`),
      );
      expect(priv).toBeDefined();
      expect(priv).toContain("secretKey: private");
      expect(priv).not.toContain("secretKey: current");
      expect(priv).not.toContain("secretKey: previous");

      // Public block: exposes current + previous — does NOT include private.
      const pub = blocks.find((b) =>
        b.includes(`name: ${baseInput.publicConfigMapName}\n`),
      );
      expect(pub).toBeDefined();
      expect(pub).not.toContain("secretKey: private");
      expect(pub).toContain("secretKey: current");
      expect(pub).toContain("secretKey: previous");
    });
  });
});
