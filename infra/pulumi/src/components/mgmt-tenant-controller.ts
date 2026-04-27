// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Extracted from mgmt-bootstrap.ts — Tenant Controller deployment.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE } from "./bootstrap-shared";
import { createScopedKubeconfig } from "./mgmt-scoped-kubeconfig";

export function installTenantController(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): k8s.apps.v1.Deployment {
  // CRDs
  const crds = new k8s.yaml.ConfigGroup(
    "loopzero-crds",
    { files: ["../../operators/config/crd/bases/*.yaml"] },
    { provider },
  );

  // ServiceAccount
  const sa = new k8s.core.v1.ServiceAccount(
    "tenant-controller-sa",
    {
      metadata: { name: "tenant-controller", namespace: PLATFORM_NAMESPACE },
      imagePullSecrets: [{ name: "loopzero-system" }],
    },
    { provider, dependsOn: [ns] },
  );

  // ClusterRole
  const role = new k8s.rbac.v1.ClusterRole(
    "tenant-controller-role",
    {
      metadata: { name: "tenant-controller" },
      rules: [
        {
          apiGroups: ["loopzero.io"],
          resources: [
            "tenants", "environments", "loopzeroapplications", "exposedroutes",
            "platformbootstraps", "gitrepositorybindings", "clustercredentials",
            "appsecrets", "previewenvironments", "reconciliationreports",
          ],
          verbs: ["get", "list", "watch", "create", "update", "patch", "delete"],
        },
        {
          apiGroups: ["loopzero.io"],
          resources: [
            "tenants/status", "tenants/finalizers", "environments/status", "environments/finalizers",
            "loopzeroapplications/status", "loopzeroapplications/finalizers", "exposedroutes/status", "exposedroutes/finalizers",
            "platformbootstraps/status", "platformbootstraps/finalizers", "gitrepositorybindings/status", "gitrepositorybindings/finalizers",
            "clustercredentials/status", "clustercredentials/finalizers", "appsecrets/status", "appsecrets/finalizers",
            "previewenvironments/status", "previewenvironments/finalizers", "reconciliationreports/status", "reconciliationreports/finalizers",
          ],
          verbs: ["get", "update", "patch"],
        },
        { apiGroups: [""], resources: ["namespaces"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
        { apiGroups: [""], resources: ["resourcequotas", "limitranges", "secrets", "serviceaccounts"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
        { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
        { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"], verbs: ["get", "list", "watch", "create", "update", "patch", "bind"] },
        { apiGroups: ["argoproj.io"], resources: ["appprojects", "applications"], verbs: ["get", "list", "watch", "create", "update", "patch", "delete"] },
        { apiGroups: ["coordination.k8s.io"], resources: ["leases"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
        { apiGroups: [""], resources: ["events"], verbs: ["create", "patch"] },
      ],
    },
    { provider },
  );

  const binding = new k8s.rbac.v1.ClusterRoleBinding(
    "tenant-controller-binding",
    {
      metadata: { name: "tenant-controller" },
      subjects: [{ kind: "ServiceAccount", name: "tenant-controller", namespace: PLATFORM_NAMESPACE }],
      roleRef: { kind: "ClusterRole", name: "tenant-controller", apiGroup: "rbac.authorization.k8s.io" },
    },
    { provider, dependsOn: [role, sa] },
  );

  // Scoped kubeconfigs
  const controllerBuildKubeconfig = createScopedKubeconfig("ctrl-build", infra.buildCluster, "argo", [
    { apiGroups: [""], resources: ["namespaces", "secrets", "serviceaccounts", "resourcequotas", "limitranges"], verbs: ["get", "list", "create", "update", "patch"] },
    { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch"] },
    { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "bind"] },
  ]);

  const controllerRuntimeKubeconfig = createScopedKubeconfig("ctrl-runtime", infra.runtimeCluster, PLATFORM_NAMESPACE, [
    { apiGroups: [""], resources: ["namespaces", "secrets", "serviceaccounts", "resourcequotas", "limitranges"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
    { apiGroups: [""], resources: ["pods"], verbs: ["get", "list", "delete"] },
    { apiGroups: [""], resources: ["namespaces"], verbs: ["delete"] },
    { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch"] },
    { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "bind"] },
    { apiGroups: ["scheduling.k8s.io"], resources: ["priorityclasses"], verbs: ["get", "list", "create", "update", "patch"] },
  ]);

  const controllerRuntime2Kubeconfig = createScopedKubeconfig("ctrl-runtime-2", infra.runtimeCluster2, PLATFORM_NAMESPACE, [
    { apiGroups: [""], resources: ["namespaces", "secrets", "serviceaccounts", "resourcequotas", "limitranges"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
    { apiGroups: [""], resources: ["pods"], verbs: ["get", "list", "delete"] },
    { apiGroups: [""], resources: ["namespaces"], verbs: ["delete"] },
    { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch"] },
    { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "bind"] },
    { apiGroups: ["scheduling.k8s.io"], resources: ["priorityclasses"], verbs: ["get", "list", "create", "update", "patch"] },
  ]);

  const controllerKubeconfig = new k8s.core.v1.Secret(
    "tenant-controller-kubeconfig",
    {
      metadata: { name: "tenant-controller-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: {
        "build-kubeconfig": controllerBuildKubeconfig,
        "runtime-kubeconfig": controllerRuntimeKubeconfig,
        "runtime-2-kubeconfig": controllerRuntime2Kubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // ClusterCredentials for multi-cell
  const runtimeCellKubeconfig = new k8s.core.v1.Secret(
    "runtime-cell-kubeconfig",
    {
      metadata: { name: "runtime-cell-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: { kubeconfig: controllerRuntimeKubeconfig },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apiextensions.CustomResource("runtime-cluster-credential", {
    apiVersion: "loopzero.io/v1", kind: "ClusterCredential",
    metadata: { name: `runtime-${config.region}-01`, namespace: PLATFORM_NAMESPACE, labels: { "loopzero.io/ingress-domain": config.domain } },
    spec: {
      clusterName: infra.runtimeCluster.clusterName, apiServer: infra.runtimeCluster.endpoint,
      secretRef: { name: "runtime-cell-kubeconfig", namespace: PLATFORM_NAMESPACE },
      scope: { argocd: true, operators: true }, tls: { insecure: false }, cellID: "cell-default",
    },
  }, { provider, dependsOn: [runtimeCellKubeconfig] });

  const runtime2CellKubeconfig = new k8s.core.v1.Secret(
    "runtime-2-cell-kubeconfig",
    {
      metadata: { name: "runtime-2-cell-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: { kubeconfig: controllerRuntime2Kubeconfig },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apiextensions.CustomResource("runtime-2-cluster-credential", {
    apiVersion: "loopzero.io/v1", kind: "ClusterCredential",
    metadata: { name: `runtime-${config.region}-02`, namespace: PLATFORM_NAMESPACE, labels: { "loopzero.io/ingress-domain": config.domain } },
    spec: {
      clusterName: infra.runtimeCluster2.clusterName, apiServer: infra.runtimeCluster2.endpoint,
      secretRef: { name: "runtime-2-cell-kubeconfig", namespace: PLATFORM_NAMESPACE },
      scope: { argocd: true, operators: true }, tls: { insecure: false }, cellID: "cell-runtime-2",
    },
  }, { provider, dependsOn: [runtime2CellKubeconfig] });

  // Controller credentials
  const controllerCredentials = new k8s.core.v1.Secret(
    "tenant-controller-credentials",
    {
      metadata: { name: "tenant-controller-credentials", namespace: PLATFORM_NAMESPACE },
      stringData: {
        SPACES_ACCESS_KEY_ID: config.spacesAccessKeyId,
        SPACES_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
        DIGITALOCEAN_TOKEN: config.digitaloceanToken,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Deployment
  const deploy = new k8s.apps.v1.Deployment(
    "tenant-controller",
    {
      metadata: {
        name: "tenant-controller", namespace: PLATFORM_NAMESPACE,
        labels: { app: "tenant-controller" },
        annotations: { "pulumi.com/skipAwait": "true" },
      },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "tenant-controller" } },
        template: {
          metadata: {
            labels: { app: "tenant-controller" },
            annotations: { "prometheus.io/scrape": "true", "prometheus.io/port": "8080" },
          },
          spec: {
            serviceAccountName: "tenant-controller",
            securityContext: { runAsNonRoot: true, runAsUser: 1000, fsGroup: 1000, seccompProfile: { type: "RuntimeDefault" } },
            imagePullSecrets: [{ name: "loopzero-system" }],
            volumes: [{ name: "kubeconfigs", secret: { secretName: "tenant-controller-kubeconfig" } }],
            containers: [{
              name: "controller",
              image: pulumi.interpolate`${infra.registryEndpoint}/loopzero-tenant-controller:${config.environment}`,
              imagePullPolicy: "Always",
              args: ["--leader-elect=true"],
              ports: [{ name: "metrics", containerPort: 8080 }, { name: "health", containerPort: 8081 }],
              volumeMounts: [{ name: "kubeconfigs", mountPath: "/etc/loopzero", readOnly: true }],
              env: [
                { name: "GITEA_URL", value: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000` },
                { name: "GITEA_TOKEN", valueFrom: { secretKeyRef: { name: "loopzero-platform-tokens", key: "GITEA_TOKEN" } } },
                { name: "BUILD_KUBECONFIG", value: "/etc/loopzero/build-kubeconfig" },
                { name: "RUNTIME_KUBECONFIG", value: "/etc/loopzero/runtime-kubeconfig" },
                { name: "WORKLOAD_CLUSTER_URL", value: pulumi.interpolate`${infra.runtimeCluster.endpoint}` },
                { name: "ARGOCD_NAMESPACE", value: PLATFORM_NAMESPACE },
                { name: "BASE_DOMAIN", value: config.domain },
                { name: "MINIO_ENDPOINT", value: infra.spacesEndpoint },
                { name: "SPACES_ACCESS_KEY_ID", valueFrom: { secretKeyRef: { name: "tenant-controller-credentials", key: "SPACES_ACCESS_KEY_ID" } } },
                { name: "SPACES_SECRET_ACCESS_KEY", valueFrom: { secretKeyRef: { name: "tenant-controller-credentials", key: "SPACES_SECRET_ACCESS_KEY" } } },
                { name: "DIGITALOCEAN_TOKEN", valueFrom: { secretKeyRef: { name: "tenant-controller-credentials", key: "DIGITALOCEAN_TOKEN" } } },
                // Sprint 8 v3.2 fix 2026-04-27: tenant-controller refused to start
                // with `invalid config: LOOPZERO_CLUSTER_ISSUER is required (prod:
                // letsencrypt-prod, dev: loopzero-ca-issuer)`. Mirror the value used
                // by loopzero-api (mgmt-api.ts:173) so cert-manager Certificate
                // generation aligns across the platform.
                { name: "LOOPZERO_CLUSTER_ISSUER", value: "letsencrypt-prod" },
              ],
              resources: { requests: { cpu: "100m", memory: "128Mi" }, limits: { cpu: "500m", memory: "256Mi" } },
              livenessProbe: { httpGet: { path: "/healthz", port: 8081 }, initialDelaySeconds: 15 },
              readinessProbe: { httpGet: { path: "/readyz", port: 8081 }, initialDelaySeconds: 5 },
              securityContext: { runAsNonRoot: true, allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: ["ALL"] } },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns, crds, binding, controllerKubeconfig, controllerCredentials] },
  );

  // Service
  new k8s.core.v1.Service("tenant-controller-svc", {
    metadata: { name: "tenant-controller", namespace: PLATFORM_NAMESPACE, labels: { app: "tenant-controller" } },
    spec: { selector: { app: "tenant-controller" }, ports: [{ name: "metrics", port: 8080, targetPort: 8080 }, { name: "health", port: 8081, targetPort: 8081 }] },
  }, { provider, dependsOn: [deploy] });

  // PDB
  new k8s.policy.v1.PodDisruptionBudget("tenant-controller-pdb", {
    metadata: { name: "tenant-controller", namespace: PLATFORM_NAMESPACE },
    spec: { minAvailable: 1, selector: { matchLabels: { app: "tenant-controller" } } },
  }, { provider });

  return deploy;
}
