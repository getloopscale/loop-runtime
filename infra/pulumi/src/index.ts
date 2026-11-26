// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Pulumi L1 — Cloud infrastructure ONLY.
// CHG-2026-04-28-LAYERED-BOOTSTRAP — Phase 1 / T1.1.
//
// This stack manages the cloud-side resources of the LoopRuntime platform:
//   - DigitalOcean: VPC, DOKS clusters (mgmt + build + runtime + runtime-2),
//     PostgreSQL, Spaces, Container Registry
//   - Cloudflare API: Tunnel resource + ingress config + DNS, Worker, KV
//
// It MUST NOT manage any Kubernetes object. Helm charts, Secrets, ConfigMaps,
// Deployments, etc. are owned by the Helmfile layer (L2 — infra/helmfile/).
// This separation eliminates the zombie-state problem on `pulumi destroy`.

import * as pulumi from "@pulumi/pulumi";
import { loadStackConfig } from "./config/stack";
import { createInfra } from "./components/infra";
import { createStaticDelivery, StaticDeliveryOutput } from "./providers/cloudflare/static-delivery";
import { createTunnel, TunnelOutput } from "./providers/cloudflare/tunnel";
import { resolveOrphans, OrphanLookupResult } from "./core/adopt-or-create";
import { resourceName } from "./core/naming";
import {
  createServiceAuthEd25519,
  ServiceAuthEd25519Outputs,
} from "./components/service-auth-ed25519";
import {
  createAwsKmsOpenbao,
  AwsKmsOpenbaoOutputs,
} from "./components/aws-kms-openbao";
import {
  createDoSpacesOpenbaoBackups,
  DoSpacesOpenbaoBackupsOutputs,
} from "./components/do-spaces-openbao-backups";

// --- Configuration ---
const config = loadStackConfig();

// --- Pre-flight: Adopt-or-create lookups (idempotency) ---
const baseName = resourceName({
  project: config.project,
  env: config.environment,
  region: config.region,
});
let orphans: OrphanLookupResult = {};
if (config.cfAccountId && config.cfZoneId) {
  orphans = resolveOrphans({
    vpcName: `${baseName}-net`,
    tunnelName: `loop-runtime-${config.environment}`,
    kvTitle: `loop-runtime-routing-${config.environment}`,
    dnsWildcard: `*.${config.domain}`,
    workerScriptName: `loop-runtime-static-delivery-${config.environment}`,
    workerRoutePattern: `*.${config.domain}/*`,
    cfAccountId: config.cfAccountId,
    cfZoneId: config.cfZoneId,
  });
} else {
  const { lookupExistingVpc } = require("./core/adopt-or-create");
  orphans = { vpcId: lookupExistingVpc(`${baseName}-net`) };
}

// --- Phase 1: Base Infrastructure ---
const infra = createInfra(config, orphans);

// --- Phase 1.5: Cloudflare API Resources (no K8s objects) ---
let staticDelivery: StaticDeliveryOutput | undefined;
let tunnelOutput: TunnelOutput | undefined;

if (config.cfAccountId && config.cfApiToken && config.cfZoneId) {
  // 1. CF Tunnel — L1 creates the Tunnel API resource only.
  //    The cloudflared Deployment is created at L2 (Helmfile runtime).
  tunnelOutput = createTunnel({
    accountId: config.cfAccountId,
    zoneId: config.cfZoneId,
    environment: config.environment,
    baseDomain: config.domain,
    traefikService: "https://runtime-traefik.loop-runtime-system.svc:443",
    importIds: {
      tunnel: orphans.tunnelImportId,
      dnsRecord: orphans.dnsRecordImportId,
    },
  });

  // 2. CF Worker + KV — static frontend routing (DO Spaces via edge).
  staticDelivery = createStaticDelivery({
    accountId: config.cfAccountId,
    zoneId: config.cfZoneId,
    environment: config.environment,
    baseDomain: config.domain,
    s3Endpoint: pulumi.interpolate`https://${infra.staticBucketEndpoint}`,
    importIds: {
      kvNamespace: orphans.kvNamespaceImportId,
      workerScript: orphans.workerScriptImportId,
      workerRoute: orphans.workerRouteImportId,
    },
  });
}

// ---------------------------------------------------------------------------
// Stack Outputs (stable contract consumed by Helmfile L2 + loop-runtime-api wiring)
// ---------------------------------------------------------------------------
export const provider = config.provider;
export const environment = config.environment;
export const region = config.region;
export const infraVersion = config.infraVersion;

