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
} as const;

/**
 * Phase 2 outputs — platform services installed on the cluster.
 *
 * This is the contract between bootstrap and expose phases.
 * Phase 3 consumes ingressEndpoint to create DNS records.
 */
export interface BootstrapOutputs {
  /** True when Traefik Helm release status is "deployed" */
  readonly ingressReady: pulumi.Output<boolean>;
  /** LoadBalancer IP or hostname assigned by the cloud provider */
  readonly ingressEndpoint: pulumi.Output<string>;
  /** True when cert-manager Helm release status is "deployed" */
  readonly certManagerReady: pulumi.Output<boolean>;
  /** The namespace where platform services are installed */
  readonly platformNamespace: pulumi.Output<string>;
}

/**
 * Phase 2 — Bootstrap Cluster
 *
 * Uses the kubeconfig from Phase 1 to install base platform services.
 * Order matters: namespace → cert-manager → Traefik.
 *
 * Dependencies in the Pulumi graph:
 *   InfraOutputs.kubeconfig → k8sProvider → namespace → cert-manager → Traefik → LB lookup
 *
 * Idempotent: `pulumi up` with no changes = no-op. Helm Release tracks
 * release state server-side; Pulumi diffs against it.
 *
 * Health signals:
 * - Helm "deployed" status means Kubernetes resources are ready (Deployments
 *   have available replicas, Services exist). Pulumi's K8s provider enforces this.
 * - LB endpoint is populated only after the cloud provider assigns an IP/hostname.
 *   On DigitalOcean this is an IP; on AWS it's a hostname (ELB).
 */
export function bootstrapCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): BootstrapOutputs {
  // --- Kubernetes provider from Phase 1 kubeconfig ---
  // This creates an explicit graph dependency: cluster must exist before
  // any K8s resource is created. Pulumi resolves this via Output<string>.
  const k8sProvider = new k8s.Provider("platform-k8s", {
    kubeconfig: infra.kubeconfig,
  });

  // --- Platform namespace ---
  // All platform services live here. Workload namespaces are separate.
  const ns = new k8s.core.v1.Namespace(
    PLATFORM_NAMESPACE,
    {
      metadata: {
        name: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/environment": config.environment,
          "loopzero.io/project": config.project,
        },
      },
    },
    { provider: k8sProvider },
  );

  // --- cert-manager ---
  // Installed before Traefik because Traefik's TLS support (IngressRoute
  // with cert-manager annotations) requires cert-manager CRDs to exist.
  // installCRDs: true is the simplest path for cert-manager < 1.15.
  const certManager = new k8s.helm.v3.Release(
    "cert-manager",
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
        // Resource limits for predictable scheduling
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "256Mi" },
        },
      },
    },
    { provider: k8sProvider, dependsOn: [ns] },
  );

  // --- Traefik ingress controller ---
  // Creates a LoadBalancer Service that the cloud provider backs with a
  // real LB (DO Load Balancer, AWS ELB/NLB). The LB IP/hostname is what
  // Phase 3 uses for DNS records.
  const traefik = new k8s.helm.v3.Release(
    "traefik",
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
        // Resource limits
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
      },
    },
    // Traefik depends on cert-manager so that CRDs are available
    // when Traefik tries to create IngressRoute resources with TLS.
    { provider: k8sProvider, dependsOn: [ns, certManager] },
  );

  // --- Readiness signals ---
  // Helm Release status "deployed" means:
  // 1. Helm install/upgrade succeeded
  // 2. Pulumi's K8s provider confirmed resources are ready
  //    (Deployments have available replicas, Services exist)
  const certManagerReady = certManager.status.apply(
    (s) => s.status === "deployed",
  );
  const ingressReady = traefik.status.apply((s) => s.status === "deployed");

  // --- LoadBalancer endpoint extraction ---
  // Reads the Traefik Service to get the cloud-assigned external endpoint.
  // Pulumi's K8s provider waits for LoadBalancer ingress to be assigned
  // before marking the Service as ready, so this output is populated
  // by the time the stack finishes.
  //
  // Service name: the Traefik Helm chart creates a Service named after
  // the release name ("traefik") in the release namespace.
  const traefikSvc = k8s.core.v1.Service.get(
    "traefik-lb-lookup",
    pulumi.interpolate`${PLATFORM_NAMESPACE}/traefik`,
    { provider: k8sProvider, dependsOn: [traefik] },
  );

  // DigitalOcean returns an IP; AWS returns a hostname (ELB).
  // Both are valid DNS targets — Phase 3 handles the difference.
  const ingressEndpoint = traefikSvc.status.apply((status) => {
    const ingress = status?.loadBalancer?.ingress?.[0];
    return ingress?.ip ?? ingress?.hostname ?? "";
  });

  return {
    ingressReady,
    ingressEndpoint,
    certManagerReady,
    platformNamespace: ns.metadata.name,
  };
}
