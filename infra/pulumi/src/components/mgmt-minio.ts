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
        // Sprint 8 v3.2 / CHG-002a / T2.3 — MinIO distributed mode (4 drives EC:2).
        // Eliminates SPOF risk per ADR-0010 deliverable #5. Bootstrap had empty
        // buckets (verified via kubectl exec ls /export — only loopzero-artifacts +
        // loopzero-builds dirs, no objects), so the blue/green migration runbook
        // (docs/runbooks/minio-distributed-migration.md Phase A-F) reduces to
        // an in-place mode change. EC:2 (2 parity, 2 data) survives loss of 2
        // drives simultaneously — meets CHG-002c chaos recovery <2min target.
        mode: "distributed",
        replicas: 4,
        drivesPerNode: 1,
        rootUser: "loopzero-admin",
        // Production overrides via: pulumi config set --secret minioRootPassword <password>
        rootPassword: config.minioRootPassword ?? "loopzero-minio-dev-2026",
        persistence: {
          enabled: true,
          // Per-drive size; total raw = 4 × 10Gi = 40Gi, useful ~20Gi after EC:2.
          size: "10Gi",
        },
        service: {
          type: "ClusterIP",
        },
        consoleService: {
          type: "ClusterIP",
        },
        resources: {
          // Distributed mode has higher CPU overhead from erasure coding +
          // inter-node coordination. Bumped from standalone baseline.
          requests: { cpu: "200m", memory: "256Mi" },
          limits: { cpu: "1000m", memory: "1Gi" },
        },
        // Anti-affinity: enforce that the 4 pods land on distinct mgmt nodes
        // — pod kill on one node leaves 3/4 drives online (EC:2 still healthy).
        // Requires mgmtMaxNodes >= 4 (Sprint 8 v3.2 T0.2: mgmtMaxNodes=5 met).
        affinity: {
          podAntiAffinity: {
            requiredDuringSchedulingIgnoredDuringExecution: [
              {
                labelSelector: {
                  matchLabels: {
                    app: "minio",
                  },
                },
                topologyKey: "kubernetes.io/hostname",
              },
            ],
          },
        },
        // Auto-create buckets — same buckets as standalone for backward compat.
        buckets: [
          { name: "loopzero-artifacts", policy: "none" },
          { name: "loopzero-builds", policy: "none" },
        ],
        postJob: {
          activeDeadlineSeconds: 600,
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
