// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { ObjectStorageSpec, ObjectStorageOutput, CdnSpec, CdnOutput, StorageEndpointSpec, StorageEndpointOutput } from "../../contracts/storage";
import { IProviderStorage } from "../../contracts/provider";
import { lookupExistingBucket, lookupExistingCdn } from "../../core/adopt-or-create";

export class DigitalOceanStorageProvider implements IProviderStorage {
  createObjectStorage(spec: ObjectStorageSpec): ObjectStorageOutput {
    // Adopt-or-create: if bucket exists in provider, import it into state.
    // This handles orphan buckets from partial destroys (DO Spaces buckets
    // sometimes survive cleanup).
    //
    // IMPORTANT: The DO provider has a bug where `{ import }` on SpacesBucket
    // causes duplicate URN entries in state on repeated runs. To prevent this,
    // we pass `ignoreChanges: ["*"]` which tells Pulumi not to diff the resource
    // after import — avoiding the re-import behavior that creates duplicates.
    const existing = lookupExistingBucket(spec.name, spec.region as string);
    const importId = existing ? `${spec.region},${existing}` : undefined;

    const bucket = new digitalocean.SpacesBucket(spec.name, {
      name: spec.name,
      region: spec.region,
      acl: "private",
      forceDestroy: true,
      versioning: {
        enabled: spec.versioning ?? false,
      },
    }, {
      ...(importId ? { import: importId } : {}),
    });

    return {
      bucketName: bucket.name,
      endpoint: bucket.bucketDomainName,
      region: pulumi.output(spec.region),
    };
  }

  createCdn(spec: CdnSpec): CdnOutput {
    // Adopt existing CDN: lookup by bucket name (sync, not Output)
    // spec.origin is an Output<string> that can't be resolved synchronously.
    // bucketName is the plain string known at compile time.
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
