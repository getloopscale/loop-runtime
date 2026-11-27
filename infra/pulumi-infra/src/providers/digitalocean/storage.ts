// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { ObjectStorageSpec, ObjectStorageOutput, CdnSpec, CdnOutput, StorageEndpointSpec, StorageEndpointOutput } from "../../contracts/storage";
import { IProviderStorage } from "../../contracts/provider";

export class DigitalOceanStorageProvider implements IProviderStorage {
  createObjectStorage(spec: ObjectStorageSpec): ObjectStorageOutput {
    // DO Spaces don't support tags — tracked via naming convention only.
    const bucket = new digitalocean.SpacesBucket(spec.name, {
      name: spec.name,
      region: spec.region,
      versioning: {
        enabled: spec.versioning ?? false,
      },
      // Retention travels with the bucket, not with a Taskfile (SD-15.8).
      // Applied to the whole bucket: these buckets hold one kind of object each,
      // so a prefix would be a distinction without a difference.
      ...(spec.expireAfterDays !== undefined
        ? {
            lifecycleRules: [
              {
                enabled: true,
                expiration: { days: spec.expireAfterDays },
              },
            ],
          }
        : {}),
    });

    return {
      bucketName: bucket.name,
      endpoint: bucket.bucketDomainName,
      region: pulumi.output(spec.region),
    };
  }

  createCdn(spec: CdnSpec): CdnOutput {
    const cdn = new digitalocean.Cdn("static-cdn", {
      origin: spec.origin,
      ttl: spec.ttl,
    });

    return {
      cdnEndpoint: cdn.endpoint,
    };
  }

  getStorageEndpoint(spec: StorageEndpointSpec): StorageEndpointOutput {
    return {
      endpoint: pulumi.output(`https://${spec.region}.digitaloceanspaces.com`),
      s3Endpoint: pulumi.output(`https://s3.${spec.region}.digitaloceanspaces.com`),
    };
  }
}
