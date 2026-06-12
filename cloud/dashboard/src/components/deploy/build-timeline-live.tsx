import { useDeploymentStream, type DeploymentStatus } from '@/hooks/use-deployment-stream';
import type { BuildStep, PhaseGroup } from '@/lib/api/sse-events';
import { BuildStepCard } from './build-step-card';
import { BuildErrorPill } from './build-error-pill';

/**
 * BuildTimelineLive — Plan J-17 T3.1.
 *
 * Reactive replacement for the static BuildTimeline. Subscribes to the
 * build's SSE stream via useDeploymentStream and renders steps grouped
 * by phase (Clone / Install / Build / Sign / Deliver + Other fallback
 * for non-canonical steps from EC-11).
 *
 * States:
 *   - no events yet (queued): renders a 5-row skeleton placeholder
 *   - running: highlights currentStep with animate-pulse
 *   - failed: renders BuildErrorPill below the timeline
 *   - succeeded: all steps green
 */

export interface BuildTimelineLiveProps {
  buildId: string;
  projectName: string;
  /** Optional Last-Event-ID hint seeded from REST (EC-5). */
  initialLastEventID?: string | null;
}

const PHASE_ORDER: PhaseGroup[] = ['clone', 'install', 'build', 'sign', 'deliver', 'other'];

const PHASE_LABEL: Record<PhaseGroup, string> = {
  clone: 'Clone',
  install: 'Install',
  build: 'Build',
  sign: 'Sign',
  deliver: 'Deliver',
  other: 'Other',
};

export function BuildTimelineLive({ buildId, projectName, initialLastEventID }: BuildTimelineLiveProps) {
  const { steps, currentStep, status, error, connected } = useDeploymentStream(buildId, {
    initialLastEventID,
  });

  const grouped = groupByPhase(steps);
  const empty = steps.length === 0;

  return (
    <div className="space-y-6" data-status={status}>
      {empty ? (
        <Skeleton connected={connected} />
      ) : (
        PHASE_ORDER.map((phase) => {
          const phaseSteps = grouped[phase] ?? [];
          if (phaseSteps.length === 0) return null;
          return (
            <section key={phase} aria-label={`${PHASE_LABEL[phase]} phase`}>
              <h3 className="mb-2 text-label uppercase tracking-wide text-muted-foreground">
                {PHASE_LABEL[phase]}
              </h3>
              <ul className="space-y-2">
                {phaseSteps.map((step) => (
                  <li key={step.step_name}>
                    <BuildStepCard
                      step={step}
                      active={currentStep?.step_name === step.step_name}
                    />
                  </li>
                ))}
              </ul>
            </section>
          );
        })
      )}

      {error && (
        <BuildErrorPill
          errorClass={error.error_class}
          errorMessage={error.error_message}
          suggestion={error.suggestion}
          projectName={projectName}
          buildId={buildId}
        />
      )}

      {status === 'succeeded' && !error && (
        <div
          className="rounded-md border border-success/40 bg-success/5 p-3 text-body-sm text-success"
          role="status"
        >
          Deploy complete — release is live.
        </div>
      )}
    </div>
  );
}

function groupByPhase(steps: BuildStep[]): Partial<Record<PhaseGroup, BuildStep[]>> {
  const result: Partial<Record<PhaseGroup, BuildStep[]>> = {};
  for (const step of steps) {
    (result[step.phase_group] ??= []).push(step);
  }
  return result;
}

interface SkeletonProps {
  connected: boolean;
}

function Skeleton({ connected }: SkeletonProps) {
  return (
    <div className="space-y-3" aria-label="Waiting for build to start">
      <p className="text-body-sm text-muted-foreground">
        {connected ? 'Waiting for build to start…' : 'Connecting to build stream…'}
      </p>
      {PHASE_ORDER.slice(0, 5).map((phase) => (
        <div key={phase} className="rounded-md border border-dashed border-border/60 p-3">
          <div className="h-3 w-24 rounded bg-muted-foreground/10" />
          <div className="mt-2 h-2 w-40 rounded bg-muted-foreground/5" />
        </div>
      ))}
    </div>
  );
}

// Re-export so consumers (TimelineTab) can use the canonical type.
export type { DeploymentStatus };
