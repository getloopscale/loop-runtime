// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";

/**
 * Execution state tracking for the production topology.
 *
 * Tracks per-phase and per-component readiness derived from actual
 * Pulumi Outputs — not from program control flow.
 *
 * Query: `pulumi stack output execution --json`
 */

export type PhaseName = "infra" | "bootstrap" | "expose";
export type ComponentName =
  | "network"
  | "mgmtCluster"
  | "buildCluster"
  | "runtimeCluster"
  | "runtimeCluster2"
  | "database"
  | "storage"
  | "registry"
  | "mgmtIngress"
  | "mgmtCertManager"
  | "mgmtArgocd"
  | "mgmtGitea"
  | "mgmtPrometheus"
  | "mgmtGrafana"
  | "mgmtLoopRuntimeApi"
  | "mgmtTenantController"
  | "mgmtTls"
  | "buildArgoWorkflows"
  | "runtimeIngress"
  | "mgmtDns"
  | "runtimeDns";
export type PhaseStatus = "completed" | "pending";

export interface ComponentRecord {
  readonly component: ComponentName;
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
  readonly phases: PhaseRecord[];
}

/**
 * Builds execution state from component readiness signals.
 *
 * A phase is "completed" when ALL its components are "completed".
 * Each component's status is derived from a real Pulumi Output<boolean>.
 */
export function buildExecutionState(args: {
  environment: string;
  provider: string;
  infraVersion: string;
  components: Record<ComponentName, pulumi.Output<boolean>>;
}): pulumi.Output<ExecutionState> {
  const stack = pulumi.getStack();
  const installationId = `${stack}-${args.infraVersion}`;

  const componentNames = Object.keys(args.components) as ComponentName[];
  const componentOutputs = componentNames.map((name) => args.components[name]);

  return pulumi.all(componentOutputs).apply((values) => {
    const componentMap: Record<string, boolean> = {};
    componentNames.forEach((name, i) => {
      componentMap[name] = values[i];
    });

    const infraComponents: ComponentName[] = [
      "network",
      "mgmtCluster",
      "buildCluster",
      "runtimeCluster",
      "database",
      "storage",
      "registry",
    ];
    const bootstrapComponents: ComponentName[] = [
      "mgmtIngress",
      "mgmtCertManager",
      "mgmtArgocd",
      "mgmtGitea",
      "mgmtPrometheus",
      "mgmtGrafana",
      "mgmtLoopRuntimeApi",
      "mgmtTenantController",
      "mgmtTls",
      "buildArgoWorkflows",
      "runtimeIngress",
    ];
    const exposeComponents: ComponentName[] = ["mgmtDns", "runtimeDns"];

    function buildPhase(
      phase: PhaseName,
      names: ComponentName[],
    ): PhaseRecord {
      const components = names.map((name) => ({
        component: name,
        status: toStatus(componentMap[name] ?? false),
      }));
      const allCompleted = components.every((c) => c.status === "completed");
      return {
        phase,
        status: toStatus(allCompleted),
        components,
      };
    }

    return {
      installationId,
      stack,
      environment: args.environment,
      provider: args.provider,
      infraVersion: args.infraVersion,
      phases: [
        buildPhase("infra", infraComponents),
        buildPhase("bootstrap", bootstrapComponents),
        buildPhase("expose", exposeComponents),
      ],
    };
  });
}

function toStatus(ready: boolean): PhaseStatus {
  return ready ? "completed" : "pending";
}
