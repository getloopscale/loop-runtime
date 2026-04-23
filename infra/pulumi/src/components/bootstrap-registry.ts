// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Container registry pull secrets + metrics-server.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as digitalocean from "@pulumi/digitalocean";
import { HELM_VERSIONS } from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Container registry pull secret
// ---------------------------------------------------------------------------

/**
 * Creates a `kubernetes.io/dockerconfigjson` Secret authorizing pulls from
 * DigitalOcean Container Registry (DOCR).
 *
 * Uses `digitalocean.getContainerRegistryDockerCredentials` to issue long-lived
 * read credentials and persists them as a dockerconfigjson Secret.
 *
 * Use `write: true` only for namespaces whose pods need to *push* images.
 */
export function createDockerRegistryPullSecret(
  resourceName: string,
  registryName: pulumi.Input<string>,
  namespace: pulumi.Input<string>,
  provider: k8s.Provider,
  opts: { write?: boolean; dependsOn?: pulumi.Resource[] } = {},
): k8s.core.v1.Secret {
  const creds = new digitalocean.ContainerRegistryDockerCredentials(
    `${resourceName}-creds`,
    {
      registryName,
      write: opts.write ?? false,
      expirySeconds: 0,
    },
  );

  return new k8s.core.v1.Secret(
    resourceName,
    {
      metadata: {
        name: "loopzero-infra-registry",
        namespace,
        annotations: { "pulumi.com/patchForce": "true" },
      },
      type: "kubernetes.io/dockerconfigjson",
      stringData: {
        ".dockerconfigjson": creds.dockerCredentials,
      },
    },
    { provider, dependsOn: opts.dependsOn },
  );
}

// ---------------------------------------------------------------------------
// metrics-server — MEET-2026-04-09-045: enables kubectl top + HPA
// ---------------------------------------------------------------------------

/**
 * Installs metrics-server on a cluster. DOKS does not always pre-install it.
 * Required for `kubectl top`, HPA, and resource pressure visibility.
 */
export function installMetricsServer(
  role: string,
  provider: k8s.Provider,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    `${role}-metrics-server`,
    {
      name: "metrics-server",
      chart: "metrics-server",
      version: HELM_VERSIONS.metricsServer,
      repositoryOpts: {
        repo: "https://kubernetes-sigs.github.io/metrics-server/",
      },
      namespace: "kube-system",
      createNamespace: false,
      values: {
        args: ["--kubelet-insecure-tls"],
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "100m", memory: "128Mi" },
        },
      },
    },
    { provider },
  );
}
