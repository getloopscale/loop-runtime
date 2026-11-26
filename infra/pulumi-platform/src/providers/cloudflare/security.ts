// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Cloudflare WAF, Rate Limiting, and Zero Trust Access — edge security layer.
 *
 * NOT WIRED (measured 2026-09-07). Nothing in this repository calls
 * createWafRuleset, createRateLimitRules or createZeroTrustAccess: each is
 * exported and its only occurrence in the pulumi source trees is its own
 * declaration. What follows therefore describes an INTENDED design, not a
 * protection the deployment currently has — read it as a reference for the
 * AWS/Terraform port (`../loop-runtime-infra-modules`), not as a description of the
 * running edge.
 *
 * The design, once wired, protects the platform BEFORE traffic reaches the
 * cluster:
 * - WAF: OWASP Top 10 + bot management at CF edge
 * - Rate limiting: protects API from abuse
 * - Zero Trust Access: locks admin UIs (ArgoCD, Grafana, Gitea) behind auth
 *
 * All traffic flows: Internet → CF Edge (WAF+RateLimit) → CF Tunnel → Traefik → Pod
 */

import * as cloudflare from "@pulumi/cloudflare";

export interface EdgeSecurityConfig {
  /** Cloudflare zone ID */
  readonly zoneId: string;
  /** Cloudflare account ID */
  readonly accountId: string;
  /** Base domain (e.g., "dev.getloopscale.com") */
  readonly domain: string;
  /** Environment name for naming */
  readonly environment: string;
  /** Email domains allowed for Zero Trust Access (e.g., ["getloopscale.com"]) */
  readonly allowedEmailDomains?: string[];
}

/**
 * Enable CF Managed WAF rules — OWASP Core Ruleset at edge.
 * Blocks SQL injection, XSS, path traversal etc. before reaching origin.
 */
export function createWafRuleset(config: EdgeSecurityConfig): cloudflare.Ruleset {
  return new cloudflare.Ruleset(`waf-managed-${config.environment}`, {
    zoneId: config.zoneId,
    name: `loop-runtime-${config.environment}-waf-managed`,
    description: "Cloudflare Managed WAF — OWASP protection at edge",
    kind: "zone",
    phase: "http_request_firewall_managed",
    rules: [
      {
        action: "execute",
        actionParameters: {
          id: "efb7b8c949ac4650a09736fc376e9aee", // CF Managed Ruleset
        },
        expression: "true",
        description: "Enable Cloudflare Managed Ruleset",
        enabled: true,
      },
      {
        action: "execute",
        actionParameters: {
          id: "4814384a9e5d4991b9815dcfc25d2f1f", // OWASP Core Ruleset
        },
        expression: "true",
        description: "Enable OWASP Core Ruleset",
        enabled: true,
      },
    ],
  });
}

/**
 * Rate limiting rules — protect API from abuse.
 * Applied at CF edge, before traffic enters the tunnel.
 */
export function createRateLimitRules(config: EdgeSecurityConfig): cloudflare.Ruleset {
  return new cloudflare.Ruleset(`rate-limit-${config.environment}`, {
    zoneId: config.zoneId,
    name: `loop-runtime-${config.environment}-rate-limits`,
    description: "Rate limiting for API and auth endpoints",
    kind: "zone",
    phase: "http_ratelimit",
    rules: [
      {
        // Auth endpoints: strict limit (login brute force protection)
        action: "block",
        ratelimit: {
          characteristics: ["ip.src"],
          period: 60,
          requestsPerPeriod: 20,
          mitigationTimeout: 300,
        },
        expression: `(http.host eq "auth.${config.domain}" and http.request.method eq "POST")`,
        description: "Auth endpoint: 20 POST/min per IP",
        enabled: true,
      },
      {
        // API: generous limit (CLI + webhooks)
        action: "challenge",
        ratelimit: {
          characteristics: ["ip.src"],
          period: 60,
          requestsPerPeriod: 300,
          mitigationTimeout: 60,
        },
        expression: `(http.host eq "api.${config.domain}")`,
        description: "API: 300 req/min per IP",
        enabled: true,
      },
      {
        // Global: catch-all rate limit
        action: "challenge",
        ratelimit: {
          characteristics: ["ip.src"],
          period: 60,
          requestsPerPeriod: 1000,
          mitigationTimeout: 60,
        },
        expression: "true",
        description: "Global: 1000 req/min per IP",
        enabled: true,
      },
    ],
  });
}

/**
 * Zero Trust Access — protects admin UIs behind CF authentication.
 * ArgoCD, Grafana, and Gitea are only accessible after CF Access auth.
 * Eliminates need for VPN or public exposure of admin interfaces.
 */
export function createZeroTrustAccess(config: EdgeSecurityConfig): void {
  const allowedDomains = config.allowedEmailDomains ?? ["getloopscale.com"];

  const adminApps = [
    { name: "ArgoCD", subdomain: "argocd" },
    { name: "Grafana", subdomain: "grafana" },
    { name: "Gitea", subdomain: "git" },
  ];

  for (const app of adminApps) {
    new cloudflare.ZeroTrustAccessApplication(
      `zt-${app.subdomain}-${config.environment}`,
      {
        accountId: config.accountId,
        name: `${app.name} (${config.environment})`,
        domain: `${app.subdomain}.${config.domain}`,
        type: "self_hosted",
        sessionDuration: "12h",
        autoRedirectToIdentity: true,
        // Policy: allow only emails from specified domains
        policies: [
          {
            name: "team-only",
            decision: "allow",
            precedence: 1,
            includes: allowedDomains.map(d => ({
              emailDomain: { domain: d },
            })) as any,
          },
        ],
      },
    );
  }
}
