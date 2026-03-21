import * as pulumi from "@pulumi/pulumi";
import { BaseLabels, NodeClass } from "../core/types";

/**
 * Compute instance contract — VMs outside Kubernetes.
 *
 * Used for stateful services that don't belong in K8s:
 * - Gitea (git server, stateful, critical path)
 * - CI runners (unpredictable resource consumption)
 *
 * Each provider maps this to its own construct:
 * - DigitalOcean: Droplet
 * - AWS: EC2 Instance
 */

export interface ComputeInstanceSpec {
  readonly name: string;
  readonly region: pulumi.Input<string>;
  readonly size: NodeClass;
  readonly image: string;
  readonly network: { readonly vpcId: pulumi.Input<string> };
  readonly sshKeyIds?: pulumi.Input<string>[];
  readonly userData?: pulumi.Input<string>;
  readonly tags: BaseLabels;
}

export interface ComputeInstanceOutput {
  readonly instanceId: pulumi.Output<string>;
  readonly name: pulumi.Output<string>;
  readonly ipv4Address: pulumi.Output<string>;
  readonly privateIpv4Address: pulumi.Output<string>;
}
