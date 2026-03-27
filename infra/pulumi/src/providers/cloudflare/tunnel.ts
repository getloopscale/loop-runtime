// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cloudflare Tunnel provisioning (Sprint 10).
// Creates: Tunnel, ingress config, DNS CNAME, cloudflared K8s Deployment.
// All runtime traffic is routed through the CF Tunnel — zero public IPs.

import * as pulumi from "@pulumi/pulumi";
import * as cloudflare from "@pulumi/cloudflare";
import * as k8s from "@pulumi/kubernetes";
import * as crypto from "crypto";

export interface TunnelConfig {
  /** Cloudflare account ID */
  readonly accountId: string;
  /** Cloudflare zone ID for the domain */
  readonly zoneId: string;
  /** Base domain (e.g., "dev.loopzero.example") — wildcard CNAME points to tunnel */
  readonly baseDomain: string;
  /** K8s provider for the runtime cluster (where cloudflared runs) */
  readonly runtimeProvider: k8s.Provider;
  /** Namespace for cloudflared deployment */
  readonly namespace: string;
  /** Traefik service address inside the runtime cluster */
  readonly traefikService: string;
}

export interface TunnelOutput {
  /** Tunnel ID */
  readonly tunnelId: pulumi.Output<string>;
  /** Tunnel token (for cloudflared --token) */
  readonly tunnelToken: pulumi.Output<string>;
}

/**
 * Provisions the Cloudflare Tunnel stack:
 * 1. Tunnel resource
 * 2. Tunnel ingress config (wildcard → Traefik)
 * 3. DNS CNAME record (*.{baseDomain} → tunnel.cfargotunnel.com)
 * 4. cloudflared Deployment in runtime cluster
 */
export function createTunnel(config: TunnelConfig): TunnelOutput {
  // 1. Generate a stable tunnel secret (32 bytes, base64)
  const tunnelSecret = crypto.randomBytes(32).toString("base64");

  const tunnel = new cloudflare.ZeroTrustTunnelCloudflared("loopzero-tunnel", {
    accountId: config.accountId,
    name: "loopzero-dev",
    tunnelSecret: tunnelSecret,
    configSrc: "cloudflare",
  }, {
    // Import existing tunnel created before Pulumi management.
    // Safe to remove after first successful `pulumi up`.
    import: "362767409c4de7fda7990c10103efdd9/c35b9d63-20b0-4cb1-aa39-873f3d8ade58",
  });

  // 2. Tunnel ingress config — route wildcard to Traefik HTTPS
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
            originRequest: {
              noTlsVerify: true, // Traefik uses self-signed cert internally
            },
          },
          {
            // Catch-all — required by Cloudflare
            service: "http_status:404",
          },
        ],
      },
    },
    { dependsOn: [tunnel] },
  );

  // 3. DNS CNAME record — *.{baseDomain} → tunnel.cfargotunnel.com
  new cloudflare.DnsRecord("loopzero-tunnel-dns", {
    zoneId: config.zoneId,
    name: `*.${config.baseDomain}`,
    type: "CNAME",
    ttl: 1, // Auto (required when proxied=true)
    content: pulumi.interpolate`${tunnel.id}.cfargotunnel.com`,
    proxied: true,
  });

  // 4. Build the tunnel token (base64 JSON with account_id, tunnel_id, tunnel_secret).
  // This is the format cloudflared expects with `tunnel run --token`.
  const tunnelToken = pulumi.all([tunnel.id]).apply(([tunnelId]) => {
    const tokenPayload = JSON.stringify({
      a: config.accountId,
      t: tunnelId,
      s: tunnelSecret,
    });
    return Buffer.from(tokenPayload).toString("base64");
  });

  new k8s.apps.v1.Deployment(
    "cloudflared",
    {
      metadata: {
        name: "cloudflared",
        namespace: config.namespace,
        labels: { app: "cloudflared" },
      },
      spec: {
        replicas: 2,
        selector: { matchLabels: { app: "cloudflared" } },
        template: {
          metadata: { labels: { app: "cloudflared" } },
          spec: {
            containers: [
              {
                name: "cloudflared",
                image: "cloudflare/cloudflared:2024.12.2",
                args: ["tunnel", "--no-autoupdate", "run", "--token", tunnelToken],
                resources: {
                  requests: { cpu: "50m", memory: "64Mi" },
                  limits: { cpu: "200m", memory: "256Mi" },
                },
              },
            ],
          },
        },
      },
    },
    { provider: config.runtimeProvider },
  );

  return {
    tunnelId: tunnel.id,
    tunnelToken: tunnelToken,
  };
}
