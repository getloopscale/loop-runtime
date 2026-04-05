// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";
import { InfraOutputs, ClusterOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { InfraError } from "../core/errors";
import { installPrometheus, installGrafana, installTempo } from "./mgmt-observability";
import {
  PLATFORM_NAMESPACE,
  HELM_VERSIONS,
  MgmtBootstrapOutputs,
  createPlatformNamespace,
  installCertManager,
  installTraefik,
  lookupLoadBalancerEndpoint,
  createClusterIssuer,
  parseKubeconfig,
} from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Management cluster bootstrap
// ---------------------------------------------------------------------------

export function bootstrapMgmtCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
  lokiEndpoint: pulumi.Output<string>,
): MgmtBootstrapOutputs {
  // REVIEW-2026-03-30: Fixes CRIT-01 — fail-fast if production deploys without
  // explicit passwords. Dev defaults are acceptable only for non-prod environments.
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

  // cert-manager first — ArgoCD and Traefik may need TLS CRDs
  const certManager = installCertManager("mgmt", provider, ns);

  // ArgoCD — GitOps engine, manages runtime cluster
  const argocd = installArgoCD(provider, ns, certManager);

  // Gitea — internal git server for desired state (ArgoCD reads from here)
  const gitea = installGitea(provider, ns, infra, config);

  // MinIO — S3-compatible artifact storage (source bundles, build cache)
  const minio = installMinIO(provider, ns, config);

  // MinIO credentials — needed by API for source bundle upload/download.
  // Credentials MUST match the rootUser/rootPassword in installMinIO() Helm values.
  // In production, minioRootPassword is set via: pulumi config set --secret minioRootPassword
  const minioPassword = config.minioRootPassword ?? pulumi.output("loopzero-minio-dev-2026");
  new k8s.core.v1.Secret(
    "minio-credentials-mgmt",
    {
      metadata: {
        name: "loopzero-minio-credentials",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        AWS_ACCESS_KEY_ID: "loopzero-admin",
        AWS_SECRET_ACCESS_KEY: minioPassword,
        MINIO_ENDPOINT: `http://minio.${PLATFORM_NAMESPACE}.svc.cluster.local:9000`,
      },
    },
    { provider, dependsOn: [minio] },
  );

  // Ory Kratos — identity management (signup, login, recovery)
  const oryKratos = installOryKratos(provider, ns, infra, config);

  // Ory Hydra — OAuth2 server (JWT tokens, Device Code Flow, PKCE)
  const oryHydra = installOryHydra(provider, ns, infra, config, oryKratos);

  // Prometheus — metrics collection (kube-state-metrics, node-exporter, cAdvisor)
  const prometheus = installPrometheus(provider, ns);

  // Grafana — dashboards and visualization, pre-configured with Prometheus + Loki data sources
  const grafana = installGrafana(provider, ns, config);

  // Tempo — distributed trace backend (MEET-2026-03-30-020)
  const tempo = installTempo(provider, ns, config);

  // Gitea bootstrap — creates admin token, orgs, and stores credentials in K8s Secret.
  // The Job runs after Gitea is ready and is idempotent (safe to re-run).
  bootstrapGitea(provider, ns, gitea, config);

  // ArgoCD repo credentials — allows ArgoCD to authenticate to Gitea repos.
  createArgoCDRepoCredentials(provider, ns, argocd, gitea);

  // LoopZero API — platform HTTP server (connects to DB, Gitea, ArgoCD, Argo Workflows)
  const loopzeroApi = installLoopZeroAPI(provider, ns, infra, config, lokiEndpoint);

  // Cloud Auth Service — OAuth2 (Hydra), identity (Kratos), billing (Stripe), dashboard
  installCloudService(provider, ns, infra, config);

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
  // Webhook readiness gate via Deployment.get() in createClusterIssuer (MEET-2026-04-01-025 #5)
  const clusterIssuer = createClusterIssuer(provider, certManager, config);

  // TLS IngressRoutes for platform services
  const tlsRoutes = createPlatformIngressRoutes(
    provider,
    config,
    traefik,
    clusterIssuer,
  );

  // API Gateway — path-based routing under api.{domain}
  // Serves loopzero-api, loopzero-cloud, and dashboard from a single domain.
  createAPIGatewayRoute(provider, config, traefik, clusterIssuer);

  // REVIEW-2026-03-30: Fixes HIGH-01 — NetworkPolicies for production.
  // Local dev uses helm/network-policies/loopzero-system.yaml (namespace: loopzero-system).
  // Production uses platform-system namespace — these Pulumi resources ensure
  // network isolation is applied correctly.
  createPlatformNetworkPolicies(provider, ns);

  return {
    certManagerReady: certManager.status.apply((s) => s.status === "deployed"),
    argoCdReady: argocd.status.apply((s) => s.status === "deployed"),
    giteaReady: gitea.status.apply((s) => s.status === "deployed"),
    prometheusReady: prometheus.status.apply((s) => s.status === "deployed"),
    grafanaReady: grafana.status.apply((s) => s.status === "deployed"),
    loopzeroApiReady: loopzeroApi.metadata.name.apply((n) => !!n),
    tenantControllerReady: tenantController.metadata.name.apply((n) => !!n),
    oryKratosReady: oryKratos.status.apply((s) => s.status === "deployed"),
    oryHydraReady: oryHydra.status.apply((s) => s.status === "deployed"),
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    tlsConfigured: tlsRoutes.apply((routes) => routes.length > 0),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
  };
}

