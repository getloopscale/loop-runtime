import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { DnsRecordSpec, DnsRecordOutput } from "../../contracts/dns";
import { IProviderDns } from "../../contracts/provider";

export class DigitalOceanDnsProvider implements IProviderDns {
  createDnsRecord(spec: DnsRecordSpec): DnsRecordOutput {
    const record = new digitalocean.DnsRecord(
      `${spec.recordName}.${spec.zone}`,
      {
        domain: spec.zone,
        type: "CNAME",
        name: spec.recordName,
        value: pulumi.interpolate`${spec.target}.`,
        ttl: spec.ttl ?? 300,
      },
    );

    return {
      fqdn: record.fqdn,
    };
  }
}
