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

// --- Phase 2: Bootstrap Clusters ---
const bootstrap = bootstrapCluster(infra, config);

// --- Phase 3: Expose Platform (stub) ---
const expose = exposePlatform(bootstrap, infra, config, factory);

// --- Execution state (operational tracking) ---
export const execution = buildExecutionState({
  environment: config.environment,
  provider: config.provider,
  components: {
    // Phase 1
    network: infra.vpcId.apply((id) => !!id),
    mgmtCluster: infra.mgmtCluster.kubeconfig.apply((k) => !!k),
    runtimeCluster: infra.runtimeCluster.kubeconfig.apply((k) => !!k),
    gitea: infra.gitea.ipv4Address.apply((ip) => !!ip),
    runners: infra.runners.length > 0
      ? infra.runners[0].ipv4Address.apply((ip) => !!ip)
      : pulumi.output(true),
    database: infra.database.host.apply((h) => !!h),
    storage: infra.bucketName.apply((n) => !!n),

    // Phase 2
    mgmtIngress: bootstrap.mgmt.ingressReady,
    mgmtCertManager: bootstrap.mgmt.certManagerReady,
    mgmtArgocd: bootstrap.mgmt.argoCdReady,
    runtimeIngress: bootstrap.runtime.ingressReady,

    // Phase 3
    mgmtDns: expose.mgmtDnsConfigured,
    runtimeDns: expose.runtimeDnsConfigured,
  },
});

// --- Stack Outputs (stable contract) ---
export const provider = config.provider;
export const environment = config.environment;
export const region = config.region;
export const infraVersion = config.infraVersion;

// Phase 1 — Network
export const networkId = infra.networkId;

// Phase 1 — Management cluster
export const mgmtClusterName = infra.mgmtCluster.clusterName;
export const mgmtClusterEndpoint = infra.mgmtCluster.endpoint;
export const mgmtKubeconfig = pulumi.secret(infra.mgmtCluster.kubeconfig);

// Phase 1 — Runtime cluster
export const runtimeClusterName = infra.runtimeCluster.clusterName;
export const runtimeClusterEndpoint = infra.runtimeCluster.endpoint;
export const runtimeKubeconfig = pulumi.secret(infra.runtimeCluster.kubeconfig);

// Phase 1 — Gitea
export const giteaIp = infra.gitea.ipv4Address;
export const giteaPrivateIp = infra.gitea.privateIpv4Address;

// Phase 1 — Runners
export const runnerIps = infra.runners.map((r) => r.ipv4Address);

// Phase 1 — Database
export const dbHost = infra.database.privateHost;
export const dbPort = infra.database.port;
export const dbUri = pulumi.secret(infra.database.uri);
export const dbPassword = pulumi.secret(infra.database.password);

// Phase 1 — Storage
export const bucketName = infra.bucketName;
export const bucketEndpoint = infra.bucketEndpoint;

// Phase 2 — Management cluster services
export const mgmtIngressReady = bootstrap.mgmt.ingressReady;
export const mgmtIngressEndpoint = bootstrap.mgmt.ingressEndpoint;
export const certManagerReady = bootstrap.mgmt.certManagerReady;
export const argoCdReady = bootstrap.mgmt.argoCdReady;

// Phase 2 — Runtime cluster services
export const runtimeIngressReady = bootstrap.runtime.ingressReady;
export const runtimeIngressEndpoint = bootstrap.runtime.ingressEndpoint;

// Phase 3 — DNS
export const mgmtDnsConfigured = expose.mgmtDnsConfigured;
export const runtimeDnsConfigured = expose.runtimeDnsConfigured;

// --- Wiring outputs (connection details for operators) ---
export const wiring = pulumi.output({
  gitea: {
    publicUrl: pulumi.interpolate`http://${infra.gitea.ipv4Address}:3000`,
    privateUrl: pulumi.interpolate`http://${infra.gitea.privateIpv4Address}:3000`,
  },
  database: {
    privateHost: infra.database.privateHost,
    port: infra.database.port,
    database: infra.database.database,
    user: infra.database.user,
  },
  storage: {
    bucket: infra.bucketName,
    endpoint: infra.bucketEndpoint,
  },
  mgmt: {
    ingressEndpoint: bootstrap.mgmt.ingressEndpoint,
    clusterEndpoint: infra.mgmtCluster.endpoint,
  },
  runtime: {
    ingressEndpoint: bootstrap.runtime.ingressEndpoint,
    clusterEndpoint: infra.runtimeCluster.endpoint,
  },
});
