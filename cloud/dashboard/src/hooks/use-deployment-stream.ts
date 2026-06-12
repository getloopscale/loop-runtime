import { useMemo } from 'react';
import { useSSEStream } from './use-sse-stream';
import type {
  BuildStep,
  BuildStepStartedPayload,
  BuildStepSucceededPayload,
  BuildStepFailedPayload,
  ErrorClass,
  PhaseGroup,
} from '@/lib/api/sse-events';

/**
 * useDeploymentStream — Plan J-17 T2.2.
 *
 * Wraps useSSEStream<unknown> for a single build's event stream
 * (/engine/api/v1/builds/{buildID}/stream) and reduces the event sequence into
 * a stable map<step_name, BuildStep>. Returns the canonical UI model:
 * steps array (sorted by started_at), currentStep, derived overall
 * status, derived total duration, and (if failed) error.
 *
 * EC-5 dedup: reducer upserts by step_name — events for the same step
 * (started -> succeeded) merge into one BuildStep instead of appending.
 *
 * EC-5 race with REST: caller MAY pass initialLastEventID seeded from
 * the REST snapshot's `last_event_id` field so the SSE only delivers
 * the delta. Without that, the server replays all events (Last-Event-ID
 * defaults to 0) and the reducer still dedups, just at higher cost.
 */

const STEP_EVENT_TYPES = [
  'build.step.started',
  'build.step.succeeded',
  'build.step.failed',
];

export type DeploymentStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | 'unknown';

export interface DeploymentBuildError {
  step_name: string;
  error_class: ErrorClass;
  error_message: string;
  suggestion: string;
}

export interface UseDeploymentStreamResult {
  steps: BuildStep[];
  currentStep: BuildStep | null;
  totalDurationSeconds: number | null;
  status: DeploymentStatus;
  error: DeploymentBuildError | null;
  connected: boolean;
  /** Last-Event-ID observed; useful for resume hint. */
  lastEventID: string | null;
}

export interface UseDeploymentStreamOptions {
  initialLastEventID?: string | null;
}

export function useDeploymentStream(
  buildID: string | null | undefined,
  options: UseDeploymentStreamOptions = {},
): UseDeploymentStreamResult {
  // LIVE bug caught 2026-06-06 J-17 dogfood: dashboard runs at
  // app-dev.loopzero.example; Caddy + cloud BFF proxy only routes /engine/* to
  // loopzero-api. Plain /api/v1/... returns 404. Mirror the engineFetch prefix
  // pattern used by use-log-stream consumers + cloudFetch helpers.
  const path = buildID
    ? `/engine/api/v1/builds/${encodeURIComponent(buildID)}/stream`
    : null;

  const stream = useSSEStream(path, {
    eventTypes: STEP_EVENT_TYPES,
    initialLastEventID: options.initialLastEventID ?? null,
    maxEvents: 200, // 14 canonical steps * 3 events each + cushion
  });

  return useMemo(() => {
    const stepsMap = new Map<string, BuildStep>();

    for (const ev of stream.events) {
      if (ev.type === 'build.step.started') {
        const p = ev.data as BuildStepStartedPayload;
        // Upsert: if a step already exists with status running/failed
        // from a retry, the new started replaces the prior with reset
        // ended_at + duration.
        stepsMap.set(p.step_name, {
          step_name: p.step_name,
          phase_group: p.phase_group as PhaseGroup,
          status: 'running',
          started_at: p.started_at,
        });
      } else if (ev.type === 'build.step.succeeded') {
        const p = ev.data as BuildStepSucceededPayload;
        const prior = stepsMap.get(p.step_name);
        stepsMap.set(p.step_name, {
          ...prior,
          step_name: p.step_name,
          phase_group: p.phase_group as PhaseGroup,
          status: 'succeeded',
          started_at: p.started_at,
          ended_at: p.ended_at,
          duration_seconds: p.duration_seconds,
        });
      } else if (ev.type === 'build.step.failed') {
        const p = ev.data as BuildStepFailedPayload;
        stepsMap.set(p.step_name, {
          step_name: p.step_name,
          phase_group: p.phase_group as PhaseGroup,
          status: 'failed',
          started_at: p.started_at,
          ended_at: p.ended_at,
          duration_seconds: p.duration_seconds,
          error_class: p.error_class as ErrorClass,
          error_message: p.error_message,
          suggestion: p.suggestion,
        });
      }
    }

    const steps = Array.from(stepsMap.values()).sort(
      (a, b) =>
        new Date(a.started_at).getTime() - new Date(b.started_at).getTime(),
    );

    // currentStep: the last running OR most recent (succeeded or failed).
    let currentStep: BuildStep | null = null;
    for (const s of steps) {
      if (s.status === 'running') {
        currentStep = s;
      }
    }
    if (!currentStep && steps.length > 0) {
      currentStep = steps[steps.length - 1];
    }

    // status derivation:
    //   no events => queued
    //   any failed step => failed
    //   any running step => running
    //   all succeeded => succeeded
    //   otherwise => unknown
    let status: DeploymentStatus = 'queued';
    if (steps.length > 0) {
      const anyFailed = steps.some((s) => s.status === 'failed');
      const anyRunning = steps.some((s) => s.status === 'running');
      if (anyFailed) {
        status = 'failed';
      } else if (anyRunning) {
        status = 'running';
      } else if (steps.every((s) => s.status === 'succeeded')) {
        status = 'succeeded';
      } else {
        status = 'unknown';
      }
    }

    // totalDuration:
    //   succeeded => sum of durations
    //   running => now - first.started_at
    let totalDurationSeconds: number | null = null;
    if (steps.length > 0) {
      if (status === 'succeeded' || status === 'failed') {
        totalDurationSeconds = steps.reduce(
          (acc, s) => acc + (s.duration_seconds ?? 0),
          0,
        );
      } else if (status === 'running') {
        const first = steps[0];
        if (first?.started_at) {
          totalDurationSeconds =
            (Date.now() - new Date(first.started_at).getTime()) / 1000;
        }
      }
    }

    // error: first failed step (chronological).
    let error: DeploymentBuildError | null = null;
    const firstFailed = steps.find((s) => s.status === 'failed');
    if (firstFailed) {
      error = {
        step_name: firstFailed.step_name,
        error_class: firstFailed.error_class ?? 'UNKNOWN',
        error_message: firstFailed.error_message ?? '',
        suggestion: firstFailed.suggestion ?? '',
      };
    }

    return {
      steps,
      currentStep,
      totalDurationSeconds,
      status,
      error,
      connected: stream.connected,
      lastEventID: stream.lastEventID,
    };
  }, [stream.events, stream.connected, stream.lastEventID]);
}
