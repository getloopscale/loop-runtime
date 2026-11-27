// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { NodeClass } from "../../core/types";
import { InfraError } from "../../core/errors";

/**
 * Maps abstract NodeClass to DigitalOcean droplet sizes.
 *
 * Used for K8s node pools AND standalone Droplets (Gitea, runners).
 * When DO releases new sizes or deprecates old ones,
 * update THIS file only — nothing else changes.
 */
const DO_NODE_MAP: Record<NodeClass, string> = {
  small: "s-2vcpu-4gb",
  medium: "s-4vcpu-8gb-amd", // AMD variant — s-4vcpu-8gb unavailable in SFO3
  large: "s-8vcpu-16gb",
  xlarge: "s-16vcpu-32gb",
};

/**
 * Maps abstract NodeClass to DigitalOcean managed database sizes.
 *
 * DB sizes use a different slug format than droplets.
 * See: https://docs.digitalocean.com/reference/api/api-reference/#tag/Databases
 */
const DO_DB_NODE_MAP: Record<NodeClass, string> = {
  small: "db-s-1vcpu-2gb",
  medium: "db-s-2vcpu-4gb",
  large: "db-s-4vcpu-8gb",
  xlarge: "db-s-8vcpu-16gb",
};

export function resolveNodeSize(nodeClass: NodeClass): string {
  const size = DO_NODE_MAP[nodeClass];
  if (!size) {
    throw new InfraError({
      message: `Node class "${nodeClass}" has no mapping for DigitalOcean`,
      code: "NODE_CLASS_NOT_SUPPORTED",
      context: { provider: "digitalocean", resource: "kubernetes", nodeClass },
    });
  }
  return size;
}

export function resolveDbSize(nodeClass: NodeClass): string {
  const size = DO_DB_NODE_MAP[nodeClass];
  if (!size) {
    throw new InfraError({
      message: `Node class "${nodeClass}" has no DB mapping for DigitalOcean`,
      code: "NODE_CLASS_NOT_SUPPORTED",
      context: { provider: "digitalocean", resource: "database", nodeClass },
    });
  }
  return size;
}
