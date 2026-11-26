// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";

const HELM_VERSION = "v1.14.5";

export interface CertManagerArgs {
  /** K8s provider for the target cluster */
  provider: k8s.Provider;
  /** Namespace to install into */
  namespace: string;
  /** Unique prefix for resource naming (e.g., "mgmt", "runtime") */
  role: string;
}

/**
 * cert-manager — TLS certificate management.
 * Installs CRDs, webhook, and cainjector.
 * Required before Traefik and ClusterIssuers.
 */
export class CertManager extends pulumi.ComponentResource {
  public readonly release: k8s.helm.v3.Release;
  public readonly ready: pulumi.Output<boolean>;

  constructor(name: string, args: CertManagerArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loop-runtime:platform:CertManager", name, {}, opts);

    this.release = new k8s.helm.v3.Release(
      `${args.role}-cert-manager`,
      {
        name: "cert-manager",
        chart: "cert-manager",
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://charts.jetstack.io" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          installCRDs: true,
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
          webhook: {
            resources: {
              requests: { cpu: "25m", memory: "32Mi" },
              limits: { cpu: "100m", memory: "64Mi" },
            },
          },
          cainjector: {
            resources: {
              requests: { cpu: "25m", memory: "64Mi" },
              limits: { cpu: "100m", memory: "128Mi" },
            },
          },
        },
      },
      { provider: args.provider, parent: this },
    );

    this.ready = this.release.status.apply((s) => s.status === "deployed");
    this.registerOutputs({ ready: this.ready });
  }
}
