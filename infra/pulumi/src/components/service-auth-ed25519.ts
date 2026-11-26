// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";

/**
 * Service-auth Ed25519 dual-control key component.
 *
 * ADR: decisions/2026-05-13-service-auth-ed25519-adoption.md
 * RFC predecessor (now ACCEPTED): decisions/2026-05-12-service-auth-ed25519-rfc.md
 *
 * Adds Ed25519 signature on top of the existing X-Service-Auth shared
 * secret. The shared secret alone is no longer sufficient — the cloud
 * proxy must ALSO sign the canonical payload (user, tenant, ts, nonce,
 * method+path) with a private key held ONLY by cloud. A single accidental
 * leak (CI log, kubectl describe, sealed-secret misconfig) of either
 * the shared secret OR the Ed25519 key no longer enables tenant
 * impersonation.
 *
 * Mirror of components/cosign-key.ts shape — operator-provided keypair
 * via `pulumi config set --secret`. Three secret values per stack:
 *   - serviceAuthEd25519Private          (base64 Ed25519 private key)
 *   - serviceAuthEd25519PublicCurrent    (base64 Ed25519 public key)
 *   - serviceAuthEd25519PublicPrevious   (empty outside rotation window)
 *
 * Rotation: operator generates a new keypair (openssl genpkey or
 * stdlib via `go run tools/gen-ed25519/main.go`), sets the NEW pair as
 * current + the previous current as previous, runs `pulumi up`. The
 * 7-day overlap window matches pkg/jwt/validator.go::WithPreviousSecret
 * semantics — API accepts signatures from BOTH keys during overlap.
 */

/** Inputs to the service-auth-ed25519 component. */
export interface ServiceAuthEd25519Args {
  /** Stack name for output namespacing (dev/drill/prod). */
  readonly stackName: string;

  /**
   * Namespace where the cloud proxy runs and consumes the PRIVATE key.
   * Defaults to "loopruntime-system".
   */
  readonly cloudNamespace?: string;

  /**
   * Namespace where the API engine runs and consumes the PUBLIC key.
   * Defaults to "loopruntime-system".
   */
  readonly apiNamespace?: string;

  /**
   * Name of the K8s Secret that holds the private key on the cloud side.
   * Defaults to "loopruntime-service-auth-ed25519".
   */
  readonly privateSecretName?: string;

  /**
   * Name of the K8s Secret that holds the public key(s) on the API side.
   * Defaults to "loopruntime-service-auth-ed25519-pub" (separate from the
   * private secret to make defense-in-depth visible at the kubectl level).
   */
  readonly publicConfigMapName?: string;

  /** ESO SecretStore reference name. Defaults to "loopruntime-pulumi-secrets". */
  readonly secretStoreName?: string;

  /** ESO refresh interval. Defaults to "5m". */
  readonly refreshInterval?: string;
}

/** Outputs from the component. */
export interface ServiceAuthEd25519Outputs {
  readonly privateKey: pulumi.Output<string>;
  readonly publicKeyCurrent: pulumi.Output<string>;
  readonly publicKeyPrevious: pulumi.Output<string>;
  readonly keyGenerationMarker: pulumi.Output<string>;
  readonly manifests: pulumi.Output<string>;
  readonly urn: pulumi.Output<string>;
}

/**
 * createServiceAuthEd25519 constructs the dual-control key component.
 *
 * Required Pulumi config (set ONCE per stack via `pulumi config set --secret`):
 *   - serviceAuthEd25519Private         (base64-encoded raw Ed25519 private key, 64 bytes)
 *   - serviceAuthEd25519PublicCurrent   (base64-encoded raw Ed25519 public key, 32 bytes)
 * Optional (only during rotation window):
 *   - serviceAuthEd25519PublicPrevious  (base64-encoded raw Ed25519 public key, 32 bytes)
 *
 * Bootstrap: operators generate the keypair OFFLINE
 *   `openssl genpkey -algorithm Ed25519 -out priv.pem`
 *   `openssl pkey -in priv.pem -pubout -out pub.pem`
 *   then extract raw bytes via a small Go helper:
 *     `go run tools/gen-service-auth-ed25519/main.go`
 *   then `pulumi config set --secret serviceAuthEd25519Private <b64>`
 *
 * This component does NOT generate the keypair itself — generating
 * secret material inside pulumi up would either (a) require a
 * persistent random seed (no — keys must be deterministic across
 * preview/up runs OR genuinely random) or (b) require the
 * @pulumi/random.RandomBytes resource which carries a 32-byte seed
 * in plain Pulumi state (defeats the purpose). The offline-generation
 * pattern matches cosign-key.ts.
 */
