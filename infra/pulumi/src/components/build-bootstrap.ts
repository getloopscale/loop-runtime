// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Build cluster bootstrap orchestrator.
// Split modules: build-argo.ts, build-loki.ts, build-secrets.ts

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as command from "@pulumi/command";

import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { BuildBootstrapOutputs, installMetricsServer } from "./bootstrap-shared";
import { installBuildArgo } from "./build-argo";
import { installBuildLoki } from "./build-loki";
import { createBuildSecrets } from "./build-secrets";

// ---------------------------------------------------------------------------
// Build cluster bootstrap
// ---------------------------------------------------------------------------

export function bootstrapBuildCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): BuildBootstrapOutputs {
  const provider = new k8s.Provider("build-k8s", {
    kubeconfig: infra.buildCluster.kubeconfig,
  });

  installMetricsServer("build", provider);

  const ns = new k8s.core.v1.Namespace(
    "build-argo",
    {
      metadata: {
        name: "argo",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/cluster-role": "build",
          "pod-security.kubernetes.io/enforce": "privileged",
          "pod-security.kubernetes.io/warn": "baseline",
          "pod-security.kubernetes.io/audit": "baseline",
        },
      },
    },
    { provider },
  );

  // Secrets: cosign, S3, BuildKit registry, DOCR pull secret, SA
  createBuildSecrets(provider, ns, infra, config);

  // Argo Workflows: Helm, CWTs, RBAC, StorageClass, metrics NodePort
  const { argoWorkflows } = installBuildArgo(provider, ns, config);

  // Loki + Promtail: log aggregation with NodePort for cross-cluster access
  installBuildLoki(provider, infra, config);

  // Resolve build cluster node IP for cross-cluster endpoints
  const buildNodeIp = new command.local.Command(
    "resolve-build-node-ip",
    {
      create: pulumi.interpolate`kubectl --kubeconfig <(echo '${infra.buildCluster.kubeconfig}') get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'`,
      interpreter: ["/bin/bash", "-c"],
      triggers: [infra.buildCluster.endpoint],
    },
  );
  const lokiEndpoint = buildNodeIp.stdout.apply(
    (ip: string) => ip.trim() ? `http://${ip.trim()}:30100` : "http://pending-loki-ip:30100",
  );
  const argoMetricsEndpoint = buildNodeIp.stdout.apply(
    (ip: string) => ip.trim() ? `${ip.trim()}:30090` : "pending-argo-metrics-ip:30090",
  );

  return {
    argoWorkflowsReady: argoWorkflows.status.apply(
      (s) => s.status === "deployed",
    ),
    lokiEndpoint,
    argoMetricsEndpoint,
  };
}
