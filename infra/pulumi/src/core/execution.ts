import * as pulumi from "@pulumi/pulumi";

/**
 * Execution state tracking for the three-phase provisioning pipeline.
 *
 * In Pulumi, the program declares a resource graph; the engine provisions it.
 * This means:
 * - "completed" = Pulumi resolved all Outputs for this phase (resources exist)
 * - "pending"   = phase was declared but Outputs haven't resolved yet (stub, or
 *                 first run before `pulumi up` finishes)
 * - "failed"    = not tracked here — Pulumi fails the entire update if any
 *                 resource fails, so this output only appears on success
 *
 * The execution state is a stack output. Query it with:
 *   pulumi stack output execution --json
 *
 * Use it for: operational dashboards, provisioning audit, CI gate checks.
 */

export type PhaseName = "infra" | "bootstrap" | "expose";
export type PhaseStatus = "completed" | "pending";

export interface PhaseRecord {
  readonly phase: PhaseName;
  readonly status: PhaseStatus;
}

export interface ExecutionState {
  /** Unique identifier for this provisioning run: {stack}-{timestamp} */
  readonly installationId: string;
  /** Pulumi stack name */
  readonly stack: string;
  /** Target environment */
  readonly environment: string;
  /** Cloud provider */
  readonly provider: string;
  /** ISO 8601 timestamp when the program was evaluated */
  readonly declaredAt: string;
  /** Per-phase status derived from actual resource readiness */
  readonly phases: PhaseRecord[];
}

/**
 * Builds execution state from phase readiness signals.
 *
 * Each phase's status is derived from actual Pulumi Outputs — not from
 * "we got past this line of code." This means the status reflects real
 * infrastructure state after `pulumi up` completes.
 *
 * Example output after a successful run with bootstrap implemented:
 * {
 *   "installationId": "dev-2026-03-21T10-00-00-000Z",
 *   "phases": [
 *     { "phase": "infra",     "status": "completed" },
 *     { "phase": "bootstrap", "status": "completed" },
 *     { "phase": "expose",    "status": "pending"   }  // stub
 *   ]
 * }
 */
export function buildExecutionState(args: {
  environment: string;
  provider: string;
  infraReady: pulumi.Output<boolean>;
  bootstrapReady: pulumi.Output<boolean>;
  exposeReady: pulumi.Output<boolean>;
}): pulumi.Output<ExecutionState> {
  const declaredAt = new Date().toISOString();
  const stack = pulumi.getStack();
  const installationId = `${stack}-${declaredAt.replace(/[:.]/g, "-")}`;

  return pulumi
    .all([args.infraReady, args.bootstrapReady, args.exposeReady])
    .apply(([infra, bootstrap, expose]) => ({
      installationId,
      stack,
      environment: args.environment,
      provider: args.provider,
      declaredAt,
      phases: [
        { phase: "infra" as const, status: toStatus(infra) },
        { phase: "bootstrap" as const, status: toStatus(bootstrap) },
        { phase: "expose" as const, status: toStatus(expose) },
      ],
    }));
}

function toStatus(ready: boolean): PhaseStatus {
  return ready ? "completed" : "pending";
}
