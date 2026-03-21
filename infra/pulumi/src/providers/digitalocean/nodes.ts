import { NodeClass } from "../../core/types";
import { InfraError } from "../../core/errors";

/**
 * Maps abstract NodeClass to DigitalOcean droplet sizes.
 *
 * When DO releases new sizes or deprecates old ones,
 * update THIS file only — nothing else changes.
 */
const DO_NODE_MAP: Record<NodeClass, string> = {
  small: "s-2vcpu-4gb",
  medium: "s-4vcpu-8gb",
  large: "s-8vcpu-16gb",
  xlarge: "s-16vcpu-32gb",
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
