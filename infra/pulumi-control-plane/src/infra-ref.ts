// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import type { InfraStackOutputs, PlatformStackOutputs } from "./contracts";

/**
 * Resolves upstream StackReference outputs for control-plane.
 *
 * Reference targets are parametrized via config keys:
 *   infraStackRef    → default: loopzero-infra/{currentStack}
 *   platformStackRef → default: loopzero-platform/{currentStack}
 *
 * Override per-stack: `pulumi config set infraStackRef org/loopzero-infra/dev`
 */

const cfg = new pulumi.Config();
const currentStack = pulumi.getStack();
const infraRef = new pulumi.StackReference(cfg.get("infraStackRef") ?? `loopzero-infra/${currentStack}`);
const platformRef = new pulumi.StackReference(cfg.get("platformStackRef") ?? `loopzero-platform/${currentStack}`);

function infraGet<K extends keyof InfraStackOutputs>(key: K): pulumi.Output<InfraStackOutputs[K]> {
  return infraRef.getOutput(key) as pulumi.Output<InfraStackOutputs[K]>;
}

function infraSecret<K extends keyof InfraStackOutputs>(key: K): pulumi.Output<InfraStackOutputs[K]> {
  return infraRef.requireOutput(key) as pulumi.Output<InfraStackOutputs[K]>;
}

function platformGet<K extends keyof PlatformStackOutputs>(key: K): pulumi.Output<PlatformStackOutputs[K]> {
  return platformRef.getOutput(key) as pulumi.Output<PlatformStackOutputs[K]>;
}

/**
 * Infra outputs consumed by control-plane.
 * Only fields actually referenced by control-plane functions.
 * No invented values — every field from a real StackReference output.
 */
export const infra = {
  mgmtKubeconfig: infraSecret("mgmtKubeconfig"),
  buildKubeconfig: infraSecret("buildKubeconfig"),
  runtimeKubeconfig: infraSecret("runtimeKubeconfig"),

  mgmtClusterEndpoint: infraGet("mgmtClusterEndpoint"),
  buildClusterEndpoint: infraGet("buildClusterEndpoint"),
  runtimeClusterEndpoint: infraGet("runtimeClusterEndpoint"),

  dbHost: infraGet("dbHost"),
  dbPort: infraGet("dbPort"),
  dbUri: infraSecret("dbUri"),
  dbPassword: infraSecret("dbPassword"),

  registryEndpoint: infraGet("registryEndpoint"),
  registryName: infraGet("registryName"),
  spacesEndpoint: infraGet("spacesEndpoint"),
};

/** Platform outputs consumed by control-plane */
export const platform = {
  lokiEndpoint: platformGet("lokiEndpoint"),
  cfKvNamespaceId: platformGet("cfKvNamespaceId"),
};
