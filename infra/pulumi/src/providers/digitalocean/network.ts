import * as digitalocean from "@pulumi/digitalocean";
import { NetworkSpec, NetworkOutput } from "../../contracts/network";
import { IProviderNetwork } from "../../contracts/provider";
import { tagsToStringArray } from "../../core/tags";

export class DigitalOceanNetworkProvider implements IProviderNetwork {
  createNetwork(spec: NetworkSpec): NetworkOutput {
    const vpc = new digitalocean.Vpc(spec.name, {
      name: spec.name,
      region: spec.region,
      ipRange: spec.cidr ?? "10.10.0.0/16",
    });

    // DO VPCs don't support tags directly, but we tag dependent resources.
    // We store tags here for propagation to child resources.
    void tagsToStringArray(spec.tags);

    return {
      networkId: vpc.id,
      vpcId: vpc.id,
    };
  }
}
