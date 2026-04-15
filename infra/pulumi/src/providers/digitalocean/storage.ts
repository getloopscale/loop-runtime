// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { ObjectStorageSpec, ObjectStorageOutput, CdnSpec, CdnOutput, StorageEndpointSpec, StorageEndpointOutput } from "../../contracts/storage";
import { IProviderStorage } from "../../contracts/provider";
import { lookupExistingBucket, lookupExistingCdn } from "../../core/adopt-or-create";

export class DigitalOceanStorageProvider implements IProviderStorage {
  createObjectStorage(spec: ObjectStorageSpec): ObjectStorageOutput {
    // Adopt-or-create: check if bucket already exists (orphan from partial
    // destroy — DO Spaces buckets sometimes can't be deleted even when empty).
    const existing = lookupExistingBucket(spec.name, spec.region as string);
    // DO SpacesBucket import format: "region,bucket-name"
    const importId = existing ? `${spec.region},${existing}` : undefined;

    const bucket = new digitalocean.SpacesBucket(spec.name, {
      name: spec.name,
      region: spec.region,
      versioning: {
        enabled: spec.versioning ?? false,
      },
    }, importId ? { import: importId } : undefined);

    return {
      bucketName: bucket.name,
      endpoint: bucket.bucketDomainName,
      region: pulumi.output(spec.region),
    };
  }

  createCdn(spec: CdnSpec): CdnOutput {
    // Adopt existing CDN if it persists from a previous deploy
    const existingId = lookupExistingCdn(spec.origin as string);
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
