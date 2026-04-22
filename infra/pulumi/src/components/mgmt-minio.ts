// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as k8s from "@pulumi/kubernetes";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// MinIO — S3-compatible artifact storage on mgmt cluster
// ---------------------------------------------------------------------------

export function installMinIO(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "minio",
    {
      name: "minio",
      chart: "minio",
      version: HELM_VERSIONS.minio,
      repositoryOpts: {
        repo: "https://charts.min.io/",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "minio",
        mode: "standalone",
        rootUser: "loopzero-admin",
        // Production overrides via: pulumi config set --secret minioRootPassword <password>
        rootPassword: config.minioRootPassword ?? "loopzero-minio-dev-2026",
        persistence: {
          enabled: true,
          size: "10Gi",
        },
        service: {
          type: "ClusterIP",
        },
        consoleService: {
          type: "ClusterIP",
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
        // Auto-create buckets for platform use.
        // The MinIO chart creates a post-install hook Job to create these buckets.
        // The hook Job needs to connect to MinIO pod on port 9000.
        buckets: [
          { name: "loopzero-artifacts", policy: "none" },
          { name: "loopzero-builds", policy: "none" },
        ],
        // Increase post-install hook timeout — on fresh clusters with slow storage
        // provisioning, the default 60s may not be enough for MinIO to start + create buckets.
        postJob: {
          activeDeadlineSeconds: 300,
        },
      },
    },
    {
      provider,
      dependsOn: [ns],
      // Increase timeout: MinIO make-bucket-job may take time on fresh clusters.
      customTimeouts: { create: "10m", update: "10m" },
    },
  );
}
