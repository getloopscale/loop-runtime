// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cloudflare Tunnel provisioning (L1 / Pulumi).
// CHG-2026-04-28-LAYERED-BOOTSTRAP — ADR D9.
//
// L1 (Pulumi, this file): Tunnel resource + ingress config + DNS CNAME on the
//                          Cloudflare API. Zero K8s resources.
// L2 (Helmfile, infra/helm/cloudflared/): cloudflared Deployment + Secret
//                          inside the runtime cluster. Token is consumed via
//                          env var LOOPZERO_CF_TUNNEL_TOKEN sourced from
//                          `pulumi stack output cfTunnelToken --show-secrets`.
//
// Why this split: when the runtime cluster is destroyed, the cloudflared
// Deployment + Secret in the old design became zombies in the Pulumi state
// (pulumi-kubernetes Issue #2517). Moving them to L2 makes destroy safe.
//
// Custom domains: tenants can attach their own domains via the v2 Custom
// Domains flow (CF Zone Mgmt + NS change by user, see api/internal/domains/cfzone/).
// The wildcard CNAME `*.{baseDomain} → {tunnelId}.cfargotunnel.com` covers
// the platform's own subdomains; per-tenant custom domains create their own
// CF Zone, separate from this tunnel resource. See MEET-2026-04-05-033 for
// the architecture decision (custom domains do NOT route through this tunnel —
// they have dedicated CF resources to avoid blast radius coupling).

import * as pulumi from "@pulumi/pulumi";
import * as cloudflare from "@pulumi/cloudflare";

export interface TunnelConfig {
  /** Cloudflare account ID */
  readonly accountId: string;
  /** Cloudflare zone ID for the domain */
  readonly zoneId: string;
  /** Environment name (dev, staging, prod) — used to namespace CF resources */
  readonly environment: string;
  /** Base domain (e.g., "dev.loopzero.example") — wildcard CNAME points to tunnel */
  readonly baseDomain: string;
  /** Traefik service address inside the runtime cluster */
  readonly traefikService: string;
  /** Import IDs for adopt-or-create pattern (orphan resolution). */
  readonly importIds?: {
    tunnel?: string;
    dnsRecord?: string;
  };
}

export interface TunnelOutput {
  /** Tunnel ID */
  readonly tunnelId: pulumi.Output<string>;
  /** Tunnel token (for cloudflared --token; consumed by Helmfile L2). */
  readonly tunnelToken: pulumi.Output<string>;
}

/**
 * Provisions the Cloudflare Tunnel stack at L1 (zero K8s resources):
 *   1. Tunnel resource
 *   2. Tunnel ingress config (wildcard → Traefik)
 *   3. DNS CNAME record (*.{baseDomain} → tunnel.cfargotunnel.com)
 *
 * The cloudflared Deployment + Secret are created at L2 (Helmfile) using the
 * tunnelToken returned here.
 */
export function createTunnel(config: TunnelConfig): TunnelOutput {
  // 1. Stable tunnel secret — read from Pulumi config.
  // The secret MUST be stable across `pulumi up` runs. If it changes,
  // CF Tunnel is recreated causing downtime for all runtime traffic.
  // REVIEW-2026-03-30: Fixes CRIT-03 (crypto.randomBytes caused new secret every run).
  //
  // Set once via: pulumi config set --secret tunnelSecret "$(openssl rand -base64 32)"
  const cfConfig = new pulumi.Config();
  const tunnelSecret = cfConfig.requireSecret("tunnelSecret");

  const tunnelName = `loopzero-${config.environment}`;
  const tunnel = new cloudflare.ZeroTrustTunnelCloudflared(`loopzero-tunnel-${config.environment}`, {
    accountId: config.accountId,
    name: tunnelName,
    tunnelSecret: tunnelSecret,
    configSrc: "cloudflare",
  }, {
    // Ignore tunnelSecret changes — provider detects drift when
    // cloudflare:apiToken is added/changed, triggering a replace that
    // fails because the tunnel has active cloudflared connections.
    // The secret is stable (set once via pulumi config) and never changes.
    ignoreChanges: ["tunnelSecret"],
    ...(config.importIds?.tunnel ? { import: config.importIds.tunnel } : {}),
  });

  // 2. Tunnel ingress config — route ALL traffic to Traefik
  new cloudflare.ZeroTrustTunnelCloudflaredConfig(
    "loopzero-tunnel-config",
    {
      accountId: config.accountId,
      tunnelId: tunnel.id,
      source: "cloudflare",
      config: {
        ingresses: [
          {
            hostname: `*.${config.baseDomain}`,
            service: config.traefikService,
            originRequest: { noTlsVerify: true },
          },
          {
            // Catch-all — routes ALL other hostnames to Traefik.
            service: config.traefikService,
            originRequest: { noTlsVerify: true },
          },
        ],
      },
    },
    { dependsOn: [tunnel] },
  );

  // 3. DNS — wildcard CNAME for all platform subdomains
  new cloudflare.DnsRecord("loopzero-tunnel-dns", {
    zoneId: config.zoneId,
    name: `*.${config.baseDomain}`,
    type: "CNAME",
    ttl: 1,
    content: pulumi.interpolate`${tunnel.id}.cfargotunnel.com`,
    proxied: true,
  }, config.importIds?.dnsRecord ? { import: config.importIds.dnsRecord } : undefined);

  // 4. Build the tunnel token (base64 JSON with account_id, tunnel_id, tunnel_secret).
  // This is the format cloudflared expects with `tunnel run --token`.
  // It is exported from the stack and consumed by Helmfile L2.
  const tunnelToken = pulumi.all([tunnel.id, tunnelSecret]).apply(([tunnelId, secret]) => {
    const tokenPayload = JSON.stringify({
      a: config.accountId,
      t: tunnelId,
      s: secret,
    });
    return Buffer.from(tokenPayload).toString("base64");
  });

  return {
    tunnelId: tunnel.id,
    tunnelToken: tunnelToken,
  };
}
