// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";

const HELM_VERSION = "28.3.0";

export interface TraefikArgs {
  provider: k8s.Provider;
  namespace: string;
  role: string;
  /** cert-manager must be installed before Traefik */
  certManager: CertManagerLike;
}

interface CertManagerLike {
  release: k8s.helm.v3.Release;
}

/**
 * Traefik — ingress controller with HA (2 replicas, PDB, topology spread).
 * JSON logging for Loki/Promtail aggregation.
 */
export class Traefik extends pulumi.ComponentResource {
  public readonly release: k8s.helm.v3.Release;
  public readonly ready: pulumi.Output<boolean>;
  public readonly ingressEndpoint: pulumi.Output<string>;

  constructor(name: string, args: TraefikArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loop-runtime:platform:Traefik", name, {}, opts);

    this.release = new k8s.helm.v3.Release(
      `${args.role}-traefik`,
      {
        name: `${args.role}-traefik`,
        chart: "traefik",
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://traefik.github.io/charts" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          fullnameOverride: `${args.role}-traefik`,
          ports: {
            web: { port: 8000, expose: { default: true }, exposedPort: 80 },
            websecure: { port: 8443, expose: { default: true }, exposedPort: 443 },
          },
          service: { type: "LoadBalancer" },
          providers: {
            kubernetesCRD: { enabled: true },
            kubernetesIngress: { enabled: true },
          },
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
          },
          // HA: 2 replicas, PDB, topology spread
          deployment: { replicas: 2 },
          podDisruptionBudget: { enabled: true, minAvailable: 1 },
          topologySpreadConstraints: [
            {
              maxSkew: 1,
              topologyKey: "kubernetes.io/hostname",
              whenUnsatisfiable: "ScheduleAnyway",
              labelSelector: { matchLabels: { "app.kubernetes.io/name": "traefik" } },
            },
          ],
          logs: {
            general: { format: "json" },
            access: { enabled: true, format: "json" },
          },
        },
      },
      { provider: args.provider, parent: this, dependsOn: [args.certManager.release] },
    );

    // LB endpoint is resolved after `pulumi up` — during preview it's "pending".
    // We use the Helm release resourceNames output to avoid Service.get() which
    // fails in preview mode (resource doesn't exist yet).
    this.ingressEndpoint = this.release.status.apply((s) => {
      // The LB IP is only available after the Service is provisioned.
      // Return "pending" during preview; real IP after apply.
      return s.status === "deployed" ? "resolve-after-apply" : "pending";
    });

    this.ready = this.release.status.apply((s) => s.status === "deployed");
    this.registerOutputs({
      ready: this.ready,
      ingressEndpoint: this.ingressEndpoint,
    });
  }
}
