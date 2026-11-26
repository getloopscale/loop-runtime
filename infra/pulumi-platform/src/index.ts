// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * loop-runtime-platform — infrastructure services on K8s clusters.
 *
 * Architecture: ComponentResources with explicit dependencies.
 * Each service declares what it needs (inputs) and what it produces (outputs).
 * No `type = any`, no invented values, no adapters.
 *
 * Dependency chain:
 *   cert-manager → Traefik (needs CRDs)
 *   cert-manager → ArgoCD (needs CRDs)
 *   ArgoCD + Gitea → Gitea Bootstrap Job
 *   Loki + Promtail (build cluster, independent)
 *   ESO (runtime cluster, independent)
 *
 * All infra values come from StackReference (fail-fast via requireOutput).
 */

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import { loadPlatformConfig } from "./config";
import * as refs from "./refs";
import { CertManager } from "./services/cert-manager";
import { Traefik } from "./services/traefik";
import { ArgoCD } from "./services/argocd";
import { Gitea } from "./services/gitea";
import { Loki } from "./services/loki";
import { Prometheus } from "./services/prometheus";
import { Grafana } from "./services/grafana";
import { Tempo, OtelCollector } from "./services/tempo";

const config = loadPlatformConfig();
const NAMESPACE = "loop-runtime-system";

// --- K8s Providers (one per cluster, from StackReference) ---
const mgmtProvider = new k8s.Provider("mgmt-k8s", { kubeconfig: refs.mgmtKubeconfig });
const buildProvider = new k8s.Provider("build-k8s", { kubeconfig: refs.buildKubeconfig });
const runtimeProvider = new k8s.Provider("runtime-k8s", { kubeconfig: refs.runtimeKubeconfig });

// --- Platform namespace (mgmt cluster) ---
const mgmtNs = new k8s.core.v1.Namespace("mgmt-loop-runtime-system", {
  metadata: {
    name: NAMESPACE,
    labels: {
      "app.kubernetes.io/managed-by": "pulumi",
      "app.kubernetes.io/part-of": "loop-runtime-platform",
      "pod-security.kubernetes.io/enforce": "baseline",
      "pod-security.kubernetes.io/warn": "restricted",
      "pod-security.kubernetes.io/audit": "restricted",
    },
  },
}, { provider: mgmtProvider });

// --- Platform namespace (runtime cluster) ---
const runtimeNs = new k8s.core.v1.Namespace("runtime-loop-runtime-system", {
  metadata: {
    name: NAMESPACE,
    labels: {
      "app.kubernetes.io/managed-by": "pulumi",
      "app.kubernetes.io/part-of": "loop-runtime-platform",
      "pod-security.kubernetes.io/enforce": "baseline",
      "pod-security.kubernetes.io/warn": "restricted",
      "pod-security.kubernetes.io/audit": "restricted",
    },
  },
}, { provider: runtimeProvider });

// --- Docker registry credentials (from loop-runtime-infra, for imagePullSecrets) ---
new k8s.core.v1.Secret("mgmt-registry-creds", {
  metadata: { name: "loop-runtime-registry", namespace: NAMESPACE },
  type: "kubernetes.io/dockerconfigjson",
  stringData: { ".dockerconfigjson": refs.dockerConfigJson },
}, { provider: mgmtProvider, dependsOn: [mgmtNs] });

// ═══════════════════════════════════════════════
// MGMT CLUSTER — platform control services
// ═══════════════════════════════════════════════

// 1. cert-manager (first — other services depend on CRDs)
const mgmtCertManager = new CertManager("mgmt-cert-manager", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
  role: "mgmt",
}, { dependsOn: [mgmtNs] });

// 2. Traefik (depends on cert-manager for TLS)
const mgmtTraefik = new Traefik("mgmt-traefik", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
  role: "mgmt",
  certManager: mgmtCertManager,
}, { dependsOn: [mgmtNs] });

