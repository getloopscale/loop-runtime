// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { BaseLabels, Environment } from "./types";

/**
 * Generates standard tags/labels for all resources.
 * Every cloud resource MUST carry these tags — no exceptions.
 *
 * Used for: cost attribution, lifecycle management, audit trails.
 */
export function standardTags(project: string, environment: Environment): BaseLabels {
  return {
    project,
    environment,
    managedBy: "pulumi",
  };
}

/**
 * Converts BaseLabels to a flat string array for providers that use tag arrays (e.g. DO).
 * Format: "key:value"
 */
export function tagsToStringArray(labels: BaseLabels): string[] {
  return [
    `project:${labels.project}`,
    `env:${labels.environment}`,
    `managed-by:${labels.managedBy}`,
    ...(labels.owner ? [`owner:${labels.owner}`] : []),
    ...(labels.costCenter ? [`cost-center:${labels.costCenter}`] : []),
  ];
}
