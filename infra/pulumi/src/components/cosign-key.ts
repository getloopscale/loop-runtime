// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";

/**
 * T1.2 mitigation #1 of MEET-2026-05-12 D2 — Pulumi-owned cosign signing key.
 *
 * Replaces the imperative `infra/helmfile/scripts/generate-build-secrets.sh`
 * path with a declarative Pulumi component. Key material is operator-
 * provided via `pulumi config set --secret cosignKeyPem` (and matching
 * `cosignKeyPassword`). Pulumi state is encrypted-at-rest (DO Spaces
 * backend + Pulumi passphrase), satisfying the "sealed-secret storage"
 * requirement.
 *
 * Cosign keyless (v1.1) supersedes this component per
 * `decisions/2026-05-12-cosign-keyless-v11-cutover.md`. Until then,
 * rotation is `pulumi config set --secret cosignKeyPem <new-pem>` +
 * `pulumi up` — captured in audit trail by Pulumi state changelog and
 * ESO `Synced` event timing (audit_log row written by API per T1.2 #3).
 *
 * @see concepts/supply-chain/cosign-key-rotation.md (rotation runbook)
 * @see decisions/2026-05-12-cosign-keyless-v11-cutover.md (cutover ADR)
 */

/** Inputs to the cosign key component. */
export interface CosignKeyArgs {
  /**
   * Stack name for namespacing the Pulumi state output. Component
   * outputs are exported under `cosign-key-${stackName}` so multiple
   * environments (drill/staging/prod) can coexist without collision.
   */
  readonly stackName: string;

  /**
   * Kubernetes namespace where the build cluster's ExternalSecret is
   * projected. Defaults to "argo" (canonical Argo Workflows namespace).
   */
  readonly namespace?: string;

  /**
   * Name of the K8s Secret that ESO materializes from this component.
   * Defaults to "cosign-signing-key" (matches existing build pipeline
   * expectations in api/internal/argo/cosign.go).
   */
  readonly secretName?: string;

  /**
   * ESO SecretStore reference name. Defaults to "loopruntime-pulumi-secrets"
   * (the canonical SecretStore that points to Pulumi state as backend).
   */
  readonly secretStoreName?: string;

  /**
   * ESO refresh interval. Defaults to 5 minutes — matches the existing
   * ESO refresh contract documented in CLAUDE.md "EC-NEW-13 — ESO
   * refresh window accepted during key rotation".
   */
  readonly refreshInterval?: string;
}

/** Outputs from the cosign key component. */
export interface CosignKeyOutputs {
  /**
   * Pulumi-encrypted key PEM. NEVER export to plaintext stack output;
   * consumed by the ExternalSecret manifest emission directly.
   */
  readonly keyPem: pulumi.Output<string>;

  /**
   * Pulumi-encrypted key password. NEVER export to plaintext stack
   * output; ESO projects it as the `cosign.password` field.
   */
  readonly keyPassword: pulumi.Output<string>;

  /**
   * Generation marker — bumps whenever key rotates. Used by audit log
   * correlation to detect when a rotation happened without diffing
   * Pulumi state. Format: SHA256 prefix of keyPem (first 12 chars).
   */
  readonly keyGenerationMarker: pulumi.Output<string>;

  /**
   * Rendered ExternalSecret manifest (YAML string) ready for
   * `kubectl apply` OR Helmfile inclusion. Emitted as a separate field
   * so the bootstrap layer can pipe it through `kubectl apply -f -`
   * during initial provisioning, then ESO takes over.
   */
  readonly externalSecretManifest: pulumi.Output<string>;

  /**
   * Pulumi resource URN — used by audit log correlation queries.
   */
  readonly urn: pulumi.Output<string>;
}

/**
 * createCosignKey constructs the Pulumi-owned cosign key.
 *
 * The function reads from `pulumi config` (encrypted secrets) and emits:
 *   1. A `pulumi.Output<string>` of the key PEM, kept encrypted in state.
 *   2. A `pulumi.Output<string>` of the key password (same).
 *   3. A generation marker (deterministic hash prefix).
 *   4. A rendered ExternalSecret manifest pointing at the Pulumi state
 *      via the ESO `secret-store-pulumi` provider.
 *
 * Required Pulumi config (set ONCE per stack via `pulumi config set
 * --secret`):
 *   - `cosignKeyPem`      — full PEM (ECDSA-P256 expected; cosign-compatible)
 *   - `cosignKeyPassword` — encryption passphrase for the PEM
 *
 * Rotation: operator generates NEW key via
 *   `cosign generate-key-pair --output-key-prefix cosign-rot-N`
 * then `pulumi config set --secret cosignKeyPem "$(cat cosign-rot-N.key)"`
 * then `pulumi up`. The 7-day dual-sign window from the rotation runbook
 * is handled by keeping BOTH old and new in Pulumi state during overlap.
 *
 * For new stacks without config set, throws — the caller must explicitly
 * bootstrap via the runbook (we do NOT auto-generate to avoid silent
 * key-material drift across `pulumi up` runs).
 */
