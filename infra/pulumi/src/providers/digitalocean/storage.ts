// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { ObjectStorageSpec, ObjectStorageOutput, CdnSpec, CdnOutput, StorageEndpointSpec, StorageEndpointOutput } from "../../contracts/storage";
import { IProviderStorage } from "../../contracts/provider";
// Only CDN adopt-or-create remains. SpacesBucket import was removed because it causes
// duplicate URN entries in state on every run (DO provider bug with import).
import { lookupExistingCdn } from "../../core/adopt-or-create";

export class DigitalOceanStorageProvider implements IProviderStorage {
  createObjectStorage(spec: ObjectStorageSpec): ObjectStorageOutput {
    // DO NOT use adopt-or-create (import) for SpacesBuckets.
    //
    // Root cause analysis (2026-04-16): The `{ import: id }` option on SpacesBucket
    // causes Pulumi to create DUPLICATE state entries on every run, even when the
    // resource is already managed. This corrupts the state (4x entries per bucket)
    // and breaks idempotency. The bug is in the DO provider's import handling —
    // it doesn't detect that the URN already exists in state.
    //
    // Solution: Let Pulumi create the bucket normally. On fresh deploy after destroy,
    // the bucket may already exist (orphan from partial destroy). In that case,
    // the DO provider returns a 409 Conflict and `pulumi up` fails — but the
    // pre-destroy-cleanup.sh script now deletes all stack-scoped buckets before
    // state reset, so orphan buckets should not exist on a clean deploy.
    //
    // If orphan adoption is ever needed again, use `pulumi import` CLI command
    // (one-time operation) instead of the programmatic import option.
    const bucket = new digitalocean.SpacesBucket(spec.name, {
      name: spec.name,
      region: spec.region,
      acl: "private",
      forceDestroy: true,
      versioning: {
        enabled: spec.versioning ?? false,
      },
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
