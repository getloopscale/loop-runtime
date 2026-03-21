import * as pulumi from "@pulumi/pulumi";
import { BootstrapOutputs } from "./bootstrap";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { CloudProviderFactory } from "../contracts/provider";

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
 * Phase 3 — Expose Platform
 *
 * Creates DNS records pointing to the ingress load balancers
 * of both clusters.
 *
 * NOT IMPLEMENTED YET — depends on Phase 2 being validated in real clusters.
 *
 * When implementing:
 * 1. Use bootstrap.mgmt.ingressEndpoint for management DNS (e.g., argocd.loopzero.example)
 * 2. Use bootstrap.runtime.ingressEndpoint for app DNS (e.g., *.loopzero.example)
 * 3. Handle A records (for IPs) vs CNAME records (for hostnames)
 * 4. Use config.domain for the base domain
 * 5. Use factory.createDnsRecord() for provider-agnostic DNS creation
 */
export function exposePlatform(
  _bootstrap: BootstrapOutputs,
  _infra: InfraOutputs,
  _config: PlatformStackConfig,
  _factory: CloudProviderFactory,
): ExposeOutputs {
  return {
    mgmtDnsConfigured: pulumi.output(false),
    runtimeDnsConfigured: pulumi.output(false),
  };
}
