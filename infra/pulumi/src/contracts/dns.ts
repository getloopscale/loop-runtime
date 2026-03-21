import * as pulumi from "@pulumi/pulumi";

export type DnsRecordType = "A" | "CNAME" | "AAAA";

export interface DnsRecordSpec {
  readonly zone: string;
  readonly recordName: string;
  readonly target: pulumi.Input<string>;
  /** If omitted, provider auto-detects from target (IP → A, hostname → CNAME) */
  readonly recordType?: DnsRecordType;
  readonly ttl?: number;
}

export interface DnsRecordOutput {
  readonly fqdn: pulumi.Output<string>;
}
