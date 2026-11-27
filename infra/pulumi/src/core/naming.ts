// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { Environment } from "./types";

interface NamingArgs {
  readonly project: string;
  readonly env: Environment;
  readonly region: string;
  readonly suffix?: string;
}

/**
 * Generates a deterministic resource name.
 * Format: {project}-{env}-{region}[-{suffix}]
 *
 * All Pulumi resources MUST use this function for naming consistency
 * and to avoid collisions across environments.
 */
export function resourceName(args: NamingArgs): string {
  const base = `${args.project}-${args.env}-${args.region}`;
  return args.suffix ? `${base}-${args.suffix}` : base;
}

/**
 * Generates a bucket-safe name (lowercase, no underscores).
 * S3 and Spaces both require lowercase bucket names with no underscores.
 */
export function bucketName(args: NamingArgs): string {
  return resourceName(args).replace(/_/g, "-").toLowerCase();
}
