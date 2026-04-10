// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as cloudflare from "@pulumi/cloudflare";
import * as pulumi from "@pulumi/pulumi";
import { DnsRecordSpec, DnsRecordOutput, DnsRecordType } from "../../contracts/dns";
import { IProviderDns } from "../../contracts/provider";

function detectRecordType(target: string): DnsRecordType {
  if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(target)) {
    return "A";
  }
  if (target.includes(":")) {
    return "AAAA";
  }
  return "CNAME";
}

/**
 * Creates DNS records in Cloudflare.
 *
 * Used when the domain's authoritative nameservers are Cloudflare (not the
 * cloud provider). The zoneId must be provided at construction time.
 *
 * Records are created with proxy DISABLED (DNS-only / grey cloud) so that
 * Traefik handles TLS termination with Let's Encrypt certificates.
 */
export class CloudflareDnsProvider implements IProviderDns {
  constructor(private readonly zoneId: pulumi.Input<string>) {}

  createDnsRecord(spec: DnsRecordSpec): DnsRecordOutput {
    const resolvedType: pulumi.Output<DnsRecordType> = spec.recordType
      ? pulumi.output(spec.recordType)
      : pulumi.output(spec.target).apply(detectRecordType);

    const record = new cloudflare.DnsRecord(
      `cf-${spec.recordName}.${spec.zone}`,
      {
        zoneId: this.zoneId,
        type: resolvedType,
        name: spec.recordName,
        content: spec.target,
        ttl: spec.ttl ?? 300,
        // DNS-only (grey cloud) — Traefik terminates TLS, not Cloudflare.
        // This ensures cert-manager can serve HTTP-01 challenges and
        // Traefik's Let's Encrypt certs are used end-to-end.
        proxied: false,
      },
    );

    return {
      fqdn: pulumi.interpolate`${spec.recordName}.${spec.zone}`,
    };
  }
}
