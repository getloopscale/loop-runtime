// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Build cluster: Argo Workflows Helm, CWTs, metrics NodePort, RBAC, StorageClass.

import * as k8s from "@pulumi/kubernetes";
import * as path from "path";
import { PlatformStackConfig } from "../config/stack";
import { HELM_VERSIONS } from "./bootstrap-shared";

export interface ArgoWorkflowsOutputs {
  readonly argoWorkflows: k8s.helm.v3.Release;
}

/**
 * Install build-cluster infrastructure: StorageClass, RBAC, Argo Workflows, CWTs, metrics NodePort.
 */
export function installBuildArgo(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): ArgoWorkflowsOutputs {
  // StorageClass "local-path" alias for DOKS
  new k8s.storage.v1.StorageClass(
    "build-local-path-sc",
    {
      metadata: { name: "local-path" },
      provisioner: "dobs.csi.digitalocean.com",
      reclaimPolicy: "Delete",
      volumeBindingMode: "Immediate",
      allowVolumeExpansion: true,
    },
    { provider, dependsOn: [ns] },
  );

  // ClusterRole for tenant-controller SA to manage build executor RBAC
  const buildRbacRole = new k8s.rbac.v1.ClusterRole(
    "build-loopzero-ctrl-rbac-admin",
    {
      metadata: { name: "loopzero-ctrl-build-rbac-admin" },
      rules: [
        { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "delete", "bind", "escalate"] },
        { apiGroups: [""], resources: ["namespaces", "serviceaccounts", "secrets"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: [""], resources: ["pods", "pods/log"], verbs: ["get", "list", "watch", "patch"] },
        { apiGroups: ["argoproj.io"], resources: ["workflowtaskresults", "workflows"], verbs: ["get", "list", "create", "update", "patch", "delete", "watch"] },
      ],
    },
    { provider },
  );
  new k8s.rbac.v1.ClusterRoleBinding(
    "build-loopzero-ctrl-rbac-admin",
    {
      metadata: { name: "loopzero-ctrl-build-rbac-admin" },
      roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "ClusterRole", name: "loopzero-ctrl-build-rbac-admin" },
      subjects: [{ kind: "ServiceAccount", name: "loopzero-ctrl-build-sa", namespace: "argo" }],
    },
    { provider, dependsOn: [buildRbacRole, ns] },
  );

  // Argo Workflows Helm release
  const argoWorkflows = new k8s.helm.v3.Release(
    "argo-workflows",
    {
      name: "argo-workflows",
      chart: "argo-workflows",
      version: HELM_VERSIONS.argoWorkflows,
      repositoryOpts: {
        repo: "https://argoproj.github.io/argo-helm",
      },
      namespace: "argo",
      createNamespace: false,
      values: {
        fullnameOverride: "argo-workflows",
        controller: {
          workflowWorkers: 32,
          podWorkers: 32,
          resources: {
            requests: { cpu: "500m", memory: "512Mi" },
            limits: { cpu: "2", memory: "1Gi" },
          },
          metricsConfig: {
            enabled: true,
            port: 9090,
            path: "/metrics",
          },
        },
        server: {
          enabled: true,
          service: { type: "ClusterIP" },
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
        workflow: {
          serviceAccount: { create: false, name: "loopzero-build-sa" },
          rbac: { create: true },
        },
      },
    },
    {
      provider,
      dependsOn: [ns],
      customTimeouts: { create: "10m", update: "10m" },
    },
  );

  // ClusterWorkflowTemplates
  new k8s.helm.v3.Release(
    "loopzero-build-cwts",
    {
      name: "loopzero-build-cwts",
      chart: path.resolve(__dirname, "../../../../infra/helm/argo-workflows"),
      namespace: "argo",
      createNamespace: false,
      values: {
        minio: { insecure: config.environment !== "prod" },
      },
    },
    { provider, dependsOn: [ns, argoWorkflows] },
  );

  // NodePort Service for Argo Workflows controller metrics
  new k8s.core.v1.Service(
    "argo-wf-metrics-nodeport",
    {
      metadata: {
        name: "argo-wf-metrics-nodeport",
        namespace: "argo",
        labels: {
          "app.kubernetes.io/name": "argo-workflows-workflow-controller",
          "app.kubernetes.io/component": "nodeport-metrics",
        },
      },
      spec: {
        type: "NodePort",
        ports: [
          { port: 9090, targetPort: 9090, nodePort: 30090, protocol: "TCP", name: "metrics" },
        ],
        selector: { "app.kubernetes.io/name": "argo-workflows-workflow-controller" },
      },
    },
    { provider, dependsOn: [argoWorkflows, ns] },
  );

  return { argoWorkflows };
}