// ---------------------------------------------------------------------------
// ArgoCD
// ---------------------------------------------------------------------------

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

        global: {
          image: { tag: "v2.13.3" },
        },

        // Server accessed via Traefik — TLS terminated at ingress
        server: {
          service: { type: "ClusterIP" },
          containerSecurityContext: { runAsNonRoot: true },
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { cpu: "500m", memory: "256Mi" },
          },
          metrics: {
            enabled: true,
            serviceMonitor: {
              enabled: true,
              additionalLabels: { release: "monitoring" },
              namespace: "monitoring",
            },
          },
        },

        configs: {
          params: {
            // TLS terminated at Traefik, ArgoCD serves plain HTTP
            "server.insecure": true,
            // Manual sync only — API controls when to sync, not ArgoCD auto-sync
            "controller.self.heal.enabled": "false",
            // MEET-2026-03-29-019: Terminate stuck sync operations after 5 minutes.
            "controller.operation.processing.timeout": "300",
          },
          rbac: {
            // LoopZero API: sync + get + action (terminate stuck operations).
            // MEET-2026-03-29-019: action/* needed for TerminateOperation.
            "policy.csv": `p, loopzero-api, applications, sync, */*, allow
p, loopzero-api, applications, get, */*, allow
p, loopzero-api, applications, action/*, */*, allow`,
          },
          // repositories: Gitea repo credentials are created by the Bootstrap step
          // in infra-deploy.yml with real Gitea admin tokens. Do NOT configure
          // repos here — Helm creates secrets WITHOUT credentials, and ArgoCD
          // prioritizes repository-type secrets over repo-creds templates,
          // causing "authentication required" errors.
          cm: {
            // Dedicated service account for LoopZero API — apiKey only, no login.
            // Tokens generated with `argocd account generate-token` never expire.
            // MEET-2026-03-28-013: replaces old pattern of using admin session tokens (24h expiry).
            "accounts.loopzero-api": "apiKey",
            // Health assessment for Application resources
            "resource.customizations.health.argoproj.io_Application": `hs = {}
hs.status = "Progressing"
hs.message = ""
if obj.status ~= nil then
  if obj.status.health ~= nil then
    hs.status = obj.status.health.status
    if obj.status.health.message ~= nil then
      hs.message = obj.status.health.message
    end
  end
end
return hs`,
          },
        },

        // ArgoCD Notifications — push-based deploy activation (MEET-2026-03-27-012).
        // When an Application reaches Synced + Healthy, ArgoCD calls the API
        // callback endpoint. Replaces Phase D synchronous polling.
        // NOTE: These keys are TOP-LEVEL in the argo-cd Helm chart, NOT under configs.
        notificationsController: { enabled: true },
        notifications: {
          // IMPORTANT: the argo-cd Helm chart v7.x uses "notifiers" (not "services")
          // for the notifications ConfigMap. "services" is silently ignored.
          notifiers: {
            // MEET-2026-04-04-029 P0: Authorization header must match LOOPZERO_ARGO_CALLBACK_SECRET.
            // TODO (Sprint 6): inject from Kubernetes Secret instead of hardcode.
            "service.webhook.loopzero-api": `url: http://loopzero-api.${PLATFORM_NAMESPACE}.svc.cluster.local:8080/internal/argocd-callback
headers:
- name: Content-Type
  value: application/json
- name: Authorization
  value: "Bearer cb-loopzero-dev-2026"`,
          },
          templates: {
            "template.sync-completed": `webhook:
  loopzero-api:
    method: POST
    body: |
      {"app_name": "{{.app.metadata.name}}", "sync_status": "{{.app.status.sync.status}}", "health_status": "{{.app.status.health.status}}", "revision": "{{.app.status.sync.revision}}"}`,
          },
          triggers: {
            "trigger.on-sync-healthy": `- when: app.status.sync.status == 'Synced' && app.status.health.status == 'Healthy'
  send: [sync-completed]`,
          },
        },

        // Controller manages syncs to runtime cluster
        controller: {
          containerSecurityContext: { runAsNonRoot: true },
          resources: {
            requests: { cpu: "100m", memory: "256Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
          },
          metrics: {
            enabled: true,
            serviceMonitor: {
              enabled: true,
              additionalLabels: { release: "monitoring" },
              namespace: "monitoring",
            },
          },
        },

        // Repo server clones from Gitea
        repoServer: {
          containerSecurityContext: { runAsNonRoot: true },
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
          },
          metrics: {
            enabled: true,
            serviceMonitor: {
              enabled: true,
              additionalLabels: { release: "monitoring" },
              namespace: "monitoring",
            },
          },
        },

        // Redis for caching
        redis: {
          containerSecurityContext: { runAsNonRoot: true },
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

// ---------------------------------------------------------------------------
// Gitea
// ---------------------------------------------------------------------------

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
            password: config.giteaAdminPassword ?? GITEA_ADMIN_PASSWORD_DEFAULT,
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
// MinIO — S3-compatible artifact storage on mgmt cluster
// ---------------------------------------------------------------------------

function installMinIO(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
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
        // Production overrides via: pulumi config set --secret minioRootPassword <password>
        rootPassword: config.minioRootPassword ?? "loopzero-minio-dev-2026",
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
        // Auto-create buckets for platform use.
        // The MinIO chart creates a post-install hook Job to create these buckets.
        // The hook Job needs to connect to MinIO pod on port 9000.
        buckets: [
          { name: "loopzero-artifacts", policy: "none" },
          { name: "loopzero-builds", policy: "none" },
        ],
        // Increase post-install hook timeout — on fresh clusters with slow storage
        // provisioning, the default 60s may not be enough for MinIO to start + create buckets.
        postJob: {
          activeDeadlineSeconds: 300,
        },
      },
    },
    {
      provider,
      dependsOn: [ns],
      // Increase timeout: MinIO make-bucket-job may take time on fresh clusters.
      customTimeouts: { create: "10m", update: "10m" },
    },
  );
}

// ---------------------------------------------------------------------------
// Ory — Identity (Kratos) + OAuth2 (Hydra) for LoopZero Cloud auth
// ---------------------------------------------------------------------------

/**
 * Ory Kratos — Identity management (signup, login, recovery, verification).
 * Uses the managed PostgreSQL instance with a dedicated `kratos` database.
 * Configured with email+password and OIDC (GitHub, Google) identity methods.
 */
