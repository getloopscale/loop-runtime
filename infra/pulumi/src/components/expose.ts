import * as pulumi from "@pulumi/pulumi";
import { BootstrapOutputs } from "./bootstrap";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { CloudProviderFactory } from "../contracts/provider";

/**
 * Phase 3 outputs — external access configured.
 */
export interface ExposeOutputs {
  readonly dnsConfigured: pulumi.Output<boolean>;
}

/**
 * Phase 3 — Expose Platform
 *
 * Creates DNS records pointing to the ingress load balancer.
 * Only runs AFTER Phase 2 confirms ingress is ready.
 *
 * NOT IMPLEMENTED YET — depends on Phase 2 being validated in a real cluster.
 * When implementing:
 * 1. Use bootstrap.ingressEndpoint as the DNS target
 * 2. Create DNS records via the provider factory (A record for IP, CNAME for hostname)
 * 3. Use config.domain for the target domain
 * 4. Optionally wait for DNS propagation
 */
export function exposePlatform(
  _bootstrap: BootstrapOutputs,
  _infra: InfraOutputs,
  _config: PlatformStackConfig,
  _factory: CloudProviderFactory,
): ExposeOutputs {
  return {
    dnsConfigured: pulumi.output(false),
  };
}
