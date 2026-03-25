// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { DnsRecordSpec, DnsRecordOutput, DnsRecordType } from "../../contracts/dns";
import { IProviderDns } from "../../contracts/provider";

/**
 * Detects DNS record type from target value.
 * IPv4 → A, IPv6 → AAAA, hostname → CNAME.
 *
 * DigitalOcean LBs return IPs; AWS ELBs return hostnames.
 * This ensures the correct record type regardless of provider.
 */
function detectRecordType(target: string): DnsRecordType {
  if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(target)) {
    return "A";
  }
  if (target.includes(":")) {
    return "AAAA";
  }
  return "CNAME";
}

export class DigitalOceanDnsProvider implements IProviderDns {
  createDnsRecord(spec: DnsRecordSpec): DnsRecordOutput {
    // Record type: explicit from spec, or auto-detected from target.
    // target is pulumi.Input<string>, so we resolve it for type detection.
    const resolvedType: pulumi.Output<DnsRecordType> = spec.recordType
      ? pulumi.output(spec.recordType)
      : pulumi.output(spec.target).apply(detectRecordType);

    // CNAME targets require trailing dot; A/AAAA targets are bare IPs.
    const resolvedValue = pulumi.all([pulumi.output(spec.target), resolvedType]).apply(
      ([target, type]) => (type === "CNAME" ? `${target}.` : target),
    );

    const record = new digitalocean.DnsRecord(
      `${spec.recordName}.${spec.zone}`,
      {
        domain: spec.zone,
        type: resolvedType,
        name: spec.recordName,
        value: resolvedValue,
        ttl: spec.ttl ?? 300,
      },
    );

    return {
      fqdn: record.fqdn,
    };
  }
}
