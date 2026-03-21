import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
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
}

export interface BootstrapOutputs {
  readonly mgmt: MgmtBootstrapOutputs;
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
  const runtime = bootstrapRuntimeCluster(infra, config);

  return { mgmt, runtime };
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

  // Traefik — ingress for platform services (ArgoCD UI, API, etc.)
  const traefik = installTraefik("mgmt", provider, ns, certManager);

  const ingressEndpoint = lookupLoadBalancerEndpoint(
    "mgmt",
    provider,
    traefik,
  );

  return {
    certManagerReady: certManager.status.apply((s) => s.status === "deployed"),
    argoCdReady: argocd.status.apply((s) => s.status === "deployed"),
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
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
  // Traefik Helm chart creates a Service named after the release.
  // Our release is "{role}-traefik", but the K8s Service name follows
  // the Helm chart template, which is just "traefik" (release name
  // without the Pulumi resource prefix).
  // The actual K8s service name depends on the chart's fullnameOverride.
  // By default, Traefik chart uses the release name as the service name.
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