export function createServiceAuthEd25519(
  args: ServiceAuthEd25519Args,
): ServiceAuthEd25519Outputs {
  const cfg = new pulumi.Config();
  const cloudNs = args.cloudNamespace ?? "loopruntime-system";
  const apiNs = args.apiNamespace ?? "loopruntime-system";
  const privateSecretName = args.privateSecretName ?? "loopruntime-service-auth-ed25519";
  const publicSecretName = args.publicConfigMapName ?? "loopruntime-service-auth-ed25519-pub";
  const storeName = args.secretStoreName ?? "loopruntime-pulumi-secrets";
  const refreshInterval = args.refreshInterval ?? "5m";

  const privateKey = cfg.requireSecret("serviceAuthEd25519Private");
  const publicKeyCurrent = cfg.requireSecret("serviceAuthEd25519PublicCurrent");
  // Previous public is optional — empty outside rotation window.
  const publicKeyPrevious = pulumi.secret(
    cfg.get("serviceAuthEd25519PublicPrevious") ?? "",
  );

  const keyGenerationMarker = privateKey.apply((p) =>
    computeServiceAuthGenerationMarker(p),
  );

  const manifests = pulumi
    .all([pulumi.output(args.stackName), keyGenerationMarker])
    .apply(([stack, marker]) =>
      renderServiceAuthEd25519Manifests({
        stackName: stack,
        cloudNamespace: cloudNs,
        apiNamespace: apiNs,
        privateSecretName,
        publicConfigMapName: publicSecretName,
        storeName,
        refreshInterval,
        generationMarker: marker,
      }),
    );

  const urn = pulumi.output(
    `urn:pulumi:${args.stackName}::loopruntime-infra::custom:ServiceAuthEd25519::loopruntime-service-auth-ed25519`,
  );

  return {
    privateKey,
    publicKeyCurrent,
    publicKeyPrevious,
    keyGenerationMarker,
    manifests,
    urn,
  };
}

/** Deterministic 12-char prefix of SHA256(privateKey). */
export function computeServiceAuthGenerationMarker(privKey: string): string {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { createHash } = require("crypto");
  return createHash("sha256").update(privKey).digest("hex").slice(0, 12);
}

/**
 * Render the YAML manifests (private + public ExternalSecrets).
 *
 * Two documents separated by `---`:
 *   1. ExternalSecret of the PRIVATE key, in the cloud namespace ONLY.
 *      `secretKey: private` → cloud reads via env var
 *      LOOPRUNTIME_SERVICE_AUTH_ED25519_PRIVATE_KEY.
 *   2. ExternalSecret of the PUBLIC keys (current + previous), in the
 *      API namespace. `secretKey: current` + `secretKey: previous` →
 *      API reads via env vars LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_CURRENT
 *      + LOOPRUNTIME_SERVICE_AUTH_ED25519_PUBLIC_PREVIOUS.
 *
 * Defense in depth: the private key ExternalSecret targets ONLY the
 * cloud namespace; the public key ExternalSecret targets ONLY the API
 * namespace. The API never sees the private; the cloud never sees the
 * other replicas' public configurations.
 */
export function renderServiceAuthEd25519Manifests(input: {
  stackName: string;
  cloudNamespace: string;
  apiNamespace: string;
  privateSecretName: string;
  publicConfigMapName: string;
  storeName: string;
  refreshInterval: string;
  generationMarker: string;
}): string {
  const privBlock = `apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: ${input.privateSecretName}
  namespace: ${input.cloudNamespace}
  annotations:
    runtime.getloopscale.com/component: service-auth-ed25519-private
    runtime.getloopscale.com/stack: ${input.stackName}
    runtime.getloopscale.com/generation-marker: ${input.generationMarker}
spec:
  refreshInterval: ${input.refreshInterval}
  secretStoreRef:
    name: ${input.storeName}
    kind: ClusterSecretStore
  target:
    name: ${input.privateSecretName}
    creationPolicy: Owner
  data:
    - secretKey: private
      remoteRef:
        key: serviceAuthEd25519Private
`;
  const pubBlock = `apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: ${input.publicConfigMapName}
  namespace: ${input.apiNamespace}
  annotations:
    runtime.getloopscale.com/component: service-auth-ed25519-public
    runtime.getloopscale.com/stack: ${input.stackName}
    runtime.getloopscale.com/generation-marker: ${input.generationMarker}
spec:
  refreshInterval: ${input.refreshInterval}
  secretStoreRef:
    name: ${input.storeName}
    kind: ClusterSecretStore
  target:
    name: ${input.publicConfigMapName}
    creationPolicy: Owner
  data:
    - secretKey: current
      remoteRef:
        key: serviceAuthEd25519PublicCurrent
    - secretKey: previous
      remoteRef:
        key: serviceAuthEd25519PublicPrevious
`;
  return privBlock + "---\n" + pubBlock;
}