// Network
export const networkId = infra.networkId;

// Management cluster
export const mgmtClusterName = infra.mgmtCluster.clusterName;
export const mgmtClusterEndpoint = infra.mgmtCluster.endpoint;
export const mgmtKubeconfig = pulumi.secret(infra.mgmtCluster.kubeconfig);

// Build cluster
export const buildClusterName = infra.buildCluster.clusterName;
export const buildClusterEndpoint = infra.buildCluster.endpoint;
export const buildKubeconfig = pulumi.secret(infra.buildCluster.kubeconfig);

// Runtime cluster (cell-1)
export const runtimeClusterName = infra.runtimeCluster.clusterName;
export const runtimeClusterEndpoint = infra.runtimeCluster.endpoint;
export const runtimeKubeconfig = pulumi.secret(infra.runtimeCluster.kubeconfig);

// Runtime cluster (cell-1) — physically provisioned, activation handled at L2.
// T7.5 (plan 2026-05-21-deep-review-remediation): align with pulumi-drill
// convention. pulumi-drill exports `runtimeCluster2{Id,Name,Endpoint,
// Kubeconfig}` (Naming ADR Rule 2: no numeric suffix without semantic
// context — `Cluster` provides the context). Renamed canonical exports
// to match; OLD `runtime2*` names retained as aliases for one release
// window per EC-04 mandatory (pulumi-output.sh suppresses stderr — silent
// failures if a consumer reads a missing key during migration).
export const runtimeCluster2Name = infra.runtimeCluster2.clusterName;
export const runtimeCluster2Endpoint = infra.runtimeCluster2.endpoint;
export const runtimeCluster2Kubeconfig = pulumi.secret(infra.runtimeCluster2.kubeconfig);

// EC-04 backward-compat aliases — DELETE in subsequent PR after consumer
// scripts (drill-helmfile-bootstrap.sh + lib/pulumi-output.sh) are
// confirmed to read the canonical names.
export const runtime2ClusterName = runtimeCluster2Name;
export const runtime2ClusterEndpoint = runtimeCluster2Endpoint;
export const runtime2Kubeconfig = runtimeCluster2Kubeconfig;

// Database
export const dbHost = infra.database.privateHost;
export const dbPort = infra.database.port;
export const dbUri = pulumi.secret(infra.database.uri);
export const dbPassword = pulumi.secret(infra.database.password);
export const dbName = infra.database.database;
export const dbUser = infra.database.user;

// Storage (Spaces / S3)
export const bucketName = infra.bucketName;
export const bucketEndpoint = infra.bucketEndpoint;
export const staticBucketEndpoint = infra.staticBucketEndpoint;
export const staticCdnEndpoint = infra.staticCdnEndpoint;

// Container Registry
export const registryEndpoint = infra.registryEndpoint;
export const registryName = infra.registryName;

// Cloudflare API resources
export const cfTunnelId = tunnelOutput?.tunnelId;
// CHG-2026-04-28-LAYERED-BOOTSTRAP — exposed for Helmfile L2 cloudflared release.
export const cfTunnelToken = tunnelOutput ? pulumi.secret(tunnelOutput.tunnelToken) : undefined;
export const cfKvNamespaceId = staticDelivery?.kvNamespaceId;
export const cfWorkerName = staticDelivery?.workerName;

// Service-auth Ed25519 dual-control (ADR 2026-05-13). Component is
// opt-in via `pulumi config set serviceAuthEd25519Enabled true` AFTER
// the operator has run `tools/gen-service-auth-ed25519` and set the
// two private/public config keys. When the flag is true but the keys
// are absent, Pulumi throws at requireSecret time — that's the Q5
// fail-closed contract. When the flag is false (default), exports are
// undefined and Helmfile L2 falls through to "no ExternalSecret
// applied" (Phase 1 inert default).
let serviceAuthEd25519: ServiceAuthEd25519Outputs | undefined;
const serviceAuthEd25519Cfg = new pulumi.Config();
if (serviceAuthEd25519Cfg.getBoolean("serviceAuthEd25519Enabled") === true) {
  serviceAuthEd25519 = createServiceAuthEd25519({
    stackName: pulumi.getStack(),
  });
}
export const serviceAuthEd25519PrivateKey = serviceAuthEd25519
  ? pulumi.secret(serviceAuthEd25519.privateKey)
  : undefined;
export const serviceAuthEd25519PublicCurrent = serviceAuthEd25519
  ? pulumi.secret(serviceAuthEd25519.publicKeyCurrent)
  : undefined;
