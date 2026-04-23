// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Management cluster bootstrap orchestrator.
// Each subsystem is extracted into its own module for maintainability.
// This file wires them together in the correct dependency order.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { CloudflareOutputs } from "./bootstrap-shared";
import { InfraError } from "../core/errors";
import {
  PLATFORM_NAMESPACE,
  HELM_VERSIONS,
  MgmtBootstrapOutputs,
  createPlatformNamespace,
  createDockerRegistryPullSecret,
  installCertManager,
  installTraefik,
  lookupLoadBalancerEndpoint,
  createClusterIssuer,
  installMetricsServer,
} from "./bootstrap-shared";

// Extracted subsystems
import { installArgoCD } from "./mgmt-argocd";
import { installNfsProvisioner, installGiteaRedis, installGitea } from "./mgmt-gitea";
import { createGiteaBackupCronJob } from "./mgmt-gitea-backup";
import { installMinIO } from "./mgmt-minio";
import { installOryKratos, installOryHydra } from "./mgmt-ory";
import { bootstrapGitea, createArgoCDRepoCredentials } from "./mgmt-gitea-bootstrap";
import { GITEA_ADMIN_PASSWORD_DEFAULT } from "./mgmt-gitea";
import { installCloudService } from "./mgmt-cloud-service";
import { installLoopZeroAPI, installLoopZeroController } from "./mgmt-api";
import { installTenantController } from "./mgmt-tenant-controller";
import { createPlatformIngressRoutes, createAPIGatewayRoute, createCloudGatewayRoute } from "./mgmt-ingress";
import { createPlatformNetworkPolicies, registerRuntimeCluster } from "./mgmt-network";
import { installPrometheus, installGrafana, installTempo, installOtelCollector, provisionGrafanaDashboards, installBlackboxExporter } from "./mgmt-observability";
import { installHarbor } from "./harbor-bootstrap";

// ---------------------------------------------------------------------------
// Management cluster bootstrap
// ---------------------------------------------------------------------------

