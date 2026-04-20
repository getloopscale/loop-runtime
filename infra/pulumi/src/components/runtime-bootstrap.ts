// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import * as command from "@pulumi/command";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import {
  PLATFORM_NAMESPACE,
  HELM_VERSIONS,
  RuntimeBootstrapOutputs,
  createPlatformNamespace,
  installCertManager,
  installTraefik,
  lookupLoadBalancerEndpoint,
  createClusterIssuer,
  installMetricsServer,
  createDockerRegistryPullSecret,
} from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Runtime cluster bootstrap
// ---------------------------------------------------------------------------

export function bootstrapRuntimeCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): RuntimeBootstrapOutputs {
  const provider = new k8s.Provider("runtime-k8s", {
    kubeconfig: infra.runtimeCluster.kubeconfig,
  });

  const ns = createPlatformNamespace("runtime", config, provider);

  // MEET-2026-04-09-045: metrics-server for kubectl top + HPA
  installMetricsServer("runtime", provider);

  // cert-manager — needed for Let's Encrypt TLS on tenant app IngressRoutes
  const certManager = installCertManager("runtime", provider, ns);

  // ClusterIssuer — Let's Encrypt production certs for tenant apps
  // Webhook readiness gate via Deployment.get() in createClusterIssuer (MEET-2026-04-01-025 #5)
  const runtimeClusterIssuer = createClusterIssuer(provider, certManager, config, "runtime");

  // Traefik — ingress for user apps, depends on cert-manager for TLS
  const traefik = installTraefik("runtime", provider, ns, certManager);

  const ingressEndpoint = lookupLoadBalancerEndpoint(
    "runtime",
    provider,
    traefik,
  );

  // RBAC for EnvironmentController to manage tenant namespaces on runtime.
  // The controller runs on mgmt cluster and creates Roles/RoleBindings/NetworkPolicies
  // on the runtime cluster via loopzero-ctrl-runtime-sa ServiceAccount.
  const runtimeRbacRole = new k8s.rbac.v1.ClusterRole(
    "runtime-loopzero-ctrl-rbac-admin",
    {
      metadata: { name: "loopzero-ctrl-runtime-rbac-admin" },
      rules: [
        { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "delete", "bind", "escalate"] },
        { apiGroups: [""], resources: ["namespaces", "serviceaccounts", "secrets", "configmaps", "services"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["apps"], resources: ["deployments"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: [""], resources: ["resourcequotas", "limitranges"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["policy"], resources: ["poddisruptionbudgets"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
      ],
    },
    { provider },
  );
  new k8s.rbac.v1.ClusterRoleBinding(
    "runtime-loopzero-ctrl-rbac-admin",
    {
      metadata: { name: "loopzero-ctrl-runtime-rbac-admin" },
      roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "ClusterRole", name: "loopzero-ctrl-runtime-rbac-admin" },
      subjects: [{ kind: "ServiceAccount", name: "loopzero-ctrl-runtime-sa", namespace: PLATFORM_NAMESPACE }],
    },
    { provider, dependsOn: [runtimeRbacRole, ns] },
  );

  // DOCR pull secret — runtime cluster needs to pull tenant app images from DOCR.
  // The TenantController creates per-namespace copies, but this ensures the
  // Secret exists at cluster level for propagation.
  createDockerRegistryPullSecret(
    "runtime-docr-pull-secret",
    infra.registryName,
    PLATFORM_NAMESPACE,
    provider,
    { write: false, dependsOn: [ns] },
  );

  // External Secrets Operator — syncs secrets from mgmt cluster to runtime namespaces.
  // ESO runs on runtime (workload) cluster and reads from mgmt via kubeconfig.
  installESO(provider, infra);

  // MEET-2026-04-04-030 Phase 3.1: RBAC for loopzero-api to create ExternalSecret CRs
  // on the runtime cluster. The SecretK8sManager (api/internal/k8s/secret_k8s_manager.go)
  // creates ExternalSecret CRs in tenant namespaces when `loopzero secret set` is called.
  // The scoped kubeconfig SA (loopzero-runtime-sa) needs permissions for ESO CRDs.
  const esoWriterRole = new k8s.rbac.v1.ClusterRole(
    "loopzero-eso-writer",
    {
      metadata: { name: "loopzero-eso-writer" },
      rules: [
        {
          apiGroups: ["external-secrets.io"],
          resources: ["externalsecrets"],
          verbs: ["get", "list", "create", "update", "patch", "delete"],
        },
        // Also need K8s Secret read to verify sync status
        {
          apiGroups: [""],
          resources: ["secrets"],
          verbs: ["get", "list", "create", "update", "patch", "delete"],
        },
      ],
    },
    { provider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    "loopzero-eso-writer",
    {
      metadata: { name: "loopzero-eso-writer" },
      subjects: [
        { kind: "ServiceAccount", name: "loopzero-runtime-sa", namespace: PLATFORM_NAMESPACE },
      ],
      roleRef: { kind: "ClusterRole", name: "loopzero-eso-writer", apiGroup: "rbac.authorization.k8s.io" },
    },
    { provider, dependsOn: [esoWriterRole] },
  );

  // ---------------------------------------------------------------------------
  // Monitoring namespace — shared by Promtail, kube-state-metrics
  // Privileged PSA: Promtail needs hostPath volumes for log collection.
  // ---------------------------------------------------------------------------
  const runtimeMonitoringNs = new k8s.core.v1.Namespace(
    "runtime-monitoring",
    {
      metadata: {
        name: "monitoring",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "pod-security.kubernetes.io/enforce": "privileged",
          "pod-security.kubernetes.io/warn": "restricted",
          "pod-security.kubernetes.io/audit": "restricted",
        },
      },
    },
    { provider },
  );

  // Promtail — ships runtime pod logs to centralized Loki on build cluster.
  // Uses lokiPushUrl (NodePort on build cluster) for cross-cluster log shipping.
  // skipAwait + customTimeouts mitigate Pulumi K8s provider gRPC issues
  // during node pool resize operations.
  if (config.lokiPushUrl) {
    new k8s.helm.v3.Release(
      "runtime-promtail",
      {
        name: "promtail",
        chart: "promtail",
        version: HELM_VERSIONS.promtail,
        forceUpdate: true,
        skipAwait: true,
        repositoryOpts: {
          repo: "https://grafana.github.io/helm-charts",
        },
        namespace: "monitoring",
        createNamespace: false,
        values: {
          config: {
            clients: [
              {
                url: `${config.lokiPushUrl}/loki/api/v1/push`,
                batchwait: "5s",
                batchsize: 1048576,
                backoff_config: {
                  max_retries: 20,
                  min_period: "500ms",
                  max_period: "5m",
                },
              },
            ],
          },
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
      },
      {
        provider,
        dependsOn: [runtimeMonitoringNs],
        customTimeouts: { create: "10m", update: "10m" },
      },
    );
  }

  // ---------------------------------------------------------------------------
  // kube-state-metrics — monitors tenant pod/deployment/namespace state.
  // Prometheus on mgmt scrapes this cross-cluster via NodePort 30092.
  // Dashboards (tenant-workloads, tenant-ecosystem) use job="runtime-kube-state-metrics".
  // ---------------------------------------------------------------------------

  const ksm = new k8s.helm.v3.Release(
    "runtime-kube-state-metrics",
    {
      name: "kube-state-metrics",
      chart: "kube-state-metrics",
      version: HELM_VERSIONS.kubeStateMetrics,
      repositoryOpts: {
        repo: "https://prometheus-community.github.io/helm-charts",
      },
      namespace: "monitoring",
      createNamespace: false,
      values: {
        fullnameOverride: "kube-state-metrics",
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "100m", memory: "128Mi" },
        },
      },
    },
    { provider, dependsOn: [runtimeMonitoringNs] },
  );

  // NodePort Service — enables cross-cluster Prometheus scrape from mgmt.
  // Port 30092 (pattern: 30090=argo-wf, 30100=loki, 30092=runtime-ksm).
  new k8s.core.v1.Service(
    "runtime-ksm-nodeport",
    {
      metadata: {
        name: "ksm-nodeport",
        namespace: "monitoring",
        labels: {
          "app.kubernetes.io/name": "kube-state-metrics",
          "app.kubernetes.io/component": "nodeport-cross-cluster",
        },
      },
      spec: {
        type: "NodePort",
        ports: [
          {
            port: 8080,
            targetPort: 8080,
            nodePort: 30092,
            protocol: "TCP",
            name: "metrics",
          },
        ],
        selector: {
          "app.kubernetes.io/name": "kube-state-metrics",
        },
      },
    },
    { provider, dependsOn: [ksm, runtimeMonitoringNs] },
  );

  // Resolve runtime cluster node IP for cross-cluster access.
  // Pattern mirrors build-bootstrap.ts resolve-build-node-ip.
  const runtimeNodeIp = new command.local.Command(
    "resolve-runtime-node-ip",
    {
      create: pulumi.interpolate`kubectl --kubeconfig <(echo '${infra.runtimeCluster.kubeconfig}') get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'`,
      interpreter: ["/bin/bash", "-c"],
      triggers: [infra.runtimeCluster.endpoint],
    },
  );
  const ksmEndpoint = runtimeNodeIp.stdout.apply(
    (ip: string) => ip.trim() ? `${ip.trim()}:30092` : "pending-runtime-ksm-ip:30092",
  );

  return {
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
    ksmEndpoint,
  };
}

// ---------------------------------------------------------------------------
// External Secrets Operator (ESO)
// ---------------------------------------------------------------------------

function installESO(
  provider: k8s.Provider,
  infra: InfraOutputs,
): void {
  const esoNs = new k8s.core.v1.Namespace(
    "runtime-external-secrets",
    {
      metadata: {
        name: "external-secrets",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "pod-security.kubernetes.io/enforce": "baseline",
          "pod-security.kubernetes.io/warn": "restricted",
          "pod-security.kubernetes.io/audit": "restricted",
        },
      },
    },
    { provider },
  );

  // ESO Helm chart
  const eso = new k8s.helm.v3.Release(
    "external-secrets",
    {
      name: "external-secrets",
      chart: "external-secrets",
      version: "0.14.3",
      repositoryOpts: {
        repo: "https://charts.external-secrets.io",
      },
      namespace: "external-secrets",
      createNamespace: false,
      values: {
        installCRDs: true,
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "128Mi" },
        },
        webhook: { create: true },
        certController: { create: true },
      },
    },
    { provider, dependsOn: [esoNs] },
  );

  // REVIEW-2026-03-30: Fixes HIGH-04 — was using admin kubeconfig token.
  // Now creates a dedicated ServiceAccount on mgmt cluster with read-only
  // access to Secrets in platform-system namespace only.
  const mgmtProvider = new k8s.Provider("eso-mgmt-k8s", {
    kubeconfig: infra.mgmtCluster.kubeconfig,
  });

  const esoSaName = "eso-secret-reader";
  const esoSa = new k8s.core.v1.ServiceAccount(
    "eso-mgmt-sa",
    { metadata: { name: esoSaName, namespace: PLATFORM_NAMESPACE } },
    { provider: mgmtProvider },
  );

  const esoRole = new k8s.rbac.v1.Role(
    "eso-mgmt-role",
    {
      metadata: { name: esoSaName, namespace: PLATFORM_NAMESPACE },
      rules: [
        // ESO only needs to read Secrets from platform-system namespace
        { apiGroups: [""], resources: ["secrets"], verbs: ["get", "list", "watch"] },
      ],
    },
    { provider: mgmtProvider },
  );

  new k8s.rbac.v1.RoleBinding(
    "eso-mgmt-binding",
    {
      metadata: { name: esoSaName, namespace: PLATFORM_NAMESPACE },
      subjects: [{ kind: "ServiceAccount", name: esoSaName, namespace: PLATFORM_NAMESPACE }],
      roleRef: { kind: "Role", name: esoSaName, apiGroup: "rbac.authorization.k8s.io" },
    },
    { provider: mgmtProvider, dependsOn: [esoRole, esoSa] },
  );

  // Long-lived token for the scoped SA
  const esoMgmtToken = new k8s.core.v1.Secret(
    "eso-mgmt-sa-token",
    {
      metadata: {
        name: `${esoSaName}-token`,
        namespace: PLATFORM_NAMESPACE,
        annotations: { "kubernetes.io/service-account.name": esoSaName },
      },
      type: "kubernetes.io/service-account-token",
    },
    { provider: mgmtProvider, dependsOn: [esoSa] },
  );

  // Extract scoped token + CA from the SA token Secret
  const scopedToken = esoMgmtToken.data.apply(
    (data) => data?.["token"] ? Buffer.from(data["token"], "base64").toString("utf-8") : "",
  );
  const scopedCa = esoMgmtToken.data.apply(
    (data) => data?.["ca.crt"] ? Buffer.from(data["ca.crt"]).toString("base64") : "",
  );

  // Store scoped token in runtime cluster for ESO to use
  new k8s.core.v1.Secret(
    "eso-mgmt-token",
    {
      metadata: {
        name: "mgmt-cluster-token",
        namespace: "external-secrets",
      },
      stringData: {
        token: scopedToken,
        ca: scopedCa,
      },
    },
    { provider, dependsOn: [esoNs] },
  );

  // ClusterSecretStore — tells ESO how to read secrets from mgmt cluster.
  // Uses scoped bearer token auth + mgmt cluster API server URL.
  // NOTE (MED-03): ExternalSecret consumers are created dynamically by the
  // TenantController when tenants are provisioned. The ClusterSecretStore is
  // the infrastructure foundation; consumers are created at runtime.
  new k8s.apiextensions.CustomResource(
    "eso-cluster-secret-store",
    {
      apiVersion: "external-secrets.io/v1beta1",
      kind: "ClusterSecretStore",
      metadata: {
        name: "loopzero-mgmt-store",
      },
      spec: {
        provider: {
          kubernetes: {
            remoteNamespace: PLATFORM_NAMESPACE,
            server: {
              url: infra.mgmtCluster.endpoint,
              caBundle: scopedCa,
            },
            auth: {
              token: {
                bearerToken: {
                  name: "mgmt-cluster-token",
                  namespace: "external-secrets",
                  key: "token",
                },
              },
            },
          },
        },
      },
    },
    { provider, dependsOn: [eso] },
  );
}