function installOryKratos(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  const kratosValuesPath = path.resolve(
    __dirname,
    "../../../helm/ory-kratos/values.yaml",
  );
  const kratosValues = fs.existsSync(kratosValuesPath)
    ? kratosValuesPath
    : undefined;

  // Build DSN for Kratos database.
  // Production: managed PostgreSQL (sslmode=require).
  // Local dev: falls back to values.yaml default.
  const kratosDsn = infra.database
    ? pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/kratos?sslmode=require`
    : undefined;

  return new k8s.helm.v3.Release(
    "ory-kratos",
    {
      name: "ory-kratos",
      chart: "kratos",
      version: HELM_VERSIONS.oryKratos,
      repositoryOpts: {
        repo: "https://k8s.ory.sh/helm/charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      ...(kratosValues ? { valueYamlFiles: [new pulumi.asset.FileAsset(kratosValues)] } : {}),
      values: {
        fullnameOverride: "ory-kratos",
        kratos: {
          config: {
            ...(kratosDsn ? { dsn: kratosDsn } : {}),
            // SMTP for email delivery — verification and recovery emails.
            // When not set, falls back to values.yaml default (stdout in dev).
            ...(config.smtpConnectionUri
              ? {
                  courier: {
                    smtp: {
                      connection_uri: config.smtpConnectionUri,
                      from_address: `noreply@${config.domain}`,
                      from_name: "LoopZero",
                    },
                  },
                }
              : {}),
            // Kratos must know its public URL for CSRF, return_to validation, etc.
            serve: {
              public: {
                base_url: `https://accounts.${config.domain}/`,
              },
            },
            // Cookies must be accessible across subdomains (dashboard, accounts, api)
            // so the Kratos session cookie is sent with fetch requests from the dashboard.
            cookies: {
              domain: `.${config.domain}`,
              same_site: "Lax",
            },
            // Allow return_to URLs from dashboard and API (consent handler).
            // MEET-2026-04-05-032: Kratos UI at accounts.{domain} (same-origin, no CORS).
            // Dashboard is a pure PKCE client — never renders Kratos flows.
            selfservice: {
              default_browser_return_url: `https://dashboard.${config.domain}/`,
              allowed_return_urls: [
                `https://dashboard.${config.domain}/`,
                `https://api.${config.domain}/`,
                `https://accounts.${config.domain}/`,
              ],
              flows: {
                login: { ui_url: `https://accounts.${config.domain}/login` },
                registration: { ui_url: `https://accounts.${config.domain}/registration` },
                verification: { ui_url: `https://accounts.${config.domain}/verification`, enabled: true },
                recovery: { ui_url: `https://accounts.${config.domain}/recovery`, enabled: true },
                settings: { ui_url: `https://accounts.${config.domain}/settings` },
                error: { ui_url: `https://accounts.${config.domain}/error` },
                logout: { after: { default_browser_return_url: `https://dashboard.${config.domain}/login` } },
              },
              // Social login providers — only included when secrets are configured.
              ...(config.githubOAuthClientId || config.googleOAuthClientId
                ? {
                    methods: {
                      oidc: {
                        enabled: true,
                        config: {
                          base_redirect_uri: `https://accounts.${config.domain}/`,
                          providers: [
                            ...(config.githubOAuthClientId
                              ? [
                                  {
                                    id: "github",
                                    provider: "github",
                                    client_id: config.githubOAuthClientId,
                                    client_secret: config.githubOAuthClientSecret,
                                    mapper_url:
                                      "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dFZhcignY2xhaW1zJyk7CnsKICBpZGVudGl0eTogewogICAgdHJhaXRzOiB7CiAgICAgIGVtYWlsOiBjbGFpbXMuZW1haWwsCiAgICAgIFtpZiBzdGQub2JqZWN0SGFzKGNsYWltcywgJ25hbWUnKSAmJiBjbGFpbXMubmFtZSAhPSBudWxsIHRoZW4gJ25hbWUnXTogY2xhaW1zLm5hbWUsCiAgICB9LAogIH0sCn0K",
                                    scope: ["user:email"],
                                  },
                                ]
                              : []),
                            ...(config.googleOAuthClientId
                              ? [
                                  {
                                    id: "google",
                                    provider: "google",
                                    client_id: config.googleOAuthClientId,
                                    client_secret: config.googleOAuthClientSecret,
                                    mapper_url:
                                      "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dFZhcignY2xhaW1zJyk7CnsKICBpZGVudGl0eTogewogICAgdHJhaXRzOiB7CiAgICAgIGVtYWlsOiBjbGFpbXMuZW1haWwsCiAgICAgIFtpZiBzdGQub2JqZWN0SGFzKGNsYWltcywgJ25hbWUnKSAmJiBjbGFpbXMubmFtZSAhPSBudWxsIHRoZW4gJ25hbWUnXTogY2xhaW1zLm5hbWUsCiAgICB9LAogIH0sCn0K",
                                    scope: ["email", "profile"],
                                  },
                                ]
                              : []),
                          ],
                        },
                      },
                    },
                  }
                : {}),
            },
          },
        },
        resources: {
          requests: { cpu: "50m", memory: "128Mi" },
          limits: { cpu: "300m", memory: "256Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );

  // MEET-2026-04-05-032: Kratos Self-Service UI — renders login/registration/recovery forms.
  // Deployed at accounts.{domain} (same origin as Kratos API — no CORS issues).
  // Uses ory/kratos-selfservice-ui-node (official, maintained by Ory).
  const kratosUiImage = "oryd/kratos-selfservice-ui-node:v1.3.0";

  new k8s.core.v1.Service(
    "kratos-ui",
    {
      metadata: { name: "kratos-ui", namespace: PLATFORM_NAMESPACE },
      spec: {
        selector: { app: "kratos-ui" },
        ports: [{ name: "http", port: 3000, targetPort: 3000 }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apps.v1.Deployment(
    "kratos-ui",
    {
      metadata: { name: "kratos-ui", namespace: PLATFORM_NAMESPACE },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "kratos-ui" } },
        template: {
          metadata: { labels: { app: "kratos-ui" } },
          spec: {
            containers: [{
              name: "ui",
              image: kratosUiImage,
              ports: [{ containerPort: 3000 }],
              env: [
                // Kratos public URL (internal — same cluster, no TLS needed)
                { name: "KRATOS_PUBLIC_URL", value: `http://ory-kratos-public.${PLATFORM_NAMESPACE}.svc.cluster.local:80` },
                // Hydra admin URL (for consent/login challenge resolution)
                { name: "HYDRA_ADMIN_URL", value: `http://ory-hydra-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:4445` },
                // Base URL for CSRF cookie domain
                { name: "KRATOS_BROWSER_URL", value: `https://accounts.${config.domain}` },
                { name: "PORT", value: "3000" },
                { name: "SECURITY_MODE", value: "" },
                { name: "COOKIE_SECRET", value: "loopzero-kratos-ui-cookie-secret-32chars!" },
                { name: "CSRF_COOKIE_NAME", value: "loopzero_csrf" },
              ],
              resources: {
                requests: { cpu: "20m", memory: "32Mi" },
                limits: { cpu: "100m", memory: "64Mi" },
              },
              readinessProbe: {
                httpGet: { path: "/health/alive", port: 3000 },
                initialDelaySeconds: 5,
                periodSeconds: 10,
              },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

/**
 * Ory Hydra — OAuth2 / OpenID Connect server.
 * Issues JWT access tokens (RFC 8628 Device Code Flow for CLI, PKCE for dashboard).
 * Uses the managed PostgreSQL instance with a dedicated `hydra` database.
 */
function installOryHydra(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  kratos: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  const hydraValuesPath = path.resolve(
    __dirname,
    "../../../helm/ory-hydra/values.yaml",
  );
  const hydraValues = fs.existsSync(hydraValuesPath)
    ? hydraValuesPath
    : undefined;

  // Build DSN for Hydra database.
  const hydraDsn = infra.database
    ? pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/hydra?sslmode=require`
    : undefined;

  // Issuer URL must match the public-facing auth domain.
  // Always set — dev mode uses auth.dev.loopzero.example, prod uses auth.loopzero.example.
  const authDomain = config.domain || "dev.loopzero.example";
  const issuerUrl = `https://auth.${authDomain}/`;

  return new k8s.helm.v3.Release(
    "ory-hydra",
    {
      name: "ory-hydra",
      chart: "hydra",
      version: HELM_VERSIONS.oryHydra,
      repositoryOpts: {
        repo: "https://k8s.ory.sh/helm/charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      ...(hydraValues ? { valueYamlFiles: [new pulumi.asset.FileAsset(hydraValues)] } : {}),
      values: {
        fullnameOverride: "ory-hydra",
        hydra: {
              dev: false, // Disable dev mode — use real HTTPS issuer
              config: {
                ...(hydraDsn ? { dsn: hydraDsn } : {}),
                urls: {
                  self: { issuer: issuerUrl },
                  // Cloud service handles login/consent/logout flows
                  login: `https://api.${authDomain}/auth/login`,
                  consent: `https://api.${authDomain}/auth/consent`,
                  logout: `https://api.${authDomain}/auth/logout`,
                },
                serve: {
                  public: {
                    cors: {
                      enabled: true,
                      allowed_origins: [
                        `https://dashboard.${authDomain}`,
                        `https://*.${authDomain}`,
                      ],
                    },
                  },
                },
              },
            },
        resources: {
          requests: { cpu: "50m", memory: "128Mi" },
          limits: { cpu: "300m", memory: "256Mi" },
        },
      },
    },
    // Hydra depends on Kratos being deployed first (same auth system)
    { provider, dependsOn: [ns, kratos] },
  );
}

// ---------------------------------------------------------------------------
// Platform application services
// ---------------------------------------------------------------------------

/**
 * Gitea admin password default — only for dev/staging environments.
 * Production MUST override via: pulumi config set --secret giteaAdminPassword <password>
 * REVIEW-2026-03-30: CRIT-01 fail-fast enforced in bootstrapMgmtCluster().
 */
const GITEA_ADMIN_PASSWORD_DEFAULT = "loopzero-gitea-dev-2026";

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
          // Force SSA field ownership AND force replacement.
          // patchForce resolves field manager conflicts between Pulumi sessions.
          // replaceUnready forces Pulumi to delete+recreate (Jobs have immutable spec.template).
          "pulumi.com/patchForce": "true",
          "pulumi.com/replaceUnready": "true",
          // REVIEW-2026-03-30: Fixes MED-07 — was new Date().toISOString() which
          // forced Job recreation on every pulumi up. Now uses environment+version
          // so the Job only re-runs when config actually changes.
          "loopzero.io/run-id": `${config.environment}-${config.infraVersion}`,
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
                image: "bitnami/kubectl:latest", // Bitnami removes versioned tags; latest is the only available tag
                env: [
                  {
                    name: "GITEA_ADMIN_PASS_ENV",
                    value: config.giteaAdminPassword
                      ? pulumi.interpolate`${config.giteaAdminPassword}`
                      : GITEA_ADMIN_PASSWORD_DEFAULT,
                  },
                ],
                command: ["bash", "-c"],
                args: [
                  `set -e
GITEA_URL="http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000"
GITEA_USER="gitea_admin"
GITEA_PASS="\${GITEA_ADMIN_PASS_ENV}"
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
    -d '{"name":"loopzero-platform","scopes":["write:repository","write:organization","read:user"]}' | grep -o '"sha1":"[^"]*"' | cut -d'"' -f4)

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

echo "=== Step 5: Generate ArgoCD apiKey token for loopzero-api account ==="
# MEET-2026-03-28-013: generate a proper apiKey JWT, NOT store the admin password.
# The loopzero-api account is configured in ArgoCD Helm values with capability: apiKey.
# This token has RBAC limited to sync+get on applications (least privilege).
ARGOCD_PASS=""
ARGOCD_TOKEN=""
if kubectl get secret argocd-initial-admin-secret -n "$NS" -o jsonpath='{.data.password}' 2>/dev/null | base64 -d > /tmp/argocd_pass 2>/dev/null; then
  ARGOCD_PASS=$(cat /tmp/argocd_pass)
  rm -f /tmp/argocd_pass
  echo "ArgoCD admin password read successfully"

  # Login as admin to get session token
  ARGOCD_URL="http://argocd-server.\${NS}.svc.cluster.local:80"
  for attempt in $(seq 1 30); do
    ADMIN_SESSION=$(curl -sf "$ARGOCD_URL/api/v1/session" \
      -d "{\\"username\\":\\"admin\\",\\"password\\":\\"$ARGOCD_PASS\\"}" 2>/dev/null | \
      python3 -c "import json,sys; print(json.load(sys.stdin).get('token',''))" 2>/dev/null || true)
    if [ -n "$ADMIN_SESSION" ]; then
      echo "ArgoCD admin session obtained"
      break
    fi
    echo "Waiting for ArgoCD server... (attempt $attempt/30)"
    sleep 5
  done

  if [ -n "$ADMIN_SESSION" ]; then
    # Generate apiKey token for loopzero-api account (non-expiring)
    ARGOCD_TOKEN=$(curl -sf "$ARGOCD_URL/api/v1/account/loopzero-api/token" \
      -H "Authorization: Bearer $ADMIN_SESSION" \
      -d '{"name":"platform-deploy"}' 2>/dev/null | \
      python3 -c "import json,sys; print(json.load(sys.stdin).get('token',''))" 2>/dev/null || true)

    if [ -n "$ARGOCD_TOKEN" ]; then
      echo "ArgoCD apiKey token generated for loopzero-api (JWT, non-expiring)"
    else
      # REVIEW-2026-03-30: Fixes MED-05 — was WARN-only, now fails the Job.
      echo "ERROR: Could not generate ArgoCD apiKey token — account loopzero-api may not exist"
      echo "ERROR: Ensure Helm values have: accounts.loopzero-api: apiKey"
      exit 1
    fi
  else
    echo "ERROR: Could not login to ArgoCD — cannot generate API token"
    exit 1
  fi
else
  echo "ERROR: Could not read ArgoCD admin password"
  exit 1
fi

echo "=== Step 6: Update loopzero-platform-tokens Secret ==="
kubectl create secret generic loopzero-platform-tokens \
  --namespace="$NS" \
  --from-literal="GITEA_TOKEN=$GITEA_TOKEN" \
  --from-literal="ARGOCD_TOKEN=$ARGOCD_TOKEN" \
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
    {
      provider,
      dependsOn: [ns, gitea, bootstrapSa],
      // The Job has ttlSecondsAfterFinished: 300 — Kubernetes auto-deletes it
      // after completion. On next `pulumi up`, the run-id annotation forces a diff,
      // but the old Job is gone. deleteBeforeReplace + replaceOnChanges ensures
      // Pulumi creates a fresh Job instead of trying to update a deleted resource.
      retainOnDelete: true,
      deleteBeforeReplace: true,
      // Jobs have immutable spec.template — any change must be delete+recreate.
      // metadata.annotations changes (run-id) also force replacement to re-run the bootstrap.
      replaceOnChanges: ["metadata.annotations", "spec"],
    },
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
        // REVIEW-2026-03-30: Fixes HIGH-09 — placeholder was "admin" which matches
        // the ArgoCD admin password. Use clearly-invalid placeholder instead.
        ARGOCD_TOKEN: "PLACEHOLDER_NOT_A_TOKEN",
      },
    },
    { provider, dependsOn: [ns] },
  );
}

/**
 * ArgoCD repo credentials — allows ArgoCD to clone from Gitea repos.
 *
 * IMPORTANT: This function intentionally does NOT create the repo-creds secret.
 * The secret is created by the Bootstrap step in infra-deploy.yml with a real
 * Gitea admin token. Creating it here with a placeholder causes ArgoCD to
 * immediately try (and fail) to authenticate before bootstrap runs.
 *
 * The Bootstrap step creates two secrets:
 *   1. gitea-repo-creds (type=repo-creds) — credential template for all repos
 *   2. argocd-repo-tn-loopzero-gitops (type=repository) — explicit repo for bootstrap tenant
 * Both use the real Gitea token generated during bootstrap.
 */
function createArgoCDRepoCredentials(
  _provider: k8s.Provider,
  _ns: k8s.core.v1.Namespace,
  _argocd: k8s.helm.v3.Release,
  _gitea: k8s.helm.v3.Release,
): void {
  // No-op: credentials created by Bootstrap step with real token.
  // See .github/workflows/infra-deploy.yml section "3. UPDATE K8s SECRETS".
}

// ---------------------------------------------------------------------------
// Cloud Auth Service (OAuth2 + Billing + Dashboard)
// ---------------------------------------------------------------------------

function installCloudService(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): void {
  const registrySecretName = "loopzero-infra-registry";
  const imageTag = config.environment || "dev";

  // ConfigMap — non-sensitive environment variables
  const configMap = new k8s.core.v1.ConfigMap(
    "loopzero-cloud-config",
    {
      metadata: { name: "loopzero-cloud-config", namespace: PLATFORM_NAMESPACE },
      data: {
        CLOUD_LISTEN_ADDR: ":13002",
        LOOPZERO_ENV: config.environment || "dev",
        // Kratos public URL — served via cloud gateway (same domain as dashboard, no CORS).
        KRATOS_PUBLIC_URL: `https://cloud.${config.domain || "dev.loopzero.example"}`,
        HYDRA_PUBLIC_URL: `https://auth.${config.domain || "dev.loopzero.example"}`,
        // Internal URLs (server-to-server, never exposed to browser)
        KRATOS_ADMIN_URL: `http://ory-kratos-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        HYDRA_ADMIN_URL: `http://ory-hydra-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:4445`,
        HYDRA_CLIENT_ID: "loopzero-cli",
        ENGINE_API_URL: `http://loopzero-api.${PLATFORM_NAMESPACE}.svc.cluster.local:8080`,
        BILLING_RETURN_URL: `https://dashboard.${config.domain || "dev.loopzero.example"}`,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Secret — Stripe API keys (empty in dev, set via pulumi config in prod)
  const secret = new k8s.core.v1.Secret(
    "loopzero-cloud-secrets",
    {
      metadata: { name: "loopzero-cloud-secrets", namespace: PLATFORM_NAMESPACE },
      stringData: {
        STRIPE_API_KEY: config.stripeApiKey || "",
        STRIPE_WEBHOOK_SECRET: config.stripeWebhookSecret || "",
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Service
  new k8s.core.v1.Service(
    "loopzero-cloud",
    {
      metadata: { name: "loopzero-cloud", namespace: PLATFORM_NAMESPACE },
      spec: {
        selector: { app: "loopzero-cloud" },
        ports: [{ name: "http", port: 13002, targetPort: 13002 }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Deployment
  new k8s.apps.v1.Deployment(
    "loopzero-cloud",
    {
      metadata: { name: "loopzero-cloud", namespace: PLATFORM_NAMESPACE },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "loopzero-cloud" } },
        template: {
          metadata: { labels: { app: "loopzero-cloud" } },
          spec: {
            serviceAccountName: "default",
            imagePullSecrets: [{ name: registrySecretName }],
            containers: [{
              name: "cloud",
              image: `registry.digitalocean.com/loopzero-infra-registry/loopzero-cloud:${imageTag}`,
              imagePullPolicy: "Always",
              ports: [{ containerPort: 13002 }],
              envFrom: [
                { configMapRef: { name: "loopzero-cloud-config" } },
                { secretRef: { name: "loopzero-cloud-secrets" } },
              ],
              resources: {
                requests: { cpu: "50m", memory: "64Mi" },
                limits: { cpu: "200m", memory: "128Mi" },
              },
              readinessProbe: {
                httpGet: { path: "/health", port: 13002 },
                initialDelaySeconds: 5,
                periodSeconds: 10,
              },
              livenessProbe: {
                httpGet: { path: "/health", port: 13002 },
                initialDelaySeconds: 10,
                periodSeconds: 30,
              },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns, configMap, secret] },
  );

  // Dashboard — React SPA served by nginx
  new k8s.core.v1.Service(
    "loopzero-dashboard",
    {
      metadata: { name: "loopzero-dashboard", namespace: PLATFORM_NAMESPACE },
      spec: {
        selector: { app: "loopzero-dashboard" },
        ports: [{ name: "http", port: 80, targetPort: 80 }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apps.v1.Deployment(
    "loopzero-dashboard",
    {
      metadata: { name: "loopzero-dashboard", namespace: PLATFORM_NAMESPACE },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "loopzero-dashboard" } },
        template: {
          metadata: { labels: { app: "loopzero-dashboard" } },
          spec: {
            imagePullSecrets: [{ name: registrySecretName }],
            containers: [{
              name: "dashboard",
              image: `registry.digitalocean.com/loopzero-infra-registry/loopzero-dashboard:${imageTag}`,
              imagePullPolicy: "Always",
              ports: [{ containerPort: 80 }],
              resources: {
                requests: { cpu: "10m", memory: "16Mi" },
                limits: { cpu: "100m", memory: "64Mi" },
              },
              readinessProbe: {
                httpGet: { path: "/", port: 80 },
                initialDelaySeconds: 2,
                periodSeconds: 10,
              },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

// ---------------------------------------------------------------------------
// LoopZero API
// ---------------------------------------------------------------------------

function installLoopZeroAPI(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  lokiEndpoint: pulumi.Output<string>,
): k8s.apps.v1.Deployment {
  // Database connection string for the `loopzero` database (created in Phase 1).
  // MEET-2026-03-31-024: When loopzeroApiDbPassword is set, the API connects as loopzero_api role
  // (DML only, no DDL). Migrations continue using the admin user.
  // When not set, falls back to admin user (acceptable for dev only).
  const databaseUrl = config.loopzeroApiDbPassword
    ? pulumi.interpolate`postgres://loopzero_api:${config.loopzeroApiDbPassword}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`
    : pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`;

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

  // Cloudflare credentials for static frontend delivery (Sprint 10).
  // Only created when CF config values are provided.
  if (config.cfApiToken && config.cfAccountId && config.cfKvNamespaceId) {
    new k8s.core.v1.Secret(
      "loopzero-cf-credentials",
      {
        metadata: { name: "loopzero-cf-credentials", namespace: PLATFORM_NAMESPACE },
        stringData: {
          CF_API_TOKEN: config.cfApiToken,
          CF_ACCOUNT_ID: config.cfAccountId,
          CF_KV_NAMESPACE_ID: config.cfKvNamespaceId,
        },
      },
      { provider, dependsOn: [ns] },
    );
  }

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

  // REVIEW-2026-03-30: Fixes CRIT-02 — was storing full admin kubeconfigs.
  // Now uses scoped ServiceAccount tokens with minimal RBAC per cluster.
  // API needs: submit/watch Workflows (build), get/list pods (runtime), Secrets (mgmt).
  // The kubeconfigs still use the cluster endpoints from Phase 1, but the auth
  // tokens are scoped ServiceAccount tokens instead of DOKS admin tokens.
  const buildScopedKubeconfig = createScopedKubeconfig(
    "build",
    infra.buildCluster,
    "argo",
    [
      // Submit, get, list, watch, delete Argo Workflows
      { apiGroups: ["argoproj.io"], resources: ["workflows"], verbs: ["create", "get", "list", "watch", "delete"] },
      // Create/get ConfigMaps (workflow parameters)
      { apiGroups: [""], resources: ["configmaps"], verbs: ["create", "get", "list"] },
      // Get pods (for build status)
      { apiGroups: [""], resources: ["pods", "pods/log"], verbs: ["get", "list", "watch"] },
    ],
  );

  const runtimeScopedKubeconfig = createScopedKubeconfig(
    "runtime",
    infra.runtimeCluster,
    PLATFORM_NAMESPACE,
    [
      // Get pods and services (health check, status)
      { apiGroups: [""], resources: ["pods", "services", "endpoints"], verbs: ["get", "list", "watch"] },
      // Manage namespaces (for tenant namespaces — read only for API)
      { apiGroups: [""], resources: ["namespaces"], verbs: ["get", "list"] },
    ],
  );

  const kubeconfigSecret = new k8s.core.v1.Secret(
    "loopzero-api-kubeconfig",
    {
      metadata: { name: "loopzero-api-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: {
        // mgmt: API runs here — uses in-cluster SA (no kubeconfig needed for mgmt).
        // But some code paths reference it, so provide a minimal config.
        "mgmt-kubeconfig": infra.mgmtCluster.kubeconfig,
        "build-kubeconfig": buildScopedKubeconfig,
        "runtime-kubeconfig": runtimeScopedKubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // ServiceAccount for loopzero-api — provides RBAC for Tenant CR management.
  // The API needs to create/update Tenant CRs on the mgmt cluster when users
  // run `loopzero init` or `loopzero deploy` for the first time. Without this SA,
  // the API uses the default SA which has no permissions for loopzero.io CRDs.
  //
  // imagePullSecrets: doctl kubernetes cluster registry add configures the
  // default SA in the default namespace, not custom SAs in platform-system.
  // Without this, API pods get 401 Unauthorized pulling from DO registry.
  const registrySecretName = "loopzero-infra-registry";
  const apiSA = new k8s.core.v1.ServiceAccount(
    "loopzero-api-sa",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
      },
      imagePullSecrets: [{ name: registrySecretName }],
    },
    { provider, dependsOn: [ns] },
  );

  // ClusterRole for loopzero-api — minimal RBAC for Tenant CR operations.
  // Only get/create/update/patch — no delete (tenant deletion goes through operator).
  const apiClusterRole = new k8s.rbac.v1.ClusterRole(
    "loopzero-api",
    {
      metadata: { name: "loopzero-api" },
      rules: [
        {
          apiGroups: ["loopzero.io"],
          resources: ["tenants"],
          verbs: ["get", "list", "create", "update", "patch"],
        },
      ],
    },
    { provider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    "loopzero-api",
    {
      metadata: { name: "loopzero-api" },
      roleRef: {
        apiGroup: "rbac.authorization.k8s.io",
        kind: "ClusterRole",
        name: "loopzero-api",
      },
      subjects: [
        {
          kind: "ServiceAccount",
          name: "loopzero-api",
          namespace: PLATFORM_NAMESPACE,
        },
      ],
    },
    { provider, dependsOn: [apiClusterRole, apiSA] },
  );

  const configMap = new k8s.core.v1.ConfigMap(
    "loopzero-api-config",
    {
      metadata: { name: "loopzero-api-config", namespace: PLATFORM_NAMESPACE },
      data: {
        PORT: "8080",
        METRICS_PORT: "9090",
        // Tenant CRs live in the same namespace as the platform services.
        // Local dev uses loopzero-system; production uses platform-system.
        TENANT_NAMESPACE: PLATFORM_NAMESPACE,
        LOOPZERO_ENV: config.environment === "prod" ? "production" : "development",
        BASE_DOMAIN: config.domain,
        WORKLOAD_INGRESS_URL: `https://${config.domain}`,
        GITEA_URL: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`,
        ARGOCD_URL: `http://argocd-server.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        ARGOCD_INSECURE: "true",
        ARGO_NAMESPACE: "argo",
        MGMT_KUBECONFIG: "/etc/loopzero/mgmt-kubeconfig",
        // Mgmt uses the DOKS admin kubeconfig (API runs on mgmt cluster).
        // Build/runtime use scoped kubeconfigs with role-based context names.
        MGMT_KUBE_CONTEXT: pulumi.interpolate`do-${config.region}-${infra.mgmtCluster.clusterName}`,
        BUILD_KUBECONFIG: "/etc/loopzero/build-kubeconfig",
        BUILD_KUBE_CONTEXT: "build",
        RUNTIME_KUBECONFIG: "/etc/loopzero/runtime-kubeconfig",
        RUNTIME_KUBE_CONTEXT: "runtime",
        MINIO_ENDPOINT: infra.spacesEndpoint,
        REGISTRY_URL: infra.registryEndpoint,
        REGISTRY_INSECURE: "false",
        ARGOCD_NAMESPACE: PLATFORM_NAMESPACE,
        // Release reaper PENDING timeout must exceed ArgoCD sync time.
        // On DO, sync can take 3-5 min (image pull + pod scheduling).
        // Default 2m is too short — reaper kills releases before sync completes.
        // AUDIT-2026-03-28: raised to 10m to match RELEASING timeout.
        RELEASE_PENDING_TIMEOUT: "10m",
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

  // REVIEW-2026-03-30: Fixes HIGH-02 — API had no PDB, both pods could be evicted during drain.
  new k8s.policy.v1.PodDisruptionBudget(
    "loopzero-api-pdb",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        minAvailable: 1,
        selector: {
          matchLabels: { app: "loopzero-api" },
        },
      },
    },
    { provider },
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
            serviceAccountName: "loopzero-api",
            // Database migrations run before API starts via init container.
            // The API image includes migration SQL files at /migrations/.
            initContainers: [
              {
                name: "db-migrate",
                image: "migrate/migrate:v4.18.1",
                // Auto-recover from dirty migration state before running up.
                // If a previous migration failed mid-run, golang-migrate marks the
                // DB as "dirty" and refuses all future runs. This wrapper detects
                // dirty state and forces the version clean before proceeding.
                command: ["/bin/sh", "-c"],
                args: [
                  [
                    'DIRTY=$(migrate -path=/migrations -database "$DATABASE_URL" version 2>&1 || true);',
                    'if echo "$DIRTY" | grep -q "dirty"; then',
                    '  VER=$(echo "$DIRTY" | grep -oE "[0-9]+");',
                    '  echo "Dirty database at version $VER — auto-recovering";',
                    '  migrate -path=/migrations -database "$DATABASE_URL" force "$VER";',
                    "fi;",
                    'migrate -path=/migrations -database "$DATABASE_URL" up',
                  ].join(" "),
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
                // REVIEW-2026-03-30: Fixes HIGH-08 — uses infra output instead of hardcoded constant.
                image: pulumi.interpolate`${infra.registryEndpoint}/loopzero-api:${config.environment}`,
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
                  // Cloudflare KV credentials for static frontend routing (Sprint 10).
                  // Optional: secret only exists when CF config is set.
                  ...(config.cfApiToken ? [{ secretRef: { name: "loopzero-cf-credentials" } }] : []),
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
    { provider, dependsOn: [ns, secret, kubeconfigSecret, migrationsConfigMap, configMap, svc, apiSA] },
  );
}

// ---------------------------------------------------------------------------
// Tenant Controller
// ---------------------------------------------------------------------------

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

  // REVIEW-2026-03-30: Fixes CRIT-02 — controller kubeconfigs now use scoped tokens.
  // Controller needs: create namespaces + RBAC + quotas (runtime), manage Secrets (build).
  const controllerBuildKubeconfig = createScopedKubeconfig(
    "ctrl-build",
    infra.buildCluster,
    "argo",
    [
      // Tenant controller creates per-tenant build namespaces with quotas, limits, RBAC,
      // network policies, and Argo workflow executor roles.
      // Must match runtime cluster RBAC (below) — both clusters need the same resource access.
      { apiGroups: [""], resources: ["namespaces", "secrets", "serviceaccounts", "resourcequotas", "limitranges"], verbs: ["get", "list", "create", "update", "patch"] },
      { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch"] },
      { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "bind", "escalate"] },
    ],
  );
  const controllerRuntimeKubeconfig = createScopedKubeconfig(
    "ctrl-runtime",
    infra.runtimeCluster,
    PLATFORM_NAMESPACE,
    [
      { apiGroups: [""], resources: ["namespaces", "secrets", "serviceaccounts", "resourcequotas", "limitranges"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
      { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch"] },
      { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "bind", "escalate"] },
      // PriorityClasses are cluster-scoped. Tenant controller creates per-plan classes
      // (loopzero-tenant-starter, loopzero-tenant-growth, loopzero-tenant-enterprise) on the runtime cluster.
      { apiGroups: ["scheduling.k8s.io"], resources: ["priorityclasses"], verbs: ["get", "list", "create", "update", "patch"] },
    ],
  );
  const controllerKubeconfig = new k8s.core.v1.Secret(
    "tenant-controller-kubeconfig",
    {
      metadata: {
        name: "tenant-controller-kubeconfig",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        "build-kubeconfig": controllerBuildKubeconfig,
        "runtime-kubeconfig": controllerRuntimeKubeconfig,
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

  const deploy = new k8s.apps.v1.Deployment(
    "tenant-controller",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "tenant-controller" },
        annotations: {
          // skipAwait: don't block on pod readiness. On fresh deploys,
          // images don't exist yet (built in Step 2). Step 3 converges.
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
            securityContext: {
              runAsNonRoot: true,
              runAsUser: 1000,
              fsGroup: 1000,
            },
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
                image: pulumi.interpolate`${infra.registryEndpoint}/tenant-controller:${config.environment}`,
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
                  {
                    name: "BASE_DOMAIN",
                    value: config.domain,
                  },
                  // Object storage endpoint — S3-compatible (DO Spaces).
                  {
                    name: "MINIO_ENDPOINT",
                    value: infra.spacesEndpoint,
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
                  // DO Container Registry auth — BuildKit needs this for image push.
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
                securityContext: {
                  runAsNonRoot: true,
                  allowPrivilegeEscalation: false,
                  readOnlyRootFilesystem: true,
                  capabilities: { drop: ["ALL"] },
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

  // Service — exposes metrics and health ports for Prometheus scraping and readiness checks.
  new k8s.core.v1.Service(
    "tenant-controller-svc",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "tenant-controller" },
      },
      spec: {
        selector: { app: "tenant-controller" },
        ports: [
          { name: "metrics", port: 8080, targetPort: 8080 },
          { name: "health", port: 8081, targetPort: 8081 },
        ],
      },
    },
    { provider, dependsOn: [deploy] },
  );

  // PodDisruptionBudget — ensures at least 1 controller pod stays available during
  // voluntary disruptions (node drain, cluster upgrade).
  new k8s.policy.v1.PodDisruptionBudget(
    "tenant-controller-pdb",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        minAvailable: 1,
        selector: {
          matchLabels: { app: "tenant-controller" },
        },
      },
    },
    { provider },
  );

  return deploy;
}

// ---------------------------------------------------------------------------
// Runtime cluster registration
// ---------------------------------------------------------------------------

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
    // infra-arch-005: use typed parser instead of fragile regex
    const parsed = parseKubeconfig(kc);
    return JSON.stringify({
      bearerToken: parsed.token,
      tlsClientConfig: {
        caData: parsed.certificateAuthorityData,
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

// Observability (Prometheus + Grafana) extracted to mgmt-observability.ts — ARCH-001

// ---------------------------------------------------------------------------
// TLS IngressRoutes
// ---------------------------------------------------------------------------

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
  // "api" subdomain uses path-based routing (see createAPIGatewayRoute below)
  // to serve loopzero-api, loopzero-cloud, and loopzero-dashboard under one domain.
  { subdomain: "dashboard", serviceName: "loopzero-dashboard", servicePort: 80 },
  // OAuth2 authorization server — browser-accessible for PKCE/consent flows.
  // User-facing URL is auth.{domain}, no internal technology names exposed.
  // OAuth2 authorization server — browser-accessible for PKCE/consent flows.
  { subdomain: "auth", serviceName: "ory-hydra-public", servicePort: 4444 },
  // Identity management — Kratos Self-Service UI (login/signup/recovery).
  // MEET-2026-04-05-032: kratos-ui renders forms; ory-kratos-public is the API (same origin).
  { subdomain: "accounts", serviceName: "kratos-ui", servicePort: 3000 },
] as const;

/**
 * API Gateway — path-based routing under api.{domain}.
 *
 * Routes:
 *   /v1/auth/*        → loopzero-cloud:13002 (OAuth2 Device Flow, token refresh)
 *   /v1/billing/*     → loopzero-cloud:13002 (Stripe integration)
 *   /v1/webhooks/*    → loopzero-cloud:13002 (Stripe webhooks)
 *   /auth/*           → loopzero-cloud:13002 (Kratos/Hydra consent handlers)
 *   /health           → loopzero-cloud:13002 (cloud service health)
 *   /*                → loopzero-api:8080    (default: all other API routes)
 *
 * This eliminates separate subdomains for cloud service — one domain,
 * one TLS cert, no CORS, CLI only needs one URL.
 */
function createAPIGatewayRoute(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): void {
  const fqdn = `api.${config.domain}`;
  const certName = "tls-api";

  // TLS Certificate
  const cert = new k8s.apiextensions.CustomResource(
    "cert-api",
    {
      apiVersion: "cert-manager.io/v1",
      kind: "Certificate",
      metadata: { name: certName, namespace: PLATFORM_NAMESPACE },
      spec: {
        secretName: `${certName}-secret`,
        issuerRef: { name: "letsencrypt-prod", kind: "ClusterIssuer" },
        dnsNames: [fqdn],
      },
    },
    { provider, dependsOn: [clusterIssuer] },
  );

  // IngressRoute with path-based routing (priority: specific paths first)
  new k8s.apiextensions.CustomResource(
    "ingress-api-gateway",
    {
      apiVersion: "traefik.io/v1alpha1",
      kind: "IngressRoute",
      metadata: {
        name: "api-gateway",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
        },
      },
      spec: {
        entryPoints: ["websecure"],
        routes: [
          // Cloud auth service routes (higher priority — matched first)
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/v1/auth\`) || PathPrefix(\`/v1/billing\`) || PathPrefix(\`/v1/webhooks\`) || PathPrefix(\`/auth\`))`,
            kind: "Rule",
            priority: 100,
            services: [{ name: "loopzero-cloud", port: 13002, namespace: PLATFORM_NAMESPACE }],
          },
          // Default: all other routes go to loopzero-api
          {
            match: `Host(\`${fqdn}\`)`,
            kind: "Rule",
            priority: 1,
            services: [{ name: "loopzero-api", port: 8080, namespace: PLATFORM_NAMESPACE }],
          },
        ],
        tls: { secretName: `${certName}-secret` },
      },
    },
    { provider, dependsOn: [cert, traefik] },
  );
}

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

// ---------------------------------------------------------------------------
// Network Policies — production network isolation
// ---------------------------------------------------------------------------

/**
 * Creates NetworkPolicies for the platform namespace.
 * REVIEW-2026-03-30: Fixes HIGH-01 — local dev uses helm/network-policies/loopzero-system.yaml
 * but production had NO network isolation because the namespace didn't match.
 *
 * These Pulumi resources ensure production uses the correct namespace (platform-system).
 */
function createPlatformNetworkPolicies(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): void {
  // Default deny all ingress
  // Network isolation: allow all intra-namespace traffic + external webhook access.
  //
  // The platform namespace runs trusted services (cert-manager, ArgoCD, Gitea, MinIO,
  // Prometheus, Grafana, Traefik, loopzero-api, tenant-controller) that need to communicate
  // freely. Helm post-install hooks (Jobs) also need access to their parent services.
  //
  // Previous approach (default-deny + per-service allow-list) was brittle: every new
  // service or Helm hook required a new NetworkPolicy. Missing policies caused silent
  // failures (cert-manager webhook blocked, MinIO hook blocked).
  //
  // Current approach: allow all namespace-internal traffic + external access for
  // services that need it (webhooks from K8s API, Traefik from LoadBalancer).
  // Workload namespaces (tenant apps) use stricter isolation.
  new k8s.networking.v1.NetworkPolicy(
    "platform-network-policy",
    {
      metadata: {
        name: "platform-network-policy",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        podSelector: {},
        policyTypes: ["Ingress"],
        ingress: [
          {
            // Allow all traffic from pods within the same namespace
            from: [{ podSelector: {} }],
          },
          {
            // Allow external ingress on specific ports:
            // - 10250: cert-manager webhook (K8s API server calls)
            // - 8443: ArgoCD webhook
            // - 8000/8443: Traefik (LoadBalancer → ingress controller)
            // - 8080: loopzero-api, ArgoCD server (Traefik → backends)
            // - 3000: Gitea (ArgoCD, cross-namespace access)
            // - 9090/9402: Prometheus metrics scraping
            ports: [
              { port: 10250, protocol: "TCP" },
              { port: 8443, protocol: "TCP" },
              { port: 8000, protocol: "TCP" },
              { port: 8080, protocol: "TCP" },
              { port: 3000, protocol: "TCP" },
              { port: 9090, protocol: "TCP" },
              { port: 9402, protocol: "TCP" },
            ],
          },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Egress: allow all pods to reach external services.
  // Platform services need: DNS, K8s API, PostgreSQL (managed DO), HTTPS (GitHub,
  // DO Spaces, CF, Let's Encrypt), cross-cluster (build K8s API, Loki NodePort).
  new k8s.networking.v1.NetworkPolicy(
    "platform-egress-policy",
    {
      metadata: {
        name: "platform-egress-policy",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        podSelector: {},
        policyTypes: ["Egress"],
        egress: [
          // DNS
          { ports: [{ port: 53, protocol: "UDP" }, { port: 53, protocol: "TCP" }] },
          // K8s API server (DOKS managed — external, uses 443 or 6443)
          { ports: [{ port: 443, protocol: "TCP" }, { port: 6443, protocol: "TCP" }] },
          // PostgreSQL (managed DO database — port 25060)
          { ports: [{ port: 25060, protocol: "TCP" }] },
          // HTTP (Gitea internal, MinIO, Loki cross-cluster on NodePort)
          { ports: [{ port: 80, protocol: "TCP" }, { port: 8080, protocol: "TCP" }, { port: 9000, protocol: "TCP" }] },
          // Cross-cluster (build K8s API, Loki NodePort 30100)
          { ports: [{ port: 30100, protocol: "TCP" }] },
          // Intra-namespace (all pods can talk to each other)
          { to: [{ podSelector: {} }] },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );
}

// ---------------------------------------------------------------------------
// Scoped Kubeconfigs — REVIEW-2026-03-30: Fixes CRIT-02
// ---------------------------------------------------------------------------

interface RbacRule {
  apiGroups: string[];
  resources: string[];
  verbs: string[];
}

/**
 * Creates a scoped kubeconfig for a remote cluster using a dedicated ServiceAccount
 * with minimal RBAC instead of the full admin kubeconfig.
 *
 * This prevents privilege escalation if the API or controller pod is compromised —
 * the attacker only gets the specific permissions defined in the rules, not cluster-admin.
 */
function createScopedKubeconfig(
  role: string,
  cluster: ClusterOutputs,
  namespace: string,
  rules: RbacRule[],
): pulumi.Output<string> {
  const k8sProvider = new k8s.Provider(`${role}-scoped-k8s`, {
    kubeconfig: cluster.kubeconfig,
  });

  const saName = `loopzero-${role}-sa`;

  const sa = new k8s.core.v1.ServiceAccount(
    `${role}-scoped-sa`,
    {
      metadata: { name: saName, namespace },
    },
    { provider: k8sProvider },
  );

  const clusterRole = new k8s.rbac.v1.ClusterRole(
    `${role}-scoped-role`,
    {
      metadata: { name: saName },
      rules: rules.map((r) => ({
        apiGroups: r.apiGroups,
        resources: r.resources,
        verbs: r.verbs,
      })),
    },
    { provider: k8sProvider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    `${role}-scoped-binding`,
    {
      metadata: { name: saName },
      subjects: [{ kind: "ServiceAccount", name: saName, namespace }],
      roleRef: {
        kind: "ClusterRole",
        name: saName,
        apiGroup: "rbac.authorization.k8s.io",
      },
    },
    { provider: k8sProvider, dependsOn: [clusterRole, sa] },
  );

  // Long-lived token Secret — K8s 1.24+ requires explicit creation for SA tokens.
  const tokenSecret = new k8s.core.v1.Secret(
    `${role}-scoped-token`,
    {
      metadata: {
        name: `${saName}-token`,
        namespace,
        annotations: { "kubernetes.io/service-account.name": saName },
      },
      type: "kubernetes.io/service-account-token",
    },
    { provider: k8sProvider, dependsOn: [sa] },
  );

  // Build kubeconfig using scoped SA token instead of admin credentials.
  return pulumi
    .all([cluster.endpoint, tokenSecret.data])
    .apply(([endpoint, data]) => {
      // Pulumi returns Secret data as base64-encoded strings.
      // token: decode to UTF-8 for bearer auth.
      // ca.crt: already base64 — use directly as certificate-authority-data
      // (kubeconfig expects base64-encoded PEM in that field).
      const token = data?.["token"]
        ? Buffer.from(data["token"], "base64").toString("utf-8")
        : "";
      const ca = data?.["ca.crt"] ?? "";

      return `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${endpoint}
    certificate-authority-data: ${ca}
  name: ${role}
contexts:
- context:
    cluster: ${role}
    user: ${saName}
    namespace: ${namespace}
  name: ${role}
current-context: ${role}
users:
- name: ${saName}
  user:
    token: ${token}`;
    });
}
