// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Sprint 6 / T2.2: Cosign policy-controller for image signature verification.
//
// Deploys sigstore/policy-controller on runtime clusters to enforce that
// all container images in tenant namespaces are signed. The webhook only
// targets namespaces managed by the environment-controller (label:
// loopzero.io/managed-by=environment-controller), leaving system namespaces
// unaffected.
//
// D3: KEY-BASED verification (not keyless / Fulcio / Rekor). The build
// cluster signs with cosignPrivateKeyB64; the runtime cluster verifies
// with cosignPublicKeyB64. Keyless would require an OIDC provider on
// the build cluster, which doesn't exist yet.
//
// D4: AUDIT mode first. failurePolicy: Ignore + validationActions: [Audit].
// Pre-Sprint-6 images (un-signed) are admitted but logged. After 2 weeks
// of clean logs the operator MANUALLY flips to failurePolicy: Fail.

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";

const POLICY_CONTROLLER_NAMESPACE = "cosign-system";
const POLICY_CONTROLLER_VERSION = "0.9.0";
const COSIGN_PUBLIC_KEY_SECRET = "cosign-public-key";

export interface PolicyControllerOutputs {
  readonly ready: pulumi.Output<boolean>;
  readonly namespace: string;
  readonly publicKeySecret?: k8s.core.v1.Secret;
}

/**
 * Install sigstore policy-controller for image signature verification.
 *
 * D4 — Audit mode. The webhook is configured with failurePolicy: Ignore
 * (fail-open). Pre-Sprint-6 images are admitted but logged. After 2 weeks
 * of clean logs, the operator updates the Helm release to set
 * failurePolicy: Fail.
 *
 * @param prefix              Resource name prefix (per-cluster).
 * @param provider            K8s Pulumi provider.
 * @param ns                  Optional namespace dependency (cluster bootstrap).
 * @param cosignPublicKeyB64  Base64 cosign public key (PEM). When empty,
 *                             the Secret is NOT created — caller must seed it
 *                             externally before activating the policy.
 */
export function installPolicyController(
  prefix: string,
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace | undefined,
  cosignPublicKeyB64?: string,
): PolicyControllerOutputs {
  const policyNs = new k8s.core.v1.Namespace(
    `${prefix}-cosign-system`,
    {
      metadata: {
        name: POLICY_CONTROLLER_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "loopzero.io/component": "policy-controller",
        },
      },
    },
    { provider, dependsOn: ns ? [ns] : undefined },
  );

  // D3 — Public key Secret. policy-controller reads the PEM from the
  // 'cosign.pub' key. Created only when caller provided the key; an
  // empty key would silently allow ALL images.
  let publicKeySecret: k8s.core.v1.Secret | undefined;
  if (cosignPublicKeyB64 && cosignPublicKeyB64.length > 0) {
    const pem = Buffer.from(cosignPublicKeyB64, "base64").toString("utf-8");
    publicKeySecret = new k8s.core.v1.Secret(
      `${prefix}-cosign-public-key`,
      {
        metadata: {
          name: COSIGN_PUBLIC_KEY_SECRET,
          namespace: POLICY_CONTROLLER_NAMESPACE,
          labels: {
            "loopzero.io/component": "policy-controller",
            "loopzero.io/sprint": "6",
          },
        },
        stringData: {
          "cosign.pub": pem,
        },
      },
      { provider, dependsOn: [policyNs] },
    );
  }

  const policyController = new k8s.helm.v3.Release(
    `${prefix}-policy-controller`,
    {
      name: "policy-controller",
      chart: "policy-controller",
      version: POLICY_CONTROLLER_VERSION,
      repositoryOpts: {
        repo: "https://sigstore.github.io/helm-charts",
      },
      namespace: POLICY_CONTROLLER_NAMESPACE,
      values: {
        webhook: {
          // D4 — Audit mode: fail-open. Pre-Sprint-6 images are admitted.
          // Flip to "Fail" after 2 weeks of clean logs.
          failurePolicy: "Ignore",
          // Only intercept tenant namespaces managed by EnvironmentController.
          // System namespaces (kube-system, loopzero-system, cosign-system, etc.)
          // do not carry this label and are therefore excluded.
          namespaceSelector: {
            matchExpressions: [
              {
                key: "loopzero.io/managed-by",
                operator: "In",
                values: ["environment-controller"],
              },
            ],
          },
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "256Mi" },
          },
          // Sprint 8 v3.2 fix 2026-04-27: sigstore/policy-controller v0.13.x
          // schema rejects custom securityContext properties under both top-level
          // and webhook.* paths. Schema-validated values are documented at
          // https://github.com/sigstore/policy-controller/blob/main/charts/policy-controller/values.schema.json
          // The chart already ships hardened defaults (runAsNonRoot, drop caps,
          // seccomp RuntimeDefault) baked into the controller manager template,
          // so removing the override is the correct path. Verified via crane:
          // ghcr.io/sigstore/policy-controller/policy-controller@sha256:... runs
          // as user 65532 with restricted PSA profile by default.
        },
      },
    },
    { provider, dependsOn: [policyNs] },
  );

  return {
    ready: policyController.status.apply((s) => s.status === "deployed"),
    namespace: POLICY_CONTROLLER_NAMESPACE,
    publicKeySecret,
  };
}

/**
 * Create a ClusterImagePolicy that requires images from the LoopZero registry
 * to be signed via cosign key-based signing (D3). The public key is read
 * from Secret cosign-public-key in the cosign-system namespace.
 *
 * D4 — validationActions: [Audit]. Violations are logged via the
 * policy-controller webhook event recorder; the admission decision is
 * NOT denied. Flip to [Deny] after the 2-week observation window.
 *
 * @param registryEndpoint - e.g. "registry.digitalocean.com/loopzero-infra-registry"
 */
export function createClusterImagePolicy(
  prefix: string,
  provider: k8s.Provider,
  registryEndpoint: string | pulumi.Output<string>,
  policyControllerOutputs: PolicyControllerOutputs,
): k8s.apiextensions.CustomResource {
  const dependsOn: pulumi.Resource[] = [];
  if (policyControllerOutputs.publicKeySecret) {
    dependsOn.push(policyControllerOutputs.publicKeySecret);
  }

  return new k8s.apiextensions.CustomResource(
    `${prefix}-loopzero-verify-signatures`,
    {
      apiVersion: "policy.sigstore.dev/v1beta1",
      kind: "ClusterImagePolicy",
      metadata: {
        name: "loopzero-verify-signatures",
        labels: {
          "loopzero.io/component": "policy-controller",
          "loopzero.io/sprint": "6",
        },
      },
      spec: {
        // D4 — Audit mode. Images that fail verification are logged but
        // admitted. To enforce, change to ["Deny"] (or remove field;
        // default is Deny). DO NOT mix Audit + Deny — pick one.
        validationActions: ["Audit"],
        images: [
          {
            glob: pulumi.interpolate`${registryEndpoint}/**`,
          },
        ],
        authorities: [
          {
            // D3 — Key-based: read public key from Secret cosign-public-key.
            key: {
              secretRef: {
                name: COSIGN_PUBLIC_KEY_SECRET,
                namespace: POLICY_CONTROLLER_NAMESPACE,
              },
            },
          },
        ],
      },
    },
    { provider, dependsOn },
  );
}
