/**
 * SSE event types for deployment streams.
 *
 * Plan J-17 v1.1 (2026-06-06). Mirrors `pkg/events/deployment.go` — every
 * literal in EventType MUST exist as a constant in that Go file, and
 * every Payload type matches a struct there. NO codegen today; honor
 * system. If you add a new event:
 *   1. Add the constant + struct to pkg/events/deployment.go
 *   2. Add the literal + interface here
 *   3. Bump the cross-ref comment in both files
 */

// Discriminator literals — keep in sync with pkg/events/deployment.go.
export type SSEEventType =
  | 'build.started'
  | 'build.succeeded'
  | 'build.failed'
  | 'build.step.started'
  | 'build.step.succeeded'
  | 'build.step.failed'
  | 'deploy.delivering'
  | 'deploy.verifying'
  | 'deploy.live'
  | 'deploy.completed'
  | 'deploy.sync_failed'
  | 'rollout.aborted';

/** Phase group rendered in the timeline UI (5 groups + 'other' fallback). */
export type PhaseGroup =
  | 'clone'
  | 'install'
  | 'build'
  | 'sign'
  | 'deliver'
  | 'other';

/** Error class — mirrors api/internal/services/error_classifier.go ErrorClass. */
export type ErrorClass =
  | 'OOM'
  | 'BUILD_TIMEOUT'
  | 'IMAGE_PULL_FAILED'
  | 'COSIGN_FAILED'
  | 'REGISTRY_AUTH'
  | 'DOCKERFILE_LINT'
  | 'LOOPZERO_YAML_INVALID'
  | 'CANCELLED_BY_USER'
  | 'WORKFLOW_DELETED'
  | 'UNKNOWN';

export interface BuildStepStartedPayload {
  step_name: string;
  started_at: string; // ISO 8601
  phase_group: PhaseGroup;
}

export interface BuildStepSucceededPayload {
  step_name: string;
  started_at: string;
  ended_at: string;
  duration_seconds: number;
  phase_group: PhaseGroup;
}

export interface BuildStepFailedPayload {
  step_name: string;
  started_at: string;
  ended_at: string;
  duration_seconds: number;
  phase_group: PhaseGroup;
  error_class: ErrorClass;
  error_message: string;
  suggestion: string;
}

/** Discriminated union for SSE event objects coming off the stream. */
export type SSEEvent =
  | { id: string; type: 'build.step.started'; data: BuildStepStartedPayload }
  | { id: string; type: 'build.step.succeeded'; data: BuildStepSucceededPayload }
  | { id: string; type: 'build.step.failed'; data: BuildStepFailedPayload }
  // Lifecycle events have a free-form payload we don't model strictly here.
  | { id: string; type: Exclude<SSEEventType, 'build.step.started' | 'build.step.succeeded' | 'build.step.failed'>; data: Record<string, unknown> };

/** Step model derived from accumulated events in useDeploymentStream. */
export interface BuildStep {
  step_name: string;
  phase_group: PhaseGroup;
  status: 'running' | 'succeeded' | 'failed';
  started_at: string;
  ended_at?: string;
  duration_seconds?: number;
  error_class?: ErrorClass;
  error_message?: string;
  suggestion?: string;
}