// 3. ArgoCD callback token (generated once, stored in Pulumi state)
const callbackToken = new random.RandomPassword("argocd-callback-token", {
  length: 32,
  special: false,
});

// 4. ArgoCD (depends on cert-manager)
const argocd = new ArgoCD("argocd", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
  callbackToken: callbackToken.result,
  certManager: mgmtCertManager,
}, { dependsOn: [mgmtNs] });

// 5. Gitea (internal git server).
// Uses doadmin credentials because DO managed PG15+ restricts schema permissions
// on the `public` schema — custom users can't CREATE tables without GRANT.
// The doadmin user has full privileges, which is required for Gitea's migrations.
// Role separation for Gitea is documented as follow-up (requires GRANT job).
// T1.4 (plan 2026-05-21-deep-review-remediation): no hardcoded fallback.
// Pulumi up fails fast if config not set, with named error directing operator
// to set the secret config.
if (!config.giteaAdminPassword) {
  throw new Error(
    "giteaAdminPassword Pulumi config secret is required — set via: " +
      "`pulumi config set --secret giteaAdminPassword <value>`. " +
      "See docs/operations.md.",
  );
}
const giteaPassword = config.giteaAdminPassword;
const gitea = new Gitea("gitea", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
  domain: config.domain,
  dbHost: refs.dbHost,
  dbPort: refs.dbPort,
  dbUser: pulumi.output("doadmin"),
  dbPassword: refs.dbPassword,
  adminPassword: giteaPassword,
}, { dependsOn: [mgmtNs] });

// 6. Prometheus (metrics collection + alerting)
const prometheus = new Prometheus("prometheus", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
}, { dependsOn: [mgmtNs] });

// 7. Grafana (dashboards + visualization) — depends on Prometheus for datasource
const grafana = new Grafana("grafana", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
  domain: config.domain,
  environment: config.environment,
  prometheus,
}, { dependsOn: [mgmtNs] });

// 8. Tempo (distributed tracing backend)
const tempo = new Tempo("tempo", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
}, { dependsOn: [mgmtNs] });

// 9. OTEL Collector (trace pipeline: API → Collector → Tempo)
const otelCollector = new OtelCollector("otel-collector", {
  provider: mgmtProvider,
  namespace: NAMESPACE,
  tempo,
}, { dependsOn: [mgmtNs] });

// ═══════════════════════════════════════════════
// BUILD CLUSTER — build pipeline services
// ═══════════════════════════════════════════════

// Loki + Promtail (build log aggregation)
const loki = new Loki("build-loki", {
  provider: buildProvider,
  namespace: "monitoring",
  s3Endpoint: refs.s3Endpoint,
  logsBucketName: refs.logsBucketName,
  accessKeyId: config.spacesAccessKeyId,
  secretAccessKey: config.spacesSecretAccessKey,
});

// ═══════════════════════════════════════════════
// RUNTIME CLUSTER — tenant-facing services
// ═══════════════════════════════════════════════

const runtimeCertManager = new CertManager("runtime-cert-manager", {
  provider: runtimeProvider,
  namespace: NAMESPACE,
  role: "runtime",
}, { dependsOn: [runtimeNs] });

const runtimeTraefik = new Traefik("runtime-traefik", {
  provider: runtimeProvider,
  namespace: NAMESPACE,
  role: "runtime",
  certManager: runtimeCertManager,
}, { dependsOn: [runtimeNs] });

// ═══════════════════════════════════════════════
// STACK OUTPUTS — consumed by loop-runtime-control-plane
// ═══════════════════════════════════════════════

export const mgmtIngressEndpoint = mgmtTraefik.ingressEndpoint;
export const runtimeIngressEndpoint = runtimeTraefik.ingressEndpoint;
export const lokiEndpoint = loki.lokiEndpoint;
export const giteaReady = gitea.ready;
export const argoCdReady = argocd.ready;
export const prometheusReady = prometheus.ready;
export const grafanaReady = grafana.ready;
