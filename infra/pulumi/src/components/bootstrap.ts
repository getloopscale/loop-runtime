import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";

/**
 * Platform namespace — all LoopZero platform services live here.
 * Workload namespaces are separate, created by operators.
 */
const PLATFORM_NAMESPACE = "platform-system";

/**
 * Pinned Helm chart versions for deterministic provisioning.
 * Same commit = same infrastructure. Update these explicitly, never float.
 */
const HELM_VERSIONS = {
  certManager: "v1.14.5",
  traefik: "28.3.0",
  argocd: "7.3.11",
  gitea: "10.6.0",
  minio: "5.4.0",
  argoWorkflows: "1.0.5",
  prometheus: "25.27.0",
  grafana: "8.5.2",
  loki: "6.24.0",
  promtail: "6.16.6",
} as const;

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

export interface ClusterBootstrapOutputs {
  readonly ingressReady: pulumi.Output<boolean>;
  readonly ingressEndpoint: pulumi.Output<string>;
  readonly platformNamespace: pulumi.Output<string>;
}

export interface MgmtBootstrapOutputs extends ClusterBootstrapOutputs {
  readonly certManagerReady: pulumi.Output<boolean>;
  readonly argoCdReady: pulumi.Output<boolean>;
  readonly giteaReady: pulumi.Output<boolean>;
  readonly prometheusReady: pulumi.Output<boolean>;
  readonly grafanaReady: pulumi.Output<boolean>;
  readonly loopzeroApiReady: pulumi.Output<boolean>;
  readonly tenantControllerReady: pulumi.Output<boolean>;
  readonly tlsConfigured: pulumi.Output<boolean>;
}

export interface BuildBootstrapOutputs {
  readonly argoWorkflowsReady: pulumi.Output<boolean>;
  readonly lokiEndpoint: pulumi.Output<string>;
}

export interface BootstrapOutputs {
  readonly mgmt: MgmtBootstrapOutputs;
  readonly build: BuildBootstrapOutputs;
  readonly runtime: ClusterBootstrapOutputs;
}

// ---------------------------------------------------------------------------
// Phase 2 — Bootstrap both clusters
// ---------------------------------------------------------------------------

/**
 * Bootstraps both clusters with role-appropriate services.
 *
 * Management cluster gets:
 *   namespace → cert-manager → ArgoCD → Traefik
 *
 * Runtime cluster gets:
 *   namespace → Traefik (minimal ingress for user apps)
 *
 * ArgoCD runs only on management — it syncs to runtime cluster via
 * kubeconfig. This means management cluster outage doesn't affect
 * running applications, only new deploys.
 */
export function bootstrapCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): BootstrapOutputs {
  // Build cluster bootstrapped first — exports lokiEndpoint for mgmt API config
  const build = bootstrapBuildCluster(infra, config);
  const mgmt = bootstrapMgmtCluster(infra, config, build.lokiEndpoint);
  const runtime = bootstrapRuntimeCluster(infra, config);

  return { mgmt, build, runtime };
}

// ---------------------------------------------------------------------------
// Management cluster bootstrap
// ---------------------------------------------------------------------------

function bootstrapMgmtCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
  lokiEndpoint: pulumi.Output<string>,
): MgmtBootstrapOutputs {
  const provider = new k8s.Provider("mgmt-k8s", {
    kubeconfig: infra.mgmtCluster.kubeconfig,
  });

  const ns = createPlatformNamespace("mgmt", config, provider);

  // cert-manager first — ArgoCD and Traefik may need TLS CRDs
  const certManager = installCertManager("mgmt", provider, ns);

  // ArgoCD — GitOps engine, manages runtime cluster
  const argocd = installArgoCD(provider, ns, certManager);

  // Gitea — internal git server for desired state (ArgoCD reads from here)
  const gitea = installGitea(provider, ns, infra, config);

  // MinIO — S3-compatible artifact storage (source bundles, build cache)
  const minio = installMinIO(provider, ns);

  // MinIO credentials — needed by API for source bundle upload/download
  new k8s.core.v1.Secret(
    "minio-credentials-mgmt",
    {
      metadata: {
        name: "loopzero-minio-credentials",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        AWS_ACCESS_KEY_ID: "loopzero-admin",
        AWS_SECRET_ACCESS_KEY: "loopzero-minio-dev-2026",
        MINIO_ENDPOINT: `http://minio.${PLATFORM_NAMESPACE}.svc.cluster.local:9000`,
      },
    },
    { provider, dependsOn: [minio] },
  );

  // Prometheus — metrics collection (kube-state-metrics, node-exporter, cAdvisor)
  const prometheus = installPrometheus(provider, ns);

  // Grafana — dashboards and visualization, pre-configured with Prometheus + Loki data sources
  const grafana = installGrafana(provider, ns, config);

  // Gitea bootstrap — creates admin token, orgs, and stores credentials in K8s Secret.
  // The Job runs after Gitea is ready and is idempotent (safe to re-run).
  bootstrapGitea(provider, ns, gitea, config);

  // ArgoCD repo credentials — allows ArgoCD to authenticate to Gitea repos.
  createArgoCDRepoCredentials(provider, ns, argocd, gitea);

  // LoopZero API — platform HTTP server (connects to DB, Gitea, ArgoCD, Argo Workflows)
  const loopzeroApi = installLoopZeroAPI(provider, ns, infra, config, lokiEndpoint);

  // TenantController — K8s operator for tenant lifecycle (namespaces, RBAC, quotas)
  const tenantController = installTenantController(provider, ns, infra, config);

  // ArgoCD cluster secret — registers runtime cluster as deploy target
  registerRuntimeCluster(provider, ns, infra, argocd);

  // Traefik — ingress for platform services (ArgoCD UI, API, etc.)
  const traefik = installTraefik("mgmt", provider, ns, certManager);

  const ingressEndpoint = lookupLoadBalancerEndpoint(
    "mgmt",
    provider,
    traefik,
  );

  // ClusterIssuer — Let's Encrypt production certificates via HTTP-01
  const clusterIssuer = createClusterIssuer(provider, certManager, config);

  // TLS IngressRoutes for platform services
  const tlsRoutes = createPlatformIngressRoutes(
    provider,
    config,
    traefik,
    clusterIssuer,
  );

  return {
    certManagerReady: certManager.status.apply((s) => s.status === "deployed"),
    argoCdReady: argocd.status.apply((s) => s.status === "deployed"),
    giteaReady: gitea.status.apply((s) => s.status === "deployed"),
    prometheusReady: prometheus.status.apply((s) => s.status === "deployed"),
    grafanaReady: grafana.status.apply((s) => s.status === "deployed"),
    loopzeroApiReady: loopzeroApi.metadata.name.apply((n) => !!n),
    tenantControllerReady: tenantController.metadata.name.apply((n) => !!n),
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    tlsConfigured: tlsRoutes.apply((routes) => routes.length > 0),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
  };
}

