import { CloudProviderFactory } from "../../contracts/provider";
import { NetworkSpec, NetworkOutput } from "../../contracts/network";
import { KubernetesClusterSpec, KubernetesClusterOutput } from "../../contracts/kubernetes";
import { ObjectStorageSpec, ObjectStorageOutput } from "../../contracts/storage";
import { DnsRecordSpec, DnsRecordOutput } from "../../contracts/dns";
import { InfraError } from "../../core/errors";

/**
 * AWS provider stub — not implemented yet.
 *
 * When implementing:
 * 1. Create nodes.ts with NodeClass → instance type map
 * 2. Create network.ts (VPC + subnets + IGW + NAT)
 * 3. Create kubernetes.ts (EKS + managed node group)
 * 4. Create storage.ts (S3 bucket)
 * 5. Create dns.ts (Route53 record)
 * 6. Wire them here like DigitalOceanProviderFactory
 */
export class AwsProviderFactory implements CloudProviderFactory {
  createNetwork(_spec: NetworkSpec): NetworkOutput {
    throw new InfraError({
      message: "AWS provider not implemented yet",
      code: "PROVIDER_NOT_IMPLEMENTED",
      context: { provider: "aws", resource: "network", operation: "create" },
    });
  }

  createKubernetesCluster(_spec: KubernetesClusterSpec): KubernetesClusterOutput {
    throw new InfraError({
      message: "AWS provider not implemented yet",
      code: "PROVIDER_NOT_IMPLEMENTED",
      context: { provider: "aws", resource: "kubernetes", operation: "create" },
    });
  }

  createObjectStorage(_spec: ObjectStorageSpec): ObjectStorageOutput {
    throw new InfraError({
      message: "AWS provider not implemented yet",
      code: "PROVIDER_NOT_IMPLEMENTED",
      context: { provider: "aws", resource: "storage", operation: "create" },
    });
  }

  createDnsRecord(_spec: DnsRecordSpec): DnsRecordOutput {
    throw new InfraError({
      message: "AWS provider not implemented yet",
      code: "PROVIDER_NOT_IMPLEMENTED",
      context: { provider: "aws", resource: "dns", operation: "create" },
    });
  }
}
