import * as pulumi from "@pulumi/pulumi";
import { CloudProvider, Environment, NodeClass } from "../core/types";

export interface PlatformStackConfig {
  readonly project: string;
  readonly environment: Environment;
  readonly provider: CloudProvider;
  readonly region: string;
  readonly domain: string;
  readonly k8sVersion?: string;
  readonly nodeClass: NodeClass;
  readonly minNodes: number;
  readonly maxNodes?: number;
  readonly infraVersion: string;
}

/**
 * Loads stack configuration from Pulumi config.
 * All required values fail-fast if missing — no silent defaults.
 */
export function loadStackConfig(): PlatformStackConfig {
  const config = new pulumi.Config();

  return {
    project: pulumi.getProject(),
    environment: config.require("environment") as Environment,
    provider: config.require("provider") as CloudProvider,
    region: config.require("region"),
    domain: config.require("domain"),
    k8sVersion: config.get("k8sVersion") ?? undefined,
    nodeClass: config.require("nodeClass") as NodeClass,
    minNodes: config.requireNumber("minNodes"),
    maxNodes: config.getNumber("maxNodes") ?? undefined,
    infraVersion: config.get("infraVersion") ?? "v1",
  };
}