export function bootstrapMgmtCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
  lokiEndpoint: pulumi.Output<string>,
  argoMetricsEndpoint: pulumi.Output<string>,
  cf?: CloudflareOutputs,
  runtimeKSMEndpoint?: pulumi.Output<string>,
): MgmtBootstrapOutputs {
  // Fail-fast for production without explicit passwords
  if (config.environment === "prod") {
    if (!config.giteaAdminPassword) {
      throw new InfraError({
        message: "Production requires giteaAdminPassword. Set via: pulumi config set --secret giteaAdminPassword <password>",
        code: "INVALID_CONFIG",
        context: { operation: "bootstrapMgmtCluster" },
      });
    }
    if (!config.minioRootPassword) {
      throw new InfraError({
        message: "Production requires minioRootPassword. Set via: pulumi config set --secret minioRootPassword <password>",
        code: "INVALID_CONFIG",
        context: { operation: "bootstrapMgmtCluster" },
      });
    }
  }

  const provider = new k8s.Provider("mgmt-k8s", {
    kubeconfig: infra.mgmtCluster.kubeconfig,
  });

  const ns = createPlatformNamespace("mgmt", config, provider);

  // DOCR pull secret
  createDockerRegistryPullSecret(
    "mgmt-docr-pull-secret", infra.registryName, PLATFORM_NAMESPACE, provider, { dependsOn: [ns] },
  );

  // cert-manager first — ArgoCD and Traefik may need TLS CRDs
  const certManager = installCertManager("mgmt", provider, ns);
  installMetricsServer("mgmt", provider);

  // Random secrets
  const callbackToken = new random.RandomPassword("argocd-callback-token", { length: 32, special: false });
  const serviceAuthSecret = new random.RandomPassword("service-auth-secret", { length: 48, special: false });
  const cookieSecret = new random.RandomPassword("kratos-ui-cookie-secret", { length: 32, special: false });

  // ArgoCD
  const argocd = installArgoCD(provider, ns, certManager, callbackToken.result);

  // ArgoCD repo-creds (inline — small, coupled to argocd)
  const giteaAdminPassword = config.giteaAdminPassword ?? GITEA_ADMIN_PASSWORD_DEFAULT;
  new k8s.core.v1.Secret("argocd-repo-creds-internal", {
    metadata: { name: "argocd-repo-creds-internal", namespace: PLATFORM_NAMESPACE, labels: { "argocd.argoproj.io/secret-type": "repo-creds" } },
    stringData: { type: "git", url: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`, username: "gitea_admin", password: giteaAdminPassword },
  }, { provider, dependsOn: [argocd] });
  new k8s.core.v1.Secret("argocd-repo-creds-external", {
    metadata: { name: "argocd-repo-creds-external", namespace: PLATFORM_NAMESPACE, labels: { "argocd.argoproj.io/secret-type": "repo-creds" } },
    stringData: { type: "git", url: `https://git.${config.domain}`, username: "gitea_admin", password: giteaAdminPassword, insecure: "true" },
  }, { provider, dependsOn: [argocd] });

  // Gitea subsystem
  const nfsProvisioner = installNfsProvisioner(provider, ns);
  const giteaRedis = installGiteaRedis(provider, ns);
  const gitea = installGitea(provider, ns, infra, config, giteaRedis, nfsProvisioner);

  // Gitea HPA
  new k8s.autoscaling.v2.HorizontalPodAutoscaler("gitea-hpa", {
    metadata: { name: "gitea", namespace: PLATFORM_NAMESPACE },
    spec: {
      scaleTargetRef: { apiVersion: "apps/v1", kind: "Deployment", name: "gitea" },
      minReplicas: 4, maxReplicas: 12,
      metrics: [{ type: "Resource", resource: { name: "cpu", target: { type: "Utilization", averageUtilization: 70 } } }],
      behavior: {
        scaleUp: { stabilizationWindowSeconds: 30, policies: [{ type: "Percent", value: 100, periodSeconds: 30 }] },
        scaleDown: { stabilizationWindowSeconds: 300, policies: [{ type: "Percent", value: 25, periodSeconds: 60 }] },
      },
    },
  }, { provider, dependsOn: [gitea] });

  createGiteaBackupCronJob(provider, ns, infra, config, gitea);

  // Shared dependency HPAs
  const hpaBehavior = {
    scaleUp: { stabilizationWindowSeconds: 30, policies: [{ type: "Percent", value: 100, periodSeconds: 30 }] },
    scaleDown: { stabilizationWindowSeconds: 300, policies: [{ type: "Percent", value: 25, periodSeconds: 60 }] },
  };
  const hpaCpuTarget = [{ type: "Resource", resource: { name: "cpu", target: { type: "Utilization", averageUtilization: 70 } } }];
  for (const [name, min, max] of [
    ["harbor-core", 1, 5], ["harbor-registry", 1, 8], ["harbor-jobservice", 1, 3],
    ["argocd-repo-server", 2, 10], ["argocd-server", 1, 3],
    ["ory-hydra", 1, 3], ["ory-kratos", 1, 3], ["mgmt-traefik", 2, 6],
  ] as const) {
    new k8s.autoscaling.v2.HorizontalPodAutoscaler(`${name}-hpa`, {
      metadata: { name, namespace: PLATFORM_NAMESPACE },
      spec: { scaleTargetRef: { apiVersion: "apps/v1", kind: "Deployment", name }, minReplicas: min, maxReplicas: max, metrics: hpaCpuTarget, behavior: hpaBehavior },
    }, { provider, dependsOn: [ns] });
  }

  // MinIO + credentials
  const minio = installMinIO(provider, ns, config);
  const minioPassword = config.minioRootPassword ?? pulumi.output("loopzero-minio-dev-2026");
  new k8s.core.v1.Secret("minio-credentials-mgmt", {
    metadata: { name: "loopzero-minio-credentials", namespace: PLATFORM_NAMESPACE },
    stringData: { AWS_ACCESS_KEY_ID: "loopzero-admin", AWS_SECRET_ACCESS_KEY: minioPassword, MINIO_ENDPOINT: `http://minio.${PLATFORM_NAMESPACE}.svc.cluster.local:9000` },
  }, { provider, dependsOn: [minio] });

  // Auth
  const oryKratos = installOryKratos(provider, ns, infra, config, cookieSecret);
  const oryHydra = installOryHydra(provider, ns, infra, config, oryKratos);

  // Observability
  const prometheus = installPrometheus(
    provider, ns, argoMetricsEndpoint, runtimeKSMEndpoint,
    config.slackWebhookUrl ? pulumi.output(config.slackWebhookUrl) : undefined,
    config.slackCriticalWebhookUrl ? pulumi.output(config.slackCriticalWebhookUrl) : undefined,
  );
  const grafana = installGrafana(provider, ns, config);
  provisionGrafanaDashboards(provider, ns, grafana);
  const tempo = installTempo(provider, ns, config, infra.spacesEndpoint);
  const otelCollector = installOtelCollector(provider, ns, tempo);
  void otelCollector;
  const blackbox = installBlackboxExporter(provider, ns);
  void blackbox;

  // Application services
  bootstrapGitea(provider, ns, gitea, config);
  createArgoCDRepoCredentials(provider, ns, argocd, gitea);
  const loopzeroApi = installLoopZeroAPI(provider, ns, infra, config, lokiEndpoint, callbackToken, cf, serviceAuthSecret);
  const loopzeroController = installLoopZeroController(provider, loopzeroApi, config, infra, lokiEndpoint);
  installCloudService(provider, ns, infra, config, serviceAuthSecret);
  const tenantController = installTenantController(provider, ns, infra, config);

  // Cluster registration
  registerRuntimeCluster(provider, ns, infra, argocd);

  // Ingress
  const traefik = installTraefik("mgmt", provider, ns, certManager);
  const ingressEndpoint = lookupLoadBalancerEndpoint("mgmt", provider, traefik);
  const clusterIssuer = createClusterIssuer(provider, certManager, config);
  const tlsRoutes = createPlatformIngressRoutes(provider, config, traefik, clusterIssuer);
  createAPIGatewayRoute(provider, config, traefik, clusterIssuer);
  createCloudGatewayRoute(provider, config, traefik, clusterIssuer);

  // Network + Harbor
  createPlatformNetworkPolicies(provider, ns);
  installHarbor(provider, ns, infra, config, certManager, traefik);

  return {
    certManagerReady: certManager.status.apply((s) => s.status === "deployed"),
    argoCdReady: argocd.status.apply((s) => s.status === "deployed"),
    giteaReady: gitea.status.apply((s) => s.status === "deployed"),
    prometheusReady: prometheus.status.apply((s) => s.status === "deployed"),
    grafanaReady: grafana.status.apply((s) => s.status === "deployed"),
    loopzeroApiReady: loopzeroApi.metadata.name.apply((n) => !!n),
    loopzeroControllerReady: loopzeroController.metadata.name.apply((n) => !!n),
    tenantControllerReady: tenantController.metadata.name.apply((n) => !!n),
    oryKratosReady: oryKratos.status.apply((s) => s.status === "deployed"),
    oryHydraReady: oryHydra.status.apply((s) => s.status === "deployed"),
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    tlsConfigured: tlsRoutes.apply((routes) => routes.length > 0),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
  };
}
