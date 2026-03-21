import * as pulumi from "@pulumi/pulumi";

export interface DnsRecordSpec {
  readonly zone: string;
  readonly recordName: string;
  readonly target: pulumi.Input<string>;
  readonly ttl?: number;
}

export interface DnsRecordOutput {
  readonly fqdn: pulumi.Output<string>;
}
