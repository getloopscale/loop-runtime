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
import { createClusterImagePolicy, installPolicyController } from "./runtime-policy-controller";

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

  // RBAC for EnvironmentController + ArgoCD remote-cluster sync.
  //
  // ArgoCD application-controller uses this SA's bearer token (see
  // clustercredential_controller.go) to discover and reconcile resources
  // across the runtime cluster. It needs cluster-wide list/watch on
  // EVERY resource it might manage — including cluster-scoped types like
  // PersistentVolume, Node, StorageClass — even when no Application
  // currently references them, because Argo's resource-cache primer walks
  // the full API surface during initial cluster sync.
  //
  // CHG-002b 2026-04-27 — added cluster-scoped read on `persistentvolumes`,
  // `nodes`, `storageclasses` (storage.k8s.io). Without these, ArgoCD
  // emits `ComparisonError: failed to load initial state of resource
  // PersistentVolume: persistentvolumes is forbidden` and never marks
  // any Application Healthy. This was masked previously because
  // L1/L2 didn't exercise enough sync activity to surface the cache primer.
  const runtimeRbacRole = new k8s.rbac.v1.ClusterRole(
    "runtime-loopzero-ctrl-rbac-admin",
    {
      metadata: { name: "loopzero-ctrl-runtime-rbac-admin" },
      rules: [
        // Tenant resource provisioning (write).
        { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "delete", "bind", "escalate"] },
        { apiGroups: [""], resources: ["namespaces", "serviceaccounts", "secrets", "configmaps", "services"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["apps"], resources: ["deployments"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: [""], resources: ["resourcequotas", "limitranges"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        { apiGroups: ["policy"], resources: ["poddisruptionbudgets"], verbs: ["get", "list", "create", "update", "patch", "delete"] },
        // ArgoCD cache primer (read). Required for any Application sync to
        // succeed once tenant manifests reference these types.
        { apiGroups: [""], resources: ["persistentvolumes", "nodes", "events", "endpoints", "persistentvolumeclaims", "pods"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["storage.k8s.io"], resources: ["storageclasses", "volumeattachments", "csidrivers", "csinodes"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["apps"], resources: ["replicasets", "daemonsets", "statefulsets"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["batch"], resources: ["jobs", "cronjobs"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["autoscaling"], resources: ["horizontalpodautoscalers"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["traefik.io"], resources: ["ingressroutes", "middlewares", "tlsoptions"], verbs: ["get", "list", "watch", "create", "update", "patch", "delete"] },
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

  // Sprint 6 / T2.2: sigstore policy-controller. Verifies cosign signatures
  // on tenant images before admission. D4 — Audit mode: failurePolicy: Ignore
  // and validationActions: [Audit], so unsigned/legacy images are LOGGED but
  // not rejected for the first 2 weeks. Operator flips to Deny manually.
  // D3 — Key-based verification: cosignPublicKeyB64 is the public half of
  // the cosignPrivateKeyB64 used by the build cluster.
  const policyController = installPolicyController(
    "runtime",
    provider,
    ns,
    config.cosignPublicKeyB64,
  );
  // Only attach the policy when a public key is configured. Without it,
  // the ClusterImagePolicy would reference a non-existent Secret and the
  // webhook would reject (or in Audit mode, log) every image.
  if (config.cosignPublicKeyB64 && config.cosignPublicKeyB64.length > 0) {
    createClusterImagePolicy(
      "runtime",
      provider,
      infra.registryEndpoint,
      policyController,
    );
  }

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

  // RBAC for EnvironmentController + ArgoCD remote-cluster sync (see
  // runtime cluster role for full rationale; CHG-002b).
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
        // ArgoCD cache primer (read).
        { apiGroups: [""], resources: ["persistentvolumes", "nodes", "events", "endpoints", "persistentvolumeclaims", "pods"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["storage.k8s.io"], resources: ["storageclasses", "volumeattachments", "csidrivers", "csinodes"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["apps"], resources: ["replicasets", "daemonsets", "statefulsets"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["batch"], resources: ["jobs", "cronjobs"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["autoscaling"], resources: ["horizontalpodautoscalers"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["traefik.io"], resources: ["ingressroutes", "middlewares", "tlsoptions"], verbs: ["get", "list", "watch", "create", "update", "patch", "delete"] },
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
