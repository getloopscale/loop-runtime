// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { ObjectStorageSpec, ObjectStorageOutput, CdnSpec, CdnOutput, StorageEndpointSpec, StorageEndpointOutput } from "../../contracts/storage";
import { IProviderStorage } from "../../contracts/provider";
import { lookupExistingCdn } from "../../core/adopt-or-create";

export class DigitalOceanStorageProvider implements IProviderStorage {
  createObjectStorage(spec: ObjectStorageSpec): ObjectStorageOutput {
    // NO programmatic import for SpacesBucket.
    //
    // Root cause (2026-04-16): the DO provider's SpacesBucket import creates
    // duplicate URN entries in state on every run. This corrupts state and
    // breaks idempotency. Confirmed across multiple destroy→up cycles.
    //
    // If an orphan bucket exists after partial destroy, resolve via CLI:
    //   pulumi import digitalocean:index/spacesBucket:SpacesBucket <name> <region>,<name>
    const bucket = new digitalocean.SpacesBucket(
      spec.name,
      {
        name: spec.name,
        region: spec.region,
        acl: "private",
        forceDestroy: spec.forceDestroy ?? true,
        versioning: {
          enabled: spec.versioning ?? false,
        },
      },
      spec.protect ? { protect: true } : undefined,
    );

    return {
      bucketName: bucket.name,
      endpoint: bucket.bucketDomainName,
      region: pulumi.output(spec.region),
    };
  }

  createCdn(spec: CdnSpec): CdnOutput {
    const existingId = spec.bucketName
      ? lookupExistingCdn(spec.bucketName)
      : undefined;
    const cdn = new digitalocean.Cdn("static-cdn", {
      origin: spec.origin,
      ttl: spec.ttl,
    }, existingId ? { import: existingId } : undefined);

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
