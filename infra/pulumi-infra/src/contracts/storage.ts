// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import { BaseLabels } from "../core/types";

export interface ObjectStorageSpec {
  readonly name: string;
  readonly region: pulumi.Input<string>;
  readonly versioning?: boolean;
  /**
   * Days after which an object expires. Omitted means the bucket keeps objects
   * forever, which is correct for versioned artifact buckets a rollback may
   * still need and wrong for anything that grows with customer activity.
   *
   * Added 2026-09-07 (system-design audit SD-15.8): the in-cluster MinIO expires
   * source bundles at 30 days through an ILM rule applied by a Taskfile, and the
   * production Spaces bucket that actually holds them declared no retention at
   * all. Retention belongs in the infrastructure that creates the bucket.
   */
  readonly expireAfterDays?: number;
  readonly tags: BaseLabels;
}

export interface ObjectStorageOutput {
  readonly bucketName: pulumi.Output<string>;
  readonly endpoint: pulumi.Output<string>;
  readonly region: pulumi.Output<string>;
}

export interface CdnSpec {
  /** Origin: bucket domain name (e.g., "loop-runtime-static-assets.sfo3.digitaloceanspaces.com") */
  readonly origin: pulumi.Input<string>;
  /** Edge cache TTL in seconds */
  readonly ttl: number;
}

export interface CdnOutput {
  readonly cdnEndpoint: pulumi.Output<string>;
}

/**
 * S3-compatible storage endpoint for the provider's region.
 * Eliminates hardcoded endpoint strings like "sfo3.digitaloceanspaces.com".
 */
export interface StorageEndpointSpec {
  readonly region: string;
}

export interface StorageEndpointOutput {
  /** Full HTTPS endpoint (e.g., "https://sfo3.digitaloceanspaces.com") */
  readonly endpoint: pulumi.Output<string>;
  /** S3-compatible endpoint for SDKs (e.g., "https://s3.sfo3.digitaloceanspaces.com") */
  readonly s3Endpoint: pulumi.Output<string>;
}
