// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";

/**
 * Execution state tracking for the production topology.
 *
 * Tracks per-phase and per-component readiness derived from actual
 * Pulumi Outputs — not from program control flow.
 *
 * Multi-cluster: components are derived dynamically from the cluster registry,
 * not from a fixed enum. This supports N clusters per role.
 *
 * Query: `pulumi stack output execution --json`
 */

export type PhaseName = "infra" | "bootstrap" | "expose";
export type PhaseStatus = "completed" | "pending";

export interface ComponentRecord {
  readonly component: string;
  readonly status: PhaseStatus;
}

export interface PhaseRecord {
  readonly phase: PhaseName;
  readonly status: PhaseStatus;
  readonly components: ComponentRecord[];
}

export interface ExecutionState {
  readonly installationId: string;
  readonly stack: string;
  readonly environment: string;
  readonly provider: string;
  readonly infraVersion: string;
  readonly clusterCount: number;
  readonly phases: PhaseRecord[];
}

/**
 * Builds execution state from dynamic component readiness signals.
 *
 * Components are passed as a string-keyed record (not a fixed enum),
 * allowing N clusters per role. Phase assignment is derived from
 * component name prefix:
 *   - "cluster:" prefix → infra phase
 *   - "bootstrap:" prefix → bootstrap phase
 *   - "expose:" prefix → expose phase
 *   - no prefix → infra phase (legacy compatibility)
 */
export function buildExecutionState(args: {
  environment: string;
  provider: string;
  infraVersion: string;
  clusterCount: number;
  components: Record<string, pulumi.Output<boolean>>;
}): pulumi.Output<ExecutionState> {
  const stack = pulumi.getStack();
  const installationId = `${stack}-${args.infraVersion}`;

  const componentNames = Object.keys(args.components);
  const componentOutputs = componentNames.map((name) => args.components[name]);

  return pulumi.all(componentOutputs).apply((values) => {
    const componentMap: Record<string, boolean> = {};
    componentNames.forEach((name, i) => {
      componentMap[name] = values[i];
    });

    // Classify components into phases by prefix
    const infraNames: string[] = [];
    const bootstrapNames: string[] = [];
    const exposeNames: string[] = [];

    for (const name of componentNames) {
      if (name.startsWith("bootstrap:")) {
        bootstrapNames.push(name);
      } else if (name.startsWith("expose:")) {
        exposeNames.push(name);
      } else {
        infraNames.push(name); // cluster:*, database, storage, registry, network
      }
    }

    function buildPhase(phase: PhaseName, names: string[]): PhaseRecord {
      const components = names.map((name) => ({
        component: name,
        status: toStatus(componentMap[name] ?? false),
      }));
      const allCompleted = components.length > 0 && components.every((c) => c.status === "completed");
      return { phase, status: toStatus(allCompleted), components };
    }

    return {
      installationId,
      stack,
      environment: args.environment,
      provider: args.provider,
      infraVersion: args.infraVersion,
      clusterCount: args.clusterCount,
      phases: [
        buildPhase("infra", infraNames),
        buildPhase("bootstrap", bootstrapNames),
        buildPhase("expose", exposeNames),
      ],
    };
  });
}

function toStatus(ready: boolean): PhaseStatus {
  return ready ? "completed" : "pending";
}
