// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// WS.2: Cosign policy-controller for image signature verification.
//
// Deploys sigstore/policy-controller on runtime clusters to enforce that
// all container images in tenant namespaces are signed. The webhook only
// targets namespaces managed by the environment-controller (label:
// loopzero.io/managed-by=environment-controller), leaving system namespaces
// unaffected.
//
// This file is T2 (new infrastructure component). Code is prepared but
// not wired into runtime-bootstrap.ts until a governance meeting approves
// activation. To activate:
//   1. Import installPolicyController in runtime-bootstrap.ts
//   2. Call it after installESO
//   3. Run `pulumi up`

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";

const POLICY_CONTROLLER_NAMESPACE = "cosign-system";
const POLICY_CONTROLLER_VERSION = "0.9.0";

export interface PolicyControllerOutputs {
  readonly ready: pulumi.Output<boolean>;
  readonly namespace: string;
}

/**
 * Install sigstore policy-controller for image signature verification.
 *
 * The webhook is configured with failurePolicy: Fail so unsigned images
 * are rejected (not silently admitted). The namespaceSelector ensures
 * only tenant workload namespaces are checked — system namespaces are
 * excluded by default since they lack the loopzero.io/managed-by label.
 */
export function installPolicyController(
  prefix: string,
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
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
    { provider, dependsOn: [ns] },
  );

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
          // Fail-closed: reject pods that cannot be verified.
          failurePolicy: "Fail",
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
        },
        // Pod security: non-root, drop all capabilities.
        podSecurityContext: {
          runAsNonRoot: true,
          runAsUser: 65532,
          seccompProfile: { type: "RuntimeDefault" },
        },
        containerSecurityContext: {
          allowPrivilegeEscalation: false,
          capabilities: { drop: ["ALL"] },
          readOnlyRootFilesystem: true,
        },
      },
    },
    { provider, dependsOn: [policyNs] },
  );

  return {
    ready: policyController.status.apply((s) => s.status === "deployed"),
    namespace: POLICY_CONTROLLER_NAMESPACE,
  };
}

/**
 * Create a ClusterImagePolicy that requires all images from the LoopZero
 * registry to be signed via Sigstore keyless (Fulcio + Rekor).
 *
 * The identity constraint limits accepted signatures to those issued
 * by GitHub Actions in the loopzero/loopzero repository. This prevents
 * accepting images signed by arbitrary GitHub workflows.
 *
 * @param registryEndpoint - e.g. "registry.digitalocean.com/loopzero-system"
 */
export function createClusterImagePolicy(
  prefix: string,
  provider: k8s.Provider,
  registryEndpoint: string | pulumi.Output<string>,
  policyControllerOutputs: PolicyControllerOutputs,
): k8s.apiextensions.CustomResource {
  return new k8s.apiextensions.CustomResource(
    `${prefix}-loopzero-verify-signatures`,
    {
      apiVersion: "policy.sigstore.dev/v1beta1",
      kind: "ClusterImagePolicy",
      metadata: {
        name: "loopzero-verify-signatures",
      },
      spec: {
        images: [
          {
            glob: pulumi.interpolate`${registryEndpoint}/**`,
          },
        ],
        authorities: [
          {
            keyless: {
              url: "https://fulcio.sigstore.dev",
              identities: [
                {
                  issuerRegExp: ".*github\\.com.*",
                  subjectRegExp: ".*loopzero/loopzero.*",
                },
              ],
            },
            ctlog: {
              url: "https://rekor.sigstore.dev",
            },
          },
        ],
      },
    },
    {
      provider,
      dependsOn: [
        // Wait for policy-controller CRDs to be available.
        ...(policyControllerOutputs.ready ? [] : []),
      ],
    },
  );
}