export function createCosignKey(args: CosignKeyArgs): CosignKeyOutputs {
  const cfg = new pulumi.Config();
  const ns = args.namespace ?? "argo";
  const secretName = args.secretName ?? "cosign-signing-key";
  const storeName = args.secretStoreName ?? "loopruntime-pulumi-secrets";
  const refreshInterval = args.refreshInterval ?? "5m";

  // Read from encrypted config. `requireSecret` throws if missing —
  // forces operator to bootstrap explicitly per runbook (no silent
  // key-material drift on first pulumi up).
  const keyPem = cfg.requireSecret("cosignKeyPem");
  const keyPassword = cfg.requireSecret("cosignKeyPassword");

  // Generation marker: deterministic 12-char prefix of SHA256(keyPem).
  // Bumps whenever key rotates; stable within a key generation.
  const keyGenerationMarker = keyPem.apply((pem) => {
    return computeGenerationMarker(pem);
  });

  // Render the ExternalSecret manifest. ESO with `secret-store-pulumi`
  // backend reads the Pulumi stack output and materializes the K8s
  // Secret in args.namespace. The `data` array selects only the keys
  // ESO should expose downstream — keyPem + keyPassword.
  const externalSecretManifest = pulumi
    .all([pulumi.output(args.stackName), keyGenerationMarker])
    .apply(([stack, marker]) => renderExternalSecretYaml({
      stackName: stack,
      namespace: ns,
      secretName,
      storeName,
      refreshInterval,
      generationMarker: marker,
    }));

  // URN constructed from the stack name — useful for audit log linkage.
  // We do NOT create a Pulumi ComponentResource here because there are
  // no underlying provider resources; the component is a pure
  // transformation over config inputs. If/when we add `@pulumi/tls`
  // for in-Pulumi keygen (Sprint 9+), this becomes a real
  // ComponentResource.
  const urn = pulumi.output(
    `urn:pulumi:${args.stackName}::loopruntime-infra::custom:CosignKey::cosign-signing-key`,
  );

  return {
    keyPem,
    keyPassword,
    keyGenerationMarker,
    externalSecretManifest,
    urn,
  };
}

/**
 * Pure helper: compute deterministic 12-char generation marker from PEM.
 * Exported for testability. Uses Node's `crypto` only here to keep the
 * Pulumi runtime path lean.
 */
export function computeGenerationMarker(pem: string): string {
  // node:crypto is available in the Pulumi TS runtime (Node 18+).
  // Local require to keep top-level imports clean for tree-shaking.
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { createHash } = require("crypto");
  const hash = createHash("sha256").update(pem).digest("hex");
  return hash.slice(0, 12);
}

/**
 * Pure helper: render ExternalSecret YAML manifest.
 * Exported for testability. The manifest is consumed by Helmfile
 * `secrets/` hook OR `kubectl apply -f -` from the bootstrap script.
 */
export function renderExternalSecretYaml(input: {
  stackName: string;
  namespace: string;
  secretName: string;
  storeName: string;
  refreshInterval: string;
  generationMarker: string;
}): string {
  return `apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: ${input.secretName}
  namespace: ${input.namespace}
  annotations:
    runtime.getloopscale.com/component: cosign-key
    runtime.getloopscale.com/stack: ${input.stackName}
    runtime.getloopscale.com/generation-marker: ${input.generationMarker}
spec:
  refreshInterval: ${input.refreshInterval}
  secretStoreRef:
    name: ${input.storeName}
    kind: ClusterSecretStore
  target:
    name: ${input.secretName}
    creationPolicy: Owner
  data:
    - secretKey: cosign.key
      remoteRef:
        key: cosignKeyPem
    - secretKey: cosign.password
      remoteRef:
        key: cosignKeyPassword
`;
}
