import * as pulumi from "@pulumi/pulumi";
import { BootstrapOutputs } from "./bootstrap";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { CloudProviderFactory } from "../contracts/provider";

/**
 * Phase 3 outputs — external access configured.
 */
export interface ExposeOutputs {
  /** DNS records point to management cluster ingress */
  readonly mgmtDnsConfigured: pulumi.Output<boolean>;
  /** DNS records point to runtime cluster ingress */
  readonly runtimeDnsConfigured: pulumi.Output<boolean>;
}

/**
 * Management cluster subdomains.
 * Each maps to a platform service exposed via Traefik ingress.
 */
const MGMT_SUBDOMAINS = ["api", "argocd", "grafana"] as const;

/**
 * Phase 3 — Expose Platform
 *
 * Creates DNS records pointing to the ingress load balancers
 * of both clusters:
 *
 *   Management: api.{domain}, argocd.{domain}, grafana.{domain}
 *     → all point to mgmt cluster LB
 *
 *   Runtime: *.{domain} (wildcard)
 *     → points to runtime cluster LB, so any deployed app
 *       gets a URL automatically (e.g., myapp.loopzero.example)
 *
 * Record type (A vs CNAME) is auto-detected by the provider
 * based on whether the endpoint is an IP or hostname.
 */
export function exposePlatform(
  bootstrap: BootstrapOutputs,
  _infra: InfraOutputs,
  config: PlatformStackConfig,
  factory: CloudProviderFactory,
): ExposeOutputs {
  const domain = config.domain;

  // --- Management cluster DNS ---
  // One record per platform subdomain, all pointing to mgmt ingress LB.
  const mgmtRecords = MGMT_SUBDOMAINS.map((sub) =>
    factory.createDnsRecord({
      zone: domain,
      recordName: sub,
      target: bootstrap.mgmt.ingressEndpoint,
      ttl: 300,
    }),
  );

  // --- Runtime cluster DNS ---
  // Wildcard record: any app deployed gets *.{domain} automatically.
  const runtimeWildcard = factory.createDnsRecord({
    zone: domain,
    recordName: "*",
    target: bootstrap.runtime.ingressEndpoint,
    ttl: 300,
  });

  // Readiness: all management records created + wildcard created.
  const mgmtDnsConfigured = pulumi
    .all(mgmtRecords.map((r) => r.fqdn))
    .apply((fqdns) => fqdns.every((fqdn) => fqdn.length > 0));

  const runtimeDnsConfigured = runtimeWildcard.fqdn.apply(
    (fqdn) => fqdn.length > 0,
  );

  return {
    mgmtDnsConfigured,
    runtimeDnsConfigured,
  };
}
