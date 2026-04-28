// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Build cluster: Argo Workflows Helm, CWTs, metrics NodePort, RBAC, StorageClass.

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import * as path from "path";
import { PlatformStackConfig } from "../config/stack";
import { HELM_VERSIONS } from "./bootstrap-shared";

export interface ArgoWorkflowsOutputs {
  readonly argoWorkflows: k8s.helm.v3.Release;
}

// CHG-002b L3 fix: install Rancher local-path-provisioner via the upstream
// manifest (no Helm chart is published). The StorageClass `local-path` below
// depends on this — it provides hostPath-backed PVCs without DO API calls,
// avoiding the 429 rate-limit cascade observed at 30 concurrent builds.
//
// The manifest creates: namespace local-path-storage, ServiceAccount,
// ClusterRole/Binding, ConfigMap, Deployment, and a default StorageClass
// `local-path` — we DELETE the default SC and recreate it explicitly
// (immutability of provisioner field forces this dance).
function installLocalPathProvisioner(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.yaml.ConfigFile {
  return new k8s.yaml.ConfigFile(
    "local-path-provisioner",
    {
      file: "https://raw.githubusercontent.com/rancher/local-path-provisioner/v0.0.31/deploy/local-path-storage.yaml",
      // The upstream manifest creates a `local-path` StorageClass; we drop it
      // via transform so our explicit StorageClass below is authoritative.
      transformations: [
        (obj: any, opts: pulumi.CustomResourceOptions) => {
          if (obj?.kind === "StorageClass" && obj?.metadata?.name === "local-path") {
            obj.metadata.name = "local-path-upstream-discard";
          }
        },
      ],
    },
    { provider, dependsOn: [ns] },
  );
}

/**
 * Install build-cluster infrastructure: StorageClass, RBAC, Argo Workflows, CWTs, metrics NodePort.
 */
export function installBuildArgo(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): ArgoWorkflowsOutputs {
  // StorageClass "local-path" — Rancher local-path-provisioner (hostPath).
  //
  // CHG-002b L3 fix 2026-04-27: prior config used `dobs.csi.digitalocean.com`
  // as a "local-path alias for DOKS". Under L3 load (30 concurrent builds),
  // each workspace PVC triggered a DO Block Storage volume create, hitting
  // DO API rate limit (HTTP 429) and stalling all builds.
  //
  // workflow_builder.go:217 explicitly requires Rancher local-path-provisioner
  // to be deployed on the build cluster. The provisioner Helm/manifest is
  // installed before this StorageClass; see runbook
  // docs/runbooks/build-cluster-local-path.md for bootstrap procedure.
  //
  // WaitForFirstConsumer: PVC binding deferred until pod scheduled, ensuring
  // the volume lands on the same node as the pod (matches podAffinity rule
  // in workflow_builder.go that co-locates DAG steps).
  const localPathProvisioner = installLocalPathProvisioner(provider, ns);
  new k8s.storage.v1.StorageClass(
    "build-local-path-sc",
    {
      metadata: { name: "local-path" },
      provisioner: "rancher.io/local-path",
      reclaimPolicy: "Delete",
      volumeBindingMode: "WaitForFirstConsumer",
      allowVolumeExpansion: false,
    },
    { provider, dependsOn: [ns, localPathProvisioner] },
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