// ---------------------------------------------------------------------------
// Runtime cluster bootstrap
// ---------------------------------------------------------------------------

function bootstrapRuntimeCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): ClusterBootstrapOutputs {
  const provider = new k8s.Provider("runtime-k8s", {
    kubeconfig: infra.runtimeCluster.kubeconfig,
  });

  const ns = createPlatformNamespace("runtime", config, provider);

  // cert-manager — needed for Let's Encrypt TLS on tenant app IngressRoutes
  const certManager = installCertManager("runtime", provider, ns);

  // ClusterIssuer — Let's Encrypt production certs for tenant apps
  const runtimeClusterIssuer = createClusterIssuer(provider, certManager, config, "runtime");

  // Traefik — ingress for user apps, depends on cert-manager for TLS
  const traefik = installTraefik("runtime", provider, ns, certManager);

  const ingressEndpoint = lookupLoadBalancerEndpoint(
    "runtime",
    provider,
    traefik,
  );

  // External Secrets Operator — syncs secrets from mgmt cluster to runtime namespaces.
  // ESO runs on runtime (workload) cluster and reads from mgmt via kubeconfig.
  installESO(provider, infra);

  return {
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
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

  // Kubeconfig Secret — allows ESO to read secrets from mgmt cluster.
  // Uses the mgmt cluster kubeconfig with full credentials.
  new k8s.core.v1.Secret(
    "eso-mgmt-kubeconfig",
    {
      metadata: {
        name: "mgmt-cluster-kubeconfig",
        namespace: "external-secrets",
      },
      stringData: {
        kubeconfig: infra.mgmtCluster.kubeconfig,
      },
    },
    { provider, dependsOn: [esoNs] },
  );

  // ClusterSecretStore — tells ESO how to read secrets from mgmt cluster.
  // Uses the Kubernetes provider with kubeconfig from the Secret above.
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
              caProvider: {
                type: "Secret",
                name: "mgmt-cluster-kubeconfig",
                namespace: "external-secrets",
                key: "kubeconfig",
              },
            },
            auth: {
              kubeconfig: {
                secretRef: {
                  name: "mgmt-cluster-kubeconfig",
                  namespace: "external-secrets",
                  key: "kubeconfig",
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
// Shared installation functions
// ---------------------------------------------------------------------------

function createPlatformNamespace(
  role: string,
  config: PlatformStackConfig,
  provider: k8s.Provider,
): k8s.core.v1.Namespace {
  return new k8s.core.v1.Namespace(
    `${role}-${PLATFORM_NAMESPACE}`,
    {
      metadata: {
        name: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/environment": config.environment,
          "loopzero.io/project": config.project,
          "loopzero.io/cluster-role": role,
        },
      },
    },
    { provider },
  );
}

function installCertManager(
  role: string,
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    `${role}-cert-manager`,
    {
      name: `${role}-cert-manager`,
      chart: "cert-manager",
      version: HELM_VERSIONS.certManager,
      repositoryOpts: {
        repo: "https://charts.jetstack.io",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        installCRDs: true,
        global: {
          leaderElection: {
            namespace: PLATFORM_NAMESPACE,
          },
        },
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "256Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

function installArgoCD(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  certManager: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "argocd",
    {
      name: "argocd",
      chart: "argo-cd",
      version: HELM_VERSIONS.argocd,
      repositoryOpts: {
        repo: "https://argoproj.github.io/argo-helm",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        // Deterministic Service names so IngressRoutes can reference them.
        // Without this, Pulumi's Helm provider adds a hash suffix to the release name.
        fullnameOverride: "argocd",
        // Server accessed via Traefik — TLS terminated at ingress
        server: {
          service: {
            type: "ClusterIP",
          },
        },
        configs: {
          params: {
            // TLS terminated at Traefik, ArgoCD serves plain HTTP
            "server.insecure": true,
          },
        },
        // Controller manages syncs to runtime cluster
        controller: {
          resources: {
            requests: { cpu: "100m", memory: "256Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
          },
        },
        // Repo server clones from Gitea
        repoServer: {
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
          },
        },
        // Redis for caching
        redis: {
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "256Mi" },
          },
        },
      },
    },
    { provider, dependsOn: [ns, certManager] },
  );
}

function installTraefik(
  role: string,
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  certManager?: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  const dependsOn: pulumi.Resource[] = [ns];
  if (certManager) {
    dependsOn.push(certManager);
  }

  return new k8s.helm.v3.Release(
    `${role}-traefik`,
    {
      name: "mgmt-traefik",
      chart: "traefik",
      version: HELM_VERSIONS.traefik,
      repositoryOpts: {
        repo: "https://traefik.github.io/charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        // Deterministic Service name so lookupLoadBalancerEndpoint can find it.
        // Without this, the chart template may produce an unexpected name.
        fullnameOverride: `${role}-traefik`,
        ports: {
          web: {
            port: 8000,
            expose: { default: true },
            exposedPort: 80,
          },
          websecure: {
            port: 8443,
            expose: { default: true },
            exposedPort: 443,
          },
        },
        service: {
          type: "LoadBalancer",
        },
        providers: {
          kubernetesCRD: { enabled: true },
          kubernetesIngress: { enabled: true },
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
      },
    },
    { provider, dependsOn },
  );
}

/**
 * Gitea — lightweight git server for desired state storage.
 *
 * Runs as a StatefulSet with a PVC for repository data.
 * Uses the managed PostgreSQL as database backend.
 * ArgoCD reads from Gitea to reconcile cluster state.
 *
 * The Helm chart creates:
 *   - StatefulSet with 1 replica
 *   - PVC (10Gi default) for /data
 *   - Service "gitea-http" on port 3000
 *   - Service "gitea-ssh" on port 22 (for git push via SSH)
 */
function installGitea(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "gitea",
    {
      name: "gitea",
      chart: "gitea",
      version: HELM_VERSIONS.gitea,
      repositoryOpts: {
        repo: "https://dl.gitea.com/charts/",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        // Deterministic Service names so IngressRoutes can reference them.
        // Without this, Pulumi's Helm provider adds a hash suffix to the release name.
        fullnameOverride: "gitea",
        // Use managed PostgreSQL — no embedded database
        postgresql: { enabled: false },
        "postgresql-ha": { enabled: false },
        redis: { enabled: false },
        "redis-cluster": { enabled: false },
        gitea: {
          admin: {
            username: "gitea_admin",
            password: GITEA_ADMIN_PASSWORD,
            email: "admin@loopzero.internal",
          },
          config: {
            database: {
              DB_TYPE: "postgres",
              HOST: pulumi.interpolate`${infra.database.privateHost}:${infra.database.port}`,
              NAME: "gitea",
              USER: infra.database.user,
              PASSWD: infra.database.password,
              SSL_MODE: "require",
            },
            server: {
              DOMAIN: `git.${config.domain}`,
              ROOT_URL: `https://git.${config.domain}/`,
              HTTP_PORT: "3000",
            },
            service: {
              DISABLE_REGISTRATION: true,
            },
            // In-memory cache instead of Redis — sufficient for internal use
            cache: {
              ADAPTER: "memory",
            },
            session: {
              PROVIDER: "memory",
            },
            queue: {
              TYPE: "level",
            },
          },
        },
        persistence: {
          enabled: true,
          size: "10Gi",
          accessModes: ["ReadWriteOnce"],
        },
        resources: {
          requests: { cpu: "100m", memory: "256Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

// ---------------------------------------------------------------------------
// Build cluster bootstrap
// ---------------------------------------------------------------------------

function bootstrapBuildCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): BuildBootstrapOutputs {
  const provider = new k8s.Provider("build-k8s", {
    kubeconfig: infra.buildCluster.kubeconfig,
  });

  const ns = new k8s.core.v1.Namespace(
    "build-argo",
    {
      metadata: {
        name: "argo",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/cluster-role": "build",
        },
      },
    },
    { provider },
  );

  // MinIO credentials — needed by CWTs for source download and cache
  new k8s.core.v1.Secret(
    "minio-credentials-build",
    {
      metadata: {
        name: "loopzero-minio-credentials",
        namespace: "argo",
      },
      stringData: {
        AWS_ACCESS_KEY_ID: "loopzero-admin",
        AWS_SECRET_ACCESS_KEY: "loopzero-minio-dev-2026",
        MINIO_ENDPOINT: `http://minio.${PLATFORM_NAMESPACE}.svc.cluster.local:9000`,
      },
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
          serviceAccount: {
            create: true,
            name: "loopzero-build-sa",
          },
          rbac: { create: true },
        },
      },
    },
    { provider, dependsOn: [ns] },
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
          storage: {
            type: "filesystem",
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
                object_store: "filesystem",
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

  // Loki endpoint for cross-cluster access from mgmt API.
  // Node IP is resolved post-deploy by infra-deploy.yml pipeline step
  // "Configure cross-cluster Loki" which runs kubectl to get the build
  // cluster node IP and patches the API configmap.
  // Placeholder value — pipeline replaces it with actual IP.
  const lokiEndpoint = pulumi.output("http://pending-loki-ip:30100");

  return {
    argoWorkflowsReady: argoWorkflows.status.apply(
      (s) => s.status === "deployed",
    ),
    lokiEndpoint,
  };
}

// ---------------------------------------------------------------------------
// MinIO — S3-compatible artifact storage on mgmt cluster
// ---------------------------------------------------------------------------

function installMinIO(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "minio",
    {
      name: "minio",
      chart: "minio",
      version: HELM_VERSIONS.minio,
      repositoryOpts: {
        repo: "https://charts.min.io/",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "minio",
        mode: "standalone",
        rootUser: "loopzero-admin",
        // Stable password — Date.now() caused rolling updates on every pulumi up.
        // For production, use pulumi.secret() or external secret manager.
        rootPassword: "loopzero-minio-dev-2026",
        persistence: {
          enabled: true,
          size: "10Gi",
        },
        service: {
          type: "ClusterIP",
        },
        consoleService: {
          type: "ClusterIP",
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
        // Auto-create buckets for platform use
        buckets: [
          { name: "loopzero-artifacts", policy: "none" },
          { name: "loopzero-builds", policy: "none" },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );
}

// ---------------------------------------------------------------------------
// Platform application services
// ---------------------------------------------------------------------------

/**
 * Container image registry endpoint. Must match the registry provisioned in Phase 1.
 * DOKS clusters are integrated via `doctl kubernetes cluster registry add`.
 */
const REGISTRY = "registry.digitalocean.com/loopzero-infra-registry";

/**
 * Stable Gitea admin password — used for basic auth to generate API tokens.
 * The Gitea Helm chart creates the admin user with this password.
 * For production, use pulumi.secret() or external secret manager.
 */
const GITEA_ADMIN_PASSWORD = "loopzero-gitea-dev-2026";

/**
 * Gitea bootstrap — creates admin token, orgs, and stores credentials in K8s Secret.
 *
 * The Job runs after Gitea is ready. It:
 * 1. Waits for Gitea to be healthy
 * 2. Generates an admin API token via Gitea API (basic auth)
 * 3. Creates orgs "loopzero" and "loopzero-platform" via Gitea API
 * 4. Creates/updates the "loopzero-platform-tokens" Secret with the real token
 * 5. Reads the ArgoCD admin password and stores it in the same Secret
 * 6. Calls the API bootstrap endpoint for DB seed
 *
 * ARGOCD_TOKEN is set to the ArgoCD admin password (read from argocd-initial-admin-secret).
 * The API uses this password to call ArgoCD's session API for JWT on demand.
 */
function bootstrapGitea(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  gitea: k8s.helm.v3.Release,
  config: PlatformStackConfig,
): void {
  // ServiceAccount for the bootstrap Job — needs permission to create/update Secrets
  const bootstrapSa = new k8s.core.v1.ServiceAccount(
    "gitea-bootstrap-sa",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
      },
    },
    { provider, dependsOn: [ns] },
  );

  const bootstrapRole = new k8s.rbac.v1.Role(
    "gitea-bootstrap-role",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
      },
      rules: [
        {
          apiGroups: [""],
          resources: ["secrets"],
          verbs: ["get", "create", "update", "patch"],
        },
      ],
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.rbac.v1.RoleBinding(
    "gitea-bootstrap-binding",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
      },
      subjects: [
        {
          kind: "ServiceAccount",
          name: "gitea-bootstrap",
          namespace: PLATFORM_NAMESPACE,
        },
      ],
      roleRef: {
        kind: "Role",
        name: "gitea-bootstrap",
        apiGroup: "rbac.authorization.k8s.io",
      },
    },
    { provider, dependsOn: [bootstrapRole, bootstrapSa] },
  );

  new k8s.batch.v1.Job(
    "gitea-bootstrap",
    {
      metadata: {
        name: "gitea-bootstrap",
        namespace: PLATFORM_NAMESPACE,
        annotations: {
          "pulumi.com/skipAwait": "true",
          "loopzero.io/run-id": new Date().toISOString(),
        },
      },
      spec: {
        backoffLimit: 10,
        ttlSecondsAfterFinished: 300,
        template: {
          spec: {
            serviceAccountName: "gitea-bootstrap",
            restartPolicy: "OnFailure",
            containers: [
              {
                name: "bootstrap",
                image: "bitnami/kubectl:latest",
                command: ["bash", "-c"],
                args: [
                  `set -e
GITEA_URL="http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000"
GITEA_USER="gitea_admin"
GITEA_PASS="${GITEA_ADMIN_PASSWORD}"
NS="${PLATFORM_NAMESPACE}"

echo "=== Step 1: Wait for Gitea to be ready ==="
for i in $(seq 1 120); do
  if curl -sf "$GITEA_URL/api/v1/settings/api" > /dev/null 2>&1; then
    echo "Gitea is ready after $i attempts"
    break
  fi
  if [ "$i" = "120" ]; then
    echo "ERROR: Gitea not ready after 120 attempts"
    exit 1
  fi
  sleep 5
done

echo "=== Step 2: Check if existing token is valid ==="
EXISTING_TOKEN=""
if kubectl get secret loopzero-platform-tokens -n "$NS" -o jsonpath='{.data.GITEA_TOKEN}' 2>/dev/null | base64 -d > /tmp/existing_token 2>/dev/null; then
  EXISTING_TOKEN=$(cat /tmp/existing_token)
fi

if [ -n "$EXISTING_TOKEN" ] && [ "$EXISTING_TOKEN" != "placeholder-set-after-first-deploy" ]; then
  if curl -sf "$GITEA_URL/api/v1/user" -H "Authorization: token $EXISTING_TOKEN" > /dev/null 2>&1; then
    echo "Existing Gitea token is valid, skipping token generation"
    GITEA_TOKEN="$EXISTING_TOKEN"
  fi
fi

echo "=== Step 3: Generate Gitea API token ==="
if [ -z "\${GITEA_TOKEN:-}" ]; then
  # Delete old token if it exists (idempotent)
  curl -sf -X DELETE "$GITEA_URL/api/v1/users/$GITEA_USER/tokens/loopzero-platform" \
    -u "$GITEA_USER:$GITEA_PASS" > /dev/null 2>&1 || true

  # Generate new token with all scopes
  GITEA_TOKEN=$(curl -sf -X POST "$GITEA_URL/api/v1/users/$GITEA_USER/tokens" \
    -u "$GITEA_USER:$GITEA_PASS" \
    -H "Content-Type: application/json" \
    -d '{"name":"loopzero-platform","scopes":["all"]}' | grep -o '"sha1":"[^"]*"' | cut -d'"' -f4)

  if [ -z "$GITEA_TOKEN" ]; then
    echo "ERROR: Failed to generate Gitea token"
    exit 1
  fi
  echo "Gitea token generated successfully"
fi

echo "=== Step 4: Create orgs ==="
for ORG in loopzero loopzero-platform; do
  if curl -sf "$GITEA_URL/api/v1/orgs/$ORG" -H "Authorization: token $GITEA_TOKEN" > /dev/null 2>&1; then
    echo "Org $ORG already exists"
  else
    curl -sf -X POST "$GITEA_URL/api/v1/orgs" \
      -H "Authorization: token $GITEA_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\\"username\\":\\"$ORG\\",\\"visibility\\":\\"private\\"}" > /dev/null 2>&1 && \
      echo "Created org $ORG" || echo "WARN: Could not create org $ORG"
  fi
done

echo "=== Step 5: Read ArgoCD admin password ==="
ARGOCD_PASS=""
if kubectl get secret argocd-initial-admin-secret -n "$NS" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d > /tmp/argocd_pass 2>/dev/null; then
  ARGOCD_PASS=$(cat /tmp/argocd_pass)
  echo "ArgoCD admin password read successfully"
else
  echo "WARN: Could not read ArgoCD admin password"
fi

echo "=== Step 6: Update loopzero-platform-tokens Secret ==="
kubectl create secret generic loopzero-platform-tokens \
  --namespace="$NS" \
  --from-literal="GITEA_TOKEN=$GITEA_TOKEN" \
  --from-literal="ARGOCD_TOKEN=\${ARGOCD_PASS:-admin}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "=== Step 7: Update ArgoCD repo credentials ==="
kubectl create secret generic gitea-repo-creds \
  --namespace="$NS" \
  --from-literal="type=git" \
  --from-literal="url=http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000" \
  --from-literal="username=$GITEA_USER" \
  --from-literal="password=$GITEA_TOKEN" \
  --dry-run=client -o yaml | kubectl label -f - --dry-run=client -o yaml --local "argocd.argoproj.io/secret-type=repo-creds" | kubectl apply -f -

echo "=== Step 8: Restart ArgoCD to pick up new credentials ==="
kubectl rollout restart deployment/argocd-repo-server -n "$NS" 2>/dev/null || true

echo "=== Step 9: Call API bootstrap endpoint ==="
API_URL="http://loopzero-api.${PLATFORM_NAMESPACE}.svc.cluster.local:8080"
for i in $(seq 1 30); do
  if curl -sf "$API_URL/api/v1/health" > /dev/null 2>&1; then
    echo "API is healthy"
    curl -sf -X POST "$API_URL/internal/bootstrap" \
      -H "Content-Type: application/json" \
      -d '{"admin_github_id":"1","admin_login":"admin","tenant_slug":"loopzero","tenant_display_name":"loopzero","plan":"starter"}' && \
      echo "Bootstrap endpoint called successfully" || echo "WARN: Bootstrap endpoint returned error (may already be seeded)"
    break
  fi
  if [ "$i" = "30" ]; then
    echo "WARN: API not ready after 30 attempts, skipping bootstrap endpoint"
  fi
  sleep 5
done

echo "=== Bootstrap complete ==="
`,
                ],
                resources: {
                  requests: { cpu: "50m", memory: "64Mi" },
                  limits: { cpu: "200m", memory: "128Mi" },
                },
              },
            ],
          },
        },
      },
    },
    { provider, dependsOn: [ns, gitea, bootstrapSa] },
  );

  // Create the platform tokens Secret with initial placeholders.
  // The bootstrap Job above will update this with real values once Gitea is ready.
  // Pulumi creates it first so the API deployment can reference it without errors.
  new k8s.core.v1.Secret(
    "loopzero-platform-tokens",
    {
      metadata: {
        name: "loopzero-platform-tokens",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        // Placeholder — will be overwritten by the bootstrap Job with a real token.
        GITEA_TOKEN: "placeholder-set-after-first-deploy",
        // ArgoCD admin password — will be overwritten by the bootstrap Job.
        ARGOCD_TOKEN: "admin",
      },
    },
    { provider, dependsOn: [ns] },
  );
}

/**
 * ArgoCD repo credentials — allows ArgoCD to clone from Gitea repos.
 * Uses a repo-creds pattern (URL prefix match) so all repos under the
 * Gitea org are automatically authenticated.
 */
function createArgoCDRepoCredentials(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  argocd: k8s.helm.v3.Release,
  gitea: k8s.helm.v3.Release,
): void {
  new k8s.core.v1.Secret(
    "argocd-gitea-repo-creds",
    {
      metadata: {
        name: "gitea-repo-creds",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "argocd.argoproj.io/secret-type": "repo-creds",
        },
        annotations: {
          // Force Pulumi to take field ownership from kubectl-client-side-apply.
          // The Bootstrap step updates the password via kubectl after first deploy.
          "pulumi.com/patchForce": "true",
        },
      },
      stringData: {
        type: "git",
        url: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`,
        username: "gitea_admin",
        password: "placeholder-set-after-first-deploy",
      },
    },
    {
      provider,
      dependsOn: [ns, argocd, gitea],
      ignoreChanges: ["stringData"],
    },
  );
}

function installLoopZeroAPI(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  lokiEndpoint: pulumi.Output<string>,
): k8s.apps.v1.Deployment {
  // Database connection string for the `loopzero` database (created in Phase 1).
  const databaseUrl = pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`;

  const secret = new k8s.core.v1.Secret(
    "loopzero-api-secrets",
    {
      metadata: { name: "loopzero-api-secrets", namespace: PLATFORM_NAMESPACE },
      stringData: {
        DATABASE_URL: databaseUrl,
        LOOPZERO_ARGO_CALLBACK_SECRET: "cb-loopzero-dev-2026",
        // Spaces credentials (S3-compatible) — used by S3 uploader for source bundles
        MINIO_ACCESS_KEY: config.spacesAccessKeyId,
        MINIO_SECRET_KEY: config.spacesSecretAccessKey,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Database migrations — read SQL files and embed in a ConfigMap.
  // The init container (migrate/migrate) mounts this and applies them.
  const migrationsDir = path.resolve(__dirname, "../../../../api/internal/db/migrations");
  const migrationFiles = fs.readdirSync(migrationsDir).filter((f) => f.endsWith(".up.sql")).sort();
  const migrationData: Record<string, string> = {};
  for (const f of migrationFiles) {
    migrationData[f] = fs.readFileSync(path.join(migrationsDir, f), "utf-8");
  }

  const migrationsConfigMap = new k8s.core.v1.ConfigMap(
    "loopzero-api-migrations",
    {
      metadata: { name: "loopzero-api-migrations", namespace: PLATFORM_NAMESPACE },
      data: migrationData,
    },
    { provider, dependsOn: [ns] },
  );

  // Kubeconfigs — the API runs in mgmt but submits workflows to the build cluster.
  const kubeconfigSecret = new k8s.core.v1.Secret(
    "loopzero-api-kubeconfig",
    {
      metadata: { name: "loopzero-api-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: {
        "mgmt-kubeconfig": infra.mgmtCluster.kubeconfig,
        "build-kubeconfig": infra.buildCluster.kubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  const configMap = new k8s.core.v1.ConfigMap(
    "loopzero-api-config",
    {
      metadata: { name: "loopzero-api-config", namespace: PLATFORM_NAMESPACE },
      data: {
        PORT: "8080",
        METRICS_PORT: "9090",
        LOOPZERO_ENV: config.environment === "prod" ? "production" : "development",
        BASE_DOMAIN: config.domain,
        WORKLOAD_INGRESS_URL: `https://${config.domain}`,
        GITEA_URL: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`,
        ARGOCD_URL: `http://argocd-server.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        ARGOCD_INSECURE: "true",
        ARGO_NAMESPACE: "argo",
        MGMT_KUBECONFIG: "/etc/loopzero/mgmt-kubeconfig",
        MGMT_KUBE_CONTEXT: pulumi.interpolate`do-${config.region}-${infra.mgmtCluster.clusterName}`,
        BUILD_KUBECONFIG: "/etc/loopzero/build-kubeconfig",
        BUILD_KUBE_CONTEXT: pulumi.interpolate`do-${config.region}-${infra.buildCluster.clusterName}`,
        MINIO_ENDPOINT: `https://${config.region}.digitaloceanspaces.com`,
        REGISTRY_URL: "registry.digitalocean.com/loopzero-infra-registry",
        REGISTRY_INSECURE: "false",
        ARGOCD_NAMESPACE: PLATFORM_NAMESPACE,
        // Loki runs on build cluster. API accesses via cross-cluster NodePort 30100.
        // Node IP resolved dynamically from build cluster node list.
        LOKI_URL: pulumi.interpolate`${lokiEndpoint}`,
      },
    },
    { provider, dependsOn: [ns] },
  );

  const svc = new k8s.core.v1.Service(
    "loopzero-api-svc",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "loopzero-api" },
        annotations: { "pulumi.com/skipAwait": "true" },
      },
      spec: {
        selector: { app: "loopzero-api" },
        ports: [{ port: 8080, targetPort: 8080, name: "http" }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  return new k8s.apps.v1.Deployment(
    "loopzero-api",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "loopzero-api" },
        annotations: {
          "pulumi.com/skipAwait": "true",
        },
      },
      spec: {
        replicas: 2,
        selector: { matchLabels: { app: "loopzero-api" } },
        template: {
          metadata: {
            labels: { app: "loopzero-api" },
            annotations: {
              "prometheus.io/scrape": "true",
              "prometheus.io/port": "9090",
            },
          },
          spec: {
            // Database migrations run before API starts via init container.
            // The API image includes migration SQL files at /migrations/.
            initContainers: [
              {
                name: "db-migrate",
                image: "migrate/migrate:v4.18.1",
                args: [
                  "-path=/migrations",
                  "-database",
                  "$(DATABASE_URL)",
                  "up",
                ],
                env: [
                  {
                    name: "DATABASE_URL",
                    valueFrom: {
                      secretKeyRef: {
                        name: "loopzero-api-secrets",
                        key: "DATABASE_URL",
                      },
                    },
                  },
                ],
                volumeMounts: [
                  {
                    name: "api-migrations",
                    mountPath: "/migrations",
                    readOnly: true,
                  },
                ],
                resources: {
                  requests: { cpu: "50m", memory: "64Mi" },
                  limits: { cpu: "100m", memory: "128Mi" },
                },
              },
            ],
            volumes: [
              {
                name: "kubeconfig",
                secret: { secretName: "loopzero-api-kubeconfig" },
              },
              {
                name: "api-migrations",
                configMap: { name: "loopzero-api-migrations" },
              },
            ],
            containers: [
              {
                name: "api",
                image: `${REGISTRY}/loopzero-api:dev`,
                imagePullPolicy: "Always",
                ports: [
                  { name: "http", containerPort: 8080 },
                  { name: "metrics", containerPort: 9090 },
                ],
                envFrom: [
                  { configMapRef: { name: "loopzero-api-config" } },
                  { secretRef: { name: "loopzero-api-secrets" } },
                  // Gitea token + ArgoCD password — created by gitea-bootstrap Job
                  { secretRef: { name: "loopzero-platform-tokens" } },
                ],
                volumeMounts: [
                  {
                    name: "kubeconfig",
                    mountPath: "/etc/loopzero",
                    readOnly: true,
                  },
                ],
                resources: {
                  requests: { cpu: "200m", memory: "512Mi" },
                  limits: { cpu: "1", memory: "1Gi" },
                },
                livenessProbe: {
                  httpGet: { path: "/api/v1/health", port: 8080 },
                  initialDelaySeconds: 15,
                  periodSeconds: 10,
                },
                readinessProbe: {
                  httpGet: { path: "/api/v1/health", port: 8080 },
                  initialDelaySeconds: 5,
                  periodSeconds: 5,
                },
              },
            ],
          },
        },
      },
    },
    { provider, dependsOn: [ns, secret, kubeconfigSecret, migrationsConfigMap, configMap, svc] },
  );
}

function installTenantController(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): k8s.apps.v1.Deployment {
  // CRDs — LoopZero custom resource definitions.
  // Path is relative to infra/pulumi/ (the Pulumi working directory).
  const crds = new k8s.yaml.ConfigGroup(
    "loopzero-crds",
    {
      files: ["../../operators/config/crd/bases/*.yaml"],
    },
    { provider },
  );

  // ServiceAccount for the controller
  const sa = new k8s.core.v1.ServiceAccount(
    "tenant-controller-sa",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
      },
      imagePullSecrets: [{ name: "loopzero-infra-registry" }],
    },
    { provider, dependsOn: [ns] },
  );

  // ClusterRole — permissions for tenant lifecycle management
  const role = new k8s.rbac.v1.ClusterRole(
    "tenant-controller-role",
    {
      metadata: { name: "tenant-controller" },
      rules: [
        // LoopZero CRDs — all custom resources managed by the operator.
        // Uses wildcard to cover all current and future loopzero.io resources:
        // tenants, environments, loopzeroapplications,
        // exposedroutes, platformbootstraps, gitrepositorybindings,
        // clustercredentials, appsecrets, previewenvironments, reconciliationreports
        {
          apiGroups: ["loopzero.io"],
          resources: ["*"],
          verbs: ["get", "list", "watch", "create", "update", "patch", "delete"],
        },
        {
          apiGroups: ["loopzero.io"],
          resources: ["*/status", "*/finalizers"],
          verbs: ["get", "update", "patch"],
        },
        // Namespace lifecycle
        {
          apiGroups: [""],
          resources: ["namespaces"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // Resource management per tenant namespace
        {
          apiGroups: [""],
          resources: ["resourcequotas", "limitranges", "secrets", "serviceaccounts"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // Network isolation
        {
          apiGroups: ["networking.k8s.io"],
          resources: ["networkpolicies"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // RBAC per tenant
        {
          apiGroups: ["rbac.authorization.k8s.io"],
          resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"],
          verbs: ["get", "list", "watch", "create", "update", "patch", "bind", "escalate"],
        },
        // ArgoCD resources (AppProjects + Applications per tenant)
        {
          apiGroups: ["argoproj.io"],
          resources: ["appprojects", "applications"],
          verbs: ["get", "list", "watch", "create", "update", "patch", "delete"],
        },
        // Leader election
        {
          apiGroups: ["coordination.k8s.io"],
          resources: ["leases"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // Events
        {
          apiGroups: [""],
          resources: ["events"],
          verbs: ["create", "patch"],
        },
      ],
    },
    { provider },
  );

  const binding = new k8s.rbac.v1.ClusterRoleBinding(
    "tenant-controller-binding",
    {
      metadata: { name: "tenant-controller" },
      subjects: [
        {
          kind: "ServiceAccount",
          name: "tenant-controller",
          namespace: PLATFORM_NAMESPACE,
        },
      ],
      roleRef: {
        kind: "ClusterRole",
        name: "tenant-controller",
        apiGroup: "rbac.authorization.k8s.io",
      },
    },
    { provider, dependsOn: [role, sa] },
  );

  // Secret with kubeconfigs for build + runtime clusters.
  // The controller uses these to create namespaces on the correct clusters.
  const controllerKubeconfig = new k8s.core.v1.Secret(
    "tenant-controller-kubeconfig",
    {
      metadata: {
        name: "tenant-controller-kubeconfig",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        "build-kubeconfig": infra.buildCluster.kubeconfig,
        "runtime-kubeconfig": infra.runtimeCluster.kubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Secret with cloud provider credentials for the tenant controller.
  // These are used to create registry credentials and Spaces access in build namespaces.
  // Stored as Secret (not plain env vars) to prevent exposure via K8s API.
  const controllerCredentials = new k8s.core.v1.Secret(
    "tenant-controller-credentials",
    {
      metadata: {
        name: "tenant-controller-credentials",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        SPACES_ACCESS_KEY_ID: config.spacesAccessKeyId,
        SPACES_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
        DIGITALOCEAN_TOKEN: config.digitaloceanToken,
      },
    },
    { provider, dependsOn: [ns] },
  );

  return new k8s.apps.v1.Deployment(
    "tenant-controller",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "tenant-controller" },
        annotations: {
          "pulumi.com/skipAwait": "true",
        },
      },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "tenant-controller" } },
        template: {
          metadata: {
            labels: { app: "tenant-controller" },
            annotations: {
              "prometheus.io/scrape": "true",
              "prometheus.io/port": "8080",
            },
          },
          spec: {
            serviceAccountName: "tenant-controller",
            imagePullSecrets: [{ name: "loopzero-infra-registry" }],
            volumes: [
              {
                name: "kubeconfigs",
                secret: { secretName: "tenant-controller-kubeconfig" },
              },
            ],
            containers: [
              {
                name: "controller",
                image: `${REGISTRY}/tenant-controller:dev`,
                imagePullPolicy: "Always",
                args: ["--leader-elect=true"],
                ports: [
                  { name: "metrics", containerPort: 8080 },
                  { name: "health", containerPort: 8081 },
                ],
                volumeMounts: [
                  {
                    name: "kubeconfigs",
                    mountPath: "/etc/loopzero",
                    readOnly: true,
                  },
                ],
                env: [
                  {
                    name: "GITEA_URL",
                    value: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`,
                  },
                  {
                    name: "GITEA_TOKEN",
                    valueFrom: {
                      secretKeyRef: {
                        name: "loopzero-platform-tokens",
                        key: "GITEA_TOKEN",
                      },
                    },
                  },
                  {
                    name: "BUILD_KUBECONFIG",
                    value: "/etc/loopzero/build-kubeconfig",
                  },
                  {
                    name: "RUNTIME_KUBECONFIG",
                    value: "/etc/loopzero/runtime-kubeconfig",
                  },
                  {
                    name: "WORKLOAD_CLUSTER_URL",
                    value: pulumi.interpolate`${infra.runtimeCluster.endpoint}`,
                  },
                  {
                    name: "ARGOCD_NAMESPACE",
                    value: PLATFORM_NAMESPACE,
                  },
                  // Object storage endpoint — DigitalOcean Spaces (S3-compatible).
                  // Used by controller to create credentials Secret in build namespaces.
                  {
                    name: "MINIO_ENDPOINT",
                    value: `https://${config.region}.digitaloceanspaces.com`,
                  },
                  {
                    name: "SPACES_ACCESS_KEY_ID",
                    valueFrom: {
                      secretKeyRef: {
                        name: "tenant-controller-credentials",
                        key: "SPACES_ACCESS_KEY_ID",
                      },
                    },
                  },
                  {
                    name: "SPACES_SECRET_ACCESS_KEY",
                    valueFrom: {
                      secretKeyRef: {
                        name: "tenant-controller-credentials",
                        key: "SPACES_SECRET_ACCESS_KEY",
                      },
                    },
                  },
                  // DO Container Registry auth — Kaniko needs this for image push.
                  {
                    name: "DIGITALOCEAN_TOKEN",
                    valueFrom: {
                      secretKeyRef: {
                        name: "tenant-controller-credentials",
                        key: "DIGITALOCEAN_TOKEN",
                      },
                    },
                  },
                ],
                resources: {
                  requests: { cpu: "100m", memory: "128Mi" },
                  limits: { cpu: "500m", memory: "256Mi" },
                },
                livenessProbe: {
                  httpGet: { path: "/healthz", port: 8081 },
                  initialDelaySeconds: 15,
                },
                readinessProbe: {
                  httpGet: { path: "/readyz", port: 8081 },
                  initialDelaySeconds: 5,
                },
              },
            ],
          },
        },
      },
    },
    {
      provider,
      dependsOn: [ns, crds, binding, controllerKubeconfig, controllerCredentials],
    },
  );
}

/**
 * Registers the runtime cluster in ArgoCD as a deploy target.
 * ArgoCD discovers clusters via labeled Secrets in its namespace.
 *
 * DOKS kubeconfigs use bearer tokens (not client certs or AWS auth).
 * We parse the kubeconfig YAML to extract the token and CA certificate.
 */
function registerRuntimeCluster(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  argocd: k8s.helm.v3.Release,
): void {
  // Extract bearerToken and caData from the DOKS kubeconfig.
  // DOKS kubeconfigs embed: clusters[0].cluster.certificate-authority-data + users[0].user.token
  const argocdConfig = infra.runtimeCluster.kubeconfig.apply((kc) => {
    // Parse YAML kubeconfig (simple regex extraction — avoids yaml dependency)
    const tokenMatch = kc.match(/token:\s*(.+)/);
    const caMatch = kc.match(/certificate-authority-data:\s*(.+)/);

    const token = tokenMatch?.[1]?.trim() ?? "";
    const caData = caMatch?.[1]?.trim() ?? "";

    return JSON.stringify({
      bearerToken: token,
      tlsClientConfig: {
        caData,
      },
    });
  });

  new k8s.core.v1.Secret(
    "argocd-runtime-cluster",
    {
      metadata: {
        name: "runtime-cluster",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "argocd.argoproj.io/secret-type": "cluster",
        },
      },
      stringData: {
        name: "runtime",
        server: infra.runtimeCluster.endpoint,
        config: argocdConfig,
      },
    },
    { provider, dependsOn: [ns, argocd] },
  );
}

// ---------------------------------------------------------------------------
// Observability
// ---------------------------------------------------------------------------

function installPrometheus(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "prometheus",
    {
      name: "prometheus",
      chart: "prometheus",
      version: HELM_VERSIONS.prometheus,
      repositoryOpts: {
        repo: "https://prometheus-community.github.io/helm-charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "prometheus",
        // Disable components not needed for platform monitoring
        alertmanager: { enabled: false },
        "prometheus-pushgateway": { enabled: false },
        // Server — central metrics store
        server: {
          retention: "15d",
          persistentVolume: {
            enabled: true,
            size: "10Gi",
          },
          resources: {
            requests: { cpu: "200m", memory: "512Mi" },
            limits: { cpu: "500m", memory: "1Gi" },
          },
        },
        // kube-state-metrics — K8s object metrics (pods, deployments, etc.)
        "kube-state-metrics": {
          fullnameOverride: "kube-state-metrics",
        },
        // node-exporter — host-level metrics (CPU, memory, disk)
        "prometheus-node-exporter": {
          fullnameOverride: "node-exporter",
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

function installGrafana(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "grafana",
    {
      name: "grafana",
      chart: "grafana",
      version: HELM_VERSIONS.grafana,
      repositoryOpts: {
        repo: "https://grafana.github.io/helm-charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        // Deterministic Service name — IngressRoute references "grafana" on port 3000.
        fullnameOverride: "grafana",
        service: {
          type: "ClusterIP",
          port: 3000,
        },
        // Data sources pre-configured — no manual setup needed.
        datasources: {
          "datasources.yaml": {
            apiVersion: 1,
            datasources: [
              {
                name: "Prometheus",
                type: "prometheus",
                url: `http://prometheus-server.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
                access: "proxy",
                isDefault: true,
              },
              // Loki runs on the build cluster. Grafana accesses it cross-cluster
              // via the build cluster node IP + NodePort 30100.
              // The IP is updated by infra/scripts/update-cross-cluster-ips.sh
              // (same pattern as Prometheus cross-cluster scraping).
              // For initial setup, use placeholder — update after clusters are ready.
              // {
              //   name: "Loki",
              //   type: "loki",
              //   url: "http://<build-cluster-ip>:30100",
              //   access: "proxy",
              //   isDefault: false,
              // },
            ],
          },
        },
        // Default dashboards for Kubernetes monitoring
        dashboardProviders: {
          "dashboardproviders.yaml": {
            apiVersion: 1,
            providers: [
              {
                name: "default",
                orgId: 1,
                folder: "",
                type: "file",
                disableDeletion: false,
                editable: true,
                options: {
                  path: "/var/lib/grafana/dashboards/default",
                },
              },
            ],
          },
        },
        dashboards: {
          default: {
            // Kubernetes cluster overview
            "k8s-cluster": {
              gnetId: 7249,
              revision: 1,
              datasource: "Prometheus",
            },
            // Node exporter dashboard
            "node-exporter": {
              gnetId: 1860,
              revision: 37,
              datasource: "Prometheus",
            },
            // Kubernetes pods dashboard
            "k8s-pods": {
              gnetId: 6417,
              revision: 1,
              datasource: "Prometheus",
            },
          },
        },
        persistence: {
          enabled: true,
          size: "5Gi",
        },
        adminUser: "admin",
        "grafana.ini": {
          server: {
            domain: `grafana.${config.domain}`,
            root_url: `https://grafana.${config.domain}/`,
            serve_from_sub_path: false,
          },
          // Anonymous read access for dashboards — simplifies team onboarding
          "auth.anonymous": {
            enabled: true,
            org_role: "Viewer",
          },
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "300m", memory: "256Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

/**
 * Reads the Traefik LoadBalancer Service to get the external endpoint.
 *
 * Pulumi's K8s provider waits for the LB to be assigned before
 * marking the Service as ready, so this output resolves only
 * after the cloud provider has allocated an IP/hostname.
 *
 * DigitalOcean returns an IP; AWS returns a hostname (ELB).
 */
function lookupLoadBalancerEndpoint(
  role: string,
  provider: k8s.Provider,
  traefik: k8s.helm.v3.Release,
): pulumi.Output<string> {
  // Service name is deterministic: fullnameOverride="{role}-traefik"
  // is set in installTraefik, so the K8s Service name matches exactly.
  const svc = k8s.core.v1.Service.get(
    `${role}-traefik-lb-lookup`,
    pulumi.interpolate`${PLATFORM_NAMESPACE}/${role}-traefik`,
    { provider, dependsOn: [traefik] },
  );

  return svc.status.apply((status) => {
    const ingress = status?.loadBalancer?.ingress?.[0];
    return ingress?.ip ?? ingress?.hostname ?? "";
  });
}

// ---------------------------------------------------------------------------
// TLS — Let's Encrypt via cert-manager + Traefik IngressRoute
// ---------------------------------------------------------------------------

/**
 * Creates a ClusterIssuer for Let's Encrypt production certificates.
 * Uses HTTP-01 challenge solved by Traefik's ingress class.
 */
function createClusterIssuer(
  provider: k8s.Provider,
  certManager: k8s.helm.v3.Release,
  config: PlatformStackConfig,
  role: string = "mgmt",
): k8s.apiextensions.CustomResource {
  return new k8s.apiextensions.CustomResource(
    `${role}-letsencrypt-prod`,
    {
      apiVersion: "cert-manager.io/v1",
      kind: "ClusterIssuer",
      metadata: {
        name: "letsencrypt-prod",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/environment": config.environment,
        },
      },
      spec: {
        acme: {
          email: `admin@${config.dnsZone}`,
          server: "https://acme-v02.api.letsencrypt.org/directory",
          privateKeySecretRef: {
            name: "letsencrypt-prod-account-key",
          },
          solvers: [
            {
              http01: {
                ingress: {
                  class: "traefik",
                },
              },
            },
          ],
        },
      },
    },
    { provider, dependsOn: [certManager] },
  );
}

/**
 * Platform subdomains that need TLS IngressRoutes on the management cluster.
 *
 * ArgoCD server runs on port 80 (insecure mode, TLS terminated at Traefik).
 * Other services will be added as they are deployed.
 */
const PLATFORM_INGRESS_ROUTES = [
  { subdomain: "argocd", serviceName: "argocd-server", servicePort: 80 },
  { subdomain: "git", serviceName: "gitea-http", servicePort: 3000 },
  { subdomain: "grafana", serviceName: "grafana", servicePort: 3000 },
  { subdomain: "api", serviceName: "loopzero-api", servicePort: 8080 },
] as const;

/**
 * Creates TLS-terminated IngressRoutes for platform services.
 *
 * Each route:
 * 1. Requests a Certificate from Let's Encrypt via the ClusterIssuer
 * 2. Creates an IngressRoute with TLS passthrough to the service
 *
 * Services that don't exist yet (grafana, api) are skipped —
 * they'll get routes when deployed in later sprints.
 */
function createPlatformIngressRoutes(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): pulumi.Output<string[]> {
  const createdRoutes: pulumi.Output<string>[] = [];

  for (const route of PLATFORM_INGRESS_ROUTES) {
    const fqdn =
      config.domain === config.dnsZone
        ? `${route.subdomain}.${config.domain}`
        : `${route.subdomain}.${config.domain}`;

    const certName = `tls-${route.subdomain}`;

    // Certificate resource — cert-manager watches this and issues a cert
    const cert = new k8s.apiextensions.CustomResource(
      `cert-${route.subdomain}`,
      {
        apiVersion: "cert-manager.io/v1",
        kind: "Certificate",
        metadata: {
          name: certName,
          namespace: PLATFORM_NAMESPACE,
        },
        spec: {
          secretName: `${certName}-secret`,
          issuerRef: {
            name: "letsencrypt-prod",
            kind: "ClusterIssuer",
          },
          dnsNames: [fqdn],
        },
      },
      { provider, dependsOn: [clusterIssuer] },
    );

    // IngressRoute — Traefik CRD for routing with TLS
    const ingressRoute = new k8s.apiextensions.CustomResource(
      `ingress-${route.subdomain}`,
      {
        apiVersion: "traefik.io/v1alpha1",
        kind: "IngressRoute",
        metadata: {
          name: `${route.subdomain}-ingress`,
          namespace: PLATFORM_NAMESPACE,
          labels: {
            "app.kubernetes.io/managed-by": "pulumi",
            "app.kubernetes.io/part-of": "loopzero-platform",
          },
        },
        spec: {
          entryPoints: ["websecure"],
          routes: [
            {
              match: `Host(\`${fqdn}\`)`,
              kind: "Rule",
              services: [
                {
                  name: route.serviceName,
                  port: route.servicePort,
                  namespace: PLATFORM_NAMESPACE,
                },
              ],
            },
          ],
          tls: {
            secretName: `${certName}-secret`,
          },
        },
      },
      { provider, dependsOn: [cert, traefik] },
    );

    createdRoutes.push(ingressRoute.metadata.name);
  }

  return pulumi.all(createdRoutes);
}
