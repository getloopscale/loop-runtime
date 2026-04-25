// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Runtime cluster bootstrap orchestrator.
// Split modules: runtime-eso.ts, runtime-monitoring.ts

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import {
  PLATFORM_NAMESPACE,
  RuntimeBootstrapOutputs,
  createPlatformNamespace,
  installCertManager,
  installTraefik,
  lookupLoadBalancerEndpoint,
  createClusterIssuer,
  installMetricsServer,
  createDockerRegistryPullSecret,
} from "./bootstrap-shared";
import { installESO } from "./runtime-eso";
import { installKeda, installKedaHttpAddon } from "./runtime-keda";
import { installRuntimeMonitoring } from "./runtime-monitoring";

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

  installMetricsServer("runtime", provider);

  const certManager = installCertManager("runtime", provider, ns);
  createClusterIssuer(provider, certManager, config, "runtime");

  const traefik = installTraefik("runtime", provider, ns, certManager);
  const ingressEndpoint = lookupLoadBalancerEndpoint("runtime", provider, traefik);

  // RBAC for EnvironmentController
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

  // DOCR pull secret
  createDockerRegistryPullSecret(
    "runtime-docr-pull-secret",
    infra.registryName,
    PLATFORM_NAMESPACE,
    provider,
    { write: false, dependsOn: [ns] },
  );

  // External Secrets Operator
  installESO(provider, infra);

  // KEDA — event-driven autoscaler for scale-to-zero tenant apps
  const keda = installKeda("runtime", provider, ns);
  installKedaHttpAddon("runtime", provider, keda);

  // ESO writer RBAC for loopzero-api
  const esoWriterRole = new k8s.rbac.v1.ClusterRole(
    "loopzero-eso-writer",
    {
      metadata: { name: "loopzero-eso-writer" },
      rules: [
        { apiGroups: ["external-secrets.io"], resources: ["externalsecrets"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: [""], resources: ["secrets"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
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

  // Monitoring
  const { ksmEndpoint } = installRuntimeMonitoring(provider, infra, config);

  return {
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
    ksmEndpoint,
  };
}

// ---------------------------------------------------------------------------
// Runtime cluster 2 bootstrap (multi-cell)
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

  // KEDA — event-driven autoscaler for scale-to-zero tenant apps
  const keda2 = installKeda("runtime-2", provider, ns);
  installKedaHttpAddon("runtime-2", provider, keda2);

  return {
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
  };
}
