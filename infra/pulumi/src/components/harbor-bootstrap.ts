// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * Harbor container registry bootstrap — installs Harbor via Helm on mgmt cluster.
 *
 * MEET-2026-04-11-048: Harbor replaces DOCR for container image storage.
 * Components: core×2, registry×2, jobservice×1, Redis×1 (internal).
 * Storage: S3 backend on DO Spaces. DB: external PostgreSQL (harbor_db).
 * Trivy/Notary/Chartmuseum/Portal disabled.
 */

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as path from "path";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";

const PLATFORM_NAMESPACE = "platform-system";

export interface HarborBootstrapOutputs {
  readonly harborReady: pulumi.Output<boolean>;
}

/**
 * Install Harbor registry on the management cluster.
 * Only runs when config.registryType === "harbor".
 */
export function installHarbor(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  certManager: k8s.helm.v3.Release,
  traefik: k8s.helm.v3.Release,
): HarborBootstrapOutputs {
  // Harbor is the tenant image registry — always installed when harborExternalUrl is set.
  // It coexists with DOCR: platform images stay in DOCR, tenant builds go to Harbor.
  if (!config.harborExternalUrl) {
    return { harborReady: pulumi.output(false) };
  }

  const harborUrl = config.harborExternalUrl;

  // Harbor Helm chart — using local chart wrapper that pulls from goharbor
  const harbor = new k8s.helm.v3.Release(
    "harbor",
    {
      name: "harbor",
      chart: path.resolve(__dirname, "../../../../infra/helm/harbor"),
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      timeout: 600,
      values: {
        harbor: {
          externalURL: `https://${harborUrl}`,
          // Database — external PG (LoopZero's managed instance)
          database: {
            type: "external",
            external: {
              host: infra.database.privateHost,
              port: "25060",
              username: "doadmin",
              password: infra.database.password,
              coreDatabase: "harbor_db",
              sslmode: "require",
              maxOpenConns: 10,
              maxIdleConns: 5,
            },
          },
          // S3 storage — DO Spaces for image blobs
          persistence: {
            enabled: true,
            resourcePolicy: "keep",
            imageChartStorage: {
              type: "s3",
              s3: {
                region: "us-east-1",
                regionendpoint: infra.spacesEndpoint.apply(ep => ep.replace("https://", "")),
                bucket: infra.containerImagesBucketName ?? pulumi.output(""),
                accesskey: config.spacesAccessKeyId,
                secretkey: config.spacesSecretAccessKey,
                rootdirectory: "/harbor",
                encrypt: false,
                secure: true,
              },
            },
          },
          // Expose via ClusterIP — Traefik handles TLS
          expose: {
            type: "clusterIP",
            tls: { enabled: false },
          },
          nginx: { replicas: 0 },
          // Disabled components
          trivy: { enabled: false },
          notary: { enabled: false },
          chartmuseum: { enabled: false },
          portal: { enabled: false },
          // Minimal replicas + small resource requests.
          // Homolog/dev mgmt clusters run small nodes (~2 vCPU/2 GB each);
          // Harbor defaults (core×2 + jobservice + registry×2 + redis + portal)
          // do not fit alongside the existing 26 platform pods. These values are
          // tuned for smoke-testing tenant registry flows — scale up in prod.
          core: {
            replicas: 1,
            resources: {
              requests: { cpu: "50m", memory: "256Mi" },
              limits: { cpu: "500m", memory: "512Mi" },
            },
          },
          jobservice: {
            replicas: 1,
            resources: {
              requests: { cpu: "50m", memory: "128Mi" },
              limits: { cpu: "300m", memory: "256Mi" },
            },
          },
          registry: {
            replicas: 1,
            registry: {
              resources: {
                requests: { cpu: "50m", memory: "128Mi" },
                limits: { cpu: "500m", memory: "512Mi" },
              },
            },
            controller: {
              resources: {
                requests: { cpu: "20m", memory: "64Mi" },
                limits: { cpu: "200m", memory: "256Mi" },
              },
            },
          },
          exporter: {
            resources: {
              requests: { cpu: "20m", memory: "64Mi" },
              limits: { cpu: "200m", memory: "256Mi" },
            },
          },
          redis: {
            internal: {
              resources: {
                requests: { cpu: "20m", memory: "64Mi" },
                limits: { cpu: "200m", memory: "256Mi" },
              },
            },
          },
          // Security
          containerSecurityContext: {
            runAsNonRoot: true,
            runAsUser: 10000,
            runAsGroup: 10000,
            allowPrivilegeEscalation: false,
            capabilities: { drop: ["ALL"] },
          },
          // Metrics — exposed on port 8001 by harbor-exporter.
          // ServiceMonitor is DISABLED because the platform runs standalone
          // Prometheus (chart `prometheus`), which does NOT ship the Prometheus
          // Operator / `monitoring.coreos.com/v1` CRDs. Scrape is instead wired
          // via `extraScrapeConfigs` in mgmt-observability.ts → installPrometheus,
          // targeting harbor-exporter.platform-system.svc:8001.
          metrics: {
            enabled: true,
            serviceMonitor: {
              enabled: false,
            },
          },
          // GC — daily at 2 AM
          gc: {
            enabled: true,
            schedule: "0 2 * * *",
            deleteUntagged: true,
          },
          // Canary CronJob — MEET-048 monitoring gate.
          canary: {
            enabled: true,
            image: "curlimages/curl:8.7.1",
            credentialSecretRef: {
              name: "harbor-admin-credentials",
            },
          },
          // Post-install init Job — configures Harbor after deployment.
          // Requires harbor-init image built by build-images.yml workflow.
          // Password comes from K8s Secret synced by ESO.
          init: {
            enabled: true,
            image: `registry.digitalocean.com/loopzero-infra-registry/harbor-init:latest`,
            passwordSecretRef: {
              name: "harbor-admin-credentials",
              key: "harbor-admin-password",
            },
          },
        },
      },
    },
    {
      provider,
      dependsOn: [ns, certManager, traefik],
      customTimeouts: { create: "15m", update: "15m" },
    },
  );

  // IngressRoute for Harbor — routes harbor.{domain} to Harbor core service
  new k8s.apiextensions.CustomResource(
    "harbor-ingress-cert",
    {
      apiVersion: "cert-manager.io/v1",
      kind: "Certificate",
      metadata: {
        name: "tls-harbor",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        secretName: "tls-harbor",
        issuerRef: {
          name: "letsencrypt-prod",
          kind: "ClusterIssuer",
        },
        dnsNames: [harborUrl],
      },
    },
    { provider, dependsOn: [certManager] },
  );

  new k8s.apiextensions.CustomResource(
    "harbor-ingress",
    {
      apiVersion: "traefik.io/v1alpha1",
      kind: "IngressRoute",
      metadata: {
        name: "harbor",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        entryPoints: ["websecure"],
        routes: [
          {
            match: `Host(\`${harborUrl}\`)`,
            kind: "Rule",
            services: [
              {
                name: "harbor-core",
                port: 80,
              },
            ],
          },
        ],
        tls: {
          secretName: "tls-harbor",
        },
      },
    },
    { provider, dependsOn: [harbor, traefik] },
  );

  return {
    harborReady: harbor.status.apply((s) => s.status === "deployed"),
  };
}
