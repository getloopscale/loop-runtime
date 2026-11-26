// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import { BaseLabels } from "../core/types";

export interface ObjectStorageSpec {
  readonly name: string;
  readonly region: pulumi.Input<string>;
  readonly versioning?: boolean;
  readonly tags: BaseLabels;
  /**
   * When true, mark the bucket as protected at the Pulumi resource level
   * so accidental `pulumi destroy` does not delete it. Used for backup
   * buckets in production where loss is unrecoverable. Default: false.
   */
  readonly protect?: boolean;
  /**
   * When false, refuse to delete the bucket if it still has objects.
   * Defaults to true (legacy behavior — empty + delete). Set false on
   * backup buckets in production to add a second guard against accidental
   * destroy of non-empty backup data.
   */
  readonly forceDestroy?: boolean;
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
  /** Bucket name for adopt-or-create lookup (plain string, not Output) */
  readonly bucketName?: string;
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