export const serviceAuthEd25519PublicPrevious = serviceAuthEd25519
  ? pulumi.secret(serviceAuthEd25519.publicKeyPrevious)
  : undefined;
export const serviceAuthEd25519GenerationMarker = serviceAuthEd25519
  ? serviceAuthEd25519.keyGenerationMarker
  : undefined;
export const serviceAuthEd25519ManifestsYaml = serviceAuthEd25519
  ? serviceAuthEd25519.manifests
  : undefined;

// OpenBao AWS KMS auto-unseal (F1.1 of plan 2026-06-02-openbao-adoption v1.2).
// Opt-in via `pulumi config set openbaoEnabled true`. Default false keeps
// the stack inert until operator is ready. When true, creates:
//   - aws.kms.Key (with protect:true per EC-02)
//   - aws.iam.User openbao-unseal + inline policy (kms:Encrypt+Decrypt+DescribeKey)
//   - aws.iam.AccessKey (programmatic creds consumed by OpenBao pods via Helmfile L2)
// Exports awsKmsKeyArn (public) + awsUnsealAccessKeyId/SecretKey (secret).
let openbaoKms: AwsKmsOpenbaoOutputs | undefined;
const openbaoCfg = new pulumi.Config();
if (openbaoCfg.getBoolean("openbaoEnabled") === true) {
  openbaoKms = createAwsKmsOpenbao({
    stackName: pulumi.getStack(),
    region: openbaoCfg.get("openbaoAwsRegion") ?? "us-east-1",
  });
}
export const awsKmsKeyArn = openbaoKms?.kmsKeyArn;
export const awsKmsKeyId = openbaoKms?.kmsKeyId;
export const awsKmsKeyAlias = openbaoKms?.kmsKeyAlias;
export const awsUnsealUserName = openbaoKms?.unsealUserName;
export const awsUnsealUserArn = openbaoKms?.unsealUserArn;
export const awsUnsealAccessKeyId = openbaoKms
  ? pulumi.secret(openbaoKms.unsealAccessKeyId)
  : undefined;
export const awsUnsealSecretKey = openbaoKms
  ? pulumi.secret(openbaoKms.unsealSecretKey)
  : undefined;

// OpenBao DO Spaces backup bucket (F1.2 of plan 2026-06-02-openbao-adoption v1.2).
// Same `openbaoEnabled` flag as F1.1. Creates cross-region Spaces bucket
// (NYC3 by default for SFO3 mgmt) + lifecycle policy (7d hourly/30d
// daily/12w weekly per Q12) + scoped SpacesKey for Bank-Vaults consumption.
// EC-13 validated at component-create time: bucket region MUST differ
// from mgmt region.
let openbaoBackups: DoSpacesOpenbaoBackupsOutputs | undefined;
if (openbaoCfg.getBoolean("openbaoEnabled") === true) {
  openbaoBackups = createDoSpacesOpenbaoBackups({
    stackName: pulumi.getStack(),
    bucketRegion: openbaoCfg.get("openbaoBackupRegion") ?? "nyc3",
    mgmtRegion: config.region,
  });
}
export const openbaoBackupBucketName = openbaoBackups?.bucketName;
export const openbaoBackupBucketEndpoint = openbaoBackups?.bucketEndpoint;
export const openbaoBackupRegion = openbaoBackups?.bucketRegion;
export const openbaoBackupAccessKeyId = openbaoBackups
  ? pulumi.secret(openbaoBackups.accessKeyId)
  : undefined;
export const openbaoBackupSecretKey = openbaoBackups
  ? pulumi.secret(openbaoBackups.secretKey)
  : undefined;

// Cross-cluster Loki push URL (audit M08 #18 / §6.C.4b step 6).
// Re-export of `config.lokiPushUrl` as a stable Pulumi stack output so
// Helmfile L2 can consume the canonical URL via `pulumi stack output
// lokiPushUrl` (zero drift between Pulumi state and helmfile env files).
//
// The per-environment yaml files at `infra/helmfile/environments/<env>.yaml`
// still carry a `loki.pushUrl` field as a deploy-time fallback when
// Pulumi state is unreachable (helmfile sync against unhealthy Pulumi
// shouldn't no-op silently). The Pulumi output is the SoT in steady
// state; the env-file value is the operator escape hatch.
//
// Undefined when the operator has not run
// `pulumi config set lokiPushUrl http://<host>:<port>/loki/api/v1/push`
// — caller is expected to fall through to the env-file value.
export const lokiPushUrl: string | undefined = config.lokiPushUrl;
