// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import { CloudProvider, Environment, NodeClass } from "../core/types";

/**
 * Infrastructure stack configuration — only cloud primitive fields.
 * Platform and control-plane configs live in their own projects.
 */
export interface InfraStackConfig {
  readonly project: string;
  readonly environment: Environment;
  readonly provider: CloudProvider;
  readonly region: string;
  readonly domain: string;
  readonly dnsZone: string;
  readonly infraVersion: string;

  // Management cluster
  readonly mgmtK8sVersion?: string;
  readonly mgmtNodeClass: NodeClass;
  readonly mgmtMinNodes: number;
  readonly mgmtMaxNodes?: number;

  // Runtime cluster
  readonly runtimeK8sVersion?: string;
  readonly runtimeNodeClass: NodeClass;
  readonly runtimeMinNodes: number;
  readonly runtimeMaxNodes?: number;

  // Build cluster
  readonly buildK8sVersion?: string;
  readonly buildNodeClass: NodeClass;
  readonly buildMinNodes: number;
  readonly buildMaxNodes?: number;

  // Database
  readonly dbNodeClass: NodeClass;
  readonly dbVersion: string;
  readonly dbNodeCount: number;

  // SSH keys
  readonly sshKeyIds?: string[];

  // Secrets — cloud provider credentials
  readonly spacesAccessKeyId: pulumi.Output<string>;
  readonly spacesSecretAccessKey: pulumi.Output<string>;
  readonly digitaloceanToken: pulumi.Output<string>;

  // Cloudflare — DNS zone (if NS is Cloudflare)
  readonly cfZoneId?: string;
}

// NOT CALLED (measured 2026-09-07): the only occurrence of loadInfraStackConfig
// in the pulumi source trees is this declaration. src/index.ts reads its config
// by another path, so this loader is a prepared seam rather than the live one.
export function loadInfraStackConfig(): InfraStackConfig {
  const config = new pulumi.Config();

  return {
    project: pulumi.getProject(),
    environment: config.require("environment") as Environment,
    provider: config.require("provider") as CloudProvider,
    region: config.require("region"),
    domain: config.require("domain"),
    dnsZone: config.get("dnsZone") ?? config.require("domain"),
    infraVersion: config.get("infraVersion") ?? "v1",

    // Management cluster
    mgmtK8sVersion: config.get("mgmtK8sVersion") ?? undefined,
    mgmtNodeClass: config.require("mgmtNodeClass") as NodeClass,
    mgmtMinNodes: config.requireNumber("mgmtMinNodes"),
    mgmtMaxNodes: config.getNumber("mgmtMaxNodes") ?? undefined,

    // Runtime cluster
    runtimeK8sVersion: config.get("runtimeK8sVersion") ?? undefined,
    runtimeNodeClass: config.require("runtimeNodeClass") as NodeClass,
    runtimeMinNodes: config.requireNumber("runtimeMinNodes"),
    runtimeMaxNodes: config.getNumber("runtimeMaxNodes") ?? undefined,

    // Build cluster
    buildK8sVersion: config.get("buildK8sVersion") ?? undefined,
    buildNodeClass: (config.get("buildNodeClass") ?? "small") as NodeClass,
    buildMinNodes: config.getNumber("buildMinNodes") ?? 2,
    buildMaxNodes: config.getNumber("buildMaxNodes") ?? undefined,

    // Database
    dbNodeClass: (config.get("dbNodeClass") ?? "small") as NodeClass,
    dbVersion: config.get("dbVersion") ?? "16",
    dbNodeCount: config.getNumber("dbNodeCount") ?? 1,

    // SSH keys
    sshKeyIds: config.getObject<string[]>("sshKeyIds") ?? undefined,

    // Secrets
    spacesAccessKeyId: config.requireSecret("spacesAccessKeyId"),
    spacesSecretAccessKey: config.requireSecret("spacesSecretAccessKey"),
    digitaloceanToken: config.requireSecret("digitaloceanToken"),

    // Cloudflare DNS
    cfZoneId: config.get("cfZoneId") ?? undefined,
  };
}
