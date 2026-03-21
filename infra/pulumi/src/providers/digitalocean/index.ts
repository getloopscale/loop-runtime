import { CloudProviderFactory } from "../../contracts/provider";
import { DigitalOceanNetworkProvider } from "./network";
import { DigitalOceanKubernetesProvider } from "./kubernetes";
import { DigitalOceanStorageProvider } from "./storage";
import { DigitalOceanDnsProvider } from "./dns";

/**
 * DigitalOcean provider factory — fully functional.
 *
 * Each capability is delegated to a focused provider class (SRP).
 * Only files inside providers/digitalocean/ may import @pulumi/digitalocean.
 */
export class DigitalOceanProviderFactory implements CloudProviderFactory {
  private readonly network = new DigitalOceanNetworkProvider();
  private readonly kubernetes = new DigitalOceanKubernetesProvider();
  private readonly storage = new DigitalOceanStorageProvider();
  private readonly dns = new DigitalOceanDnsProvider();

  createNetwork = this.network.createNetwork.bind(this.network);
  createKubernetesCluster = this.kubernetes.createKubernetesCluster.bind(this.kubernetes);
  createObjectStorage = this.storage.createObjectStorage.bind(this.storage);
  createDnsRecord = this.dns.createDnsRecord.bind(this.dns);
}
