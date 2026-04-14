// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import { IProviderRegistry } from "../../contracts/provider";
import { ContainerRegistrySpec, ContainerRegistryOutput } from "../../contracts/registry";

/**
 * Harbor registry provider.
 *
 * Harbor is deployed via Helm chart on the management cluster, not as a
 * cloud provider resource. This provider fulfills the ISP contract by
 * returning deterministic endpoints derived from the Harbor Helm release
 * configuration (namespace + ingress hostname).
 *
 * The actual Harbor deployment is managed by the bootstrap phase
 * (Helm chart in infra/helm/harbor/).
 *
 * S3 backend storage for container images is provisioned separately
 * as a DO Spaces bucket in the infra phase (container-images bucket).
 */
export class HarborRegistryProvider implements IProviderRegistry {
  private readonly externalUrl: string;

  /**
   * @param externalUrl  The external Harbor URL reachable by build and workload
   *                     clusters (e.g. "harbor.dev.loopzero.example"). Must NOT
   *                     include the protocol scheme — it is added automatically.
   */
  constructor(externalUrl: string) {
    this.externalUrl = externalUrl;
  }

  createContainerRegistry(spec: ContainerRegistrySpec): ContainerRegistryOutput {
    // Harbor is a Helm release, not a cloud resource. We use a ComponentResource
    // as a logical marker so that downstream Pulumi resources can express
    // dependency on the registry existing in the stack graph.
    const marker = new HarborRegistryMarker(spec.name, {
      endpoint: this.externalUrl,
    });

    return {
      endpoint: pulumi.output(this.externalUrl),
      name: pulumi.output(spec.name),
      resource: marker,
    };
  }
}

/**
 * Logical Pulumi resource for dependency tracking.
 *
 * No actual cloud resource is created. This marker allows other Pulumi
 * resources to declare dependsOn the registry, and exposes the endpoint
 * as a stack output for cross-stack references.
 */
class HarborRegistryMarker extends pulumi.ComponentResource {
  public readonly endpoint: pulumi.Output<string>;

  constructor(name: string, args: { endpoint: string }, opts?: pulumi.ComponentResourceOptions) {
    super("loopzero:harbor:RegistryMarker", name, {}, opts);

    this.endpoint = pulumi.output(args.endpoint);

    this.registerOutputs({
      endpoint: this.endpoint,
    });
  }
}
