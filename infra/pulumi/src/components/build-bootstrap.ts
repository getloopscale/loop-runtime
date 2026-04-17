// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as command from "@pulumi/command";

import * as path from "path";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { HELM_VERSIONS, BuildBootstrapOutputs, installMetricsServer, createDockerRegistryPullSecret } from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Build cluster bootstrap
// ---------------------------------------------------------------------------

export function bootstrapBuildCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): BuildBootstrapOutputs {
  const provider = new k8s.Provider("build-k8s", {
    kubeconfig: infra.buildCluster.kubeconfig,
  });

  // MEET-2026-04-09-045: metrics-server for kubectl top + HPA
  installMetricsServer("build", provider);

  const ns = new k8s.core.v1.Namespace(
    "build-argo",
    {
      metadata: {
        name: "argo",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/cluster-role": "build",
          "pod-security.kubernetes.io/enforce": "baseline",
          "pod-security.kubernetes.io/warn": "restricted",
          "pod-security.kubernetes.io/audit": "restricted",
        },
      },
    },
    { provider },
  );

  // Object storage credentials — needed by CWTs for source download and cache.
  // On DigitalOcean: uses DO Spaces (S3-compatible), NOT the mgmt-cluster MinIO.
  // The build cluster has no network access to MinIO in the mgmt cluster.
  // MEET-2026-03-27-012: cross-cluster networking fix.
  new k8s.core.v1.Secret(
    "minio-credentials-build",
    {
      metadata: {
        name: "loopzero-minio-credentials",
        namespace: "argo",
      },
      stringData: {
        AWS_ACCESS_KEY_ID: config.spacesAccessKeyId,
        AWS_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
        MINIO_ENDPOINT: infra.spacesEndpoint,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // BuildKit registry credentials — needed by build-image step to PUSH images.
  // The DO token provides read+write access to the container registry.
  // Previously this was an empty placeholder populated by CI — now Pulumi
  // creates it with real credentials on every deploy (zero manual intervention).
  // Format: docker config.json with base64-encoded "token:<DO_TOKEN>" auth.
  const registryAuth = config.digitaloceanToken.apply(
    (token: string) => Buffer.from(`${token}:${token}`).toString("base64"),
  );
  new k8s.core.v1.Secret(
    "buildkit-registry-config",
    {
      metadata: {
        name: "buildkit-registry-config",
        namespace: "argo",
      },
      stringData: {
        "config.json": pulumi.interpolate`{"auths":{"registry.digitalocean.com":{"auth":"${registryAuth}"}}}`,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // DOCR pull secret — replaces manual `doctl kubernetes cluster registry add`.
  // Workflow pods (loopzero-clone-runner, loopzero-packs-runner, etc.) need to pull
  // private images from DOCR; without this they get 401 Unauthorized.
  // Write=true because BuildKit in the same namespace pushes built user images.
  const buildRegistryPullSecret = createDockerRegistryPullSecret(
    "build-docr-pull-secret",
    infra.registryName,
    "argo",
    provider,
    { write: true, dependsOn: [ns] },
  );

  // Pre-create the workflow ServiceAccount with Helm annotations.
  // This prevents the "invalid ownership metadata" error on destroy+recreate:
  // Helm requires resources to have release annotations, but partial first runs
  // create the SA without them. By creating via Pulumi with the correct
  // annotations, Helm recognizes it as owned and skips creation.
  const registrySecretName = "loopzero-infra-registry";
  void buildRegistryPullSecret; // Referenced by name in imagePullSecrets below.

  const buildSA = new k8s.core.v1.ServiceAccount(
    "loopzero-build-sa",
    {
      metadata: {
        name: "loopzero-build-sa",
        namespace: "argo",
        labels: { "app.kubernetes.io/managed-by": "Helm" },
        annotations: {
          "meta.helm.sh/release-name": "argo-workflows",
          "meta.helm.sh/release-namespace": "argo",
        },
      },
      imagePullSecrets: [{ name: registrySecretName }],
    },
    { provider, dependsOn: [ns] },
  );

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
          // SA is pre-created by Pulumi with Helm annotations above.
          // Helm sees it as owned and skips creation — avoids ownership conflicts.
          serviceAccount: {
            create: false,
            name: "loopzero-build-sa",
          },
          rbac: { create: true },
        },
      },
    },
    {
      provider,
      dependsOn: [ns],
      // On destroy+recreate, a partial first run may leave resources (ServiceAccount,
      // RBAC) without Helm annotations. Helm then refuses to install because it can't
      // adopt them. forceUpdate tells Helm to overwrite ownership metadata.
      // This also handles the Promtail Helm release below.
      customTimeouts: { create: "10m", update: "10m" },
    },
  );

  // ClusterWorkflowTemplates — reusable build steps (clone, cache-restore, cache-save).
  // Uses the local Helm chart at infra/helm/argo-workflows/ which contains the CWT templates.
  const cwts = new k8s.helm.v3.Release(
    "loopzero-build-cwts",
    {
      name: "loopzero-build-cwts",
      chart: path.resolve(__dirname, "../../../../infra/helm/argo-workflows"),
      namespace: "argo",
      createNamespace: false,
      values: {
        minio: {
          // In dev, MinIO runs without TLS inside the VPC
          insecure: config.environment !== "prod",
        },
      },
    },
    { provider, dependsOn: [ns, argoWorkflows] },
  );

  // Loki + Promtail — build log aggregation (both in build cluster, no cross-cluster for log shipping)
  // API queries Loki cross-cluster via NodePort 30100.
  const monitoringNs = new k8s.core.v1.Namespace(
    "build-monitoring",
    {
      metadata: {
        name: "monitoring",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          // Promtail/Loki need hostPath volumes for log collection — privileged is the
          // industry-standard PSA level for monitoring namespaces (same pattern as nfs-system).
          "pod-security.kubernetes.io/enforce": "privileged",
          "pod-security.kubernetes.io/warn": "restricted",
          "pod-security.kubernetes.io/audit": "restricted",
        },
      },
    },
    { provider },
  );

  // Loki — log aggregation, filesystem storage, NodePort for cross-cluster access by API
  const loki = new k8s.helm.v3.Release(
    "loki",
    {
      name: "loki",
      chart: "loki",
      version: HELM_VERSIONS.loki,
      repositoryOpts: {
        repo: "https://grafana.github.io/helm-charts",
      },
      namespace: "monitoring",
      createNamespace: false,
      values: {
        fullnameOverride: "loki",
        loki: {
          auth_enabled: false,
          commonConfig: {
            replication_factor: 1,
          },
          // Storage: S3 via DO Spaces for production, filesystem for local dev.
          // DO Spaces is S3-compatible — uses the same credentials as MinIO/Spaces.
          storage: {
            type: "s3",
            // Required by Loki chart v6.24+ commonStorageConfig helper template
            bucketNames: {
              chunks: infra.logsBucketName,
              ruler: infra.logsBucketName,
              admin: infra.logsBucketName,
            },
            s3: {
              endpoint: infra.s3Endpoint,
              bucketnames: infra.logsBucketName,
              access_key_id: config.spacesAccessKeyId,
              secret_access_key: config.spacesSecretAccessKey,
              s3ForcePathStyle: true,
              insecure: false,
              region: "us-east-1", // DO Spaces uses us-east-1 regardless of actual region
            },
          },
          limits_config: {
            retention_period: "168h",
            max_query_series: 500,
          },
          schemaConfig: {
            configs: [
              {
                from: "2026-01-01",
                store: "tsdb",
                object_store: "s3",
                schema: "v13",
                index: {
                  prefix: "loki_index_",
                  period: "24h",
                },
              },
            ],
          },
        },
        deploymentMode: "SingleBinary",
        singleBinary: {
          replicas: 1,
          persistence: {
            enabled: true,
            size: "10Gi",
          },
          resources: {
            requests: { cpu: "100m", memory: "256Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
          },
        },
        read: { replicas: 0 },
        write: { replicas: 0 },
        backend: { replicas: 0 },
        gateway: { enabled: false },
        monitoring: {
          selfMonitoring: { enabled: false },
          lokiCanary: { enabled: false },
        },
        test: { enabled: false },
        chunksCache: { enabled: false },
        resultsCache: { enabled: false },
      },
    },
    { provider, dependsOn: [monitoringNs] },
  );

  // NodePort Service for Loki — enables cross-cluster access from mgmt API.
  // The Loki chart creates only a ClusterIP service. This additional NodePort
  // service exposes Loki on port 30100 so the API (on mgmt cluster) can query logs.
  const lokiNodePort = new k8s.core.v1.Service(
    "loki-nodeport",
    {
      metadata: {
        name: "loki-nodeport",
        namespace: "monitoring",
        labels: {
          "app.kubernetes.io/name": "loki",
          "app.kubernetes.io/component": "nodeport-cross-cluster",
        },
      },
      spec: {
        type: "NodePort",
        ports: [
          {
            port: 3100,
            targetPort: 3100,
            nodePort: 30100,
            protocol: "TCP",
            name: "http",
          },
        ],
        selector: {
          "app.kubernetes.io/name": "loki",
          "app.kubernetes.io/component": "single-binary",
        },
      },
    },
    { provider, dependsOn: [loki, monitoringNs] },
  );

  // Promtail — ships Argo Workflows pod logs to Loki (same cluster, no cross-cluster needed)
  new k8s.helm.v3.Release(
    "promtail",
    {
      name: "promtail",
      chart: "promtail",
      version: HELM_VERSIONS.promtail,
      // forceUpdate recovers from a previously failed Helm release
      forceUpdate: true,
      // skipAwait: Promtail is a DaemonSet that self-heals. Don't block
      // the deploy waiting for readiness — it will become healthy once
      // Loki is reachable.
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
              // Same cluster: Promtail and Loki both run in build cluster
              url: "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push",
              batchwait: "5s",
              batchsize: 1048576,
              backoff_config: {
                max_retries: 20,
                min_period: "500ms",
                max_period: "5m",
              },
            },
          ],
          snippets: {
            scrapeConfigs: `
- job_name: argo-workflow-pods
  kubernetes_sd_configs:
    - role: pod
  relabel_configs:
    # Only collect logs from Argo Workflow pods
    - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_workflow]
      action: keep
      regex: .+
    # Set __path__ so Promtail knows which log files to tail
    - source_labels: [__meta_kubernetes_pod_uid, __meta_kubernetes_pod_container_name]
      target_label: __path__
      separator: /
      replacement: /var/log/pods/*$1/$2/*.log
    # Extract workflow ID for LogQL queries
    - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_workflow]
      target_label: workflow
    # Extract step/node name for filtering
    - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_node_name]
      target_label: step
    # Standard K8s metadata
    - source_labels: [__meta_kubernetes_namespace]
      target_label: namespace
    - source_labels: [__meta_kubernetes_pod_name]
      target_label: pod
    - source_labels: [__meta_kubernetes_pod_container_name]
      target_label: container`,
          },
        },
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "128Mi" },
        },
      },
    },
    {
      provider,
      dependsOn: [monitoringNs, loki],
      // Promtail is a DaemonSet — it schedules on all nodes automatically.
      // Skip await to prevent timeout when Promtail pods take time to start
      // (e.g., waiting for Loki to become reachable cross-cluster).
      customTimeouts: { create: "10m", update: "10m" },
    },
  );

  // NodePort Service for Argo Workflows controller metrics — enables
  // cross-cluster Prometheus scrape from mgmt cluster.
  // Port 30090 mirrors the pattern used for Loki (30100).
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
          {
            port: 9090,
            targetPort: 9090,
            nodePort: 30090,
            protocol: "TCP",
            name: "metrics",
          },
        ],
        selector: {
          "app.kubernetes.io/name": "argo-workflows-workflow-controller",
        },
      },
    },
    { provider, dependsOn: [argoWorkflows, ns] },
  );

  // Loki + Argo Workflows endpoints for cross-cluster access from mgmt.
  // NodePorts 30100 (Loki) and 30090 (Argo metrics) are accessible at any
  // node's private IP within the VPC.
  // Uses cluster endpoint as stable trigger — only re-resolves when cluster
  // is recreated (new endpoint), not on every `pulumi up`.
  const buildNodeIp = new command.local.Command(
    "resolve-build-node-ip",
    {
      create: pulumi.interpolate`kubectl --kubeconfig <(echo '${infra.buildCluster.kubeconfig}') get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'`,
      interpreter: ["/bin/bash", "-c"],
      // Stable trigger: only re-resolve when cluster endpoint changes (= cluster recreated).
      // Previously used Date.now() which caused diff on every run.
      triggers: [infra.buildCluster.endpoint],
    },
  );
  const lokiEndpoint = buildNodeIp.stdout.apply(
    (ip: string) => ip.trim() ? `http://${ip.trim()}:30100` : "http://pending-loki-ip:30100",
  );

  // Argo Workflows metrics endpoint for cross-cluster Prometheus scrape.
  const argoMetricsEndpoint = buildNodeIp.stdout.apply(
    (ip: string) => ip.trim() ? `${ip.trim()}:30090` : "pending-argo-metrics-ip:30090",
  );

  return {
    argoWorkflowsReady: argoWorkflows.status.apply(
      (s) => s.status === "deployed",
    ),
    lokiEndpoint,
    argoMetricsEndpoint,
  };
}
