// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { CloudProviderFactory } from "../../contracts/provider";
import { NetworkSpec, NetworkOutput } from "../../contracts/network";
import { KubernetesClusterSpec, KubernetesClusterOutput } from "../../contracts/kubernetes";
import { ObjectStorageSpec, ObjectStorageOutput } from "../../contracts/storage";
import { DnsRecordSpec, DnsRecordOutput } from "../../contracts/dns";
import { ComputeInstanceSpec, ComputeInstanceOutput } from "../../contracts/compute";
import { DatabaseClusterSpec, DatabaseClusterOutput } from "../../contracts/database";
import { ContainerRegistrySpec, ContainerRegistryOutput } from "../../contracts/registry";
import { InfraError } from "../../core/errors";

/**
 * AWS provider stub — not implemented yet.
 *
 * When implementing:
 * 1. Create nodes.ts with NodeClass → instance type map (EC2 + RDS)
 * 2. Create network.ts (VPC + public/private subnets + IGW + NAT)
 * 3. Create kubernetes.ts (EKS + managed node group)
 * 4. Create storage.ts (S3 bucket)
 * 5. Create dns.ts (Route53 record)
 * 6. Create compute.ts (EC2 instances for Gitea/runners)
 * 7. Create database.ts (RDS PostgreSQL)
 * 8. Wire them here like DigitalOceanProviderFactory
 */
export class AwsProviderFactory implements CloudProviderFactory {
  createNetwork(_spec: NetworkSpec): NetworkOutput {
    throw this.notImplemented("network");
  }

  createKubernetesCluster(_spec: KubernetesClusterSpec): KubernetesClusterOutput {
    throw this.notImplemented("kubernetes");
  }

  createObjectStorage(_spec: ObjectStorageSpec): ObjectStorageOutput {
    throw this.notImplemented("storage");
  }

  createDnsRecord(_spec: DnsRecordSpec): DnsRecordOutput {
    throw this.notImplemented("dns");
  }

  createComputeInstance(_spec: ComputeInstanceSpec): ComputeInstanceOutput {
    throw this.notImplemented("compute");
  }

  createDatabaseCluster(_spec: DatabaseClusterSpec): DatabaseClusterOutput {
    throw this.notImplemented("database");
  }

  createContainerRegistry(_spec: ContainerRegistrySpec): ContainerRegistryOutput {
    throw this.notImplemented("registry");
  }

  private notImplemented(resource: string): InfraError {
    return new InfraError({
      message: "AWS provider not implemented yet",
      code: "PROVIDER_NOT_IMPLEMENTED",
      context: { provider: "aws", resource, operation: "create" },
    });
  }
}
