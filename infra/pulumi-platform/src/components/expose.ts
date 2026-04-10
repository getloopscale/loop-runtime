// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
// Type aliases for migration
type BootstrapOutputs = any;
type InfraOutputs = any;
type PlatformStackConfig = any;
type CloudProviderFactory = any;

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
const MGMT_SUBDOMAINS = ["api", "argocd", "git", "grafana", "auth", "cloud"] as const;

/**
 * Computes the DNS record name prefix based on domain and zone.
 *
 * Examples:
 *   domain="loopzero.example",     zone="loopzero.example"     → prefix=""      (prod)
 *   domain="dev.loopzero.example", zone="loopzero.example"     → prefix="dev"   (dev)
 *   domain="staging.loopzero.example", zone="loopzero.example" → prefix="staging"
 *
 * For a subdomain "api" with prefix "dev":
 *   recordName = "api.dev" → creates api.dev.loopzero.example
 */
function dnsPrefix(domain: string, zone: string): string {
  if (domain === zone) return "";
  return domain.replace(`.${zone}`, "");
}

/**
 * Phase 3 — Expose Platform
 *
 * Creates DNS records pointing to the ingress load balancers
 * of both clusters. Uses dnsZone as the authoritative zone
 * (registered in the DNS provider) and domain to compute
 * the record name prefix.
 *
 *   Management: api.{domain}, argocd.{domain}, grafana.{domain}
 *     → all point to mgmt cluster LB
 *
 *   Runtime: *.{domain} (wildcard)
 *     → points to runtime cluster LB, so any deployed app
 *       gets a URL automatically
 */
export function exposePlatform(
  bootstrap: BootstrapOutputs,
  _infra: InfraOutputs,
  config: PlatformStackConfig,
  factory: CloudProviderFactory,
): ExposeOutputs {
  const zone = config.dnsZone;
  const prefix = dnsPrefix(config.domain, zone);

  // Build record name: "api" + prefix "dev" → "api.dev"
  // For prod (no prefix): "api" → "api"
  const recordName = (sub: string): string =>
    prefix ? `${sub}.${prefix}` : sub;

  // --- Management cluster DNS ---
  const mgmtRecords = MGMT_SUBDOMAINS.map((sub) =>
    factory.createDnsRecord({
      zone,
      recordName: recordName(sub),
      target: bootstrap.mgmt.ingressEndpoint,
      ttl: 300,
    }),
  );

  // --- Runtime cluster DNS ---
  // When Cloudflare Tunnel is active (cfZoneId set), the wildcard CNAME is
  // managed by the tunnel module (*.{domain} → tunnel.cfargotunnel.com).
  // Creating a second record here would conflict. Skip the wildcard.
  let runtimeDnsConfigured: pulumi.Output<boolean>;

  if (config.cfZoneId) {
    // Tunnel owns the wildcard — runtime DNS is already configured.
    runtimeDnsConfigured = pulumi.output(true);
  } else {
    // No tunnel — create A record wildcard pointing to runtime LB.
    const wildcardName = prefix ? `*.${prefix}` : "*";
    const runtimeWildcard = factory.createDnsRecord({
      zone,
      recordName: wildcardName,
      target: bootstrap.runtime.ingressEndpoint,
      ttl: 300,
    });
    runtimeDnsConfigured = runtimeWildcard.fqdn.apply(
      (fqdn: string) => fqdn.length > 0,
    );
  }

  // Readiness: all management records created + runtime configured.
  const mgmtDnsConfigured = pulumi
    .all(mgmtRecords.map((r) => r.fqdn))
    .apply((fqdns) => fqdns.every((fqdn) => fqdn.length > 0));

  return {
    mgmtDnsConfigured,
    runtimeDnsConfigured,
  };
}