// ---------------------------------------------------------------------------
// Runtime cluster 2 bootstrap (multi-cell)
// Minimal bootstrap: namespace + Traefik + cert-manager + RBAC + DOCR pull secret.
// No ESO/Promtail/KSM (monitoring added later when cell is validated).
// ---------------------------------------------------------------------------

export function bootstrapRuntimeCluster2(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): { ingressReady: pulumi.Output<boolean> } {
  const provider = new k8s.Provider("runtime-2-k8s", {
    kubeconfig: infra.runtimeCluster2.kubeconfig,
  });

  const ns = createPlatformNamespace("runtime-2", config, provider);

  installMetricsServer("runtime-2", provider);

  const certManager = installCertManager("runtime-2", provider, ns);
  createClusterIssuer(provider, certManager, config, "runtime-2");

  const traefik = installTraefik("runtime-2", provider, ns, certManager);

  // RBAC for EnvironmentController
  const rt2RbacRole = new k8s.rbac.v1.ClusterRole(
    "runtime-2-loopzero-ctrl-rbac-admin",
    {
      metadata: { name: "loopzero-ctrl-runtime-rbac-admin" },
      rules: [
        { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "delete", "bind", "escalate"] },
        { apiGroups: [""], resources: ["namespaces", "serviceaccounts", "secrets", "configmaps", "services"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["apps"], resources: ["deployments"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: [""], resources: ["resourcequotas", "limitranges"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["policy"], resources: ["poddisruptionbudgets"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
      ],
    },
    { provider },
  );
  new k8s.rbac.v1.ClusterRoleBinding(
    "runtime-2-loopzero-ctrl-rbac-admin",
    {
      metadata: { name: "loopzero-ctrl-runtime-rbac-admin" },
      roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "ClusterRole", name: "loopzero-ctrl-runtime-rbac-admin" },
      subjects: [{ kind: "ServiceAccount", name: "loopzero-ctrl-runtime-sa", namespace: PLATFORM_NAMESPACE }],
    },
    { provider, dependsOn: [rt2RbacRole, ns] },
  );

  // DOCR pull secret
  createDockerRegistryPullSecret(
    "runtime-2-docr-pull-secret",
    infra.registryName,
    PLATFORM_NAMESPACE,
    provider,
    { write: false, dependsOn: [ns] },
  );

  return {
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
  };
}
