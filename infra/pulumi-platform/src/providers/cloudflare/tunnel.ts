// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Cloudflare Tunnel provisioning (Sprint 10, updated for Custom Domains).
// Creates: Tunnel, ingress config, DNS CNAMEs, cloudflared K8s Deployment.
// All runtime traffic is routed through the CF Tunnel — zero public IPs.
// Custom domains: users CNAME their domain to cname.{baseDomain}, which resolves
// to the tunnel. The catch-all ingress rule sends ALL traffic to Traefik,
// which handles hostname matching via IngressRoute Host() rules.

import * as pulumi from "@pulumi/pulumi";
import * as cloudflare from "@pulumi/cloudflare";
import * as k8s from "@pulumi/kubernetes";

export interface TunnelConfig {
  /** Cloudflare account ID */
  readonly accountId: string;
  /** Cloudflare zone ID for the domain */
  readonly zoneId: string;
  /** Environment name (dev, staging, prod) — used to namespace CF resources */
  readonly environment: string;
  /** Base domain (e.g., "dev.getloopscale.com") — wildcard CNAME points to tunnel */
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
  // 1. Stable tunnel secret — read from Pulumi config.
  // The secret MUST be stable across `pulumi up` runs. If it changes,
  // CF Tunnel is recreated causing downtime for all runtime traffic.
  // REVIEW-2026-03-30: Fixes CRIT-03 (crypto.randomBytes caused new secret every run).
  //
  // Set once via: pulumi config set --secret tunnelSecret "$(openssl rand -base64 32)"
  const cfConfig = new pulumi.Config();
  const tunnelSecret = cfConfig.requireSecret("tunnelSecret");

  const tunnelName = `loop-runtime-${config.environment}`;
  const tunnel = new cloudflare.ZeroTrustTunnelCloudflared(`loop-runtime-tunnel-${config.environment}`, {
    accountId: config.accountId,
    name: tunnelName,
    tunnelSecret: tunnelSecret,
    configSrc: "cloudflare",
  });

  // 2. Tunnel ingress config — route ALL traffic to Traefik
  // The catch-all rule sends every hostname to Traefik, which handles
  // routing via IngressRoute Host() rules. This supports:
  //   - Platform subdomains: *.{baseDomain} (auto-generated app URLs)
  //   - Custom domains: any FQDN that CNAMEs to cname.{baseDomain}
  // Unknown hostnames receive Traefik's default 404 — safe behavior.
  // MEET-2026-04-05-033: Changed catch-all from http_status:404 to Traefik
  // to enable custom domain traffic routing.
  new cloudflare.ZeroTrustTunnelCloudflaredConfig(
    "loop-runtime-tunnel-config",
    {
      accountId: config.accountId,
      tunnelId: tunnel.id,
      source: "cloudflare",
      config: {
        ingresses: [
          {
            // Platform wildcard — explicit rule for observability (shows in CF dashboard)
            hostname: `*.${config.baseDomain}`,
            service: config.traefikService,
            originRequest: {
              noTlsVerify: true, // Traefik uses self-signed cert internally
            },
          },
          {
            // Catch-all — routes ALL other hostnames to Traefik.
            // Enables custom domains: user CNAMEs api.example.com → cname.{baseDomain},
            // traffic arrives here, Traefik matches via IngressRoute Host() rule.
            // Required by Cloudflare (must be last rule, no hostname field).
            service: config.traefikService,
            originRequest: {
              noTlsVerify: true,
            },
          },
        ],
      },
    },
    {
      dependsOn: [tunnel],
    },
  );

  // 3. DNS — wildcard CNAME for all platform subdomains
  // The wildcard *.{baseDomain} covers ALL subdomains including cname.{baseDomain}.
  // No separate cname.{baseDomain} record needed — CF rejects explicit CNAME
  // when a wildcard already exists for the same host.
  // Custom domains: users CNAME to cname.{baseDomain} which resolves via wildcard.
  new cloudflare.DnsRecord("loop-runtime-tunnel-dns", {
    zoneId: config.zoneId,
    name: `*.${config.baseDomain}`,
    type: "CNAME",
    ttl: 1,
    content: pulumi.interpolate`${tunnel.id}.cfargotunnel.com`,
    proxied: true,
  });

  // 4. Build the tunnel token (base64 JSON with account_id, tunnel_id, tunnel_secret).
  // This is the format cloudflared expects with `tunnel run --token`.
  const tunnelToken = pulumi.all([tunnel.id, tunnelSecret]).apply(([tunnelId, secret]) => {
    const tokenPayload = JSON.stringify({
      a: config.accountId,
      t: tunnelId,
      s: secret,
    });
    return Buffer.from(tokenPayload).toString("base64");
  });

  // Store tunnel token in a K8s Secret instead of passing as pod arg.
  // REVIEW-2026-03-30: Fixes HIGH-03 (token was visible via kubectl describe pod).
  const tunnelTokenSecret = new k8s.core.v1.Secret(
    "cloudflared-tunnel-token",
    {
      metadata: {
        name: "cloudflared-tunnel-token",
        namespace: config.namespace,
      },
      stringData: {
        "tunnel-token": tunnelToken,
      },
    },
    { provider: config.runtimeProvider },
  );

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
            // REVIEW-2026-03-30: Fixes MED-11 (missing securityContext, probes).
            securityContext: {
              runAsNonRoot: true,
              runAsUser: 65532,
              fsGroup: 65532,
            },
            containers: [
              {
                name: "cloudflared",
                image: "cloudflare/cloudflared:2024.12.2",
                args: ["tunnel", "--no-autoupdate", "--metrics", "0.0.0.0:2000", "run", "--token", "$(TUNNEL_TOKEN)"],
                env: [
                  {
                    name: "TUNNEL_TOKEN",
                    valueFrom: {
                      secretKeyRef: {
                        name: "cloudflared-tunnel-token",
                        key: "tunnel-token",
                      },
                    },
                  },
                ],
                resources: {
                  requests: { cpu: "50m", memory: "64Mi" },
                  limits: { cpu: "200m", memory: "256Mi" },
                },
                livenessProbe: {
                  httpGet: { path: "/ready", port: 2000 },
                  initialDelaySeconds: 10,
                  periodSeconds: 15,
                },
                readinessProbe: {
                  httpGet: { path: "/ready", port: 2000 },
                  initialDelaySeconds: 5,
                  periodSeconds: 10,
                },
                securityContext: {
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
    { provider: config.runtimeProvider, dependsOn: [tunnelTokenSecret] },
  );

  return {
    tunnelId: tunnel.id,
    tunnelToken: tunnelToken,
  };
}
