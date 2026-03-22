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
  readonly minioReady: pulumi.Output<boolean>;
  readonly loopzeroApiReady: pulumi.Output<boolean>;
  readonly tenantControllerReady: pulumi.Output<boolean>;
  readonly tlsConfigured: pulumi.Output<boolean>;
}

export interface BuildBootstrapOutputs {
  readonly argoWorkflowsReady: pulumi.Output<boolean>;
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
  const mgmt = bootstrapMgmtCluster(infra, config);
  const build = bootstrapBuildCluster(infra, config);
  const runtime = bootstrapRuntimeCluster(infra, config);

  return { mgmt, build, runtime };
}

// ---------------------------------------------------------------------------
// Management cluster bootstrap
// ---------------------------------------------------------------------------

function bootstrapMgmtCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
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

  // Prometheus — metrics collection (kube-state-metrics, node-exporter, cAdvisor)
  const prometheus = installPrometheus(provider, ns);

  // Grafana — dashboards and visualization, pre-configured with Prometheus data source
  const grafana = installGrafana(provider, ns, config);

  // MinIO — S3-compatible artifact storage (source bundles, build cache, images)
  const minio = installMinIO(provider, ns);

  // LoopZero API — platform HTTP server (connects to DB, Gitea, ArgoCD, Argo Workflows)
  const loopzeroApi = installLoopZeroAPI(provider, ns, infra, config);

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
    minioReady: minio.status.apply((s) => s.status === "deployed"),
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

  // Runtime gets Traefik only — user apps ingress
  const traefik = installTraefik("runtime", provider, ns);

  const ingressEndpoint = lookupLoadBalancerEndpoint(
    "runtime",
    provider,
    traefik,
  );

  return {
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
  };
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

  const argoWorkflows = new k8s.helm.v3.Release(
    "argo-workflows",
    {
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

  return {
    argoWorkflowsReady: argoWorkflows.status.apply(
      (s) => s.status === "deployed",
    ),
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

function installLoopZeroAPI(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
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
        // Gitea admin token — created via `gitea admin user generate-access-token`
        GITEA_TOKEN: "REDACTED_SECRET_026",
        // ArgoCD admin password — from argocd-initial-admin-secret
        ARGOCD_TOKEN: infra.mgmtCluster.kubeconfig.apply(() => {
          // Read at deploy time; for now use the known password
          return "REDACTED_SECRET_059";
        }),
        // MinIO credentials
        MINIO_ACCESS_KEY: "loopzero-admin",
        MINIO_SECRET_KEY: "loopzero-minio-dev-2026",
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
        MINIO_ENDPOINT: `minio.${PLATFORM_NAMESPACE}.svc.cluster.local:9000`,
        REGISTRY_URL: "registry.digitalocean.com/loopzero-infra-registry",
        REGISTRY_INSECURE: "false",
        ARGOCD_NAMESPACE: PLATFORM_NAMESPACE,
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
                ],
                volumeMounts: [
                  {
                    name: "kubeconfig",
                    mountPath: "/etc/loopzero",
                    readOnly: true,
                  },
                ],
                resources: {
                  requests: { cpu: "200m", memory: "256Mi" },
                  limits: { cpu: "1", memory: "512Mi" },
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
        // LoopZero CRDs — all custom resources managed by the operator
        {
          apiGroups: ["loopzero.io"],
          resources: [
            "tenants", "tenants/status", "tenants/finalizers",
            "clustercredentials", "clustercredentials/status", "clustercredentials/finalizers",
            "appsecrets", "appsecrets/status", "appsecrets/finalizers",
            "previewenvironments", "previewenvironments/status", "previewenvironments/finalizers",
            "reconciliationreports", "reconciliationreports/status", "reconciliationreports/finalizers",
          ],
          verbs: ["get", "list", "watch", "create", "update", "patch", "delete"],
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

  return new k8s.apps.v1.Deployment(
    "tenant-controller",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "tenant-controller" },
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
                    value: "REDACTED_SECRET_026",
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
                    value: "https://nyc3.digitaloceanspaces.com",
                  },
                  {
                    name: "SPACES_ACCESS_KEY_ID",
                    value: "REDACTED_SECRET_049",
                  },
                  {
                    name: "SPACES_SECRET_ACCESS_KEY",
                    value: "REDACTED_SECRET_022",
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
    { provider, dependsOn: [ns, crds, binding, controllerKubeconfig] },
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
        // Prometheus data source pre-configured — no manual setup needed.
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
): k8s.apiextensions.CustomResource {
  return new k8s.apiextensions.CustomResource(
    "letsencrypt-prod",
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
