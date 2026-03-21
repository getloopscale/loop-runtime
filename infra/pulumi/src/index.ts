import * as pulumi from "@pulumi/pulumi";
import { loadStackConfig } from "./config/stack";
import { createInfra, resolveProvider } from "./components/infra";
import { bootstrapCluster } from "./components/bootstrap";
import { exposePlatform } from "./components/expose";
import { buildExecutionState } from "./core/execution";

// --- Configuration ---
const config = loadStackConfig();
const factory = resolveProvider(config.provider);

// --- Phase 1: Base Infrastructure ---
const infra = createInfra(config);

// --- Phase 2: Bootstrap Cluster ---
const bootstrap = bootstrapCluster(infra, config);

// --- Phase 3: Expose Platform (stub — not yet implemented) ---
const expose = exposePlatform(bootstrap, infra, config, factory);

// --- Phase readiness signals ---
// Phase 1: ready when core resources exist (network + cluster + kubeconfig)
const infraReady = pulumi
  .all([infra.networkId, infra.clusterName, infra.kubeconfig])
  .apply(([net, cluster, kube]) => !!(net && cluster && kube));

// --- Execution state (operational tracking) ---
// Derives phase status from actual resource readiness, not program flow.
// Query: `pulumi stack output execution --json`
export const execution = buildExecutionState({
  environment: config.environment,
  provider: config.provider,
  infraReady,
  bootstrapReady: bootstrap.ingressReady,
  exposeReady: expose.dnsConfigured,
});

// --- Stack Outputs (stable contract for consumers) ---
export const provider = config.provider;
export const environment = config.environment;
export const region = config.region;
export const infraVersion = config.infraVersion;

// Phase 1 outputs
export const networkId = infra.networkId;
export const clusterName = infra.clusterName;
export const bucketName = infra.bucketName;
export const bucketEndpoint = infra.bucketEndpoint;
export const endpoint = infra.endpoint;
export const kubeconfig = pulumi.secret(infra.kubeconfig);

// Phase 2 outputs
export const ingressReady = bootstrap.ingressReady;
export const ingressEndpoint = bootstrap.ingressEndpoint;
export const certManagerReady = bootstrap.certManagerReady;
export const platformNamespace = bootstrap.platformNamespace;

// Phase 3 outputs
export const dnsConfigured = expose.dnsConfigured;
