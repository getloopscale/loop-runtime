import * as digitalocean from "@pulumi/digitalocean";
import { IProviderCompute } from "../../contracts/provider";
import { ComputeInstanceSpec, ComputeInstanceOutput } from "../../contracts/compute";
import { resolveNodeSize } from "./nodes";
import { tagsToStringArray } from "../../core/tags";

/**
 * DigitalOcean compute provider — creates Droplets.
 *
 * Used for stateful services outside K8s:
 * - Gitea server (git state, critical path)
 * - CI/CD runners (unpredictable resource consumption)
 *
 * Droplets are placed inside the VPC for private networking
 * with the K8s clusters and managed database.
 */
export class DigitalOceanComputeProvider implements IProviderCompute {
  createComputeInstance(spec: ComputeInstanceSpec): ComputeInstanceOutput {
    const droplet = new digitalocean.Droplet(spec.name, {
      name: spec.name,
      region: spec.region,
      size: resolveNodeSize(spec.size),
      image: spec.image,
      vpcUuid: spec.network.vpcId,
      sshKeys: spec.sshKeyIds,
      userData: spec.userData,
      tags: tagsToStringArray(spec.tags),
      monitoring: true,
      dropletAgent: true,
    });

    return {
      instanceId: droplet.id,
      name: droplet.name,
      ipv4Address: droplet.ipv4Address,
      privateIpv4Address: droplet.ipv4AddressPrivate,
    };
  }
}
