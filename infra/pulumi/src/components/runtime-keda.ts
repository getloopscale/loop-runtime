// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// KEDA and HTTP Add-on for scale-to-zero on the runtime (workload) cluster.
// KEDA provides ScaledObject CRDs; the HTTP Add-on provides cold-start
// interceptor for apps at 0 replicas.

import * as k8s from "@pulumi/kubernetes";
import { HELM_VERSIONS, PLATFORM_NAMESPACE } from "./bootstrap-shared";

/**
 * Install KEDA operator on the runtime cluster.
 * Provides ScaledObject/ScaledJob CRDs for event-driven autoscaling.
 */
export function installKeda(
  role: string,
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    `${role}-keda`,
    {
      chart: "keda",
      version: HELM_VERSIONS.keda,
      repositoryOpts: { repo: "https://kedacore.github.io/charts" },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      waitForJobs: true,
      timeout: 300,
      values: {
        operator: {
          replicaCount: 1,
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { cpu: "500m", memory: "256Mi" },
          },
        },
        metricsServer: {
          replicaCount: 1,
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
        webhooks: {
          enabled: true,
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
        podSecurityContext: {
          operator: { runAsNonRoot: true },
          metricServer: { runAsNonRoot: true },
          webhooks: { runAsNonRoot: true },
        },
        // Prometheus Operator (which provides ServiceMonitor CRD) lives on the mgmt
        // cluster, NOT on runtime/runtime-2. Enabling serviceMonitor here breaks
        // bootstrap with `no matches for kind "ServiceMonitor" in version
        // "monitoring.coreos.com/v1"`. KEDA metrics are scraped cross-cluster from
        // mgmt Prometheus via Service endpoint discovery (kube-state-metrics +
        // remote_write). Sprint 8 v3.2 fix 2026-04-27.
        prometheus: {
          operator: { enabled: false },
          metricServer: { enabled: false },
          webhooks: { enabled: false },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

/**
 * Install KEDA HTTP Add-on on the runtime cluster.
 * Provides cold-start interceptor for scale-to-zero HTTP services.
 * Must be installed AFTER KEDA core (depends on KEDA CRDs).
 */
export function installKedaHttpAddon(
  role: string,
  provider: k8s.Provider,
  kedaRelease: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    `${role}-keda-http-addon`,
    {
      // Chart name in KEDA upstream Helm repo is `keda-add-ons-http`, not
      // `http-add-on`. Sprint 8 v3.2 fix 2026-04-27 — was rejecting bootstrap
      // with `chart "http-add-on" version "0.8.0" not found`.
      chart: "keda-add-ons-http",
      version: HELM_VERSIONS.kedaHttpAddon,
      repositoryOpts: { repo: "https://kedacore.github.io/charts" },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      waitForJobs: true,
      timeout: 300,
      values: {
        // Sprint 8 v3.2 fix 2026-04-27: keda-add-ons-http 0.8.0 chart defaults
        // images.kubeRbacProxy.name to `gcr.io/kubebuilder/kube-rbac-proxy:v0.13.0`
        // which has been DEPRECATED and removed from gcr.io. Override to the
        // maintained quay.io image (same upstream project, same maintainer,
        // same API). Fixes ImagePullBackOff on controller-manager pod.
        // Path verified against keda-add-ons-http-0.8.0/values.yaml at
        // https://kedacore.github.io/charts/keda-add-ons-http-0.8.0.tgz.
        images: {
          kubeRbacProxy: {
            name: "quay.io/brancz/kube-rbac-proxy",
            tag: "v0.18.1",
          },
        },
        interceptor: {
          replicas: { min: 1, max: 1 },
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
        operator: {
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
        scaler: {
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
        podSecurityContext: {
          runAsNonRoot: true,
        },
      },
    },
    { provider, dependsOn: [kedaRelease] },
  );
}
